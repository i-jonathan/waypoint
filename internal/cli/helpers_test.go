package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// sandbox is one test's private home directory and config file. Nothing in a
// test reaches the real home directory or the real config.
type sandbox struct {
	home   string
	config string
}

var (
	sandboxMu sync.Mutex
	sandboxes = map[*testing.T]*sandbox{}
)

// sandboxFor returns the test's sandbox, creating it on first use so that
// several runCLI calls in one test share one config.
func sandboxFor(t *testing.T) *sandbox {
	t.Helper()
	sandboxMu.Lock()
	defer sandboxMu.Unlock()
	if sb, ok := sandboxes[t]; ok {
		return sb
	}
	home := t.TempDir()
	sb := &sandbox{home: home, config: filepath.Join(home, "cfg", "config.yaml")}
	sandboxes[t] = sb
	t.Cleanup(func() {
		sandboxMu.Lock()
		delete(sandboxes, t)
		sandboxMu.Unlock()
	})
	return sb
}

// testNow is the fixed clock: 2026-11-03.
func testNow() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) }

// testEnv builds an Env over the test's sandbox. Getenv answers only from the
// sandbox and extra, never from the process environment.
func testEnv(t *testing.T, cwd string, extra map[string]string) (Env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	sb := sandboxFor(t)
	vars := map[string]string{"WAYPOINT_CONFIG": sb.config, "HOME": sb.home}
	for k, v := range extra {
		vars[k] = v
	}
	var out, errb bytes.Buffer
	return Env{
		Stdin:  strings.NewReader(""),
		Stdout: &out,
		Stderr: &errb,
		Getenv: func(k string) string { return vars[k] },
		Cwd:    cwd,
		Now:    testNow,
	}, &out, &errb
}

// runCLI runs the CLI in cwd with the test's sandboxed home and config.
func runCLI(t *testing.T, cwd string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	return runCLIEnv(t, cwd, nil, args...)
}

// runCLIEnv is runCLI with extra environment variables.
func runCLIEnv(t *testing.T, cwd string, extra map[string]string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	env, out, errb := testEnv(t, cwd, extra)
	code = Run(context.Background(), args, env)
	return out.String(), errb.String(), code
}

// readFile returns a file's contents, or "" if it does not exist.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err == nil {
		t.Fatalf("expected %s not to exist", path)
	}
}

func assertEq[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func assertContains(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Fatalf("%q does not contain %q", s, sub)
	}
}

func assertNotContains(t *testing.T, s, sub string) {
	t.Helper()
	if strings.Contains(s, sub) {
		t.Fatalf("%q unexpectedly contains %q", s, sub)
	}
}

// boardFile is a small valid Waypoint board with two cards.
const boardFile = `---
waypoint: 1
name: Existing
prefix: X
next_id: 3
columns:
  - { name: Todo, moves_to: [Done] }
  - { name: Done, done: true, moves_to: [Todo] }
---

## Todo

### X-1 First card

- created: 2026-11-01

Body of the first card.

### X-2 Second card

- priority: P1
- created: 2026-11-02

## Done
`
