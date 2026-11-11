package postgres_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"waypoint/internal/app"
	"waypoint/internal/domain"
	"waypoint/internal/storage/postgres"
)

// addCard appends one card under the lock.
func addCard(t *testing.T, r *postgres.Repo) {
	t.Helper()
	b, _, err := r.Load(ctx)
	if err != nil {
		t.Errorf("Load: %v", err)
		return
	}
	id := b.Settings.NextID
	b.Settings.NextID++
	b.Cards = append(b.Cards, domain.Card{ID: domain.CardID(fmt.Sprintf("T-%d", id)), Title: "c", Column: b.Settings.Columns[0].Name})
	if err := r.Save(ctx, b); err != nil {
		t.Errorf("Save: %v", err)
	}
}

func TestLockSerialisesWriters(t *testing.T) {
	_, ref := newRepo(t)
	var wg sync.WaitGroup
	for w := 0; w < 2; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := postgres.New(ref)
			for i := 0; i < 50; i++ {
				unlock, err := r.Lock(ctx)
				if err != nil {
					t.Errorf("Lock: %v", err)
					return
				}
				addCard(t, r)
				if err := unlock(); err != nil {
					t.Errorf("unlock: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	b, _, err := postgres.New(ref).Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Cards) != 100 {
		t.Fatalf("%d cards after 2x50 locked adds, want 100", len(b.Cards))
	}
}

func TestLockCapReturnsBusy(t *testing.T) {
	r1, ref := newRepo(t)
	unlock, err := r1.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	defer postgres.SetLockWaitLimit(postgres.SetLockWaitLimit(200 * time.Millisecond))
	start := time.Now()
	_, err = postgres.New(ref).Lock(ctx)
	if !errors.Is(err, app.ErrBusy) {
		t.Fatalf("second Lock = %v, want app.ErrBusy", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("second Lock took %v", d)
	}
}

func TestCallerDeadlineEndsTheWait(t *testing.T) {
	r1, ref := newRepo(t)
	unlock, err := r1.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	c, cancel := contextWithTimeout(300 * time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := postgres.New(ref).Lock(c); err == nil {
		t.Fatal("Lock should fail when the caller's deadline passes")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("Lock ignored the caller's deadline: %v", d)
	}
}

func TestRetiredWhileWaiting(t *testing.T) {
	r1, ref := newRepo(t)
	unlock, err := r1.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	waiter := postgres.New(ref)
	done := make(chan error, 1)
	go func() {
		u, err := waiter.Lock(ctx)
		if err == nil {
			u()
		}
		done <- err
	}()
	time.Sleep(400 * time.Millisecond) // let the waiter block on the row lock
	if err := r1.Retire(ctx); err != nil {
		t.Fatal(err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "waypoint_meta") {
			t.Fatalf("waiting writer got %v, want the retired error", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the waiting writer never returned")
	}
}

func TestChangedDuringWrite(t *testing.T) {
	r1, ref := newRepo(t)
	b, _, err := r1.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r2 := postgres.New(ref)
	addCard(t, r2) // loads and saves from the other Repo
	b.Settings.Name = "stale"
	if err := r1.Save(ctx, b); !errors.Is(err, app.ErrChangedDuringWrite) {
		t.Fatalf("stale Save = %v, want ErrChangedDuringWrite", err)
	}
	got, _, _ := postgres.New(ref).Load(ctx)
	if got.Settings.Name == "stale" || len(got.Cards) != 1 {
		t.Fatalf("the stale save changed the board: %q, %d cards", got.Settings.Name, len(got.Cards))
	}
}

func TestRecoversAfterConnectionKilled(t *testing.T) {
	r, ref := newRepo(t)
	if _, _, err := r.Load(ctx); err != nil {
		t.Fatal(err)
	}
	admin := raw(t, ref, "postgres")
	mustExec(t, admin, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, ref.Database)
	if _, _, err := r.Load(ctx); err != nil {
		t.Fatalf("Load after the server dropped every connection: %v", err)
	}
	// A held lock whose connection dies reports it at unlock instead of pretending.
	unlock, err := r.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, admin, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, ref.Database)
	if err := unlock(); err == nil {
		t.Error("unlock after the connection died should report an error")
	}
	if _, _, err := r.Load(ctx); err != nil {
		t.Fatalf("Load after a dead lock: %v", err)
	}
}

func TestUnlockOnContextCancel(t *testing.T) {
	r1, ref := newRepo(t)
	unlock, err := r1.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c, cancel := contextWithCancel()
	done := make(chan error, 1)
	go func() { _, err := postgres.New(ref).Lock(c); done <- err }()
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled Lock should fail")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling did not end the wait")
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	// Nothing leaked: the lock can be taken again at once.
	c2, cancel2 := contextWithTimeout(5 * time.Second)
	defer cancel2()
	u2, err := postgres.New(ref).Lock(c2)
	if err != nil {
		t.Fatalf("Lock after a cancelled wait: %v", err)
	}
	u2()
}
