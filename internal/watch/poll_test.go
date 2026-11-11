package watch

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// guarantee is how long a change may take to reach subscribers, whatever the
// kernel does, with a margin for a loaded machine.
const guarantee = 2 * time.Second

// startWith is start with Options and hooks of the test's choosing.
func startWith(t *testing.T, o Options, h hooks, targets ...Target) *recorder {
	t.Helper()
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	synced := make(chan struct{})
	h.synced = synced
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, func() []Target { return targets }, rec.publish, o, h)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	select {
	case <-synced:
	case <-time.After(3 * time.Second):
		t.Fatal("watcher did not finish its first scan")
	}
	return rec
}

// seesNext makes a change and fails unless a publish follows within the guarantee.
func seesNext(t *testing.T, rec *recorder, what string, change func()) {
	t.Helper()
	time.Sleep(300 * time.Millisecond) // let any earlier burst finish
	seen := rec.count()
	change()
	deadline := time.Now().Add(guarantee)
	for rec.count() <= seen {
		if time.Now().After(deadline) {
			t.Fatalf("%s: no publish within %v", what, guarantee)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Saves that overlap on kqueue can leave the watch on a deleted inode, and then
// nothing is reported until a restart.
func TestOverlappingSavesDoNotBlindTheWatcher(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, "board.md")
	write(t, board, "0")
	rec, _ := start(t, Target{Project: "Demo", Path: board})

	external := filepath.Join(dir, "board.md.ext")
	stop := time.Now().Add(2 * time.Second)
	for i := 0; time.Now().Before(stop); i++ {
		write(t, external, strings.Repeat("e", i%50+1))
		if err := os.Rename(external, board); err != nil {
			t.Fatal(err)
		}
	}
	seesNext(t, rec, "the edit after a run of overlapping saves", func() { replace(t, board, "after") })
}

// The case the review named: an external save and an API write 30ms apart.
func TestSavesThirtyMillisecondsApart(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, "board.md")
	write(t, board, "0")
	rec, _ := start(t, Target{Project: "Demo", Path: board})

	external := filepath.Join(dir, "board.md.ext")
	for i := range 3 {
		write(t, external, "external")
		if err := os.Rename(external, board); err != nil {
			t.Fatal(err)
		}
		time.Sleep(30 * time.Millisecond)
		replace(t, board, "api")
		seesNext(t, rec, "the edit after them", func() { replace(t, board, strings.Repeat("x", i+2)) })
	}
}

func TestKernelEventsAreNotNeeded(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, "board.md")
	write(t, board, "one")
	rewatched := make(chan string, 8)
	rec := startWith(t, Options{}, hooks{dropEvents: true, rewatched: rewatched}, Target{Project: "Demo", Path: board})

	seesNext(t, rec, "a save", func() { replace(t, board, "two") })
	seesNext(t, rec, "an archive write", func() { write(t, filepath.Join(dir, "board.archive.md"), "x") })

	select {
	case got := <-rewatched:
		if real, _ := filepath.EvalSymlinks(dir); got != real {
			t.Fatalf("rewatched %q, want %q", got, real)
		}
	case <-time.After(time.Second):
		t.Fatal("a change no event reported did not rebuild the watch")
	}
}

func TestAPollDoesNotRepeatWhatAnEventPublished(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, "board.md")
	write(t, board, "one")
	rewatched := make(chan string, 8)
	rec := startWith(t, Options{}, hooks{rewatched: rewatched}, Target{Project: "Demo", Path: board})

	replace(t, board, "two")
	waitCount(t, rec, 1, guarantee)
	time.Sleep(pollEvery + 300*time.Millisecond)
	if got := rec.count(); got != 1 {
		t.Fatalf("published %d times, want 1", got)
	}
	if len(rewatched) != 0 {
		t.Fatal("a change the kernel reported rebuilt the watch")
	}
}

func TestDirectorySwappedAtTheSamePath(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "proj")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	board := filepath.Join(dir, "board.md")
	write(t, board, "one")
	rec, _ := start(t, Target{Project: "Demo", Path: board})

	seesNext(t, rec, "the swap", func() {
		if err := os.Rename(dir, dir+".old"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		write(t, board, "swapped in")
	})
	time.Sleep(pollEvery + 300*time.Millisecond) // the watch moves to the new directory

	// The kernel watch must be on the new directory, not just the poll: an
	// event arrives well inside a poll interval.
	seen := rec.count()
	replace(t, board, "after the swap")
	waitCount(t, rec, seen+1, 500*time.Millisecond)
}

func TestAncestorSwappedAtTheSamePath(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	dir := filepath.Join(parent, "proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	board := filepath.Join(dir, "board.md")
	write(t, board, "one")
	rec, _ := start(t, Target{Project: "Demo", Path: board})

	seesNext(t, rec, "the swap", func() {
		if err := os.Rename(parent, parent+".old"); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		write(t, board, "swapped in")
	})
	seesNext(t, rec, "an edit after the swap", func() { replace(t, board, "edited") })
}

func TestRetargetedSymlink(t *testing.T) {
	first, second, linkDir := t.TempDir(), t.TempDir(), t.TempDir()
	a, b := filepath.Join(first, "board.md"), filepath.Join(second, "board.md")
	write(t, a, "a")
	write(t, b, "the other board")
	link := filepath.Join(linkDir, "board.md")
	if err := os.Symlink(a, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	rec, _ := start(t, Target{Project: "Demo", Path: link})

	seesNext(t, rec, "the retarget", func() {
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(b, link); err != nil {
			t.Fatal(err)
		}
	})
	seesNext(t, rec, "an edit to the new target", func() { replace(t, b, "edited") })
}

// A new target in a directory already watched adds no watch, so only the
// fingerprint can notice it.
func TestSymlinkRetargetedWithinADirectory(t *testing.T) {
	dir, linkDir := t.TempDir(), t.TempDir()
	a, b := filepath.Join(dir, "a.md"), filepath.Join(dir, "b.md")
	write(t, a, "a")
	write(t, b, "the other board")
	link := filepath.Join(linkDir, "board.md")
	if err := os.Symlink(a, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	rec, _ := start(t, Target{Project: "Demo", Path: link})

	seesNext(t, rec, "the retarget", func() {
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(b, link); err != nil {
			t.Fatal(err)
		}
	})
}

func TestBoardRegisteredWithTheWrongCase(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, "BOARD.md")
	write(t, board, "one")
	wrong := filepath.Join(dir, "board.md")
	if _, err := os.Stat(wrong); err != nil {
		t.Skip("this disk is case-sensitive")
	}
	rec, _ := start(t, Target{Project: "Demo", Path: wrong})

	seesNext(t, rec, "a save", func() { replace(t, board, "two") })
	seesNext(t, rec, "another save", func() { replace(t, board, "three") })
}

func TestConfigChangePublishesProjects(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	write(t, cfg, "port: 1\n")
	var projects atomic.Int32
	startWith(t, Options{ConfigPath: cfg, Projects: func() { projects.Add(1) }}, hooks{})

	time.Sleep(pollEvery + 200*time.Millisecond)
	if got := projects.Load(); got != 0 {
		t.Fatalf("published %d times with no change", got)
	}
	replace(t, cfg, "port: 2\n")
	deadline := time.Now().Add(guarantee)
	for projects.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("no projects notice within %v", guarantee)
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(pollEvery + 200*time.Millisecond)
	if got := projects.Load(); got != 1 {
		t.Fatalf("published %d times for one change", got)
	}
}

func TestSustainedSavesPublishBeforeTheyStop(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, "board.md")
	write(t, board, "0")
	rec, _ := start(t, Target{Project: "Demo", Path: board})

	stop := time.Now().Add(2 * time.Second)
	for i := 0; time.Now().Before(stop); i++ {
		replace(t, board, strings.Repeat("x", i%7+1))
		time.Sleep(50 * time.Millisecond)
	}
	if got := rec.count(); got < 2 {
		t.Fatalf("%d publishes during 2s of saves, want one about every 750ms", got)
	}
}

// syncBuffer is a log sink the watcher writes while the test reads.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestFailedWatchIsLoggedOncePerDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "missing")
	board := filepath.Join(dir, "board.md")
	var logs syncBuffer
	rec := startWith(t, Options{Logger: slog.New(slog.NewTextHandler(&logs, nil))}, hooks{}, Target{Project: "Demo", Path: board})

	time.Sleep(2*pollEvery + 300*time.Millisecond)
	if got := strings.Count(logs.String(), "cannot watch"); got != 1 {
		t.Fatalf("logged %d times, want once:\n%s", got, logs.String())
	}

	// The poll still sees the board arrive.
	seesNext(t, rec, "the board appearing", func() {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		write(t, board, "here")
	})
}
