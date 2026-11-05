package projects

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"waypoint/internal/config"
	"waypoint/internal/domain"
	"waypoint/internal/storage/sqlite"
)

func sandbox(t *testing.T) (cfgPath string, env func(string) string) {
	t.Helper()
	home := t.TempDir()
	cfgPath = filepath.Join(home, "cfg", "config.yaml")
	return cfgPath, func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}
}

func TestAddCreatesBoardAndRegisters(t *testing.T) {
	cfgPath, env := sandbox(t)
	path := filepath.Join(t.TempDir(), "a.md")
	ref, err := Add(context.Background(), cfgPath, env, AddOptions{Name: " Alpha ", Path: path, Prefix: "AL"})
	if err != nil {
		t.Fatal(err)
	}
	if ref.Name != "Alpha" || ref.Path != path || ref.Storage != "markdown" {
		t.Fatalf("ref = %+v", ref)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "prefix: AL") {
		t.Fatalf("board file: %v %q", err, data)
	}
	cfg, err := config.Load(cfgPath, env)
	if err != nil || len(cfg.Projects) != 1 {
		t.Fatalf("config: %v %+v", err, cfg)
	}
}

func TestAddRefusesASharedArchive(t *testing.T) {
	cfgPath, env := sandbox(t)
	dir := t.TempDir()
	ctx := context.Background()
	if _, err := Add(ctx, cfgPath, env, AddOptions{Name: "A", Path: filepath.Join(dir, "x.md"), Prefix: "T"}); err != nil {
		t.Fatal(err)
	}

	// x.markdown would use x.archive.md too, so one project's archive would hold the other's cards.
	twin := filepath.Join(dir, "x.markdown")
	_, err := Add(ctx, cfgPath, env, AddOptions{Name: "B", Path: twin, Prefix: "T"})
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), `shares an archive with "A"`) {
		t.Fatalf("twin: %v", err)
	}
	if _, statErr := os.Stat(twin); statErr == nil {
		t.Fatal("the refused board was created")
	}

	// An archive is not a board, even one that isn't registered yet.
	_, err = Add(ctx, cfgPath, env, AddOptions{Name: "C", Path: filepath.Join(dir, "x.archive.md"), Prefix: "T"})
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "is an archive") {
		t.Fatalf("archive as board: %v", err)
	}
}

func TestAddSeesThroughSymlinksToAnArchive(t *testing.T) {
	cfgPath, env := sandbox(t)
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "linkdir")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := Add(ctx, cfgPath, env, AddOptions{Name: "A", Path: filepath.Join(real, "x.md"), Prefix: "T"}); err != nil {
		t.Fatal(err)
	}
	_, err := Add(ctx, cfgPath, env, AddOptions{Name: "B", Path: filepath.Join(link, "x.markdown"), Prefix: "T"})
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), `shares an archive with "A"`) {
		t.Fatalf("through a symlink: %v", err)
	}
}

func TestArchiveCheckAgainstHandRegisteredEntries(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Projects: []config.ProjectRef{
		{Name: "legacy", Storage: "markdown", Path: filepath.Join(dir, "n.archive.md")},
		{Name: "db", Storage: "sqlite", Path: filepath.Join(dir, "q.db")},
	}}
	// n.md would archive into the file registered as a board.
	if err := checkArchive(cfg, filepath.Join(dir, "n.md")); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("legacy board: %v", err)
	}
	// A database has no archive file, so a board of the same name is fine.
	if err := checkArchive(cfg, filepath.Join(dir, "q.md")); err != nil {
		t.Fatalf("sqlite neighbour: %v", err)
	}
}

func TestAddRefusals(t *testing.T) {
	cfgPath, env := sandbox(t)
	dir := t.TempDir()
	ctx := context.Background()

	var err error
	_, err = Add(ctx, cfgPath, env, AddOptions{Name: "R", Path: "rel/x.md", Prefix: "T"})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("relative path without cwd: %v", err)
	}

	real := filepath.Join(dir, "real.md")
	if _, err := Add(ctx, cfgPath, env, AddOptions{Name: "One", Path: real, Prefix: "T"}); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	_, err = Add(ctx, cfgPath, env, AddOptions{Name: "Two", Path: link, Prefix: "T"})
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), `already registered as "One"`) {
		t.Fatalf("same file through a symlink: %v", err)
	}
}

func TestInspect(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.md")
	cfgPath, env := sandbox(t)
	if _, err := Add(context.Background(), cfgPath, env, AddOptions{Name: "G", Path: good, Prefix: "T"}); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "bad.md")
	os.WriteFile(bad, []byte("hello\n"), 0o644)

	cases := []struct {
		name string
		ref  config.ProjectRef
		want string // "" means no error
	}{
		{"ok", config.ProjectRef{Name: "g", Storage: "markdown", Path: good}, ""},
		{"missing", config.ProjectRef{Name: "m", Storage: "markdown", Path: filepath.Join(dir, "gone.md")}, "File not found at " + filepath.Join(dir, "gone.md")},
		{"relative", config.ProjectRef{Name: "r", Storage: "markdown", Path: "rel.md"}, "not absolute"},
		{"unknown storage", config.ProjectRef{Name: "u", Storage: "mongo", Path: good}, `Unknown storage "mongo"`},
		{"sqlite", config.ProjectRef{Name: "s", Storage: "sqlite", Path: good}, "SQLite"},
		{"directory", config.ProjectRef{Name: "d", Storage: "markdown", Path: dir}, "is a directory"},
		{"not a board", config.ProjectRef{Name: "b", Storage: "markdown", Path: bad}, "not a Waypoint board"},
	}
	for _, c := range cases {
		b, err := Inspect(context.Background(), c.ref)
		switch {
		case c.want == "" && (err != nil || b.Settings.Name != "G"):
			t.Errorf("%s: unexpected error %v", c.name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: error %v, want it to contain %q", c.name, err, c.want)
		}
	}
}

func TestAddAppliesNoDefaultPrefix(t *testing.T) {
	cfgPath, env := sandbox(t)
	path := filepath.Join(t.TempDir(), "a.md")
	_, err := Add(context.Background(), cfgPath, env, AddOptions{Name: "A", Path: path})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("empty prefix: %v", err)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Fatal("a refused add created the board")
	}
}

func TestValidateKinds(t *testing.T) {
	dir := t.TempDir()
	err := Validate(config.ProjectRef{Name: "d", Storage: "markdown", Path: dir})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("directory: %v", err)
	}
	err = Validate(config.ProjectRef{Name: "m", Storage: "markdown", Path: filepath.Join(dir, "gone.md")})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestAddRefusesDotNames(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	env := func(k string) string { return map[string]string{"HOME": dir}[k] }
	for _, name := range []string{".", "..", " .. "} {
		_, err := Add(context.Background(), cfg, env, AddOptions{Name: name, Path: filepath.Join(dir, "b.md"), Prefix: "T"})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("%q: got %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "b.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a board was created: %v", err)
	}
}

func TestAddSQLiteProject(t *testing.T) {
	cfgPath, env := sandbox(t)
	path := filepath.Join(t.TempDir(), "x.db")
	ref, err := Add(context.Background(), cfgPath, env, AddOptions{Name: "Lite", Path: path, Prefix: "L"})
	if err != nil {
		t.Fatal(err)
	}
	if ref.Storage != "sqlite" || ref.Path != path {
		t.Fatalf("ref = %+v, want sqlite at %s", ref, path)
	}
	b, err := Inspect(context.Background(), ref)
	if err != nil || b.Settings.Name != "Lite" || b.Settings.Prefix != "L" {
		t.Fatalf("Inspect = %+v, %v", b, err)
	}
}

func TestAddExistingDatabaseOpensWithoutRewriting(t *testing.T) {
	cfgPath, env := sandbox(t)
	path := filepath.Join(t.TempDir(), "kept.data") // the name does not say what it is
	if _, err := sqlite.Create(path, domain.NewBoard("Kept", "K").Settings); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	ref, err := Add(context.Background(), cfgPath, env, AddOptions{Name: "Kept", Path: path, Prefix: "T"})
	if err != nil {
		t.Fatal(err)
	}
	if ref.Storage != "sqlite" {
		t.Fatalf("storage = %q, want sqlite (the content decides, not the name)", ref.Storage)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("registering an existing database changed it")
	}
}

func TestAddForeignDatabaseRefused(t *testing.T) {
	cfgPath, env := sandbox(t)
	path := filepath.Join(t.TempDir(), "other.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE notes(x)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	_, err = Add(context.Background(), cfgPath, env, AddOptions{Name: "Other", Path: path, Prefix: "T"})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Add of a foreign database = %v, want domain.ErrInvalid", err)
	}
	if _, statErr := os.Stat(cfgPath); statErr == nil {
		t.Fatal("a refused add wrote the config")
	}
}

func TestValidateAcceptsSQLiteEntry(t *testing.T) {
	missing := config.ProjectRef{Name: "L", Storage: "sqlite", Path: filepath.Join(t.TempDir(), "gone.db")}
	if err := Validate(missing); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Validate of a missing database = %v, want domain.ErrNotFound", err)
	}
}
