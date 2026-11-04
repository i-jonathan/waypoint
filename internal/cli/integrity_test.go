package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// linkedBoard builds a board reached through two symlinks:
//
//	shortcut -> real/sub            a directory link
//	real/sub/BOARD.md -> ../boards/proj.md
//
// The kernel resolves that ".." from real/sub, so the board is
// real/boards/proj.md. Joining the paths as text gives boards/proj.md instead,
// which is an unrelated file with the same name.
func linkedBoard(t *testing.T) (link, realBoard, unrelated string) {
	t.Helper()
	root := t.TempDir()
	realBoard = filepath.Join(root, "real", "boards", "proj.md")
	unrelated = filepath.Join(root, "boards", "proj.md")
	writeFile(t, realBoard, boardFile)
	writeFile(t, unrelated, "someone else's notes\n")
	if err := os.MkdirAll(filepath.Join(root, "real", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "boards", "proj.md"), filepath.Join(root, "real", "sub", "BOARD.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real", "sub"), filepath.Join(root, "shortcut")); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "shortcut", "BOARD.md"), realBoard, unrelated
}

// projectList is what "project ls --json" reports.
func projectList(t *testing.T, cwd string) []projectJSON {
	t.Helper()
	stdout, stderr, code := runCLI(t, cwd, "project", "ls", "--json")
	if code != exitOK {
		t.Fatalf("project ls: %d %s", code, stderr)
	}
	var got []projectJSON
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestWriteThroughLinksReachesTheRealFile(t *testing.T) {
	link, realBoard, unrelated := linkedBoard(t)
	cwd := t.TempDir()
	if _, stderr, code := runCLI(t, cwd, "project", "add", "P", "--path", link); code != exitOK {
		t.Fatalf("project add: %d %s", code, stderr)
	}

	if _, stderr, code := runCLI(t, cwd, "add", "Through the links", "--project", "P"); code != exitOK {
		t.Fatalf("add: %d %s", code, stderr)
	}

	assertEq(t, readFile(t, unrelated), "someone else's notes\n")
	assertContains(t, readFile(t, realBoard), "Through the links")
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced (%v)", err)
	}

	// The archive sits beside the real file too, not beside either link.
	runCLI(t, cwd, "done", "X-1", "--project", "P")
	if _, stderr, code := runCLI(t, cwd, "archive", "--project", "P"); code != exitOK {
		t.Fatalf("archive: %d %s", code, stderr)
	}
	assertContains(t, readFile(t, strings.TrimSuffix(realBoard, ".md")+".archive.md"), "X-1")
	mustNotExist(t, strings.TrimSuffix(unrelated, ".md")+".archive.md")
	mustNotExist(t, strings.TrimSuffix(link, ".md")+".archive.md")
}

func TestProjectListKeepsTheRegisteredPath(t *testing.T) {
	link, _, _ := linkedBoard(t)
	cwd := t.TempDir()
	runCLI(t, cwd, "project", "add", "P", "--path", link)

	assertEq(t, projectList(t, cwd)[0].Path, link)
}

func TestProjectAddRefusesASecondPathToARegisteredBoard(t *testing.T) {
	link, realBoard, _ := linkedBoard(t)
	cwd := t.TempDir()
	if _, stderr, code := runCLI(t, cwd, "project", "add", "First", "--path", realBoard); code != exitOK {
		t.Fatalf("project add: %d %s", code, stderr)
	}

	_, stderr, code := runCLI(t, cwd, "project", "add", "Second", "--path", link)

	assertEq(t, code, exitError)
	assertContains(t, stderr, `already registered as "First"`)
	assertEq(t, len(projectList(t, cwd)), 1)
}
