package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	gomysql "github.com/go-sql-driver/mysql"

	"waypoint/internal/app"
)

// lockWaitLimit is how long Lock waits when its caller set no deadline, so a
// program that holds the board for good makes a command fail instead of hang.
var lockWaitLimit = 30 * time.Second

// engineLockTimeout is the server-side wait, kept below lockWaitLimit so the
// server gives up on its own if the client cannot cancel.
const engineLockTimeoutSeconds = 25

// heldLock is the connection whose open transaction holds the row lock on the
// project row. While it is held every other writer, in any process, waits.
type heldLock struct {
	db   *sql.DB
	conn *sql.Conn
}

// Lock takes the exclusive lock by starting a transaction on a pinned
// connection and locking the project row in it, then keeps the transaction
// open until unlock. Load and Save on this Repo then run inside it. It waits
// until ctx is done, or lockWaitLimit when ctx has no deadline; giving up
// returns an error wrapping app.ErrBusy.
func (r *Repo) Lock(ctx context.Context) (func() error, error) {
	if _, has := ctx.Deadline(); !has {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, lockWaitLimit)
		defer cancel()
	}
	db, err := r.openBoard(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		db.Close()
		return nil, r.plain(err)
	}
	fail := func(err error) (func() error, error) {
		conn.ExecContext(context.Background(), `ROLLBACK`)
		conn.Close()
		db.Close()
		return nil, r.lockError(ctx, err)
	}
	if _, err := conn.ExecContext(ctx, `SET SESSION innodb_lock_wait_timeout = `+strconv.Itoa(engineLockTimeoutSeconds)); err != nil {
		return fail(err)
	}
	if _, err := conn.ExecContext(ctx, `SET TRANSACTION ISOLATION LEVEL READ COMMITTED`); err != nil {
		return fail(err)
	}
	if _, err := conn.ExecContext(ctx, `START TRANSACTION`); err != nil {
		return fail(err)
	}
	if _, err := lockRevision(ctx, conn); err != nil {
		return fail(err)
	}
	// The database may have been retired while this call waited for the lock.
	if err := validate(ctx, conn); err != nil {
		return fail(err)
	}
	h := &heldLock{db: db, conn: conn}
	r.mu.Lock()
	r.held = h
	r.mu.Unlock()
	return func() error {
		r.mu.Lock()
		cur := r.held
		if cur == h {
			r.held = nil
		}
		r.mu.Unlock()
		if cur != h { // already unlocked
			return nil
		}
		uctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := conn.ExecContext(uctx, `COMMIT`)
		conn.Close()
		db.Close()
		return r.plain(err)
	}, nil
}

// lockError turns a failed wait into app.ErrBusy, and anything else into a
// plain error.
func (r *Repo) lockError(ctx context.Context, err error) error {
	var me *gomysql.MySQLError
	switch {
	case errors.As(err, &me) && (me.Number == errLockWaitTimeout || me.Number == errDeadlock),
		ctx.Err() != nil:
		cause := err
		if ctx.Err() != nil {
			cause = ctx.Err()
		}
		return fmt.Errorf("%w: another program has the board locked: %w", app.ErrBusy, cause)
	}
	return r.plain(err)
}

// run does fn on the lock's connection. A write is wrapped in a savepoint, so
// a failed save undoes itself and leaves the lock's transaction usable.
func (h *heldLock) run(ctx context.Context, write bool, fn func(execer) error) (err error) {
	if !write {
		return fn(h.conn)
	}
	if _, err := h.conn.ExecContext(ctx, `SAVEPOINT op`); err != nil {
		return err
	}
	if err = fn(h.conn); err != nil {
		h.conn.ExecContext(context.Background(), `ROLLBACK TO SAVEPOINT op`)
		h.conn.ExecContext(context.Background(), `RELEASE SAVEPOINT op`)
		return err
	}
	_, err = h.conn.ExecContext(ctx, `RELEASE SAVEPOINT op`)
	return err
}

// Retire marks the database as converted away, inside the transaction the lock
// holds. A writer already waiting for the lock gets it only to find the mark
// and fail, instead of saving into a database the project has left. Call it
// after the project's new home is registered and before unlock.
func (r *Repo) Retire(ctx context.Context) error {
	r.mu.Lock()
	h := r.held
	r.mu.Unlock()
	if h == nil {
		return errors.New("a database can be retired only while its lock is held")
	}
	_, err := h.conn.ExecContext(ctx, "UPDATE waypoint_meta SET value = 'true' WHERE `key` = 'retired'")
	return r.plain(err)
}

// CheckLock proves the lock is still held: the connection that holds it is
// alive and the board is at the revision this Repo last loaded. A lock whose
// connection died is gone, and anything may have been saved since.
func (r *Repo) CheckLock(ctx context.Context) error {
	r.mu.Lock()
	h, loaded, seen := r.held, r.loaded, r.rev
	r.mu.Unlock()
	if h == nil {
		return errors.New("the board is not locked")
	}
	cur, err := lockRevision(ctx, h.conn)
	if err != nil {
		return fmt.Errorf("the lock on the board was lost (%v); nothing was changed, try again", r.plain(err))
	}
	if loaded && cur != seen {
		return fmt.Errorf("the lock on the board was lost: it was saved by someone else meanwhile; nothing was changed, try again")
	}
	return nil
}
