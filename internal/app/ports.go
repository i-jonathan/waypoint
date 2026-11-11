// Package app holds Waypoint's use cases and the ports they depend on.
package app

import (
	"context"
	"errors"
	"time"

	"waypoint/internal/domain"
)

// ErrChangedDuringWrite is what Repository.Save returns when a stored file is no
// longer the one Load read: something outside Waypoint wrote it in between.
// Save has stopped without replacing that file, so the caller loads again and
// redoes its change on what is there now.
var ErrChangedDuringWrite = errors.New("the board changed on disk during the save")

// Repository stores one board and its archive.
type Repository interface {
	// Load reads the board and its archive. The second result lists the ids
	// given to cards that had none; Load does not write them back.
	Load(ctx context.Context) (*domain.Board, []domain.CardID, error)
	// Save replaces the stored board and archive with b. If they changed since
	// this Repository last loaded them it returns ErrChangedDuringWrite. A Save
	// with no Load before it has nothing to compare with and skips that check.
	Save(ctx context.Context, b *domain.Board) error
	// Lock takes an exclusive, cross-process lock on the board. It waits until
	// ctx is done.
	Lock(ctx context.Context) (unlock func() error, err error)
	// Path names where the board is stored.
	Path() string
}

// Clock returns the current time. Use cases take one so tests can fix it.
type Clock func() time.Time

// ErrBusy is what Lock returns when it gave up waiting: another program holds
// the board and did not let go in time.
var ErrBusy = errors.New("the project database is busy")
