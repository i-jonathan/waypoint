// Package errcode names errors for machine readers. The CLI's --json output and
// the HTTP API share this mapping, so a script and the browser see one set of
// codes.
package errcode

import (
	"errors"

	"waypoint/internal/app"
	"waypoint/internal/config"
	"waypoint/internal/dbconn"
	"waypoint/internal/domain"
	"waypoint/internal/storage/markdown"
)

// Code names an error: one of not_found, unknown_column, move_not_allowed,
// wip_limit, conflict, invalid, parse, busy (the board file would not hold still
// long enough to save), config (the config file has a mistake), unreachable (a
// database server did not answer), needs_password (a database server wants a
// password the environment does not give), or error for anything else.
func Code(err error) string {
	var (
		pe *markdown.ParseError
		le *config.LoadError
	)
	switch {
	case errors.As(err, &pe), errors.Is(err, markdown.ErrNotABoard):
		return "parse"
	case errors.As(err, &le):
		return "config"
	case errors.Is(err, app.ErrChangedDuringWrite), errors.Is(err, app.ErrBusy):
		return "busy"
	case errors.Is(err, dbconn.ErrPassword):
		return "needs_password"
	case errors.Is(err, dbconn.ErrUnreachable):
		return "unreachable"
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

// Line is the 1-based line of a parse error, or 0 if err has none.
func Line(err error) int {
	var pe *markdown.ParseError
	if errors.As(err, &pe) {
		return pe.Line
	}
	return 0
}

// Message is the text of a parse error with no path or line prefix, or "" if err
// is not one.
func Message(err error) string {
	var pe *markdown.ParseError
	switch {
	case errors.As(err, &pe):
		return pe.Msg
	case errors.Is(err, markdown.ErrNotABoard):
		return markdown.ErrNotABoard.Error()
	}
	return ""
}
