package watch

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"waypoint/internal/app"
	"waypoint/internal/domain"
	"waypoint/internal/storage/sqlite"
)

func newDatabase(t *testing.T) (string, *app.Service) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "board.db")
	if _, err := sqlite.Create(path, domain.NewBoard("Lite", "T").Settings); err != nil {
		t.Fatal(err)
	}
	return path, app.NewService(sqlite.New(path), time.Now)
}

func addCard(t *testing.T, svc *app.Service, title string) {
	t.Helper()
	if _, err := svc.CreateCard(context.Background(), domain.NewCard{Title: title}); err != nil {
		t.Fatal(err)
	}
}

func TestWatchNoticesSQLiteWrite(t *testing.T) {
	path, svc := newDatabase(t)
	rec := startWith(t, Options{}, hooks{}, Target{"Lite", path})
	seesNext(t, rec, "a card added through the service", func() { addCard(t, svc, "one") })
}

// While another program keeps the database open, a save stays in the -wal file
// and the database file itself does not change. The poll must still see it.
func TestPollNoticesWriteThatStaysInTheWAL(t *testing.T) {
	path, svc := newDatabase(t)
	other, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := other.Ping(); err != nil {
		t.Fatal(err)
	}
	rec := startWith(t, Options{}, hooks{dropEvents: true}, Target{"Lite", path})
	seesNext(t, rec, "a save that stays in the WAL", func() { addCard(t, svc, "one") })
}
