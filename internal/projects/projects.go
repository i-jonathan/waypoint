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

// DefaultPrefix is the card id prefix a caller uses for a new project when the
// user gives none.
const DefaultPrefix = "T"

// AddOptions says which project to add.
type AddOptions struct {
	Name string
	Path string // "" means <data_dir>/<slug of Name>.md
	// Prefix is the card id prefix and must be valid: Add applies no default,
	// so an empty one is refused. Callers pass DefaultPrefix when they mean it.
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
	settings := domain.NewBoard(name, o.Prefix).Settings
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

// EntryError says why a registry entry cannot be opened. Kind is
// domain.ErrNotFound for a missing file and domain.ErrInvalid for an entry that
// is wrong (relative path, unknown storage, a directory), so callers can
// answer with the right status.
type EntryError struct {
	Msg  string
	Kind error
}

func (e *EntryError) Error() string { return e.Msg }
func (e *EntryError) Unwrap() error { return e.Kind }

// Validate checks a registry entry without reading the board: a path that is
// not absolute, an unknown storage, sqlite storage, a missing file or a
// directory. It returns nil when the entry looks openable.
func Validate(ref config.ProjectRef) error {
	if !filepath.IsAbs(ref.Path) {
		return &EntryError{fmt.Sprintf("Path %q is not absolute", ref.Path), domain.ErrInvalid}
	}
	switch ref.Storage {
	case "markdown":
	case "sqlite":
		return storage.ErrSQLite
	default:
		return &EntryError{fmt.Sprintf("Unknown storage %q", ref.Storage), domain.ErrInvalid}
	}
	info, err := os.Stat(ref.Path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return &EntryError{fmt.Sprintf("File not found at %s", ref.Path), domain.ErrNotFound}
	case err != nil:
		return fmt.Errorf("Cannot read %s", ref.Path)
	case info.IsDir():
		return &EntryError{fmt.Sprintf("%s is a directory, not a board file", ref.Path), domain.ErrInvalid}
	}
	return nil
}

// Inspect loads the board of a registry entry, or reports why it cannot be
// opened (see Validate, or a board that does not parse). It writes nothing.
// The registry is a hand-edited file, so every entry is inspected on its own
// and one bad entry says nothing about the others.
func Inspect(ctx context.Context, ref config.ProjectRef) (*domain.Board, error) {
	if err := Validate(ref); err != nil {
		return nil, err
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
