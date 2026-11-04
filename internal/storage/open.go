// Package storage chooses a repository implementation for a registered project.
package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"waypoint/internal/app"
	"waypoint/internal/config"
	"waypoint/internal/domain"
	"waypoint/internal/storage/markdown"
)

// ErrSQLite is returned for a project registered with sqlite storage, which
// is not built yet.
var ErrSQLite = errors.New("sqlite storage arrives in a later version")

// Open returns the repository for an existing board. It does not touch the
// disk.
func Open(ref config.ProjectRef) (app.Repository, error) {
	switch ref.Storage {
	case "markdown":
		return markdown.New(ref.Path), nil
	case "sqlite":
		return nil, ErrSQLite
	default:
		return nil, fmt.Errorf("%w: project %q has unknown storage %q", domain.ErrInvalid, ref.Name, ref.Storage)
	}
}

// Create makes a new, empty board with settings s and returns its repository.
// Missing parent directories are created. An existing file is never
// overwritten: it gives os.ErrExist.
func Create(ref config.ProjectRef, s domain.Settings) (app.Repository, error) {
	switch ref.Storage {
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
		return nil, ErrSQLite
	default:
		return nil, fmt.Errorf("%w: project %q has unknown storage %q", domain.ErrInvalid, ref.Name, ref.Storage)
	}
}
