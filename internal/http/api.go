package httpapi

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"waypoint/internal/app"
	"waypoint/internal/config"
	"waypoint/internal/domain"
	"waypoint/internal/errcode"
	"waypoint/internal/projects"
	"waypoint/internal/storage"
)

// Deps is what the handler needs from the outside world.
type Deps struct {
	ConfigPath string              // the config file; read again on every request
	Getenv     func(string) string // for HOME when reading the config; nil means os.Getenv
	Now        app.Clock           // nil means time.Now
	Broker     *Broker             // nil means a broker of its own
	Port       int                 // the port the server listens on, for the Host check
	UI         fs.FS               // the built web UI, served for every path outside /api/; nil serves none
	Logger     *slog.Logger        // nil means slog.Default
}

// autoArchiveWait limits how long a read waits to archive what is due. A
// variable only so a test can shorten it.
var autoArchiveWait = 2 * time.Second

// api holds what the handlers share.
type api struct {
	d   Deps
	log *slog.Logger

	mu     sync.Mutex
	opened map[string]bool // board files whose auto-archive has run this server run
}

// Handler returns the Waypoint API. The registry is read from the config on
// every request, so a change made by the CLI shows up at once.
func Handler(d Deps) http.Handler {
	if d.Getenv == nil {
		d.Getenv = os.Getenv
	}
	if d.Broker == nil {
		d.Broker = NewBroker()
	}
	a := &api{d: d, log: d.Logger, opened: map[string]bool{}}
	if a.log == nil {
		a.log = slog.Default()
	}

	mux := http.NewServeMux()
	handle := func(pattern string, h func(http.ResponseWriter, *http.Request) error) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if err := h(w, r); err != nil {
				a.writeError(w, r, err)
			}
		})
	}
	handle("GET /api/projects", a.listProjects)
	handle("POST /api/projects", a.createProject)
	handle("POST /api/db/test", a.testDatabase)
	handle("DELETE /api/projects/{p}", a.deleteProject)
	handle("POST /api/projects/{p}/convert", a.convertProject)
	handle("GET /api/projects/{p}/export", a.exportProject)
	handle("POST /api/projects/{p}/import", a.importProject)
	handle("GET /api/settings", a.getSettings)
	handle("PUT /api/settings", a.putSettings)

	handle("GET /api/projects/{p}/board", a.getBoard)
	handle("GET /api/projects/{p}/archive", a.getArchive)
	handle("POST /api/projects/{p}/cards", a.createCard)
	handle("PATCH /api/projects/{p}/cards/{id}", a.patchCard)
	handle("POST /api/projects/{p}/cards/{id}/move", a.moveCard)
	handle("POST /api/projects/{p}/cards/{id}/check", a.checkItem)
	handle("POST /api/projects/{p}/cards/{id}/restore", a.restoreCard)
	handle("DELETE /api/projects/{p}/cards/{id}", a.deleteCard)
	handle("POST /api/projects/{p}/archive", a.archiveDone)
	handle("PUT /api/projects/{p}/settings", a.putProjectSettings)

	mux.HandleFunc("GET /api/events", d.Broker.ServeSSE)
	handle("/api/", func(http.ResponseWriter, *http.Request) error {
		return fmt.Errorf("%w: no such endpoint", domain.ErrNotFound)
	})
	if d.UI != nil {
		mux.Handle("/", UI(d.UI))
	}
	return a.guard(mux)
}

// publish tells every open tab that something changed. Call it only after the
// change succeeded.
func (a *api) publish(e Event) { a.d.Broker.Publish(e) }

func (a *api) loadConfig() (*config.Config, error) {
	return config.Load(a.d.ConfigPath, a.d.Getenv)
}

// project is a registered project opened for one request.
type project struct {
	ref config.ProjectRef
	cfg *config.Config
	svc *app.Service
}

// openProject finds the project named in the URL. The name is only ever looked
// up in the registry; it is never used as a path. An entry that cannot be
// opened (a relative path, an unknown or read-only storage, a missing file, a
// directory) is an error for this project and no other.
func (a *api) openProject(r *http.Request) (project, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return project{}, err
	}
	name := r.PathValue("p")
	ref, ok := cfg.Find(name)
	if !ok {
		return project{}, fmt.Errorf("%w: no project named %q", domain.ErrNotFound, name)
	}
	if err := projects.Validate(ref); err != nil {
		return project{}, err
	}
	repo, err := storage.Open(ref)
	if err != nil {
		return project{}, err
	}
	return project{ref: ref, cfg: cfg, svc: app.NewService(repo, a.d.Now)}, nil
}

// changed publishes that the project's board changed.
func (a *api) changed(p project) { a.publish(Event{Type: "changed", Project: p.ref.Name}) }

// boardKey identifies a board for the run: its path, or for a server project
// the engine, address, database and user, which together say which board it is.
func boardKey(ref config.ProjectRef) string {
	if ref.IsServer() {
		return ref.Storage + "|" + ref.Display() + "|" + ref.User
	}
	return ref.Path
}

// autoArchiveOnce archives the cards that are due, the first time a board is
// opened in this server run. A failure is logged and does not fail the read
// that triggered it; it is tried again on the next open. That includes waiting
// too long for the board's lock: a read must not hang on a slow writer.
func (a *api) autoArchiveOnce(r *http.Request, p project) {
	a.mu.Lock()
	done := a.opened[boardKey(p.ref)]
	a.opened[boardKey(p.ref)] = true
	a.mu.Unlock()
	if done {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), autoArchiveWait)
	defer cancel()
	ids, err := p.svc.AutoArchive(ctx)
	if err != nil {
		a.mu.Lock()
		delete(a.opened, p.ref.Path)
		a.mu.Unlock()
		if errcode.Code(err) == "error" { // a board with a parse error is reported by the read itself
			a.log.Warn("auto-archive failed", "project", p.ref.Name, "error", err)
		}
		return
	}
	if len(ids) > 0 {
		a.changed(p)
	}
}
