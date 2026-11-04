package domain

import "errors"

// Sentinel errors. Callers wrap them with fmt.Errorf("%w: ...") and match with errors.Is.
var (
	ErrNotFound        = errors.New("not found")
	ErrUnknownColumn   = errors.New("unknown column")
	ErrUnknownGroup    = errors.New("unknown group")
	ErrUnknownPriority = errors.New("unknown priority")
	ErrMoveNotAllowed  = errors.New("move not allowed")
	ErrWIPLimit        = errors.New("wip limit reached")
	ErrInvalid         = errors.New("invalid")
	ErrConflict        = errors.New("conflict")
)
