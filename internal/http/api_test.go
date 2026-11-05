package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"waypoint/internal/app"
	"waypoint/internal/config"
	"waypoint/internal/domain"
	"waypoint/internal/storage/markdown"
)

var fixedNow = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

// fixture is a server over a temp config with a fixed clock. Nothing in it
// touches the real home directory or config.
type fixture struct {
	t       *testing.T
	srv     *httptest.Server
	port    int
	home    string
	dir     string // holds the board files
	cfgPath string
	broker  *Broker
	logs    *bytes.Buffer
}

func newFixture(t *testing.T, names ...string) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{
		t:       t,
		home:    filepath.Join(root, "home"),
		dir:     filepath.Join(root, "boards"),
		cfgPath: filepath.Join(root, "cfg", "config.yaml"),
		broker:  NewBroker(),
		logs:    &bytes.Buffer{},
	}
	if err := os.MkdirAll(f.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f.srv = httptest.NewUnstartedServer(nil)
	f.port = f.srv.Listener.Addr().(*net.TCPAddr).Port
	cfg := &config.Config{Port: f.port, TLS: "auto", DataDir: filepath.Join(root, "data")}
	if err := cfg.Save(f.cfgPath); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		f.addBoard(n, nil)
	}
	f.srv.Config.Handler = Handler(Deps{
		ConfigPath: f.cfgPath,
		Getenv:     func(k string) string { return map[string]string{"HOME": f.home}[k] },
		Now:        func() time.Time { return fixedNow },
		Broker:     f.broker,
		Port:       f.port,
		Logger:     slog.New(slog.NewTextHandler(f.logs, nil)),
	})
	f.srv.Start()
	t.Cleanup(f.srv.Close)
	return f
}

// addBoard creates a board file and registers it, the way the CLI would.
func (f *fixture) addBoard(name string, mutate func(*domain.Settings)) string {
	f.t.Helper()
	s := domain.NewBoard(name, "T").Settings
	if mutate != nil {
		mutate(&s)
	}
	path := f.boardPath(name)
	if _, err := markdown.Create(path, s); err != nil {
		f.t.Fatal(err)
	}
	f.register(config.ProjectRef{Name: name, Storage: "markdown", Path: path})
	return path
}

func (f *fixture) register(ref config.ProjectRef) {
	f.t.Helper()
	err := config.Update(f.cfgPath, func(string) string { return f.home }, func(c *config.Config) error {
		c.Projects = append(c.Projects, ref)
		return nil
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) boardPath(name string) string { return filepath.Join(f.dir, name+".md") }

func (f *fixture) config() *config.Config {
	f.t.Helper()
	c, err := config.Load(f.cfgPath, func(string) string { return f.home })
	if err != nil {
		f.t.Fatal(err)
	}
	return c
}

// service is a Service over a registered board, with its own clock.
func (f *fixture) service(name string, now time.Time) *app.Service {
	return app.NewService(markdown.New(f.boardPath(name)), func() time.Time { return now })
}

type reply struct {
	t      *testing.T
	code   int
	body   []byte
	header http.Header
}

func (r reply) decode(v any) {
	r.t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		r.t.Fatalf("decode %q: %v", r.body, err)
	}
}

func (r reply) obj() map[string]any {
	r.t.Helper()
	var m map[string]any
	r.decode(&m)
	return m
}

func (r reply) wantCode(status int, code string) errorBody {
	r.t.Helper()
	var e errorBody
	r.decode(&e)
	if r.code != status || e.Code != code {
		r.t.Fatalf("got %d %q (%s), want %d %q", r.code, e.Code, r.body, status, code)
	}
	return e
}

func (r reply) want(status int) reply {
	r.t.Helper()
	if r.code != status {
		r.t.Fatalf("got %d, want %d: %s", r.code, status, r.body)
	}
	return r
}

// do sends a request with the Waypoint header. body is JSON-encoded unless it
// is a string or []byte, which are sent as they are.
func (f *fixture) do(method, path string, body any) reply {
	f.t.Helper()
	req := f.request(method, path, body)
	req.Header.Set("X-Waypoint", "1")
	return f.send(req)
}

func (f *fixture) request(method, path string, body any) *http.Request {
	f.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	case []byte:
		rd = bytes.NewReader(b)
	default:
		data, err := json.Marshal(b)
		if err != nil {
			f.t.Fatal(err)
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, f.srv.URL+path, rd)
	if err != nil {
		f.t.Fatal(err)
	}
	return req
}

func (f *fixture) send(req *http.Request) reply {
	f.t.Helper()
	res, err := f.srv.Client().Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return reply{t: f.t, code: res.StatusCode, body: data, header: res.Header}
}

type cardJSON struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Column    string   `json:"column"`
	Group     string   `json:"group"`
	Priority  string   `json:"priority"`
	Depends   []string `json:"depends"`
	Body      string   `json:"body"`
	Archived  bool     `json:"archived"`
	Next      bool     `json:"next"`
	Hash      string   `json:"hash"`
	Checklist struct {
		Done  int `json:"done"`
		Total int `json:"total"`
	} `json:"checklist"`
}

type boardJSON struct {
	Board struct {
		Settings     domain.Settings `json:"settings"`
		Cards        []cardJSON      `json:"cards"`
		Warnings     []string        `json:"warnings"`
		SettingsHash string          `json:"settings_hash"`
	} `json:"board"`
	Appearance map[string]any `json:"appearance"`
	Readonly   bool           `json:"readonly"`
}

func (f *fixture) board(name string) boardJSON {
	f.t.Helper()
	var b boardJSON
	f.do("GET", "/api/projects/"+name+"/board", nil).want(200).decode(&b)
	return b
}

func (f *fixture) newCard(project string, body map[string]any) cardJSON {
	f.t.Helper()
	var c cardJSON
	f.do("POST", "/api/projects/"+project+"/cards", body).want(201).decode(&c)
	return c
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
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
		t.Fatalf("%q contains %q", s, sub)
	}
}

func TestBoardRoundTrip(t *testing.T) {
	f := newFixture(t, "Alpha")
	path := f.boardPath("Alpha")

	first := f.newCard("Alpha", map[string]any{"title": "First", "column": "Todo", "priority": "P1"})
	second := f.newCard("Alpha", map[string]any{"title": "Second", "column": "Todo"})
	assertEq(t, first.ID, "T-1")
	assertEq(t, second.ID, "T-2")

	var moved cardJSON
	f.do("POST", "/api/projects/Alpha/cards/T-1/move", map[string]any{"column": "In progress", "index": 0}).want(200).decode(&moved)
	assertEq(t, moved.Column, "In progress")

	var patched cardJSON
	f.do("PATCH", "/api/projects/Alpha/cards/T-2", map[string]any{
		"title": "Second!", "body": "- [ ] one\n- [ ] two", "group": "", "set": map[string]string{"owner": "sam"},
	}).want(200).decode(&patched)
	assertEq(t, patched.Title, "Second!")

	var checked cardJSON
	f.do("POST", "/api/projects/Alpha/cards/T-2/check", map[string]any{"n": 1}).want(200).decode(&checked)
	assertEq(t, checked.Checklist.Done, 1)
	assertEq(t, checked.Checklist.Total, 2)

	b := f.board("Alpha")
	assertEq(t, len(b.Board.Cards), 2)
	assertEq(t, b.Board.Cards[0].ID, "T-2") // board order follows the columns
	assertEq(t, b.Board.Cards[0].Title, "Second!")
	assertEq(t, b.Board.Cards[1].Column, "In progress")
	assertEq(t, b.Board.Cards[0].Checklist.Done, 1)
	assertEq(t, b.Readonly, false)
	assertEq(t, b.Appearance["theme"], any("system"))
	assertEq(t, b.Board.Settings.Name, "Alpha")
	assertEq(t, len(b.Board.SettingsHash), 64)

	disk := readFile(t, path)
	assertContains(t, disk, "Second!")
	assertContains(t, disk, "- [x] one")
	assertContains(t, disk, "owner: sam")
	if strings.Index(disk, "## In progress") > strings.Index(disk, "First") {
		t.Fatalf("First is not under In progress on disk:\n%s", disk)
	}

	f.do("DELETE", "/api/projects/Alpha/cards/T-1", nil).want(204)
	assertEq(t, len(f.board("Alpha").Board.Cards), 1)
	f.do("DELETE", "/api/projects/Alpha/cards/T-1", nil).wantCode(404, "not_found")
}

func TestArchiveAndRestore(t *testing.T) {
	f := newFixture(t, "Alpha")
	f.newCard("Alpha", map[string]any{"title": "Ship", "column": "Done"})
	f.newCard("Alpha", map[string]any{"title": "Keep", "column": "Todo"})

	var res struct {
		Archived []string `json:"archived"`
	}
	f.do("POST", "/api/projects/Alpha/archive", nil).want(200).decode(&res)
	assertEq(t, strings.Join(res.Archived, ","), "T-1")

	var archived []cardJSON
	f.do("GET", "/api/projects/Alpha/archive", nil).want(200).decode(&archived)
	assertEq(t, len(archived), 1)
	assertEq(t, archived[0].Archived, true)
	assertEq(t, len(f.board("Alpha").Board.Cards), 1)

	var back cardJSON
	f.do("POST", "/api/projects/Alpha/cards/T-1/restore", nil).want(200).decode(&back)
	assertEq(t, back.Archived, false)
	assertEq(t, back.Column, "Todo")
	f.do("POST", "/api/projects/Alpha/cards/T-2/restore", nil).wantCode(404, "not_found") // not archived

	f.do("POST", "/api/projects/Alpha/cards/T-1/move", map[string]any{"column": "Done"}).want(200)
	f.do("POST", "/api/projects/Alpha/archive", nil).want(200)
	f.do("POST", "/api/projects/Alpha/cards/T-1/restore", map[string]any{"column": "Nowhere"}).wantCode(422, "unknown_column")
	f.do("POST", "/api/projects/Alpha/cards/T-1/restore", map[string]any{"column": "In progress"}).want(200).decode(&back)
	assertEq(t, back.Column, "In progress")
}

func TestFilterQuery(t *testing.T) {
	f := newFixture(t, "Alpha")
	f.newCard("Alpha", map[string]any{"title": "urgent", "column": "Todo", "priority": "P0"})
	f.newCard("Alpha", map[string]any{"title": "calm", "column": "Todo", "priority": "P2"})

	b := f.do("GET", "/api/projects/Alpha/board?q=p0", nil).want(200)
	var got boardJSON
	b.decode(&got)
	assertEq(t, len(got.Board.Cards), 1)
	assertEq(t, got.Board.Cards[0].Title, "urgent")
	assertEq(t, len(f.board("Alpha").Board.Cards), 2)

	var cards []cardJSON
	f.do("GET", "/api/projects/Alpha/archive?q=p0", nil).want(200).decode(&cards)
	assertEq(t, len(cards), 0)
	assertContains(t, string(f.do("GET", "/api/projects/Alpha/archive", nil).body), "[]")
}

func TestConflict409(t *testing.T) {
	f := newFixture(t, "Alpha")
	c := f.newCard("Alpha", map[string]any{"title": "Card", "column": "Todo", "body": "original"})
	path := f.boardPath("Alpha")
	if err := os.WriteFile(path, []byte(strings.Replace(readFile(t, path), "original", "edited elsewhere", 1)), 0o644); err != nil {
		t.Fatal(err)
	}

	r := f.do("PATCH", "/api/projects/Alpha/cards/T-1", map[string]any{"title": "Mine", "if_match": c.Hash})
	r.wantCode(409, "conflict")
	var body struct {
		Current cardJSON `json:"current"`
	}
	r.decode(&body)
	assertEq(t, body.Current.Body, "edited elsewhere")
	assertNotContains(t, readFile(t, path), "Mine")

	// With the current hash the same patch goes through.
	f.do("PATCH", "/api/projects/Alpha/cards/T-1", map[string]any{"title": "Mine", "if_match": body.Current.Hash}).want(200)
}

func TestParse423(t *testing.T) {
	f := newFixture(t, "Alpha")
	f.newCard("Alpha", map[string]any{"title": "Card", "column": "Todo"})
	path := f.boardPath("Alpha")
	if err := os.WriteFile(path, []byte(strings.Replace(readFile(t, path), "## Todo", "stray text\n\n## Todo", 1)), 0o644); err != nil {
		t.Fatal(err)
	}

	e := f.do("GET", "/api/projects/Alpha/board", nil).wantCode(423, "parse")
	if e.Line < 1 {
		t.Fatalf("no line in %+v", e)
	}
	f.do("POST", "/api/projects/Alpha/cards", map[string]any{"title": "More", "column": "Todo"}).wantCode(423, "parse")
	assertNotContains(t, readFile(t, path), "More")
}

func TestMissingProjectFile(t *testing.T) {
	f := newFixture(t, "Alpha", "Beta")
	if err := os.Remove(f.boardPath("Alpha")); err != nil {
		t.Fatal(err)
	}

	var list []projectJSON
	f.do("GET", "/api/projects", nil).want(200).decode(&list)
	assertEq(t, len(list), 2)
	assertEq(t, list[0].Name, "Alpha")
	if !strings.HasPrefix(list[0].Error, "File not found at ") {
		t.Fatalf("Alpha error = %q", list[0].Error)
	}
	assertEq(t, list[1].Error, "")
	assertEq(t, list[1].Storage, "markdown")
	assertEq(t, list[1].Path, f.boardPath("Beta"))
	assertEq(t, list[1].Accent, "orange")

	f.do("GET", "/api/projects/Beta/board", nil).want(200)
	f.do("GET", "/api/projects/Alpha/board", nil).wantCode(404, "not_found")
	f.do("POST", "/api/projects/Alpha/cards", map[string]any{"title": "x"}).wantCode(404, "not_found")
	if _, err := os.Stat(f.boardPath("Alpha")); err == nil {
		t.Fatal("a request recreated the missing board file")
	}
}

func TestProjectListValidatesEachEntry(t *testing.T) {
	f := newFixture(t, "Good")
	f.register(config.ProjectRef{Name: "Relative", Storage: "markdown", Path: "boards/rel.md"})
	f.register(config.ProjectRef{Name: "Mongo", Storage: "mongo", Path: f.boardPath("Good")})
	f.register(config.ProjectRef{Name: "Lite", Storage: "sqlite", Path: filepath.Join(f.dir, "x.db")})
	bad := filepath.Join(f.dir, "bad.md")
	os.WriteFile(bad, []byte("not a board\n"), 0o644)
	f.register(config.ProjectRef{Name: "Bad", Storage: "markdown", Path: bad})

	var list []projectJSON
	f.do("GET", "/api/projects", nil).want(200).decode(&list)
	byName := map[string]projectJSON{}
	for _, p := range list {
		byName[p.Name] = p
	}
	assertEq(t, len(list), 5)
	assertEq(t, byName["Good"].Error, "")
	assertContains(t, byName["Relative"].Error, "not absolute")
	assertContains(t, byName["Mongo"].Error, `Unknown storage "mongo"`)
	assertContains(t, byName["Lite"].Error, "File not found")
	assertContains(t, byName["Bad"].Error, "not a Waypoint board")
	for _, name := range []string{"Relative", "Mongo", "Lite"} {
		assertEq(t, byName[name].Accent != "", true)
	}

	f.do("GET", "/api/projects/Good/board", nil).want(200)
	f.do("GET", "/api/projects/Relative/board", nil).wantCode(422, "invalid")
	f.do("GET", "/api/projects/Mongo/board", nil).wantCode(422, "invalid")
	f.do("GET", "/api/projects/Lite/board", nil).wantCode(404, "not_found")
	f.do("GET", "/api/projects/Bad/board", nil).wantCode(423, "parse")
}

func TestHostCheck(t *testing.T) {
	f := newFixture(t, "Alpha")
	port := strconv.Itoa(f.port)
	cases := []struct {
		host string
		want int
	}{
		{"localhost:" + port, 200},
		{"LocalHost:" + port, 200},
		{"127.0.0.1:" + port, 200},
		{"[::1]:" + port, 200},
		{"evil.example:" + port, 421},
		{"evil.example:4747", 421},
		{"localhost:" + strconv.Itoa(f.port+1), 421},
		{"127.0.0.1", 421},
		{"localhost", 421},
		{"[::1]:1", 421},
		{"127.0.0.1.evil.example:" + port, 421},
	}
	for _, c := range cases {
		req := f.request("GET", "/api/projects", nil)
		req.Host = c.host
		r := f.send(req)
		if r.code != c.want {
			t.Errorf("Host %q: got %d, want %d", c.host, r.code, c.want)
		}
		if c.want == 421 {
			var e errorBody
			r.decode(&e)
			assertEq(t, e.Code, "misdirected")
		}
	}
}

func TestMutationNeedsHeader(t *testing.T) {
	f := newFixture(t, "Alpha")
	f.newCard("Alpha", map[string]any{"title": "Card", "column": "Todo"})
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/api/projects/Alpha/cards", `{"title":"x"}`},
		{"PATCH", "/api/projects/Alpha/cards/T-1", `{"title":"x"}`},
		{"PUT", "/api/settings", `{}`},
		{"DELETE", "/api/projects/Alpha", ``},
		{"POST", "/api/projects", `{"name":"X"}`},
	} {
		for _, hdr := range []string{"", "0", "true"} {
			req := f.request(c.method, c.path, c.body)
			if hdr != "" {
				req.Header.Set("X-Waypoint", hdr)
			}
			f.send(req).wantCode(403, "forbidden")
		}
	}
	assertEq(t, len(f.board("Alpha").Board.Cards), 1)
	assertEq(t, f.board("Alpha").Board.Cards[0].Title, "Card")
	assertEq(t, len(f.config().Projects), 1)

	// Reads need no header.
	req := f.request("GET", "/api/projects", nil)
	f.send(req).want(200)
}

func TestForeignOriginRefused(t *testing.T) {
	f := newFixture(t, "Alpha")
	own := "http://127.0.0.1:" + strconv.Itoa(f.port)
	for origin, want := range map[string]int{
		own: 200,
		"http://localhost:" + strconv.Itoa(f.port):     200,
		"http://evil.example":                          403,
		"https://evil.example:" + strconv.Itoa(f.port): 403,
		"http://127.0.0.1:1":                           403,
		"null":                                         403,
		"http://127.0.0.1:" + strconv.Itoa(f.port) + ".evil.example": 403,
	} {
		for _, method := range []string{"GET", "POST"} {
			req := f.request(method, "/api/projects/Alpha/cards", `{"title":"x","column":"Todo"}`)
			if method == "GET" {
				req = f.request("GET", "/api/projects", nil)
			}
			req.Header.Set("X-Waypoint", "1")
			req.Header.Set("Origin", origin)
			r := f.send(req)
			wantCode := want
			if method == "POST" && want == 200 {
				wantCode = 201
			}
			if r.code != wantCode {
				t.Errorf("%s with Origin %q: got %d, want %d", method, origin, r.code, wantCode)
			}
		}
	}
	// The foreign POSTs did nothing.
	assertEq(t, len(f.board("Alpha").Board.Cards), 2) // one per allowed origin
}

func TestNoPreflightAnswer(t *testing.T) {
	f := newFixture(t, "Alpha")
	req := f.request("OPTIONS", "/api/projects/Alpha/cards", nil)
	req.Header.Set("Origin", "http://evil.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	r := f.send(req)
	if r.code < 400 {
		t.Fatalf("preflight got %d", r.code)
	}
	for k := range r.header {
		if strings.HasPrefix(strings.ToLower(k), "access-control-") {
			t.Fatalf("response carries %s", k)
		}
	}
}

func TestWritePublishesChanged(t *testing.T) {
	f := newFixture(t, "Alpha")
	events, cancel := f.broker.Subscribe()
	defer cancel()

	f.newCard("Alpha", map[string]any{"title": "Card", "column": "Todo"})
	assertEq(t, receive(t, events), Event{Type: "changed", Project: "Alpha"})

	f.do("POST", "/api/projects/Alpha/cards/T-1/move", map[string]any{"column": "Done"}).want(200)
	assertEq(t, receive(t, events), Event{Type: "changed", Project: "Alpha"})

	// A refused write publishes nothing.
	f.do("POST", "/api/projects/Alpha/cards/T-1/move", map[string]any{"column": "Nowhere"}).want(422)
	f.do("PATCH", "/api/projects/Alpha/cards/T-9", map[string]any{"title": "x"}).want(404)
	select {
	case e := <-events:
		t.Fatalf("unexpected event %#v", e)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestRuleErrors422(t *testing.T) {
	f := newFixture(t, "Alpha")
	f.newCard("Alpha", map[string]any{"title": "Card", "column": "Todo"})

	// Todo cannot move to a column it does not list.
	cur := f.board("Alpha").Board
	cur.Settings.Columns[1].WIP = 1
	cur.Settings.Columns[0].MovesTo = []string{"In progress", "Done"}
	f.do("PUT", "/api/projects/Alpha/settings", map[string]any{"settings": cur.Settings, "if_match": cur.SettingsHash}).want(200)

	f.newCard("Alpha", map[string]any{"title": "Second", "column": "Todo"})
	f.do("POST", "/api/projects/Alpha/cards/T-1/move", map[string]any{"column": "In progress"}).want(200)
	e := f.do("POST", "/api/projects/Alpha/cards/T-2/move", map[string]any{"column": "In progress"}).wantCode(422, "wip_limit")
	assertContains(t, e.Error, "In progress is at its WIP limit")

	f.do("POST", "/api/projects/Alpha/cards/T-2/move", map[string]any{"column": "Nowhere"}).wantCode(422, "unknown_column")
	f.do("PATCH", "/api/projects/Alpha/cards/T-2", map[string]any{"priority": "P9"}).wantCode(422, "invalid")
	f.do("POST", "/api/projects/Alpha/cards", map[string]any{"title": "", "column": "Todo"}).wantCode(422, "invalid")
}

func TestMoveNotAllowed422(t *testing.T) {
	f := newFixture(t, "Alpha")
	f.newCard("Alpha", map[string]any{"title": "Card", "column": "Todo"})
	f.do("POST", "/api/projects/Alpha/cards/T-1/move", map[string]any{"column": "Done"}).want(200)
	f.do("POST", "/api/projects/Alpha/cards/T-1/move", map[string]any{"column": "Todo"}).wantCode(422, "move_not_allowed")
}

func TestSettingsRestartRequired(t *testing.T) {
	f := newFixture(t, "Alpha")

	var got struct {
		Port        int               `json:"port"`
		OpenBrowser bool              `json:"open_browser"`
		TLS         string            `json:"tls"`
		Appearance  domain.Appearance `json:"appearance"`
		Restart     bool              `json:"restart_required"`
	}
	f.do("GET", "/api/settings", nil).want(200).decode(&got)
	assertEq(t, got.Port, f.port)
	assertEq(t, got.TLS, "auto")

	// Appearance alone takes effect at once.
	f.do("PUT", "/api/settings", map[string]any{
		"port": f.port, "open_browser": false, "tls": "auto", "appearance": map[string]string{"accent": "teal"},
	}).want(200).decode(&got)
	assertEq(t, got.Restart, false)
	assertEq(t, f.config().Appearance.Accent, "teal")

	f.do("PUT", "/api/settings", map[string]any{
		"port": 4800, "open_browser": false, "tls": "auto", "appearance": map[string]string{"accent": "teal"},
	}).want(200).decode(&got)
	assertEq(t, got.Restart, true)
	assertEq(t, f.config().Port, 4800)

	f.do("PUT", "/api/settings", map[string]any{
		"port": 4800, "open_browser": false, "tls": "off", "appearance": map[string]string{"accent": "teal"},
	}).want(200).decode(&got)
	assertEq(t, got.Restart, true)
	assertEq(t, f.config().TLS, "off")
	// The registry, which the PUT does not carry, is kept.
	assertEq(t, len(f.config().Projects), 1)
}

func TestGlobalSettingsValidation(t *testing.T) {
	f := newFixture(t, "Alpha")
	base := func(mut func(map[string]any)) map[string]any {
		m := map[string]any{"port": f.port, "open_browser": true, "tls": "auto", "appearance": map[string]string{}}
		mut(m)
		return m
	}
	before := readFile(t, f.cfgPath)
	for name, body := range map[string]map[string]any{
		"tls":      base(func(m map[string]any) { m["tls"] = "maybe" }),
		"theme":    base(func(m map[string]any) { m["appearance"] = map[string]string{"theme": "neon"} }),
		"accent":   base(func(m map[string]any) { m["appearance"] = map[string]string{"accent": "#12345"} }),
		"port low": base(func(m map[string]any) { m["port"] = 0 }),
		"port big": base(func(m map[string]any) { m["port"] = 70000 }),
	} {
		t.Run(name, func(t *testing.T) { f.do("PUT", "/api/settings", body).wantCode(422, "invalid") })
	}
	assertEq(t, readFile(t, f.cfgPath), before)
	f.do("PUT", "/api/settings", base(func(m map[string]any) { m["tls"] = "on" })).want(200)
}

func TestOpenExistingFile(t *testing.T) {
	f := newFixture(t, "Alpha")

	// A typed path that isn't there is refused, and nothing appears on disk or in the registry.
	missing := filepath.Join(f.dir, "nodir", "x.md")
	e := f.do("POST", "/api/projects", map[string]any{"name": "Typo", "path": missing, "existing": true}).wantCode(422, "invalid")
	assertContains(t, e.Error, "no file at")
	if _, err := os.Stat(filepath.Dir(missing)); err == nil {
		t.Fatal("the folder was created")
	}
	assertEq(t, len(f.config().Projects), 1)

	f.do("POST", "/api/projects", map[string]any{"name": "NoPath", "existing": true}).wantCode(422, "invalid")

	// A real board opens.
	real := filepath.Join(f.dir, "real.md")
	f.do("POST", "/api/projects", map[string]any{"name": "Real", "path": real, "prefix": "R"}).want(201)
	f.do("DELETE", "/api/projects/Real", nil).want(204)
	f.do("POST", "/api/projects", map[string]any{"name": "Real", "path": real, "existing": true}).want(201)
}

func TestProjectAddAndRemove(t *testing.T) {
	f := newFixture(t, "Alpha")
	events, cancel := f.broker.Subscribe()
	defer cancel()
	path := filepath.Join(f.dir, "new", "gamma.md")

	var p projectJSON
	f.do("POST", "/api/projects", map[string]any{"name": "Gamma", "path": path, "prefix": "G"}).want(201).decode(&p)
	assertEq(t, p.Name, "Gamma")
	assertEq(t, p.Path, path)
	assertContains(t, readFile(t, path), "prefix: G")
	assertEq(t, receive(t, events), Event{Type: "projects"})
	assertEq(t, len(f.config().Projects), 2)

	// Default path goes under data_dir, which is the sandbox's, not the real one.
	f.do("POST", "/api/projects", map[string]any{"name": "Delta Force"}).want(201).decode(&p)
	assertEq(t, p.Path, filepath.Join(f.config().DataDir, "delta-force.db"))

	// The same file, even through a symlink, and a taken name are refused.
	link := filepath.Join(f.dir, "link.md")
	if err := os.Symlink(f.boardPath("Alpha"), link); err != nil {
		t.Fatal(err)
	}
	e := f.do("POST", "/api/projects", map[string]any{"name": "Other", "path": link}).wantCode(422, "invalid")
	assertContains(t, e.Error, `already registered as "Alpha"`)
	f.do("POST", "/api/projects", map[string]any{"name": "alpha", "path": filepath.Join(f.dir, "z.md")}).wantCode(422, "invalid")
	// A database can be created from the web too.
	f.do("POST", "/api/projects", map[string]any{"name": "Lite", "path": filepath.Join(f.dir, "x.db")}).want(201)
	if _, err := os.Stat(filepath.Join(f.dir, "x.db")); err != nil {
		t.Fatal(err)
	}
	// Only markdown files and databases can be created from the web.
	for _, bad := range []string{"notes.txt", "board", "board.md.txt", ".hidden"} {
		f.do("POST", "/api/projects", map[string]any{"name": "Ext", "path": filepath.Join(f.dir, bad)}).wantCode(422, "invalid")
		if _, err := os.Stat(filepath.Join(f.dir, bad)); err == nil {
			t.Fatalf("%s was created", bad)
		}
	}
	f.do("POST", "/api/projects", map[string]any{"name": "Upper", "path": filepath.Join(f.dir, "UPPER.MD")}).want(201)
	f.do("POST", "/api/projects", map[string]any{"name": "Long", "path": filepath.Join(f.dir, "long.markdown")}).want(201)
	f.do("POST", "/api/projects", map[string]any{"name": "Rel", "path": "boards/rel.md"}).wantCode(422, "invalid")
	f.do("POST", "/api/projects", map[string]any{"name": "Home", "path": "~/x.md"}).wantCode(422, "invalid")
	f.do("POST", "/api/projects", map[string]any{"name": "  "}).wantCode(422, "invalid")
	assertEq(t, len(f.config().Projects), 6)
	if _, err := os.Stat(filepath.Join(f.dir, "boards", "rel.md")); err == nil {
		t.Fatal("a relative path reached the filesystem")
	}

	f.do("DELETE", "/api/projects/Gamma", nil).want(204)
	assertEq(t, receive(t, events), Event{Type: "projects"})
	assertEq(t, len(f.config().Projects), 5)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("removing a project deleted its file: %v", err)
	}
	f.do("DELETE", "/api/projects/Gamma", nil).wantCode(404, "not_found")
}

func TestStaleSettingsGuard(t *testing.T) {
	f := newFixture(t, "Alpha")
	b := f.board("Alpha").Board
	hash := b.SettingsHash

	// Adding a card moves next_id but not the hash.
	f.newCard("Alpha", map[string]any{"title": "Card", "column": "Todo", "priority": "P1"})
	assertEq(t, f.board("Alpha").Board.SettingsHash, hash)

	put := func(s domain.Settings, ifMatch any, extra map[string]any) reply {
		body := map[string]any{"settings": s}
		if ifMatch != nil {
			body["if_match"] = ifMatch
		}
		for k, v := range extra {
			body[k] = v
		}
		return f.do("PUT", "/api/projects/Alpha/settings", body)
	}

	// Another writer changes the settings; the first editor's hash is stale.
	other := b.Settings
	other.Name = "Edited elsewhere"
	put(other, hash, nil).want(200)
	mine := b.Settings
	mine.Name = "Mine"
	r := put(mine, hash, nil)
	r.wantCode(409, "conflict")
	var conflict struct {
		Current struct {
			Settings     domain.Settings `json:"settings"`
			SettingsHash string          `json:"settings_hash"`
		} `json:"current"`
	}
	r.decode(&conflict)
	assertEq(t, conflict.Current.Settings.Name, "Edited elsewhere")
	assertEq(t, conflict.Current.SettingsHash, f.board("Alpha").Board.SettingsHash)
	assertContains(t, readFile(t, f.boardPath("Alpha")), "Edited elsewhere")

	// No opt-out: a missing or empty if_match is refused.
	put(mine, nil, nil).wantCode(422, "invalid")
	put(mine, "", nil).wantCode(422, "invalid")
	assertContains(t, readFile(t, f.boardPath("Alpha")), "Edited elsewhere")

	// The current hash works, and renames carry the cards.
	cur := f.board("Alpha").Board
	next := cur.Settings
	next.Priorities = []string{"P0", "Soon", "P2"}
	next.Groups = []domain.Group{{Name: "Ops", Color: "blue"}}
	var out boardJSON
	put(next, cur.SettingsHash, map[string]any{"priority_renames": map[string]string{"P1": "Soon"}}).want(200).decode(&out)
	assertEq(t, out.Board.Cards[0].Priority, "Soon")
	assertEq(t, out.Board.SettingsHash != cur.SettingsHash, true)
}

func TestProjectSettingsRenames(t *testing.T) {
	f := newFixture(t, "Alpha")
	cur := f.board("Alpha").Board
	cur.Settings.Groups = []domain.Group{{Name: "Bugs", Color: "red"}}
	f.do("PUT", "/api/projects/Alpha/settings", map[string]any{"settings": cur.Settings, "if_match": cur.SettingsHash}).want(200)
	f.newCard("Alpha", map[string]any{"title": "Card", "column": "In progress", "group": "Bugs"})

	cur = f.board("Alpha").Board
	cur.Settings.Columns[1].Name = "Doing"
	cur.Settings.Columns[0].MovesTo = []string{"Doing", "Done"}
	cur.Settings.Columns[2].MovesTo = []string{"Doing"}
	cur.Settings.Groups[0].Name = "Defects"
	cur.Settings.Appearance.Accent = "teal"
	var raw boardJSON
	f.do("PUT", "/api/projects/Alpha/settings", map[string]any{
		"settings": cur.Settings, "if_match": cur.SettingsHash,
		"column_renames": map[string]string{"In progress": "Doing"},
		"group_renames":  map[string]string{"Bugs": "Defects"},
	}).want(200).decode(&raw)
	assertEq(t, raw.Board.Cards[0].Column, "Doing")
	assertEq(t, raw.Board.Cards[0].Group, "Defects")
	assertEq(t, raw.Appearance["accent"], any("teal"))
	assertEq(t, raw.Appearance["source"].(map[string]any)["accent"], any("project"))

	// A bad appearance value is refused whole.
	cur = f.board("Alpha").Board
	cur.Settings.Appearance.Density = "tiny"
	f.do("PUT", "/api/projects/Alpha/settings", map[string]any{"settings": cur.Settings, "if_match": cur.SettingsHash}).wantCode(422, "invalid")
}

func TestAutoArchiveOncePerRun(t *testing.T) {
	f := newFixture(t)
	f.addBoard("Auto", func(s *domain.Settings) { s.Archive = domain.ArchiveSettings{Mode: domain.ArchiveAuto, AfterDays: 14} })
	old := fixedNow.AddDate(0, 0, -30)
	svc := f.service("Auto", old)
	if _, err := svc.CreateCard(t.Context(), domain.NewCard{Title: "Old one", Column: "Done"}); err != nil {
		t.Fatal(err)
	}
	events, cancel := f.broker.Subscribe()
	defer cancel()

	// Opening the board archives what is due, and says so.
	assertEq(t, len(f.board("Auto").Board.Cards), 0)
	assertEq(t, receive(t, events), Event{Type: "changed", Project: "Auto"})
	var archived []cardJSON
	f.do("GET", "/api/projects/Auto/archive", nil).want(200).decode(&archived)
	assertEq(t, len(archived), 1)

	// A card that becomes due later waits for the next write or run.
	if _, err := svc.CreateCard(t.Context(), domain.NewCard{Title: "Old two", Column: "Done"}); err != nil {
		t.Fatal(err)
	}
	assertEq(t, len(f.board("Auto").Board.Cards), 1)
	assertEq(t, len(f.board("Auto").Board.Cards), 1)

	// A new server run starts over.
	h := Handler(Deps{
		ConfigPath: f.cfgPath, Now: func() time.Time { return fixedNow }, Port: f.port, Broker: f.broker,
		Getenv: func(string) string { return f.home },
	})
	req := httptest.NewRequest("GET", "/api/projects/Auto/board", nil)
	req.Host = "127.0.0.1:" + strconv.Itoa(f.port)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var again boardJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &again); err != nil || rec.Code != 200 {
		t.Fatalf("%d %s %v", rec.Code, rec.Body, err)
	}
	assertEq(t, len(again.Board.Cards), 0)
}

func TestAutoArchiveFailureDoesNotFailTheRead(t *testing.T) {
	f := newFixture(t)
	path := f.addBoard("Auto", func(s *domain.Settings) { s.Archive = domain.ArchiveSettings{Mode: domain.ArchiveAuto, AfterDays: 14} })
	svc := f.service("Auto", fixedNow.AddDate(0, 0, -30))
	if _, err := svc.CreateCard(t.Context(), domain.NewCard{Title: "Old", Column: "Done"}); err != nil {
		t.Fatal(err)
	}
	// With the folder read-only the archive cannot be saved, but the board can be read.
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	if f2, err := os.CreateTemp(dir, "probe"); err == nil { // running as root: the test cannot make a write fail
		f2.Close()
		t.Skip("directory is writable despite chmod")
	}

	b := f.board("Auto")
	assertEq(t, len(b.Board.Cards), 1)
	assertContains(t, f.logs.String(), "auto-archive failed") // it did try, and failed
}

func TestHostileBodies(t *testing.T) {
	f := newFixture(t, "Alpha")
	card := "/api/projects/Alpha/cards"

	big := `{"title":"` + strings.Repeat("a", 1<<20) + `","column":"Todo"}`
	f.do("POST", card, big).wantCode(413, "too_large")
	f.do("POST", card, `{"title":"x","column":"Todo","sneaky":1}`).wantCode(400, "invalid")
	f.do("POST", card, `{"title":"x",`).wantCode(400, "invalid")
	f.do("POST", card, `not json`).wantCode(400, "invalid")
	f.do("POST", card, ``).wantCode(400, "invalid")
	f.do("POST", card, `{"title":"x","column":"Todo"} {"title":"y"}`).wantCode(400, "invalid")
	f.do("POST", card, `{"title":5}`).wantCode(400, "invalid")
	f.do("PATCH", card+"/T-1", `{"nope":true}`).wantCode(400, "invalid")
	f.do("PUT", "/api/settings", `{"port":1,"extra":2}`).wantCode(400, "invalid")
	f.do("POST", "/api/projects", `{"name":"x","bogus":1}`).wantCode(400, "invalid")
	f.do("PUT", "/api/projects/Alpha/settings", `{"settings":{"name":"x","bogus":1},"if_match":"a"}`).wantCode(400, "invalid")
	assertEq(t, len(f.board("Alpha").Board.Cards), 0)

	// Just under the limit is fine.
	ok := `{"title":"` + strings.Repeat("a", 1<<19) + `","column":"Todo"}`
	f.do("POST", card, ok).want(201)
}

func TestProjectNameIsOnlyARegistryKey(t *testing.T) {
	f := newFixture(t, "Alpha")
	secret := filepath.Join(f.dir, "secret.md")
	if _, err := markdown.Create(secret, domain.NewBoard("Secret", "S").Settings); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"..%2F..%2Fetc%2Fpasswd",
		"%2Fetc%2Fpasswd",
		escapePath(secret),
		"..%2Fboards%2Fsecret.md",
		"secret",
		"secret.md",
		"alpha%00",
	} {
		for _, p := range []string{"/board", "/archive"} {
			f.do("GET", "/api/projects/"+name+p, nil).wantCode(404, "not_found")
		}
		f.do("POST", "/api/projects/"+name+"/cards", map[string]any{"title": "x"}).wantCode(404, "not_found")
		f.do("PUT", "/api/projects/"+name+"/settings", map[string]any{"settings": domain.Settings{}, "if_match": "x"}).wantCode(404, "not_found")
		f.do("DELETE", "/api/projects/"+name, nil).wantCode(404, "not_found")
	}
	assertEq(t, len(f.config().Projects), 1)
	// Names match the registry without regard to case, as on the command line.
	f.do("GET", "/api/projects/alpha/board", nil).want(200)
	// A name with a slash in it is one segment, not a path.
	f.register(config.ProjectRef{Name: "a/b", Storage: "markdown", Path: f.addBoard("ab", nil)})
	f.do("GET", "/api/projects/a%2Fb/board", nil).want(200)
	f.do("GET", "/api/projects/a/b/board", nil).wantCode(404, "not_found")
}

func escapePath(p string) string { return strings.ReplaceAll(strings.TrimPrefix(p, "/"), "/", "%2F") }

func TestInternalErrorsAreLoggedNotShown(t *testing.T) {
	f := newFixture(t, "Alpha")
	path := f.boardPath("Alpha")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o644) })
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("file is readable despite chmod 0")
	}

	r := f.do("GET", "/api/projects/Alpha/board", nil)
	e := r.wantCode(500, "error")
	assertEq(t, e.Error, "internal error")
	assertNotContains(t, string(r.body), f.dir)
	assertNotContains(t, string(r.body), "permission")
	assertContains(t, f.logs.String(), "permission denied")
	assertContains(t, f.logs.String(), "level=ERROR")
}

func TestUnknownRoutesAreJSON(t *testing.T) {
	f := newFixture(t, "Alpha")
	f.do("GET", "/api/nope", nil).wantCode(404, "not_found")
	f.do("GET", "/api/projects/Alpha/cards/T-1/move", nil).wantCode(404, "not_found")
}

func TestRegistryEntryThatIsADirectory(t *testing.T) {
	f := newFixture(t, "Good")
	f.register(config.ProjectRef{Name: "Dir", Storage: "markdown", Path: f.dir})

	var list []projectJSON
	f.do("GET", "/api/projects", nil).want(200).decode(&list)
	listed := list[1].Error
	assertContains(t, listed, "is a directory")

	for _, r := range []reply{
		f.do("GET", "/api/projects/Dir/board", nil),
		f.do("GET", "/api/projects/Dir/archive", nil),
		f.do("POST", "/api/projects/Dir/cards", map[string]any{"title": "x"}),
	} {
		e := r.wantCode(422, "invalid")
		assertEq(t, e.Error, listed) // the same message the list gives
	}
	assertNotContains(t, f.logs.String(), "level=ERROR")
}

func TestNosniffOnEveryResponse(t *testing.T) {
	f := newFixture(t, "Alpha")
	check := func(name string, r reply) {
		t.Helper()
		if got := r.header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s (%d): X-Content-Type-Options = %q", name, r.code, got)
		}
	}
	check("ok", f.do("GET", "/api/projects", nil))
	check("created", f.do("POST", "/api/projects/Alpha/cards", map[string]any{"title": "x", "column": "Todo"}))
	check("not found", f.do("GET", "/api/projects/Nope/board", nil))
	check("no content", f.do("DELETE", "/api/projects/Alpha/cards/T-1", nil))
	check("unknown route", f.do("GET", "/api/nope", nil))
	req := f.request("GET", "/api/projects", nil)
	req.Host = "evil.example"
	check("421", f.send(req))
	check("403", f.send(f.request("POST", "/api/projects", `{}`)))
}

// staleCard makes a card with a checklist, then edits the file behind the API,
// and returns the hash the caller is left holding.
func staleCard(t *testing.T, f *fixture) (hash, path string) {
	t.Helper()
	c := f.newCard("Alpha", map[string]any{"title": "Card", "column": "Todo", "body": "- [ ] one\n- [ ] two"})
	path = f.boardPath("Alpha")
	edited := strings.Replace(readFile(t, path), "- [ ] one", "- [ ] renamed elsewhere\n- [ ] one", 1)
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	return c.Hash, path
}

func TestCheckRefusesAStaleHash(t *testing.T) {
	f := newFixture(t, "Alpha")
	hash, path := staleCard(t, f)
	before := readFile(t, path)

	r := f.do("POST", "/api/projects/Alpha/cards/T-1/check", map[string]any{"n": 1, "if_match": hash})
	r.wantCode(409, "conflict")
	var body struct {
		Current cardJSON `json:"current"`
	}
	r.decode(&body)
	assertContains(t, body.Current.Body, "renamed elsewhere")
	assertEq(t, readFile(t, path), before)

	f.do("POST", "/api/projects/Alpha/cards/T-1/check", map[string]any{"n": 1, "if_match": body.Current.Hash}).want(200)
	assertContains(t, readFile(t, path), "- [x] renamed elsewhere")
}

func TestCheckWithNoHashStillWorks(t *testing.T) {
	f := newFixture(t, "Alpha")
	staleCard(t, f)
	f.do("POST", "/api/projects/Alpha/cards/T-1/check", map[string]any{"n": 2}).want(200)
}

func TestDeleteRefusesAStaleHash(t *testing.T) {
	f := newFixture(t, "Alpha")
	hash, path := staleCard(t, f)

	r := f.do("DELETE", "/api/projects/Alpha/cards/T-1", map[string]any{"if_match": hash})
	r.wantCode(409, "conflict")
	var body struct {
		Current cardJSON `json:"current"`
	}
	r.decode(&body)
	assertContains(t, readFile(t, path), "renamed elsewhere")

	f.do("DELETE", "/api/projects/Alpha/cards/T-1", map[string]any{"if_match": body.Current.Hash}).want(204)
	assertNotContains(t, readFile(t, path), "renamed elsewhere")
}

func TestDeleteWithNoBodyStillWorks(t *testing.T) {
	f := newFixture(t, "Alpha")
	staleCard(t, f)
	f.do("DELETE", "/api/projects/Alpha/cards/T-1", nil).want(204)
}

func TestDeleteAnArchivedCardWithAStaleHash(t *testing.T) {
	f := newFixture(t, "Alpha")
	c := f.newCard("Alpha", map[string]any{"title": "Card", "column": "Done", "body": "note"})
	f.do("POST", "/api/projects/Alpha/archive", nil).want(200)
	path := strings.TrimSuffix(f.boardPath("Alpha"), ".md") + ".archive.md"
	if err := os.WriteFile(path, []byte(strings.Replace(readFile(t, path), "note", "edited", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	f.do("DELETE", "/api/projects/Alpha/cards/T-1", map[string]any{"if_match": c.Hash}).wantCode(409, "conflict")
	assertContains(t, readFile(t, path), "edited")
}

const wantCSP = "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	f := newFixture(t, "Alpha")
	foreign := f.request("GET", "/api/projects", nil)
	foreign.Host = "evil.example"
	unmarked := f.request("POST", "/api/projects", map[string]any{})
	unmarked.Header.Del("X-Waypoint")
	for name, r := range map[string]reply{
		"api":      f.do("GET", "/api/projects", nil),
		"error":    f.do("GET", "/api/nope", nil),
		"foreign":  f.send(foreign),
		"unmarked": f.send(unmarked),
	} {
		assertEq(t, r.header.Get("X-Frame-Options"), "DENY")
		if got := r.header.Get("Content-Security-Policy"); got != wantCSP {
			t.Fatalf("%s: CSP = %q", name, got)
		}
	}
}

func TestColdReadIsNotHeldByALock(t *testing.T) {
	defer func(d time.Duration) { autoArchiveWait = d }(autoArchiveWait)
	autoArchiveWait = 50 * time.Millisecond

	f := newFixture(t)
	f.addBoard("Alpha", func(s *domain.Settings) {
		s.Archive = domain.ArchiveSettings{Mode: domain.ArchiveAuto, AfterDays: 1}
	})
	old := f.service("Alpha", fixedNow.Add(-72*time.Hour))
	c, err := old.CreateCard(context.Background(), domain.NewCard{Title: "Old"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Done(context.Background(), c.ID); err != nil {
		t.Fatal(err)
	}

	unlock, err := markdown.New(f.boardPath("Alpha")).Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	assertEq(t, len(f.board("Alpha").Board.Cards), 1)
	if took := time.Since(began); took > time.Second {
		t.Fatalf("the read waited %v for the lock", took)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}

	// The archive it could not do happens on the next read.
	assertEq(t, len(f.board("Alpha").Board.Cards), 0)
}

func TestConfigTypoIsExplainedOnEveryRoute(t *testing.T) {
	f := newFixture(t, "Alpha")
	if err := os.WriteFile(f.cfgPath, []byte("port: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, r := range []reply{
		f.do("GET", "/api/projects", nil),
		f.do("GET", "/api/settings", nil),
		f.do("GET", "/api/projects/Alpha/board", nil),
		f.do("PUT", "/api/settings", map[string]any{"open_browser": false}),
	} {
		body := r.wantCode(500, "config")
		assertContains(t, body.Error, "yaml")
		assertNotContains(t, body.Error, f.cfgPath)
	}
}

func TestSettingsRefusePortsTheHostCheckCannotServe(t *testing.T) {
	f := newFixture(t)
	for _, port := range []int{80, 443} {
		f.do("PUT", "/api/settings", map[string]any{"port": port}).wantCode(422, "invalid")
	}
	assertEq(t, f.config().Port, f.port)
}

func TestSettingsMergeAPartialAppearance(t *testing.T) {
	f := newFixture(t)
	f.do("PUT", "/api/settings", map[string]any{"appearance": map[string]any{"theme": "dark", "accent": "teal"}}).want(200)
	f.do("PUT", "/api/settings", map[string]any{"appearance": map[string]any{"density": "compact"}}).want(200)
	got := f.config().Appearance
	assertEq(t, got, domain.Appearance{Theme: "dark", Accent: "teal", Density: "compact"})

	// An empty value is sent on purpose: it goes back to inheriting.
	f.do("PUT", "/api/settings", map[string]any{"appearance": map[string]any{"accent": ""}}).want(200)
	assertEq(t, f.config().Appearance, domain.Appearance{Theme: "dark", Density: "compact"})

	f.do("PUT", "/api/settings", map[string]any{"appearance": map[string]any{"theme": "neon"}}).wantCode(422, "invalid")
	assertEq(t, f.config().Appearance.Theme, "dark")
}

func TestParseErrorCarriesABareMessage(t *testing.T) {
	f := newFixture(t, "Alpha")
	f.newCard("Alpha", map[string]any{"title": "Card", "column": "Todo"})
	path := f.boardPath("Alpha")
	if err := os.WriteFile(path, []byte(strings.Replace(readFile(t, path), "## Todo", "stray text\n\n## Todo", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	r := f.do("GET", "/api/projects/Alpha/board", nil)
	r.wantCode(423, "parse")
	var body struct {
		Error   string `json:"error"`
		Message string `json:"message"`
		Line    int    `json:"line"`
	}
	r.decode(&body)
	assertContains(t, body.Error, path)
	assertContains(t, body.Error, "line ")
	if body.Message == "" || strings.Contains(body.Message, path) || strings.HasPrefix(body.Message, "line ") {
		t.Fatalf("message = %q", body.Message)
	}
	assertContains(t, body.Error, body.Message)
}

func TestBusyBoardIsA503(t *testing.T) {
	f := newFixture(t)
	a := &api{log: slog.New(slog.DiscardHandler)}
	rec := httptest.NewRecorder()
	a.writeError(rec, f.request("GET", "/", nil), fmt.Errorf("%w: try again", app.ErrChangedDuringWrite))
	assertEq(t, rec.Code, 503)
	assertContains(t, rec.Body.String(), `"code":"busy"`)
	assertContains(t, rec.Body.String(), "try again")
}

func TestCreateProjectStorageChoice(t *testing.T) {
	f := newFixture(t, "Alpha")
	var made projectJSON
	f.do("POST", "/api/projects", map[string]any{"name": "Default"}).want(201).decode(&made)
	assertEq(t, made.Storage, "sqlite")
	assertEq(t, strings.HasSuffix(made.Path, ".db"), true)
	f.do("POST", "/api/projects", map[string]any{"name": "Plain", "storage": "markdown"}).want(201).decode(&made)
	assertEq(t, made.Storage, "markdown")
	assertEq(t, strings.HasSuffix(made.Path, ".md"), true)
	f.do("POST", "/api/projects", map[string]any{"name": "Clash", "storage": "markdown", "path": filepath.Join(f.dir, "c.db")}).wantCode(422, "invalid")
	f.do("POST", "/api/projects", map[string]any{"name": "Odd", "storage": "mongo"}).wantCode(422, "invalid")
	f.do("GET", "/api/projects/Default/board", nil).want(200)
}

func TestConvertProjectAPI(t *testing.T) {
	f := newFixture(t, "Alpha")
	f.do("POST", "/api/projects/Alpha/cards", map[string]any{"title": "Kept across the move"}).want(201)
	events, cancel := f.broker.Subscribe()
	defer cancel()

	var out struct {
		projectJSON
		Backups []string `json:"backups"`
	}
	f.do("POST", "/api/projects/Alpha/convert", map[string]any{"to": "sqlite"}).want(200).decode(&out)
	assertEq(t, out.Storage, "sqlite")
	assertEq(t, strings.HasSuffix(out.Path, "Alpha.db") || strings.HasSuffix(out.Path, "alpha.db"), true)
	assertEq(t, len(out.Backups) >= 1, true)
	assertEq(t, receive(t, events), Event{Type: "projects"})
	assertEq(t, f.config().Projects[0].Storage, "sqlite")
	if _, err := os.Stat(out.Backups[0]); err != nil {
		t.Fatalf("the old board was not kept: %v", err)
	}

	var board struct {
		Board struct {
			Cards []struct{ Title, Hash string } `json:"cards"`
		} `json:"board"`
	}
	f.do("GET", "/api/projects/Alpha/board", nil).want(200).decode(&board)
	assertEq(t, len(board.Board.Cards), 1)
	assertEq(t, board.Board.Cards[0].Title, "Kept across the move")

	// And back, to a path of the caller's choosing.
	back := filepath.Join(f.dir, "back", "alpha.md")
	f.do("POST", "/api/projects/Alpha/convert", map[string]any{"to": "markdown", "path": back}).want(200).decode(&out)
	assertEq(t, out.Storage, "markdown")
	assertEq(t, out.Path, back)
	f.do("GET", "/api/projects/Alpha/board", nil).want(200)
}

func TestConvertProjectAPIRefusals(t *testing.T) {
	f := newFixture(t, "Alpha")
	taken := filepath.Join(f.dir, "taken.db")
	os.WriteFile(taken, []byte("mine"), 0o644)

	f.do("POST", "/api/projects/Alpha/convert", map[string]any{"to": "markdown"}).wantCode(422, "invalid")
	f.do("POST", "/api/projects/Alpha/convert", map[string]any{"to": "mongo"}).wantCode(422, "invalid")
	f.do("POST", "/api/projects/Alpha/convert", map[string]any{}).wantCode(422, "invalid")
	f.do("POST", "/api/projects/Alpha/convert", map[string]any{"to": "sqlite", "path": "rel.db"}).wantCode(422, "invalid")
	f.do("POST", "/api/projects/Alpha/convert", map[string]any{"to": "sqlite", "path": taken}).wantCode(422, "invalid")
	f.do("POST", "/api/projects/Nobody/convert", map[string]any{"to": "sqlite"}).wantCode(404, "not_found")

	if data, _ := os.ReadFile(taken); string(data) != "mine" {
		t.Fatal("the file at the target was touched")
	}
	assertEq(t, f.config().Projects[0].Storage, "markdown")
	f.do("GET", "/api/projects/Alpha/board", nil).want(200)
}

func TestConvertProjectAPIMismatchSaysWhichCard(t *testing.T) {
	f := newFixture(t, "Alpha")
	f.do("POST", "/api/projects", map[string]any{"name": "Lite"}).want(201)
	f.do("POST", "/api/projects/Lite/cards", map[string]any{"title": "Odd", "body": "## a heading"}).want(201)
	e := f.do("POST", "/api/projects/Lite/convert", map[string]any{"to": "markdown"}).wantCode(422, "invalid")
	assertContains(t, e.Error, "T-1")
	assertContains(t, e.Error, "#### or deeper")
}
