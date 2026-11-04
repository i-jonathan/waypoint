package app

import (
	"bytes"
	"encoding/json"
	"time"

	"waypoint/internal/domain"
)

type fieldJSON struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type blockerJSON struct {
	ID      domain.CardID `json:"id"`
	Missing bool          `json:"missing"`
}

type checklistJSON struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// cardViewJSON is the wire shape of a CardView. Key order is part of the
// interface. Lists are always arrays, never null, and extra fields are a list
// so their order survives.
type cardViewJSON struct {
	ID        domain.CardID   `json:"id"`
	Title     string          `json:"title"`
	Column    string          `json:"column"`
	Group     string          `json:"group"`
	Priority  string          `json:"priority"`
	Depends   []domain.CardID `json:"depends"`
	Created   *string         `json:"created"`
	Done      *string         `json:"done"`
	Body      string          `json:"body"`
	Extra     []fieldJSON     `json:"extra"`
	Archived  bool            `json:"archived"`
	Blockers  []blockerJSON   `json:"blockers"`
	Next      bool            `json:"next"`
	Checklist checklistJSON   `json:"checklist"`
	Hash      string          `json:"hash"`
}

// MarshalJSON encodes the view with lower-case keys, dates as stored (see
// domain.StoredDate; null when unset), and empty lists as [].
func (cv CardView) MarshalJSON() ([]byte, error) {
	out := cardViewJSON{
		ID: cv.ID, Title: cv.Title, Column: cv.Column, Group: cv.Group, Priority: cv.Priority,
		Depends:   append([]domain.CardID{}, cv.Depends...),
		Created:   dateJSON(cv.Created),
		Done:      dateJSON(cv.Done),
		Body:      cv.Body,
		Extra:     make([]fieldJSON, 0, len(cv.Extra)),
		Archived:  cv.Archived,
		Blockers:  make([]blockerJSON, 0, len(cv.Blockers)),
		Next:      cv.Next,
		Checklist: checklistJSON{Done: cv.Checklist.Done, Total: cv.Checklist.Total},
		Hash:      cv.Hash,
	}
	for _, f := range cv.Extra {
		out.Extra = append(out.Extra, fieldJSON{f.Key, f.Value})
	}
	for _, b := range cv.Blockers {
		out.Blockers = append(out.Blockers, blockerJSON{b.ID, b.Missing})
	}
	// No HTML escaping here: an encoder that escapes (the default) re-escapes
	// the output anyway, and one that does not keeps "<" and "&" readable.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func dateJSON(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := domain.StoredDate(t)
	return &s
}
