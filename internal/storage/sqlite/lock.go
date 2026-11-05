package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	msqlite "modernc.org/sqlite"
)

// lockPoll is how often a waiting Lock tries again.
const lockPoll = 10 * time.Millisecond

// heldLock is the connection whose open write transaction is the lock. While it
// is held every other writer, in any process, waits; readers do not (WAL).
type heldLock struct {
	db   *sql.DB
	conn *sql.Conn
}

// Lock takes the exclusive write lock by starting a write transaction on a
// pinned connection and keeping it open until unlock. It waits until ctx is done.
// Load and Save on this Repo then run inside that transaction.
func (r *Repo) Lock(ctx context.Context) (func() error, error) {
	db, err := r.open(ctx)
	if err != nil {
		return nil, r.plain(err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		db.Close()
		return nil, r.plain(err)
	}
	release := func() {
		conn.Close()
		db.Close()
	}
	// Poll instead of using the driver's busy wait, so ctx ends the wait at once.
	if _, err := conn.ExecContext(ctx, `PRAGMA busy_timeout = 0`); err != nil {
		release()
		return nil, r.plain(err)
	}
	for {
		_, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`)
		if err == nil {
			break
		}
		if ctx.Err() == nil && !isBusy(err) {
			release()
			return nil, r.plain(err)
		}
		if ctx.Err() == nil && sleepCtx(ctx, lockPoll) {
			continue
		}
		release()
		return nil, fmt.Errorf("the project database is busy: another program has it locked: %w", ctx.Err())
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
		_, err := conn.ExecContext(context.Background(), `COMMIT`)
		release()
		return err
	}, nil
}

func isBusy(err error) bool {
	var se *msqlite.Error
	return errors.As(err, &se) && (se.Code()&0xff == sqliteBusy || se.Code()&0xff == sqliteLocked)
}

// sleepCtx waits d and reports false if ctx ended first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
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
		h.conn.ExecContext(context.Background(), `ROLLBACK TO op`)
		h.conn.ExecContext(context.Background(), `RELEASE op`)
		return err
	}
	_, err = h.conn.ExecContext(ctx, `RELEASE op`)
	return err
}
