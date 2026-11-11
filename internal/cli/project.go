package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"waypoint/internal/config"
	"waypoint/internal/dbconn"
	"waypoint/internal/domain"
	"waypoint/internal/projects"
)

const (
	usageProject = "waypoint project add|ls|rm|convert ..."
	usageAdd     = "waypoint project add NAME [--path FILE | --storage postgres " + serverUsage + "] [--storage sqlite|markdown] [--prefix T] [--json]"
	usageConvert = "waypoint project convert NAME --to sqlite|markdown|postgres [--path FILE | " + serverUsage + "] [--json]"
	usageLs      = "waypoint project ls [--json]"
	usageRm      = "waypoint project rm NAME [--json]"
	usageInit    = "waypoint init [NAME] [--storage sqlite|markdown|postgres " + serverUsage + "] [--prefix T] [--json]"
	serverUsage  = "[--host H] [--port N] --user U [--database D] [--ssl disable|prefer|require|verify-full] [--create-database]"

	defaultPrefix = projects.DefaultPrefix
	// What "waypoint init" creates in the current folder, by storage.
	initSQLiteFile   = "waypoint.db"
	initMarkdownFile = "BOARD.md"
)

// projectJSON is the JSON shape of a registered project.
type projectJSON struct {
	Name     string `json:"name"`
	Storage  string `json:"storage"`
	Path     string `json:"path,omitempty"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Database string `json:"database,omitempty"`
	User     string `json:"user,omitempty"`
}

func projectJSONOf(p config.ProjectRef) projectJSON {
	return projectJSON{Name: p.Name, Storage: p.Storage, Path: p.Path, Host: p.Host, Port: p.Port, Database: p.Database, User: p.User}
}

// serverFlags holds the flags that choose a database server. There is no
// password flag on purpose: a password never goes on a command line.
type serverFlags struct {
	host, user, database, ssl string
	port                      int
	createDatabase            bool
}

func (f *serverFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.host, "host", "", "")
	fs.IntVar(&f.port, "port", 0, "")
	fs.StringVar(&f.user, "user", "", "")
	fs.StringVar(&f.database, "database", "", "")
	fs.StringVar(&f.ssl, "ssl", "", "")
	fs.BoolVar(&f.createDatabase, "create-database", false, "")
}

// used reports whether any server flag was given.
func (f *serverFlags) used() bool {
	return f.host != "" || f.port != 0 || f.user != "" || f.database != "" || f.ssl != "" || f.createDatabase
}

// options returns the server options for storage kind, or nil for a file
// storage (and then no server flag may have been given).
func (f *serverFlags) options(kind string) (*projects.ServerOptions, error) {
	if !slices.Contains(projects.ServerEngines(), kind) {
		if f.used() {
			return nil, fmt.Errorf("%w: --host, --port, --user, --database, --ssl and --create-database need --storage postgres", domain.ErrInvalid)
		}
		return nil, nil
	}
	return &projects.ServerOptions{
		Engine: kind, Host: f.host, Port: f.port, User: f.user, Database: f.database, SSL: f.ssl, CreateDatabase: f.createDatabase,
	}, nil
}

func (r *runner) cmdProject(args []string) int {
	if len(args) == 0 {
		return r.usageError("missing project command", usageProject)
	}
	switch args[0] {
	case "-h", "--help", "-help":
		fmt.Fprintf(r.env.Stdout, "usage: %s\n", usageProject)
		return exitOK
	case "add":
		return r.cmdProjectAdd(args[1:])
	case "ls":
		return r.cmdProjectLs(args[1:])
	case "rm":
		return r.cmdProjectRm(args[1:])
	case "convert":
		return r.cmdProjectConvert(args[1:])
	}
	return r.usageError(fmt.Sprintf("unknown project command %q", args[0]), usageProject)
}

func (r *runner) cmdProjectAdd(args []string) int {
	var path, prefix, storageFlag string
	var sf serverFlags
	pos, code, done := r.parse(usageAdd, args, func(fs *flag.FlagSet) {
		fs.StringVar(&path, "path", "", "")
		fs.StringVar(&storageFlag, "storage", "", "")
		fs.StringVar(&prefix, "prefix", defaultPrefix, "")
		sf.register(fs)
	})
	if done {
		return code
	}
	if len(pos) != 1 {
		return r.usageError("expected one project NAME", usageAdd)
	}
	return r.addProject(pos[0], path, prefix, storageFlag, "", &sf)
}

func (r *runner) cmdInit(args []string) int {
	var prefix, storageFlag string
	var sf serverFlags
	pos, code, done := r.parse(usageInit, args, func(fs *flag.FlagSet) {
		fs.StringVar(&storageFlag, "storage", "", "")
		fs.StringVar(&prefix, "prefix", defaultPrefix, "")
		sf.register(fs)
	})
	if done {
		return code
	}
	if len(pos) > 1 {
		return r.usageError("expected at most one project NAME", usageInit)
	}
	if r.env.Cwd == "" {
		return r.fail(errors.New("the working directory is unknown"))
	}
	name := filepath.Base(r.env.Cwd)
	if len(pos) == 1 {
		name = pos[0]
	}
	if slices.Contains(projects.ServerEngines(), storageFlag) { // no file in this folder
		return r.addProject(name, "", prefix, storageFlag, "", &sf)
	}
	file := initSQLiteFile
	switch storageFlag {
	case "markdown":
		file = initMarkdownFile
	case "":
		// A Waypoint BOARD.md already here is the board the user means, as it was
		// before SQLite became the default: register it rather than start another.
		md := filepath.Join(r.env.Cwd, initMarkdownFile)
		if _, err := os.Lstat(filepath.Join(r.env.Cwd, initSQLiteFile)); err != nil && isWaypointBoard(r.ctx, md) {
			file = initMarkdownFile
		}
	}
	return r.addProject(name, filepath.Join(r.env.Cwd, file), prefix, storageFlag, file, &sf)
}

// addProject registers a project and prints the result. pathFlag is the
// --path value ("" for the default under data_dir); for "waypoint init" it is
// the absolute path of the board file in the current folder, and initFile is that
// file's name. The work is shared with the web API (projects.Add).
func (r *runner) addProject(name, pathFlag, prefix, storageFlag, initFile string, sf *serverFlags) int {
	cfgPath, err := config.DefaultPath(r.env.Getenv)
	if err != nil {
		return r.fail(err)
	}
	server, err := sf.options(storageFlag)
	if err != nil {
		return r.fail(err)
	}
	if server != nil && pathFlag != "" {
		return r.fail(fmt.Errorf("%w: a %s project has no file, so --path does not apply", domain.ErrInvalid, storageFlag))
	}
	ref, err := projects.Add(r.ctx, cfgPath, r.env.Getenv, projects.AddOptions{
		Name: name, Path: pathFlag, Prefix: prefix, Cwd: r.env.Cwd,
		Storage: storageFlag, FromInit: initFile != "", InitFile: initFile,
		Server: server,
		Warn:   func(msg string) { fmt.Fprintln(r.env.Stderr, msg) },
	})
	if err != nil {
		return r.fail(err)
	}

	if r.json {
		r.printJSON(projectJSONOf(ref))
	} else {
		fmt.Fprintf(r.env.Stdout, "Added project %s (%s) at %s\n", ref.Name, ref.Storage, ref.Display())
		if ref.IsServer() {
			fmt.Fprintf(r.env.Stdout, "Run commands with --project %s or set WAYPOINT_PROJECT=%s: this folder has no board file to find it by.\n", ref.Name, ref.Name)
			fmt.Fprintf(r.env.Stdout, "The password is read from the environment: %s.\n", dbconn.PasswordHelp(ref.Name))
		}
	}
	return exitOK
}

func (r *runner) cmdProjectLs(args []string) int {
	pos, code, done := r.parse(usageLs, args, nil)
	if done {
		return code
	}
	if len(pos) != 0 {
		return r.usageError("unexpected argument "+fmt.Sprintf("%q", pos[0]), usageLs)
	}
	cfg, err := r.loadConfig()
	if err != nil {
		return r.fail(err)
	}
	current, _ := r.resolveProject(cfg, r.project) // no current project is fine here

	if r.json {
		out := make([]struct {
			projectJSON
			Current bool `json:"current"`
		}, len(cfg.Projects))
		for i, q := range cfg.Projects {
			out[i].projectJSON = projectJSONOf(q)
			out[i].Current = q == current
		}
		r.printJSON(out)
		return exitOK
	}
	if len(cfg.Projects) == 0 {
		fmt.Fprintln(r.env.Stdout, `No projects yet. Run "waypoint project add NAME".`)
		return exitOK
	}
	tw := tabwriter.NewWriter(r.env.Stdout, 0, 0, 2, ' ', 0)
	for _, q := range cfg.Projects {
		mark := " "
		if q == current {
			mark = "*"
		}
		fmt.Fprintf(tw, "%s %s\t%s\t%s\n", mark, q.Name, q.Storage, q.Display())
	}
	tw.Flush()
	return exitOK
}

func (r *runner) cmdProjectRm(args []string) int {
	pos, code, done := r.parse(usageRm, args, nil)
	if done {
		return code
	}
	if len(pos) != 1 {
		return r.usageError("expected one project NAME", usageRm)
	}
	cfgPath, err := config.DefaultPath(r.env.Getenv)
	if err != nil {
		return r.fail(err)
	}
	var ref config.ProjectRef
	err = config.Update(cfgPath, r.env.Getenv, func(cfg *config.Config) error {
		var ok bool
		if ref, ok = cfg.Find(pos[0]); !ok {
			return fmt.Errorf("%w: no project named %q", domain.ErrNotFound, pos[0])
		}
		return cfg.Remove(pos[0])
	})
	if err != nil {
		return r.fail(err)
	}
	if r.json {
		r.printJSON(struct {
			Removed  string `json:"removed"`
			Path     string `json:"path,omitempty"`
			Host     string `json:"host,omitempty"`
			Port     int    `json:"port,omitempty"`
			Database string `json:"database,omitempty"`
		}{ref.Name, ref.Path, ref.Host, ref.Port, ref.Database})
	} else if ref.IsServer() {
		fmt.Fprintf(r.env.Stdout, "Removed %s from the registry; the database at %s was not changed\n", ref.Name, ref.Display())
	} else {
		fmt.Fprintf(r.env.Stdout, "Removed %s from the registry; %s was not deleted\n", ref.Name, ref.Path)
	}
	return exitOK
}

// convertJSON is the JSON shape of a converted project.
type convertJSON struct {
	projectJSON
	Backups []string `json:"backups"`
	Retired string   `json:"retired,omitempty"`
}

func (r *runner) cmdProjectConvert(args []string) int {
	var to, path string
	var sf serverFlags
	pos, code, done := r.parse(usageConvert, args, func(fs *flag.FlagSet) {
		fs.StringVar(&to, "to", "", "")
		fs.StringVar(&path, "path", "", "")
		sf.register(fs)
	})
	if done {
		return code
	}
	if len(pos) != 1 {
		return r.usageError("expected one project NAME", usageConvert)
	}
	if to == "" {
		return r.usageError("--to sqlite|markdown|postgres is required", usageConvert)
	}
	if path != "" && !filepath.IsAbs(path) {
		if r.env.Cwd == "" {
			return r.fail(errors.New("the working directory is unknown"))
		}
		path = filepath.Join(r.env.Cwd, path)
	}
	server, err := sf.options(to)
	if err != nil {
		return r.fail(err)
	}
	cfgPath, err := config.DefaultPath(r.env.Getenv)
	if err != nil {
		return r.fail(err)
	}
	res, err := projects.Convert(r.ctx, cfgPath, r.env.Getenv, pos[0], projects.Target{
		Storage: to, Path: path, Server: server,
		Warn: func(msg string) { fmt.Fprintln(r.env.Stderr, msg) },
	})
	if err != nil {
		if res.Ref.Name != "" && !r.json { // converted, but something was left behind
			fmt.Fprintf(r.env.Stdout, "Converted %s to %s at %s\n", res.Ref.Name, res.Ref.Storage, res.Ref.Display())
		}
		return r.fail(err)
	}
	if r.json {
		r.printJSON(convertJSON{projectJSONOf(res.Ref), nonNilStrings(res.Backups), res.Retired})
		return exitOK
	}
	fmt.Fprintf(r.env.Stdout, "Converted %s to %s at %s\n", res.Ref.Name, res.Ref.Storage, res.Ref.Display())
	switch {
	case res.Retired != "":
		fmt.Fprintf(r.env.Stdout, "The old database at %s is kept, marked as retired\n", res.Retired)
	case len(res.Backups) > 0:
		fmt.Fprintf(r.env.Stdout, "The old files are kept as %s\n", strings.Join(res.Backups, ", "))
	}
	if res.Ref.IsServer() {
		fmt.Fprintf(r.env.Stdout, "The password is read from the environment: %s.\n", dbconn.PasswordHelp(res.Ref.Name))
	}
	return exitOK
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
