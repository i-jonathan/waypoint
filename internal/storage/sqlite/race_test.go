package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"waypoint/internal/app"
	"waypoint/internal/domain"
	"waypoint/internal/storage/sqlite"
)

var day = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

func TestLockExcludesAnotherRepo(t *testing.T) {
	a, path := newDB(t)
	b := sqlite.New(path)
	unlock, err := a.Lock(context.Background())
	if err != nil {
		t.Fatalf("first Lock: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = b.Lock(ctx)
	if err == nil || !strings.Contains(err.Error(), "database is busy") {
		t.Fatalf("second Lock = %v, want the busy message", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second Lock = %v, want it to wrap the context deadline", err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	unlock2, err := b.Lock(context.Background())
	if err != nil {
		t.Fatalf("Lock after unlock: %v", err)
	}
	unlock2()
}

func TestSaveWithoutLockDetectsOtherWriter(t *testing.T) {
	a, path := newDB(t)
	ctx := context.Background()
	ba, _, err := a.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}

	b := sqlite.New(path)
	bb, _, err := b.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bb.AddCard(domain.NewCard{Title: "From B"}, day); err != nil {
		t.Fatal(err)
	}
	if err := b.Save(ctx, bb); err != nil {
		t.Fatal(err)
	}

	if _, err := ba.AddCard(domain.NewCard{Title: "From A"}, day); err != nil {
		t.Fatal(err)
	}
	if err := a.Save(ctx, ba); !errors.Is(err, app.ErrChangedDuringWrite) {
		t.Fatalf("Save over another writer's change = %v, want ErrChangedDuringWrite", err)
	}
	got, _, err := sqlite.New(path).Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Cards) != 1 || got.Cards[0].Title != "From B" {
		t.Fatalf("cards after the refused save = %#v, want only B's card", got.Cards)
	}
}

func TestParallelServicesLoseNothing(t *testing.T) {
	_, path := newDB(t)
	const workers, each = 8, 25
	var wg sync.WaitGroup
	errs := make(chan error, workers*each)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc := app.NewService(sqlite.New(path), func() time.Time { return day })
			for i := 0; i < each; i++ {
				if _, err := svc.CreateCard(context.Background(), domain.NewCard{Title: fmt.Sprintf("w%d-%d", w, i)}); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("CreateCard: %v", err)
	}
	b, _, err := sqlite.New(path).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[domain.CardID]bool{}
	for _, c := range b.Cards {
		seen[c.ID] = true
	}
	if len(b.Cards) != workers*each || len(seen) != workers*each {
		t.Fatalf("%d cards, %d distinct ids; want %d of each", len(b.Cards), len(seen), workers*each)
	}
}

func TestReadOnlyFileGivesPlainError(t *testing.T) {
	r, path := newDB(t)
	b, _, err := r.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	err = r.Save(context.Background(), b)
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("Save to a read-only database = %v, want a message saying read-only", err)
	}
}

func TestReadsDoNotBlockOnWriter(t *testing.T) {
	a, path := newDB(t)
	unlock, err := a.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	b, _, err := sqlite.New(path).Load(ctx)
	if err != nil || b.Settings.Name != "Contract" {
		t.Fatalf("Load while another repo holds the lock = %v, %v", b, err)
	}
}

func TestSaveUnderLockIsAllOrNothing(t *testing.T) {
	r, _ := newDB(t)
	ctx := context.Background()
	unlock, err := r.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	b, _, err := r.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b.AddCard(domain.NewCard{Title: "One"}, day)
	b.Cards = append(b.Cards, b.Cards[0]) // the same id twice cannot be stored
	if err := r.Save(ctx, b); err == nil {
		t.Fatal("Save of a board with a duplicate id succeeded")
	}
	got, _, err := r.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Cards) != 0 {
		t.Fatalf("after the failed save the board holds %d cards, want the 0 it had", len(got.Cards))
	}
}

func TestLockGivesUpWhenSomeoneHoldsTheDatabaseForGood(t *testing.T) {
	r, path := newDB(t)
	old := sqlite.SetLockWaitLimit(300 * time.Millisecond)
	defer sqlite.SetLockWaitLimit(old)

	holder, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	conn, err := holder.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	_, err = r.Lock(context.Background()) // no deadline of its own
	if err == nil || !strings.Contains(err.Error(), "database is busy") || !errors.Is(err, app.ErrBusy) {
		t.Fatalf("Lock = %v, want the busy message wrapping app.ErrBusy", err)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("Lock took %v to give up", took)
	}
}

// A caller that walked away is not a database that is busy.
func TestLockOfACancelledCallIsNotReportedAsBusy(t *testing.T) {
	r, _ := newDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.Lock(ctx)
	if err == nil || errors.Is(err, app.ErrBusy) || strings.Contains(err.Error(), "another program") {
		t.Fatalf("Lock = %v, want a plain cancellation", err)
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("Lock = %v, want it to say the call was cancelled", err)
	}
}
