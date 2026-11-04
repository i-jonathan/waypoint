// Package cli is the waypoint command line. Run is the only entry point; it
// reads nothing from the process (environment, working directory, clock)
// except through Env, so tests run in a sandbox.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"waypoint/internal/app"
	"waypoint/internal/config"
	"waypoint/internal/domain"
	"waypoint/internal/storage"
	"waypoint/internal/storage/markdown"
)

// Env is everything the CLI takes from the outside world.
type Env struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Getenv         func(string) string
	Cwd            string
	Now            app.Clock
}

// Exit codes returned by Run.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

const usageText = `Waypoint, a local kanban tracker

Usage: waypoint <command> [flags]

Commands:
  init [NAME]          Register ./BOARD.md in this directory as a project
  project add NAME     Add a project (--path FILE, --prefix T)
  project ls           List projects
  project rm NAME      Remove a project from the registry (the file is kept)
  ls [QUERY...]        List cards (--column X, --group G, --blocked, --next, --archived)
  show ID              Show one card with its body
  add TITLE            Add a card (--column, --group, --priority, --depends, --body TEXT|-)
  move ID COLUMN       Move a card (--top or --index N; bottom by default)
  set ID key=value...  Change fields (title, group, priority, depends, body, or any extra field)
  check ID N           Toggle checklist item N
  done ID              Move a card to the done column
  archive              Archive every card in a done column
  restore ID           Bring an archived card back (--column X)
  rm ID                Delete a card
  agents               Print a snippet to paste into AGENTS.md or CLAUDE.md
  help format          Print the board file format guide

Every command accepts --json and --project NAME.
`

// runner carries one invocation: its context, its Env and the flags every
// command shares.
type runner struct {
	ctx     context.Context
	env     Env
	json    bool   // --json
	project string // --project

	stdinRead bool // standard input has been consumed (see readStdin)
}

// Run executes one waypoint command and returns the exit code: 0 on success,
// 1 on an error, 2 on a usage error.
func Run(ctx context.Context, args []string, env Env) int {
	r := &runner{ctx: ctx, env: env}
	if len(args) == 0 {
		fmt.Fprint(env.Stdout, usageText)
		return exitOK
	}
	switch args[0] {
	case "-h", "--help", "-help":
		fmt.Fprint(env.Stdout, usageText)
		return exitOK
	case "help":
		return r.cmdHelp(args[1:])
	case "agents":
		return r.cmdAgents(args[1:])
	case "init":
		return r.cmdInit(args[1:])
	case "project":
		return r.cmdProject(args[1:])
	case "ls":
		return r.cmdLs(args[1:])
	case "show":
		return r.cmdShow(args[1:])
	case "add":
		return r.cmdAdd(args[1:])
	case "move":
		return r.cmdMove(args[1:])
	case "set":
		return r.cmdSet(args[1:])
	case "check":
		return r.cmdCheck(args[1:])
	case "done":
		return r.cmdDone(args[1:])
	case "archive":
		return r.cmdArchive(args[1:])
	case "restore":
		return r.cmdRestore(args[1:])
	case "rm":
		return r.cmdRm(args[1:])
	}
	return r.usageError(fmt.Sprintf("unknown command %q", args[0]), "waypoint <command> [flags], see waypoint --help")
}

// parse registers --json and --project beside the command's own flags (set up
// by setup), parses args with flags allowed anywhere, and returns the
// positional arguments. When done is true the command is finished and code is
// its exit code: a bad flag is a usage error, and --help prints the usage.
func (r *runner) parse(usage string, args []string, setup func(*flag.FlagSet)) (pos []string, code int, done bool) {
	fs := flag.NewFlagSet("waypoint", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&r.json, "json", false, "")
	fs.StringVar(&r.project, "project", "", "")
	if setup != nil {
		setup(fs)
	}
	pos, err := parseInterspersed(fs, args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		fmt.Fprintf(r.env.Stdout, "usage: %s\n", usage)
		return nil, exitOK, true
	case err != nil:
		msg := err.Error()
		if strings.HasPrefix(msg, "flag provided but not defined") {
			usage += " (put -- before values that start with -)"
		}
		return nil, r.usageError(msg, usage), true
	}
	return pos, 0, false
}

// parseInterspersed parses args with flags allowed before, between and after
// positional arguments, and returns the positionals in order. Everything
// after the first bare "--" is positional, verbatim, wherever that "--" sits.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if endedWithTerminator(fs, args[:len(args)-len(rest)]) {
			return append(pos, rest...), nil
		}
		if len(rest) == 0 {
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

// endedWithTerminator reports whether consumed, the arguments one fs.Parse
// call used up, contains a "--" that ended flag parsing. A "--" that is the
// value of a flag such as "--path --" is not a terminator, so the tokens are
// walked in order, skipping the value of each flag that takes one.
func endedWithTerminator(fs *flag.FlagSet, consumed []string) bool {
	for i := 0; i < len(consumed); i++ {
		t := consumed[i]
		if t == "--" {
			return true
		}
		if len(t) < 2 || t[0] != '-' || strings.Contains(t, "=") {
			continue
		}
		f := fs.Lookup(strings.TrimLeft(t, "-"))
		if f == nil {
			continue
		}
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); !ok || !bf.IsBoolFlag() {
			i++ // the next token is this flag's value
		}
	}
	return false
}

// usageError prints a one-line usage message to stderr and returns the usage
// exit code.
func (r *runner) usageError(msg, usage string) int {
	fmt.Fprintf(r.env.Stderr, "waypoint: %s; usage: %s\n", msg, usage)
	return exitUsage
}

// fail reports err and returns the error exit code. Plain output goes to
// stderr as "waypoint: <message>"; with --json it is an object on stdout.
func (r *runner) fail(err error) int {
	if !r.json {
		fmt.Fprintf(r.env.Stderr, "waypoint: %s\n", err)
		return exitError
	}
	r.printJSON(struct {
		Error string `json:"error"`
		Code  string `json:"code"`
		Line  int    `json:"line,omitempty"`
	}{err.Error(), errorCode(err), errorLine(err)})
	return exitError
}

// printJSON writes v and a newline to stdout.
func (r *runner) printJSON(v any) {
	enc := json.NewEncoder(r.env.Stdout)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

// errorCode names an error for machine readers: one of not_found,
// unknown_column, move_not_allowed, wip_limit, conflict, invalid, parse, or
// error for anything else. The CLI and the HTTP API share this mapping.
func errorCode(err error) string {
	var pe *markdown.ParseError
	switch {
	case errors.As(err, &pe), errors.Is(err, markdown.ErrNotABoard):
		return "parse"
	case errors.Is(err, domain.ErrNotFound):
		return "not_found"
	case errors.Is(err, domain.ErrUnknownColumn):
		return "unknown_column"
	case errors.Is(err, domain.ErrMoveNotAllowed):
		return "move_not_allowed"
	case errors.Is(err, domain.ErrWIPLimit):
		return "wip_limit"
	case errors.Is(err, domain.ErrConflict):
		return "conflict"
	case errors.Is(err, domain.ErrInvalid),
		errors.Is(err, domain.ErrUnknownGroup),
		errors.Is(err, domain.ErrUnknownPriority):
		return "invalid"
	}
	return "error"
}

// errorLine is the 1-based line of a parse error, or 0 if err has none.
func errorLine(err error) int {
	var pe *markdown.ParseError
	if errors.As(err, &pe) {
		return pe.Line
	}
	return 0
}

// loadConfig reads the global config. A missing file gives the defaults and is
// not created.
func (r *runner) loadConfig() (*config.Config, error) {
	path, err := config.DefaultPath(r.env.Getenv)
	if err != nil {
		return nil, err
	}
	return config.Load(path, r.env.Getenv)
}

// noProjectError is the "cannot tell which project" error. Machine readers
// see it as not_found.
type noProjectError struct{ msg string }

func (e noProjectError) Error() string { return e.msg }

func (noProjectError) Unwrap() error { return domain.ErrNotFound }

// unregisteredHere reports whether the working directory holds a Waypoint board
// named exactly BOARD.md that is not registered. Any other file or folder of
// that name is not Waypoint's, and says nothing about which project this is.
func (r *runner) unregisteredHere(cfg *config.Config) bool {
	// The directory listing has the real name; on a case-insensitive disk a
	// Stat would also find board.md.
	entries, err := os.ReadDir(r.env.Cwd)
	if err != nil || !slices.ContainsFunc(entries, func(e os.DirEntry) bool { return e.Name() == initBoardFile }) {
		return false
	}
	here := filepath.Join(r.env.Cwd, initBoardFile)
	if info, err := os.Stat(here); err != nil || !info.Mode().IsRegular() {
		return false
	}
	data, err := os.ReadFile(here)
	if err != nil || !markdown.IsBoard(data) {
		return false
	}
	_, registered := registeredAt(cfg, here)
	return !registered
}

// resolveProject picks the project a command acts on: name (the --project
// value) if given, else $WAYPOINT_PROJECT, else the project found from the
// working directory (see config.FindByDir). An unregistered Waypoint board at
// ./BOARD.md stops that search, because a board in the working directory is the
// one the user means, whatever is registered above it. Anything else is an error,
// so a card never lands on a board the user did not mean. A named project that
// does not exist is an error, not a fall-through.
func (r *runner) resolveProject(cfg *config.Config, name string) (config.ProjectRef, error) {
	if name == "" {
		name = r.env.Getenv("WAYPOINT_PROJECT")
	}
	if name != "" {
		ref, ok := cfg.Find(name)
		if !ok {
			return config.ProjectRef{}, fmt.Errorf("%w: no project named %q", domain.ErrNotFound, name)
		}
		return ref, nil
	}
	if r.env.Cwd != "" {
		if r.unregisteredHere(cfg) {
			return config.ProjectRef{}, noProjectError{initBoardFile + ` here isn't registered; run "waypoint init"`}
		}
		if ref, ok := cfg.FindByDir(r.env.Cwd); ok {
			return ref, nil
		}
	}
	return config.ProjectRef{}, noProjectError{`no project here; use --project NAME or run "waypoint init"`}
}

// service resolves the current project (see resolveProject) and returns a
// Service over its board together with its registry entry. name is the
// --project value, normally r.project after r.parse. It does not read the
// board, so a missing or broken file shows up on the first service call.
func (r *runner) service(name string) (*app.Service, config.ProjectRef, error) {
	cfg, err := r.loadConfig()
	if err != nil {
		return nil, config.ProjectRef{}, err
	}
	ref, err := r.resolveProject(cfg, name)
	if err != nil {
		return nil, config.ProjectRef{}, err
	}
	repo, err := storage.Open(ref)
	if err != nil {
		return nil, ref, err
	}
	return app.NewService(repo, r.env.Now), ref, nil
}
