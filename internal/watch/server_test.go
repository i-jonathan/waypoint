package watch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"waypoint/internal/dbconn"
)

// fakeRev stands in for a database server's revision counter.
type fakeRev struct {
	mu    sync.Mutex
	rev   int64
	err   error
	calls atomic.Int64
}

func (f *fakeRev) Revision(ctx context.Context) (int64, error) {
	f.calls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rev, f.err
}

func (f *fakeRev) set(rev int64, err error) {
	f.mu.Lock()
	f.rev, f.err = rev, err
	f.mu.Unlock()
}

func (f *fakeRev) target(project string) Target {
	return Target{Project: project, Key: project, Revision: f.Revision}
}

// logBuf is a slog destination that tests can read while the watcher writes.
type logBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func (l *logBuf) lines(contains string) int {
	n := 0
	for _, line := range strings.Split(l.String(), "\n") {
		if strings.Contains(line, contains) {
			n++
		}
	}
	return n
}

func serverOpts(logs *logBuf) Options {
	return Options{ServerPoll: 10 * time.Millisecond, Logger: slog.New(slog.NewTextHandler(logs, nil))}
}

func waitFor(t *testing.T, what string, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: not within %v", what, within)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestServerTargetPublishesOnChange(t *testing.T) {
	f := &fakeRev{rev: 1}
	var logs logBuf
	rec := startWith(t, serverOpts(&logs), hooks{}, f.target("Pg"))
	waitFor(t, "first poll", 2*time.Second, func() bool { return f.calls.Load() >= 3 })
	if n := rec.count(); n != 0 {
		t.Fatalf("%d publishes before anything changed", n)
	}
	f.set(2, nil)
	waitFor(t, "publish after a change", 3*time.Second, func() bool { return rec.count() >= 1 })
	time.Sleep(300 * time.Millisecond)
	if got := rec.all(); len(got) != 1 || got[0] != "Pg" {
		t.Fatalf("publishes = %v, want exactly [Pg]", got)
	}
}

func TestServerTargetErrorLoggedOnce(t *testing.T) {
	f := &fakeRev{err: errors.New("cannot reach the database server at db:5432")}
	var logs logBuf
	startWith(t, serverOpts(&logs), hooks{}, f.target("Pg"))
	waitFor(t, "20 failing polls", 10*time.Second, func() bool { return f.calls.Load() >= 20 })
	if n := logs.lines("cannot reach"); n != 1 {
		t.Fatalf("%d log lines for one outage, want 1:\n%s", n, logs.String())
	}
}

func TestServerTargetRecovers(t *testing.T) {
	f := &fakeRev{rev: 5, err: errors.New("down")}
	var logs logBuf
	rec := startWith(t, serverOpts(&logs), hooks{}, f.target("Pg"))
	waitFor(t, "a few failures", 5*time.Second, func() bool { return f.calls.Load() >= 4 })
	f.set(5, nil) // back, and the board is unchanged: still published, since changes may have been missed
	waitFor(t, "publish after recovery", 5*time.Second, func() bool { return rec.count() >= 1 })
	time.Sleep(300 * time.Millisecond)
	if got := rec.all(); len(got) != 1 {
		t.Fatalf("publishes = %v, want exactly one", got)
	}
	if logs.lines("down") != 1 || logs.lines("reachable again") != 1 {
		t.Fatalf("logs should have one outage line and one recovery line:\n%s", logs.String())
	}
}

func TestServerTargetMissingPasswordLoggedOnce(t *testing.T) {
	f := &fakeRev{err: fmt.Errorf("%w: db:5432 wants a password for user %q: %s", dbconn.ErrPassword, "u", dbconn.PasswordHelp("pg"))}
	var logs logBuf
	startWith(t, serverOpts(&logs), hooks{}, f.target("pg"))
	waitFor(t, "many polls", 10*time.Second, func() bool { return f.calls.Load() >= 12 })
	if n := logs.lines("WAYPOINT_DB_PASSWORD_PG"); n != 1 {
		t.Fatalf("the missing password was logged %d times, want 1:\n%s", n, logs.String())
	}
}

func TestRemovedProjectStopsPolling(t *testing.T) {
	f := &fakeRev{rev: 1}
	var mu sync.Mutex
	targets := []Target{f.target("Pg")}
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	synced := make(chan struct{})
	done := make(chan error, 1)
	var logs logBuf
	go func() {
		done <- run(ctx, func() []Target { mu.Lock(); defer mu.Unlock(); return targets }, rec.publish, serverOpts(&logs), hooks{synced: synced})
	}()
	<-synced
	waitFor(t, "polling", 2*time.Second, func() bool { return f.calls.Load() >= 3 })
	mu.Lock()
	targets = nil
	mu.Unlock()
	// The registry is re-read about once a second; polling must stop soon after.
	time.Sleep(2500 * time.Millisecond)
	before := f.calls.Load()
	time.Sleep(300 * time.Millisecond)
	if after := f.calls.Load(); after != before {
		t.Fatalf("still polling a removed project: %d -> %d calls", before, after)
	}
	cancel()
	<-done
}

func TestServerAndFileTargetsTogether(t *testing.T) {
	f := &fakeRev{rev: 1}
	dir := t.TempDir()
	board := dir + "/board.md"
	write(t, board, "one")
	var logs logBuf
	rec := startWith(t, serverOpts(&logs), hooks{}, Target{Project: "Demo", Path: board}, f.target("Pg"))
	seesNext(t, rec, "a file change", func() { write(t, board, "two") })
	f.set(2, nil)
	waitFor(t, "a server change", 3*time.Second, func() bool {
		for _, p := range rec.all() {
			if p == "Pg" {
				return true
			}
		}
		return false
	})
}
