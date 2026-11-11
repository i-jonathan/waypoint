package sqlstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"waypoint/internal/domain"
)

// Save replaces every stored row with b and returns the new revision (one more
// than before). It does not check that the revision is the one the caller
// loaded: the caller does that, inside the same transaction.
//
// The project row is updated in place, never deleted, so a lock held on it
// (SELECT ... FOR UPDATE) stays meaningful to anyone waiting for it.
func (s Store) Save(ctx context.Context, q Querier, b *domain.Board) (int64, error) {
	if err := s.validText(b); err != nil {
		return 0, err
	}
	cur, err := s.Revision(ctx, q)
	if err != nil {
		return 0, err
	}
	rev := cur + 1
	for _, table := range []string{"card_fields", "cards", "board_groups", "board_columns"} {
		if _, err := q.ExecContext(ctx, `DELETE FROM `+table); err != nil {
			return 0, err
		}
	}
	st := b.Settings
	settings, err := json.Marshal(settingsJSON{
		Name: st.Name, Prefix: st.Prefix, NextID: st.NextID, Priorities: st.Priorities,
		Archive: st.Archive, Appearance: st.Appearance,
	})
	if err != nil {
		return 0, err
	}
	res, err := q.ExecContext(ctx, s.D.Rebind(`UPDATE project SET revision = ?, settings = ? WHERE id = 1`), rev, string(settings))
	if err != nil {
		return 0, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return 0, err
	} else if n == 0 {
		if _, err := q.ExecContext(ctx, s.D.Rebind(`INSERT INTO project (id, revision, settings) VALUES (1, ?, ?)`), rev, string(settings)); err != nil {
			return 0, err
		}
	}
	for i, c := range st.Columns {
		moves, _ := json.Marshal(nonNil(c.MovesTo))
		if _, err := q.ExecContext(ctx, s.D.Rebind(`INSERT INTO board_columns (position, name, wip, done, moves_to) VALUES (?, ?, ?, ?, ?)`),
			i, c.Name, c.WIP, boolInt(c.Done), string(moves)); err != nil {
			return 0, err
		}
	}
	for i, g := range st.Groups {
		if _, err := q.ExecContext(ctx, s.D.Rebind(`INSERT INTO board_groups (position, name, color) VALUES (?, ?, ?)`), i, g.Name, g.Color); err != nil {
			return 0, err
		}
	}
	for i, c := range b.Cards {
		if err := s.insertCard(ctx, q, i, c, nil); err != nil {
			return 0, err
		}
	}
	for i, c := range b.Archived {
		at := archivedAt(c)
		if err := s.insertCard(ctx, q, i, c, &at); err != nil {
			return 0, err
		}
	}
	return rev, nil
}

func (s Store) insertCard(ctx context.Context, q Querier, pos int, c domain.Card, archivedAt *string) error {
	depends, _ := json.Marshal(nonNil(c.Depends))
	if _, err := q.ExecContext(ctx, s.D.Rebind(`INSERT INTO cards
		(id, position, column_name, title, group_name, priority, depends, created, done, body, archived_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		string(c.ID), pos, c.Column, c.Title, c.Group, c.Priority, string(depends),
		storedDate(c.Created), storedDate(c.Done), c.Body, archivedAt); err != nil {
		return err
	}
	for i, f := range c.Extra {
		if _, err := q.ExecContext(ctx, s.D.Rebind(`INSERT INTO card_fields (card_id, position, field_key, field_value) VALUES (?, ?, ?, ?)`),
			string(c.ID), i, f.Key, f.Value); err != nil {
			return err
		}
	}
	return nil
}

// archivedAt is the card's done time, else its created time, else the Unix
// epoch. It is for people querying the database; order comes from position.
func archivedAt(c domain.Card) string {
	for _, t := range []time.Time{c.Done, c.Created} {
		if !t.IsZero() {
			return domain.StoredDate(t)
		}
	}
	return domain.StoredDate(time.Unix(0, 0))
}

func storedDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return domain.StoredDate(t)
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// validText checks every string Save will bind against the dialect, so a value
// the engine cannot store is refused with its place named before anything is
// written.
func (s Store) validText(b *domain.Board) error {
	check := func(where, v string) error {
		if err := s.D.ValidText(v); err != nil {
			return fmt.Errorf("%w: %s %v", domain.ErrInvalid, where, err)
		}
		return nil
	}
	st := b.Settings
	for _, e := range []struct{ where, v string }{{"the project name", st.Name}, {"the card prefix", st.Prefix}} {
		if err := check(e.where, e.v); err != nil {
			return err
		}
	}
	for i, p := range st.Priorities {
		if err := check(fmt.Sprintf("priority %d", i+1), p); err != nil {
			return err
		}
	}
	for i, c := range st.Columns {
		if err := check(fmt.Sprintf("column %d name", i+1), c.Name); err != nil {
			return err
		}
		for _, m := range c.MovesTo {
			if err := check(fmt.Sprintf("column %d moves_to", i+1), m); err != nil {
				return err
			}
		}
	}
	for i, g := range st.Groups {
		if err := check(fmt.Sprintf("group %d name", i+1), g.Name); err != nil {
			return err
		}
		if err := check(fmt.Sprintf("group %d color", i+1), g.Color); err != nil {
			return err
		}
	}
	for _, list := range [][]domain.Card{b.Cards, b.Archived} {
		for _, c := range list {
			id := "card " + string(c.ID)
			strs := []struct{ what, v string }{
				{"id", string(c.ID)}, {"column", c.Column}, {"title", c.Title}, {"group", c.Group},
				{"priority", c.Priority}, {"body", c.Body},
			}
			for _, d := range c.Depends {
				strs = append(strs, struct{ what, v string }{"depends", string(d)})
			}
			for _, e := range strs {
				if err := check(id+" "+e.what, e.v); err != nil {
					return err
				}
			}
			for i, f := range c.Extra {
				if err := check(fmt.Sprintf("%s field %d key", id, i+1), f.Key); err != nil {
					return err
				}
				if err := check(fmt.Sprintf("%s field %d value", id, i+1), f.Value); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
