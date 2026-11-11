package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"waypoint/internal/domain"
)

func newTestRunner(t *testing.T, cwd string, extra map[string]string) *runner {
	t.Helper()
	env, _, _ := testEnv(t, cwd, extra)
	return &runner{ctx: context.Background(), env: env}
}

func TestHelpPrintsUsageListingServeAndTrust(t *testing.T) {
	stdout, stderr, code := runCLI(t, t.TempDir(), "--help")
	assertEq(t, code, 0)
	assertEq(t, stderr, "")
	assertContains(t, stdout, "Usage: waypoint")
	assertContains(t, stdout, "project add")
	assertContains(t, stdout, "init")
	assertContains(t, stdout, "serve")
	assertContains(t, stdout, "trust")
}

func TestHelpFlagsPrintUsage(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}, {"serve", "--help"}, {"trust", "-h"}, {"project", "--help"}, {"project", "add", "--help"}, {"init", "-h"}} {
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
		{"serve", "--help"}, {"trust", "--help"}, {"--help"}, {"project", "--help"}, {"project", "add", "--help"}, {"project", "ls", "--help"},
		{"project", "rm", "--help"}, {"init", "--help"}, {"bogus"}, {"project", "ls"}, {"project", "rm", "x"},
		{"project", "add", "!!!"}, {"project", "add", "A", "--bogus"},
	} {
		o, e, _ := runCLI(t, dir, args...)
		all.WriteString(o)
		all.WriteString(e)
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
	cases := []struct {
		name string
		args []string
		pos  string // positionals joined with ","
		s    string // value of -s
		b    bool   // value of -b
	}{
		{"flags anywhere", []string{"a", "-s", "v", "b", "-b", "c"}, "a,b,c", "v", true},
		{"-- after positional", []string{"a", "--", "-b", "-x"}, "a,-b,-x", "", false},
		{"-- first", []string{"--", "-b", "x"}, "-b,x", "", false},
		{"positional, flag, --", []string{"T-1", "-b", "--", "-5", "-6"}, "T-1,-5,-6", "", true},
		{"flag, --", []string{"-b", "--", "-5", "-6"}, "-5,-6", "", true},
		{"flag, --, same flag literal", []string{"-b", "--", "a", "-b"}, "a,-b", "", true},
		{"literal -- kept after the first", []string{"x", "-b", "--", "a", "--"}, "x,a,--", "", true},
		{"positional, --", []string{"T-1", "--", "-5", "-6"}, "T-1,-5,-6", "", false},
		{"-- is a flag value, not a terminator", []string{"-s", "--", "a", "-b"}, "a", "--", true},
		{"-- after a flag value", []string{"-s", "v", "--", "-b"}, "-b", "v", false},
		{"empty", nil, "", "", false},
	}
	for _, c := range cases {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		s := fs.String("s", "", "")
		b := fs.Bool("b", false, "")
		pos, err := parseInterspersed(fs, c.args)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := strings.Join(pos, ","); got != c.pos || *s != c.s || *b != c.b {
			t.Errorf("%s: pos=%q s=%q b=%v, want pos=%q s=%q b=%v", c.name, got, *s, *b, c.pos, c.s, c.b)
		}
	}

	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if _, err := parseInterspersed(fs, []string{"a", "-zzz"}); err == nil {
		t.Fatal("want an error for an unknown flag")
	}
}

func TestUndefinedFlagHintsAtDoubleDash(t *testing.T) {
	stdout, stderr, code := runCLI(t, t.TempDir(), "project", "rm", "-5")
	assertEq(t, code, 2)
	assertEq(t, stdout, "")
	assertEq(t, strings.Count(stderr, "\n"), 1)
	if !strings.HasSuffix(stderr, " (put -- before values that start with -)\n") {
		t.Fatalf("stderr = %q", stderr)
	}
	// With the terminator the value is a positional, so it is a plain not_found.
	_, stderr, code = runCLI(t, t.TempDir(), "project", "rm", "--", "-5")
	assertEq(t, code, 1)
	assertContains(t, stderr, `no project named "-5"`)

	// Other flag errors carry no hint.
	_, stderr, _ = runCLI(t, t.TempDir(), "project", "add", "A", "--path")
	assertNotContains(t, stderr, "put --")
}

func TestServiceResolutionOrder(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	runCLI(t, root, "project", "add", "Alpha", "--path", filepath.Join(a, "BOARD.md"))

	// A project below the working directory is not the answer.
	r := newTestRunner(t, root, nil)
	_, ref, err := r.service("")
	if err == nil {
		t.Fatalf("parent folder reached %v", ref)
	}

	runCLI(t, root, "project", "add", "Beta", "--path", filepath.Join(b, "BOARD.md"))

	// Nor is either of two.
	r = newTestRunner(t, root, nil)
	_, _, err = r.service("")
	if err == nil || !strings.Contains(err.Error(), `no project here; use --project NAME or run "waypoint init"`) {
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

func TestVersionCommand(t *testing.T) {
	old := Version
	Version = "v9.9.9"
	t.Cleanup(func() { Version = old })
	for _, args := range [][]string{{"version"}, {"--version"}, {"-v"}} {
		stdout, stderr, code := runCLI(t, t.TempDir(), args...)
		assertEq(t, code, 0)
		assertEq(t, stderr, "")
		assertContains(t, stdout, "waypoint v9.9.9")
		assertContains(t, stdout, runtime.GOOS+"/"+runtime.GOARCH)
	}
	stdout, _, code := runCLI(t, t.TempDir(), "version", "--json")
	assertEq(t, code, 0)
	assertContains(t, stdout, `"version":"v9.9.9"`)
	// Asking for the version needs no config and writes none.
	mustNotExist(t, sandboxFor(t).config)
	// It is listed in the help.
	help, _, _ := runCLI(t, t.TempDir(), "--help")
	assertContains(t, help, "version")
}
