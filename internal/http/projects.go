package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"waypoint/internal/config"
	"waypoint/internal/domain"
	"waypoint/internal/projects"
)

// projectJSON is a registered project as the project list shows it. Error is
// set when the entry cannot be opened, and says why; the other fields are the
// registry's own.
type projectJSON struct {
	Name    string `json:"name"`
	Storage string `json:"storage"`
	Path    string `json:"path"`
	Accent  string `json:"accent"`
	Error   string `json:"error,omitempty"`
}

// projectOut describes ref. It reads the board but never writes it.
func projectOut(ctx context.Context, cfg *config.Config, ref config.ProjectRef) projectJSON {
	out := projectJSON{Name: ref.Name, Storage: ref.Storage, Path: ref.Path}
	var own domain.Appearance
	if b, err := projects.Inspect(ctx, ref); err != nil {
		out.Error = err.Error()
	} else {
		own = b.Settings.Appearance
	}
	out.Accent = config.Resolve(cfg.Appearance, own).Accent
	return out
}

func (a *api) listProjects(w http.ResponseWriter, r *http.Request) error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	out := make([]projectJSON, 0, len(cfg.Projects))
	for _, ref := range cfg.Projects {
		out = append(out, projectOut(r.Context(), cfg, ref))
	}
	a.writeJSON(w, r, http.StatusOK, out)
	return nil
}

// isBoardFile reports whether path has an extension Waypoint boards use, so a web
// client cannot make Waypoint create a board at an arbitrary kind of file.
func isBoardFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".db", ".sqlite", ".sqlite3":
		return true
	}
	return false
}

func (a *api) createProject(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Name   string `json:"name"`
		Path   string `json:"path"`
		Prefix string `json:"prefix"`
		// Storage is "sqlite" (the default) or "markdown". It is checked against an
		// existing file, which must match.
		Storage string `json:"storage"`
		// Existing means open a board that is already there, so a mistyped path is an error
		// rather than a new file.
		Existing bool `json:"existing"`
	}
	if err := decodeBody(w, r, &in, false); err != nil {
		return err
	}
	if in.Path != "" && !filepath.IsAbs(in.Path) {
		return invalidRequest("path must be an absolute path")
	}
	if in.Path != "" && !isBoardFile(in.Path) {
		return invalidRequest("path must end in .md, .markdown, .db, .sqlite or .sqlite3")
	}
	if in.Existing {
		if in.Path == "" {
			return invalidRequest("path is required to open a file")
		}
		if _, err := os.Stat(in.Path); errors.Is(err, os.ErrNotExist) {
			return invalidRequest("no file at " + in.Path)
		}
	}
	if in.Prefix == "" {
		in.Prefix = projects.DefaultPrefix
	}
	ref, err := projects.Add(r.Context(), a.d.ConfigPath, a.d.Getenv, projects.AddOptions{
		Name: in.Name, Path: in.Path, Prefix: in.Prefix, Storage: in.Storage,
	})
	if err != nil {
		return err
	}
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	a.publish(Event{Type: "projects"})
	a.writeJSON(w, r, http.StatusCreated, projectOut(r.Context(), cfg, ref))
	return nil
}

// deleteProject unregisters a project. Its files are left alone.
func (a *api) deleteProject(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("p")
	err := config.Update(a.d.ConfigPath, a.d.Getenv, func(cfg *config.Config) error {
		if _, ok := cfg.Find(name); !ok {
			return fmt.Errorf("%w: no project named %q", domain.ErrNotFound, name)
		}
		return cfg.Remove(name)
	})
	if err != nil {
		return err
	}
	a.publish(Event{Type: "projects"})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// globalJSON is the global config as the settings screen shows it.
type globalJSON struct {
	Port            int               `json:"port"`
	OpenBrowser     bool              `json:"open_browser"`
	TLS             string            `json:"tls"`
	Appearance      domain.Appearance `json:"appearance"`
	RestartRequired bool              `json:"restart_required"`
}

func (a *api) getSettings(w http.ResponseWriter, r *http.Request) error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	a.writeJSON(w, r, http.StatusOK, globalJSON{Port: cfg.Port, OpenBrowser: cfg.OpenBrowser, TLS: cfg.TLS, Appearance: cfg.Appearance})
	return nil
}

// appearanceIn is an appearance change. A field left out is kept; an empty one
// goes back to inheriting.
type appearanceIn struct {
	Theme    *string `json:"theme"`
	Accent   *string `json:"accent"`
	Density  *string `json:"density"`
	Swimlane *string `json:"swimlane"`
	Textures *string `json:"textures"`
}

// fields pairs each field of the change with its name and its place in a.
func (in *appearanceIn) fields(a *domain.Appearance) []struct {
	name string
	from *string
	to   *string
} {
	return []struct {
		name string
		from *string
		to   *string
	}{
		{"theme", in.Theme, &a.Theme},
		{"accent", in.Accent, &a.Accent},
		{"density", in.Density, &a.Density},
		{"swimlane", in.Swimlane, &a.Swimlane},
		{"textures", in.Textures, &a.Textures},
	}
}

// validate checks only the fields sent, so a typo in the file never blocks a
// change to another field.
func (in *appearanceIn) validate() error {
	for _, f := range in.fields(&domain.Appearance{}) {
		if f.from == nil {
			continue
		}
		if err := domain.CheckAppearanceField(f.name, *f.from); err != nil {
			return err
		}
	}
	return nil
}

func (in *appearanceIn) mergeInto(a *domain.Appearance) {
	for _, f := range in.fields(a) {
		if f.from != nil {
			*f.to = *f.from
		}
	}
}

// putSettings changes the global config. A field the body leaves out is kept,
// and that goes for each field of appearance too.
// The registry and data_dir are not part of it. A new port or tls mode takes
// effect when the server restarts, and the response says so.
func (a *api) putSettings(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Port        *int          `json:"port"`
		OpenBrowser *bool         `json:"open_browser"`
		TLS         *string       `json:"tls"`
		Appearance  *appearanceIn `json:"appearance"`
	}
	if err := decodeBody(w, r, &in, false); err != nil {
		return err
	}
	if in.Port != nil && (*in.Port < 1 || *in.Port > 65535) {
		return invalidRequest(fmt.Sprintf("port %d must be between 1 and 65535", *in.Port))
	}
	if in.Port != nil && (*in.Port == 80 || *in.Port == 443) {
		return invalidRequest(fmt.Sprintf("port %d is not supported: browsers leave it out of the Host header, which Waypoint checks", *in.Port))
	}
	if in.TLS != nil && !slices.Contains([]string{"auto", "on", "off"}, *in.TLS) {
		return invalidRequest(fmt.Sprintf("tls %q must be one of auto, on, off", *in.TLS))
	}
	if in.Appearance != nil {
		if err := in.Appearance.validate(); err != nil {
			return err
		}
	}

	var out globalJSON
	err := config.Update(a.d.ConfigPath, a.d.Getenv, func(cfg *config.Config) error {
		port, tls := cfg.Port, cfg.TLS
		if in.Port != nil {
			cfg.Port = *in.Port
		}
		if in.OpenBrowser != nil {
			cfg.OpenBrowser = *in.OpenBrowser
		}
		if in.TLS != nil {
			cfg.TLS = *in.TLS
		}
		if in.Appearance != nil {
			in.Appearance.mergeInto(&cfg.Appearance)
		}
		out = globalJSON{
			Port: cfg.Port, OpenBrowser: cfg.OpenBrowser, TLS: cfg.TLS, Appearance: cfg.Appearance,
			RestartRequired: cfg.Port != port || cfg.TLS != tls,
		}
		return nil
	})
	if err != nil {
		return err
	}
	a.publish(Event{Type: "projects"}) // every board's resolved appearance may have moved
	a.writeJSON(w, r, http.StatusOK, out)
	return nil
}
