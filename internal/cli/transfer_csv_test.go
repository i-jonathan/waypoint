package cli

import (
	"path/filepath"
	"testing"
)

func TestExportCSVAndMarkdown(t *testing.T) {
	a, _ := twoProjects(t)
	stdout, stderr, code := runCLI(t, a, "export", "--format", "csv")
	assertEq(t, code, 0)
	assertEq(t, stderr, "")
	assertContains(t, stdout, "id,title,column,group,priority,depends,created,done,body\n")
	assertContains(t, stdout, "Paint the hallway")
	assertContains(t, stdout, ",Archived,")

	// The extension chooses the format when --format is not given.
	out := filepath.Join(a, "copy.md")
	stdout, _, code = runCLI(t, a, "export", "--out", out)
	assertEq(t, code, 0)
	assertContains(t, stdout, out)
	text := readFile(t, out)
	assertContains(t, text, "### T-2 Paint the hallway")
	assertContains(t, text, "# Archive")

	md, _, code := runCLI(t, a, "export", "--format", "md")
	assertEq(t, code, 0)
	assertContains(t, md, "### T-2 Paint the hallway")

	csvOut := filepath.Join(a, "board.csv")
	_, _, code = runCLI(t, a, "export", "--out", csvOut)
	assertEq(t, code, 0)
	before := readFile(t, csvOut)
	_, stderr, code = runCLI(t, a, "export", "--out", csvOut)
	assertEq(t, code, 1)
	assertContains(t, stderr, "already exists")
	assertEq(t, readFile(t, csvOut), before)
}

func TestImportCSVBetweenProjects(t *testing.T) {
	a, b := twoProjects(t)
	file := filepath.Join(a, "cards.csv")
	runCLI(t, a, "export", "--out", file)

	stdout, stderr, code := runCLI(t, b, "import", file)
	assertEq(t, code, 0)
	assertEq(t, stderr, "")
	assertContains(t, stdout, "2 created")
	stdout, _, _ = runCLI(t, b, "ls", "--archived")
	assertContains(t, stdout, "Paint the hallway")
	assertContains(t, stdout, "Measure every room")

	// Importing the same file again updates the cards by id.
	stdout, _, code = runCLI(t, b, "import", file, "--json")
	assertEq(t, code, 0)
	assertContains(t, stdout, `"updated":["T-2","T-1"]`)
	assertContains(t, stdout, `"created":[]`)
}

func TestImportCSVNeedsCreateForNewNames(t *testing.T) {
	_, b := twoProjects(t)
	file := filepath.Join(b, "new.csv")
	writeFile(t, file, "title,column,group\nShip it,Review,Ops\n")

	_, stderr, code := runCLI(t, b, "import", file)
	assertEq(t, code, 1)
	assertContains(t, stderr, "Review")
	assertContains(t, stderr, "Ops")
	assertContains(t, stderr, "--create")
	stdout, _, _ := runCLI(t, b, "ls")
	assertNotContains(t, stdout, "Ship it")

	stdout, _, code = runCLI(t, b, "import", file, "--create")
	assertEq(t, code, 0)
	assertContains(t, stdout, "Review")
	assertContains(t, stdout, "Ops")
	stdout, _, _ = runCLI(t, b, "ls")
	assertContains(t, stdout, "Ship it")
}

func TestImportCSVBadRowChangesNothingAndNamesTheLine(t *testing.T) {
	_, b := twoProjects(t)
	file := filepath.Join(b, "bad.csv")
	writeFile(t, file, "title,priority\nGood,P1\nBad,P9\n")
	_, stderr, code := runCLI(t, b, "import", file)
	assertEq(t, code, 1)
	assertContains(t, stderr, "line 3")
	stdout, _, _ := runCLI(t, b, "ls")
	assertNotContains(t, stdout, "Good")
}

func TestImportFlagsMustMatchTheFormat(t *testing.T) {
	a, b := twoProjects(t)
	csvFile := filepath.Join(b, "x.csv")
	writeFile(t, csvFile, "title\nOne\n")
	_, _, code := runCLI(t, b, "import", csvFile, "--replace")
	assertEq(t, code, 2)
	jsonFile := filepath.Join(a, "x.json")
	runCLI(t, a, "export", "--out", jsonFile)
	_, _, code = runCLI(t, b, "import", jsonFile, "--create")
	assertEq(t, code, 2)
	// --format csv works for a file with another extension; markdown cannot be imported.
	txt := filepath.Join(b, "cards.txt")
	writeFile(t, txt, "title\nFrom text\n")
	_, _, code = runCLI(t, b, "import", txt, "--format", "csv")
	assertEq(t, code, 0)
	_, _, code = runCLI(t, b, "import", filepath.Join(a, "copy.md"))
	assertEq(t, code, 2)
}
