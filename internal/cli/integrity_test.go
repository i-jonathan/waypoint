package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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
	runCLI(t, dir, "init", "--storage", "markdown", "P")
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
	assertContains(t, stderr, "T-1 is on the board and in the archive with different content")
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
	residue := "## Todo\n\n### T-1 Keep me\n\n- created: 2026-11-03T12:00:00Z\n\nMonths of notes.\n"
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
				if _, stderr, code := runCLI(t, dir, "init", "--storage", "markdown", "P"); code != exitOK {
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

// hashOf reads the hash out of a command's JSON output.
func hashOf(t *testing.T, stdout string) string {
	t.Helper()
	var got struct {
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("%v in %q", err, stdout)
	}
	if got.Hash == "" {
		t.Fatalf("no hash in %q", stdout)
	}
	return got.Hash
}

func TestReturnedHashMatchesTheFile(t *testing.T) {
	cases := map[string]struct {
		stdin string
		args  []string
	}{
		"trailing newline": {"text\n", []string{"body=-"}},
		"heading":          {"# Heading\n\ntext", []string{"body=-"}},
		"unclosed fence":   {"```go\ncode", []string{"body=-"}},
		"crlf":             {"one\r\ntwo\r\n", []string{"body=-"}},
		"extra with space": {"", []string{"note=value "}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			runCLI(t, dir, "init", "--storage", "markdown", "P")
			runCLI(t, dir, "add", "Card")

			args := append([]string{"set", "T-1", "--json"}, c.args...)
			stdout, stderr, code := runStdin(t, dir, c.stdin, args...)
			if code != exitOK {
				t.Fatalf("set: %d %s", code, stderr)
			}
			hash := hashOf(t, stdout)

			shown, _, _ := runCLI(t, dir, "show", "T-1", "--json")
			assertEq(t, hash, hashOf(t, shown))
			if _, stderr, code := runCLI(t, dir, "set", "T-1", "other=1", "--if-match", hash); code != exitOK {
				t.Fatalf("next write with the returned hash: %d %s", code, stderr)
			}
		})
	}
}

func TestReturnedHashMatchesTheFileWhenAddingACard(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, dir, "init", "--storage", "markdown", "P")

	stdout, stderr, code := runStdin(t, dir, "# Heading\n", "add", "Card", "--body", "-", "--json")
	if code != exitOK {
		t.Fatalf("add: %d %s", code, stderr)
	}

	if _, stderr, code := runCLI(t, dir, "set", "T-1", "other=1", "--if-match", hashOf(t, stdout)); code != exitOK {
		t.Fatalf("next write with the returned hash: %d %s", code, stderr)
	}
}

func TestReturnedHashMatchesTheFileForAnAutoArchivedCard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "BOARD.md")
	runCLI(t, dir, "init", "--storage", "markdown", "P")
	writeFile(t, path, strings.Replace(readFile(t, path), "columns:", "archive: {mode: auto, after_days: 0}\ncolumns:", 1))
	runCLI(t, dir, "add", "Card")

	stdout, stderr, code := runCLI(t, dir, "done", "T-1", "--json")
	if code != exitOK {
		t.Fatalf("done: %d %s", code, stderr)
	}
	assertContains(t, stdout, `"archived":true`)
	assertNotContains(t, readFile(t, path), "### T-1")

	if _, stderr, code := runCLI(t, dir, "set", "T-1", "other=1", "--if-match", hashOf(t, stdout)); code != exitOK {
		t.Fatalf("next write with the returned hash: %d %s", code, stderr)
	}
}

func TestWriteOutputNamesTheArchiveForAnAutoArchivedCard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "BOARD.md")
	runCLI(t, dir, "init", "--storage", "markdown", "P")
	writeFile(t, path, strings.Replace(readFile(t, path), "columns:", "archive: {mode: auto, after_days: 0}\ncolumns:", 1))

	stdout, _, _ := runCLI(t, dir, "add", "Straight to done", "--column", "Done")
	assertEq(t, stdout, "Created T-1 in the archive\n")

	runCLI(t, dir, "add", "Finish me")
	stdout, _, _ = runCLI(t, dir, "done", "T-2")
	assertEq(t, stdout, "Moved T-2 to the archive\n")

	runCLI(t, dir, "add", "Move me")
	stdout, _, _ = runCLI(t, dir, "move", "T-3", "Done")
	assertEq(t, stdout, "Moved T-3 to the archive\n")

	stdout, _, _ = runCLI(t, dir, "restore", "T-3", "--column", "Todo")
	assertEq(t, stdout, "Restored T-3 to Todo\n")
}

// readOnly makes path read-only for its owner, as "chmod 444" does.
func readOnly(t *testing.T, path string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("file permissions do not stop root")
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
}

func TestReadOnlyBoardIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "BOARD.md")
	runCLI(t, dir, "init", "--storage", "markdown", "P")
	runCLI(t, dir, "add", "One")
	content := readFile(t, path)
	readOnly(t, path)

	_, stderr, code := runCLI(t, dir, "add", "Two")

	assertEq(t, code, exitError)
	assertContains(t, stderr, "refusing to write: "+path+" is read-only")
	assertEq(t, readFile(t, path), content)
}

func TestReadOnlyArchiveStopsTheWriteBeforeTheBoardChanges(t *testing.T) {
	dir, board, archive := archivedBoard(t)
	runCLI(t, dir, "add", "Second")
	runCLI(t, dir, "done", "T-2")
	boardBefore, archiveBefore := readFile(t, board), readFile(t, archive)
	readOnly(t, archive)

	_, stderr, code := runCLI(t, dir, "archive")

	assertEq(t, code, exitError)
	assertContains(t, stderr, "refusing to write: ")
	assertContains(t, stderr, "BOARD.archive.md is read-only")
	assertEq(t, readFile(t, board), boardBefore)
	assertEq(t, readFile(t, archive), archiveBefore)
}

func TestReadOnlyBoardStopsTheWriteBeforeTheArchiveChanges(t *testing.T) {
	dir, board, archive := archivedBoard(t)
	runCLI(t, dir, "add", "Second")
	runCLI(t, dir, "done", "T-2")
	boardBefore, archiveBefore := readFile(t, board), readFile(t, archive)
	readOnly(t, board)

	_, _, code := runCLI(t, dir, "archive")

	assertEq(t, code, exitError)
	assertEq(t, readFile(t, board), boardBefore)
	assertEq(t, readFile(t, archive), archiveBefore)
}

func TestNoOpOnAReadOnlyBoardSucceeds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "BOARD.md")
	runCLI(t, dir, "init", "--storage", "markdown", "P")
	readOnly(t, path)

	if _, stderr, code := runCLI(t, dir, "archive"); code != exitOK {
		t.Fatalf("archive: %d %s", code, stderr)
	}
}

func TestNoProjectHereIsAnErrorEvenWithOneRegistered(t *testing.T) {
	root := t.TempDir()
	board := filepath.Join(root, "registered", "BOARD.md")
	runCLI(t, root, "project", "add", "Only", "--path", board)
	before := readFile(t, board)
	elsewhere := t.TempDir()

	_, stderr, code := runCLI(t, elsewhere, "add", "Lost card")

	assertEq(t, code, exitError)
	assertContains(t, stderr, `no project here; use --project NAME or run "waypoint init"`)
	assertEq(t, readFile(t, board), before)
}

func TestUnregisteredBoardHereIsNamedInTheError(t *testing.T) {
	cases := map[string]bool{"another project registered": true, "nothing registered": false}
	for name, register := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			here := filepath.Join(dir, "BOARD.md")
			writeFile(t, here, boardFile)
			if register {
				other := t.TempDir()
				runCLI(t, other, "project", "add", "Other", "--path", filepath.Join(other, "BOARD.md"))
			}

			_, stderr, code := runCLI(t, dir, "add", "Lost card")

			assertEq(t, code, exitError)
			assertContains(t, stderr, `BOARD.md here isn't registered; run "waypoint init --storage markdown"`)
			assertEq(t, readFile(t, here), boardFile)

			stdout, _, _ := runCLI(t, dir, "ls", "--json")
			assertContains(t, stdout, `"code":"not_found"`)
		})
	}
}

func TestRegisteredBoardHereIsNotCalledUnregisteredWhenAmbiguous(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "BOARD.md"), boardFile)
	runCLI(t, dir, "project", "add", "One", "--path", filepath.Join(dir, "BOARD.md"))
	runCLI(t, dir, "project", "add", "Two", "--path", filepath.Join(dir, "other.md"))

	_, stderr, code := runCLI(t, dir, "ls")

	assertEq(t, code, exitError)
	assertContains(t, stderr, "no project here")
}

func TestProjectListWorksAnywhere(t *testing.T) {
	root := t.TempDir()
	runCLI(t, root, "project", "add", "Only", "--path", filepath.Join(root, "p", "BOARD.md"))
	elsewhere := t.TempDir()

	stdout, stderr, code := runCLI(t, elsewhere, "project", "ls")

	assertEq(t, code, exitOK)
	assertEq(t, stderr, "")
	assertContains(t, stdout, "  Only")
	assertNotContains(t, stdout, "* Only")
}

func TestSetRefusesKeysThatLookLikeColumnIDOrRank(t *testing.T) {
	cases := map[string]string{
		"column=Done": `use "waypoint move"`,
		"column=":     `use "waypoint move"`,
		"rank=1":      `use "waypoint move"`,
		"id=T-9":      "id cannot be changed",
	}
	for arg, want := range cases {
		t.Run(arg, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "BOARD.md")
			runCLI(t, dir, "init", "--storage", "markdown", "P")
			runCLI(t, dir, "add", "Card")
			before := readFile(t, path)

			stdout, stderr, code := runCLI(t, dir, "set", "T-1", arg)
			assertEq(t, code, exitError)
			assertContains(t, stderr, want)
			assertEq(t, stdout, "")
			assertEq(t, readFile(t, path), before)

			stdout, _, _ = runCLI(t, dir, "set", "T-1", arg, "--json")
			assertContains(t, stdout, `"code":"invalid"`)
		})
	}
}

func TestFilesHoldingColumnIDOrRankKeysStillRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "BOARD.md")
	content := strings.Replace(boardFile, "- created: 2026-11-01\n", "- created: 2026-11-01\n- column: Done\n- id: 7\n- rank: 3\n", 1)
	writeFile(t, path, content)
	runCLI(t, dir, "init", "--storage", "markdown", "P")

	if _, stderr, code := runCLI(t, dir, "set", "X-1", "note=kept"); code != exitOK {
		t.Fatalf("set: %d %s", code, stderr)
	}

	assertContains(t, readFile(t, path), "- column: Done\n- id: 7\n- rank: 3\n- note: kept\n")
	stdout, _, _ := runCLI(t, dir, "show", "X-1")
	assertContains(t, stdout, "column: Todo\n")
}

func TestParallelInitsAllRegister(t *testing.T) {
	const n = 12
	var wg sync.WaitGroup
	for i := range n {
		dir := t.TempDir()
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, stderr, code := runCLI(t, dir, "init", "--storage", "markdown", fmt.Sprintf("P%d", i)); code != exitOK {
				t.Errorf("init: %d %s", code, stderr)
			}
		}()
	}
	wg.Wait()

	assertEq(t, len(projectList(t, t.TempDir())), n)
}

func TestParallelAddsAndRemovesAllTakeEffect(t *testing.T) {
	root := t.TempDir()
	for i := range 6 {
		runCLI(t, root, "project", "add", fmt.Sprintf("Old%d", i), "--path", filepath.Join(root, fmt.Sprintf("old%d.md", i)))
	}

	var wg sync.WaitGroup
	for i := range 6 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, stderr, code := runCLI(t, root, "project", "rm", fmt.Sprintf("Old%d", i)); code != exitOK {
				t.Errorf("rm: %d %s", code, stderr)
			}
		}()
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("New%d", i)
			if _, stderr, code := runCLI(t, root, "project", "add", name, "--path", filepath.Join(root, name+".md")); code != exitOK {
				t.Errorf("add: %d %s", code, stderr)
			}
		}()
	}
	wg.Wait()

	got := projectList(t, root)
	assertEq(t, len(got), 6)
	for _, p := range got {
		if !strings.HasPrefix(p.Name, "New") {
			t.Fatalf("%s is still registered", p.Name)
		}
	}
}

func TestNegativeAfterDaysDoesNotArchiveADoneCard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "BOARD.md")
	runCLI(t, dir, "init", "--storage", "markdown", "P")
	runCLI(t, dir, "add", "Card")
	edited := strings.Replace(readFile(t, path), "columns:", "archive: {mode: auto, after_days: -5}\ncolumns:", 1)
	writeFile(t, path, edited)

	_, stderr, code := runCLI(t, dir, "done", "T-1")

	assertEq(t, code, exitError)
	assertContains(t, stderr, "after_days")
	assertEq(t, readFile(t, path), edited)
}

func TestHeadingWithAnotherPrefixIsATitle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "BOARD.md")
	runCLI(t, dir, "init", "--storage", "markdown", "P")
	writeFile(t, path, strings.Replace(readFile(t, path), "## Todo\n", "## Todo\n\n### T-1 Real\n\n### COVID-19 booster\n\n### COVID-19\n", 1))

	stdout, stderr, code := runCLI(t, dir, "ls")

	assertEq(t, code, exitOK)
	assertEq(t, stderr, "")
	assertContains(t, stdout, "T-2  Todo  COVID-19 booster")
	assertContains(t, stdout, "T-3  Todo  COVID-19")
	assertContains(t, readFile(t, path), "### T-2 COVID-19 booster\n")
}

// The cost of the wrong-prefix rule: with no card of this board's prefix yet,
// two id-shaped titles cannot be told from a mistyped prefix.
func TestTwoIDShapedTitlesOnAnEmptyBoardAreRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "BOARD.md")
	runCLI(t, dir, "init", "--storage", "markdown", "P")
	edited := strings.Replace(readFile(t, path), "## Todo\n", "## Todo\n\n### COVID-19 booster\n\n### COVID-19 test\n", 1)
	writeFile(t, path, edited)

	_, stderr, code := runCLI(t, dir, "ls")

	assertEq(t, code, exitError)
	assertContains(t, stderr, `no card uses the prefix "T"`)
	assertContains(t, stderr, "if these are titles, give one an id")
	assertEq(t, readFile(t, path), edited)
}

func TestRepeatedDependencyIsShownOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "BOARD.md")
	runCLI(t, dir, "init", "--storage", "markdown", "P")
	writeFile(t, path, strings.Replace(readFile(t, path), "## Todo\n", "## Todo\n\n### T-2 x\n\n- depends: T-1, T-1\n", 1))

	stdout, _, _ := runCLI(t, dir, "ls")

	assertContains(t, stdout, "[BLOCKED by T-1?]")
}

func TestUnchangedArchiveIsNotRewrittenByABoardWrite(t *testing.T) {
	dir, _, archive := archivedBoard(t)
	before, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runCLI(t, dir, "add", "x"); code != exitOK {
		t.Fatalf("add: %d %s", code, stderr)
	}

	after, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("the archive was replaced")
	}
}

func TestRestoringACardWithAnotherPrefixLosesNothing(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, "BOARD.md")
	archive := filepath.Join(dir, "BOARD.archive.md")
	runCLI(t, dir, "init", "--storage", "markdown", "P")
	writeFile(t, archive, "# Archive\n\n## 2026-10\n\n### OLD-7 From another board\n\n- done: 2026-10-01\n")
	boardBefore, archiveBefore := readFile(t, board), readFile(t, archive)

	_, stderr, code := runCLI(t, dir, "restore", "OLD-7")

	assertEq(t, code, exitError)
	assertContains(t, stderr, `OLD-7 cannot be restored: its prefix does not match this board's prefix "T"`)
	assertNotContains(t, stderr, "not in memory")
	assertEq(t, readFile(t, board), boardBefore)
	assertEq(t, readFile(t, archive), archiveBefore)
}

func TestFormatGuideCoversCommentsLockFileAndPrefix(t *testing.T) {
	stdout, _, _ := runCLI(t, t.TempDir(), "help", "format")
	for _, want := range []string{"Comments in the front matter are not kept", "BOARD.md.lock", "*.lock", "Only this board's prefix makes an id", "next time it\nreads the file"} {
		assertContains(t, stdout, want)
	}
}

func TestUnregisteredBoardHereBeatsARegisteredAncestorOrChild(t *testing.T) {
	cases := map[string]struct{ registered, cwd string }{
		"under a registered ancestor": {"BOARD.md", "sub"},
		"above a registered child":    {filepath.Join("sub", "BOARD.md"), "."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			registered := filepath.Join(root, c.registered)
			runCLI(t, root, "project", "add", "Other", "--path", registered)
			cwd := filepath.Join(root, c.cwd)
			here := filepath.Join(cwd, "BOARD.md")
			writeFile(t, here, boardFile)
			before := readFile(t, registered)

			_, stderr, code := runCLI(t, cwd, "add", "Lost card")

			assertEq(t, code, exitError)
			assertContains(t, stderr, `BOARD.md here isn't registered; run "waypoint init --storage markdown"`)
			assertEq(t, readFile(t, here), boardFile)
			assertEq(t, readFile(t, registered), before)
		})
	}
}

func TestParentFolderOfAProjectIsNotInIt(t *testing.T) {
	root := t.TempDir()
	board := filepath.Join(root, "p", "BOARD.md")
	runCLI(t, root, "project", "add", "Only", "--path", board)
	before := readFile(t, board)

	_, stderr, code := runCLI(t, root, "add", "Lost card")

	assertEq(t, code, exitError)
	assertContains(t, stderr, `no project here; use --project NAME or run "waypoint init"`)
	assertEq(t, readFile(t, board), before)
}

func TestSubfolderOfAProjectIsInIt(t *testing.T) {
	root := t.TempDir()
	runCLI(t, root, "init", "--storage", "markdown", "P")
	sub := filepath.Join(root, "docs", "notes")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runCLI(t, sub, "add", "Found it"); code != exitOK {
		t.Fatalf("add: %d %s", code, stderr)
	}
	assertContains(t, readFile(t, filepath.Join(root, "BOARD.md")), "Found it")
}

// Bodies a save would normalise: the demoted heading and the closed fence are
// what Format writes to the archive while the board still holds the original.
var unsavedBodies = map[string]struct{ original, saved string }{
	"heading":        {"# Notes\n\ntext", "#### Notes\n\ntext"},
	"unclosed fence": {"```go\ncode", "```go\ncode\n```"},
}

// residueFiles writes the state a crash leaves: T-1 on the board with boardBody
// and in the archive with archiveBody.
func residueFiles(t *testing.T, dir, boardBody, archiveBody string) (board, archive string) {
	t.Helper()
	board = filepath.Join(dir, "BOARD.md")
	archive = filepath.Join(dir, "BOARD.archive.md")
	runCLI(t, dir, "init", "--storage", "markdown", "P")
	writeFile(t, board, strings.Replace(readFile(t, board), "## Done\n", "## Done\n\n### T-1 Card\n\n- created: 2026-11-01\n- done: 2026-11-02\n\n"+boardBody+"\n", 1))
	writeFile(t, archive, "# Archive\n\n## 2026-11\n\n### T-1 Card\n\n- created: 2026-11-01\n- done: 2026-11-02\n\n"+archiveBody+"\n")
	return board, archive
}

func TestCrashResidueWithUnsavedBodyResolves(t *testing.T) {
	for name, body := range unsavedBodies {
		// Archive writes the archive first, so the board holds the original
		// body. Restore of a hand-written archive is the other way round.
		cases := map[string][2]string{
			"archive": {body.original, body.saved},
			"restore": {body.saved, body.original},
		}
		for op, bodies := range cases {
			t.Run(name+"/"+op, func(t *testing.T) {
				dir := t.TempDir()
				board, archive := residueFiles(t, dir, bodies[0], bodies[1])

				_, stderr, code := runCLI(t, dir, "ls")
				assertEq(t, code, exitOK)
				assertContains(t, stderr, "keeping the board copy")

				if _, stderr, code := runCLI(t, dir, "add", "x"); code != exitOK {
					t.Fatalf("add: %d %s", code, stderr)
				}
				assertNotContains(t, readFile(t, archive), "### T-1")
				assertContains(t, readFile(t, board), "### T-1 Card")
			})
		}
	}
}

func TestDifferentBodyWithTheSameIDIsStillRefused(t *testing.T) {
	dir := t.TempDir()
	residueFiles(t, dir, "# Notes", "#### Other notes")

	_, stderr, code := runCLI(t, dir, "add", "x")

	assertEq(t, code, exitError)
	assertContains(t, stderr, "T-1 is on the board and in the archive with different content")
	assertContains(t, stderr, "delete the copy you don't want")
	assertContains(t, stderr, "if they are separate cards, give one a new id")
}

func TestWrongPrefixIsAnErrorAndWritesNothing(t *testing.T) {
	cases := map[string]string{"another letter": "W", "wrong case": "t"}
	for name, prefix := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "BOARD.md")
			runCLI(t, dir, "init", "--storage", "markdown", "P")
			runCLI(t, dir, "add", "One")
			runCLI(t, dir, "add", "Two", "--depends", "T-1")
			runCLI(t, dir, "add", "Three")
			edited := strings.Replace(readFile(t, path), "prefix: T\n", "prefix: "+prefix+"\n", 1)
			writeFile(t, path, edited)

			for _, args := range [][]string{{"ls"}, {"add", "Four"}} {
				_, stderr, code := runCLI(t, dir, args...)
				assertEq(t, code, exitError)
				assertContains(t, stderr, `no card uses the prefix "`+prefix+`", but 3 look like ids with another prefix (first on line`)
				assertContains(t, stderr, `check "prefix" in the front matter; or, if these are titles, give one an id`)
				assertEq(t, readFile(t, path), edited)
			}

			stdout, _, _ := runCLI(t, dir, "ls", "--json")
			assertContains(t, stdout, `"code":"parse"`)
		})
	}
}

func TestRealCardsOrALoneTitleAreNotAWrongPrefix(t *testing.T) {
	cases := map[string]string{
		"lone title":           "### COVID-19 booster\n",
		"beside a real card":   "### T-1 Real\n\n### COVID-19 booster\n\n### H1N1-2 jab\n",
		"one foreign, id-less": "### COVID-19 booster\n\n### Call the plumber\n",
	}
	for name, cards := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "BOARD.md")
			runCLI(t, dir, "init", "--storage", "markdown", "P")
			writeFile(t, path, strings.Replace(readFile(t, path), "## Todo\n", "## Todo\n\n"+cards, 1))

			if _, stderr, code := runCLI(t, dir, "ls"); code != exitOK {
				t.Fatalf("ls: %d %s", code, stderr)
			}
			assertContains(t, readFile(t, path), " COVID-19 booster\n")
		})
	}
}

// runAt runs the CLI with the clock at now, in now's location.
func runAt(t *testing.T, dir string, now time.Time, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	env, out, errb := testEnv(t, dir, nil)
	env.Now = func() time.Time { return now }
	code = Run(context.Background(), args, env)
	return out.String(), errb.String(), code
}

var (
	westOfUTC = time.FixedZone("UTC-5", -5*3600)
	eastOfUTC = time.FixedZone("UTC+9", 9*3600)
)

func TestDatesAreStoredInUTCAndShownInLocalTime(t *testing.T) {
	moment := time.Date(2026, 11, 4, 2, 30, 10, 700_000_000, time.UTC)
	cases := map[string]struct {
		zone *time.Location
		want string
	}{
		"west of UTC, still the 3rd": {westOfUTC, "2026-11-03"},
		"east of UTC, the 4th":       {eastOfUTC, "2026-11-04"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			now := moment.In(c.zone)
			runCLI(t, dir, "init", "--storage", "markdown", "P")
			if _, stderr, code := runAt(t, dir, now, "add", "Card", "--column", "Done"); code != exitOK {
				t.Fatalf("add: %d %s", code, stderr)
			}

			board := readFile(t, filepath.Join(dir, "BOARD.md"))
			assertContains(t, board, "- created: 2026-11-04T02:30:10Z\n")
			assertContains(t, board, "- done: 2026-11-04T02:30:10Z\n")

			stdout, _, _ := runAt(t, dir, now, "show", "T-1")
			assertContains(t, stdout, "created: "+c.want+"\n")
			assertContains(t, stdout, "done: "+c.want+"\n")

			stdout, _, _ = runAt(t, dir, now, "show", "T-1", "--json")
			assertContains(t, stdout, `"created":"2026-11-04T02:30:10Z"`)
			assertContains(t, stdout, `"done":"2026-11-04T02:30:10Z"`)
		})
	}
}

func TestDateOnlyValuesRoundTripAndAreShownVerbatim(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "BOARD.md")
	content := strings.Replace(boardFile, "## Done\n", "## Done\n\n### X-3 Finished\n\n- created: 2026-10-26\n- done: 2026-10-27\n", 1)
	writeFile(t, path, strings.Replace(content, "next_id: 3", "next_id: 4", 1))
	runCLI(t, dir, "init", "--storage", "markdown", "P")
	now := time.Date(2026, 11, 3, 20, 0, 0, 0, westOfUTC)

	if _, stderr, code := runAt(t, dir, now, "set", "X-1", "note=x"); code != exitOK {
		t.Fatalf("set: %d %s", code, stderr)
	}

	board := readFile(t, path)
	assertContains(t, board, "- created: 2026-10-26\n- done: 2026-10-27\n")
	assertContains(t, board, "- created: 2026-11-01\n")

	stdout, _, _ := runAt(t, dir, now, "show", "X-3")
	assertContains(t, stdout, "created: 2026-10-26\n")
	assertContains(t, stdout, "done: 2026-10-27\n")

	stdout, _, _ = runAt(t, dir, now, "show", "X-3", "--json")
	assertContains(t, stdout, `"created":"2026-10-26","done":"2026-10-27"`)
	stdout, _, _ = runAt(t, dir, now, "show", "X-2", "--json")
	assertContains(t, stdout, `"done":null`)
}

func TestHandWrittenOffsetIsNormalisedToUTC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "BOARD.md")
	writeFile(t, path, strings.Replace(boardFile, "- created: 2026-11-01\n", "- created: 2026-11-04T10:00:00+01:00\n", 1))
	runCLI(t, dir, "init", "--storage", "markdown", "P")

	if _, stderr, code := runCLI(t, dir, "set", "X-1", "note=x"); code != exitOK {
		t.Fatalf("set: %d %s", code, stderr)
	}

	assertContains(t, readFile(t, path), "- created: 2026-11-04T09:00:00Z\n")
}

func TestBadTimestampIsAParseErrorOnItsLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "BOARD.md")
	writeFile(t, path, boardFile)
	runCLI(t, dir, "init", "--storage", "markdown", "P")
	content := strings.Replace(boardFile, "- created: 2026-11-01\n", "- created: 2026-11-04T25:00:00Z\n", 1)
	writeFile(t, path, content)
	line := 1 + strings.Count(content[:strings.Index(content, "T25:00")], "\n")

	stdout, _, code := runCLI(t, dir, "ls", "--json")

	assertEq(t, code, exitError)
	assertContains(t, stdout, `"code":"parse"`)
	assertContains(t, stdout, fmt.Sprintf(`"line":%d`, line))
	assertEq(t, readFile(t, path), content)
}

func TestArchiveMonthIsTheMonthOfTheStoredUTCValue(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, dir, "init", "--storage", "markdown", "P")
	runCLI(t, dir, "add", "Card")
	// Still October here, but already November in UTC.
	now := time.Date(2026, 10, 31, 22, 0, 0, 0, westOfUTC)

	if _, stderr, code := runAt(t, dir, now, "done", "T-1"); code != exitOK {
		t.Fatalf("done: %d %s", code, stderr)
	}
	runAt(t, dir, now, "archive")

	archive := readFile(t, filepath.Join(dir, "BOARD.archive.md"))
	assertContains(t, archive, "## 2026-11\n")
	assertContains(t, archive, "- done: 2026-11-01T03:00:00Z\n")
}

// oldFormatBoard is a file named BOARD.md that Waypoint did not write.
const oldFormatBoard = "# Pharmacy board\n\n## To do\n- [ ] Check the fridge log\n\n## Done\n- Stock count\n"

func TestFileThatIsNotAWaypointBoardDoesNotBlockProjectSearch(t *testing.T) {
	contents := map[string]string{
		"no front matter":         oldFormatBoard,
		"another tool's":          "---\ntitle: Notes\n---\n\nText\n",
		"opens with a rule":       "---\n\nJust a rule, never closed.\n",
		"another tool's bad YAML": "---\ntitle: a: b\n---\n",
		"empty":                   "",
	}
	for name, content := range contents {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "pharmacy")
			here := filepath.Join(dir, "BOARD.md")
			writeFile(t, here, content)
			if _, stderr, code := runCLI(t, dir, "project", "add", "pharmacy", "--path", "waypoint.md"); code != exitOK {
				t.Fatalf("project add: %d %s", code, stderr)
			}

			if _, stderr, code := runCLI(t, dir, "add", "From root"); code != exitOK {
				t.Fatalf("add: %d %s", code, stderr)
			}
			stdout, stderr, code := runCLI(t, dir, "ls")
			assertEq(t, code, exitOK)
			assertEq(t, stderr, "")
			assertContains(t, stdout, "From root")
			stdout, _, code = runCLI(t, dir, "ls", "--json")
			assertEq(t, code, exitOK)
			assertContains(t, stdout, `"title":"From root"`)

			stdout, _, _ = runCLI(t, dir, "project", "ls")
			assertContains(t, stdout, "* pharmacy")
			assertEq(t, readFile(t, here), content)
			assertContains(t, readFile(t, filepath.Join(dir, "waypoint.md")), "From root")
		})
	}
}

func TestFolderNamedBoardDoesNotBlockProjectSearch(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "BOARD.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	runCLI(t, dir, "project", "add", "P", "--path", "waypoint.md")

	if _, stderr, code := runCLI(t, dir, "add", "Card"); code != exitOK {
		t.Fatalf("add: %d %s", code, stderr)
	}
}

func TestOtherCaseOfTheBoardNameDoesNotBlockProjectSearch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "board.md"), boardFile)
	runCLI(t, dir, "project", "add", "P", "--path", "waypoint.md")

	if _, stderr, code := runCLI(t, dir, "add", "Card"); code != exitOK {
		t.Fatalf("add: %d %s", code, stderr)
	}
}

func TestBrokenWaypointBoardHereStillBlocks(t *testing.T) {
	root := t.TempDir()
	registered := filepath.Join(root, "BOARD.md")
	runCLI(t, root, "project", "add", "Other", "--path", registered)
	before := readFile(t, registered)
	cwd := filepath.Join(root, "sub")
	writeFile(t, filepath.Join(cwd, "BOARD.md"), "---\nwaypoint: 1\nname: a: b\n---\n")

	_, stderr, code := runCLI(t, cwd, "add", "Lost card")

	assertEq(t, code, exitError)
	assertContains(t, stderr, `BOARD.md here isn't registered; run "waypoint init --storage markdown"`)
	assertEq(t, readFile(t, registered), before)
}

func TestSymlinkToAWaypointBoardBlocks(t *testing.T) {
	root := t.TempDir()
	runCLI(t, root, "project", "add", "Other", "--path", filepath.Join(root, "BOARD.md"))
	elsewhere := filepath.Join(t.TempDir(), "real.md")
	writeFile(t, elsewhere, boardFile)
	cwd := filepath.Join(root, "sub")
	if err := os.Mkdir(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(cwd, "BOARD.md")); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runCLI(t, cwd, "add", "Lost card")

	assertEq(t, code, exitError)
	assertContains(t, stderr, "isn't registered")
}
