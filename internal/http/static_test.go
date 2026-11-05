package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

const testPort = 4747

func uiFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":          {Data: []byte("<!doctype html><title>Waypoint</title>INDEX")},
		"placeholder.html":    {Data: []byte(`The Waypoint UI was not built. Run "make web".`)},
		"assets/app-3f2a.js":  {Data: []byte("console.log(1)")},
		"assets/app-3f2a.css": {Data: []byte("body{}")},
		"favicon.svg":         {Data: []byte("<svg/>")},
	}
}

func getUI(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSPAFallback(t *testing.T) {
	h := UI(uiFS())
	for _, p := range []string{"/", "/p/Demo", "/p/Demo/card/T-1", "/settings"} {
		rec := getUI(t, h, "GET", p)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "INDEX") {
			t.Fatalf("%s: status %d body %q", p, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Fatalf("%s: Cache-Control = %q, want no-cache", p, got)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("%s: Content-Type = %q", p, ct)
		}
	}
}

func TestPlaceholderWhenUnbuilt(t *testing.T) {
	fsys := uiFS()
	delete(fsys, "index.html")
	rec := getUI(t, UI(fsys), "GET", "/p/Demo/card/T-1")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `The Waypoint UI was not built. Run "make web".`) {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestStaticAssets(t *testing.T) {
	h := UI(uiFS())
	rec := getUI(t, h, "GET", "/assets/app-3f2a.js")
	if rec.Code != 200 || rec.Body.String() != "console.log(1)" {
		t.Fatalf("js: %d %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("Content-Type = %q", ct)
	}
	if ct := getUI(t, h, "GET", "/assets/app-3f2a.css").Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Fatalf("css Content-Type = %q", ct)
	}
	if got := getUI(t, h, "GET", "/favicon.svg").Header().Get("Cache-Control"); got != "" {
		t.Fatalf("non-asset Cache-Control = %q, want none", got)
	}
	if rec := getUI(t, h, "HEAD", "/assets/app-3f2a.js"); rec.Code != 200 || rec.Body.Len() != 0 {
		t.Fatalf("HEAD: %d %q", rec.Code, rec.Body.String())
	}
}

// A path is a file only if dist has it. A route may hold a dot, because a project
// name may: a hard reload there has to get the app.
func TestAnythingNotInDistIsARoute(t *testing.T) {
	h := UI(uiFS())
	for _, p := range []string{"/p/my.project", "/p/v1.2/card/T-1", "/p/index.html", "/assets", "/assets/missing.js", "/nope.png"} {
		rec := getUI(t, h, "GET", p)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "INDEX") {
			t.Fatalf("%s: status %d body %q", p, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Fatalf("%s: Cache-Control = %q", p, got)
		}
	}
}

func TestAPIPathsAreNeverARoute(t *testing.T) {
	h := uiHandler(t, uiFS())
	req := httptest.NewRequest("GET", "/api/no.such.thing", nil)
	req.Host = "127.0.0.1:4747"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 404 || strings.Contains(rec.Body.String(), "INDEX") {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
}

func TestUIRejectsOtherMethods(t *testing.T) {
	rec := getUI(t, UI(uiFS()), "DELETE", "/")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestUIPathCannotEscape(t *testing.T) {
	for _, p := range []string{"/../go.mod", "/assets/../../go.mod", "/%2e%2e/x.txt"} {
		rec := getUI(t, UI(uiFS()), "GET", p)
		if strings.Contains(rec.Body.String(), "module ") {
			t.Fatalf("%s leaked a file", p)
		}
	}
}

func uiHandler(t *testing.T, fsys fstest.MapFS) http.Handler {
	t.Helper()
	root := t.TempDir()
	return Handler(Deps{
		ConfigPath: root + "/config.yaml",
		Getenv:     func(k string) string { return map[string]string{"HOME": root}[k] },
		Port:       testPort,
		UI:         fsys,
	})
}

func TestHandlerServesUIAndAPI(t *testing.T) {
	h := uiHandler(t, uiFS())
	do := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", target, nil)
		req.Host = "127.0.0.1:4747"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if rec := do("/p/Demo"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "INDEX") {
		t.Fatalf("UI: %d %q", rec.Code, rec.Body.String())
	}
	rec := do("/api/projects")
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("API: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := do("/api/nope"); rec.Code != 404 || strings.Contains(rec.Body.String(), "INDEX") {
		t.Fatalf("unknown API path: %d %q", rec.Code, rec.Body.String())
	}
}

func TestUIIsBehindTheHostCheckAndNosniff(t *testing.T) {
	h := uiHandler(t, uiFS())
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "evil.example:4747"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMisdirectedRequest {
		t.Fatalf("status %d, want 421", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "INDEX") {
		t.Fatal("UI served to a foreign Host")
	}

	req = httptest.NewRequest("GET", "/assets/app-3f2a.js", nil)
	req.Host = "localhost:4747"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("status %d nosniff %q", rec.Code, rec.Header().Get("X-Content-Type-Options"))
	}
}

func TestEmbeddedDistHasAPlaceholder(t *testing.T) {
	// With only placeholder.html committed, the embedded UI must still answer.
	srv := httptest.NewServer(UI(DistFS()))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/p/x")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || len(body) == 0 {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
}

func TestBrokerCloseEndsOpenStreams(t *testing.T) {
	b := NewBroker()
	srv := httptest.NewServer(http.HandlerFunc(b.ServeSSE))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	waitFor(t, func() bool { return b.subscriberCount() == 1 })

	done := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, resp.Body); done <- err }()
	b.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream still open after Close")
	}
	waitFor(t, func() bool { return b.subscriberCount() == 0 })
	b.Close() // safe to repeat
}

func TestSSEAfterCloseReturnsImmediately(t *testing.T) {
	b := NewBroker()
	b.Close()
	rec := httptest.NewRecorder()
	finished := make(chan struct{})
	go func() {
		b.ServeSSE(rec, httptest.NewRequest("GET", "/api/events", nil))
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("ServeSSE blocked on a closed broker")
	}
	if b.subscriberCount() != 0 {
		t.Fatal("a subscriber was left behind")
	}
}
