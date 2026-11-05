// Package errcode names errors for machine readers. The CLI's --json output and
// the HTTP API share this mapping, so a script and the browser see one set of
// codes.
package errcode

import (
	"errors"

	"waypoint/internal/app"
	"waypoint/internal/domain"
	"waypoint/internal/storage/markdown"
)

// Code names an error: one of not_found, unknown_column, move_not_allowed,
// wip_limit, conflict, invalid, parse, busy (the board file would not hold still
// long enough to save), or error for anything else.
func Code(err error) string {
	var pe *markdown.ParseError
	switch {
	case errors.As(err, &pe), errors.Is(err, markdown.ErrNotABoard):
		return "parse"
	case errors.Is(err, app.ErrChangedDuringWrite):
		return "busy"
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
