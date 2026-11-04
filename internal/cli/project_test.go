package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectAddNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pharmacy.md")

	stdout, stderr, code := runCLI(t, dir, "project", "add", "Pharmacy", "--path", path)
	assertEq(t, code, 0)
	assertEq(t, stderr, "")
	assertEq(t, stdout, "Added project Pharmacy (markdown) at "+path+"\n")

	board := readFile(t, path)
	assertContains(t, board, "name: Pharmacy")
	assertContains(t, board, "prefix: T")

	stdout, _, code = runCLI(t, dir, "project", "ls")
	assertEq(t, code, 0)
	assertContains(t, stdout, "Pharmacy")
	assertContains(t, stdout, "markdown")
	assertContains(t, stdout, path)

	cfg := readFile(t, sandboxFor(t).config)
	assertContains(t, cfg, "name: Pharmacy")
	assertContains(t, cfg, path)
}

func TestProjectAddDefaultPathCreatesDataDir(t *testing.T) {
	dir := t.TempDir()
	// A fresh machine: data_dir does not exist yet.
	want := filepath.Join(sandboxFor(t).home, ".local", "share", "waypoint", "my-big-project.md")
	mustNotExist(t, filepath.Dir(want))

	stdout, stderr, code := runCLI(t, dir, "project", "add", "  My Big_Project!! ")
	assertEq(t, code, 0)
	assertEq(t, stderr, "")
	assertContains(t, stdout, want)
	mustExist(t, want)
}

func TestProjectAddEmptySlugAsksForPath(t *testing.T) {
	dir := t.TempDir()
	_, stderr, code := runCLI(t, dir, "project", "add", "!!!")
	assertEq(t, code, 1)
	assertContains(t, stderr, "--path")
	mustNotExist(t, sandboxFor(t).config)
}

func TestProjectAddRelativePathUsesEnvCwd(t *testing.T) {
	dir := t.TempDir()
	_, stderr, code := runCLI(t, dir, "project", "add", "Rel", "--path", "sub/board.md")
	assertEq(t, code, 0)
	assertEq(t, stderr, "")
	mustExist(t, filepath.Join(dir, "sub", "board.md"))
	// The process working directory was not used.
	if wd, err := os.Getwd(); err == nil {
		mustNotExist(t, filepath.Join(wd, "sub", "board.md"))
	}
}

func TestProjectAddCustomPrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.md")
	_, _, code := runCLI(t, dir, "project", "add", "B", "--path", path, "--prefix", "WP")
	assertEq(t, code, 0)
	assertContains(t, readFile(t, path), "prefix: WP")
}

func TestProjectAddInvalidPrefixWritesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.md")
	_, stderr, code := runCLI(t, dir, "project", "add", "B", "--path", path, "--prefix", "9x")
	assertEq(t, code, 1)
	assertContains(t, stderr, `prefix "9x" must match`)
	mustNotExist(t, path)
	mustNotExist(t, sandboxFor(t).config)
}

func TestProjectAddExistingBoard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.md")
	writeFile(t, path, boardFile)

	stdout, stderr, code := runCLI(t, dir, "project", "add", "Existing", "--path", path)
	assertEq(t, code, 0)
	assertEq(t, stderr, "")
	assertContains(t, stdout, "Added project Existing (markdown) at "+path)
	assertEq(t, readFile(t, path), boardFile) // cards and bytes kept, nothing rewritten
	assertContains(t, readFile(t, sandboxFor(t).config), "name: Existing")
}

func TestProjectAddExistingBoardWithIdlessCardIsNotRewritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "idless.md")
	content := strings.Replace(boardFile, "### X-2 Second card", "### Second card", 1)
	writeFile(t, path, content)

	_, stderr, code := runCLI(t, dir, "project", "add", "Idless", "--path", path)
	assertEq(t, code, 0)
	assertEq(t, stderr, "")
	assertEq(t, readFile(t, path), content)
}

func TestProjectAddPlainMarkdown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.md")
	plain := "# Board\n## Todo\n- item\n"
	writeFile(t, path, plain)

	_, stderr, code := runCLI(t, dir, "project", "add", "Plain", "--path", path)
	assertEq(t, code, 1)
	assertContains(t, stderr, "not a Waypoint board")
	assertEq(t, readFile(t, path), plain)
	mustNotExist(t, sandboxFor(t).config)
}

func TestProjectAddParseErrorNamesLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.md")
	broken := strings.Replace(boardFile, "- created: 2026-11-01", "- created: yesterday", 1)
	writeFile(t, path, broken)

	_, stderr, code := runCLI(t, dir, "project", "add", "Broken", "--path", path)
	assertEq(t, code, 1)
	assertContains(t, stderr, "line ")
	assertEq(t, readFile(t, path), broken)
	mustNotExist(t, sandboxFor(t).config)

	stdout, _, code := runCLI(t, dir, "project", "add", "Broken", "--path", path, "--json")
	assertEq(t, code, 1)
	var e struct {
		Error string
		Code  string
		Line  int
	}
	if err := json.Unmarshal([]byte(stdout), &e); err != nil {
		t.Fatalf("not json: %q: %v", stdout, err)
	}
	assertEq(t, e.Code, "parse")
	if e.Line < 1 {
		t.Fatalf("want a line number, got %d", e.Line)
	}
}

func TestProjectAddNotABoardJSONHasNoLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.md")
	writeFile(t, path, "# just notes\n")
	stdout, _, code := runCLI(t, dir, "project", "add", "P", "--path", path, "--json")
	assertEq(t, code, 1)
	assertContains(t, stdout, `"code":"parse"`)
	assertNotContains(t, stdout, `"line"`)
}

func TestProjectAddSQLiteRefused(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.db", "a.sqlite", "a.sqlite3"} {
		path := filepath.Join(dir, name)
		_, stderr, code := runCLI(t, dir, "project", "add", "S", "--path", path)
		assertEq(t, code, 1)
		assertContains(t, stderr, "sqlite storage arrives in a later version")
		mustNotExist(t, path)
	}
	mustNotExist(t, sandboxFor(t).config)
}

func TestProjectAddDuplicateCreatesNoFile(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "one.md")
	second := filepath.Join(dir, "two.md")
	_, _, code := runCLI(t, dir, "project", "add", "Same", "--path", first)
	assertEq(t, code, 0)

	_, stderr, code := runCLI(t, dir, "project", "add", "same", "--path", second)
	assertEq(t, code, 1)
	assertContains(t, stderr, "already exists")
	mustNotExist(t, second)
}

// unwritableConfig returns a WAYPOINT_CONFIG override whose directory exists
// but cannot be written, so reading the config works and saving it fails.
func unwritableConfig(t *testing.T, dir string) map[string]string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("permissions are not enforced for root")
	}
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	return map[string]string{"WAYPOINT_CONFIG": filepath.Join(locked, "config.yaml")}
}

func TestProjectAddConfigSaveFailureNamesLeftoverFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.md")

	_, stderr, code := runCLIEnv(t, dir, unwritableConfig(t, dir), "project", "add", "New", "--path", path)
	assertEq(t, code, 1)
	assertContains(t, stderr, path)
	assertContains(t, stderr, "left behind")
	mustExist(t, path)
}

func TestProjectAddConfigSaveFailureForExistingBoardNamesNoLeftover(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.md")
	writeFile(t, path, boardFile)

	_, stderr, code := runCLIEnv(t, dir, unwritableConfig(t, dir), "project", "add", "E", "--path", path)
	assertEq(t, code, 1)
	assertNotContains(t, stderr, "left behind")
	assertEq(t, readFile(t, path), boardFile)
}

func TestProjectAddJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "j.md")
	stdout, _, code := runCLI(t, dir, "project", "add", "J", "--path", path, "--json")
	assertEq(t, code, 0)
	var got map[string]string
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("not json: %q: %v", stdout, err)
	}
	assertEq(t, got["name"], "J")
	assertEq(t, got["storage"], "markdown")
	assertEq(t, got["path"], path)
	assertEq(t, len(got), 3)
}

func TestProjectLsEmpty(t *testing.T) {
	dir := t.TempDir()
	stdout, _, code := runCLI(t, dir, "project", "ls")
	assertEq(t, code, 0)
	assertEq(t, stdout, "No projects yet. Run \"waypoint project add NAME\".\n")
	mustNotExist(t, sandboxFor(t).config) // a read never creates the config

	stdout, _, code = runCLI(t, dir, "project", "ls", "--json")
	assertEq(t, code, 0)
	assertEq(t, stdout, "[]\n")
}

func TestProjectLsMarksCurrentAndJSON(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a", "a.md")
	b := filepath.Join(dir, "b", "b.md")
	runCLI(t, dir, "project", "add", "A", "--path", a)
	runCLI(t, dir, "project", "add", "B", "--path", b)

	stdout, _, code := runCLI(t, filepath.Join(dir, "b"), "project", "ls")
	assertEq(t, code, 0)
	for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
		if strings.Contains(line, b) {
			assertEq(t, strings.HasPrefix(line, "* "), true)
		} else {
			assertEq(t, strings.HasPrefix(line, "  "), true)
		}
	}

	stdout, _, _ = runCLI(t, filepath.Join(dir, "b"), "project", "ls", "--json")
	var got []struct {
		Name, Storage, Path string
		Current             bool
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("not json: %q: %v", stdout, err)
	}
	assertEq(t, len(got), 2)
	assertEq(t, got[0].Current, false)
	assertEq(t, got[1].Name, "B")
	assertEq(t, got[1].Current, true)
}

func TestProjectRmKeepsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "k.md")
	runCLI(t, dir, "project", "add", "Keep", "--path", path)

	stdout, stderr, code := runCLI(t, dir, "project", "rm", "keep")
	assertEq(t, code, 0)
	assertEq(t, stderr, "")
	assertEq(t, stdout, "Removed Keep from the registry; "+path+" was not deleted\n")
	mustExist(t, path)
	assertNotContains(t, readFile(t, sandboxFor(t).config), "name: Keep")
}

func TestProjectRmJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "k.md")
	runCLI(t, dir, "project", "add", "Keep", "--path", path)
	stdout, _, code := runCLI(t, dir, "project", "rm", "Keep", "--json")
	assertEq(t, code, 0)
	var got map[string]string
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("not json: %q: %v", stdout, err)
	}
	assertEq(t, got["removed"], "Keep")
	assertEq(t, got["path"], path)
	assertEq(t, len(got), 2)
}

func TestJSONErrorShape(t *testing.T) {
	dir := t.TempDir()
	stdout, stderr, code := runCLI(t, dir, "project", "rm", "nope", "--json")
	assertEq(t, code, 1)
	assertEq(t, stderr, "")
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("not json: %q: %v", stdout, err)
	}
	assertEq(t, got["code"], any("not_found"))
	if msg, _ := got["error"].(string); !strings.Contains(msg, "nope") {
		t.Fatalf("error message %q does not name the project", msg)
	}
	mustNotExist(t, sandboxFor(t).config)
}

func TestPlainErrorShape(t *testing.T) {
	dir := t.TempDir()
	stdout, stderr, code := runCLI(t, dir, "project", "rm", "nope")
	assertEq(t, code, 1)
	assertEq(t, stdout, "")
	if !strings.HasPrefix(stderr, "waypoint: ") || !strings.HasSuffix(stderr, "\n") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestInitInCwd(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "My Repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, dir, "init")
	assertEq(t, code, 0)
	assertEq(t, stderr, "")
	board := filepath.Join(dir, "BOARD.md")
	assertEq(t, stdout, "Added project My Repo (markdown) at "+board+"\n")
	assertContains(t, readFile(t, board), "name: My Repo")

	// Registered, and the cwd resolves to it.
	stdout, _, _ = runCLI(t, dir, "project", "ls")
	assertEq(t, strings.HasPrefix(stdout, "* My Repo"), true)
}

func TestInitWithNameAndPrefix(t *testing.T) {
	dir := t.TempDir()
	_, _, code := runCLI(t, dir, "init", "Named", "--prefix", "NM")
	assertEq(t, code, 0)
	board := readFile(t, filepath.Join(dir, "BOARD.md"))
	assertContains(t, board, "name: Named")
	assertContains(t, board, "prefix: NM")
}

func TestInitRefusesRegisteredBoard(t *testing.T) {
	dir := t.TempDir()
	_, _, code := runCLI(t, dir, "init", "First")
	assertEq(t, code, 0)
	before := readFile(t, filepath.Join(dir, "BOARD.md"))

	_, stderr, code := runCLI(t, dir, "init", "Second")
	assertEq(t, code, 1)
	assertContains(t, stderr, "First")
	assertContains(t, stderr, "already registered")
	assertEq(t, readFile(t, filepath.Join(dir, "BOARD.md")), before)
}

func TestInitRegistersExistingUnregisteredBoard(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, "BOARD.md")
	writeFile(t, board, boardFile)

	_, stderr, code := runCLI(t, dir, "init", "Adopted")
	assertEq(t, code, 0)
	assertEq(t, stderr, "")
	assertEq(t, readFile(t, board), boardFile)
	assertContains(t, readFile(t, sandboxFor(t).config), "name: Adopted")
}

func TestInitRefusesPlainBoardMd(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, "BOARD.md")
	writeFile(t, board, "# my notes\n")
	_, stderr, code := runCLI(t, dir, "init")
	assertEq(t, code, 1)
	assertContains(t, stderr, "not a Waypoint board")
	assertEq(t, readFile(t, board), "# my notes\n")
	mustNotExist(t, sandboxFor(t).config)
}

func TestResolveProjectByCwd(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	runCLI(t, root, "project", "add", "First", "--path", filepath.Join(first, "BOARD.md"))
	runCLI(t, root, "project", "add", "Second", "--path", filepath.Join(second, "BOARD.md"))
	nested := filepath.Join(second, "src", "pkg")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	r := newTestRunner(t, nested, nil)
	_, ref, err := r.service("")
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, ref.Name, "Second")

	stdout, _, _ := runCLI(t, nested, "project", "ls")
	assertContains(t, stdout, "* Second")
	assertContains(t, stdout, "  First")
}
