package cli

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"waypoint/internal/config"
	"waypoint/internal/domain"
	"waypoint/internal/storage"
	"waypoint/internal/watch"
)

func TestServeWatchesPostgres(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		ref := e.fresh(t)
		if _, err := storage.CreateWith(context.Background(), ref, domain.NewBoard("W", "T").Settings, storage.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		sb := sandboxFor(t)
		env := func(k string) string {
			if k == "HOME" {
				return sb.home
			}
			return ""
		}
		if err := config.Update(sb.config, env, func(c *config.Config) error { return c.Add(ref) }); err != nil {
			t.Fatal(err)
		}

		var mu sync.Mutex
		var got []string
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- watch.Run(ctx, watchTargets(sb.config, env, slog.New(slog.NewTextHandler(io.Discard, nil))),
				func(p string) { mu.Lock(); got = append(got, p); mu.Unlock() },
				watch.Options{ConfigPath: sb.config, ServerPoll: 200 * time.Millisecond})
		}()
		defer func() { cancel(); <-done }()
		time.Sleep(1500 * time.Millisecond) // the first poll takes the baseline

		// A save from another Repo, as another process would make it.
		repo, _ := storage.Open(ref)
		b, _, err := repo.Load(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		b.AddCard(domain.NewCard{Title: "from elsewhere"}, time.Now())
		if err := repo.Save(context.Background(), b); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			mu.Lock()
			n := len(got)
			mu.Unlock()
			if n > 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("no publish within 5s of a save from another process")
			}
			time.Sleep(20 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		if got[0] != ref.Name {
			t.Fatalf("published %v, want %s", got, ref.Name)
		}
	})
}
