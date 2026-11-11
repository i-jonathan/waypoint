package cli

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"waypoint/internal/config"
	"waypoint/internal/dbconn"
)

func flagsFor(ref config.ProjectRef) []string {
	return []string{"--storage", ref.Storage, "--host", ref.Host, "--port", strconv.Itoa(ref.Port), "--user", ref.User, "--database", ref.Database}
}

func TestInitPostgresFlags(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		ref := e.fresh(t)
		dir := t.TempDir()
		args := append([]string{"init", ref.Name}, flagsFor(ref)...)
		stdout, stderr, code := runCLI(t, dir, args...)
		assertEq(t, code, 0)
		assertEq(t, stderr, "")
		assertContains(t, stdout, "Added project servertest ("+e.name+") at "+ref.Display())
		assertContains(t, stdout, "WAYPOINT_PROJECT")
		mustNotExist(t, dir+"/waypoint.db")
		assertContains(t, readFile(t, sandboxFor(t).config), "database: "+ref.Database)

		// The board is usable by name.
		out, _, code := runCLIEnv(t, dir, map[string]string{"WAYPOINT_PROJECT": ref.Name}, "add", "First card")
		assertEq(t, code, 0)
		assertContains(t, out, "T-1")
	})
}

func TestProjectLsShowsServer(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		ref := e.fresh(t)
		dir := t.TempDir()
		_, _, code := runCLI(t, dir, append([]string{"project", "add", ref.Name}, flagsFor(ref)...)...)
		assertEq(t, code, 0)

		stdout, _, code := runCLI(t, dir, "project", "ls")
		assertEq(t, code, 0)
		assertContains(t, stdout, ref.Display())
		assertContains(t, stdout, e.name)

		stdout, _, code = runCLI(t, dir, "project", "ls", "--json")
		assertEq(t, code, 0)
		var rows []map[string]any
		if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
			t.Fatal(err)
		}
		row := rows[0]
		if _, has := row["path"]; has {
			t.Errorf("a server project has no path key: %v", row)
		}
		if row["host"] != ref.Host || row["database"] != ref.Database || row["user"] != ref.User || row["port"] != float64(ref.Port) {
			t.Errorf("row = %v", row)
		}
	})
}

func TestProjectRmKeepsDatabase(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		ref := e.fresh(t)
		dir := t.TempDir()
		runCLI(t, dir, append([]string{"project", "add", ref.Name}, flagsFor(ref)...)...)
		stdout, _, code := runCLI(t, dir, "project", "rm", ref.Name)
		assertEq(t, code, 0)
		assertContains(t, stdout, "not changed")
		assertContains(t, stdout, ref.Display())
		// Adding it again adopts the board that is still there.
		_, _, code = runCLI(t, dir, append([]string{"project", "add", ref.Name}, flagsFor(ref)...)...)
		assertEq(t, code, 0)
	})
}

func TestNoPasswordFlag(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		dir := t.TempDir()
		for _, args := range [][]string{
			{"project", "add", "X", "--storage", e.name, "--user", "u", "--password", "hunter2"},
			{"init", "X", "--storage", e.name, "--user", "u", "--password", "hunter2"},
		} {
			stdout, stderr, code := runCLI(t, dir, args...)
			assertEq(t, code, 2)
			assertEq(t, stdout, "")
			assertContains(t, stderr, "usage: waypoint")
		}
		mustNotExist(t, sandboxFor(t).config)
	})
}

func TestPasswordNotInOutput(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		ref := e.fresh(t)
		pw := os.Getenv(dbconn.PasswordVar(ref.Name))
		dir := t.TempDir()
		var all strings.Builder
		for _, args := range [][]string{
			append([]string{"project", "add", ref.Name, "--json"}, flagsFor(ref)...),
			{"project", "ls"}, {"project", "ls", "--json"},
			{"project", "rm", ref.Name, "--json"},
			append([]string{"project", "add", ref.Name}, flagsFor(ref)...),
		} {
			o, se, _ := runCLI(t, dir, args...)
			all.WriteString(o + se)
		}
		// And a failure path: the wrong password.
		t.Setenv(dbconn.PasswordVar(ref.Name), "s3cr3t-marker")
		runCLI(t, dir, "project", "rm", ref.Name)
		o, se, code := runCLI(t, dir, append([]string{"project", "add", ref.Name}, flagsFor(ref)...)...)
		assertEq(t, code, 1)
		all.WriteString(o + se)
		assertContains(t, se, dbconn.PasswordHelp(ref.Name))
		assertNotContains(t, all.String(), pw)
		assertNotContains(t, all.String(), "s3cr3t-marker")
		assertNotContains(t, readFile(t, sandboxFor(t).config), pw)
	})
}

func TestWarnsOnStderrForNonLocalServer(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		dir := t.TempDir()
		stdout, stderr, code := runCLI(t, dir, "project", "add", "remote", "--storage", e.name, "--host", "waypoint-test.invalid", "--user", "u")
		assertEq(t, code, 1)
		assertEq(t, stdout, "")
		assertContains(t, stderr, dbconn.NonLocalWarning)
		mustNotExist(t, sandboxFor(t).config)
	})
}

func TestServerStorageNeedsUser(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		dir := t.TempDir()
		_, stderr, code := runCLI(t, dir, "project", "add", "x", "--storage", e.name)
		assertEq(t, code, 1)
		assertContains(t, stderr, "--user")
	})
}

func TestProjectConvertToPostgresAndBack(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		ref := e.fresh(t)
		dir := t.TempDir()
		_, _, code := runCLI(t, dir, "project", "add", ref.Name, "--path", dir+"/c.db")
		assertEq(t, code, 0)
		runCLIEnv(t, dir, map[string]string{"WAYPOINT_PROJECT": ref.Name}, "add", "Card one")

		stdout, stderr, code := runCLI(t, dir, "project", "convert", ref.Name, "--to", e.name,
			"--host", ref.Host, "--port", strconv.Itoa(ref.Port), "--user", ref.User, "--database", ref.Database)
		assertEq(t, code, 0)
		assertEq(t, stderr, "")
		assertContains(t, stdout, "Converted servertest to "+e.name+" at "+ref.Display())
		assertContains(t, stdout, "kept as")
		assertContains(t, stdout, dbconn.PasswordHelp(ref.Name))

		stdout, _, code = runCLI(t, dir, "project", "convert", ref.Name, "--to", "markdown", "--path", dir+"/c.md", "--json")
		assertEq(t, code, 0)
		var out map[string]any
		if err := json.Unmarshal([]byte(stdout), &out); err != nil {
			t.Fatal(err)
		}
		if out["retired"] != ref.Display() || out["storage"] != "markdown" {
			t.Errorf("json = %v", out)
		}
		if b, _ := out["backups"].([]any); len(b) != 0 {
			t.Errorf("backups = %v", out["backups"])
		}
		list, _, _ := runCLIEnv(t, dir, map[string]string{"WAYPOINT_PROJECT": ref.Name}, "ls")
		assertContains(t, list, "Card one")
	})
}

func TestServerStorageRefusesPath(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		dir := t.TempDir()
		_, stderr, code := runCLI(t, dir, "project", "add", "x", "--storage", e.name, "--user", "u", "--path", dir+"/x.db")
		assertEq(t, code, 1)
		assertContains(t, stderr, "--path")
		mustNotExist(t, sandboxFor(t).config)
	})
}
