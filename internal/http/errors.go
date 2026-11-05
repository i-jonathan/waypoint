package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"

	"waypoint/internal/app"
	"waypoint/internal/config"
	"waypoint/internal/errcode"
)

// errorBody is the JSON shape of every error: the CLI's {"error","code","line"?}
// plus, on a parse error, the text alone with no path or line prefix, for a
// banner to show, and on a conflict the current state to merge against.
type errorBody struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message,omitempty"`
	Current any    `json:"current,omitempty"`
}

// requestError is a problem with the request itself, with the status and code
// to answer it with. Domain errors do not use it: their status comes from the
// code errcode gives them.
type requestError struct {
	status int
	code   string
	msg    string
}

func (e *requestError) Error() string { return e.msg }

func invalidRequest(msg string) *requestError {
	return &requestError{http.StatusUnprocessableEntity, "invalid", msg}
}

func malformedRequest(msg string) *requestError {
	return &requestError{http.StatusBadRequest, "invalid", msg}
}

// statuses maps an error code to its HTTP status. A code not listed is a 500.
var statuses = map[string]int{
	"not_found":        http.StatusNotFound,
	"conflict":         http.StatusConflict,
	"parse":            http.StatusLocked, // the board is read-only until the file is fixed
	"invalid":          http.StatusUnprocessableEntity,
	"unknown_column":   http.StatusUnprocessableEntity,
	"unknown_group":    http.StatusUnprocessableEntity,
	"unknown_priority": http.StatusUnprocessableEntity,
	"move_not_allowed": http.StatusUnprocessableEntity,
	"wip_limit":        http.StatusUnprocessableEntity,
	"busy":             http.StatusServiceUnavailable, // nothing was changed; the caller may try again
}

// writeJSON writes v as the response. It encodes first, so an encoding failure
// is a clean 500 and never half a body.
func (a *api) writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		a.log.Error("encode response", "method", r.Method, "path", r.URL.Path, "error", err)
		status = http.StatusInternalServerError
		buf.Reset()
		buf.WriteString(`{"error":"internal error","code":"error"}` + "\n")
	}
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

// writeError answers err. Errors a caller can act on are shown as they are.
// Anything else is logged and answered with a generic message, so a response
// never carries a stack, a system error or a file path of the server's own.
func (a *api) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var (
		re  *requestError
		ce  *app.ConflictError
		sce *app.SettingsConflictError
		le  *config.LoadError
	)
	switch {
	case errors.As(err, &re):
		a.writeJSON(w, r, re.status, errorBody{Error: re.msg, Code: re.code})
		return
	case errors.As(err, &ce):
		a.writeJSON(w, r, http.StatusConflict, errorBody{Error: ce.Error(), Code: "conflict", Current: ce.Current})
		return
	case errors.As(err, &sce):
		a.writeJSON(w, r, http.StatusConflict, errorBody{Error: sce.Error(), Code: "conflict", Current: sce.Current})
		return
	case errors.As(err, &le): // the Detail, so the response holds no path of the server's own
		a.writeJSON(w, r, http.StatusInternalServerError, errorBody{Error: "the config file has a mistake: " + le.Detail, Code: "config"})
		return
	case errors.Is(err, fs.ErrNotExist):
		a.writeJSON(w, r, http.StatusNotFound, errorBody{Error: "the project's file was not found", Code: "not_found"})
		return
	}
	code := errcode.Code(err)
	if status, ok := statuses[code]; ok {
		a.writeJSON(w, r, status, errorBody{Error: err.Error(), Code: code, Line: errcode.Line(err), Message: errcode.Message(err)})
		return
	}
	a.internalError(w, r, err)
}

// internalError logs err and answers with a generic 500.
func (a *api) internalError(w http.ResponseWriter, r *http.Request, err error) {
	a.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	a.writeJSON(w, r, http.StatusInternalServerError, errorBody{Error: "internal error", Code: "error"})
}
