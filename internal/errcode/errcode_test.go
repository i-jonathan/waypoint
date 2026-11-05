package errcode

import (
	"errors"
	"fmt"
	"testing"

	"waypoint/internal/app"
	"waypoint/internal/domain"
	"waypoint/internal/storage/markdown"
)

func TestCode(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w: x", domain.ErrNotFound), "not_found"},
		{fmt.Errorf("%w: x", domain.ErrUnknownColumn), "unknown_column"},
		{fmt.Errorf("%w: x", domain.ErrMoveNotAllowed), "move_not_allowed"},
		{fmt.Errorf("%w: x", domain.ErrWIPLimit), "wip_limit"},
		{fmt.Errorf("%w: x", domain.ErrConflict), "conflict"},
		{&app.ConflictError{}, "conflict"},
		{fmt.Errorf("%w: x", domain.ErrInvalid), "invalid"},
		{fmt.Errorf("%w: x", domain.ErrUnknownGroup), "invalid"},
		{fmt.Errorf("%w: x", domain.ErrUnknownPriority), "invalid"},
		{fmt.Errorf("file: %w", &markdown.ParseError{Line: 4, Msg: "bad"}), "parse"},
		{fmt.Errorf("file: %w", markdown.ErrNotABoard), "parse"},
		{errors.New("disk on fire"), "error"},
	}
	for _, c := range cases {
		if got := Code(c.err); got != c.want {
			t.Errorf("Code(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

func TestLine(t *testing.T) {
	if got := Line(fmt.Errorf("file: %w", &markdown.ParseError{Line: 4, Msg: "bad"})); got != 4 {
		t.Fatalf("Line = %d, want 4", got)
	}
	if got := Line(errors.New("x")); got != 0 {
		t.Fatalf("Line = %d, want 0", got)
	}
}

func TestChangedDuringWriteIsBusy(t *testing.T) {
	if got := Code(fmt.Errorf("x: %w", app.ErrChangedDuringWrite)); got != "busy" {
		t.Fatalf("got %q", got)
	}
}
