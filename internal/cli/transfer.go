package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"waypoint/internal/app"
	"waypoint/internal/domain"
)

const (
	usageExport = "waypoint export [--format json] [--out FILE] [--project NAME]"
	usageImport = "waypoint import FILE [--format json] [--replace] [--project NAME] [--json]"
)

// formatOf picks the transfer format: the --format value, else the extension of
// file, else def. It returns "" for something Waypoint does not know.
func formatOf(flagValue, file, def string) string {
	f := strings.ToLower(strings.TrimPrefix(flagValue, "."))
	if f == "" && file != "" {
		f = strings.ToLower(strings.TrimPrefix(filepath.Ext(file), "."))
	}
	if f == "" {
		f = def
	}
	switch f {
	case "json":
		return f
	}
	return ""
}

func (r *runner) abs(path string) string {
	if filepath.IsAbs(path) || r.env.Cwd == "" {
		return path
	}
	return filepath.Join(r.env.Cwd, path)
}

func (r *runner) cmdExport(args []string) int {
	var format, out string
	pos, code, done := r.parse(usageExport, args, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "format", "", "")
		fs.StringVar(&out, "out", "", "")
	})
	if done {
		return code
	}
	if len(pos) != 0 {
		return r.usageError(fmt.Sprintf("unexpected argument %q", pos[0]), usageExport)
	}
	kind := formatOf(format, out, "json")
	if kind == "" {
		return r.usageError(fmt.Sprintf("unknown format %q: this version exports json", format+filepath.Ext(out)), usageExport)
	}
	svc, ref, err := r.service(r.project)
	if err != nil {
		return r.fail(err)
	}
	snap, err := svc.Snapshot(r.ctx)
	if err != nil {
		return r.fail(err)
	}
	if out == "" {
		if err := app.WriteSnapshot(r.env.Stdout, snap); err != nil {
			return r.fail(err)
		}
		return exitOK
	}
	path := r.abs(out)
	// O_EXCL: an export never replaces a file that is already there.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return r.fail(fmt.Errorf("%w: %s already exists; choose a file name that is not taken", domain.ErrInvalid, path))
	}
	if err != nil {
		return r.fail(err)
	}
	if err := app.WriteSnapshot(f, snap); err != nil {
		f.Close()
		os.Remove(path)
		return r.fail(err)
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return r.fail(err)
	}
	if r.json {
		r.printJSON(struct {
			File     string `json:"file"`
			Cards    int    `json:"cards"`
			Archived int    `json:"archived"`
		}{path, len(snap.Cards), len(snap.Archived)})
	} else {
		fmt.Fprintf(r.env.Stdout, "Exported %s to %s (%s)\n", ref.Name, path, counts(len(snap.Cards), len(snap.Archived), "and"))
	}
	return exitOK
}

func (r *runner) cmdImport(args []string) int {
	var format string
	var replace bool
	pos, code, done := r.parse(usageImport, args, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "format", "", "")
		fs.BoolVar(&replace, "replace", false, "")
	})
	if done {
		return code
	}
	if len(pos) != 1 {
		return r.usageError("expected one FILE to import", usageImport)
	}
	file := r.abs(pos[0])
	if formatOf(format, file, "") == "" {
		return r.usageError(fmt.Sprintf("cannot tell the format of %s: use a .json file, or --format json", pos[0]), usageImport)
	}
	f, err := os.Open(file)
	if err != nil {
		return r.fail(err)
	}
	defer f.Close()
	snap, err := app.ReadSnapshot(f)
	if err != nil {
		return r.fail(err)
	}
	svc, ref, err := r.service(r.project)
	if err != nil {
		return r.fail(err)
	}
	res, err := svc.ImportSnapshot(r.ctx, snap, app.ImportOptions{Replace: replace})
	if err != nil {
		return r.fail(err)
	}
	if r.json {
		r.printJSON(res)
	} else {
		fmt.Fprintf(r.env.Stdout, "Imported %s into %s\n", counts(res.Cards, res.Archived, "and"), ref.Name)
	}
	return exitOK
}

// counts reads like "2 cards and 1 archived card".
func counts(cards, archived int, join string) string {
	plural := func(n int, one, many string) string {
		if n == 1 {
			return fmt.Sprintf("1 %s", one)
		}
		return fmt.Sprintf("%d %s", n, many)
	}
	return plural(cards, "card", "cards") + " " + join + " " + plural(archived, "archived card", "archived cards")
}
