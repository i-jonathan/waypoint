package app

import (
	"context"
	"fmt"
	"time"

	"waypoint/internal/domain"
)

// Service is the use-case layer. Every interface (CLI, HTTP, MCP) reads and
// writes the board through it.
//
// A write takes the board's lock, loads the current state from the
// repository, applies one operation and saves, so a change made outside
// Waypoint to another card is never lost.
type Service struct {
	repo Repository
	now  Clock
}

// NewService returns a Service over r. A nil now means time.Now.
func NewService(r Repository, now Clock) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repo: r, now: now}
}

// CardView is a card with the facts derived from its board. It marshals to
// JSON through MarshalJSON (see view_json.go), which the CLI and the HTTP API
// share.
type CardView struct {
	domain.Card `json:"-"`
	Archived    bool             `json:"archived"`
	Blockers    []domain.Blocker `json:"blockers"` // empty for an archived card
	Next        bool             `json:"next"`     // true only for the card NextUp returns
	Checklist   struct {
		Done  int `json:"done"`
		Total int `json:"total"`
	} `json:"checklist"`
	Hash string `json:"hash"` // CardHash, to pass back as ifMatch
}

// BoardView is a board's settings, live cards and warnings.
type BoardView struct {
	Settings domain.Settings
	Cards    []CardView
	Warnings []string
}

// ConflictError reports that a card changed since the caller read it. Current
// is the card as it is now.
type ConflictError struct{ Current CardView }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s: card %s changed since it was read", domain.ErrConflict, e.Current.ID)
}

// Unwrap makes errors.Is(err, domain.ErrConflict) true.
func (e *ConflictError) Unwrap() error { return domain.ErrConflict }

// viewer builds CardViews for one board.
type viewer struct {
	b      *domain.Board
	next   domain.CardID
	hasNxt bool
}

func newViewer(b *domain.Board) viewer {
	c, ok := b.NextUp()
	return viewer{b: b, next: c.ID, hasNxt: ok}
}

func (v viewer) view(c domain.Card, archived bool) CardView {
	cv := CardView{Card: c, Archived: archived, Blockers: []domain.Blocker{}, Hash: domain.CardHash(c)}
	if !archived {
		if bl := v.b.Blockers(c.ID); len(bl) > 0 {
			cv.Blockers = bl
		}
		cv.Next = v.hasNxt && v.next == c.ID
	}
	items := domain.Checklist(c.Body)
	cv.Checklist.Total = len(items)
	for _, it := range items {
		if it.Checked {
			cv.Checklist.Done++
		}
	}
	return cv
}

// find returns the view of a card on the board or in its archive.
func (v viewer) find(id domain.CardID) (CardView, error) {
	c, archived, ok := v.b.Find(id)
	if !ok {
		return CardView{}, fmt.Errorf("%w: card %s", domain.ErrNotFound, id)
	}
	return v.view(c, archived), nil
}

// apply runs one operation under the board's lock: load fresh state, run fn,
// auto-archive when due, save. Nothing is saved if Load or fn fails, or if
// nothing changed and Load assigned no ids, so a no-op leaves a hand-kept file
// exactly as it is. It returns the board as the file now holds it: a save can
// normalise what fn wrote, and every view and hash a caller gets must match
// the file, so after saving it loads again while still holding the lock. An
// unlock failure is reported only when nothing else failed.
func (s *Service) apply(ctx context.Context, fn func(*domain.Board) error) (b *domain.Board, err error) {
	unlock, err := s.repo.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if uerr := unlock(); uerr != nil && err == nil {
			b, err = nil, uerr
		}
	}()

	b, assigned, err := s.repo.Load(ctx)
	if err != nil {
		return nil, err
	}
	before := b.Clone()
	if err = fn(b); err != nil {
		return nil, err
	}
	today := domain.Day(s.now())
	if due := b.DueForArchive(today); len(due) > 0 { // empty unless mode is auto
		if err = b.Archive(due, today); err != nil {
			return nil, err
		}
	}
	if len(assigned) == 0 && b.Equal(before) {
		return b, nil
	}
	if err = s.repo.Save(ctx, b); err != nil {
		return nil, err
	}
	b, _, err = s.repo.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("the change was saved, but reading the board back failed: %w", err)
	}
	return b, nil
}

// read loads the board for a read path. It takes no lock unless Load reports
// that cards were given ids; then it saves them under the lock, so the ids
// are stable, and returns the board that was saved.
func (s *Service) read(ctx context.Context) (*domain.Board, error) {
	b, assigned, err := s.repo.Load(ctx)
	if err != nil {
		return nil, err
	}
	if len(assigned) == 0 {
		return b, nil
	}
	return s.apply(ctx, func(*domain.Board) error { return nil })
}

// Board returns the live cards and settings.
func (s *Service) Board(ctx context.Context) (BoardView, error) {
	b, err := s.read(ctx)
	if err != nil {
		return BoardView{}, err
	}
	v := newViewer(b)
	out := BoardView{Settings: b.Settings, Cards: make([]CardView, 0, len(b.Cards)), Warnings: b.Warnings()}
	for _, c := range b.Cards {
		out.Cards = append(out.Cards, v.view(c, false))
	}
	return out, nil
}

// List returns the cards matching query (see domain.ParseFilter), board cards
// first and, if includeArchived, archived cards after them.
func (s *Service) List(ctx context.Context, query string, includeArchived bool) ([]CardView, error) {
	b, err := s.read(ctx)
	if err != nil {
		return nil, err
	}
	f := domain.ParseFilter(query)
	v := newViewer(b)
	out := []CardView{}
	for _, c := range b.Cards {
		if f.Match(b, c) {
			out = append(out, v.view(c, false))
		}
	}
	if includeArchived {
		for _, c := range b.Archived {
			if f.Match(b, c) {
				out = append(out, v.view(c, true))
			}
		}
	}
	return out, nil
}

// Get returns one card, live or archived.
func (s *Service) Get(ctx context.Context, id domain.CardID) (CardView, error) {
	b, err := s.read(ctx)
	if err != nil {
		return CardView{}, err
	}
	return newViewer(b).find(id)
}

// change applies fn and returns the view of card id from the saved board. The
// card may be archived if auto-archive just moved it.
func (s *Service) change(ctx context.Context, id domain.CardID, fn func(*domain.Board) error) (CardView, error) {
	b, err := s.apply(ctx, fn)
	if err != nil {
		return CardView{}, err
	}
	return newViewer(b).find(id)
}

// CreateCard adds a card at the bottom of its column.
func (s *Service) CreateCard(ctx context.Context, n domain.NewCard) (CardView, error) {
	var id domain.CardID
	b, err := s.apply(ctx, func(b *domain.Board) error {
		c, err := b.AddCard(n, domain.Day(s.now()))
		id = c.ID
		return err
	})
	if err != nil {
		return CardView{}, err
	}
	return newViewer(b).find(id)
}

// MoveCard moves a card to a column and position.
func (s *Service) MoveCard(ctx context.Context, id domain.CardID, to string, index int) (CardView, error) {
	return s.change(ctx, id, func(b *domain.Board) error {
		return b.MoveCard(id, to, index, domain.Day(s.now()))
	})
}

// Done moves a card to the bottom of the first done column, under the usual
// move rules.
func (s *Service) Done(ctx context.Context, id domain.CardID) (CardView, error) {
	return s.change(ctx, id, func(b *domain.Board) error {
		for _, col := range b.Settings.Columns {
			if col.Done {
				return b.MoveCard(id, col.Name, -1, domain.Day(s.now()))
			}
		}
		return fmt.Errorf("%w: board has no done column", domain.ErrInvalid)
	})
}

// UpdateCard applies a patch. If ifMatch is not empty and is not the hash of
// the card as it is now, it returns a *ConflictError and changes nothing.
func (s *Service) UpdateCard(ctx context.Context, id domain.CardID, p domain.CardPatch, ifMatch string) (CardView, error) {
	return s.change(ctx, id, func(b *domain.Board) error {
		c, archived, ok := b.Find(id)
		if !ok {
			return fmt.Errorf("%w: card %s", domain.ErrNotFound, id)
		}
		if ifMatch != "" && domain.CardHash(c) != ifMatch {
			return &ConflictError{Current: newViewer(b).view(c, archived)}
		}
		return b.UpdateCard(id, p)
	})
}

// CheckItem toggles the nth (1-based) checklist item of a card.
func (s *Service) CheckItem(ctx context.Context, id domain.CardID, n int) (CardView, error) {
	return s.change(ctx, id, func(b *domain.Board) error { return b.ToggleCheck(id, n) })
}

// DeleteCard removes a card from the board or the archive.
func (s *Service) DeleteCard(ctx context.Context, id domain.CardID) error {
	_, err := s.apply(ctx, func(b *domain.Board) error { return b.DeleteCard(id) })
	return err
}

// ArchiveDone archives every card in a done column. It is refused when the
// archive mode is off.
func (s *Service) ArchiveDone(ctx context.Context) ([]domain.CardID, error) {
	ids := []domain.CardID{}
	_, err := s.apply(ctx, func(b *domain.Board) error {
		if b.Settings.Archive.Mode == domain.ArchiveOff {
			return fmt.Errorf("%w: archiving is off for this board", domain.ErrInvalid)
		}
		ids = append(ids, b.DoneCards()...)
		return b.Archive(ids, domain.Day(s.now()))
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// Restore moves an archived card back to the bottom of a column; "" means the
// first column.
func (s *Service) Restore(ctx context.Context, id domain.CardID, to string) (CardView, error) {
	return s.change(ctx, id, func(b *domain.Board) error {
		return b.Restore(id, to, domain.Day(s.now()))
	})
}
