// Package mysql stores a board in a MySQL database. The board-to-rows work is in
// sqlstore; this package owns what is specific to MySQL: the driver, the
// connection, TLS, the schema and its transactions.
//
// A Repo holds no open connection between calls, so one that is dropped leaks
// nothing and a server restart is recovered from by the next call. Text columns
// use utf8mb4 with the binary collation, so names and keys that differ only in
// case stay distinct.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	gomysql "github.com/go-sql-driver/mysql"

	"waypoint/internal/app"
	"waypoint/internal/config"
	"waypoint/internal/dbconn"
	"waypoint/internal/domain"
	"waypoint/internal/storage/sqlstore"
)

// connectTimeout bounds how long an unreachable server can hold up a command.
const connectTimeout = 10 * time.Second

// maxName is the length, in characters, of a name or key column.
const maxName = 255

// MySQL error numbers used here.
const (
	errDBAccessDenied  = 1044
	errAccessDenied    = 1045
	errBadDatabase     = 1049
	errTableAccess     = 1142
	errLockWaitTimeout = 1205
	errDeadlock        = 1213
)

type dialect struct{}

// Rebind leaves the ? placeholders as they are.
func (dialect) Rebind(q string) string { return q }

// ValidText accepts everything: MySQL stores any text, a NUL included.
func (dialect) ValidText(string) error { return nil }

// ValidName refuses a name or key longer than its column.
func (dialect) ValidName(s string) error {
	if n := utf8.RuneCountInString(s); n > maxName {
		return fmt.Errorf("is %d characters long; MySQL keeps names and keys of at most %d characters", n, maxName)
	}
	return nil
}

var store = sqlstore.Store{D: dialect{}}

// execer is what a transaction body is given.
type execer = sqlstore.Querier

// Repo is an app.Repository over one MySQL database.
type Repo struct {
	ref config.ProjectRef

	mu     sync.Mutex
	held   *heldLock // set while this Repo holds Lock
	loaded bool      // Load has run, so Save has a revision to compare with
	rev    int64     // the revision Load saw
}

var _ app.Repository = (*Repo)(nil)

// New returns the repository for ref's database. Nothing is connected until a
// call. The password comes from the environment at the time of each call.
func New(ref config.ProjectRef) *Repo { return &Repo{ref: ref} }

// Path says where the board is, without the user or a password.
func (r *Repo) Path() string { return r.ref.Display() }

func password(ref config.ProjectRef) string { return dbconn.Password(os.Getenv, ref.Name) }

func portOf(ref config.ProjectRef) int {
	if ref.Port == 0 {
		return dbconn.DefaultPort("mysql")
	}
	return ref.Port
}

// tlsFor maps the --ssl values to the driver's TLS settings.
func tlsFor(ssl string) string {
	switch ssl {
	case "disable":
		return "false"
	case "prefer":
		return "preferred"
	case "require":
		return "skip-verify" // encrypted, certificate not checked
	case "verify-full":
		return "true"
	}
	return "preferred"
}

// connConfig builds the driver configuration for ref and database ("" for none).
// The password is a structured value, never part of a string.
func connConfig(ref config.ProjectRef, database string) *gomysql.Config {
	ssl := ref.SSL
	if ssl == "" {
		ssl = dbconn.DefaultSSL(ref.Host)
	}
	cfg := gomysql.NewConfig()
	cfg.User = ref.User
	cfg.Passwd = password(ref)
	if strings.HasPrefix(ref.Host, "/") {
		cfg.Net, cfg.Addr = "unix", ref.Host
	} else {
		cfg.Net, cfg.Addr = "tcp", fmt.Sprintf("%s:%d", strings.TrimSuffix(strings.TrimPrefix(ref.Host, "["), "]"), portOf(ref))
		if strings.Contains(ref.Host, ":") && !strings.HasPrefix(ref.Host, "[") {
			cfg.Addr = fmt.Sprintf("[%s]:%d", ref.Host, portOf(ref))
		}
	}
	cfg.DBName = database
	cfg.Collation = "utf8mb4_bin"
	cfg.TLSConfig = tlsFor(ssl)
	cfg.Timeout = connectTimeout
	cfg.ParseTime = false
	cfg.MultiStatements = false
	// Strict, so a value that does not fit is an error and never silently cut.
	cfg.Params = map[string]string{"sql_mode": "'STRICT_ALL_TABLES,NO_ENGINE_SUBSTITUTION'"}
	return cfg
}

// open connects to database ("" for none) on ref's server and proves the
// connection works. Failures are turned into messages a person can act on.
func open(ctx context.Context, ref config.ProjectRef, database string) (*sql.DB, error) {
	connector, err := gomysql.NewConnector(connConfig(ref, database))
	if err != nil {
		return nil, dbconn.Scrub(err, password(ref))
	}
	db := sql.OpenDB(connector)
	cctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := db.PingContext(cctx); err != nil {
		db.Close()
		return nil, connectError(ref, database, err)
	}
	return db, nil
}

// connectError says what went wrong in connecting, naming the server and, for
// a password problem, the variable to set. It never contains the password.
func connectError(ref config.ProjectRef, database string, err error) error {
	pw := password(ref)
	err = dbconn.Scrub(err, pw)
	var me *gomysql.MySQLError
	if errors.As(err, &me) {
		switch me.Number {
		case errAccessDenied:
			if pw == "" {
				return fmt.Errorf("%w: %s wants a password for user %q: %s", dbconn.ErrPassword, ref.Display(), ref.User, dbconn.PasswordHelp(ref.Name))
			}
			return fmt.Errorf("%w: %s rejected the password for user %q: check the value; %s", dbconn.ErrPassword, ref.Display(), ref.User, dbconn.PasswordHelp(ref.Name))
		case errBadDatabase:
			return fmt.Errorf("%w: database %q does not exist on %s:%d; create it, or ask Waypoint to with --create-database", domain.ErrNotFound, database, ref.Host, portOf(ref))
		case errDBAccessDenied:
			return fmt.Errorf("the user %q may not use database %q on %s:%d", ref.User, database, ref.Host, portOf(ref))
		}
		return fmt.Errorf("cannot use %s: %s", ref.Display(), me.Message)
	}
	return fmt.Errorf("%w at %s:%d: %v", dbconn.ErrUnreachable, ref.Host, portOf(ref), err)
}

// plain scrubs a driver error and, for the few the user can act on, says how.
func (r *Repo) plain(err error) error {
	if err == nil || errors.Is(err, domain.ErrInvalid) || errors.Is(err, app.ErrChangedDuringWrite) {
		return err
	}
	err = dbconn.Scrub(err, password(r.ref))
	var me *gomysql.MySQLError
	if errors.As(err, &me) {
		switch me.Number {
		case errTableAccess, errDBAccessDenied:
			return fmt.Errorf("the user %q is not allowed to do that in %s: %s", r.ref.User, r.ref.Display(), me.Message)
		}
	}
	return err
}

// openBoard connects to the board's database and checks it is a Waypoint board
// this build can use.
func (r *Repo) openBoard(ctx context.Context) (*sql.DB, error) {
	db, err := open(ctx, r.ref, r.ref.Database)
	if err != nil {
		return nil, err
	}
	if err := validate(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("%s: %w", r.ref.Display(), r.plain(err))
	}
	return db, nil
}

// inTx runs fn in one transaction on one connection. A read sees one snapshot.
func inTx(ctx context.Context, db *sql.DB, write bool, fn func(execer) error) (err error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if write {
		// Each statement sees what others have committed, which is what a writer
		// that waited for the row lock needs.
		if _, err := conn.ExecContext(ctx, `SET TRANSACTION ISOLATION LEVEL READ COMMITTED`); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, `START TRANSACTION`); err != nil {
			return err
		}
	} else if _, err := conn.ExecContext(ctx, `START TRANSACTION WITH CONSISTENT SNAPSHOT, READ ONLY`); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	if err = fn(conn); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, "COMMIT")
	return err
}

// run does fn in a transaction: inside the lock's transaction when this Repo
// holds the lock, otherwise on a fresh connection that is closed afterwards.
func (r *Repo) run(ctx context.Context, write bool, fn func(execer) error) error {
	r.mu.Lock()
	h := r.held
	r.mu.Unlock()
	if h != nil {
		return r.plain(h.run(ctx, write, fn))
	}
	db, err := r.openBoard(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	return r.plain(inTx(ctx, db, write, fn))
}

// Load reads the board. Cards without ids cannot exist in a database, so the
// list of assigned ids is always empty.
func (r *Repo) Load(ctx context.Context) (*domain.Board, []domain.CardID, error) {
	var b *domain.Board
	var rev int64
	err := r.run(ctx, false, func(q execer) (err error) {
		b, rev, err = store.Load(ctx, q)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	r.mu.Lock()
	r.loaded, r.rev = true, rev
	r.mu.Unlock()
	return b, nil, nil
}

// lockRevision reads the revision and takes the row lock on the project row,
// so a concurrent writer waits here and then sees the revision it left.
func lockRevision(ctx context.Context, q execer) (int64, error) {
	var rev int64
	err := q.QueryRowContext(ctx, `SELECT revision FROM project WHERE id = 1 FOR UPDATE`).Scan(&rev)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return rev, err
}

// Save replaces the stored board with b. If the database was saved since this
// Repo last loaded it, it returns app.ErrChangedDuringWrite and changes nothing.
func (r *Repo) Save(ctx context.Context, b *domain.Board) error {
	r.mu.Lock()
	loaded, seen := r.loaded, r.rev
	r.mu.Unlock()
	var rev int64
	err := r.run(ctx, true, func(q execer) (err error) {
		cur, err := lockRevision(ctx, q)
		if err != nil {
			return err
		}
		if loaded && cur != seen {
			return app.ErrChangedDuringWrite
		}
		rev, err = store.Save(ctx, q, b)
		return err
	})
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.loaded, r.rev = true, rev
	r.mu.Unlock()
	return nil
}

// Revision returns the project's save counter: a number that changes with every
// save, for watching a board without loading it. It is one short query and does
// not check the schema.
func (r *Repo) Revision(ctx context.Context) (int64, error) {
	db, err := open(ctx, r.ref, r.ref.Database)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var rev int64
	err = db.QueryRowContext(ctx, `SELECT revision FROM project WHERE id = 1`).Scan(&rev)
	return rev, r.plain(err)
}

// Discard drops the tables a Waypoint board owns, and nothing else.
func (r *Repo) Discard(ctx context.Context) error {
	db, err := open(ctx, r.ref, r.ref.Database)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `DROP TABLE IF EXISTS `+strings.Join(waypointTables, ", "))
	return r.plain(err)
}

// Create makes a new board in ref's database. The database must hold no tables.
// With createDatabase a missing database is created first (an existing one is
// used). MySQL cannot roll back DDL, so a failure drops the tables this call
// made and leaves nothing.
func Create(ctx context.Context, ref config.ProjectRef, s domain.Settings, createDatabase bool) (*Repo, error) {
	return CreateFrom(ctx, ref, &domain.Board{Settings: s}, createDatabase)
}

// afterSave is a test seam: CreateFrom calls it once the board is written and
// before the board is released.
var afterSave = func() error { return nil }

// CreateFrom is Create with the board's cards written too. MySQL cannot make the
// schema and the cards one transaction, so the board is created marked retired
// and released only when every card is in: a crash in between leaves a board
// that cannot be mistaken for a finished one.
func CreateFrom(ctx context.Context, ref config.ProjectRef, b *domain.Board, createDatabase bool) (*Repo, error) {
	if createDatabase {
		if err := createDB(ctx, ref); err != nil {
			return nil, err
		}
	}
	db, err := open(ctx, ref, ref.Database)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	r := New(ref)
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, r.plain(err)
	}
	defer conn.Close()
	rels, err := relations(ctx, conn)
	if err != nil {
		return nil, r.plain(err)
	}
	if len(rels) > 0 {
		return nil, fmt.Errorf("%w: %s is not empty (it holds %s); a new board needs an empty database", domain.ErrInvalid, ref.Display(), rels[0])
	}
	if err := migrate(ctx, conn); err != nil {
		return nil, r.plain(err)
	}
	err = inTx(ctx, db, true, func(q execer) error {
		_, err := store.Save(ctx, q, b)
		return err
	})
	if err == nil {
		err = afterSave()
	}
	if err == nil {
		_, err = db.ExecContext(ctx, "UPDATE waypoint_meta SET value = 'false' WHERE `key` = 'retired'")
	}
	if err != nil {
		_, _ = db.ExecContext(context.Background(), `DROP TABLE IF EXISTS `+strings.Join(waypointTables, ", "))
		return nil, r.plain(err)
	}
	return r, nil
}

// quoteIdent quotes a MySQL identifier.
func quoteIdent(name string) string { return "`" + strings.ReplaceAll(name, "`", "``") + "`" }

// createDB creates ref's database if it does not exist. It connects without a
// default database to do so.
func createDB(ctx context.Context, ref config.ProjectRef) error {
	if ref.Database == "" || strings.ContainsRune(ref.Database, 0) || utf8.RuneCountInString(ref.Database) > 64 {
		return fmt.Errorf("%w: %q is not a usable MySQL database name (1-64 characters, no NUL)", domain.ErrInvalid, ref.Database)
	}
	db, err := open(ctx, ref, "")
	if err != nil {
		return err
	}
	defer db.Close()
	var exists int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = ?`, ref.Database).Scan(&exists); err != nil {
		return dbconn.Scrub(err, password(ref))
	}
	if exists > 0 {
		return nil
	}
	stmt := `CREATE DATABASE ` + quoteIdent(ref.Database) + ` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		err = dbconn.Scrub(err, password(ref))
		var me *gomysql.MySQLError
		if errors.As(err, &me) && (me.Number == errDBAccessDenied || me.Number == errTableAccess) {
			return fmt.Errorf("the user %q may not create databases on %s:%d; create it yourself with: %s;", ref.User, ref.Host, portOf(ref), stmt)
		}
		return err
	}
	return nil
}

// ProbeResult says what is on the server for a project about to be set up.
type ProbeResult = dbconn.ProbeResult

// Probe looks at the server without changing anything.
func Probe(ctx context.Context, ref config.ProjectRef) (ProbeResult, error) {
	db, err := open(ctx, ref, ref.Database)
	if errors.Is(err, domain.ErrNotFound) {
		// The database is missing; the server itself may still be fine.
		m, merr := open(ctx, ref, "")
		if merr != nil {
			return ProbeResult{}, merr
		}
		m.Close()
		return ProbeResult{Reachable: true}, nil
	}
	if err != nil {
		return ProbeResult{}, err
	}
	defer db.Close()
	res := ProbeResult{Reachable: true, DatabaseExists: true}
	rels, err := relations(ctx, db)
	if err != nil {
		return res, dbconn.Scrub(err, password(ref))
	}
	res.Empty = len(rels) == 0
	for _, t := range rels {
		if t == "waypoint_meta" {
			res.HasBoard = true
		}
	}
	return res, nil
}
