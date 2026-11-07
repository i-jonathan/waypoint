package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"waypoint/internal/app"
	"waypoint/internal/domain"
	"waypoint/internal/storage/markdown"
)

const (
	usageExport = "waypoint export [--format json|csv|md] [--out FILE] [--project NAME]"
	usageImport = "waypoint import FILE [--format json|csv] [--replace | --create] [--project NAME] [--json]"
)

// formatOf picks the transfer format: the --format value, else the extension of
// file, else def. It returns "json", "csv" or "md", or "" for something
// Waypoint does not know.
func formatOf(flagValue, file, def string) string {
	f := strings.ToLower(strings.TrimPrefix(flagValue, "."))
	if f == "" && file != "" {
		f = strings.ToLower(strings.TrimPrefix(filepath.Ext(file), "."))
	}
	if f == "" {
		f = def
	}
	switch f {
	case "json", "csv", "md":
		return f
	case "markdown":
		return "md"
	}
	return ""
}

// render writes the project in the given format.
func render(r *runner, svc *app.Service, kind string, w io.Writer) error {
	switch kind {
	case "csv":
		return svc.ExportCSV(r.ctx, w)
	case "md":
		return svc.ExportMarkdown(r.ctx, w, app.MarkdownFormat{Board: markdown.Format, Archive: markdown.FormatArchive})
	}
	snap, err := svc.Snapshot(r.ctx)
	if err != nil {
		return err
	}
	return app.WriteSnapshot(w, snap)
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
		return r.usageError(fmt.Sprintf("unknown format %q: export json, csv or md", format+filepath.Ext(out)), usageExport)
	}
	svc, ref, err := r.service(r.project)
	if err != nil {
		return r.fail(err)
	}
	if out == "" {
		// Rendered first, so a failure prints nothing to stdout.
		var buf bytes.Buffer
		if err := render(r, svc, kind, &buf); err != nil {
			return r.fail(err)
		}
		if _, err := r.env.Stdout.Write(buf.Bytes()); err != nil {
			return r.fail(err)
		}
		return exitOK
	}
	var buf bytes.Buffer
	if err := render(r, svc, kind, &buf); err != nil {
		return r.fail(err)
	}
	snap, err := svc.Snapshot(r.ctx) // for the counts
	if err != nil {
		return r.fail(err)
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
	if _, err := f.Write(buf.Bytes()); err != nil {
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
	var replace, create bool
	pos, code, done := r.parse(usageImport, args, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "format", "", "")
		fs.BoolVar(&replace, "replace", false, "")
		fs.BoolVar(&create, "create", false, "")
	})
	if done {
		return code
	}
	if len(pos) != 1 {
		return r.usageError("expected one FILE to import", usageImport)
	}
	file := r.abs(pos[0])
	kind := formatOf(format, file, "")
	if kind != "json" && kind != "csv" {
		return r.usageError(fmt.Sprintf("cannot tell the format of %s: use a .json or .csv file, or --format json or csv", pos[0]), usageImport)
	}
	if kind == "csv" && replace {
		return r.usageError("--replace is for json imports; a csv import adds to the project and updates cards by id", usageImport)
	}
	if kind == "json" && create {
		return r.usageError("--create is for csv imports", usageImport)
	}
	f, err := os.Open(file)
	if err != nil {
		return r.fail(err)
	}
	defer f.Close()
	if kind == "csv" {
		svc, ref, err := r.service(r.project)
		if err != nil {
			return r.fail(err)
		}
		res, err := svc.ImportCSV(r.ctx, f, app.CSVOptions{Create: create})
		if err != nil {
			return r.fail(err)
		}
		if r.json {
			r.printJSON(res)
			return exitOK
		}
		fmt.Fprintf(r.env.Stdout, "Imported into %s: %d created, %d updated\n", ref.Name, len(res.Created), len(res.Updated))
		if len(res.NewColumns) > 0 {
			fmt.Fprintf(r.env.Stdout, "Added columns: %s\n", strings.Join(res.NewColumns, ", "))
		}
		if len(res.NewGroups) > 0 {
			fmt.Fprintf(r.env.Stdout, "Added groups: %s\n", strings.Join(res.NewGroups, ", "))
		}
		return exitOK
	}
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
