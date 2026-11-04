package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"waypoint/internal/domain"
)

func assertEqual(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func assertNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("error %v, want it to wrap %v", err, target)
	}
}

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDefaultPath(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"explicit wins", map[string]string{"WAYPOINT_CONFIG": "/x/c.yaml", "XDG_CONFIG_HOME": "/xdg", "HOME": "/h"}, "/x/c.yaml"},
		{"xdg next", map[string]string{"XDG_CONFIG_HOME": "/xdg", "HOME": "/h"}, "/xdg/waypoint/config.yaml"},
		{"home last", map[string]string{"HOME": "/h"}, "/h/.config/waypoint/config.yaml"},
	}
	for _, c := range cases {
		got, err := DefaultPath(envOf(c.env))
		assertNoErr(t, err)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	if _, err := DefaultPath(envOf(nil)); err == nil {
		t.Error("no HOME and nothing else set should be an error")
	}
}

func TestLoadMissingGivesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.yaml")
	c, err := Load(path, envOf(map[string]string{"HOME": "/h"}))
	assertNoErr(t, err)
	if c.Port != 4747 || !c.OpenBrowser {
		t.Fatalf("defaults wrong: %+v", c)
	}
	if !strings.HasSuffix(c.DataDir, ".local/share/waypoint") || c.DataDir != "/h/.local/share/waypoint" {
		t.Fatalf("DataDir = %q", c.DataDir)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub")); !os.IsNotExist(err) {
		t.Fatalf("Load must not create anything, stat err = %v", err)
	}
}

func TestLoadPartialKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	assertNoErr(t, os.WriteFile(path, []byte("port: 9000\n"), 0o644))
	c, err := Load(path, envOf(map[string]string{"HOME": "/h"}))
	assertNoErr(t, err)
	if c.Port != 9000 || !c.OpenBrowser || c.DataDir != "/h/.local/share/waypoint" {
		t.Fatalf("got %+v", c)
	}
}

func TestLoadOpenBrowserFalseSticks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	assertNoErr(t, os.WriteFile(path, []byte("open_browser: false\n"), 0o644))
	c, err := Load(path, envOf(map[string]string{"HOME": "/h"}))
	assertNoErr(t, err)
	if c.OpenBrowser {
		t.Fatal("open_browser: false was overridden by the default")
	}
}

func TestLoadEmptyFileGivesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	assertNoErr(t, os.WriteFile(path, nil, 0o644))
	c, err := Load(path, envOf(map[string]string{"HOME": "/h"}))
	assertNoErr(t, err)
	if c.Port != 4747 || !c.OpenBrowser {
		t.Fatalf("got %+v", c)
	}
}

func TestLoadInvalidYAMLNamesFileAndIsKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	bad := "port: [unclosed\n"
	assertNoErr(t, os.WriteFile(path, []byte(bad), 0o644))
	_, err := Load(path, envOf(map[string]string{"HOME": "/h"}))
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("want error naming %s, got %v", path, err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != bad {
		t.Fatalf("invalid file was modified: %q", got)
	}
}

func TestLoadUnknownKeyIsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	assertNoErr(t, os.WriteFile(path, []byte("prot: 1\n"), 0o644))
	_, err := Load(path, envOf(map[string]string{"HOME": "/h"}))
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "prot") {
		t.Fatalf("want error naming file and key, got %v", err)
	}
}

func TestLoadExpandsTilde(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	body := "data_dir: ~/data\nprojects:\n  - name: a\n    storage: markdown\n    path: ~/p/BOARD.md\n"
	assertNoErr(t, os.WriteFile(path, []byte(body), 0o644))
	c, err := Load(path, envOf(map[string]string{"HOME": "/h"}))
	assertNoErr(t, err)
	if c.DataDir != "/h/data" {
		t.Errorf("DataDir = %q", c.DataDir)
	}
	if c.Projects[0].Path != "/h/p/BOARD.md" {
		t.Errorf("Path = %q", c.Projects[0].Path)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "waypoint", "config.yaml")
	env := envOf(map[string]string{"HOME": "/h"})
	c, err := Load(path, env)
	assertNoErr(t, err)
	c.Port = 5000
	c.OpenBrowser = false
	c.Appearance = domain.Appearance{Theme: "dark", Accent: "#7c3aed"}
	assertNoErr(t, c.Add(ProjectRef{Name: "pharmacy", Storage: "markdown", Path: "/p/pharmacy/BOARD.md"}))
	assertNoErr(t, c.Save(path))

	info, err := os.Stat(path)
	assertNoErr(t, err)
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("temp file left behind: %v", entries)
	}

	got, err := Load(path, env)
	assertNoErr(t, err)
	assertEqual(t, got, c)
}

func TestSaveOmitsEmptyAppearance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	c, err := Load(path, envOf(map[string]string{"HOME": "/h"}))
	assertNoErr(t, err)
	assertNoErr(t, c.Save(path))
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "theme") || strings.Contains(string(raw), "accent") {
		t.Errorf("empty appearance fields written:\n%s", raw)
	}
}

func TestAddDuplicate(t *testing.T) {
	c := &Config{}
	assertNoErr(t, c.Add(ProjectRef{Name: "Pharmacy", Storage: "markdown", Path: "/p/pharmacy/BOARD.md"}))
	assertIs(t, c.Add(ProjectRef{Name: "pharmacy", Storage: "markdown", Path: "/q/BOARD.md"}), domain.ErrInvalid)
	assertIs(t, c.Add(ProjectRef{Name: "other", Storage: "markdown", Path: "/p/pharmacy/../pharmacy/BOARD.md"}), domain.ErrInvalid)
	assertIs(t, c.Add(ProjectRef{Name: "", Storage: "markdown", Path: "/z/BOARD.md"}), domain.ErrInvalid)
	assertEqual(t, len(c.Projects), 1)
}

func TestAddMakesPathAbsoluteAndClean(t *testing.T) {
	c := &Config{}
	assertNoErr(t, c.Add(ProjectRef{Name: "a", Storage: "markdown", Path: "/p/x/../a//BOARD.md"}))
	assertEqual(t, c.Projects[0].Path, "/p/a/BOARD.md")

	assertNoErr(t, c.Add(ProjectRef{Name: "rel", Storage: "markdown", Path: "rel/BOARD.md"}))
	if !filepath.IsAbs(c.Projects[1].Path) {
		t.Errorf("relative path not made absolute: %q", c.Projects[1].Path)
	}
}

func TestRemoveAndFind(t *testing.T) {
	c := &Config{}
	assertNoErr(t, c.Add(ProjectRef{Name: "Pharmacy", Storage: "markdown", Path: "/p/pharmacy/BOARD.md"}))
	ref, ok := c.Find("PHARMACY")
	if !ok || ref.Name != "Pharmacy" {
		t.Fatalf("Find = %+v, %v", ref, ok)
	}
	if _, ok := c.Find("nope"); ok {
		t.Fatal("Find unknown should be false")
	}
	assertIs(t, c.Remove("nope"), domain.ErrNotFound)
	assertNoErr(t, c.Remove("pharmacy"))
	assertEqual(t, len(c.Projects), 0)
}

func TestFindByDir(t *testing.T) {
	c := &Config{}
	assertNoErr(t, c.Add(ProjectRef{Name: "pharmacy", Storage: "markdown", Path: "/p/pharmacy/BOARD.md"}))
	assertNoErr(t, c.Add(ProjectRef{Name: "sub", Storage: "markdown", Path: "/p/pharmacy/sub/x.md"}))

	cases := []struct {
		cwd  string
		want string
	}{
		{"/p/pharmacy/backend", "pharmacy"},
		{"/p/pharmacy", "pharmacy"},
		{"/p/pharmacy/sub/deep", "sub"},
		{"/p/pharmacy/sub", "sub"},
		{"/p", ""},
		{"/elsewhere", ""},
	}
	for _, tc := range cases {
		ref, ok := c.FindByDir(tc.cwd)
		if tc.want == "" {
			if ok {
				t.Errorf("cwd %s: got %q, want none", tc.cwd, ref.Name)
			}
			continue
		}
		if !ok || ref.Name != tc.want {
			t.Errorf("cwd %s: got %q (%v), want %q", tc.cwd, ref.Name, ok, tc.want)
		}
	}
}

func TestFindByDirSingleProjectBelowCwd(t *testing.T) {
	c := &Config{}
	assertNoErr(t, c.Add(ProjectRef{Name: "only", Storage: "markdown", Path: "/p/only/BOARD.md"}))
	ref, ok := c.FindByDir("/p")
	if !ok || ref.Name != "only" {
		t.Fatalf("got %+v, %v", ref, ok)
	}
}

func TestFindByDirComparesSegmentsNotPrefixes(t *testing.T) {
	c := &Config{}
	assertNoErr(t, c.Add(ProjectRef{Name: "pharmacy", Storage: "markdown", Path: "/p/pharmacy/BOARD.md"}))
	if ref, ok := c.FindByDir("/p/pharmacy2"); ok {
		t.Fatalf("/p/pharmacy2 matched %q", ref.Name)
	}
	if ref, ok := c.FindByDir("/p/pharmacy2/src"); ok {
		t.Fatalf("/p/pharmacy2/src matched %q", ref.Name)
	}
	// Below-cwd direction: cwd /p/pharm must not claim /p/pharmacy.
	if ref, ok := c.FindByDir("/p/pharm"); ok {
		t.Fatalf("/p/pharm matched %q", ref.Name)
	}
}

func TestFindByDirThroughSymlink(t *testing.T) {
	real := t.TempDir()
	if r, err := filepath.EvalSymlinks(real); err == nil {
		real = r
	}
	proj := filepath.Join(real, "proj")
	assertNoErr(t, os.MkdirAll(filepath.Join(proj, "src"), 0o755))
	link := filepath.Join(t.TempDir(), "link")
	assertNoErr(t, os.Symlink(real, link))

	c := &Config{}
	assertNoErr(t, c.Add(ProjectRef{Name: "p", Storage: "markdown", Path: filepath.Join(proj, "BOARD.md")}))

	// cwd reached through the symlink, project registered by real path.
	ref, ok := c.FindByDir(filepath.Join(link, "proj", "src"))
	if !ok || ref.Name != "p" {
		t.Fatalf("symlinked cwd: got %+v, %v", ref, ok)
	}

	// Project registered through the symlink, cwd by real path.
	c2 := &Config{}
	assertNoErr(t, c2.Add(ProjectRef{Name: "q", Storage: "markdown", Path: filepath.Join(link, "proj", "BOARD.md")}))
	ref, ok = c2.FindByDir(filepath.Join(proj, "src"))
	if !ok || ref.Name != "q" {
		t.Fatalf("symlinked project path: got %+v, %v", ref, ok)
	}
}

func TestStorageFor(t *testing.T) {
	cases := map[string]string{
		"/a/b.db": "sqlite", "/a/b.sqlite": "sqlite", "/a/b.sqlite3": "sqlite",
		"/a/B.DB": "sqlite", "/a/BOARD.md": "markdown", "/a/noext": "markdown",
	}
	for p, want := range cases {
		if got := StorageFor(p); got != want {
			t.Errorf("StorageFor(%q) = %q, want %q", p, got, want)
		}
	}
}

func TestFindByDirTieIsAmbiguous(t *testing.T) {
	c := &Config{}
	assertNoErr(t, c.Add(ProjectRef{Name: "one", Storage: "markdown", Path: "/p/a/one.md"}))
	assertNoErr(t, c.Add(ProjectRef{Name: "two", Storage: "markdown", Path: "/p/a/two.md"}))
	for _, cwd := range []string{"/p/a", "/p/a/x"} {
		if ref, ok := c.FindByDir(cwd); ok {
			t.Errorf("cwd %s: tie matched %q, want none", cwd, ref.Name)
		}
	}

	// A shallower project must not win when the deepest level is tied.
	assertNoErr(t, c.Add(ProjectRef{Name: "top", Storage: "markdown", Path: "/p/top.md"}))
	for _, cwd := range []string{"/p/a", "/p/a/x"} {
		if ref, ok := c.FindByDir(cwd); ok {
			t.Errorf("cwd %s with shallower project: tie fell through to %q, want none", cwd, ref.Name)
		}
	}
	// Outside the tied directory the shallower project still matches.
	if ref, ok := c.FindByDir("/p/b"); !ok || ref.Name != "top" {
		t.Errorf("cwd /p/b: got %+v, %v, want top", ref, ok)
	}
}

func TestAddValidatesPathAndStorage(t *testing.T) {
	c := &Config{}
	assertIs(t, c.Add(ProjectRef{Name: "a", Storage: "markdown", Path: ""}), domain.ErrInvalid)
	assertIs(t, c.Add(ProjectRef{Name: "a", Storage: "postgres", Path: "/p/a.md"}), domain.ErrInvalid)
	assertIs(t, c.Add(ProjectRef{Name: "a", Storage: "Markdown", Path: "/p/a.md"}), domain.ErrInvalid)
	assertEqual(t, len(c.Projects), 0)

	assertNoErr(t, c.Add(ProjectRef{Name: "md", Path: "/p/a/BOARD.md"}))
	assertNoErr(t, c.Add(ProjectRef{Name: "db", Path: "/p/b/board.sqlite3"}))
	assertNoErr(t, c.Add(ProjectRef{Name: "explicit", Storage: "sqlite", Path: "/p/c/data.bin"}))
	assertEqual(t, c.Projects[0].Storage, "markdown")
	assertEqual(t, c.Projects[1].Storage, "sqlite")
	assertEqual(t, c.Projects[2].Storage, "sqlite")
}

func TestUpdateCreatesTheConfigAndItsDirectory(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "new", "dir", "config.yaml")
	env := envOf(map[string]string{"HOME": home})

	err := Update(path, env, func(c *Config) error {
		return c.Add(ProjectRef{Name: "A", Path: filepath.Join(home, "a.md")})
	})

	assertNoErr(t, err)
	c, err := Load(path, env)
	assertNoErr(t, err)
	assertEqual(t, len(c.Projects), 1)
}

func TestUpdateSavesNothingWhenFnFails(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	boom := errors.New("boom")

	err := Update(path, envOf(map[string]string{"HOME": home}), func(c *Config) error {
		c.Port = 1
		return boom
	})

	assertIs(t, err, boom)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the config was written: %v", err)
	}
}

func TestUpdateFailsOnABrokenConfigWithoutRunningFn(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(path, []byte("no_such_key: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Update(path, envOf(map[string]string{"HOME": home}), func(*Config) error {
		t.Fatal("fn ran on a config that does not parse")
		return nil
	})

	if err == nil {
		t.Fatal("Update succeeded")
	}
	data, _ := os.ReadFile(path)
	assertEqual(t, string(data), "no_such_key: 1\n")
}

func TestConcurrentUpdatesAllLand(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	env := envOf(map[string]string{"HOME": home})

	const n = 20
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := Update(path, env, func(c *Config) error {
				name := fmt.Sprintf("P%d", i)
				return c.Add(ProjectRef{Name: name, Path: filepath.Join(home, name+".md")})
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	c, err := Load(path, env)
	assertNoErr(t, err)
	assertEqual(t, len(c.Projects), n)
}
