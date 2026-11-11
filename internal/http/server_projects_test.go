package httpapi

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"waypoint/internal/config"
	"waypoint/internal/dbconn"
	"waypoint/internal/projects"
)

func serverBody(ref config.ProjectRef, extra map[string]any) map[string]any {
	m := map[string]any{
		"host": ref.Host, "port": ref.Port, "user": ref.User, "database": ref.Database,
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestDBTestEndpointNoPasswordField(t *testing.T) {
	f := newFixture(t)
	body := serverBody(config.ProjectRef{Host: "localhost", Port: 5432, User: "u", Database: "d"},
		map[string]any{"engine": "postgres", "project": "p", "password": "hunter2"})
	r := f.do("POST", "/api/db/test", body)
	r.wantCode(400, "invalid")
	if strings.Contains(string(r.body), "hunter2") {
		t.Fatal("the response echoed the password")
	}
}

func TestDBTestReportsTheServer(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		ref := e.fresh(t)
		f := newFixture(t)
		body := serverBody(ref, map[string]any{"engine": e.name, "project": ref.Name})
		out := f.do("POST", "/api/db/test", body).want(200).obj()
		assertEq[any](t, out["reachable"], true)
		assertEq[any](t, out["database_exists"], true)
		assertEq[any](t, out["empty"], true)
		assertEq[any](t, out["has_board"], false)
		assertEq[any](t, out["non_local"], false)
		assertEq[any](t, out["password_var"], dbconn.PasswordVar(ref.Name))
		assertEq[any](t, out["global_password_var"], dbconn.GlobalPasswordVar)

		f.do("POST", "/api/projects", serverBody(ref, map[string]any{"name": ref.Name, "storage": e.name})).want(201)
		out = f.do("POST", "/api/db/test", body).want(200).obj()
		assertEq[any](t, out["has_board"], true)
		assertEq[any](t, out["empty"], false)

		body["database"] = ref.Database + "_missing"
		out = f.do("POST", "/api/db/test", body).want(200).obj()
		assertEq[any](t, out["reachable"], true)
		assertEq[any](t, out["database_exists"], false)
	})
}

func TestDBTestNonLocalFlag(t *testing.T) {
	f := newFixture(t)
	body := map[string]any{"engine": "postgres", "project": "remote", "host": "waypoint-test.invalid", "user": "u", "database": "d"}
	out := f.do("POST", "/api/db/test", body).want(200).obj()
	assertEq[any](t, out["non_local"], true)
	assertEq[any](t, out["reachable"], false)
	if s, _ := out["error"].(string); s == "" {
		t.Fatalf("an unreachable server should explain itself: %v", out)
	}
	body["host"] = "localhost"
	body["port"] = 1
	out = f.do("POST", "/api/db/test", body).want(200).obj()
	assertEq[any](t, out["non_local"], false)
}

func TestDBTestRefusesBadInput(t *testing.T) {
	f := newFixture(t)
	for _, body := range []map[string]any{
		{"engine": "postgres", "project": "p", "host": "localhost", "database": "d"},                          // no user
		{"engine": "oracle", "project": "p", "host": "localhost", "user": "u", "database": "d"},               // engine
		{"engine": "postgres", "host": "localhost", "user": "u", "database": "d"},                             // no project
		{"engine": "postgres", "project": "日本語", "host": "localhost", "user": "u", "database": "d"},           // no password variable
		{"engine": "postgres", "project": "p", "host": "localhost", "user": "u", "database": "d", "ssl": "x"}, // ssl
	} {
		f.do("POST", "/api/db/test", body).wantCode(422, "invalid")
	}
}

func TestCreateServerProject(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		ref := e.fresh(t)
		f := newFixture(t)
		r := f.do("POST", "/api/projects", serverBody(ref, map[string]any{"name": ref.Name, "storage": e.name})).want(201)
		out := r.obj()
		assertEq[any](t, out["host"], ref.Host)
		assertEq[any](t, out["database"], ref.Database)
		assertEq[any](t, out["user"], ref.User)
		assertEq[any](t, out["storage"], e.name)
		if _, has := out["path"]; has {
			t.Errorf("a server project has no path: %v", out)
		}
		if out["error"] != nil {
			t.Errorf("error = %v", out["error"])
		}
		f.do("GET", "/api/projects/"+ref.Name+"/board", nil).want(200)
		var list []projectJSON
		f.do("GET", "/api/projects", nil).want(200).decode(&list)
		assertEq(t, len(list), 1)
		assertEq(t, list[0].Database, ref.Database)
		// A path makes no sense for a server project.
		f.do("POST", "/api/projects", map[string]any{"name": "x", "storage": "postgres", "path": "/tmp/x.db", "user": "u"}).wantCode(422, "invalid")
	})
}

func TestConvertToServerOverHTTP(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		ref := e.fresh(t)
		f := newFixture(t)
		f.addBoard(ref.Name, nil)
		out := f.do("POST", "/api/projects/"+ref.Name+"/convert", serverBody(ref, map[string]any{"to": e.name})).want(200).obj()
		assertEq[any](t, out["storage"], e.name)
		assertEq[any](t, out["database"], ref.Database)
		if _, has := out["path"]; has {
			t.Errorf("path in %v", out)
		}
		f.do("GET", "/api/projects/"+ref.Name+"/board", nil).want(200)
		back := f.do("POST", "/api/projects/"+ref.Name+"/convert", map[string]any{"to": "sqlite", "path": filepath.Join(f.dir, "back.db")}).want(200).obj()
		assertEq[any](t, back["storage"], "sqlite")
		assertEq[any](t, back["retired"], ref.Display())
	})
}

func TestResponsesNeverContainPassword(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		ref := e.fresh(t)
		real := os.Getenv(dbconn.PasswordVar(ref.Name))
		const marker = "s3cr3t-marker"
		t.Setenv(dbconn.PasswordVar(ref.Name), marker)
		f := newFixture(t)
		f.addBoard(ref.Name+"-md", nil)

		check := func(what string, r reply) {
			t.Helper()
			if strings.Contains(string(r.body), marker) || strings.Contains(string(r.body), real) {
				t.Fatalf("%s: the response holds a password: %s", what, r.body)
			}
		}
		test := f.do("POST", "/api/db/test", serverBody(ref, map[string]any{"engine": e.name, "project": ref.Name}))
		check("db/test", test)
		assertEq[any](t, test.obj()["reachable"], false)
		assertContains(t, test.obj()["error"].(string), dbconn.PasswordHelp(ref.Name))

		create := f.do("POST", "/api/projects", serverBody(ref, map[string]any{"name": ref.Name, "storage": e.name}))
		check("create", create)
		create.wantCode(422, "needs_password")
		assertContains(t, create.obj()["error"].(string), dbconn.PasswordHelp(ref.Name))

		// Register it by hand with the wrong password still set, and read it.
		f.register(config.ProjectRef{Name: ref.Name, Storage: e.name, Host: ref.Host, Port: ref.Port, Database: ref.Database, User: ref.User, SSL: "prefer"})
		check("list", f.do("GET", "/api/projects", nil))
		check("board", f.do("GET", "/api/projects/"+ref.Name+"/board", nil))

		f.addBoard("mdboard", nil)
		conv := f.do("POST", "/api/projects/mdboard/convert", serverBody(ref, map[string]any{"to": e.name}))
		check("convert", conv)
		if conv.code == 200 {
			t.Fatalf("a wrong password converted: %s", conv.body)
		}
	})
}

func TestUnreachableServerProjectListsWithError(t *testing.T) {
	f := newFixture(t, "Good")
	f.register(config.ProjectRef{Name: "Down", Storage: "postgres", Host: "127.0.0.1", Port: 1, Database: "d", User: "u", SSL: "prefer"})
	start := time.Now()
	var list []projectJSON
	f.do("GET", "/api/projects", nil).want(200).decode(&list)
	if d := time.Since(start); d > 12*time.Second {
		t.Fatalf("the list took %v", d)
	}
	byName := map[string]projectJSON{}
	for _, p := range list {
		byName[p.Name] = p
	}
	assertEq(t, byName["Good"].Error, "")
	assertContains(t, byName["Down"].Error, "127.0.0.1:"+strconv.Itoa(1))
	assertEq(t, byName["Down"].Path, "")
	f.do("GET", "/api/projects/Down/board", nil).wantCode(503, "unreachable")
	f.do("GET", "/api/projects/Good/board", nil).want(http.StatusOK)
}

// The project is converted even when the old database could not be marked
// retired: the answer must say so, carry the statement to run, and tell open tabs.
func TestConvertThatCouldNotRetireTheOldDatabaseIsStillAnAnswer(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		ref := e.fresh(t)
		f := newFixture(t)
		f.addBoard(ref.Name, nil)
		f.do("POST", "/api/projects/"+ref.Name+"/convert", serverBody(ref, map[string]any{"to": e.name})).want(200)

		restore := projects.SetRetireSourceForTest(func(context.Context) error { return errors.New("connection reset") })
		defer restore()
		events, cancel := f.broker.Subscribe()
		defer cancel()
		out := f.do("POST", "/api/projects/"+ref.Name+"/convert", map[string]any{"to": "sqlite", "path": filepath.Join(f.dir, "y.db")}).want(200).obj()
		assertEq[any](t, out["storage"], "sqlite")
		assertContains(t, out["warning"].(string), "UPDATE waypoint_meta")
		select {
		case ev := <-events:
			assertEq(t, ev.Type, "projects")
		case <-time.After(2 * time.Second):
			t.Fatal("open tabs were not told the project changed")
		}
	})
}
