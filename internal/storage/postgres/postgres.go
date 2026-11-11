// Package postgres stores a board in a PostgreSQL database. The board-to-rows
// work is in sqlstore; this package owns what is specific to Postgres: the
// driver, the connection, TLS, the schema and its transactions.
//
// A Repo holds no open connection between calls, so one that is dropped leaks
// nothing and a server restart is recovered from by the next call.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"

	"waypoint/internal/app"
	"waypoint/internal/config"
	"waypoint/internal/dbconn"
	"waypoint/internal/domain"
	"waypoint/internal/storage/sqlstore"
)

// connectTimeout bounds how long an unreachable server can hold up a command.
const connectTimeout = 10 * time.Second

type dialect struct{}

// Rebind turns each ? into $1, $2, ...
func (dialect) Rebind(q string) string {
	var b strings.Builder
	n := 0
	for _, r := range q {
		if r == '?' {
			n++
			b.WriteString("$" + strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ValidText refuses NUL, which a Postgres text value cannot hold.
func (dialect) ValidText(s string) error {
	if strings.ContainsRune(s, 0) {
		return errors.New("contains a NUL character, which PostgreSQL cannot store; remove it")
	}
	if !utf8.ValidString(s) {
		return errors.New("is not valid UTF-8 text (a stray byte from another encoding?), which PostgreSQL cannot store; fix it")
	}
	return nil
}

var store = sqlstore.Store{D: dialect{}}

// execer is what a transaction body is given.
type execer = sqlstore.Querier

// Repo is an app.Repository over one Postgres database.
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

// quoteDSN quotes one value of a key/value connection string.
func quoteDSN(v string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(v) + "'"
}

// connConfig builds the driver configuration for ref and database. The
// password is set on the structure afterwards, so it is never part of a string.
func connConfig(ref config.ProjectRef, database string) (*pgx.ConnConfig, error) {
	ssl := ref.SSL
	if ssl == "" {
		ssl = dbconn.DefaultSSL(ref.Host)
	}
	port := ref.Port
	if port == 0 {
		port = dbconn.DefaultPort("postgres")
	}
	dsn := strings.Join([]string{
		"host=" + quoteDSN(dbconn.BareHost(ref.Host)),
		"port=" + strconv.Itoa(port),
		"user=" + quoteDSN(ref.User),
		"dbname=" + quoteDSN(database),
		"sslmode=" + ssl,
		"connect_timeout=" + strconv.Itoa(int(connectTimeout/time.Second)),
		// Never hand the password over in clear text or as an md5 hash: only a
		// SCRAM exchange (which does not reveal it), or no password at all. A
		// server the user pointed Waypoint at by mistake cannot collect it.
		"require_auth=none,scram-sha-256",
	}, " ")
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, dbconn.Scrub(err, password(ref))
	}
	cfg.Password = password(ref)
	cfg.RuntimeParams["application_name"] = "waypoint"
	return cfg, nil
}

// open connects to database on ref's server and proves the connection works.
// Failures are turned into messages a person can act on.
func open(ctx context.Context, ref config.ProjectRef, database string) (*sql.DB, error) {
	cfg, err := connConfig(ref, database)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(stdlib.GetConnector(*cfg))
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
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.Code {
		case "28P01", "28000": // invalid password, invalid authorization
			if pw == "" {
				return fmt.Errorf("%w: %s wants a password for user %q: %s", dbconn.ErrPassword, ref.Display(), ref.User, dbconn.PasswordHelp(ref.Name))
			}
			return fmt.Errorf("%w: %s rejected the password for user %q: check the value; %s", dbconn.ErrPassword, ref.Display(), ref.User, dbconn.PasswordHelp(ref.Name))
		case "3D000": // invalid catalog name
			return fmt.Errorf("%w: database %q does not exist on %s:%d; create it, or ask Waypoint to with --create-database", domain.ErrNotFound, database, ref.Host, portOf(ref))
		}
		return fmt.Errorf("cannot use %s: %s", ref.Display(), pe.Message)
	}
	if strings.Contains(err.Error(), "require_auth") {
		// The server answered; it asked for a password method Waypoint refuses.
		return fmt.Errorf("%w: %s uses md5 or clear-text password authentication, and Waypoint accepts only SCRAM (scram-sha-256) so the password cannot be collected by a look-alike server; change the server's pg_hba.conf and the user's password to SCRAM", domain.ErrInvalid, ref.Display())
	}
	return fmt.Errorf("%w at %s:%d: %v", dbconn.ErrUnreachable, ref.Host, portOf(ref), err)
}

func portOf(ref config.ProjectRef) int {
	if ref.Port == 0 {
		return dbconn.DefaultPort("postgres")
	}
	return ref.Port
}

// plain scrubs a driver error and, for the few the user can act on, says how.
func (r *Repo) plain(err error) error {
	if err == nil || errors.Is(err, domain.ErrInvalid) || errors.Is(err, app.ErrChangedDuringWrite) {
		return err
	}
	err = dbconn.Scrub(err, password(r.ref))
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "42501" {
		return fmt.Errorf("the user %q is not allowed to do that in %s: %s", r.ref.User, r.ref.Display(), pe.Message)
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
	begin := "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY"
	if write {
		begin = "BEGIN"
	}
	if _, err := conn.ExecContext(ctx, begin); err != nil {
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
	return r.plain(inTx(ctx, db, true, func(q execer) error {
		_, err := q.ExecContext(ctx, `DROP TABLE IF EXISTS `+strings.Join(waypointTables, ", "))
		return err
	}))
}

// Create makes a new board in ref's database. The schema must be empty. With
// createDatabase a missing database is created first (an existing one is
// used). All tables are made in one transaction, so a failure leaves nothing.
func Create(ctx context.Context, ref config.ProjectRef, s domain.Settings, createDatabase bool) (*Repo, error) {
	return CreateFrom(ctx, ref, &domain.Board{Settings: s}, createDatabase)
}

// afterSave is a test seam: CreateFrom calls it once the board is written and
// before the transaction commits.
var afterSave = func() error { return nil }

// CreateFrom is Create with the board's cards written in the same transaction as
// the schema, so no moment exists at which a valid but empty board is there
// and the cards are not.
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
	err = inTx(ctx, db, true, func(q execer) error {
		rels, err := relations(ctx, q)
		if err != nil {
			return err
		}
		if len(rels) > 0 {
			return fmt.Errorf("%w: %s is not empty (it holds %s); a new board needs an empty database", domain.ErrInvalid, ref.Display(), rels[0])
		}
		if err := migrate(ctx, q); err != nil {
			return err
		}
		if _, err = store.Save(ctx, q, b); err != nil {
			return err
		}
		return afterSave()
	})
	if err != nil {
		return nil, r.plain(err)
	}
	return r, nil
}

// quoteIdent quotes a Postgres identifier.
func quoteIdent(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

// createDB creates ref's database if it does not exist. It connects to the
// maintenance database to do so.
func createDB(ctx context.Context, ref config.ProjectRef) error {
	if ref.Database == "" || strings.ContainsRune(ref.Database, 0) || len(ref.Database) > 63 {
		return fmt.Errorf("%w: %q is not a usable Postgres database name (1-63 characters, no NUL)", domain.ErrInvalid, ref.Database)
	}
	db, err := open(ctx, ref, "postgres")
	if err != nil {
		return err
	}
	defer db.Close()
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, ref.Database).Scan(&exists); err != nil {
		return dbconn.Scrub(err, password(ref))
	}
	if exists {
		return nil
	}
	stmt := `CREATE DATABASE ` + quoteIdent(ref.Database) + ` ENCODING 'UTF8'`
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		err = dbconn.Scrub(err, password(ref))
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "42501" {
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
		m, merr := open(ctx, ref, "postgres")
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
