package projects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"waypoint/internal/config"
	"waypoint/internal/domain"
	"waypoint/internal/storage"
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

func TestAddRefusals(t *testing.T) {
	cfgPath, env := sandbox(t)
	dir := t.TempDir()
	ctx := context.Background()

	_, err := Add(ctx, cfgPath, env, AddOptions{Name: "S", Path: filepath.Join(dir, "s.db"), Prefix: "T"})
	if !errors.Is(err, storage.ErrSQLite) {
		t.Fatalf("sqlite path: %v", err)
	}
	if _, statErr := os.Stat(cfgPath); statErr == nil {
		t.Fatal("a refused add wrote the config")
	}

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
		{"sqlite", config.ProjectRef{Name: "s", Storage: "sqlite", Path: good}, "sqlite"},
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
