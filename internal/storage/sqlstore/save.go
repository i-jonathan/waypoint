package sqlstore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	var columns, groups, cards, fields [][]any
	for i, c := range st.Columns {
		moves, _ := json.Marshal(nonNil(c.MovesTo))
		columns = append(columns, []any{i, c.Name, c.WIP, boolInt(c.Done), string(moves)})
	}
	for i, g := range st.Groups {
		groups = append(groups, []any{i, g.Name, g.Color})
	}
	addCards := func(list []domain.Card, archived bool) {
		for i, c := range list {
			depends, _ := json.Marshal(nonNil(c.Depends))
			var at any // NULL for a live card
			if archived {
				at = archivedAt(c)
			}
			cards = append(cards, []any{string(c.ID), i, c.Column, c.Title, c.Group, c.Priority, string(depends),
				storedDate(c.Created), storedDate(c.Done), c.Body, at})
			for j, f := range c.Extra {
				fields = append(fields, []any{string(c.ID), j, f.Key, f.Value})
			}
		}
	}
	addCards(b.Cards, false)
	addCards(b.Archived, true)
	for _, t := range []struct {
		table string
		cols  []string
		rows  [][]any
	}{
		{"board_columns", []string{"position", "name", "wip", "done", "moves_to"}, columns},
		{"board_groups", []string{"position", "name", "color"}, groups},
		{"cards", []string{"id", "position", "column_name", "title", "group_name", "priority", "depends", "created", "done", "body", "archived_at"}, cards},
		{"card_fields", []string{"card_id", "position", "field_key", "field_value"}, fields},
	} {
		if err := s.insertRows(ctx, q, t.table, t.cols, t.rows); err != nil {
			return 0, err
		}
	}
	return rev, nil
}

// batchRows is how many rows one INSERT carries. The widest table has 11
// columns, so a batch stays well under the 65,535 placeholders an engine allows.
const batchRows = 100

// insertRows inserts rows into table in batches, so a save costs a few round
// trips and not one per card, which matters when the database is across a network.
func (s Store) insertRows(ctx context.Context, q Querier, table string, cols []string, rows [][]any) error {
	for start := 0; start < len(rows); start += batchRows {
		chunk := rows[start:min(start+batchRows, len(rows))]
		one := "(" + strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ") + ")"
		query := "INSERT INTO " + table + " (" + strings.Join(cols, ", ") + ") VALUES " +
			strings.TrimSuffix(strings.Repeat(one+", ", len(chunk)), ", ")
		args := make([]any, 0, len(chunk)*len(cols))
		for _, r := range chunk {
			args = append(args, r...)
		}
		if _, err := q.ExecContext(ctx, s.D.Rebind(query), args...); err != nil {
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
			names := []struct{ what, v string }{
				{"id", string(c.ID)}, {"column", c.Column}, {"group", c.Group}, {"priority", c.Priority},
			}
			for _, e := range names {
				if err := check(id+" "+e.what, e.v); err != nil {
					return err
				}
			}
			texts := []struct{ what, v string }{{"title", c.Title}, {"body", c.Body}}
			for _, d := range c.Depends {
				texts = append(texts, struct{ what, v string }{"depends", string(d)})
			}
			for _, e := range texts {
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
