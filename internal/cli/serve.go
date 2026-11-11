package cli

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"strconv"
	"sync"

	"waypoint/internal/config"
	httpapi "waypoint/internal/http"
	"waypoint/internal/server"
	"waypoint/internal/storage"
	"waypoint/internal/watch"
)

const usageServe = "waypoint serve [--port N] [--no-open]"

// cmdServe runs the web server until the context ends: the board watcher, the
// event broker, the JSON API and the UI, on 127.0.0.1.
func (r *runner) cmdServe(args []string) int {
	var portFlag string
	var noOpen bool
	pos, code, done := r.parse(usageServe, args, func(fs *flag.FlagSet) {
		fs.StringVar(&portFlag, "port", "", "")
		fs.BoolVar(&noOpen, "no-open", false, "")
	})
	if done {
		return code
	}
	if len(pos) != 0 {
		return r.usageError("serve takes no arguments", usageServe)
	}

	cfgPath, err := config.DefaultPath(r.env.Getenv)
	if err != nil {
		return r.fail(err)
	}
	cfg, err := config.Load(cfgPath, r.env.Getenv)
	if err != nil {
		return r.fail(err)
	}
	port := cfg.Port
	if portFlag != "" {
		if port, err = strconv.Atoi(portFlag); err != nil {
			return r.usageError(fmt.Sprintf("invalid --port %q", portFlag), usageServe)
		}
	}
	if err := checkPort(port); err != nil {
		return r.fail(err)
	}

	// Bind first: the handler's Host check needs the port that was really got.
	ln, err := server.Listen(port)
	if err != nil {
		return r.fail(err)
	}
	log := slog.New(slog.NewTextHandler(r.env.Stderr, nil))
	broker := httpapi.NewBroker()
	handler := httpapi.Handler(httpapi.Deps{
		ConfigPath: cfgPath,
		Getenv:     r.env.Getenv,
		Now:        r.env.Now,
		Broker:     broker,
		Port:       ln.Addr().(*net.TCPAddr).Port,
		UI:         httpapi.DistFS(),
		Logger:     log,
	})

	ctx, cancel := context.WithCancel(r.ctx)
	var watcher sync.WaitGroup
	watcher.Add(1)
	go func() {
		defer watcher.Done()
		publish := func(project string) { broker.Publish(httpapi.Event{Type: "changed", Project: project}) }
		opts := watch.Options{
			ConfigPath: cfgPath,
			Projects:   func() { broker.Publish(httpapi.Event{Type: "projects"}) },
			Logger:     log,
		}
		if err := watch.Run(ctx, watchTargets(cfgPath, r.env.Getenv, log), publish, opts); err != nil {
			log.Warn("not watching board files; the page will not update by itself", "error", err)
		}
	}()

	open := cfg.OpenBrowser && !noOpen && r.env.OpenURL != nil
	err = server.Run(ctx, server.Options{
		Listener: ln,
		TLS:      cfg.TLS,
		Certs:    certsFor(cfg, r.env.Getenv),
		Handler:  handler,
		Now:      r.env.Now,
		Broker:   broker,
		Tip:      func(msg string) { fmt.Fprintln(r.env.Stderr, msg) },
		Ready: func(url string) {
			fmt.Fprintf(r.env.Stdout, "Waypoint running at %s\n", url)
			if open {
				if err := r.env.OpenURL(url); err != nil {
					fmt.Fprintf(r.env.Stderr, "waypoint: could not open the browser: %v\n", err)
				}
			}
		},
	})
	cancel()
	watcher.Wait()
	if err != nil {
		return r.fail(err)
	}
	return exitOK
}

// checkPort rejects ports the server cannot serve. Browsers leave the port out
// of the Host header on 80 and 443, and the Host check compares it explicitly.
func checkPort(port int) error {
	switch {
	case port < 0 || port > 65535:
		return fmt.Errorf("port %d is out of range", port)
	case port == 80 || port == 443:
		return fmt.Errorf(`port %d is not supported: browsers leave it out of the Host header, which Waypoint checks; set "port" in the config or pass --port`, port)
	}
	return nil
}

// certsFor places the certificate files under data_dir/tls.
func certsFor(cfg *config.Config, getenv func(string) string) server.Certs {
	return server.Certs{Dir: filepath.Join(cfg.DataDir, "tls"), Home: getenv("HOME")}
}

// watchTargets returns the function the watcher calls every second
// for the boards to watch. It re-reads the registry each time, so a project
// added while the server runs is picked up. When the config cannot be read
// (half-edited, say) the previous targets stay, so a typo does not make the
// page stop updating. Only the watcher goroutine calls it.
func watchTargets(path string, getenv func(string) string, log *slog.Logger) func() []watch.Target {
	var last []watch.Target
	var lastErr string
	return func() []watch.Target {
		cfg, err := config.Load(path, getenv)
		if err != nil {
			if msg := err.Error(); msg != lastErr {
				lastErr = msg
				log.Warn("cannot read the config; keeping the boards watched so far", "error", err)
			}
			return last
		}
		lastErr = ""
		last = last[:0:0]
		for _, p := range cfg.Projects {
			if p.IsServer() {
				last = append(last, serverTarget(p))
				continue
			}
			last = append(last, watch.Target{Project: p.Name, Path: p.Path})
		}
		return last
	}
}

// revisioner is what a repository on a database server offers the watcher.
type revisioner interface {
	Revision(ctx context.Context) (int64, error)
}

// serverTarget is the watcher's view of a project on a database server. The key
// changes when anything about the connection does, so the watcher reconnects.
func serverTarget(p config.ProjectRef) watch.Target {
	t := watch.Target{Project: p.Name, Key: fmt.Sprintf("%s|%s|%s|%s", p.Storage, p.Display(), p.User, p.SSL)}
	repo, err := storage.Open(p)
	if err == nil {
		if rv, ok := repo.(revisioner); ok {
			t.Revision = rv.Revision
			return t
		}
		err = fmt.Errorf("storage %q cannot be watched", p.Storage)
	}
	t.Revision = func(context.Context) (int64, error) { return 0, err }
	return t
}
