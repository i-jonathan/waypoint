// Package sqlite stores a board in one SQLite file. The board-to-rows work is
// in sqlstore; this package owns what is specific to SQLite: the driver, the
// file, its pragmas and its transactions.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"waypoint/internal/app"
	"waypoint/internal/domain"
	"waypoint/internal/storage/sqlstore"
)

type dialect struct{}

func (dialect) Rebind(q string) string { return q }

var store = sqlstore.Store{D: dialect{}}

// execer is what a pinned connection offers a transaction body.
type execer = sqlstore.Querier

// Repo is an app.Repository over one SQLite file. It holds no open database
// between calls, so a Repo that is dropped leaks nothing.
type Repo struct {
	path string

	mu     sync.Mutex
	loaded bool  // Load has run, so Save has a revision to compare with
	rev    int64 // the revision Load saw
}

var _ app.Repository = (*Repo)(nil)

// New returns the repository for the database at path. It does not touch the file.
func New(path string) *Repo { return &Repo{path: path} }

// Path names the database file.
func (r *Repo) Path() string { return r.path }

// Create makes a new database at path holding an empty board with settings s.
// The parent directory must exist. An existing file is never touched: it gives
// os.ErrExist. A failure leaves no file behind.
func Create(path string, s domain.Settings) (*Repo, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, err
	}
	f.Close()
	fail := func(err error) (*Repo, error) {
		os.Remove(path)
		return nil, err
	}
	ctx := context.Background()
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return fail(err)
	}
	defer db.Close()
	var mode string
	if err := db.QueryRowContext(ctx, `PRAGMA journal_mode = WAL`).Scan(&mode); err != nil {
		return fail(err)
	}
	if err := migrate(ctx, db); err != nil {
		return fail(err)
	}
	err = inTx(ctx, db, true, func(q execer) error {
		_, err := store.Save(ctx, q, &domain.Board{Settings: s})
		return err
	})
	if err != nil {
		return fail(err)
	}
	return New(path), nil
}

// dsn is the connection string: the file must already exist, and a busy
// database is waited for.
func dsn(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	return "file:" + (&url.URL{Path: abs}).EscapedPath() + "?" + q.Encode()
}

// open connects to an existing Waypoint database. The file is checked before
// anything is written to it.
func (r *Repo) open(ctx context.Context) (*sql.DB, error) {
	if _, err := os.Stat(r.path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn(r.path))
	if err != nil {
		return nil, err
	}
	if err := validate(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("%s: %w", r.path, err)
	}
	return db, nil
}

// inTx runs fn in one transaction on one connection. write takes the write
// lock up front (BEGIN IMMEDIATE), so two writers queue instead of failing
// when one tries to upgrade.
func inTx(ctx context.Context, db *sql.DB, write bool, fn func(execer) error) (err error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	begin := "BEGIN"
	if write {
		begin = "BEGIN IMMEDIATE"
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

// Load reads the board. Cards without ids cannot exist in a database, so the
// list of assigned ids is always empty.
func (r *Repo) Load(ctx context.Context) (*domain.Board, []domain.CardID, error) {
	db, err := r.open(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()
	var b *domain.Board
	var rev int64
	err = inTx(ctx, db, false, func(q execer) (err error) {
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

// Save replaces the stored board with b. If the database was saved since this
// Repo last loaded it, it returns app.ErrChangedDuringWrite and changes nothing.
func (r *Repo) Save(ctx context.Context, b *domain.Board) error {
	db, err := r.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	r.mu.Lock()
	loaded, seen := r.loaded, r.rev
	r.mu.Unlock()
	var rev int64
	err = inTx(ctx, db, true, func(q execer) error {
		if loaded {
			cur, err := store.Revision(ctx, q)
			if err != nil {
				return err
			}
			if cur != seen {
				return app.ErrChangedDuringWrite
			}
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

// Lock is built in the next task.
func (r *Repo) Lock(ctx context.Context) (func() error, error) {
	return nil, errors.ErrUnsupported
}
