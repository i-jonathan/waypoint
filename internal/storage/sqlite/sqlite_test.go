package sqlite_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"waypoint/internal/app"
	"waypoint/internal/domain"
	"waypoint/internal/storage/sqlite"
	"waypoint/internal/storage/storagetest"
)

func newDB(t *testing.T) (*sqlite.Repo, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "b.db")
	r, err := sqlite.Create(path, domain.NewBoard("Contract", "T").Settings)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return r, path
}

func TestSQLiteRepoContract(t *testing.T) {
	storagetest.RunContract(t, func(t *testing.T) app.Repository {
		r, _ := newDB(t)
		return r
	})
}

func sum(t *testing.T, path string) [32]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(data)
}

func TestCreateRefusesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b.db")
	if err := os.WriteFile(path, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := sum(t, path)
	_, err := sqlite.Create(path, domain.NewBoard("X", "T").Settings)
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("Create over an existing file = %v, want os.ErrExist", err)
	}
	if sum(t, path) != before {
		t.Fatal("Create changed the existing file")
	}
}

func TestLoadMissingFileIsNotCreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.db")
	_, _, err := sqlite.New(path).Load(context.Background())
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load of a missing file = %v, want os.ErrNotExist", err)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Fatal("Load created the file")
	}
}

func execOn(t *testing.T, path string, stmts ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

func TestOpenForeignDatabaseUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "other.db")
	execOn(t, path, "CREATE TABLE notes(x)", "PRAGMA journal_mode=DELETE")
	before := sum(t, path)
	r := sqlite.New(path)
	if _, _, err := r.Load(context.Background()); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Load = %v, want domain.ErrInvalid", err)
	}
	if err := r.Save(context.Background(), domain.NewBoard("X", "T")); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Save = %v, want domain.ErrInvalid", err)
	}
	if sum(t, path) != before {
		t.Fatal("a foreign database was changed")
	}
}

func TestOpenEmptyFileRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.db")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := sqlite.New(path).Load(context.Background()); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Load of an empty file = %v, want domain.ErrInvalid", err)
	}
	if info, _ := os.Stat(path); info.Size() != 0 {
		t.Fatal("an empty file was written to")
	}
}

func TestOpenNewerSchemaRefused(t *testing.T) {
	_, path := newDB(t)
	execOn(t, path, "PRAGMA user_version = 999")
	_, _, err := sqlite.New(path).Load(context.Background())
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("Load = %v, want an ErrInvalid that says the database is from a newer Waypoint", err)
	}
}

func TestLongAndUnicodeText(t *testing.T) {
	r, _ := newDB(t)
	ctx := context.Background()
	b, _, err := r.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b.Settings.Groups = []domain.Group{{Name: "Café & crème", Color: "blue"}}
	body := strings.Repeat("line of text ✓\n", 70000) + "```\n### T-1 not a card\n```"
	if _, err := b.AddCard(domain.NewCard{Title: "Ünïcode — 日本語", Group: "Café & crème", Body: body}, time.Date(2026, 11, 5, 9, 30, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(ctx, b); err != nil {
		t.Fatal(err)
	}
	got, _, err := r.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(b) {
		t.Fatalf("board changed in the round trip; body %d bytes, got %d", len(body), len(got.Cards[0].Body))
	}
}

// A database edited by hand can hold a card number that, plus one, overflows.
// The counter must stay a valid number.
func TestLoadKeepsTheCounterValidForAHugeCardNumber(t *testing.T) {
	r, path := newDB(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO cards (id, position, column_name, title, group_name, priority, depends, created, done, body, archived_at)
		VALUES ('T-9223372036854775807', 0, 'Todo', 'huge', '', '', '[]', '', '', '', NULL)`); err != nil {
		t.Fatal(err)
	}
	b, _, err := r.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if b.Settings.NextID < 1 || b.Settings.NextID > domain.MaxCardNumber {
		t.Fatalf("NextID = %d after a huge card number, want a valid number", b.Settings.NextID)
	}
}
