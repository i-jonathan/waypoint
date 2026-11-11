package postgres

import (
	"context"
	"database/sql"
	"errors"
)

// heldLock is the connection whose open transaction is the lock.
type heldLock struct {
	db   *sql.DB
	conn *sql.Conn
}

func (h *heldLock) run(ctx context.Context, write bool, fn func(execer) error) error {
	return fn(h.conn)
}

// Lock is built in the next step of the plan.
func (r *Repo) Lock(ctx context.Context) (func() error, error) {
	return nil, errors.New("postgres lock: not built yet")
}
