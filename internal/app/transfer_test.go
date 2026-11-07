package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"waypoint/internal/app"
	"waypoint/internal/domain"
	"waypoint/internal/storage/sqlite"
)

// richService fills a markdown project with every concept a snapshot carries.
func richService(t *testing.T) *env {
	t.Helper()
	e := newEnv(t, func(s *domain.Settings) {
		s.Groups = []domain.Group{{Name: "Platform", Color: "blue"}, {Name: "Docs & help", Color: "amber"}}
		s.Archive = domain.ArchiveSettings{Mode: domain.ArchiveManual, AfterDays: 30}
		s.Appearance = domain.Appearance{Accent: "#7c3aed"}
		s.Columns[1].WIP = 3
	})
	ctx := context.Background()
	first, err := e.svc.CreateCard(ctx, domain.NewCard{Title: "First", Group: "Platform", Priority: "P0"})
	noErr(t, err)
	_, err = e.svc.CreateCard(ctx, domain.NewCard{
		Title: "Second", Column: "In progress", Depends: []domain.CardID{first.ID},
		Extra: []domain.Field{{Key: "estimate", Value: "small"}},
		Body:  "Plan:\n\n- [ ] one\n\n```sh\n### T-9 not a card\n```\n\n#### Notes\n\nShort.",
	})
	noErr(t, err)
	old, err := e.svc.CreateCard(ctx, domain.NewCard{Title: "Old news", Body: "Shipped."})
	noErr(t, err)
	_, err = e.svc.Done(ctx, old.ID)
	noErr(t, err)
	_, err = e.svc.ArchiveDone(ctx)
	noErr(t, err)
	return e
}

// wholeBoard loads the board and its archive through the repository.
func wholeBoard(t *testing.T, e *env) *domain.Board {
	t.Helper()
	b, _, err := e.repo.Load(context.Background())
	noErr(t, err)
	return b
}

func newLiteService(t *testing.T) (*app.Service, *sqlite.Repo) {
	t.Helper()
	repo, err := sqlite.Create(filepath.Join(t.TempDir(), "x.db"), domain.NewBoard("Blank", "Z").Settings)
	noErr(t, err)
	return app.NewService(repo, func() time.Time { return epoch }), repo
}

func TestSnapshotRoundTripsAcrossStorages(t *testing.T) {
	src := richService(t)
	snap, err := src.svc.Snapshot(context.Background())
	noErr(t, err)

	var buf bytes.Buffer
	noErr(t, app.WriteSnapshot(&buf, snap))
	again, err := app.ReadSnapshot(&buf)
	noErr(t, err)

	dst, repo := newLiteService(t)
	res, err := dst.ImportSnapshot(context.Background(), again, app.ImportOptions{})
	noErr(t, err)
	equal(t, res.Cards, 2)
	equal(t, res.Archived, 1)

	want := wholeBoard(t, src)
	got, _, err := repo.Load(context.Background())
	noErr(t, err)
	if !got.Equal(want) {
		t.Fatalf("imported board differs:\n got %#v\nwant %#v", got, want)
	}
}

func TestSnapshotKeepsDateOnlyAndTimestampDates(t *testing.T) {
	e := newEnv(t)
	e.write(strings.Replace(e.file(), "next_id: 1", "next_id: 3", 1) + "\n## Todo\n\n### T-1 Hand written\n\n- created: 2026-10-26\n\n### T-2 Made by Waypoint\n\n- created: 2026-11-04T23:52:10Z\n")
	snap, err := e.svc.Snapshot(context.Background())
	noErr(t, err)
	var buf bytes.Buffer
	noErr(t, app.WriteSnapshot(&buf, snap))
	text := buf.String()
	if !strings.Contains(text, `"created": "2026-10-26"`) || !strings.Contains(text, `"created": "2026-11-04T23:52:10Z"`) {
		t.Fatalf("dates were not written as they are stored:\n%s", text)
	}
}

func TestImportIntoNonEmptyProjectIsRefusedUnlessReplacing(t *testing.T) {
	src := richService(t)
	snap, err := src.svc.Snapshot(context.Background())
	noErr(t, err)

	dst, repo := newLiteService(t)
	_, err = dst.CreateCard(context.Background(), domain.NewCard{Title: "Already here"})
	noErr(t, err)
	if _, err := dst.ImportSnapshot(context.Background(), snap, app.ImportOptions{}); !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "replace") {
		t.Fatalf("import into a non-empty project = %v, want ErrInvalid that mentions replace", err)
	}
	if b, _, _ := repo.Load(context.Background()); len(b.Cards) != 1 || b.Cards[0].Title != "Already here" {
		t.Fatal("a refused import changed the project")
	}

	_, err = dst.ImportSnapshot(context.Background(), snap, app.ImportOptions{Replace: true})
	noErr(t, err)
	b, _, _ := repo.Load(context.Background())
	if !b.Equal(wholeBoard(t, src)) {
		t.Fatal("a replacing import did not give the snapshot's board")
	}
}

func TestReadSnapshotRefusals(t *testing.T) {
	good := `{"format":1,"settings":{"name":"N","prefix":"T","next_id":1,"columns":[{"name":"Todo","wip":0,"moves_to":[],"done":false}],"groups":[],"priorities":["P0"],"archive":{"mode":"manual","after_days":14},"appearance":{}},"cards":[],"archived":[]}`
	if _, err := app.ReadSnapshot(strings.NewReader(good)); err != nil {
		t.Fatalf("a good snapshot: %v", err)
	}
	for name, text := range map[string]string{
		"newer format":     strings.Replace(good, `"format":1`, `"format":2`, 1),
		"no format":        strings.Replace(good, `"format":1,`, ``, 1),
		"unknown key":      strings.Replace(good, `"cards":[]`, `"cards":[],"surprise":true`, 1),
		"not json":         `# BOARD`,
		"two documents":    good + good,
		"trailing garbage": good + `x`,
	} {
		if _, err := app.ReadSnapshot(strings.NewReader(text)); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: %v, want domain.ErrInvalid", name, err)
		}
	}
}

func TestImportValidatesTheSnapshot(t *testing.T) {
	base := func() app.Snapshot {
		s := app.Snapshot{Format: 1, Settings: domain.NewBoard("N", "T").Settings}
		s.Cards = []app.SnapshotCard{{ID: "T-1", Title: "One", Column: "Todo"}}
		s.Settings.NextID = 2
		return s
	}
	cases := map[string]struct {
		mutate func(*app.Snapshot)
		want   string // must appear in the error
	}{
		"bad settings":               {func(s *app.Snapshot) { s.Settings.Prefix = "9" }, "prefix"},
		"column not on board":        {func(s *app.Snapshot) { s.Cards[0].Column = "Nowhere" }, "Nowhere"},
		"empty title":                {func(s *app.Snapshot) { s.Cards[0].Title = "" }, "T-1"},
		"two-line title":             {func(s *app.Snapshot) { s.Cards[0].Title = "a\nb" }, "T-1"},
		"bad id":                     {func(s *app.Snapshot) { s.Cards[0].ID = "one" }, "one"},
		"lowercase prefix":           {func(s *app.Snapshot) { s.Cards[0].ID = "t-1" }, "t-1"},
		"duplicate id":               {func(s *app.Snapshot) { s.Cards = append(s.Cards, s.Cards[0]) }, "T-1"},
		"live and archived id":       {func(s *app.Snapshot) { s.Archived = []app.SnapshotCard{{ID: "T-1", Title: "Again"}} }, "T-1"},
		"unknown group":              {func(s *app.Snapshot) { s.Cards[0].Group = "Ghosts" }, "Ghosts"},
		"unknown priority":           {func(s *app.Snapshot) { s.Cards[0].Priority = "P9" }, "P9"},
		"bad depends id":             {func(s *app.Snapshot) { s.Cards[0].Depends = []string{"nope"} }, "nope"},
		"bad date":                   {func(s *app.Snapshot) { s.Cards[0].Created = "yesterday" }, "yesterday"},
		"extra field with blank key": {func(s *app.Snapshot) { s.Cards[0].Extra = []app.SnapshotField{{Key: " ", Value: "x"}} }, "T-1"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dst, repo := newLiteService(t)
			s := base()
			c.mutate(&s)
			_, err := dst.ImportSnapshot(context.Background(), s, app.ImportOptions{})
			if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("ImportSnapshot = %v, want ErrInvalid containing %q", err, c.want)
			}
			if b, _, _ := repo.Load(context.Background()); b.Settings.Name != "Blank" || len(b.Cards) != 0 {
				t.Fatal("a refused import changed the project")
			}
		})
	}
}

func TestImportRaisesNextIDPastEveryCard(t *testing.T) {
	dst, repo := newLiteService(t)
	s := app.Snapshot{Format: 1, Settings: domain.NewBoard("N", "T").Settings}
	s.Settings.NextID = 1
	s.Cards = []app.SnapshotCard{{ID: "T-7", Title: "Seven", Column: "Todo"}}
	s.Archived = []app.SnapshotCard{{ID: "T-12", Title: "Twelve"}}
	_, err := dst.ImportSnapshot(context.Background(), s, app.ImportOptions{})
	noErr(t, err)
	b, _, _ := repo.Load(context.Background())
	if b.Settings.NextID != 13 {
		t.Fatalf("next id = %d, want 13", b.Settings.NextID)
	}
}

func TestSnapshotJSONShape(t *testing.T) {
	src := richService(t)
	snap, err := src.svc.Snapshot(context.Background())
	noErr(t, err)
	var buf bytes.Buffer
	noErr(t, app.WriteSnapshot(&buf, snap))
	var top map[string]json.RawMessage
	noErr(t, json.Unmarshal(buf.Bytes(), &top))
	for _, key := range []string{"format", "settings", "cards", "archived"} {
		if _, ok := top[key]; !ok {
			t.Errorf("snapshot has no %q key: %s", key, buf.String())
		}
	}
	if len(top) != 4 {
		t.Errorf("snapshot has keys %v, want exactly format, settings, cards, archived", len(top))
	}
}
