package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"waypoint/internal/domain"
)

// Add registers a project. Its Path is made absolute and cleaned. A blank
// name, a name already taken (compared case-insensitively) or a path already
// registered is domain.ErrInvalid.
func (c *Config) Add(p ProjectRef) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("%w: project name is empty", domain.ErrInvalid)
	}
	abs, err := filepath.Abs(p.Path)
	if err != nil {
		return fmt.Errorf("%w: project path %q: %v", domain.ErrInvalid, p.Path, err)
	}
	p.Path = filepath.Clean(abs)
	for _, q := range c.Projects {
		if strings.EqualFold(q.Name, p.Name) {
			return fmt.Errorf("%w: project %q already exists", domain.ErrInvalid, q.Name)
		}
		if q.Path == p.Path {
			return fmt.Errorf("%w: path %s is already registered as %q", domain.ErrInvalid, p.Path, q.Name)
		}
	}
	c.Projects = append(c.Projects, p)
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

// FindByDir picks the project for a working directory. It returns the deepest
// project whose board file sits in cwd or one of its ancestors; failing that,
// the single project whose board file is under cwd. Ties and several
// candidates are ambiguous and give false. Directories are compared by path
// segment, after resolving symlinks where possible.
func (c *Config) FindByDir(cwd string) (ProjectRef, bool) {
	cwd = resolve(cwd)

	var best []ProjectRef
	bestDepth := -1
	for _, q := range c.Projects {
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
	if len(best) > 0 {
		if len(best) == 1 {
			return best[0], true
		}
		return ProjectRef{}, false
	}

	var below []ProjectRef
	for _, q := range c.Projects {
		if isAncestor(cwd, resolve(filepath.Dir(q.Path))) {
			below = append(below, q)
		}
	}
	if len(below) == 1 {
		return below[0], true
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
