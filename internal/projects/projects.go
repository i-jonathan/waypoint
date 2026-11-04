// Package projects holds the registry operations the CLI and the web API
// share: adding a project and checking a registry entry.
package projects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"waypoint/internal/config"
	"waypoint/internal/domain"
	"waypoint/internal/storage"
)

// DefaultPrefix is the card id prefix of a new project when none is given.
const DefaultPrefix = "T"

// AddOptions says which project to add.
type AddOptions struct {
	Name   string
	Path   string // "" means <data_dir>/<slug of Name>.md
	Prefix string
	// Cwd resolves a relative Path. With Cwd empty a relative Path is refused.
	Cwd string
	// FromInit words the "already registered" error for "waypoint init".
	FromInit bool
	// InitFile names the board file in that error; used only with FromInit.
	InitFile string
}

// Add registers a project and opens or creates its board.
//
// The order is: validate, then under the config lock check the registry, create
// or open the board, and only then write the config. A failure before the last
// step leaves the config as it was. A path already registered, even through a
// symlink, is refused, and so is a sqlite path (sqlite storage is not built).
// If the board was created but the config could not be saved, the error says
// the new file was left behind.
func Add(ctx context.Context, cfgPath string, env func(string) string, o AddOptions) (config.ProjectRef, error) {
	name := strings.TrimSpace(o.Name)
	if name == "" {
		return config.ProjectRef{}, fmt.Errorf("%w: project name is empty", domain.ErrInvalid)
	}
	prefix := o.Prefix
	if prefix == "" {
		prefix = DefaultPrefix
	}
	settings := domain.NewBoard(name, prefix).Settings
	if err := settings.Validate(); err != nil {
		return config.ProjectRef{}, err
	}

	var ref config.ProjectRef
	var created bool
	err := config.Update(cfgPath, env, func(cfg *config.Config) error {
		path, err := boardPath(cfg, name, o.Path, o.Cwd)
		if err != nil {
			return err
		}
		if q, ok := RegisteredAt(cfg, path); ok {
			if o.FromInit {
				return fmt.Errorf("%w: %s here is already registered as project %q", domain.ErrInvalid, o.InitFile, q.Name)
			}
			return fmt.Errorf("%w: %s is already registered as %q", domain.ErrInvalid, path, q.Name)
		}
		if config.StorageFor(path) == "sqlite" {
			return storage.ErrSQLite
		}
		if err := cfg.Add(config.ProjectRef{Name: name, Path: path}); err != nil {
			return err
		}
		ref = cfg.Projects[len(cfg.Projects)-1]
		created, err = openOrCreate(ctx, ref, settings)
		return err
	})
	if err != nil {
		if created { // the board was made, so it was the save that failed
			err = fmt.Errorf("%w (the new board file %s was left behind)", err, ref.Path)
		}
		return config.ProjectRef{}, err
	}
	return ref, nil
}

// openOrCreate makes sure a board exists at ref. An existing file is loaded
// through its repository to prove it is a Waypoint board, and is not written.
// A missing file is created from settings. created reports which happened.
func openOrCreate(ctx context.Context, ref config.ProjectRef, settings domain.Settings) (created bool, err error) {
	if info, statErr := os.Stat(ref.Path); statErr == nil {
		if info.IsDir() {
			return false, fmt.Errorf("%w: %s is a directory, not a board file", domain.ErrInvalid, ref.Path)
		}
		repo, err := storage.Open(ref)
		if err != nil {
			return false, err
		}
		_, _, err = repo.Load(ctx)
		return false, err
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return false, statErr
	}
	_, err = storage.Create(ref, settings)
	return err == nil, err
}

// boardPath is where a new project's board lives: path resolved against cwd, or
// <data_dir>/<slug of name>.md when path is empty.
func boardPath(cfg *config.Config, name, path, cwd string) (string, error) {
	if path == "" {
		s := slug(name)
		if s == "" {
			return "", fmt.Errorf("%w: cannot make a file name from %q; use --path", domain.ErrInvalid, name)
		}
		return filepath.Join(cfg.DataDir, s+".md"), nil
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	if cwd == "" {
		return "", fmt.Errorf("%w: cannot resolve %q: the working directory is unknown", domain.ErrInvalid, path)
	}
	return filepath.Join(cwd, path), nil
}

// slug lowercases s, turns each run of characters outside [a-z0-9] into one
// "-" and trims "-" from both ends.
func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, c := range strings.ToLower(s) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(c)
		} else {
			dash = true
		}
	}
	return b.String()
}

// RegisteredAt finds the project whose board file is path, comparing cleaned
// paths and then the files themselves, so a symlinked path matches.
func RegisteredAt(cfg *config.Config, path string) (config.ProjectRef, bool) {
	want, statErr := os.Stat(path)
	for _, q := range cfg.Projects {
		if filepath.Clean(q.Path) == filepath.Clean(path) {
			return q, true
		}
		if statErr != nil {
			continue
		}
		if got, err := os.Stat(q.Path); err == nil && os.SameFile(want, got) {
			return q, true
		}
	}
	return config.ProjectRef{}, false
}

// Inspect loads the board of a registry entry, or reports why it cannot be
// opened: a path that is not absolute, an unknown storage, a missing file, a
// directory, sqlite storage or a board that does not parse. It writes nothing.
// The registry is a hand-edited file, so every entry is inspected on its own
// and one bad entry says nothing about the others.
func Inspect(ctx context.Context, ref config.ProjectRef) (*domain.Board, error) {
	if !filepath.IsAbs(ref.Path) {
		return nil, fmt.Errorf("Path %q is not absolute", ref.Path)
	}
	switch ref.Storage {
	case "markdown":
	case "sqlite":
		return nil, storage.ErrSQLite
	default:
		return nil, fmt.Errorf("Unknown storage %q", ref.Storage)
	}
	info, err := os.Stat(ref.Path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("File not found at %s", ref.Path)
	case err != nil:
		return nil, fmt.Errorf("Cannot read %s", ref.Path)
	case info.IsDir():
		return nil, fmt.Errorf("%s is a directory, not a board file", ref.Path)
	}
	repo, err := storage.Open(ref)
	if err != nil {
		return nil, err
	}
	b, _, err := repo.Load(ctx)
	if err != nil {
		return nil, err
	}
	return b, nil
}
