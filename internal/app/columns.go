package app

import (
	"context"
	"fmt"
	"strings"

	"waypoint/internal/domain"
)

// ResolveColumn maps what a user or agent typed to a column of the board: an
// exact name first, then a case-insensitive match that must be unique. An
// empty name stays empty (the caller's default). Anything else is
// domain.ErrUnknownColumn.
func (s *Service) ResolveColumn(ctx context.Context, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	bv, err := s.Board(ctx)
	if err != nil {
		return "", err
	}
	var folded []string
	for _, c := range bv.Settings.Columns {
		if c.Name == name {
			return c.Name, nil
		}
		if strings.EqualFold(c.Name, name) {
			folded = append(folded, c.Name)
		}
	}
	switch len(folded) {
	case 1:
		return folded[0], nil
	case 0:
		return "", fmt.Errorf("%w: %q", domain.ErrUnknownColumn, name)
	}
	return "", fmt.Errorf("%w: %q matches %s", domain.ErrUnknownColumn, name, strings.Join(folded, ", "))
}
