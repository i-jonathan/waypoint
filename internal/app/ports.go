// Package app holds Waypoint's use cases and the ports they depend on.
package app

import (
	"context"
	"time"

	"waypoint/internal/domain"
)

// Repository stores one board and its archive.
type Repository interface {
	// Load reads the board and its archive. The second result lists the ids
	// given to cards that had none; Load does not write them back.
	Load(ctx context.Context) (*domain.Board, []domain.CardID, error)
	// Save replaces the stored board and archive with b.
	Save(ctx context.Context, b *domain.Board) error
	// Lock takes an exclusive, cross-process lock on the board. It waits until
	// ctx is done.
	Lock(ctx context.Context) (unlock func() error, err error)
	// Path names where the board is stored.
	Path() string
}

// Clock returns the current time. Use cases take one so tests can fix it.
type Clock func() time.Time
