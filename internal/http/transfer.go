package httpapi

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"waypoint/internal/app"
	"waypoint/internal/storage/markdown"
)

// maxImport is the largest file an import reads.
const maxImport = 10 << 20

// downloadName makes a file name from a project name: ASCII letters, digits,
// dot, dash and underscore, so no name can break out of the header it goes in.
func downloadName(project, ext string) string {
	var b strings.Builder
	for _, r := range project {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), ".-")
	if name == "" {
		name = "project"
	}
	return name[:min(len(name), 80)] + ext
}

// exportProject sends the whole project as a file: csv, json or md.
func (a *api) exportProject(w http.ResponseWriter, r *http.Request) error {
	format := r.URL.Query().Get("format")
	var ctype, ext string
	switch format {
	case "csv":
		ctype, ext = "text/csv; charset=utf-8", ".csv"
	case "json":
		ctype, ext = "application/json; charset=utf-8", ".json"
	case "md":
		ctype, ext = "text/markdown; charset=utf-8", ".md"
	default:
		return invalidRequest("format must be csv, json or md")
	}
	p, err := a.openProject(r)
	if err != nil {
		return err
	}
	var buf bytes.Buffer // built first, so a failure is an error and not half a file
	switch format {
	case "csv":
		err = p.svc.ExportCSV(r.Context(), &buf)
	case "json":
		var snap app.Snapshot
		if snap, err = p.svc.Snapshot(r.Context()); err == nil {
			err = app.WriteSnapshot(&buf, snap)
		}
	case "md":
		err = p.svc.ExportMarkdown(r.Context(), &buf, app.MarkdownFormat{Board: markdown.Format, Archive: markdown.FormatArchive})
	}
	if err != nil {
		return err
	}
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, downloadName(p.ref.Name, ext)))
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write(buf.Bytes())
	return nil
}

// importProject reads a csv or json file from the request body into the
// project. With dry_run=1 it only reports what it would do.
func (a *api) importProject(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	flag := func(k string) bool { return q.Get(k) == "1" || q.Get(k) == "true" }
	format := q.Get("format")
	if format != "csv" && format != "json" {
		return invalidRequest("format must be csv or json")
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxImport))
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return &requestError{http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("the file is larger than %d MB", maxImport>>20)}
	}
	if err != nil {
		return malformedRequest("the file could not be read")
	}
	p, err := a.openProject(r)
	if err != nil {
		return err
	}
	dry := flag("dry_run")
	if format == "csv" {
		res, err := p.svc.ImportCSV(r.Context(), bytes.NewReader(body), app.CSVOptions{Create: flag("create"), DryRun: dry})
		if err != nil {
			return err
		}
		if !dry {
			a.changed(p)
		}
		a.writeJSON(w, r, http.StatusOK, struct {
			Format string `json:"format"`
			DryRun bool   `json:"dry_run"`
			app.CSVResult
		}{"csv", dry, res})
		return nil
	}
	snap, err := app.ReadSnapshot(bytes.NewReader(body))
	if err != nil {
		return err
	}
	res, err := p.svc.ImportSnapshot(r.Context(), snap, app.ImportOptions{Replace: flag("replace"), DryRun: dry})
	if err != nil {
		return err
	}
	if !dry {
		a.changed(p)
	}
	a.writeJSON(w, r, http.StatusOK, struct {
		Format string `json:"format"`
		DryRun bool   `json:"dry_run"`
		app.ImportResult
	}{"json", dry, res})
	return nil
}
