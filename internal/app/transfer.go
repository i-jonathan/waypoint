package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"waypoint/internal/domain"
)

// SnapshotFormat is the version of the JSON snapshot this build writes and reads.
const SnapshotFormat = 1

// Snapshot is a whole project (settings, cards and archive) as JSON: a lossless
// backup, and a form an agent or script can read. Dates are written as the
// board stores them (see domain.StoredDate).
type Snapshot struct {
	Format   int             `json:"format"`
	Settings domain.Settings `json:"settings"`
	Cards    []SnapshotCard  `json:"cards"`
	Archived []SnapshotCard  `json:"archived"`
}

// SnapshotCard is one card of a Snapshot. An archived card has no column.
type SnapshotCard struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	Column   string          `json:"column,omitempty"`
	Group    string          `json:"group,omitempty"`
	Priority string          `json:"priority,omitempty"`
	Depends  []string        `json:"depends,omitempty"`
	Created  string          `json:"created,omitempty"`
	Done     string          `json:"done,omitempty"`
	Body     string          `json:"body,omitempty"`
	Extra    []SnapshotField `json:"extra,omitempty"`
}

// SnapshotField is an extra card field.
type SnapshotField struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ImportOptions says how an import treats what is already in the project.
type ImportOptions struct {
	// Replace swaps the whole project for the imported one. Without it the
	// project must be empty.
	Replace bool
}

// ImportResult counts what an import put in the project.
type ImportResult struct {
	Cards    int `json:"cards"`
	Archived int `json:"archived"`
}

// WriteSnapshot writes s as indented JSON followed by a newline.
func WriteSnapshot(w io.Writer, s Snapshot) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// ReadSnapshot reads one snapshot. It refuses unknown keys, a missing or newer
// format, and anything after the document, so a file that is not what it looks
// like is stopped here. Errors wrap domain.ErrInvalid.
func ReadSnapshot(r io.Reader) (Snapshot, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var s Snapshot
	if err := dec.Decode(&s); err != nil {
		return Snapshot{}, fmt.Errorf("%w: this is not a Waypoint snapshot: %v", domain.ErrInvalid, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Snapshot{}, fmt.Errorf("%w: this is not a Waypoint snapshot: there is more after the first JSON document", domain.ErrInvalid)
	}
	switch {
	case s.Format == 0:
		return Snapshot{}, fmt.Errorf("%w: this is not a Waypoint snapshot: it has no format number", domain.ErrInvalid)
	case s.Format > SnapshotFormat:
		return Snapshot{}, fmt.Errorf("%w: this snapshot is format %d and this Waypoint reads up to format %d; update Waypoint", domain.ErrInvalid, s.Format, SnapshotFormat)
	case s.Format < SnapshotFormat:
		return Snapshot{}, fmt.Errorf("%w: unknown snapshot format %d", domain.ErrInvalid, s.Format)
	}
	return s, nil
}

// Snapshot returns the whole project.
func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	b, err := s.read(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Format: SnapshotFormat, Settings: b.Settings, Cards: []SnapshotCard{}, Archived: []SnapshotCard{}}
	for _, c := range b.Cards {
		snap.Cards = append(snap.Cards, snapshotCard(c, true))
	}
	for _, c := range b.Archived {
		snap.Archived = append(snap.Archived, snapshotCard(c, false))
	}
	return snap, nil
}

func snapshotCard(c domain.Card, live bool) SnapshotCard {
	out := SnapshotCard{
		ID: string(c.ID), Title: c.Title, Group: c.Group, Priority: c.Priority, Body: c.Body,
	}
	if live {
		out.Column = c.Column
	}
	for _, d := range c.Depends {
		out.Depends = append(out.Depends, string(d))
	}
	if !c.Created.IsZero() {
		out.Created = domain.StoredDate(c.Created)
	}
	if !c.Done.IsZero() {
		out.Done = domain.StoredDate(c.Done)
	}
	for _, f := range c.Extra {
		out.Extra = append(out.Extra, SnapshotField{f.Key, f.Value})
	}
	return out
}

// ImportSnapshot puts a snapshot's project in place of this one's. The project
// must be empty unless o.Replace. The snapshot is checked first (domain.Board.Validate),
// and nothing is changed if it is refused. The counter for new ids is raised past
// every imported card.
func (s *Service) ImportSnapshot(ctx context.Context, snap Snapshot, o ImportOptions) (ImportResult, error) {
	imported, err := snap.board()
	if err != nil {
		return ImportResult{}, err
	}
	_, err = s.apply(ctx, func(b *domain.Board) error {
		if !o.Replace && (len(b.Cards) > 0 || len(b.Archived) > 0) {
			return fmt.Errorf("%w: this project already has cards; import into an empty project, or use replace to swap everything", domain.ErrInvalid)
		}
		b.Settings, b.Cards, b.Archived, b.Notices = imported.Settings, imported.Cards, imported.Archived, nil
		return nil
	})
	if err != nil {
		return ImportResult{}, err
	}
	return ImportResult{Cards: len(imported.Cards), Archived: len(imported.Archived)}, nil
}

// board converts the snapshot to a validated board.
func (snap Snapshot) board() (*domain.Board, error) {
	b := &domain.Board{Settings: snap.Settings}
	for _, list := range []struct {
		from []SnapshotCard
		live bool
	}{{snap.Cards, true}, {snap.Archived, false}} {
		for _, sc := range list.from {
			c, err := sc.card(list.live)
			if err != nil {
				return nil, err
			}
			if list.live {
				b.Cards = append(b.Cards, c)
			} else {
				b.Archived = append(b.Archived, c)
			}
		}
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}
	// Never hand out an id a card already has.
	for _, list := range [][]domain.Card{b.Cards, b.Archived} {
		for _, c := range list {
			prefix, num, _ := strings.Cut(string(c.ID), "-")
			if n, err := strconv.Atoi(num); err == nil && prefix == b.Settings.Prefix {
				b.Settings.NextID = max(b.Settings.NextID, n+1)
			}
		}
	}
	return b, nil
}

func (sc SnapshotCard) card(live bool) (domain.Card, error) {
	c := domain.Card{
		ID: domain.CardID(sc.ID), Title: sc.Title, Group: sc.Group, Priority: sc.Priority, Body: sc.Body,
	}
	if live {
		c.Column = sc.Column
	}
	for _, d := range sc.Depends {
		c.Depends = append(c.Depends, domain.CardID(d))
	}
	var err error
	if c.Created, err = domain.ParseStored(sc.Created); err != nil {
		return c, fmt.Errorf("%w: card %s: created: %v", domain.ErrInvalid, sc.ID, err)
	}
	if c.Done, err = domain.ParseStored(sc.Done); err != nil {
		return c, fmt.Errorf("%w: card %s: done: %v", domain.ErrInvalid, sc.ID, err)
	}
	for _, f := range sc.Extra {
		c.Extra = append(c.Extra, domain.Field{Key: f.Key, Value: f.Value})
	}
	return c, nil
}
