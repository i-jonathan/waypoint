package watch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"
)

// recorder collects published project names.
type recorder struct {
	mu  sync.Mutex
	got []string
}

func (r *recorder) publish(project string) {
	r.mu.Lock()
	r.got = append(r.got, project)
	r.mu.Unlock()
}

func (r *recorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.got...)
}

func (r *recorder) count() int { return len(r.all()) }

// start runs the watcher over fixed targets and returns once its first scan of
// the targets is done, so the test's writes cannot beat the watch being set up.
// The returned wait func cancels the watcher and returns Run's result.
func start(t *testing.T, targets ...Target) (*recorder, func() error) {
	t.Helper()
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	synced := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- run(ctx, func() []Target { return targets }, rec.publish, Options{}, hooks{synced: synced})
	}()
	select {
	case <-synced:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("watcher did not finish its first scan")
	}
	var once sync.Once
	var err error
	stop := func() error {
		once.Do(func() {
			cancel()
			select {
			case err = <-result:
			case <-time.After(3 * time.Second):
				t.Error("Run did not return after its context ended")
			}
		})
		return err
	}
	t.Cleanup(func() { stop() })
	return rec, stop
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// replace writes content to a temp file beside path and renames it over path,
// the way Waypoint saves a board.
func replace(t *testing.T, path, content string) {
	t.Helper()
	tmp := filepath.Join(filepath.Dir(path), ".waypoint-12345")
	write(t, tmp, content)
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

// waitCount waits up to timeout for at least n publishes.
func waitCount(t *testing.T, rec *recorder, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for rec.count() < n {
		if time.Now().After(deadline) {
			t.Fatalf("got %v, want at least %d publishes within %v", rec.all(), n, timeout)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// quiet asserts no more than n publishes in total after letting the dust settle.
func quiet(t *testing.T, rec *recorder, n int) {
	t.Helper()
	time.Sleep(400 * time.Millisecond)
	if got := rec.all(); len(got) != n {
		t.Fatalf("got %v, want exactly %d publishes", got, n)
	}
}

func TestWatchAtomicRename(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, "board.md")
	write(t, board, "one")
	rec, _ := start(t, Target{"Demo", board})

	replace(t, board, "two")

	waitCount(t, rec, 1, 2*time.Second)
	quiet(t, rec, 1)
	if got := rec.all()[0]; got != "Demo" {
		t.Fatalf("published %q, want Demo", got)
	}
}

func TestWatchDebounce(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, "board.md")
	write(t, board, "0")
	rec, _ := start(t, Target{"Demo", board})

	for i := 1; i <= 5; i++ {
		write(t, board, string(rune('0'+i)))
		time.Sleep(10 * time.Millisecond)
	}

	waitCount(t, rec, 1, 2*time.Second)
	quiet(t, rec, 1)
}

func TestWatchIgnoresOtherFiles(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, "board.md")
	write(t, board, "one")
	rec, _ := start(t, Target{"Demo", board})

	write(t, filepath.Join(dir, "notes.md"), "x")
	write(t, filepath.Join(dir, ".waypoint-999"), "x")
	write(t, filepath.Join(dir, "board.md.lock"), "x")
	write(t, filepath.Join(dir, "other.archive.md"), "x")
	if err := os.Remove(filepath.Join(dir, "notes.md")); err != nil {
		t.Fatal(err)
	}

	quiet(t, rec, 0)
}

func TestWatchArchiveAndWalMapToTheirProject(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.md")
	b := filepath.Join(dir, "b.md")
	write(t, a, "a")
	write(t, b, "b")
	rec, _ := start(t, Target{"A", a}, Target{"B", b})

	write(t, filepath.Join(dir, "a.archive.md"), "x")
	waitCount(t, rec, 1, 2*time.Second)
	quiet(t, rec, 1)
	if got := rec.all()[0]; got != "A" {
		t.Fatalf("archive change published %q, want A", got)
	}

	write(t, filepath.Join(dir, "b.md-wal"), "x")
	waitCount(t, rec, 2, 2*time.Second)
	quiet(t, rec, 2)
	if got := rec.all()[1]; got != "B" {
		t.Fatalf("wal change published %q, want B", got)
	}
}

func TestWatchFollowsSymlinkedBoard(t *testing.T) {
	realDir := t.TempDir()
	linkDir := t.TempDir()
	real := filepath.Join(realDir, "board.md")
	write(t, real, "one")
	link := filepath.Join(linkDir, "board.md")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	rec, _ := start(t, Target{"Demo", link})

	replace(t, real, "two") // Waypoint writes next to the real file

	waitCount(t, rec, 1, 2*time.Second)
	quiet(t, rec, 1)
}

func TestWatchNoticesBoardCreatedAfterStart(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, "board.md") // does not exist yet
	rec, _ := start(t, Target{"Demo", board})

	write(t, board, "hello")

	waitCount(t, rec, 1, 2*time.Second)
}

func TestWatchSurvivesDirectoryRemoval(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "proj")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	board := filepath.Join(dir, "board.md")
	write(t, board, "one")
	rec, stop := start(t, Target{"Demo", board})

	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	seen := rec.count() // whatever the removal itself published

	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, board, "back")

	// The directory is back; the 2s refresh must pick it up again.
	waitCount(t, rec, seen+1, 5*time.Second)
	time.Sleep(400 * time.Millisecond)
	seen = rec.count()

	// And it is watched properly again.
	replace(t, board, "again")
	waitCount(t, rec, seen+1, 2*time.Second)

	if err := stop(); err != nil {
		t.Fatalf("Run returned %v", err)
	}
}

func TestWatchReloadsTargets(t *testing.T) {
	first := filepath.Join(t.TempDir(), "first.md")
	second := filepath.Join(t.TempDir(), "second.md")
	write(t, first, "1")
	write(t, second, "2")

	var mu sync.Mutex
	targets := []Target{{"First", first}}
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	synced := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, func() []Target {
			mu.Lock()
			defer mu.Unlock()
			return append([]Target(nil), targets...)
		}, rec.publish, Options{}, hooks{synced: synced})
	}()
	<-synced

	mu.Lock()
	targets = append(targets, Target{"Second", second})
	mu.Unlock()

	// The new directory is noticed on the next refresh, and published once
	// because it may have changed while unwatched.
	waitCount(t, rec, 1, 5*time.Second)
	quiet(t, rec, 1)
	if got := rec.all()[0]; got != "Second" {
		t.Fatalf("published %q, want Second", got)
	}

	replace(t, second, "3")
	waitCount(t, rec, 2, 2*time.Second)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned %v", err)
	}
}

func TestRunReturnsWhenContextEnds(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, "board.md")
	write(t, board, "one")
	_, stop := start(t, Target{"Demo", board})
	if err := stop(); err != nil {
		t.Fatalf("Run returned %v after cancel, want nil", err)
	}
}

func TestWatchErrorRepublishesEveryProject(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.md")
	b := filepath.Join(t.TempDir(), "b.md")
	write(t, a, "a")
	write(t, b, "b")
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	synced := make(chan struct{})
	errs := make(chan error, 1)
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, func() []Target { return []Target{{"A", a}, {"B", b}} }, rec.publish, Options{}, hooks{synced: synced, errs: errs})
	}()
	<-synced

	errs <- errors.New("queue overflow")

	waitCount(t, rec, 2, 2*time.Second)
	quiet(t, rec, 2)
	got := rec.all()
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Fatalf("got %v, want A and B once each", got)
	}
	cancel()
	<-done
}
