package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"waypoint/internal/dbconn"
	"waypoint/internal/domain"
)

// Add registers a project. A file project's Path is made absolute and cleaned;
// a blank name or path, a storage other than the four known, a name already
// taken (compared case-insensitively) or a path already registered is
// domain.ErrInvalid, and so is a file project with server fields. An empty
// Storage on a file project is chosen by StorageFor(path). A server project
// needs host, user and database, a port of 0-65535 (0 means the engine's
// default), a valid ssl value and no path; its name must give a password
// variable no other server project's name gives.
func (c *Config) Add(p ProjectRef) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("%w: project name is empty", domain.ErrInvalid)
	}
	if p.IsServer() {
		if err := validateServer(p); err != nil {
			return err
		}
	} else {
		if p.Host != "" || p.Port != 0 || p.Database != "" || p.User != "" || p.SSL != "" {
			return fmt.Errorf("%w: server settings need storage postgres or mysql", domain.ErrInvalid)
		}
		if p.Path == "" {
			return fmt.Errorf("%w: project path is empty", domain.ErrInvalid)
		}
		abs, err := filepath.Abs(p.Path)
		if err != nil {
			return fmt.Errorf("%w: project path %q: %v", domain.ErrInvalid, p.Path, err)
		}
		p.Path = filepath.Clean(abs)
		if p.Storage == "" {
			p.Storage = StorageFor(p.Path)
		}
		if p.Storage != "markdown" && p.Storage != "sqlite" {
			return fmt.Errorf("%w: storage %q must be markdown, sqlite, postgres or mysql", domain.ErrInvalid, p.Storage)
		}
	}
	for _, q := range c.Projects {
		if strings.EqualFold(q.Name, p.Name) {
			return fmt.Errorf("%w: project %q already exists", domain.ErrInvalid, q.Name)
		}
		if !p.IsServer() && !q.IsServer() && q.Path == p.Path {
			return fmt.Errorf("%w: path %s is already registered as %q", domain.ErrInvalid, p.Path, q.Name)
		}
		if p.IsServer() && q.IsServer() && p.Storage == q.Storage && sameDatabase(p, q) {
			return fmt.Errorf("%w: %s is already registered as %q", domain.ErrInvalid, p.Display(), q.Name)
		}
		if (p.IsServer() || q.IsServer()) && dbconn.PasswordVar(q.Name) == dbconn.PasswordVar(p.Name) {
			return fmt.Errorf("%w: project names %q and %q would both use %s for the password; rename one",
				domain.ErrInvalid, q.Name, p.Name, dbconn.PasswordVar(p.Name))
		}
	}
	c.Projects = append(c.Projects, p)
	return nil
}

// sameDatabase reports whether two server entries of one engine name the same
// database: the same host and port (0 means the engine's default) and name.
func sameDatabase(a, b ProjectRef) bool {
	port := func(p ProjectRef) int {
		if p.Port == 0 {
			return dbconn.DefaultPort(p.Storage)
		}
		return p.Port
	}
	return strings.EqualFold(a.Host, b.Host) && port(a) == port(b) && a.Database == b.Database
}

func validateServer(p ProjectRef) error {
	switch {
	case p.Path != "":
		return fmt.Errorf("%w: a %s project has no path", domain.ErrInvalid, p.Storage)
	case strings.TrimSpace(p.Host) == "":
		return fmt.Errorf("%w: host is empty", domain.ErrInvalid)
	case strings.TrimSpace(p.User) == "":
		return fmt.Errorf("%w: user is empty", domain.ErrInvalid)
	case strings.TrimSpace(p.Database) == "":
		return fmt.Errorf("%w: database is empty", domain.ErrInvalid)
	case p.Port < 0 || p.Port > 65535:
		return fmt.Errorf("%w: port %d must be 1-65535", domain.ErrInvalid, p.Port)
	case p.SSL != "" && !dbconn.ValidSSL(p.SSL):
		return fmt.Errorf("%w: ssl %q must be disable, prefer, require or verify-full", domain.ErrInvalid, p.SSL)
	}
	if !strings.ContainsFunc(p.Name, func(r rune) bool {
		return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
	}) {
		return fmt.Errorf("%w: project name %q has no letters or digits, so it cannot name a password variable; choose another name", domain.ErrInvalid, p.Name)
	}
	return nil
}

// Remove unregisters the project with that name (case-insensitive). Unknown
// names wrap domain.ErrNotFound.
func (c *Config) Remove(name string) error {
	for i, q := range c.Projects {
		if strings.EqualFold(q.Name, name) {
			c.Projects = append(c.Projects[:i], c.Projects[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: no project named %q", domain.ErrNotFound, name)
}

// Find returns the project with that name (case-insensitive).
func (c *Config) Find(name string) (ProjectRef, bool) {
	for _, q := range c.Projects {
		if strings.EqualFold(q.Name, name) {
			return q, true
		}
	}
	return ProjectRef{}, false
}

// FindByDir picks the project for a working directory: the deepest project
// whose board file sits in cwd or one of its ancestors. A project below cwd
// never matches, so running from a parent folder such as $HOME reaches nothing.
// A tie is ambiguous and gives false. Directories are compared by path segment,
// after resolving symlinks where possible.
func (c *Config) FindByDir(cwd string) (ProjectRef, bool) {
	cwd = resolve(cwd)

	var best []ProjectRef
	bestDepth := -1
	for _, q := range c.Projects {
		if q.IsServer() { // no folder: found by name, never by where you are
			continue
		}
		dir := resolve(filepath.Dir(q.Path))
		if !isAncestor(dir, cwd) {
			continue
		}
		depth := len(strings.Split(dir, string(filepath.Separator)))
		switch {
		case depth > bestDepth:
			best, bestDepth = []ProjectRef{q}, depth
		case depth == bestDepth:
			best = append(best, q)
		}
	}
	if len(best) == 1 {
		return best[0], true
	}
	return ProjectRef{}, false
}

// isAncestor reports whether p is dir itself or lies beneath it, by path
// segment, so /p/pharmacy2 is not beneath /p/pharmacy.
func isAncestor(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// resolve makes p absolute and cleaned and resolves symlinks in the longest
// part that exists, so a path through /tmp and one through /private/tmp
// compare equal even when the tail does not exist yet. If nothing resolves it
// returns the cleaned path.
func resolve(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	p = filepath.Clean(p)
	tail := ""
	for cur := p; ; {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, tail)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		tail = filepath.Join(filepath.Base(cur), tail)
		cur = parent
	}
}

// StorageFor guesses the storage kind from a file extension: ".db",
// ".sqlite" and ".sqlite3" are "sqlite", anything else is "markdown".
func StorageFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".db", ".sqlite", ".sqlite3":
		return "sqlite"
	}
	return "markdown"
}
