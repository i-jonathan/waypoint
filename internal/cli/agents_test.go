package cli

import (
	"strings"
	"testing"

	"waypoint/docs"
)

func TestAgentsSnippet(t *testing.T) {
	// Run from an empty directory with no config: agents needs no project.
	stdout, stderr, code := runCLI(t, t.TempDir(), "agents")
	assertEq(t, code, 0)
	assertEq(t, stderr, "")
	for _, want := range []string{
		`This project's board is managed by Waypoint. Prefer the CLI over editing the board file.`,
		`waypoint ls --next --json`,
		`waypoint add "Title" --group "Group" --priority P1`,
		`waypoint move <ID> "<Column>"`,
		`waypoint set <ID> key=value`,
		`waypoint done <ID>`,
		`If the board is a Markdown file such as BOARD.md and you edit it directly, follow the format printed by "waypoint help format"; a card heading without an id gets one automatically. Never edit a SQLite board (a .db file) directly: use the CLI.`,
		"`waypoint ls --json` and `waypoint show <ID> --json` for reading all cards and one card.",
		`Run waypoint from inside this project's folder, or pass --project NAME. "waypoint project ls" shows the board file's path.`,
		`Groups, priorities and columns must already exist on the board. On a Markdown board, read them from the file's front matter before using them.`,
	} {
		assertContains(t, stdout, want)
	}
	assertNotContains(t, stdout, "—")
	assertNotContains(t, stdout, "–")
}

func TestAgentsNeedsNoConfig(t *testing.T) {
	sb := sandboxFor(t)
	runCLI(t, t.TempDir(), "agents")
	mustNotExist(t, sb.config)
}

func TestAgentsRejectsExtraArguments(t *testing.T) {
	stdout, stderr, code := runCLI(t, t.TempDir(), "agents", "extra")
	assertEq(t, code, 2)
	assertEq(t, stdout, "")
	assertContains(t, stderr, "usage: waypoint agents")
}

func TestHelpFormat(t *testing.T) {
	stdout, stderr, code := runCLI(t, t.TempDir(), "help", "format")
	assertEq(t, code, 0)
	assertEq(t, stderr, "")
	assertEq(t, stdout, docs.BoardFormat)
	if !strings.HasPrefix(stdout, "# Waypoint board format") {
		t.Fatalf("help format should start with the doc heading, got %q", stdout[:40])
	}
}

func TestHelpUnknownTopicIsUsageError(t *testing.T) {
	stdout, stderr, code := runCLI(t, t.TempDir(), "help", "nope")
	assertEq(t, code, 2)
	assertEq(t, stdout, "")
	assertContains(t, stderr, `unknown help topic "nope"`)
	assertContains(t, stderr, "waypoint help [format]")

	_, _, code = runCLI(t, t.TempDir(), "help", "format", "extra")
	assertEq(t, code, 2)
}

func TestHelpWithoutTopicPrintsUsage(t *testing.T) {
	stdout, _, code := runCLI(t, t.TempDir(), "help")
	assertEq(t, code, 0)
	assertContains(t, stdout, "Usage: waypoint")
}

func TestUsageListsAgentsAndHelpFormat(t *testing.T) {
	stdout, _, _ := runCLI(t, t.TempDir(), "--help")
	assertContains(t, stdout, "agents")
	assertContains(t, stdout, "help format")
}

func TestHelpAcceptsGlobalFlags(t *testing.T) {
	for _, args := range [][]string{
		{"help", "format", "--json"},
		{"help", "format", "--project", "x"},
		{"help", "--json", "format"},
	} {
		stdout, stderr, code := runCLI(t, t.TempDir(), args...)
		assertEq(t, code, 0)
		assertEq(t, stderr, "")
		assertEq(t, stdout, docs.BoardFormat)
	}
	stdout, stderr, code := runCLI(t, t.TempDir(), "help", "--json")
	assertEq(t, code, 0)
	assertEq(t, stderr, "")
	assertContains(t, stdout, "Usage: waypoint")

	for _, args := range [][]string{{"agents", "--json"}, {"agents", "--project", "x"}} {
		stdout, stderr, code := runCLI(t, t.TempDir(), args...)
		assertEq(t, code, 0)
		assertEq(t, stderr, "")
		assertContains(t, stdout, "waypoint done <ID>")
	}
}
