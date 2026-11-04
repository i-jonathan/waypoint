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

// archivedBoard is a project in a new directory whose T-1, with a body, has
// been archived.
func archivedBoard(t *testing.T) (dir, board, archive string) {
	t.Helper()
	dir = t.TempDir()
	board = filepath.Join(dir, "BOARD.md")
	archive = filepath.Join(dir, "BOARD.archive.md")
	runCLI(t, dir, "init", "P")
	runCLI(t, dir, "add", "Keep me", "--body", "Months of notes.")
	runCLI(t, dir, "done", "T-1")
	if _, stderr, code := runCLI(t, dir, "archive"); code != exitOK {
		t.Fatalf("archive: %d %s", code, stderr)
	}
	return dir, board, archive
}

func TestHandAddedCardReusingAnArchivedIDIsAnError(t *testing.T) {
	dir, board, archive := archivedBoard(t)
	edited := strings.Replace(readFile(t, board), "## Todo\n", "## Todo\n\n### T-1 Something else\n", 1)
	writeFile(t, board, edited)
	archived := readFile(t, archive)

	_, stderr, code := runCLI(t, dir, "add", "x")

	assertEq(t, code, exitError)
	assertContains(t, stderr, "T-1 is on the board and in the archive with different content; give one a new id")
	assertContains(t, stderr, board)
	assertContains(t, stderr, archive)
	assertEq(t, readFile(t, archive), archived)
	assertEq(t, readFile(t, board), edited)

	stdout, _, code := runCLI(t, dir, "ls", "--json")
	assertEq(t, code, exitError)
	var got struct {
		Code string `json:"code"`
		Line *int   `json:"line"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	assertEq(t, got.Code, "parse")
	if got.Line != nil {
		t.Fatalf("line = %d, want none: the clash spans two files", *got.Line)
	}
}

func TestCrashResidueStillResolvesWhenColumnAndDoneDiffer(t *testing.T) {
	dir, board, archive := archivedBoard(t)
	// What a crash leaves behind: the same card in both files, but on the
	// board it is in another column and has no done date.
	residue := "## Todo\n\n### T-1 Keep me\n\n- created: 2026-11-03\n\nMonths of notes.\n"
	writeFile(t, board, strings.Replace(readFile(t, board), "## Todo\n", residue, 1))

	stdout, stderr, code := runCLI(t, dir, "ls")
	assertEq(t, code, exitOK)
	assertContains(t, stdout, "T-1")
	assertContains(t, stderr, "keeping the board copy")

	if _, stderr, code := runCLI(t, dir, "add", "x"); code != exitOK {
		t.Fatalf("add: %d %s", code, stderr)
	}
	assertNotContains(t, readFile(t, archive), "### T-1")
	assertContains(t, readFile(t, board), "### T-1 Keep me")
}

// handKeptBoard is a valid board that a save would rewrite: comments in the
// front matter, loose spacing, and fields out of order.
const handKeptBoard = `---
# Notes to self: this comment goes on a real save.
waypoint: 1
name: Hand kept
prefix: T
next_id: 2
columns:
  - {name: Todo, moves_to: [Done]}   # first
  - {name: Done, done: true, moves_to: [Todo]}
---

## Done


## Todo


### T-1    Spaced out

- created: 2026-11-01
- priority:   P1

Body.
`

func TestNoOpsLeaveTheBoardFileAlone(t *testing.T) {
	boards := map[string]string{
		"comments": handKeptBoard,
		"crlf":     strings.ReplaceAll(handKeptBoard, "\n", "\r\n"),
	}
	commands := [][]string{
		{"archive"},
		{"move", "T-1", "Todo"},
		{"set", "T-1", "note="},
	}
	for name, content := range boards {
		for _, args := range commands {
			t.Run(name+"/"+args[0], func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "BOARD.md")
				writeFile(t, path, content)
				if _, stderr, code := runCLI(t, dir, "init", "P"); code != exitOK {
					t.Fatalf("init: %d %s", code, stderr)
				}
				before, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}

				if _, stderr, code := runCLI(t, dir, args...); code != exitOK {
					t.Fatalf("%v: %d %s", args, code, stderr)
				}

				assertEq(t, readFile(t, path), content)
				after, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if !os.SameFile(before, after) {
					t.Fatal("the file was replaced by a new inode")
				}
			})
		}
	}
}
