package mysql_test

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

	gomysql "github.com/go-sql-driver/mysql"

	"waypoint/internal/app"
	"waypoint/internal/config"
	"waypoint/internal/dbconn"
	"waypoint/internal/domain"
	"waypoint/internal/storage/mysql"
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

func newRepo(t *testing.T) (*mysql.Repo, config.ProjectRef) {
	t.Helper()
	ref := servertest.MySQL(t)
	r, err := mysql.Create(ctx, ref, settings(), false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return r, ref
}

// raw opens a plain connection with the password the test environment holds for
// ref. database "" means no default database.
func raw(t *testing.T, ref config.ProjectRef, database ...string) *sql.DB {
	t.Helper()
	cfg := gomysql.NewConfig()
	cfg.User, cfg.Passwd, cfg.Net = ref.User, dbconn.Password(os.Getenv, ref.Name), "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", ref.Host, ref.Port)
	cfg.DBName = ref.Database
	if len(database) > 0 {
		cfg.DBName = database[0]
	}
	cfg.Collation = "utf8mb4_bin"
	connector, err := gomysql.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
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
	rows, err := db.Query(`SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() ORDER BY 1`)
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
	ref := servertest.MySQL(t)
	db := raw(t, ref)
	mustExec(t, db, `CREATE TABLE precious_things (id int)`)
	mustExec(t, db, `INSERT INTO precious_things VALUES (42)`)
	_, err := mysql.Create(ctx, ref, settings(), false)
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
	ref := servertest.MySQL(t)
	restore := mysql.FailCreateAfterFirstTable()
	_, err := mysql.Create(ctx, ref, settings(), false)
	restore()
	if err == nil {
		t.Fatal("Create should have failed")
	}
	if got := tables(t, raw(t, ref)); len(got) != 0 {
		t.Fatalf("a failed Create left tables behind: %v", got)
	}
}

func TestCreateDatabase(t *testing.T) {
	ref := servertest.MySQL(t)
	admin := raw(t, ref, "")
	ref.Database = fmt.Sprintf("waypoint_made_%d", time.Now().UnixNano())
	t.Cleanup(func() { admin.Exec("DROP DATABASE IF EXISTS `" + ref.Database + "`") })

	_, err := mysql.Create(ctx, ref, settings(), false)
	if err == nil || !strings.Contains(err.Error(), ref.Database) {
		t.Fatalf("Create without the flag = %v, want an error naming the database", err)
	}
	r, err := mysql.Create(ctx, ref, settings(), true)
	if err != nil {
		t.Fatalf("Create with the flag: %v", err)
	}
	if _, _, err := r.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}
	var charset, collation string
	admin.QueryRow(`SELECT default_character_set_name, default_collation_name FROM information_schema.schemata WHERE schema_name = ?`, ref.Database).Scan(&charset, &collation)
	if charset != "utf8mb4" || collation != "utf8mb4_bin" {
		t.Errorf("created database is %s/%s, want utf8mb4/utf8mb4_bin", charset, collation)
	}
}

func TestOpenRefusesForeignDatabase(t *testing.T) {
	ref := servertest.MySQL(t)
	mustExec(t, raw(t, ref), `CREATE TABLE somebody_elses (id int)`)
	_, _, err := mysql.New(ref).Load(ctx)
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Load on a foreign database = %v, want ErrInvalid", err)
	}
	r2, ref2 := newRepo(t)
	mustExec(t, raw(t, ref2), `CREATE TABLE extra (id int)`)
	if _, _, err := r2.Load(ctx); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Load beside a foreign table = %v, want ErrInvalid", err)
	}
}

func TestRefusesNewerSchema(t *testing.T) {
	r, ref := newRepo(t)
	mustExec(t, raw(t, ref), "UPDATE waypoint_meta SET value = '999' WHERE `key` = 'schema_version'")
	_, _, err := r.Load(ctx)
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("Load = %v, want ErrInvalid about a newer Waypoint", err)
	}
}

func TestRefusesRetired(t *testing.T) {
	r, ref := newRepo(t)
	mustExec(t, raw(t, ref), "UPDATE waypoint_meta SET value = 'true' WHERE `key` = 'retired'")
	_, _, err := r.Load(ctx)
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "waypoint_meta") || !strings.Contains(err.Error(), "`key`") {
		t.Fatalf("Load = %v, want ErrInvalid with the MySQL statement that clears the flag", err)
	}
}

func TestStoresNULRoundTrip(t *testing.T) {
	r, _ := newRepo(t)
	b := domain.NewBoard("Contract", "T")
	b.Cards = []domain.Card{{ID: "T-1", Title: "nul\x00title", Body: "bo\x00dy", Column: b.Settings.Columns[0].Name,
		Extra: []domain.Field{{Key: "k", Value: "v\x00v"}}}}
	b.Settings.NextID = 2
	if err := r.Save(ctx, b); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _, err := r.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cards[0].Title != "nul\x00title" || got.Cards[0].Body != "bo\x00dy" || got.Cards[0].Extra[0].Value != "v\x00v" {
		t.Fatalf("NUL did not survive: %+v", got.Cards[0])
	}
}

func TestOddPassword(t *testing.T) {
	ref := servertest.MySQL(t)
	admin := raw(t, ref, "")
	const pw = `p@ss:w/rd?#% '"`
	mustExec(t, admin, "CREATE USER 'odd_user'@'%' IDENTIFIED BY 'p@ss:w/rd?#% \\'\"'")
	mustExec(t, admin, "GRANT ALL ON *.* TO 'odd_user'@'%'")
	t.Cleanup(func() {
		admin.Exec("DROP DATABASE IF EXISTS `1-odd-name`")
		admin.Exec("DROP USER IF EXISTS 'odd_user'@'%'")
	})
	ref.User, ref.Database = "odd_user", "1-odd-name"
	t.Setenv(dbconn.PasswordVar(ref.Name), pw)
	r, err := mysql.Create(ctx, ref, settings(), true)
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
	ref := servertest.MySQL(t)
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
	_, _, err := mysql.New(ref).Load(ctx)
	check("Load", err)
	_, err = mysql.Create(ctx, ref, settings(), false)
	check("Create", err)
	_, err = mysql.Probe(ctx, ref)
	check("Probe", err)
	if err != nil && !strings.Contains(err.Error(), dbconn.PasswordHelp(ref.Name)) {
		t.Errorf("a rejected password should name the variable: %v", err)
	}
}

func hardBoard() (*domain.Board, string) {
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
	return b, big
}

func TestRoundTripHardBoard(t *testing.T) {
	r, _ := newRepo(t)
	b, big := hardBoard()
	if err := r.Save(ctx, b); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _, err := r.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
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
		t.Fatalf("MySQL and SQLite load different boards")
	}
	if len(got.Cards[0].Body) != len(big) {
		t.Errorf("1 MiB body came back as %d bytes", len(got.Cards[0].Body))
	}
}

func TestCaseSensitiveKeys(t *testing.T) {
	r, _ := newRepo(t)
	b, _ := hardBoard() // columns Todo/todo and fields Key/key
	if err := r.Save(ctx, b); err != nil {
		t.Fatalf("Save of case-only twins: %v", err)
	}
	got, _, err := r.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Settings.Columns[0].Name != "Todo" || got.Settings.Columns[1].Name != "todo" {
		t.Errorf("case-only columns were merged: %+v", got.Settings.Columns)
	}
	if f := got.Cards[0].Extra; len(f) != 2 || f[0].Key != "Key" || f[1].Key != "key" {
		t.Errorf("case-only field keys were merged: %+v", f)
	}
	// The group names too.
	b.Settings.Groups = []domain.Group{{Name: "Ops", Color: "blue"}, {Name: "ops", Color: "red"}}
	if err := r.Save(ctx, b); err != nil {
		t.Fatalf("Save of case-only groups: %v", err)
	}
}

func TestTrailingSpacesKept(t *testing.T) {
	r, _ := newRepo(t)
	b := domain.NewBoard("Contract", "T")
	b.Settings.Columns = []domain.Column{{Name: "Todo "}, {Name: "Done", Done: true}}
	b.Cards = []domain.Card{{ID: "T-1", Title: "ends with space ", Column: "Todo ", Body: "body \n"}}
	b.Settings.NextID = 2
	if err := r.Save(ctx, b); err != nil {
		t.Fatal(err)
	}
	got, _, err := r.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Settings.Columns[0].Name != "Todo " || got.Cards[0].Title != "ends with space " || got.Cards[0].Column != "Todo " {
		t.Fatalf("trailing spaces were lost: %q %q %q", got.Settings.Columns[0].Name, got.Cards[0].Title, got.Cards[0].Column)
	}
}

func TestLongNameRefused(t *testing.T) {
	r, _ := newRepo(t)
	long := strings.Repeat("x", 256)
	b := domain.NewBoard("Contract", "T")
	b.Settings.Columns = append(b.Settings.Columns, domain.Column{Name: long})
	err := r.Save(ctx, b)
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "column") || !strings.Contains(err.Error(), "255") {
		t.Fatalf("Save = %v, want ErrInvalid naming the column and the limit", err)
	}
	b = domain.NewBoard("Contract", "T")
	b.Settings.Columns[0].Name = strings.Repeat("x", 255) // exactly the limit is fine
	if err := r.Save(ctx, b); err != nil {
		t.Fatalf("a 255-character name should fit: %v", err)
	}
	// 255 emoji are 255 characters although 1020 bytes.
	b.Settings.Columns[0].Name = strings.Repeat("🎉", 255)
	if err := r.Save(ctx, b); err != nil {
		t.Fatalf("255 multibyte characters should fit: %v", err)
	}
}

func TestMultibyteKeysEmoji(t *testing.T) {
	r, _ := newRepo(t)
	b := domain.NewBoard("Contract", "T")
	b.Settings.Columns = []domain.Column{{Name: "🎉 Party"}, {Name: "日本語", Done: true}}
	b.Settings.Groups = []domain.Group{{Name: "グループ", Color: "blue"}}
	b.Cards = []domain.Card{{ID: "T-1", Title: "🎉", Column: "🎉 Party", Group: "グループ", Extra: []domain.Field{{Key: "ключ", Value: "значение"}}}}
	b.Settings.NextID = 2
	if err := r.Save(ctx, b); err != nil {
		t.Fatal(err)
	}
	got, _, err := r.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cards[0].Column != "🎉 Party" || got.Cards[0].Extra[0].Key != "ключ" || got.Settings.Groups[0].Name != "グループ" {
		t.Fatalf("multibyte text changed: %+v", got.Cards[0])
	}
}

func TestCreateFromReleasesTheBoardOnlyWhenComplete(t *testing.T) {
	ref := servertest.MySQL(t)
	b := domain.NewBoard("From", "T")
	for i := 1; i <= 5; i++ {
		b.Cards = append(b.Cards, domain.Card{ID: domain.CardID(fmt.Sprintf("T-%d", i)), Title: "c", Column: b.Settings.Columns[0].Name})
	}
	b.Settings.NextID = 6
	restore := mysql.FailAfterSave()
	_, err := mysql.CreateFrom(ctx, ref, b, false)
	restore()
	if err == nil {
		t.Fatal("CreateFrom should have failed")
	}
	if got := tables(t, raw(t, ref)); len(got) != 0 {
		t.Fatalf("a failed CreateFrom left %v", got)
	}
	r, err := mysql.CreateFrom(ctx, ref, b, false)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := r.Load(ctx)
	if err != nil || len(got.Cards) != 5 {
		t.Fatalf("Load = %v, %v", got, err)
	}
}
