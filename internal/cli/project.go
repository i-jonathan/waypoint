package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"waypoint/internal/config"
	"waypoint/internal/domain"
	"waypoint/internal/projects"
)

const (
	usageProject = "waypoint project add|ls|rm|convert ..."
	usageAdd     = "waypoint project add NAME [--path FILE] [--storage sqlite|markdown] [--prefix T] [--json]"
	usageConvert = "waypoint project convert NAME --to sqlite|markdown [--path FILE] [--json]"
	usageLs      = "waypoint project ls [--json]"
	usageRm      = "waypoint project rm NAME [--json]"
	usageInit    = "waypoint init [NAME] [--storage sqlite|markdown] [--prefix T] [--json]"

	defaultPrefix = projects.DefaultPrefix
	// What "waypoint init" creates in the current folder, by storage.
	initSQLiteFile   = "waypoint.db"
	initMarkdownFile = "BOARD.md"
)

// projectJSON is the JSON shape of a registered project.
type projectJSON struct {
	Name    string `json:"name"`
	Storage string `json:"storage"`
	Path    string `json:"path"`
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
	pos, code, done := r.parse(usageAdd, args, func(fs *flag.FlagSet) {
		fs.StringVar(&path, "path", "", "")
		fs.StringVar(&storageFlag, "storage", "", "")
		fs.StringVar(&prefix, "prefix", defaultPrefix, "")
	})
	if done {
		return code
	}
	if len(pos) != 1 {
		return r.usageError("expected one project NAME", usageAdd)
	}
	return r.addProject(pos[0], path, prefix, storageFlag, "")
}

func (r *runner) cmdInit(args []string) int {
	var prefix, storageFlag string
	pos, code, done := r.parse(usageInit, args, func(fs *flag.FlagSet) {
		fs.StringVar(&storageFlag, "storage", "", "")
		fs.StringVar(&prefix, "prefix", defaultPrefix, "")
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
	return r.addProject(name, filepath.Join(r.env.Cwd, file), prefix, storageFlag, file)
}

// addProject registers a project and prints the result. pathFlag is the
// --path value ("" for the default under data_dir); for "waypoint init" it is
// the absolute path of the board file in the current folder, and initFile is that
// file's name. The work is shared with the web API (projects.Add).
func (r *runner) addProject(name, pathFlag, prefix, storageFlag, initFile string) int {
	cfgPath, err := config.DefaultPath(r.env.Getenv)
	if err != nil {
		return r.fail(err)
	}
	ref, err := projects.Add(r.ctx, cfgPath, r.env.Getenv, projects.AddOptions{
		Name: name, Path: pathFlag, Prefix: prefix, Cwd: r.env.Cwd,
		Storage: storageFlag, FromInit: initFile != "", InitFile: initFile,
	})
	if err != nil {
		return r.fail(err)
	}

	if r.json {
		r.printJSON(projectJSON{ref.Name, ref.Storage, ref.Path})
	} else {
		fmt.Fprintf(r.env.Stdout, "Added project %s (%s) at %s\n", ref.Name, ref.Storage, ref.Path)
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
			out[i].projectJSON = projectJSON{q.Name, q.Storage, q.Path}
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
		fmt.Fprintf(tw, "%s %s\t%s\t%s\n", mark, q.Name, q.Storage, q.Path)
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
			Removed string `json:"removed"`
			Path    string `json:"path"`
		}{ref.Name, ref.Path})
	} else {
		fmt.Fprintf(r.env.Stdout, "Removed %s from the registry; %s was not deleted\n", ref.Name, ref.Path)
	}
	return exitOK
}

// convertJSON is the JSON shape of a converted project.
type convertJSON struct {
	Name    string   `json:"name"`
	Storage string   `json:"storage"`
	Path    string   `json:"path"`
	Backups []string `json:"backups"`
}

func (r *runner) cmdProjectConvert(args []string) int {
	var to, path string
	pos, code, done := r.parse(usageConvert, args, func(fs *flag.FlagSet) {
		fs.StringVar(&to, "to", "", "")
		fs.StringVar(&path, "path", "", "")
	})
	if done {
		return code
	}
	if len(pos) != 1 {
		return r.usageError("expected one project NAME", usageConvert)
	}
	if to == "" {
		return r.usageError("--to sqlite|markdown is required", usageConvert)
	}
	if path != "" && !filepath.IsAbs(path) {
		if r.env.Cwd == "" {
			return r.fail(errors.New("the working directory is unknown"))
		}
		path = filepath.Join(r.env.Cwd, path)
	}
	cfgPath, err := config.DefaultPath(r.env.Getenv)
	if err != nil {
		return r.fail(err)
	}
	res, err := projects.Convert(r.ctx, cfgPath, r.env.Getenv, pos[0], to, path)
	if err != nil {
		return r.fail(err)
	}
	if r.json {
		r.printJSON(convertJSON{res.Ref.Name, res.Ref.Storage, res.Ref.Path, res.Backups})
	} else {
		fmt.Fprintf(r.env.Stdout, "Converted %s to %s at %s\nThe old files are kept as %s\n",
			res.Ref.Name, res.Ref.Storage, res.Ref.Path, strings.Join(res.Backups, ", "))
	}
	return exitOK
}
