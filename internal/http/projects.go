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
	"sync"

	"waypoint/internal/config"
	"waypoint/internal/dbconn"
	"waypoint/internal/domain"
	"waypoint/internal/projects"
	"waypoint/internal/storage"
)

// projectJSON is a registered project as the project list shows it. Error is
// set when the entry cannot be opened, and says why; the other fields are the
// registry's own.
type projectJSON struct {
	Name     string `json:"name"`
	Storage  string `json:"storage"`
	Path     string `json:"path,omitempty"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Database string `json:"database,omitempty"`
	User     string `json:"user,omitempty"`
	SSL      string `json:"ssl,omitempty"`
	Accent   string `json:"accent"`
	Error    string `json:"error,omitempty"`
}

// serverIn is the part of a request that names a database server. There is no
// password: it comes from the server's environment, and a request that sends
// one is refused as an unknown field.
type serverIn struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	User           string `json:"user"`
	Database       string `json:"database"`
	SSL            string `json:"ssl"`
	CreateDatabase bool   `json:"create_database"`
}

func (s serverIn) options(engine string) *projects.ServerOptions {
	return &projects.ServerOptions{Engine: engine, Host: s.Host, Port: s.Port, User: s.User, Database: s.Database, SSL: s.SSL, CreateDatabase: s.CreateDatabase}
}

func (s serverIn) used() bool { return s != serverIn{} }

// projectOut describes ref. It reads the board but never writes it.
func projectOut(ctx context.Context, cfg *config.Config, ref config.ProjectRef) projectJSON {
	out := projectJSON{Name: ref.Name, Storage: ref.Storage, Path: ref.Path,
		Host: ref.Host, Port: ref.Port, Database: ref.Database, User: ref.User, SSL: ref.SSL}
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
	// Each entry is inspected on its own, all at once, so a server that does not
	// answer holds up the list by one connect timeout and not by one per project.
	out := make([]projectJSON, len(cfg.Projects))
	var wg sync.WaitGroup
	for i, ref := range cfg.Projects {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = projectOut(r.Context(), cfg, ref)
		}()
	}
	wg.Wait()
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
		serverIn
	}
	if err := decodeBody(w, r, &in, false); err != nil {
		return err
	}
	if slices.Contains(projects.ServerEngines(), in.Storage) {
		if in.Path != "" || in.Existing {
			return invalidRequest("a " + in.Storage + " project has no file: leave out path and existing")
		}
		if in.Prefix == "" {
			in.Prefix = projects.DefaultPrefix
		}
		ref, err := projects.Add(r.Context(), a.d.ConfigPath, a.d.Getenv, projects.AddOptions{
			Name: in.Name, Prefix: in.Prefix, Server: in.serverIn.options(in.Storage),
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
	if in.serverIn.used() {
		return invalidRequest("server settings need storage " + strings.Join(projects.ServerEngines(), " or "))
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

// convertProject moves a project to the other storage. See projects.Convert.
func (a *api) convertProject(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		To   string `json:"to"`
		Path string `json:"path"`
		serverIn
	}
	if err := decodeBody(w, r, &in, false); err != nil {
		return err
	}
	if in.To == "" {
		return invalidRequest("to is required: sqlite, markdown or " + strings.Join(projects.ServerEngines(), ", "))
	}
	target := projects.Target{Storage: in.To, Path: in.Path}
	if slices.Contains(projects.ServerEngines(), in.To) {
		if in.Path != "" {
			return invalidRequest("a " + in.To + " project has no file: leave out path")
		}
		target.Server = in.serverIn.options(in.To)
	} else if in.serverIn.used() {
		return invalidRequest("server settings need to " + strings.Join(projects.ServerEngines(), " or "))
	}
	if in.Path != "" && !filepath.IsAbs(in.Path) {
		return invalidRequest("path must be an absolute path")
	}
	if in.Path != "" && !isBoardFile(in.Path) {
		return invalidRequest("path must end in .md, .markdown, .db, .sqlite or .sqlite3")
	}
	res, err := projects.Convert(r.Context(), a.d.ConfigPath, a.d.Getenv, r.PathValue("p"), target)
	warning := ""
	if err != nil {
		if res.Ref.Name == "" {
			return err
		}
		// The project did move; only the tidying of the old board failed. Say so
		// with the message, which tells what to do, and tell open tabs.
		warning = err.Error()
	}
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	a.publish(Event{Type: "projects"})
	backups := res.Backups
	if backups == nil {
		backups = []string{}
	}
	a.writeJSON(w, r, http.StatusOK, struct {
		projectJSON
		Backups []string `json:"backups"`
		Retired string   `json:"retired,omitempty"`
		Warning string   `json:"warning,omitempty"`
	}{projectOut(r.Context(), cfg, res.Ref), backups, res.Retired, warning})
	return nil
}

// testDatabase looks at a database server for the New project and Convert
// dialogs, and changes nothing. Whether the server answers is the result, not an
// error: an unreachable server is a 200 that says so. A password is not part of
// the request; the response names the variables that hold it.
func (a *api) testDatabase(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Engine  string `json:"engine"`
		Project string `json:"project"`
		serverIn
	}
	if err := decodeBody(w, r, &in, false); err != nil {
		return err
	}
	if !slices.Contains(projects.ServerEngines(), in.Engine) {
		return invalidRequest("engine must be one of " + strings.Join(projects.ServerEngines(), ", "))
	}
	if strings.TrimSpace(in.Project) == "" {
		return invalidRequest("project is required: the password variable is named after it")
	}
	ref, err := projects.ServerRef(strings.TrimSpace(in.Project), *in.serverIn.options(in.Engine))
	if err != nil {
		return err
	}
	out := struct {
		Reachable         bool   `json:"reachable"`
		NonLocal          bool   `json:"non_local"`
		DatabaseExists    bool   `json:"database_exists"`
		Empty             bool   `json:"empty"`
		HasBoard          bool   `json:"has_board"`
		PasswordVar       string `json:"password_var"`
		GlobalPasswordVar string `json:"global_password_var"`
		Error             string `json:"error,omitempty"`
	}{NonLocal: !dbconn.IsLocal(ref.Host), PasswordVar: dbconn.PasswordVar(ref.Name), GlobalPasswordVar: dbconn.GlobalPasswordVar}
	probe, err := storage.Probe(r.Context(), ref)
	out.Reachable, out.DatabaseExists, out.Empty, out.HasBoard = probe.Reachable, probe.DatabaseExists, probe.Empty, probe.HasBoard
	if err != nil {
		out.Error = err.Error()
	}
	a.writeJSON(w, r, http.StatusOK, out)
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
