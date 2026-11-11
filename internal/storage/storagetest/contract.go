// Package storagetest holds the behaviour every app.Repository must have. It
// knows nothing about how a repository stores its board.
package storagetest

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"waypoint/internal/app"
	"waypoint/internal/domain"
)

var (
	oct = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	nov = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
)

// RunContract checks a repository implementation. newRepo returns a repository
// holding a fresh NewBoard("Contract", "T"); each call must be independent.
func RunContract(t *testing.T, newRepo func(t *testing.T) app.Repository, opts ...Option) {
	var o options
	for _, f := range opts {
		f(&o)
	}
	t.Run("SaveLoadRoundTrip", func(t *testing.T) { saveLoadRoundTrip(t, newRepo(t)) })
	t.Run("ArchiveSurvivesRoundTrip", func(t *testing.T) { archiveSurvivesRoundTrip(t, newRepo(t)) })
	t.Run("IDsNeverReused", func(t *testing.T) { idsNeverReused(t, newRepo(t)) })
	t.Run("NextIDStaysPastArchivedIDs", func(t *testing.T) { nextIDStaysPastArchivedIDs(t, newRepo(t)) })
	t.Run("NoticesAreNotPersisted", func(t *testing.T) { noticesAreNotPersisted(t, newRepo(t)) })
	t.Run("LockIsExclusive", func(t *testing.T) {
		if o.skipLock {
			t.Skip("this repository has no lock yet")
		}
		lockIsExclusive(t, newRepo(t))
	})
}

type options struct{ skipLock bool }

// Option adjusts RunContract.
type Option func(*options)

// SkipLock skips the lock test, for a repository whose lock is not built yet.
func SkipLock() Option { return func(o *options) { o.skipLock = true } }

func load(t *testing.T, r app.Repository) *domain.Board {
	t.Helper()
	b, _, err := r.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return b
}

func save(t *testing.T, r app.Repository, b *domain.Board) {
	t.Helper()
	if err := r.Save(context.Background(), b); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func ids(cards []domain.Card) []domain.CardID {
	out := make([]domain.CardID, len(cards))
	for i, c := range cards {
		out[i] = c.ID
	}
	return out
}

// withoutColumns copies cards with their columns cleared, because an archived
// card has none.
func withoutColumns(cards []domain.Card) []domain.Card {
	out := slices.Clone(cards)
	for i := range out {
		out[i].Column = ""
	}
	return out
}

// sameCard reports the first difference between two cards.
func sameCard(want, got domain.Card) string {
	switch {
	case want.ID != got.ID:
		return "id"
	case want.Column != got.Column:
		return "column"
	case want.Title != got.Title:
		return "title"
	case want.Group != got.Group:
		return "group"
	case want.Priority != got.Priority:
		return "priority"
	case !slices.Equal(want.Depends, got.Depends):
		return "depends"
	case !want.Created.Equal(got.Created):
		return "created"
	case !want.Done.Equal(got.Done):
		return "done"
	case want.Body != got.Body:
		return "body"
	case !slices.Equal(want.Extra, got.Extra):
		return "extra fields"
	}
	return ""
}

func requireSameCards(t *testing.T, what string, want, got []domain.Card) {
	t.Helper()
	if !slices.Equal(ids(want), ids(got)) {
		t.Fatalf("%s: ids %v, want %v", what, ids(got), ids(want))
	}
	for i := range want {
		if d := sameCard(want[i], got[i]); d != "" {
			t.Fatalf("%s: card %s differs in %s:\n got %#v\nwant %#v", what, want[i].ID, d, got[i], want[i])
		}
	}
}

func requireSameSettings(t *testing.T, want, got domain.Settings) {
	t.Helper()
	// Compare through formatting-neutral fields; slices may be nil or empty.
	if want.Name != got.Name || want.Prefix != got.Prefix || want.NextID != got.NextID ||
		want.Archive != got.Archive || want.Appearance != got.Appearance ||
		!slices.Equal(want.Groups, got.Groups) || !slices.Equal(want.Priorities, got.Priorities) {
		t.Fatalf("settings differ:\n got %#v\nwant %#v", got, want)
	}
	if len(want.Columns) != len(got.Columns) {
		t.Fatalf("columns %#v, want %#v", got.Columns, want.Columns)
	}
	for i, w := range want.Columns {
		g := got.Columns[i]
		if w.Name != g.Name || w.WIP != g.WIP || w.Done != g.Done || !slices.Equal(w.MovesTo, g.MovesTo) {
			t.Fatalf("column %d: got %#v, want %#v", i, g, w)
		}
	}
}

func saveLoadRoundTrip(t *testing.T, r app.Repository) {
	b := load(t, r)
	if b.Settings.Name != "Contract" || b.Settings.Prefix != "T" || b.Settings.NextID != 1 || len(b.Cards) != 0 {
		t.Fatalf("fresh board = %#v", b)
	}
	b.Settings.Groups = []domain.Group{{Name: "Platform", Color: "blue"}, {Name: "Docs & help", Color: "amber"}}
	b.Settings.Archive = domain.ArchiveSettings{Mode: domain.ArchiveAuto, AfterDays: 30}
	b.Settings.Appearance = domain.Appearance{Accent: "#7c3aed"}
	b.Settings.Columns[1].WIP = 3

	first := must(b.AddCard(domain.NewCard{Title: "First", Group: "Platform", Priority: "P0"}, oct))
	must(b.AddCard(domain.NewCard{
		Title:    "Second",
		Column:   "In progress",
		Priority: "P1",
		Depends:  []domain.CardID{first.ID},
		Extra:    []domain.Field{{Key: "estimate", Value: "small"}, {Key: "reviewer", Value: "dana"}},
		Body:     "Plan:\n\n- [ ] one\n- [x] two\n\n```sh\n### not a card\nmake test\n```\n\n#### Notes\n\nKeep it short.",
	}, oct))
	must(b.AddCard(domain.NewCard{Title: "Third", Group: "Docs & help"}, oct))
	old := must(b.AddCard(domain.NewCard{Title: "Old news", Body: "Shipped."}, oct))
	recent := must(b.AddCard(domain.NewCard{Title: "Recent news"}, oct))
	if err := b.MoveCard(old.ID, "Done", -1, oct); err != nil {
		t.Fatal(err)
	}
	if err := b.Archive([]domain.CardID{old.ID}, oct); err != nil {
		t.Fatal(err)
	}
	if err := b.MoveCard(recent.ID, "Done", -1, nov); err != nil {
		t.Fatal(err)
	}
	if err := b.Archive([]domain.CardID{recent.ID}, nov); err != nil {
		t.Fatal(err)
	}
	if err := b.MoveCard(first.ID, "In progress", 0, nov); err != nil {
		t.Fatal(err)
	}

	save(t, r, b)
	got := load(t, r)

	requireSameSettings(t, b.Settings, got.Settings)
	requireSameCards(t, "cards", b.Cards, got.Cards)
	requireSameCards(t, "archived", withoutColumns(b.Archived), withoutColumns(got.Archived))
	if want := []domain.CardID{"T-3", "T-1", "T-2"}; !slices.Equal(ids(got.Cards), want) {
		t.Fatalf("rank order = %v, want %v", ids(got.Cards), want)
	}
}

func archiveSurvivesRoundTrip(t *testing.T, r app.Repository) {
	b := load(t, r)
	a := must(b.AddCard(domain.NewCard{Title: "Archived", Body: "Kept as written.\n\n```\ncode\n```", Extra: []domain.Field{{Key: "estimate", Value: "large"}}}, oct))
	must(b.AddCard(domain.NewCard{Title: "Stays"}, oct))
	if err := b.MoveCard(a.ID, "Done", -1, oct); err != nil {
		t.Fatal(err)
	}
	if err := b.Archive([]domain.CardID{a.ID}, nov); err != nil {
		t.Fatal(err)
	}
	want := withoutColumns(b.Archived)
	save(t, r, b)

	b = load(t, r)
	requireSameCards(t, "archived after first load", want, withoutColumns(b.Archived))

	// Saving the reloaded board again keeps the archive.
	save(t, r, b)
	b = load(t, r)
	requireSameCards(t, "archived after resave", want, withoutColumns(b.Archived))
	if got := ids(b.Cards); !slices.Equal(got, []domain.CardID{"T-2"}) {
		t.Fatalf("cards = %v, want [T-2]", got)
	}

	// Restoring empties the archive, and that sticks too.
	if err := b.Restore(a.ID, "Todo", nov); err != nil {
		t.Fatal(err)
	}
	save(t, r, b)
	b = load(t, r)
	if len(b.Archived) != 0 {
		t.Fatalf("archive after restore = %#v, want empty", b.Archived)
	}
	if got := ids(b.Cards); !slices.Equal(got, []domain.CardID{"T-2", a.ID}) {
		t.Fatalf("cards after restore = %v, want [T-2 %s]", got, a.ID)
	}
}

func idsNeverReused(t *testing.T, r app.Repository) {
	b := load(t, r)
	first := must(b.AddCard(domain.NewCard{Title: "Gone soon"}, oct))
	kept := must(b.AddCard(domain.NewCard{Title: "Kept"}, oct))
	if err := b.DeleteCard(first.ID); err != nil {
		t.Fatal(err)
	}
	save(t, r, b)

	b = load(t, r)
	if c := must(b.AddCard(domain.NewCard{Title: "After deletion"}, nov)); c.ID != "T-3" {
		t.Fatalf("new card is %s, want T-3 (%s was deleted, %s kept)", c.ID, first.ID, kept.ID)
	}
	if err := b.DeleteCard("T-3"); err != nil {
		t.Fatal(err)
	}
	save(t, r, b)

	// The newest card is gone, and its id still is not handed out again.
	b = load(t, r)
	if c := must(b.AddCard(domain.NewCard{Title: "Later"}, nov)); c.ID != "T-4" {
		t.Fatalf("new card is %s, want T-4", c.ID)
	}
}

func nextIDStaysPastArchivedIDs(t *testing.T, r app.Repository) {
	b := load(t, r)
	must(b.AddCard(domain.NewCard{Title: "Live"}, oct))
	old := must(b.AddCard(domain.NewCard{Title: "Archived", Column: "Done"}, oct))
	if err := b.Archive([]domain.CardID{old.ID}, oct); err != nil {
		t.Fatal(err)
	}
	// A counter that fell behind, as when a board file is reverted or edited
	// by hand. The archived id must still not be handed out again.
	b.Settings.NextID = 1
	save(t, r, b)

	b = load(t, r)
	if b.Settings.NextID < 3 {
		t.Fatalf("next id = %d, want at least 3 (T-2 is archived)", b.Settings.NextID)
	}
	if c := must(b.AddCard(domain.NewCard{Title: "New"}, nov)); c.ID != "T-3" {
		t.Fatalf("new card is %s, want T-3", c.ID)
	}
	save(t, r, b)
	b = load(t, r)
	if _, archived, ok := b.Find("T-2"); !ok || !archived || len(b.Archived) != 1 {
		t.Fatalf("archived T-2 was lost: cards %v, archived %v", ids(b.Cards), ids(b.Archived))
	}
}

func noticesAreNotPersisted(t *testing.T, r app.Repository) {
	b := load(t, r)
	must(b.AddCard(domain.NewCard{Title: "Card"}, oct))
	b.Notices = []string{"found something while reading"}
	save(t, r, b)
	if got := load(t, r); len(got.Notices) != 0 || len(got.Cards) != 1 {
		t.Fatalf("after reload: notices %q, cards %d; want no notices and 1 card", got.Notices, len(got.Cards))
	}
}

func lockIsExclusive(t *testing.T, r app.Repository) {
	unlock, err := r.Lock(context.Background())
	if err != nil {
		t.Fatalf("first Lock: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	second, err := r.Lock(ctx)
	if err == nil {
		second()
		t.Fatal("second Lock succeeded while the first was held")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second Lock error = %v, want the context deadline", err)
	}
	if time.Since(start) < 50*time.Millisecond {
		t.Fatalf("second Lock gave up after %v, before its deadline", time.Since(start))
	}

	if err := unlock(); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	again, err := r.Lock(context.Background())
	if err != nil {
		t.Fatalf("Lock after unlock: %v", err)
	}
	if err := again(); err != nil {
		t.Fatalf("unlock again: %v", err)
	}
}
