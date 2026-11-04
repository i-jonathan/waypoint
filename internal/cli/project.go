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
	"waypoint/internal/storage"
)

const (
	usageProject = "waypoint project add|ls|rm ..."
	usageAdd     = "waypoint project add NAME [--path FILE] [--prefix T] [--json]"
	usageLs      = "waypoint project ls [--json]"
	usageRm      = "waypoint project rm NAME [--json]"
	usageInit    = "waypoint init [NAME] [--prefix T] [--json]"

	defaultPrefix = "T"
	initBoardFile = "BOARD.md"
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
	}
	return r.usageError(fmt.Sprintf("unknown project command %q", args[0]), usageProject)
}

func (r *runner) cmdProjectAdd(args []string) int {
	var path, prefix string
	pos, code, done := r.parse(usageAdd, args, func(fs *flag.FlagSet) {
		fs.StringVar(&path, "path", "", "")
		fs.StringVar(&prefix, "prefix", defaultPrefix, "")
	})
	if done {
		return code
	}
	if len(pos) != 1 {
		return r.usageError("expected one project NAME", usageAdd)
	}
	return r.addProject(pos[0], path, prefix, false)
}

func (r *runner) cmdInit(args []string) int {
	var prefix string
	pos, code, done := r.parse(usageInit, args, func(fs *flag.FlagSet) {
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
	return r.addProject(name, filepath.Join(r.env.Cwd, initBoardFile), prefix, true)
}

// addProject registers a project and prints the result. pathFlag is the
// --path value ("" for the default under data_dir); with fromInit it is the
// absolute path of ./BOARD.md instead.
//
// The order is: validate, then under the config lock check the registry, create
// or open the board, and only then write the config. A failure before the last
// step leaves the config as it was.
func (r *runner) addProject(name, pathFlag, prefix string, fromInit bool) int {
	name = strings.TrimSpace(name)
	if name == "" {
		return r.fail(fmt.Errorf("%w: project name is empty", domain.ErrInvalid))
	}
	settings := domain.NewBoard(name, prefix).Settings
	if err := settings.Validate(); err != nil {
		return r.fail(err)
	}
	cfgPath, err := config.DefaultPath(r.env.Getenv)
	if err != nil {
		return r.fail(err)
	}

	var ref config.ProjectRef
	var created bool
	err = config.Update(cfgPath, r.env.Getenv, func(cfg *config.Config) error {
		path, err := r.boardPath(cfg, name, pathFlag)
		if err != nil {
			return err
		}
		if q, ok := registeredAt(cfg, path); ok {
			if fromInit {
				return fmt.Errorf("%w: %s here is already registered as project %q", domain.ErrInvalid, initBoardFile, q.Name)
			}
			return fmt.Errorf("%w: %s is already registered as %q", domain.ErrInvalid, path, q.Name)
		}
		if config.StorageFor(path) == "sqlite" {
			return storage.ErrSQLite
		}
		if err := cfg.Add(config.ProjectRef{Name: name, Path: path}); err != nil {
			return err
		}
		ref = cfg.Projects[len(cfg.Projects)-1]
		created, err = openOrCreate(r, ref, settings)
		return err
	})
	if err != nil {
		if created { // the board was made, so it was the save that failed
			err = fmt.Errorf("%w (the new board file %s was left behind)", err, ref.Path)
		}
		return r.fail(err)
	}

	if r.json {
		r.printJSON(projectJSON{ref.Name, ref.Storage, ref.Path})
	} else {
		fmt.Fprintf(r.env.Stdout, "Added project %s (%s) at %s\n", ref.Name, ref.Storage, ref.Path)
	}
	return exitOK
}

// openOrCreate makes sure a board exists at ref. An existing file is loaded
// through its repository to prove it is a Waypoint board, and is not written.
// A missing file is created from settings. created reports which happened.
func openOrCreate(r *runner, ref config.ProjectRef, settings domain.Settings) (created bool, err error) {
	if info, statErr := os.Stat(ref.Path); statErr == nil {
		if info.IsDir() {
			return false, fmt.Errorf("%w: %s is a directory, not a board file", domain.ErrInvalid, ref.Path)
		}
		repo, err := storage.Open(ref)
		if err != nil {
			return false, err
		}
		_, _, err = repo.Load(r.ctx)
		return false, err
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return false, statErr
	}
	_, err = storage.Create(ref, settings)
	return err == nil, err
}

// boardPath is where a new project's board lives: pathFlag resolved against
// the working directory, or <data_dir>/<slug of name>.md when pathFlag is
// empty.
func (r *runner) boardPath(cfg *config.Config, name, pathFlag string) (string, error) {
	if pathFlag == "" {
		s := slug(name)
		if s == "" {
			return "", fmt.Errorf("%w: cannot make a file name from %q; use --path", domain.ErrInvalid, name)
		}
		return filepath.Join(cfg.DataDir, s+".md"), nil
	}
	if filepath.IsAbs(pathFlag) {
		return filepath.Clean(pathFlag), nil
	}
	if r.env.Cwd == "" {
		return "", fmt.Errorf("%w: cannot resolve %q: the working directory is unknown", domain.ErrInvalid, pathFlag)
	}
	return filepath.Join(r.env.Cwd, pathFlag), nil
}

// slug lowercases s, turns each run of characters outside [a-z0-9] into one
// "-" and trims "-" from both ends.
func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, c := range strings.ToLower(s) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(c)
		} else {
			dash = true
		}
	}
	return b.String()
}

// registeredAt finds the project whose board file is path, comparing cleaned
// paths and then the files themselves, so a symlinked path matches.
func registeredAt(cfg *config.Config, path string) (config.ProjectRef, bool) {
	want, statErr := os.Stat(path)
	for _, q := range cfg.Projects {
		if filepath.Clean(q.Path) == filepath.Clean(path) {
			return q, true
		}
		if statErr != nil {
			continue
		}
		if got, err := os.Stat(q.Path); err == nil && os.SameFile(want, got) {
			return q, true
		}
	}
	return config.ProjectRef{}, false
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
