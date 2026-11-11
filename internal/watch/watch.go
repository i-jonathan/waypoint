// Package watch turns changes to board files on disk into per-project notices.
package watch

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	// debounce is how long a project must be quiet before it is published, so
	// one save (temp file, rename, archive write) gives one notice.
	debounce = 150 * time.Millisecond
	// maxWait caps the debounce, so saves that never pause still publish.
	maxWait = 750 * time.Millisecond
	// pollEvery is how often the targets are re-read, directories reconciled and
	// fingerprints compared. With debounce it bounds how late a change can be.
	pollEvery = time.Second
)

// Target is one board to watch: the project's name and the path of its board
// file. A project on a database server has no file: it sets Revision instead,
// and Path is ignored.
type Target struct {
	Project, Path string
	// Revision reads the board's save counter from the server. The watcher
	// calls it about every ServerPoll and publishes the project when the value
	// changes, or when the server comes back after an outage.
	Revision func(ctx context.Context) (int64, error)
	// Key identifies the connection behind Revision. Revision may be a new
	// function on every call to the targets func; the poller is restarted only
	// when Key changes.
	Key string
}

// Options are what Run needs beyond the boards.
type Options struct {
	ConfigPath string        // the config file; "" watches none
	Projects   func()        // called when the config file changes
	Logger     *slog.Logger  // nil logs nothing
	ServerPoll time.Duration // how often a server project is polled; 0 means ServerPoll (2s)
}

// Run watches every target's board until ctx ends and calls publish(project)
// once per burst of changes. Run returns only when ctx ends (nil), or if the OS
// watcher cannot be created.
//
// Two things feed it. The kernel watch is the fast path but is not relied on:
// kqueue can end up on a deleted inode, and no kernel watch notices a directory
// replaced at its path, a retargeted symlink or a name of the wrong case. So once
// a second Run also compares a fingerprint of each board, its archive and the
// config file. That poll is the guarantee: a change reaches publish within about
// 1.5s whatever the kernel does. A difference no event reported means the kernel
// watch has gone wrong, and it is rebuilt.
//
// It watches the directory holding each board's real file, not the file:
// saves rename a temp file over the board, which replaces the inode. Only the
// board, its .archive.md and its -wal file count; Waypoint's own temp files
// (.waypoint-*) and .lock files are never in that set, so they are ignored.
func Run(ctx context.Context, targets func() []Target, publish func(project string), o Options) error {
	return run(ctx, targets, publish, o, hooks{})
}

// routes maps a watched directory to the base names in it that matter, and
// each name to the projects it belongs to.
type routes map[string]map[string][]string

// hooks are test seams; the zero value does nothing.
type hooks struct {
	synced chan<- struct{} // closed after the first scan of the targets
	errs   <-chan error    // extra watcher errors, as if from fsnotify

	dropEvents bool          // ignore every kernel event, as if the watch had died
	rewatched  chan<- string // told each directory whose watch was rebuilt
}

// watcher is the state of one Run. Only its goroutine touches it.
type watcher struct {
	w       *fsnotify.Watcher
	targets func() []Target
	publish func(project string)
	opts    Options
	log     *slog.Logger
	h       hooks

	table routes
	first bool // the first scan has not finished

	// due holds, per project, when its debounce ends, and burst when the changes
	// being debounced began. One timer serves them all. (Go 1.23+ timers never
	// deliver a stale value after Stop or Reset.)
	due   map[string]time.Time
	burst map[string]time.Time
	timer *time.Timer

	boards    map[string]boardFiles  // by project
	prints    map[string]fingerprint // by project, as last published or polled
	config    fileState
	dirs      map[string]os.FileInfo // each watched directory, as it was when watched
	addFailed map[string]bool        // directories whose failure has been logged

	ctx      context.Context
	servers  map[string]*serverPoller // by project
	serverCh chan string              // projects whose server board changed
	wg       sync.WaitGroup
}

func run(ctx context.Context, targets func() []Target, publish func(project string), o Options, h hooks) error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer fw.Close()

	x := &watcher{
		w: fw, targets: targets, publish: publish, opts: o, log: o.Logger, h: h,
		first: true,
		due:   map[string]time.Time{}, burst: map[string]time.Time{},
		timer:  time.NewTimer(time.Hour),
		boards: map[string]boardFiles{}, prints: map[string]fingerprint{},
		dirs: map[string]os.FileInfo{}, addFailed: map[string]bool{},
		servers: map[string]*serverPoller{}, serverCh: make(chan string),
	}
	var stop context.CancelFunc
	x.ctx, stop = context.WithCancel(ctx)
	defer x.wg.Wait() // runs last: the pollers are cancelled first
	defer stop()
	defer x.timer.Stop()
	if x.log == nil {
		x.log = slog.New(slog.DiscardHandler)
	}

	tick := time.NewTicker(pollEvery)
	defer tick.Stop()

	x.config = stateOf(o.ConfigPath)
	x.refresh()
	x.first = false
	if h.synced != nil {
		close(h.synced)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			x.poll()
		case ev, ok := <-fw.Events:
			if !ok {
				return nil
			}
			if ev.Op == fsnotify.Chmod || h.dropEvents {
				continue
			}
			for _, p := range x.table[filepath.Dir(ev.Name)][filepath.Base(ev.Name)] {
				x.schedule(p)
			}
		case <-fw.Errors:
			x.resync()
		case <-h.errs:
			x.resync()
		case p := <-x.serverCh:
			x.schedule(p)
		case <-x.timer.C:
			x.publishDue()
		}
	}
}

func (x *watcher) rearm() {
	x.timer.Stop()
	var next time.Time
	for _, d := range x.due {
		if next.IsZero() || d.Before(next) {
			next = d
		}
	}
	if !next.IsZero() {
		x.timer.Reset(time.Until(next))
	}
}

// schedule starts or extends a project's debounce, up to maxWait from the first
// change of the burst.
func (x *watcher) schedule(project string) {
	now := time.Now()
	began, ok := x.burst[project]
	if !ok {
		began = now
		x.burst[project] = now
	}
	at := now.Add(debounce)
	if limit := began.Add(maxWait); at.After(limit) {
		at = limit
	}
	x.due[project] = at
	x.rearm()
}

// publishDue publishes every project whose debounce has ended. The fingerprint is
// taken first: a change after it shows up at the next poll, and a change before
// it is in what subscribers read after this notice, so the poll stays quiet about
// anything an event already reported.
func (x *watcher) publishDue() {
	now := time.Now()
	for project, d := range x.due {
		if d.After(now) {
			continue
		}
		delete(x.due, project)
		delete(x.burst, project)
		if files, ok := x.boards[project]; ok {
			x.prints[project] = files.fingerprint()
		}
		x.publish(project)
	}
	x.rearm()
}

// resync follows a watcher error (an overflow may have dropped events): there is
// no telling which boards changed, so every project is published.
func (x *watcher) resync() {
	for project := range x.boards {
		x.schedule(project)
	}
}

// poll is the once-a-second pass that does not depend on the kernel.
func (x *watcher) poll() {
	x.pollConfig()
	x.refresh()
	for project, files := range x.boards {
		if _, heard := x.due[project]; heard {
			continue // an event got there first, and publishing fingerprints it
		}
		now := files.fingerprint()
		if now.same(x.prints[project]) {
			continue
		}
		x.prints[project] = now
		x.schedule(project)
		x.rewatch(files.dir)
	}
}

func (x *watcher) pollConfig() {
	if x.opts.ConfigPath == "" {
		return
	}
	now := stateOf(x.opts.ConfigPath)
	if now.same(x.config) {
		return
	}
	x.config = now
	if x.opts.Projects != nil {
		x.opts.Projects()
	}
}

// refresh re-reads the targets and reconciles the watched directories. A
// directory that cannot be watched right now (it is gone) is left out and the
// next poll tries again. A directory added after the first scan may have changed
// while unwatched, so its projects are published.
func (x *watcher) refresh() {
	all := x.targets()
	var ts, servers []Target
	for _, t := range all {
		if t.Revision != nil {
			servers = append(servers, t)
		} else {
			ts = append(ts, t)
		}
	}
	x.syncServers(servers)
	x.table = buildRoutes(ts)

	boards := make(map[string]boardFiles, len(ts))
	for _, t := range ts {
		boards[t.Project] = filesOf(t)
		if _, known := x.boards[t.Project]; !known {
			x.prints[t.Project] = boards[t.Project].fingerprint()
		}
	}
	for project := range x.boards {
		if _, keep := boards[project]; !keep {
			delete(x.prints, project)
		}
	}
	x.boards = boards

	have := map[string]bool{}
	for _, d := range x.w.WatchList() {
		have[d] = true
	}
	for dir, names := range x.table {
		info, err := os.Stat(dir)
		if have[dir] && (err != nil || x.dirs[dir] == nil || !os.SameFile(info, x.dirs[dir])) {
			_ = x.w.Remove(dir) // a different directory now has that path
			delete(x.dirs, dir)
			have[dir] = false
		}
		if have[dir] {
			continue
		}
		x.clearDropped(dir)
		if !x.add(dir, info) {
			continue
		}
		if !x.first {
			for _, projects := range names {
				for _, p := range projects {
					x.schedule(p)
				}
			}
		}
	}
	for d := range have {
		if _, want := x.table[d]; !want {
			_ = x.w.Remove(d)
			delete(x.dirs, d)
		}
	}
}

// clearDropped readies a directory the kernel watch let go of by itself. On a
// rename or delete of a directory, kqueue drops the directory's own watch but
// leaves the per-file ones it made on the old inodes, and a later Add reuses them
// and stays deaf to those names. Only Remove clears them, and Remove needs the
// directory watched.
func (x *watcher) clearDropped(dir string) {
	if _, dropped := x.dirs[dir]; !dropped {
		return
	}
	if x.w.Add(dir) == nil {
		_ = x.w.Remove(dir)
		delete(x.dirs, dir)
	}
}

// add watches dir, which info describes, and reports whether that worked. A
// failure is logged once, not on every poll, until the directory can be watched.
func (x *watcher) add(dir string, info os.FileInfo) bool {
	if err := x.w.Add(dir); err != nil {
		if !x.addFailed[dir] {
			x.addFailed[dir] = true
			x.log.Warn("cannot watch a directory; changes in it will take up to a second longer to show", "dir", dir, "error", err)
		}
		return false
	}
	delete(x.addFailed, dir)
	x.dirs[dir] = info
	return true
}

// rewatch rebuilds the kernel watch on dir. A change made while it is down is
// caught by the next poll.
func (x *watcher) rewatch(dir string) {
	if x.h.rewatched != nil {
		select {
		case x.h.rewatched <- dir:
		default:
		}
	}
	_ = x.w.Remove(dir)
	delete(x.dirs, dir)
	if info, err := os.Stat(dir); err == nil {
		x.add(dir, info)
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
