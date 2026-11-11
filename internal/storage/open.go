// Package storage chooses a repository implementation for a registered project.
package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"waypoint/internal/app"
	"waypoint/internal/config"
	"waypoint/internal/dbconn"
	"waypoint/internal/domain"
	"waypoint/internal/storage/markdown"
	"waypoint/internal/storage/postgres"
	"waypoint/internal/storage/sqlite"
)

// sqliteMagic starts every SQLite database file.
const sqliteMagic = "SQLite format 3\x00"

// Sniff says what kind of board file is at path, by its content and not its
// name: "sqlite" for a SQLite database, otherwise "markdown". A missing file
// gives os.ErrNotExist and an empty one domain.ErrInvalid. It does not check
// that the file is a Waypoint board; opening it does.
func Sniff(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	head := make([]byte, len(sqliteMagic))
	n, err := io.ReadFull(f, head)
	switch {
	case n == 0 && (err == io.EOF || err == nil):
		return "", fmt.Errorf("%w: %s is empty", domain.ErrInvalid, path)
	case err != nil && err != io.ErrUnexpectedEOF:
		return "", err
	}
	if string(head[:n]) == sqliteMagic {
		return "sqlite", nil
	}
	return "markdown", nil
}

// Open returns the repository for an existing board. It does not read or
// write the board.
func Open(ref config.ProjectRef) (app.Repository, error) {
	switch ref.Storage {
	case "markdown":
		return markdown.New(ref.Path), nil
	case "sqlite":
		return sqlite.New(ref.Path), nil
	case "postgres":
		return postgres.New(ref), nil
	default:
		return nil, fmt.Errorf("%w: project %q has unknown storage %q", domain.ErrInvalid, ref.Name, ref.Storage)
	}
}

// CreateOptions adjusts Create for a server project.
type CreateOptions struct {
	// CreateDatabase makes the database if it does not exist.
	CreateDatabase bool
	// Board, when set, is written into a server board in the same step that
	// creates it, so the board never exists half-filled. Files ignore it.
	Board *domain.Board
}

// Create makes a new, empty board with settings s and returns its repository.
// Missing parent directories are created. An existing file is never
// overwritten: it gives os.ErrExist.
func Create(ref config.ProjectRef, s domain.Settings) (app.Repository, error) {
	return CreateWith(context.Background(), ref, s, CreateOptions{})
}

// CreateWith is Create with a context and options. A server project's schema
// must be empty; see postgres.Create.
func CreateWith(ctx context.Context, ref config.ProjectRef, s domain.Settings, opts CreateOptions) (app.Repository, error) {
	switch ref.Storage {
	case "postgres":
		b := opts.Board
		if b == nil {
			b = &domain.Board{Settings: s}
		}
		repo, err := postgres.CreateFrom(ctx, ref, b, opts.CreateDatabase)
		if err != nil {
			return nil, err
		}
		return repo, nil
	case "markdown":
		if err := os.MkdirAll(filepath.Dir(ref.Path), 0o755); err != nil {
			return nil, fmt.Errorf("create directory for %s: %w", ref.Path, err)
		}
		repo, err := markdown.Create(ref.Path, s)
		if err != nil {
			return nil, err
		}
		return repo, nil
	case "sqlite":
		if err := os.MkdirAll(filepath.Dir(ref.Path), 0o755); err != nil {
			return nil, fmt.Errorf("create directory for %s: %w", ref.Path, err)
		}
		repo, err := sqlite.Create(ref.Path, s)
		if err != nil {
			return nil, err
		}
		return repo, nil
	default:
		return nil, fmt.Errorf("%w: project %q has unknown storage %q", domain.ErrInvalid, ref.Name, ref.Storage)
	}
}

// Probe looks at a server project's database without changing it.
func Probe(ctx context.Context, ref config.ProjectRef) (dbconn.ProbeResult, error) {
	switch ref.Storage {
	case "postgres":
		return postgres.Probe(ctx, ref)
	}
	return dbconn.ProbeResult{}, fmt.Errorf("%w: project %q is not stored on a database server", domain.ErrInvalid, ref.Name)
}

// Discard removes the tables a server board owns and nothing else. It is how a
// board made by an operation that then failed is taken back.
func Discard(ctx context.Context, ref config.ProjectRef) error {
	switch ref.Storage {
	case "postgres":
		return postgres.New(ref).Discard(ctx)
	}
	return fmt.Errorf("%w: project %q is not stored on a database server", domain.ErrInvalid, ref.Name)
}

// RetireStatement is the SQL that marks a server board as retired, for a
// message that tells a person how to do it by hand when Waypoint could not.
func RetireStatement() string {
	return "UPDATE waypoint_meta SET value = 'true' WHERE key = 'retired'"
}
