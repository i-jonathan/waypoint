package httpapi

import (
	"bytes"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

// dist holds the built web UI. Only placeholder.html is committed, so the Go
// build works before the UI is built.
//
//go:embed all:dist
var dist embed.FS

func init() {
	// Served with nosniff, so a font or icon needs its type from the extension
	// even on a machine whose MIME database lacks it.
	for ext, typ := range map[string]string{
		".woff2": "font/woff2",
		".woff":  "font/woff",
		".ico":   "image/x-icon",
		".map":   "application/json",
	} {
		_ = mime.AddExtensionType(ext, typ)
	}
}

// DistFS returns the embedded built UI.
func DistFS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil { // dist is embedded at build time, so this cannot happen
		panic(err)
	}
	return sub
}

const (
	indexFile       = "index.html"
	placeholderFile = "placeholder.html"
)

// UI serves a built single-page app from dist. A path is a file only if dist has
// that file. Anything else is a client-side route and gets index.html, because a
// route can hold a dot: /p/my.project is a project page. When index.html is absent
// (the UI was not built), the route gets placeholder.html instead. Paths under
// /api/ never reach here: the API answers them itself.
//
// Files under /assets/ are content-hashed, so they are cached for a year; the
// page that names them is never cached.
func UI(dist fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if isFile(dist, name) {
			serveFile(w, r, dist, name)
			return
		}
		if _, err := fs.Stat(dist, indexFile); err == nil {
			serveFile(w, r, dist, indexFile)
			return
		}
		serveFile(w, r, dist, placeholderFile)
	})
}

func isFile(dist fs.FS, name string) bool {
	if name == "" || !fs.ValidPath(name) {
		return false
	}
	info, err := fs.Stat(dist, name)
	return err == nil && info.Mode().IsRegular()
}

func serveFile(w http.ResponseWriter, r *http.Request, dist fs.FS, name string) {
	if !fs.ValidPath(name) {
		http.NotFound(w, r)
		return
	}
	if info, err := fs.Stat(dist, name); err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(dist, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch {
	case name == indexFile || name == placeholderFile:
		w.Header().Set("Cache-Control", "no-cache")
	case strings.HasPrefix(name, "assets/"):
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}
