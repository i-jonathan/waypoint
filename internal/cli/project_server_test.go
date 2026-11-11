package cli

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"waypoint/internal/config"
	"waypoint/internal/dbconn"
	"waypoint/internal/storage/servertest"
)

func flagsFor(ref config.ProjectRef) []string {
	return []string{"--storage", "postgres", "--host", ref.Host, "--port", strconv.Itoa(ref.Port), "--user", ref.User, "--database", ref.Database}
}

func TestInitPostgresFlags(t *testing.T) {
	ref := servertest.Postgres(t)
	dir := t.TempDir()
	args := append([]string{"init", ref.Name}, flagsFor(ref)...)
	stdout, stderr, code := runCLI(t, dir, args...)
	assertEq(t, code, 0)
	assertEq(t, stderr, "")
	assertContains(t, stdout, "Added project servertest (postgres) at "+ref.Display())
	assertContains(t, stdout, "WAYPOINT_PROJECT")
	mustNotExist(t, dir+"/waypoint.db")
	assertContains(t, readFile(t, sandboxFor(t).config), "database: "+ref.Database)

	// The board is usable by name.
	out, _, code := runCLIEnv(t, dir, map[string]string{"WAYPOINT_PROJECT": ref.Name}, "add", "First card")
	assertEq(t, code, 0)
	assertContains(t, out, "T-1")
}

func TestProjectLsShowsServer(t *testing.T) {
	ref := servertest.Postgres(t)
	dir := t.TempDir()
	_, _, code := runCLI(t, dir, append([]string{"project", "add", ref.Name}, flagsFor(ref)...)...)
	assertEq(t, code, 0)

	stdout, _, code := runCLI(t, dir, "project", "ls")
	assertEq(t, code, 0)
	assertContains(t, stdout, ref.Display())
	assertContains(t, stdout, "postgres")

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
}

func TestProjectRmKeepsDatabase(t *testing.T) {
	ref := servertest.Postgres(t)
	dir := t.TempDir()
	runCLI(t, dir, append([]string{"project", "add", ref.Name}, flagsFor(ref)...)...)
	stdout, _, code := runCLI(t, dir, "project", "rm", ref.Name)
	assertEq(t, code, 0)
	assertContains(t, stdout, "not changed")
	assertContains(t, stdout, ref.Display())
	// Adding it again adopts the board that is still there.
	_, _, code = runCLI(t, dir, append([]string{"project", "add", ref.Name}, flagsFor(ref)...)...)
	assertEq(t, code, 0)
}

func TestNoPasswordFlag(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"project", "add", "X", "--storage", "postgres", "--user", "u", "--password", "hunter2"},
		{"init", "X", "--storage", "postgres", "--user", "u", "--password", "hunter2"},
	} {
		stdout, stderr, code := runCLI(t, dir, args...)
		assertEq(t, code, 2)
		assertEq(t, stdout, "")
		assertContains(t, stderr, "usage: waypoint")
	}
	mustNotExist(t, sandboxFor(t).config)
}

func TestPasswordNotInOutput(t *testing.T) {
	ref := servertest.Postgres(t)
	pw := os.Getenv(dbconn.PasswordVar(ref.Name))
	dir := t.TempDir()
	var all strings.Builder
	for _, args := range [][]string{
		append([]string{"project", "add", ref.Name, "--json"}, flagsFor(ref)...),
		{"project", "ls"}, {"project", "ls", "--json"},
		{"project", "rm", ref.Name, "--json"},
		append([]string{"project", "add", ref.Name}, flagsFor(ref)...),
	} {
		o, e, _ := runCLI(t, dir, args...)
		all.WriteString(o + e)
	}
	// And a failure path: the wrong password.
	t.Setenv(dbconn.PasswordVar(ref.Name), "s3cr3t-marker")
	runCLI(t, dir, "project", "rm", ref.Name)
	o, e, code := runCLI(t, dir, append([]string{"project", "add", ref.Name}, flagsFor(ref)...)...)
	assertEq(t, code, 1)
	all.WriteString(o + e)
	assertContains(t, e, dbconn.PasswordHelp(ref.Name))
	assertNotContains(t, all.String(), pw)
	assertNotContains(t, all.String(), "s3cr3t-marker")
	assertNotContains(t, readFile(t, sandboxFor(t).config), pw)
}

func TestWarnsOnStderrForNonLocalServer(t *testing.T) {
	dir := t.TempDir()
	stdout, stderr, code := runCLI(t, dir, "project", "add", "remote", "--storage", "postgres", "--host", "waypoint-test.invalid", "--user", "u")
	assertEq(t, code, 1)
	assertEq(t, stdout, "")
	assertContains(t, stderr, dbconn.NonLocalWarning)
	mustNotExist(t, sandboxFor(t).config)
}

func TestServerStorageNeedsUser(t *testing.T) {
	dir := t.TempDir()
	_, stderr, code := runCLI(t, dir, "project", "add", "x", "--storage", "postgres")
	assertEq(t, code, 1)
	assertContains(t, stderr, "--user")
}

func TestProjectConvertToPostgresAndBack(t *testing.T) {
	ref := servertest.Postgres(t)
	dir := t.TempDir()
	_, _, code := runCLI(t, dir, "project", "add", ref.Name, "--path", dir+"/c.db")
	assertEq(t, code, 0)
	runCLIEnv(t, dir, map[string]string{"WAYPOINT_PROJECT": ref.Name}, "add", "Card one")

	stdout, stderr, code := runCLI(t, dir, "project", "convert", ref.Name, "--to", "postgres",
		"--host", ref.Host, "--port", strconv.Itoa(ref.Port), "--user", ref.User, "--database", ref.Database)
	assertEq(t, code, 0)
	assertEq(t, stderr, "")
	assertContains(t, stdout, "Converted servertest to postgres at "+ref.Display())
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
}
