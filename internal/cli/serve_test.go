package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"waypoint/internal/server"
)

func init() {
	// No test may change the machine's trust store. Tests that exercise
	// "waypoint trust" install their own stub.
	installCA = func(server.Certs) error {
		panic("unexpected attempt to trust a CA in a test")
	}
}

// lockedBuffer is a bytes.Buffer that can be written by the server while the
// test reads it.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// served is a "waypoint serve" running in the background.
type served struct {
	t      *testing.T
	stdout *lockedBuffer
	stderr *lockedBuffer
	cancel context.CancelFunc
	done   chan int
	opened chan string // URLs passed to OpenURL
	url    string
	port   int
	code   int
	exited bool
}

// startServe runs the CLI with args in the test's sandbox. OpenURL only
// records; no browser is ever started.
func startServe(t *testing.T, args ...string) *served {
	t.Helper()
	env, _, _ := testEnv(t, t.TempDir(), nil)
	s := &served{t: t, stdout: &lockedBuffer{}, stderr: &lockedBuffer{}, done: make(chan int, 1), opened: make(chan string, 4)}
	env.Stdout, env.Stderr = s.stdout, s.stderr
	env.OpenURL = func(u string) error { s.opened <- u; return nil }
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go func() { s.done <- Run(ctx, args, env) }()
	t.Cleanup(func() { s.stop() })

	re := regexp.MustCompile(`Waypoint running at (https?://localhost:(\d+)/)\n`)
	deadline := time.After(5 * time.Second)
	for {
		if m := re.FindStringSubmatch(s.stdout.String()); m != nil {
			s.url = m[1]
			fmt.Sscan(m[2], &s.port)
			return s
		}
		select {
		case code := <-s.done:
			s.exited, s.code = true, code
			t.Fatalf("serve exited %d before it was ready; stdout %q stderr %q", code, s.stdout.String(), s.stderr.String())
		case <-deadline:
			t.Fatalf("serve never became ready; stdout %q stderr %q", s.stdout.String(), s.stderr.String())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// stop ends the server as SIGINT would and returns its exit code.
func (s *served) stop() int {
	s.t.Helper()
	s.cancel()
	if !s.exited {
		select {
		case s.code = <-s.done:
			s.exited = true
		case <-time.After(10 * time.Second):
			s.t.Error("serve did not stop")
		}
	}
	return s.code
}

func (s *served) get(path string) *http.Response {
	s.t.Helper()
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d%s", s.port, path))
	if err != nil {
		s.t.Fatal(err)
	}
	s.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func writeConfig(t *testing.T, yaml string) {
	t.Helper()
	writeFile(t, sandboxFor(t).config, yaml)
}

func TestServeAnswersAPIAndUIOnTheRealPort(t *testing.T) {
	s := startServe(t, "serve", "--port", "0", "--no-open")
	if s.port == 0 {
		t.Fatalf("url %q names port 0", s.url)
	}
	assertEq(t, s.url, fmt.Sprintf("http://localhost:%d/", s.port))

	// The Host guard compares against the port actually listened on.
	resp := s.get("/api/projects")
	assertEq(t, resp.StatusCode, 200)
	assertContains(t, resp.Header.Get("Content-Type"), "application/json")
	assertEq(t, resp.Header.Get("X-Content-Type-Options"), "nosniff")

	resp = s.get("/p/Demo/card/T-1")
	body, _ := io.ReadAll(resp.Body)
	assertEq(t, resp.StatusCode, 200)
	assertContains(t, string(body), "<title>Waypoint</title>") // the placeholder or the built index
	assertEq(t, resp.Header.Get("X-Content-Type-Options"), "nosniff")

	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/", s.port), nil)
	req.Host = "evil.example"
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	assertEq(t, resp2.StatusCode, http.StatusMisdirectedRequest)

	assertEq(t, s.stop(), 0)
}

func TestServeSpeaksH2CAndPrintsTheTipOnce(t *testing.T) {
	s := startServe(t, "serve", "--port", "0", "--no-open")
	var p http.Protocols
	p.SetUnencryptedHTTP2(true)
	c := &http.Client{Transport: &http.Transport{Protocols: &p}}
	resp, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/api/projects", s.port))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	assertEq(t, resp.ProtoMajor, 2)

	s.stop()
	assertEq(t, strings.Count(s.stderr.String(), `Tip: run "waypoint trust" once to serve over HTTPS with HTTP/2.`), 1)
	assertEq(t, strings.Count(s.stdout.String(), "Tip:"), 0)
}

func TestServeTLSOnServesHTTP2WithoutATip(t *testing.T) {
	writeConfig(t, "tls: on\n")
	s := startServe(t, "serve", "--port", "0", "--no-open")
	assertContains(t, s.url, "https://localhost:")
	assertNotContains(t, s.stderr.String(), "Tip:")

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM([]byte(readFile(t, filepath.Join(sandboxFor(t).home, ".local", "share", "waypoint", "tls", "ca.pem"))))
	c := &http.Client{Transport: &http.Transport{ForceAttemptHTTP2: true, TLSClientConfig: &tls.Config{RootCAs: pool}}}
	resp, err := c.Get(fmt.Sprintf("https://127.0.0.1:%d/api/projects", s.port))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	assertEq(t, resp.ProtoMajor, 2)
	assertEq(t, resp.StatusCode, 200)
}

func TestServeOpensTheBrowser(t *testing.T) {
	s := startServe(t, "serve", "--port", "0")
	select {
	case u := <-s.opened:
		assertEq(t, u, s.url)
	case <-time.After(2 * time.Second):
		t.Fatal("the browser was not opened")
	}
}

func TestServeDoesNotOpenTheBrowserWhenNotWanted(t *testing.T) {
	cases := map[string]struct {
		config string
		args   []string
	}{
		"flag":   {"", []string{"serve", "--port", "0", "--no-open"}},
		"config": {"open_browser: false\n", []string{"serve", "--port", "0"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if c.config != "" {
				writeConfig(t, c.config)
			}
			s := startServe(t, c.args...)
			s.stop()
			select {
			case u := <-s.opened:
				t.Fatalf("opened %q", u)
			default:
			}
		})
	}
}

func TestServeNeverOpensWithoutAnOpener(t *testing.T) {
	// main.go sets OpenURL; an Env without one must not crash.
	env, _, _ := testEnv(t, t.TempDir(), nil)
	out := &lockedBuffer{}
	env.Stdout = out
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- Run(ctx, []string{"serve", "--port", "0"}, env) }()
	waitUntil(t, func() bool { return strings.Contains(out.String(), "Waypoint running at") })
	cancel()
	assertEq(t, <-done, 0)
}

func TestNoArgsRunsServe(t *testing.T) {
	writeConfig(t, "port: 0\n")
	s := startServe(t)
	assertEq(t, s.get("/api/projects").StatusCode, 200)
	select {
	case <-s.opened: // open_browser defaults to true
	case <-time.After(2 * time.Second):
		t.Fatal("no-args serve did not honour open_browser")
	}
	assertEq(t, s.stop(), 0)
}

func TestServeUsesTheConfiguredPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	free := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	writeConfig(t, fmt.Sprintf("port: %d\n", free))
	s := startServe(t, "serve", "--no-open")
	assertEq(t, s.port, free)
	assertEq(t, s.get("/api/projects").StatusCode, 200)
}

func TestServePortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	stdout, stderr, code := runCLI(t, t.TempDir(), "serve", "--port", fmt.Sprint(port), "--no-open")
	assertEq(t, code, 1)
	assertEq(t, stdout, "")
	assertContains(t, stderr, fmt.Sprintf(`port %d is in use; set "port" in CONFIG or pass --port`, port))
}

func TestServeRefusesPorts80And443(t *testing.T) {
	for _, p := range []string{"80", "443"} {
		stdout, stderr, code := runCLI(t, t.TempDir(), "serve", "--port", p, "--no-open")
		assertEq(t, code, 1)
		assertEq(t, stdout, "")
		assertContains(t, stderr, "port "+p+" is not supported")
		assertContains(t, stderr, "Host")
	}
	writeConfig(t, "port: 443\n")
	_, stderr, code := runCLI(t, t.TempDir(), "serve", "--no-open")
	assertEq(t, code, 1)
	assertContains(t, stderr, "port 443 is not supported")
}

func TestServeBadPortIsAUsageError(t *testing.T) {
	_, stderr, code := runCLI(t, t.TempDir(), "serve", "--port", "abc", "--no-open")
	assertEq(t, code, 2)
	assertContains(t, stderr, "usage: waypoint serve")
	_, stderr, code = runCLI(t, t.TempDir(), "serve", "--port", "70000", "--no-open")
	assertEq(t, code, 1)
	assertContains(t, stderr, "out of range")
	_, _, code = runCLI(t, t.TempDir(), "serve", "extra")
	assertEq(t, code, 2)
}

func TestServeReportsABadConfig(t *testing.T) {
	writeConfig(t, "tls: sometimes\n")
	_, stderr, code := runCLI(t, t.TempDir(), "serve", "--no-open")
	assertEq(t, code, 1)
	assertContains(t, stderr, "tls")
}

func TestServeShutsDownWithAnOpenEventStream(t *testing.T) {
	s := startServe(t, "serve", "--port", "0", "--no-open")
	resp := s.get("/api/events")
	assertEq(t, resp.StatusCode, 200)
	began := time.Now()
	assertEq(t, s.stop(), 0)
	if took := time.Since(began); took > 2*time.Second {
		t.Fatalf("shutdown took %v with a tab open", took)
	}
}

// sseEvents reads an event stream and sends each "event:" name's data line.
func sseEvents(t *testing.T, resp *http.Response) <-chan string {
	t.Helper()
	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if line := sc.Text(); strings.HasPrefix(line, "data: ") {
				ch <- strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	return ch
}

func TestServeWatchesProjectsRegisteredAfterStart(t *testing.T) {
	s := startServe(t, "serve", "--port", "0", "--no-open")
	events := sseEvents(t, s.get("/api/events"))

	dir := newProject(t) // registers "demo" in the config the server re-reads
	deadline := time.After(10 * time.Second)
	for i := 0; ; i++ {
		select {
		case data := <-events:
			if strings.Contains(data, `"type":"projects"`) {
				continue // the registry change itself
			}
			assertContains(t, data, `"project":"demo"`)
			assertContains(t, data, `"type":"changed"`)
			return
		case <-deadline:
			t.Fatal("no change event for a project added while the server runs")
		case <-time.After(300 * time.Millisecond):
			runIn(t, dir, "add", fmt.Sprintf("card %d", i))
		}
	}
}

// waitForEvent reads events until one holds want, failing after 3 seconds.
func waitForEvent(t *testing.T, events <-chan string, want string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case data, ok := <-events:
			if !ok {
				t.Fatal("the event stream ended")
			}
			if strings.Contains(data, want) {
				return
			}
		case <-deadline:
			t.Fatalf("no event holding %s", want)
		}
	}
}

func TestServePublishesProjectsForCLIRegistryChanges(t *testing.T) {
	s := startServe(t, "serve", "--port", "0", "--no-open")
	events := sseEvents(t, s.get("/api/events"))

	dir := t.TempDir()
	runIn(t, dir, "project", "add", "Later", "--path", filepath.Join(dir, "later.md"))
	waitForEvent(t, events, `"type":"projects"`)

	runIn(t, dir, "project", "rm", "Later")
	waitForEvent(t, events, `"type":"projects"`)
}

func TestWatchTargetsReadsTheRegistryEachTime(t *testing.T) {
	sb := sandboxFor(t)
	getenv := func(k string) string { return map[string]string{"HOME": sb.home}[k] }
	targets := watchTargets(sb.config, getenv, slog.New(slog.NewTextHandler(io.Discard, nil)))
	assertEq(t, len(targets()), 0)

	writeConfig(t, "projects:\n  - {name: a, storage: markdown, path: /boards/a/BOARD.md}\n  - {name: b, storage: markdown, path: /boards/b/BOARD.md}\n")
	got := targets()
	assertEq(t, len(got), 2)
	assertEq(t, got[0].Project, "a")
	assertEq(t, got[1].Path, "/boards/b/BOARD.md")

	writeConfig(t, "projects:\n  - {name: a, storage: markdown, path: /boards/a/BOARD.md}\n")
	assertEq(t, len(targets()), 1)
}

func TestWatchTargetsKeepsThePreviousOnesWhenTheConfigCannotBeRead(t *testing.T) {
	sb := sandboxFor(t)
	getenv := func(k string) string { return map[string]string{"HOME": sb.home}[k] }
	logs := &lockedBuffer{}
	targets := watchTargets(sb.config, getenv, slog.New(slog.NewTextHandler(logs, nil)))

	writeConfig(t, "projects:\n  - {name: a, storage: markdown, path: /boards/a/BOARD.md}\n")
	assertEq(t, len(targets()), 1)

	writeConfig(t, "projects: [this is: not valid")
	for i := 0; i < 3; i++ {
		got := targets()
		assertEq(t, len(got), 1)
		assertEq(t, got[0].Project, "a")
	}
	assertEq(t, strings.Count(logs.String(), "cannot read the config"), 1) // not once per refresh

	writeConfig(t, "projects:\n  - {name: c, storage: markdown, path: /boards/c/BOARD.md}\n")
	got := targets()
	assertEq(t, len(got), 1)
	assertEq(t, got[0].Project, "c")
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
