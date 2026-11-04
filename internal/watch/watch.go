// Package watch turns changes to board files on disk into per-project notices.
package watch

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	// debounce is how long a project must be quiet before it is published, so
	// one save (temp file, rename, archive write) gives one notice.
	debounce = 150 * time.Millisecond
	// refreshEvery is how often the target list is re-read and directories
	// that vanished or appeared are reconciled.
	refreshEvery = 2 * time.Second
)

// Target is one board to watch: the project's name and the path of its board file.
type Target struct{ Project, Path string }

// Run watches every target's board until ctx ends and calls publish(project)
// once per burst of changes. targets is re-read every 2s so registry changes
// are picked up. Run returns only when ctx ends (nil), or if the OS watcher
// cannot be created.
//
// It watches the directory holding each board's real file, not the file:
// saves rename a temp file over the board, which replaces the inode. Only the
// board, its .archive.md and its -wal file count; Waypoint's own temp files
// (.waypoint-*) and .lock files are never in that set, so they are ignored.
func Run(ctx context.Context, targets func() []Target, publish func(project string)) error {
	return run(ctx, targets, publish, nil)
}

// routes maps a watched directory to the base names in it that matter, and
// each name to the projects it belongs to.
type routes map[string]map[string][]string

func run(ctx context.Context, targets func() []Target, publish func(project string), synced chan<- struct{}) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()

	tick := time.NewTicker(refreshEvery)
	defer tick.Stop()

	// due holds, per project, when its debounce ends. One timer serves them all.
	// (Go 1.23+ timers never deliver a stale value after Stop or Reset.)
	var table routes
	due := map[string]time.Time{}
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	rearm := func() {
		timer.Stop()
		var next time.Time
		for _, d := range due {
			if next.IsZero() || d.Before(next) {
				next = d
			}
		}
		if !next.IsZero() {
			timer.Reset(time.Until(next))
		}
	}
	schedule := func(project string) {
		due[project] = time.Now().Add(debounce)
		rearm()
	}

	// refresh re-reads the targets and reconciles the watched directories. A
	// directory that cannot be watched right now (it is gone) is simply left
	// out; the next refresh tries again. A directory added after the first
	// scan may have changed while unwatched, so its projects are published.
	first := true
	refresh := func() {
		table = buildRoutes(targets())
		have := map[string]bool{}
		for _, d := range w.WatchList() {
			have[d] = true
		}
		for dir, names := range table {
			if have[dir] {
				continue
			}
			if w.Add(dir) != nil {
				continue
			}
			if !first {
				for _, projects := range names {
					for _, p := range projects {
						schedule(p)
					}
				}
			}
		}
		for d := range have {
			if _, want := table[d]; !want {
				_ = w.Remove(d)
			}
		}
		first = false
	}

	refresh()
	if synced != nil {
		close(synced)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			refresh()
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if ev.Op == fsnotify.Chmod {
				continue
			}
			for _, p := range table[filepath.Dir(ev.Name)][filepath.Base(ev.Name)] {
				schedule(p)
			}
		case <-w.Errors:
			// Overflow and the like: nothing to act on, the next event or refresh recovers.
		case <-timer.C:
			now := time.Now()
			for project, d := range due {
				if !d.After(now) {
					delete(due, project)
					publish(project)
				}
			}
			rearm()
		}
	}
}

func buildRoutes(ts []Target) routes {
	table := routes{}
	for _, t := range ts {
		dir, board := locate(t.Path)
		names := table[dir]
		if names == nil {
			names = map[string][]string{}
			table[dir] = names
		}
		stem := strings.TrimSuffix(board, filepath.Ext(board))
		for _, name := range []string{board, stem + ".archive.md", board + "-wal"} {
			names[name] = append(names[name], t.Project)
		}
	}
	return table
}

// locate returns the directory and base name of the real board file. Markdown
// boards may be reached through a symlink, and saves land next to the real
// file. If the file is gone, its parent directory is still resolved so the
// file coming back is noticed.
func locate(path string) (dir, base string) {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	} else if parent, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		path = filepath.Join(parent, filepath.Base(path))
	}
	return filepath.Dir(path), filepath.Base(path)
}
