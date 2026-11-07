package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// twoProjects makes folders a (markdown, with cards) and b (an empty SQLite board).
func twoProjects(t *testing.T) (a, b string) {
	t.Helper()
	root := t.TempDir()
	a, b = filepath.Join(root, "a"), filepath.Join(root, "b")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	runCLI(t, a, "init", "--storage", "markdown", "Alpha")
	runCLI(t, a, "add", "Measure every room", "--body", "Bring a tape.\n\n- [ ] kitchen")
	runCLI(t, a, "add", "Paint the hallway", "--depends", "T-1")
	runCLI(t, a, "done", "T-1")
	runCLI(t, a, "archive")
	runCLI(t, b, "init", "Beta")
	return a, b
}

func TestExportJSONGoesToStdoutByDefault(t *testing.T) {
	a, _ := twoProjects(t)
	stdout, stderr, code := runCLI(t, a, "export")
	assertEq(t, code, 0)
	assertEq(t, stderr, "")
	var snap struct {
		Format   int `json:"format"`
		Cards    []struct{ ID, Title string }
		Archived []struct{ ID, Title string }
	}
	if err := json.Unmarshal([]byte(stdout), &snap); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	assertEq(t, snap.Format, 1)
	assertEq(t, len(snap.Cards), 1)
	assertEq(t, snap.Cards[0].Title, "Paint the hallway")
	assertEq(t, len(snap.Archived), 1)
	assertEq(t, snap.Archived[0].Title, "Measure every room")
}

func TestExportToAFileNeverOverwrites(t *testing.T) {
	a, _ := twoProjects(t)
	out := filepath.Join(a, "backup.json")
	stdout, _, code := runCLI(t, a, "export", "--out", out)
	assertEq(t, code, 0)
	assertContains(t, stdout, out)
	assertContains(t, readFile(t, out), `"format": 1`)

	before := readFile(t, out)
	_, stderr, code := runCLI(t, a, "export", "--out", out)
	assertEq(t, code, 1)
	assertContains(t, stderr, "already exists")
	assertEq(t, readFile(t, out), before)
}

func TestExportRejectsAnUnknownFormat(t *testing.T) {
	a, _ := twoProjects(t)
	_, stderr, code := runCLI(t, a, "export", "--format", "yaml")
	assertEq(t, code, 2)
	assertContains(t, stderr, "yaml")
}

func TestImportMovesAProjectBetweenStoragesAndBack(t *testing.T) {
	a, b := twoProjects(t)
	file := filepath.Join(a, "backup.json")
	runCLI(t, a, "export", "--out", file)

	stdout, stderr, code := runCLI(t, b, "import", file)
	assertEq(t, code, 0)
	assertEq(t, stderr, "")
	assertContains(t, stdout, "Imported 1 card and 1 archived card")

	stdout, _, _ = runCLI(t, b, "ls", "--archived")
	assertContains(t, stdout, "Paint the hallway")
	assertContains(t, stdout, "Measure every room")
	// The blocked state came across: T-2 depends on the archived, done T-1.
	stdout, _, _ = runCLI(t, b, "show", "T-1")
	assertContains(t, stdout, "Bring a tape.")
}

func TestImportIntoAProjectWithCardsNeedsReplace(t *testing.T) {
	a, b := twoProjects(t)
	file := filepath.Join(a, "backup.json")
	runCLI(t, a, "export", "--out", file)
	runCLI(t, b, "add", "Already here")

	_, stderr, code := runCLI(t, b, "import", file)
	assertEq(t, code, 1)
	assertContains(t, stderr, "replace")
	stdout, _, _ := runCLI(t, b, "ls")
	assertContains(t, stdout, "Already here")

	stdout, _, code = runCLI(t, b, "import", file, "--replace", "--json")
	assertEq(t, code, 0)
	assertContains(t, stdout, `"cards":1`)
	assertContains(t, stdout, `"archived":1`)
	stdout, _, _ = runCLI(t, b, "ls")
	assertNotContains(t, stdout, "Already here")
	assertContains(t, stdout, "Paint the hallway")
}

func TestImportRefusals(t *testing.T) {
	a, b := twoProjects(t)
	notes := filepath.Join(a, "notes.txt")
	writeFile(t, notes, "{}")
	bad := filepath.Join(a, "bad.json")
	writeFile(t, bad, `{"format": 1, "surprise": true}`)
	for name, args := range map[string][]string{
		"no file":             {"import"},
		"missing file":        {"import", filepath.Join(a, "nope.json")},
		"format by extension": {"import", notes},
		"not a snapshot":      {"import", bad},
		"unknown format":      {"import", bad, "--format", "yaml"},
	} {
		_, stderr, code := runCLI(t, b, args...)
		if code == 0 || strings.TrimSpace(stderr) == "" {
			t.Errorf("%s: exit %d, stderr %q; want a failure with a message", name, code, stderr)
		}
	}
	stdout, _, _ := runCLI(t, b, "ls")
	assertNotContains(t, stdout, "Paint")
}
