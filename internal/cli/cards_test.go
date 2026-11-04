package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// newProject makes a directory with a registered ./BOARD.md (default settings,
// prefix T) and returns the directory.
func newProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, stderr, code := runCLI(t, dir, "init", "demo"); code != 0 {
		t.Fatalf("init: %d %s", code, stderr)
	}
	path := filepath.Join(dir, "BOARD.md")
	groups := "groups:\n"
	for _, g := range []string{"core", "alpha", "beta", "g", "security", "ui"} {
		groups += "  - {name: " + g + "}\n"
	}
	writeFile(t, path, strings.Replace(readFile(t, path), "columns:", groups+"columns:", 1))
	return dir
}

// runIn is runCLI that expects success and returns stdout; stderr must be empty.
func runIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	stdout, stderr, code := runCLI(t, dir, args...)
	if code != 0 || stderr != "" {
		t.Fatalf("%v: exit %d, stderr %q", args, code, stderr)
	}
	return stdout
}

// runStdin runs the CLI with the given standard input.
func runStdin(t *testing.T, dir, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	env, out, errb := testEnv(t, dir, nil)
	env.Stdin = strings.NewReader(stdin)
	code = Run(context.Background(), args, env)
	return out.String(), errb.String(), code
}

func lines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func TestAddLsShow(t *testing.T) {
	dir := newProject(t)
	assertEq(t, runIn(t, dir, "add", "First card", "--priority", "P0", "--group", "core"), "Created T-1 in Todo\n")
	runIn(t, dir, "add", "Second", "--body", "Some body text")
	runIn(t, dir, "add", "Third", "--column", "In progress")

	got := lines(runIn(t, dir, "ls"))
	want := []string{
		"T-1  Todo         P0  core  First card  [NEXT]",
		"T-2  Todo                   Second",
		"T-3  In progress            Third",
	}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i := range want {
		assertEq(t, got[i], want[i])
	}

	show := runIn(t, dir, "show", "T-2")
	assertEq(t, show, "T-2 Second\ncolumn: Todo\ncreated: 2026-11-03\n\nSome body text\n")

	show = runIn(t, dir, "show", "T-1")
	assertEq(t, show, "T-1 First card\ncolumn: Todo\ngroup: core\npriority: P0\ncreated: 2026-11-03\n")
}

func TestLsAlignmentUsesPrintedRows(t *testing.T) {
	dir := newProject(t)
	runIn(t, dir, "add", "Wide", "--column", "In progress", "--priority", "P1")
	runIn(t, dir, "add", "Narrow")
	// Only the Todo card is printed, so "In progress" must not set the width.
	assertEq(t, runIn(t, dir, "ls", "narrow"), "T-2  Todo  Narrow  [NEXT]\n")
}

func TestLsEmptyMessages(t *testing.T) {
	dir := newProject(t)
	assertEq(t, runIn(t, dir, "ls"), "No cards yet. Add one with: waypoint add \"Title\"\n")
	assertEq(t, runIn(t, dir, "ls", "--json"), "[]\n")
	runIn(t, dir, "add", "Only")
	assertEq(t, runIn(t, dir, "ls", "zzz"), "No cards match.\n")
	assertEq(t, runIn(t, dir, "ls", "zzz", "--json"), "[]\n")
}

func TestLsTags(t *testing.T) {
	dir := newProject(t)
	runIn(t, dir, "add", "A")
	runIn(t, dir, "add", "B", "--depends", "T-1,T-99")
	runIn(t, dir, "add", "C", "--depends", "T-1")
	got := lines(runIn(t, dir, "ls", "blocked"))
	assertEq(t, len(got), 2)
	assertEq(t, got[0], "T-2  Todo  B  [BLOCKED by T-1, T-99?]")
	assertEq(t, got[1], "T-3  Todo  C  [BLOCKED by T-1]")
}

func TestLsFlags(t *testing.T) {
	dir := newProject(t)
	runIn(t, dir, "add", "A", "--group", "alpha")
	runIn(t, dir, "add", "B", "--column", "In progress", "--group", "beta")
	runIn(t, dir, "add", "C", "--depends", "T-1")

	assertEq(t, runIn(t, dir, "ls", "--column", "In progress"), "T-2  In progress  beta  B\n")
	assertEq(t, runIn(t, dir, "ls", "--group", "alpha"), "T-1  Todo  alpha  A  [NEXT]\n")
	assertEq(t, runIn(t, dir, "ls", "--blocked"), "T-3  Todo  C  [BLOCKED by T-1]\n")
	assertEq(t, runIn(t, dir, "ls", "--next"), "T-1  Todo  alpha  A  [NEXT]\n")
	// Flags and positional terms combine, flags may come first.
	assertEq(t, runIn(t, dir, "ls", "--group", "beta", "b"), "T-2  In progress  beta  B\n")
}

func TestLsNextWithNoNextCard(t *testing.T) {
	dir := newProject(t)
	runIn(t, dir, "add", "A")
	runIn(t, dir, "done", "T-1")
	assertEq(t, runIn(t, dir, "ls", "--next"), "No cards match.\n")
	assertEq(t, runIn(t, dir, "ls", "--next", "--json"), "[]\n")
}

func TestMoveTopAndRules(t *testing.T) {
	dir := newProject(t)
	for _, title := range []string{"One", "Two", "Three"} {
		runIn(t, dir, "add", title)
	}
	assertEq(t, runIn(t, dir, "move", "T-3", "Todo", "--top"), "Moved T-3 to Todo\n")
	got := lines(runIn(t, dir, "ls"))
	assertContains(t, got[0], "T-3")

	assertEq(t, runIn(t, dir, "move", "T-3", "Todo", "--index", "1"), "Moved T-3 to Todo\n")
	assertContains(t, lines(runIn(t, dir, "ls"))[1], "T-3")
	assertEq(t, runIn(t, dir, "move", "T-1", "Todo"), "Moved T-1 to Todo\n")
	assertContains(t, lines(runIn(t, dir, "ls"))[2], "T-1")

	// Case-insensitive column.
	assertEq(t, runIn(t, dir, "move", "T-2", "in progress"), "Moved T-2 to In progress\n")
	assertEq(t, runIn(t, dir, "move", "T-2", "done"), "Moved T-2 to Done\n")

	// Move rules from the board file.
	path := filepath.Join(dir, "BOARD.md")
	board := readFile(t, path)
	writeFile(t, path, strings.Replace(board, "{name: Todo, moves_to: [In progress, Done]}", "{name: Todo, moves_to: [In progress]}", 1))
	stdout, stderr, code := runCLI(t, dir, "move", "T-1", "Done")
	assertEq(t, code, 1)
	assertEq(t, stdout, "")
	assertEq(t, stderr, "waypoint: move not allowed: Todo cannot move to Done\n")
}

func TestMoveUsageAndColumnMatching(t *testing.T) {
	dir := newProject(t)
	runIn(t, dir, "add", "One")
	for _, args := range [][]string{
		{"move", "T-1", "Done", "--top", "--index", "0"},
		{"move", "T-1", "Done", "--index", "x"},
		{"move", "T-1", "Done", "--index", "-1"},
		{"move", "T-1"},
	} {
		stdout, stderr, code := runCLI(t, dir, args...)
		assertEq(t, code, 2)
		assertEq(t, stdout, "")
		assertContains(t, stderr, "waypoint: ")
	}
	out, _, code := runCLI(t, dir, "move", "T-1", "nowhere", "--json")
	assertEq(t, code, 1)
	assertContains(t, out, `"code":"unknown_column"`)

	// An ambiguous case-insensitive match is an unknown column.
	path := filepath.Join(dir, "BOARD.md")
	board := strings.Replace(readFile(t, path), "  - {name: Done,", "  - {name: DONE, moves_to: [Todo]}\n  - {name: Done,", 1)
	writeFile(t, path, strings.Replace(board, "moves_to: [In progress, Done]}", "moves_to: [In progress, Done, DONE]}", 1))
	out, _, code = runCLI(t, dir, "move", "T-1", "done", "--json")
	assertEq(t, code, 1)
	assertContains(t, out, `"code":"unknown_column"`)
	// An exact match still wins.
	assertEq(t, runIn(t, dir, "move", "T-1", "DONE"), "Moved T-1 to DONE\n")
}

func TestSetFieldsAndDepends(t *testing.T) {
	dir := newProject(t)
	runIn(t, dir, "add", "One")
	runIn(t, dir, "add", "Two")
	assertEq(t, runIn(t, dir, "set", "T-2", "priority=p1", "owner=me", "depends=T-1"), "Updated T-2\n")
	assertEq(t, runIn(t, dir, "ls", "--blocked"), "T-2  Todo  P1  Two  [BLOCKED by T-1]\n")
	assertContains(t, runIn(t, dir, "show", "T-2"), "owner: me\n")
	assertContains(t, runIn(t, dir, "show", "T-2"), "blocked by: T-1\n")

	runIn(t, dir, "set", "T-2", "owner=")
	assertNotContains(t, runIn(t, dir, "show", "T-2"), "owner")

	_, stderr, code := runCLI(t, dir, "set", "T-2", "depends=T-2")
	assertEq(t, code, 1)
	assertContains(t, stderr, "cannot depend on itself")

	runIn(t, dir, "set", "T-2", "depends= T-1 , ,T-3 ", "title=Renamed", "priority=")
	got := runIn(t, dir, "show", "T-2")
	assertContains(t, got, "T-2 Renamed\n")
	assertContains(t, got, "depends: T-1, T-3\n")
	assertNotContains(t, got, "priority")

	runIn(t, dir, "set", "T-2", "depends=")
	assertNotContains(t, runIn(t, dir, "show", "T-2"), "depends")
}

func TestSetIsOneWrite(t *testing.T) {
	// A bad later argument leaves the card untouched: the patch is whole or not at all.
	dir := newProject(t)
	runIn(t, dir, "add", "One")
	_, _, code := runCLI(t, dir, "set", "T-1", "title=Changed", "priority=P9")
	assertEq(t, code, 1)
	assertContains(t, runIn(t, dir, "show", "T-1"), "T-1 One\n")
}

func TestSetErrors(t *testing.T) {
	dir := newProject(t)
	runIn(t, dir, "add", "One")

	_, stderr, code := runCLI(t, dir, "set", "T-1", "novalue")
	assertEq(t, code, 2)
	assertContains(t, stderr, "novalue")
	_, _, code = runCLI(t, dir, "set", "T-1")
	assertEq(t, code, 2)

	for _, kv := range []string{"done=2026-01-01", "created=2026-01-01"} {
		out, stderr, code := runCLI(t, dir, "set", "T-1", kv, "--json")
		assertEq(t, code, 1)
		assertContains(t, out, `"code":"invalid"`)
		assertEq(t, stderr, "")
	}
	_, stderr, _ = runCLI(t, dir, "set", "T-1", "done=x")
	assertEq(t, stderr, "waypoint: invalid: done is set by moving the card\n")

	_, stderr, code = runCLI(t, dir, "set", "T-1", "title=")
	assertEq(t, code, 1)
	assertContains(t, stderr, "invalid")
}

func TestBodyFromStdin(t *testing.T) {
	dir := newProject(t)
	stdout, stderr, code := runStdin(t, dir, "line one\nline two\n", "add", "Piped", "--body", "-")
	assertEq(t, code, 0)
	assertEq(t, stderr, "")
	assertEq(t, stdout, "Created T-1 in Todo\n")
	assertContains(t, runIn(t, dir, "show", "T-1"), "\nline one\nline two\n")

	_, _, code = runStdin(t, dir, "replaced\n", "set", "T-1", "body=-")
	assertEq(t, code, 0)
	got := runIn(t, dir, "show", "T-1")
	assertContains(t, got, "replaced")
	assertNotContains(t, got, "line one")

	// A literal body that is not "-" is used as is.
	runIn(t, dir, "set", "T-1", "body=inline text")
	assertContains(t, runIn(t, dir, "show", "T-1"), "\ninline text\n")
}

func TestStdinReadTwiceIsUsageError(t *testing.T) {
	dir := newProject(t)
	runIn(t, dir, "add", "One")
	stdout, stderr, code := runStdin(t, dir, "x", "set", "T-1", "body=-", "body=-")
	assertEq(t, code, 2)
	assertEq(t, stdout, "")
	assertContains(t, stderr, "standard input")
	assertContains(t, runIn(t, dir, "show", "T-1"), "T-1 One\n")
}

func TestCheck(t *testing.T) {
	dir := newProject(t)
	runIn(t, dir, "add", "List", "--body", "- [ ] a\n- [ ] b\n- [x] c")
	assertEq(t, runIn(t, dir, "check", "T-1", "2"), "Checked item 2 of T-1\n")
	assertEq(t, runIn(t, dir, "check", "T-1", "2"), "Unchecked item 2 of T-1\n")
	assertEq(t, runIn(t, dir, "check", "T-1", "3"), "Unchecked item 3 of T-1\n")
	assertContains(t, runIn(t, dir, "show", "T-1"), "checklist: 0/3\n")
	runIn(t, dir, "check", "T-1", "1")
	assertContains(t, runIn(t, dir, "show", "T-1"), "checklist: 1/3\n")

	for _, n := range []string{"0", "-1", "x", "1.5"} {
		_, stderr, code := runCLI(t, dir, "check", "T-1", "--", n)
		assertEq(t, code, 2)
		assertContains(t, stderr, "waypoint: ")
	}
	_, _, code := runCLI(t, dir, "check", "T-1", "9")
	assertEq(t, code, 1)
	out, _, _ := runCLI(t, dir, "check", "T-1", "1", "--json")
	assertContains(t, out, `"checklist":{"done":0,"total":3}`)
}

func TestDoneArchiveRestore(t *testing.T) {
	dir := newProject(t)
	runIn(t, dir, "add", "One")
	runIn(t, dir, "add", "Two")
	assertEq(t, runIn(t, dir, "archive"), "Nothing to archive\n")
	assertEq(t, runIn(t, dir, "done", "T-1"), "Moved T-1 to Done\n")
	assertEq(t, runIn(t, dir, "archive"), "Archived 1 card\n")
	assertNotContains(t, runIn(t, dir, "ls"), "T-1")
	archived := runIn(t, dir, "ls", "--archived")
	assertContains(t, archived, "T-1")
	assertContains(t, archived, "[ARCHIVED]")
	assertContains(t, runIn(t, dir, "show", "T-1"), "archived: yes\n")

	assertEq(t, runIn(t, dir, "restore", "T-1"), "Restored T-1 to Todo\n")
	assertContains(t, runIn(t, dir, "ls"), "T-1")
	assertNotContains(t, runIn(t, dir, "show", "T-1"), "archived")

	runIn(t, dir, "done", "T-1")
	runIn(t, dir, "done", "T-2")
	assertEq(t, runIn(t, dir, "archive"), "Archived 2 cards\n")
	assertEq(t, runIn(t, dir, "restore", "T-2", "--column", "in progress"), "Restored T-2 to In progress\n")
}

func TestArchiveJSONAndOff(t *testing.T) {
	dir := newProject(t)
	runIn(t, dir, "add", "One")
	assertEq(t, runIn(t, dir, "archive", "--json"), "{\"archived\":[]}\n")
	runIn(t, dir, "done", "T-1")
	assertEq(t, runIn(t, dir, "archive", "--json"), "{\"archived\":[\"T-1\"]}\n")

	path := filepath.Join(dir, "BOARD.md")
	board := readFile(t, path)
	if !strings.Contains(board, "archive:") {
		board = strings.Replace(board, "columns:", "archive: {mode: off, after_days: 14}\ncolumns:", 1)
	}
	writeFile(t, path, board)
	_, stderr, code := runCLI(t, dir, "archive")
	assertEq(t, code, 1)
	assertContains(t, stderr, "archiving is off")
}

func TestRm(t *testing.T) {
	dir := newProject(t)
	runIn(t, dir, "add", "One")
	runIn(t, dir, "add", "Two")
	assertEq(t, runIn(t, dir, "rm", "T-1"), "Deleted T-1\n")
	assertEq(t, runIn(t, dir, "rm", "T-2", "--json"), "{\"deleted\":\"T-2\"}\n")
	_, stderr, code := runCLI(t, dir, "rm", "T-2")
	assertEq(t, code, 1)
	assertContains(t, stderr, "not found")
	// Ids are not reused.
	assertEq(t, runIn(t, dir, "add", "Three"), "Created T-3 in Todo\n")
}

func TestJSONOutput(t *testing.T) {
	dir := newProject(t)
	runIn(t, dir, "add", "One", "--group", "g", "--priority", "P0", "--body", "- [x] a\n- [ ] b")
	runIn(t, dir, "add", "Two", "--depends", "T-1")
	runIn(t, dir, "set", "T-2", "zeta=1", "alpha=2")

	out := runIn(t, dir, "ls", "--json")
	var cards []map[string]any
	if err := json.Unmarshal([]byte(out), &cards); err != nil {
		t.Fatal(err, out)
	}
	assertEq(t, len(cards), 2)
	// Keys in the documented order.
	wantKeys := `"id","title","column","group","priority","depends","created","done","body","extra","archived","blockers","next","checklist","hash"`
	dec := json.NewDecoder(strings.NewReader(out))
	var keys []string
	dec.Token() // [
	dec.Token() // {
	for dec.More() {
		k, _ := dec.Token()
		keys = append(keys, `"`+k.(string)+`"`)
		var skip json.RawMessage
		dec.Decode(&skip)
	}
	assertEq(t, strings.Join(keys, ","), wantKeys)

	assertEq(t, cards[0]["id"], "T-1")
	assertEq(t, cards[0]["next"], true)
	assertEq(t, cards[0]["created"], "2026-11-03")
	assertEq(t, cards[0]["done"], nil)
	assertEq(t, cards[0]["checklist"].(map[string]any)["total"], any(float64(2)))
	assertEq(t, len(cards[0]["blockers"].([]any)), 0)
	assertEq(t, len(cards[0]["depends"].([]any)), 0)
	assertEq(t, len(cards[0]["extra"].([]any)), 0)
	b := cards[1]["blockers"].([]any)[0].(map[string]any)
	assertEq(t, b["id"], "T-1")
	assertEq(t, b["missing"], false)

	var one map[string]any
	if err := json.Unmarshal([]byte(runIn(t, dir, "show", "T-2", "--json")), &one); err != nil {
		t.Fatal(err)
	}
	assertEq(t, len(one["extra"].([]any)), 2)
	for _, args := range [][]string{{"add", "Three"}, {"move", "T-3", "Done"}, {"set", "T-3", "group=g"}, {"check", "T-1", "2"}, {"done", "T-2"}} {
		var v map[string]any
		if err := json.Unmarshal([]byte(runIn(t, dir, append(args, "--json")...)), &v); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		assertEq(t, v["hash"] != "", true)
	}
}

func TestIfMatchConflict(t *testing.T) {
	dir := newProject(t)
	runIn(t, dir, "add", "One")
	stdout, _, code := runCLI(t, dir, "set", "T-1", "title=x", "--if-match", "deadbeef", "--json")
	assertEq(t, code, 1)
	assertContains(t, stdout, `"code":"conflict"`)
	assertContains(t, runIn(t, dir, "show", "T-1"), "T-1 One\n")

	var cv struct{ Hash string }
	if err := json.Unmarshal([]byte(runIn(t, dir, "show", "T-1", "--json")), &cv); err != nil {
		t.Fatal(err)
	}
	assertEq(t, runIn(t, dir, "set", "T-1", "title=ok", "--if-match", cv.Hash), "Updated T-1\n")
}

func TestQueryPassthrough(t *testing.T) {
	dir := newProject(t)
	runIn(t, dir, "add", "Harden auth", "--priority", "P0", "--group", "security")
	runIn(t, dir, "add", "Other", "--priority", "P0", "--group", "ui")
	runIn(t, dir, "add", "Quiet", "--group", "security")
	got := lines(runIn(t, dir, "ls", "p0", "group:security"))
	assertEq(t, len(got), 1)
	assertContains(t, got[0], "T-1")
	// A quoted phrase passed as one argument stays one term.
	assertContains(t, runIn(t, dir, "ls", "harden auth"), "T-1")
	assertEq(t, runIn(t, dir, "ls", "harden other"), "No cards match.\n")
	// After -- everything is a query term.
	assertContains(t, runIn(t, dir, "ls", "--", "group:security"), "T-1")
}

func TestWarningsGoToStderr(t *testing.T) {
	dir := newProject(t)
	runIn(t, dir, "add", "One", "--group", "g")
	path := filepath.Join(dir, "BOARD.md")
	writeFile(t, path, strings.Replace(readFile(t, path), "- group: g", "- group: mystery", 1))

	stdout, stderr, code := runCLI(t, dir, "ls")
	assertEq(t, code, 0)
	assertNotContains(t, stdout, "mystery\"")
	assertContains(t, stderr, "T-1 has unknown group")

	stdout, stderr, code = runCLI(t, dir, "ls", "--json")
	assertEq(t, code, 0)
	var cards []any
	if err := json.Unmarshal([]byte(stdout), &cards); err != nil {
		t.Fatalf("stdout is not clean JSON: %v\n%s", err, stdout)
	}
	assertContains(t, stderr, "T-1 has unknown group")

	// A write that succeeds still reports what is wrong with the board.
	stdout, stderr, code = runCLI(t, dir, "set", "T-1", "owner=me")
	assertEq(t, code, 0)
	assertEq(t, stdout, "Updated T-1\n")
	assertContains(t, stderr, "T-1 has unknown group")
}

func TestCardCommandUsageErrors(t *testing.T) {
	dir := newProject(t)
	cases := [][]string{
		{"show"}, {"show", "T-1", "T-2"},
		{"add"}, {"add", "A", "B"},
		{"set"}, {"check"}, {"check", "T-1"}, {"done"}, {"archive", "T-1"},
		{"restore"}, {"rm"}, {"ls", "--bogus"},
	}
	for _, args := range cases {
		stdout, stderr, code := runCLI(t, dir, args...)
		assertEq(t, code, 2)
		assertEq(t, stdout, "")
		if strings.Count(stderr, "\n") != 1 || !strings.HasPrefix(stderr, "waypoint: ") {
			t.Fatalf("%v: want a one-line message, got %q", args, stderr)
		}
	}
}

func TestUsageListsCardCommands(t *testing.T) {
	var out bytes.Buffer
	env, _, _ := testEnv(t, t.TempDir(), nil)
	env.Stdout = &out
	Run(context.Background(), nil, env)
	for _, c := range []string{"ls ", "show ", "add ", "move ", "set ", "check ", "done ", "archive", "restore ", "rm "} {
		assertContains(t, out.String(), "\n  "+c)
	}
}

func TestShowMissingCard(t *testing.T) {
	dir := newProject(t)
	out, _, code := runCLI(t, dir, "show", "T-9", "--json")
	assertEq(t, code, 1)
	assertContains(t, out, `"code":"not_found"`)
}
