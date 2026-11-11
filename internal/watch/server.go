package watch

import (
	"context"
	"time"
)

// ServerPoll is how often a project on a database server is checked for a new
// revision.
const ServerPoll = 2 * time.Second

// backoffSteps is how many poll intervals the wait grows to during an outage:
// 2s becomes 30s.
const backoffSteps = 15

// serverPoller is the goroutine that follows one server project.
type serverPoller struct {
	key    string
	cancel context.CancelFunc
}

// syncServers starts a poller for each server target that has none, restarts
// one whose Key changed and stops the ones whose project is gone.
func (x *watcher) syncServers(ts []Target) {
	want := map[string]Target{}
	for _, t := range ts {
		want[t.Project] = t
	}
	for project, p := range x.servers {
		if t, ok := want[project]; !ok || t.Key != p.key {
			p.cancel()
			delete(x.servers, project)
		}
	}
	for project, t := range want {
		if _, running := x.servers[project]; running {
			continue
		}
		ctx, cancel := context.WithCancel(x.ctx)
		x.servers[project] = &serverPoller{key: t.Key, cancel: cancel}
		x.wg.Add(1)
		go func() {
			defer x.wg.Done()
			x.pollServer(ctx, t)
		}()
	}
}

// pollServer reads the revision again and again until ctx ends, and tells the
// main loop when it changes. The first value is only remembered. An outage is
// logged once, at its start, and once when it ends; the wait between tries
// grows during it. The project is published when the server comes back, since
// changes made meanwhile were not seen.
func (x *watcher) pollServer(ctx context.Context, t Target) {
	poll := x.opts.ServerPoll
	if poll <= 0 {
		poll = ServerPoll
	}
	var last int64
	have, down := false, false
	failures := 0
	for {
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		rev, err := t.Revision(rctx)
		cancel()
		if ctx.Err() != nil {
			return
		}
		delay := poll
		publish := false
		switch {
		case err != nil:
			failures++
			if !down {
				down = true
				x.log.Warn("cannot read a project's database; will keep trying and say when it is back", "project", t.Project, "error", err)
			}
			delay = poll * time.Duration(min(1<<min(failures, 4), backoffSteps))
		default:
			failures = 0
			if down {
				down = false
				x.log.Info("a project's database is reachable again", "project", t.Project)
				publish = true
			} else if have && rev != last {
				publish = true
			}
			have, last = true, rev
		}
		if publish {
			select {
			case x.serverCh <- t.Project:
			case <-ctx.Done():
				return
			}
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
	}
}
