package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"waypoint/internal/app"
	"waypoint/internal/domain"
	"waypoint/internal/storage/markdown"
)

func newTestRunner(t *testing.T, cwd string, extra map[string]string) *runner {
	t.Helper()
	env, _, _ := testEnv(t, cwd, extra)
	return &runner{ctx: context.Background(), env: env}
}

func TestNoArgsPrintsUsageAndExitsZero(t *testing.T) {
	stdout, stderr, code := runCLI(t, t.TempDir())
	assertEq(t, code, 0)
	assertEq(t, stderr, "")
	assertContains(t, stdout, "Usage: waypoint")
	assertContains(t, stdout, "project add")
	assertContains(t, stdout, "init")
}

func TestHelpFlagsPrintUsage(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}, {"project", "--help"}, {"project", "add", "--help"}, {"init", "-h"}} {
		stdout, stderr, code := runCLI(t, t.TempDir(), args...)
		assertEq(t, code, 0)
		assertEq(t, stderr, "")
		assertContains(t, stdout, "waypoint")
	}
}

func TestUsageErrors(t *testing.T) {
	cases := [][]string{
		{"frobnicate"},
		{"project"},
		{"project", "frobnicate"},
		{"project", "add"},
		{"project", "add", "A", "B"},
		{"project", "ls", "extra"},
		{"project", "rm"},
		{"project", "ls", "--bogus"},
		{"project", "add", "A", "--path"},
		{"init", "A", "B"},
	}
	for _, args := range cases {
		stdout, stderr, code := runCLI(t, t.TempDir(), args...)
		assertEq(t, code, 2)
		assertEq(t, stdout, "")
		if strings.Count(stderr, "\n") != 1 || !strings.HasPrefix(stderr, "waypoint: ") {
			t.Fatalf("%v: want a one-line message, got %q", args, stderr)
		}
		assertContains(t, stderr, "usage: waypoint")
		mustNotExist(t, sandboxFor(t).config)
	}
}

func TestNoDashesInOutput(t *testing.T) {
	dir := t.TempDir()
	var all strings.Builder
	for _, args := range [][]string{
		{}, {"--help"}, {"project", "--help"}, {"project", "add", "--help"}, {"project", "ls", "--help"},
		{"project", "rm", "--help"}, {"init", "--help"}, {"bogus"}, {"project", "ls"}, {"project", "rm", "x"},
		{"project", "add", "!!!"}, {"project", "add", "A", "--bogus"},
	} {
		o, e, _ := runCLI(t, dir, args...)
		all.WriteString(o + e)
	}
	for _, bad := range []string{"—", "–"} {
		assertNotContains(t, all.String(), bad)
	}
}

func TestFlagsMayFollowPositionals(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.md")
	_, _, code := runCLI(t, dir, "project", "add", "X", "--path", path, "--json")
	assertEq(t, code, 0)
	_, _, code = runCLI(t, dir, "project", "rm", "X", "--json")
	assertEq(t, code, 0)
	_, _, code = runCLI(t, dir, "project", "add", "--json", "X", "--path", path)
	assertEq(t, code, 0)
}

func TestParseInterspersed(t *testing.T) {
	newFS := func() (*flag.FlagSet, *string, *bool) {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		return fs, fs.String("s", "", ""), fs.Bool("b", false, "")
	}
	fs, s, b := newFS()
	pos, err := parseInterspersed(fs, []string{"a", "-s", "v", "b", "-b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, strings.Join(pos, ","), "a,b,c")
	assertEq(t, *s, "v")
	assertEq(t, *b, true)

	// After "--" everything is positional, even what looks like a flag.
	fs, _, _ = newFS()
	pos, err = parseInterspersed(fs, []string{"a", "--", "-b", "-x"})
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, strings.Join(pos, ","), "a,-b,-x")

	fs, _, _ = newFS()
	if _, err = parseInterspersed(fs, []string{"a", "-zzz"}); err == nil {
		t.Fatal("want an error for an unknown flag")
	}
}

func TestErrorCode(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w: x", domain.ErrNotFound), "not_found"},
		{fmt.Errorf("%w: x", domain.ErrUnknownColumn), "unknown_column"},
		{fmt.Errorf("%w: x", domain.ErrMoveNotAllowed), "move_not_allowed"},
		{fmt.Errorf("%w: x", domain.ErrWIPLimit), "wip_limit"},
		{fmt.Errorf("%w: x", domain.ErrConflict), "conflict"},
		{&app.ConflictError{}, "conflict"},
		{fmt.Errorf("%w: x", domain.ErrInvalid), "invalid"},
		{fmt.Errorf("%w: x", domain.ErrUnknownGroup), "invalid"},
		{fmt.Errorf("%w: x", domain.ErrUnknownPriority), "invalid"},
		{fmt.Errorf("file: %w", &markdown.ParseError{Line: 4, Msg: "bad"}), "parse"},
		{fmt.Errorf("file: %w", markdown.ErrNotABoard), "parse"},
		{errors.New("disk on fire"), "error"},
	}
	for _, c := range cases {
		assertEq(t, errorCode(c.err), c.want)
	}
}

func TestServiceResolutionOrder(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	runCLI(t, root, "project", "add", "Alpha", "--path", filepath.Join(a, "BOARD.md"))

	// Exactly one project: it is the answer anywhere.
	r := newTestRunner(t, root, nil)
	_, ref, err := r.service("")
	if err != nil || ref.Name != "Alpha" {
		t.Fatalf("single project: %v %v", ref, err)
	}

	runCLI(t, root, "project", "add", "Beta", "--path", filepath.Join(b, "BOARD.md"))

	// Two projects and a cwd that matches neither: no guess.
	r = newTestRunner(t, root, nil)
	_, _, err = r.service("")
	if err == nil || !strings.Contains(err.Error(), `no project here; use --project NAME or run "waypoint project add"`) {
		t.Fatalf("ambiguous: %v", err)
	}

	// cwd beats nothing; $WAYPOINT_PROJECT beats cwd; the flag beats both.
	r = newTestRunner(t, b, nil)
	_, ref, _ = r.service("")
	assertEq(t, ref.Name, "Beta")

	r = newTestRunner(t, b, map[string]string{"WAYPOINT_PROJECT": "alpha"})
	_, ref, _ = r.service("")
	assertEq(t, ref.Name, "Alpha")

	_, ref, _ = r.service("BETA")
	assertEq(t, ref.Name, "Beta")

	// A named project that does not exist is not_found, not a fallthrough.
	_, _, err = r.service("nope")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown --project: %v", err)
	}
	r = newTestRunner(t, b, map[string]string{"WAYPOINT_PROJECT": "nope"})
	_, _, err = r.service("")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown $WAYPOINT_PROJECT: %v", err)
	}
}

func TestServiceNoProjects(t *testing.T) {
	r := newTestRunner(t, t.TempDir(), nil)
	_, _, err := r.service("")
	if err == nil || !strings.Contains(err.Error(), "no project here") {
		t.Fatalf("got %v", err)
	}
}

func TestServiceReadsBoardThroughRepository(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "BOARD.md")
	writeFile(t, path, boardFile)
	runCLI(t, dir, "project", "add", "Existing", "--path", path)

	r := newTestRunner(t, dir, nil)
	svc, ref, err := r.service("")
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, ref.Path, path)
	bv, err := svc.Board(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, bv.Settings.Name, "Existing")
	assertEq(t, len(bv.Cards), 2)
}
