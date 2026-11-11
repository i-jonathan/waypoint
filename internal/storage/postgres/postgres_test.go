package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"waypoint/internal/app"
	"waypoint/internal/config"
	"waypoint/internal/dbconn"
	"waypoint/internal/domain"
	"waypoint/internal/storage/postgres"
	"waypoint/internal/storage/servertest"
	"waypoint/internal/storage/sqlite"
	"waypoint/internal/storage/storagetest"
)

var ctx = context.Background()

func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}

func contextWithCancel() (context.Context, context.CancelFunc) { return context.WithCancel(ctx) }

func settings() domain.Settings { return domain.NewBoard("Contract", "T").Settings }

func newRepo(t *testing.T) (*postgres.Repo, config.ProjectRef) {
	t.Helper()
	ref := servertest.Postgres(t)
	r, err := postgres.Create(ctx, ref, settings(), false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return r, ref
}

// raw opens a plain connection to ref's database (or to database, when given),
// with the password the test environment holds for ref.
func raw(t *testing.T, ref config.ProjectRef, database ...string) *sql.DB {
	t.Helper()
	cfg, err := pgx.ParseConfig("")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Host, cfg.Port, cfg.User = ref.Host, uint16(ref.Port), ref.User
	cfg.Password = dbconn.Password(os.Getenv, ref.Name)
	cfg.Database = ref.Database
	if len(database) > 0 {
		cfg.Database = database[0]
	}
	db := sql.OpenDB(stdlib.GetConnector(*cfg))
	t.Cleanup(func() { db.Close() })
	return db
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func tables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind IN ('r','p','v','m','f','S') ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func TestContract(t *testing.T) {
	storagetest.RunContract(t, func(t *testing.T) app.Repository {
		r, _ := newRepo(t)
		return r
	})
}

func TestPathHasNoUserOrPassword(t *testing.T) {
	r, ref := newRepo(t)
	want := fmt.Sprintf("%s:%d/%s", ref.Host, ref.Port, ref.Database)
	if r.Path() != want {
		t.Fatalf("Path = %q, want %q", r.Path(), want)
	}
}

func TestCreateRefusesNonEmpty(t *testing.T) {
	ref := servertest.Postgres(t)
	db := raw(t, ref)
	mustExec(t, db, `CREATE TABLE precious_things (id int)`)
	mustExec(t, db, `INSERT INTO precious_things VALUES (42)`)
	_, err := postgres.Create(ctx, ref, settings(), false)
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "precious_things") {
		t.Fatalf("Create = %v, want ErrInvalid naming precious_things", err)
	}
	if got := tables(t, db); !reflect.DeepEqual(got, []string{"precious_things"}) {
		t.Fatalf("tables after refused Create = %v", got)
	}
	var n int
	db.QueryRow(`SELECT id FROM precious_things`).Scan(&n)
	if n != 42 {
		t.Fatal("existing data was touched")
	}
}

func TestCreateFailureLeavesNothing(t *testing.T) {
	ref := servertest.Postgres(t)
	restore := postgres.FailCreateAfterFirstTable()
	_, err := postgres.Create(ctx, ref, settings(), false)
	restore()
	if err == nil {
		t.Fatal("Create should have failed")
	}
	if got := tables(t, raw(t, ref)); len(got) != 0 {
		t.Fatalf("a failed Create left tables behind: %v", got)
	}
}

func TestCreateDatabase(t *testing.T) {
	ref := servertest.Postgres(t)
	admin := raw(t, ref, "postgres")
	ref.Database = "waypoint_made_" + strings.ToLower(fmt.Sprint(time.Now().UnixNano()))
	t.Cleanup(func() { admin.Exec(`DROP DATABASE IF EXISTS "` + ref.Database + `" WITH (FORCE)`) })

	_, err := postgres.Create(ctx, ref, settings(), false)
	if err == nil || !strings.Contains(err.Error(), ref.Database) {
		t.Fatalf("Create without the flag = %v, want an error naming the database", err)
	}
	r, err := postgres.Create(ctx, ref, settings(), true)
	if err != nil {
		t.Fatalf("Create with the flag: %v", err)
	}
	if _, _, err := r.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestOpenRefusesForeignDatabase(t *testing.T) {
	ref := servertest.Postgres(t)
	mustExec(t, raw(t, ref), `CREATE TABLE somebody_elses (id int)`)
	_, _, err := postgres.New(ref).Load(ctx)
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Load on a foreign database = %v, want ErrInvalid", err)
	}
	// Even a Waypoint board with a foreign table beside it is refused.
	r2, ref2 := newRepo(t)
	mustExec(t, raw(t, ref2), `CREATE TABLE extra (id int)`)
	if _, _, err := r2.Load(ctx); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Load beside a foreign table = %v, want ErrInvalid", err)
	}
}

func TestRefusesNewerSchema(t *testing.T) {
	r, ref := newRepo(t)
	mustExec(t, raw(t, ref), `UPDATE waypoint_meta SET value = '999' WHERE key = 'schema_version'`)
	_, _, err := r.Load(ctx)
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("Load = %v, want ErrInvalid about a newer Waypoint", err)
	}
}

func TestRefusesRetired(t *testing.T) {
	r, ref := newRepo(t)
	mustExec(t, raw(t, ref), `UPDATE waypoint_meta SET value = 'true' WHERE key = 'retired'`)
	_, _, err := r.Load(ctx)
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "waypoint_meta") {
		t.Fatalf("Load = %v, want ErrInvalid with the SQL that clears the flag", err)
	}
}

func TestRejectsNUL(t *testing.T) {
	cases := map[string]func(b *domain.Board){
		"title":  func(b *domain.Board) { b.Cards[0].Title = "bad\x00title" },
		"body":   func(b *domain.Board) { b.Cards[0].Body = "bad\x00body" },
		"field":  func(b *domain.Board) { b.Cards[0].Extra = []domain.Field{{Key: "k\x00ey", Value: "v"}} },
		"column": func(b *domain.Board) { b.Settings.Columns[0].Name = "col\x00umn" },
		"utf8":   func(b *domain.Board) { b.Cards[0].Body = "caf\xe9" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r, _ := newRepo(t)
			b := domain.NewBoard("Contract", "T")
			b.Cards = []domain.Card{{ID: "T-1", Title: "ok", Column: b.Settings.Columns[0].Name}}
			b.Settings.NextID = 2
			if err := r.Save(ctx, b); err != nil {
				t.Fatal(err)
			}
			if _, _, err := r.Load(ctx); err != nil {
				t.Fatal(err)
			}
			mutate(b)
			err := r.Save(ctx, b)
			if !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("Save = %v, want ErrInvalid", err)
			}
			if name != "column" && !strings.Contains(err.Error(), "T-1") {
				t.Errorf("error should name the card: %v", err)
			}
			if name == "field" && !strings.Contains(err.Error(), "field") {
				t.Errorf("error should name the field: %v", err)
			}
			got, _, err := r.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Cards) != 1 || got.Cards[0].Title != "ok" || got.Cards[0].Body != "" {
				t.Errorf("a refused save changed the board: %+v", got.Cards)
			}
		})
	}
}

func TestOddPassword(t *testing.T) {
	ref := servertest.Postgres(t)
	admin := raw(t, ref, "postgres")
	const pw = `p@ss:w/rd?#% '"`
	mustExec(t, admin, `CREATE ROLE odd_role LOGIN CREATEDB PASSWORD 'p@ss:w/rd?#% ''"'`)
	t.Cleanup(func() {
		admin.Exec(`DROP DATABASE IF EXISTS "1-odd-name" WITH (FORCE)`)
		admin.Exec(`DROP ROLE IF EXISTS odd_role`)
	})
	ref.User, ref.Database = "odd_role", "1-odd-name"
	t.Setenv(dbconn.PasswordVar(ref.Name), pw)
	r, err := postgres.Create(ctx, ref, settings(), true)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	b, _, err := r.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	b.Cards = []domain.Card{{ID: "T-1", Title: "hello", Column: b.Settings.Columns[0].Name}}
	b.Settings.NextID = 2
	if err := r.Save(ctx, b); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func TestPasswordNeverInErrors(t *testing.T) {
	ref := servertest.Postgres(t)
	const marker = "s3cr3t-marker"
	t.Setenv(dbconn.PasswordVar(ref.Name), marker)
	check := func(what string, err error) {
		t.Helper()
		if err == nil {
			t.Errorf("%s: expected an error with the wrong password", what)
			return
		}
		if strings.Contains(err.Error(), marker) {
			t.Errorf("%s: error contains the password: %v", what, err)
		}
	}
	_, _, err := postgres.New(ref).Load(ctx)
	check("Load", err)
	_, err = postgres.Create(ctx, ref, settings(), false)
	check("Create", err)
	_, err = postgres.Probe(ctx, ref)
	check("Probe", err)
	if err != nil && !strings.Contains(err.Error(), dbconn.PasswordVar(ref.Name)) {
		t.Errorf("a rejected password should name the variable: %v", err)
	}
}

func TestRoundTripHardBoard(t *testing.T) {
	r, _ := newRepo(t)
	b := domain.NewBoard("Hard", "T")
	cols := []domain.Column{{Name: "Todo"}, {Name: "todo"}, {Name: "Done", Done: true}}
	b.Settings.Columns = cols
	stamp := time.Date(2026, 11, 1, 9, 30, 0, 0, time.UTC)
	big := strings.Repeat("0123456789abcdef", 64*1024) // 1 MiB
	bodies := []string{big, "emoji 🎉👩‍👩‍👧 and \"quotes\" and 'single' \\ back", "line one\r\nline two\r\n", "日本語のテキスト"}
	for i, body := range bodies {
		b.Cards = append(b.Cards, domain.Card{
			ID: domain.CardID(fmt.Sprintf("T-%d", i+1)), Title: fmt.Sprintf("card %d 🎉", i+1), Column: cols[i%2].Name,
			Created: stamp, Body: body, Extra: []domain.Field{{Key: "Key", Value: "v"}, {Key: "key", Value: "w"}},
		})
	}
	b.Settings.NextID = 5

	if err := r.Save(ctx, b); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _, err := r.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// The reference: whatever SQLite makes of the same board.
	sq, err := sqlite.Create(filepath.Join(t.TempDir(), "ref.db"), settings())
	if err != nil {
		t.Fatal(err)
	}
	if err := sq.Save(ctx, b); err != nil {
		t.Fatal(err)
	}
	want, _, err := sq.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Postgres and SQLite load different boards")
	}
	if got.Settings.Columns[0].Name != "Todo" || got.Settings.Columns[1].Name != "todo" {
		t.Errorf("case-only columns were merged: %+v", got.Settings.Columns)
	}
	if len(got.Cards[0].Body) != len(big) {
		t.Errorf("1 MiB body came back as %d bytes", len(got.Cards[0].Body))
	}
}

func TestCreateFromWritesTheCardsInTheSameTransaction(t *testing.T) {
	ref := servertest.Postgres(t)
	b, _ := hardBoardForCreate()
	restore := postgres.FailAfterSave()
	_, err := postgres.CreateFrom(ctx, ref, b, false)
	restore()
	if err == nil {
		t.Fatal("CreateFrom should have failed")
	}
	if got := tables(t, raw(t, ref)); len(got) != 0 {
		t.Fatalf("a failure after the cards were written left %v: a half-made board must not exist", got)
	}
	r, err := postgres.CreateFrom(ctx, ref, b, false)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := r.Load(ctx)
	if err != nil || len(got.Cards) != len(b.Cards) {
		t.Fatalf("Load = %v, %v", got, err)
	}
}

func hardBoardForCreate() (*domain.Board, string) {
	b := domain.NewBoard("From", "T")
	for i := 1; i <= 5; i++ {
		b.Cards = append(b.Cards, domain.Card{ID: domain.CardID(fmt.Sprintf("T-%d", i)), Title: "c", Column: b.Settings.Columns[0].Name})
	}
	b.Settings.NextID = 6
	return b, ""
}
