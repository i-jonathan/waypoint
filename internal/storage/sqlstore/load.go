package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"waypoint/internal/domain"
)

// settingsJSON is the project row's settings: everything but the columns and
// groups, which have their own tables.
type settingsJSON struct {
	Name       string                 `json:"name"`
	Prefix     string                 `json:"prefix"`
	NextID     int                    `json:"next_id"`
	Priorities []string               `json:"priorities"`
	Archive    domain.ArchiveSettings `json:"archive"`
	Appearance domain.Appearance      `json:"appearance"`
}

// Revision returns the project's save counter, 0 when nothing was saved yet.
func (s Store) Revision(ctx context.Context, q Querier) (int64, error) {
	var rev int64
	err := q.QueryRowContext(ctx, s.D.Rebind(`SELECT revision FROM project WHERE id = 1`)).Scan(&rev)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return rev, err
}

// Load reads the whole board and the revision it was read at. Call it inside
// one read transaction so the parts agree.
func (s Store) Load(ctx context.Context, q Querier) (*domain.Board, int64, error) {
	var rev int64
	var raw string
	err := q.QueryRowContext(ctx, s.D.Rebind(`SELECT revision, settings FROM project WHERE id = 1`)).Scan(&rev, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, fmt.Errorf("%w: the database has no project row", domain.ErrInvalid)
	}
	if err != nil {
		return nil, 0, err
	}
	var js settingsJSON
	if err := json.Unmarshal([]byte(raw), &js); err != nil {
		return nil, 0, fmt.Errorf("%w: the project settings are damaged: %v", domain.ErrInvalid, err)
	}
	b := &domain.Board{Settings: domain.Settings{
		Name: js.Name, Prefix: js.Prefix, NextID: js.NextID, Priorities: js.Priorities,
		Archive: js.Archive, Appearance: js.Appearance,
	}}
	if b.Settings.Columns, err = s.loadColumns(ctx, q); err != nil {
		return nil, 0, err
	}
	if b.Settings.Groups, err = s.loadGroups(ctx, q); err != nil {
		return nil, 0, err
	}
	fields, err := s.loadFields(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	if b.Cards, err = s.loadCards(ctx, q, false, fields); err != nil {
		return nil, 0, err
	}
	if b.Archived, err = s.loadCards(ctx, q, true, fields); err != nil {
		return nil, 0, err
	}
	keepNextIDPastCards(b)
	return b, rev, nil
}

// keepNextIDPastCards makes NextID larger than every card number, so an id is
// never handed out twice even if the counter was left behind.
func keepNextIDPastCards(b *domain.Board) {
	for _, list := range [][]domain.Card{b.Cards, b.Archived} {
		for _, c := range list {
			prefix, num, _ := strings.Cut(string(c.ID), "-")
			if n, err := strconv.Atoi(num); err == nil && prefix == b.Settings.Prefix {
				b.Settings.NextID = max(b.Settings.NextID, n+1)
			}
		}
	}
}

func (s Store) loadColumns(ctx context.Context, q Querier) ([]domain.Column, error) {
	rows, err := q.QueryContext(ctx, s.D.Rebind(`SELECT name, wip, done, moves_to FROM board_columns ORDER BY position`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Column
	for rows.Next() {
		var c domain.Column
		var done int
		var moves string
		if err := rows.Scan(&c.Name, &c.WIP, &done, &moves); err != nil {
			return nil, err
		}
		c.Done = done != 0
		if err := json.Unmarshal([]byte(moves), &c.MovesTo); err != nil {
			return nil, fmt.Errorf("%w: column %q has damaged moves_to: %v", domain.ErrInvalid, c.Name, err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s Store) loadGroups(ctx context.Context, q Querier) ([]domain.Group, error) {
	rows, err := q.QueryContext(ctx, s.D.Rebind(`SELECT name, color FROM board_groups ORDER BY position`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Group
	for rows.Next() {
		var g domain.Group
		if err := rows.Scan(&g.Name, &g.Color); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s Store) loadFields(ctx context.Context, q Querier) (map[domain.CardID][]domain.Field, error) {
	rows, err := q.QueryContext(ctx, s.D.Rebind(`SELECT card_id, field_key, field_value FROM card_fields ORDER BY card_id, position`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[domain.CardID][]domain.Field{}
	for rows.Next() {
		var id domain.CardID
		var f domain.Field
		if err := rows.Scan(&id, &f.Key, &f.Value); err != nil {
			return nil, err
		}
		out[id] = append(out[id], f)
	}
	return out, rows.Err()
}

func (s Store) loadCards(ctx context.Context, q Querier, archived bool, fields map[domain.CardID][]domain.Field) ([]domain.Card, error) {
	where := "IS NULL"
	if archived {
		where = "IS NOT NULL"
	}
	rows, err := q.QueryContext(ctx, s.D.Rebind(`SELECT id, column_name, title, group_name, priority, depends, created, done, body
		FROM cards WHERE archived_at `+where+` ORDER BY position`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Card
	for rows.Next() {
		var c domain.Card
		var depends, created, done string
		if err := rows.Scan(&c.ID, &c.Column, &c.Title, &c.Group, &c.Priority, &depends, &created, &done, &c.Body); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(depends), &c.Depends); err != nil {
			return nil, fmt.Errorf("%w: card %s has damaged depends: %v", domain.ErrInvalid, c.ID, err)
		}
		if c.Created, err = domain.ParseStored(created); err != nil {
			return nil, fmt.Errorf("card %s created: %w", c.ID, err)
		}
		if c.Done, err = domain.ParseStored(done); err != nil {
			return nil, fmt.Errorf("card %s done: %w", c.ID, err)
		}
		c.Extra = fields[c.ID]
		out = append(out, c)
	}
	return out, rows.Err()
}
