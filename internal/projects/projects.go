// Package projects holds the registry operations the CLI and the web API
// share: adding a project and checking a registry entry.
package projects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"waypoint/internal/config"
	"waypoint/internal/dbconn"
	"waypoint/internal/domain"
	"waypoint/internal/storage"
	"waypoint/internal/storage/markdown"
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
	// Storage is "sqlite" or "markdown". Empty means: what an existing file is,
	// else what the path's extension says, else sqlite.
	Storage string
	// Cwd resolves a relative Path. With Cwd empty a relative Path is refused.
	Cwd string
	// FromInit words the "already registered" error for "waypoint init".
	FromInit bool
	// InitFile names the board file in that error; used only with FromInit.
	InitFile string
	// Server, when set, adds a project stored on a database server instead of
	// in a file; Path and Storage are then ignored.
	Server *ServerOptions
	// Warn receives a notice for the person setting up a server project (it
	// is not on this computer; TLS is off). It is called before connecting.
	Warn func(string)
}

// ServerOptions says where a server project lives. The password is never
// part of it: it comes from the environment (see dbconn.Password).
type ServerOptions struct {
	Engine         string // "postgres"
	Host           string // default localhost
	Port           int    // default: the engine's
	User           string // required
	Database       string // default: the project name as a slug
	SSL            string // default: dbconn.DefaultSSL(Host)
	CreateDatabase bool   // make the database if it is missing
}

// Add registers a project and opens or creates its board.
//
// The order is: validate, then under the config lock check the registry, create
// or open the board, and only then write the config. A failure before the last
// step leaves the config as it was. A path already registered, even through a
// symlink, is refused.
// If the board was created but the config could not be saved, the error says
// the new file was left behind.
func Add(ctx context.Context, cfgPath string, env func(string) string, o AddOptions) (config.ProjectRef, error) {
	name := strings.TrimSpace(o.Name)
	if name == "" {
		return config.ProjectRef{}, fmt.Errorf("%w: project name is empty", domain.ErrInvalid)
	}
	if name == "." || name == ".." { // a URL path cleans these away, so the project could never be opened
		return config.ProjectRef{}, fmt.Errorf("%w: project name %q is not allowed", domain.ErrInvalid, name)
	}
	settings := domain.NewBoard(name, o.Prefix).Settings
	if err := settings.Validate(); err != nil {
		return config.ProjectRef{}, err
	}

	if o.Server != nil {
		return addServer(ctx, cfgPath, env, name, settings, o)
	}

	var ref config.ProjectRef
	var created bool
	err := config.Update(cfgPath, env, func(cfg *config.Config) error {
		path, err := boardPath(cfg, name, o.Path, o.Cwd, o.Storage)
		if err != nil {
			return err
		}
		if q, ok := RegisteredAt(cfg, path); ok {
			if o.FromInit {
				return fmt.Errorf("%w: %s here is already registered as project %q", domain.ErrInvalid, o.InitFile, q.Name)
			}
			return fmt.Errorf("%w: %s is already registered as %q", domain.ErrInvalid, path, q.Name)
		}
		kind, err := chooseStorage(path, o.Storage)
		if err != nil {
			return err
		}
		if kind == "markdown" {
			if err := checkArchive(cfg, path); err != nil {
				return err
			}
		}
		if err := cfg.Add(config.ProjectRef{Name: name, Path: path, Storage: kind}); err != nil {
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

// serverEngines are the engines a project can be stored on, in the order they are listed.
var serverEngines = []string{"postgres"}

// ServerEngines lists the engines a project can be stored on.
func ServerEngines() []string { return slices.Clone(serverEngines) }

// defaultDatabase is the database name used when none is given.
func defaultDatabase(name string) string { return slug(name) }

// ServerRef is the registry entry for a project on a server, with the defaults
// of ServerOptions filled in and checked as Add would check them.
//
// It is checked beside the projects already in cfg, so a name, a password
// variable or a database that Add would refuse is refused here too. With
// converting, the project of that name is left out of the check: it is the one
// being moved.
func ServerRef(cfg *config.Config, name string, so ServerOptions, converting bool) (config.ProjectRef, error) {
	ref, err := serverRef(name, so)
	if err != nil {
		return ref, err
	}
	others := &config.Config{}
	for _, q := range cfg.Projects {
		if converting && strings.EqualFold(q.Name, name) {
			continue
		}
		others.Projects = append(others.Projects, q)
	}
	if err := others.Add(ref); err != nil {
		return ref, err
	}
	return ref, nil
}

// serverRef fills in the defaults of ServerOptions and checks them.
func serverRef(name string, so ServerOptions) (config.ProjectRef, error) {
	if !slices.Contains(serverEngines, so.Engine) {
		return config.ProjectRef{}, fmt.Errorf("%w: storage %q must be %s", domain.ErrInvalid, so.Engine, strings.Join(append([]string{"sqlite", "markdown"}, serverEngines...), ", "))
	}
	if so.Host == "" {
		so.Host = "localhost"
	}
	if so.Port == 0 {
		so.Port = dbconn.DefaultPort(so.Engine)
	}
	if strings.TrimSpace(so.User) == "" {
		return config.ProjectRef{}, fmt.Errorf("%w: a database user is required (--user)", domain.ErrInvalid)
	}
	if so.Database == "" {
		if so.Database = defaultDatabase(name); so.Database == "" {
			return config.ProjectRef{}, fmt.Errorf("%w: cannot make a database name from %q; use --database", domain.ErrInvalid, name)
		}
	}
	if so.SSL == "" {
		so.SSL = dbconn.DefaultSSL(so.Host)
	}
	return config.ProjectRef{Name: name, Storage: so.Engine, Host: so.Host, Port: so.Port, Database: so.Database, User: so.User, SSL: so.SSL}, nil
}

// addServer registers a project on a database server. It connects before it
// registers anything: an unreachable server, a wrong password, a database that
// holds something else or a retired board refuse the add and change nothing.
// An empty database gets a new board; a database that holds a Waypoint board
// is adopted as it is.
func addServer(ctx context.Context, cfgPath string, env func(string) string, name string, settings domain.Settings, o AddOptions) (config.ProjectRef, error) {
	want, err := serverRef(name, *o.Server)
	if err != nil {
		return config.ProjectRef{}, err
	}
	if o.Warn != nil && !dbconn.IsLocal(want.Host) {
		o.Warn(dbconn.NonLocalWarning)
		if want.SSL == "disable" {
			o.Warn(dbconn.UnencryptedNote)
		}
	}
	// The config lock is held while the server answers, so a server that stalls
	// must not hold every other config change for good.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var ref config.ProjectRef
	var created bool
	err = config.Update(cfgPath, env, func(cfg *config.Config) error {
		if err := cfg.Add(want); err != nil {
			return err
		}
		ref = cfg.Projects[len(cfg.Projects)-1]
		probe, err := storage.Probe(ctx, ref)
		if err != nil {
			return err
		}
		switch {
		case probe.DatabaseExists && probe.HasBoard:
			repo, err := storage.Open(ref)
			if err != nil {
				return err
			}
			_, _, err = repo.Load(ctx) // proves it is a board this build can use, and not retired
			return err
		case !probe.DatabaseExists && !o.Server.CreateDatabase:
			return fmt.Errorf("%w: database %q does not exist on %s; create it, or add --create-database", domain.ErrNotFound, ref.Database, ref.Display())
		}
		_, err = storage.CreateWith(ctx, ref, settings, storage.CreateOptions{CreateDatabase: !probe.DatabaseExists})
		created = err == nil
		return err
	})
	if err != nil {
		if created { // the board was made, so it was the save that failed
			if derr := storage.Discard(ctx, ref); derr != nil {
				err = fmt.Errorf("%w (the new board in %s could not be removed: %v)", err, ref.Display(), derr)
			}
		}
		return config.ProjectRef{}, err
	}
	return ref, nil
}

// chooseStorage decides which storage a project at path uses. An existing file
// is what its content says; a path with nothing there yet follows want, then its
// extension, then the default, sqlite. A want that contradicts the file or the
// extension is domain.ErrInvalid, so a board never ends up in a file named for
// the other kind.
func chooseStorage(path, want string) (string, error) {
	switch want {
	case "", "markdown", "sqlite":
	default:
		return "", fmt.Errorf("%w: storage %q must be sqlite or markdown", domain.ErrInvalid, want)
	}
	if kind, err := storage.Sniff(path); err == nil {
		if want != "" && want != kind {
			return "", fmt.Errorf("%w: %s is a %s file, not %s", domain.ErrInvalid, path, kind, want)
		}
		return kind, nil
	}
	if _, err := os.Stat(path); err == nil { // there, but empty or unreadable: opening it says why
		return config.StorageFor(path), nil
	}
	implied := ""
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		implied = "markdown"
	case ".db", ".sqlite", ".sqlite3":
		implied = "sqlite"
	}
	switch {
	case want != "" && implied != "" && want != implied:
		return "", fmt.Errorf("%w: %s ends in %s, which is a %s file, not %s", domain.ErrInvalid, path, filepath.Ext(path), implied, want)
	case want != "":
		return want, nil
	case implied != "":
		return implied, nil
	}
	return "sqlite", nil
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
// <data_dir>/<slug of name>.db (.md for markdown storage) when path is empty.
func boardPath(cfg *config.Config, name, path, cwd, storageWanted string) (string, error) {
	if path == "" {
		s := slug(name)
		if s == "" {
			return "", fmt.Errorf("%w: cannot make a file name from %q; use --path", domain.ErrInvalid, name)
		}
		ext := ".db"
		if storageWanted == "markdown" {
			ext = ".md"
		}
		return filepath.Join(cfg.DataDir, s+ext), nil
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

// checkArchive refuses a board that would collide with an archive: one project's archive file
// holding another's cards loses them across projects. Paths compare as the repository resolves
// them, so a symlinked folder is seen through, and without case, since most Mac disks ignore it;
// a false refusal is only a rename away.
func checkArchive(cfg *config.Config, path string) error {
	if markdown.IsArchiveFile(path) {
		return fmt.Errorf("%w: %s is an archive, not a board", domain.ErrInvalid, path)
	}
	_, mine := markdown.New(path).Files()
	for _, q := range cfg.Projects {
		if q.Storage != "markdown" {
			continue
		}
		board, archive := markdown.New(q.Path).Files()
		if mine != "" && (strings.EqualFold(mine, archive) || strings.EqualFold(mine, board)) {
			return fmt.Errorf("%w: %s shares an archive with %q; rename the file", domain.ErrInvalid, path, q.Name)
		}
	}
	return nil
}

// RegisteredAt finds the project whose board file is path, comparing cleaned
// paths and then the files themselves, so a symlinked path matches.
func RegisteredAt(cfg *config.Config, path string) (config.ProjectRef, bool) {
	want, statErr := os.Stat(path)
	for _, q := range cfg.Projects {
		if q.IsServer() {
			continue
		}
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
// not absolute, an unknown storage, a missing file or a
// directory. It returns nil when the entry looks openable.
func Validate(ref config.ProjectRef) error {
	if ref.IsServer() {
		if ref.Host == "" || ref.User == "" || ref.Database == "" || ref.Path != "" {
			return &EntryError{"The server settings are incomplete: a host, user and database are needed", domain.ErrInvalid}
		}
		return nil
	}
	if !filepath.IsAbs(ref.Path) {
		return &EntryError{fmt.Sprintf("Path %q is not absolute", ref.Path), domain.ErrInvalid}
	}
	switch ref.Storage {
	case "markdown", "sqlite":
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
