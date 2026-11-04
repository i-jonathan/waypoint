package app_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"waypoint/internal/app"
	"waypoint/internal/domain"
	"waypoint/internal/storage/markdown"
)

var epoch = time.Date(2026, 11, 3, 9, 30, 0, 0, time.UTC)

// env is a board file on a temp dir, a Service over it and a settable clock.
type env struct {
	t    *testing.T
	path string
	repo *markdown.Repo
	svc  *app.Service
	now  time.Time
}

func newEnv(t *testing.T, mutate ...func(*domain.Settings)) *env {
	t.Helper()
	s := domain.NewBoard("Test", "T").Settings
	for _, m := range mutate {
		m(&s)
	}
	path := filepath.Join(t.TempDir(), "board.md")
	repo, err := markdown.Create(path, s)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	e := &env{t: t, path: path, repo: repo, now: epoch}
	e.svc = app.NewService(repo, func() time.Time { return e.now })
	return e
}

func withArchive(mode domain.ArchiveMode, days int) func(*domain.Settings) {
	return func(s *domain.Settings) { s.Archive = domain.ArchiveSettings{Mode: mode, AfterDays: days} }
}

func (e *env) file() string {
	e.t.Helper()
	data, err := os.ReadFile(e.path)
	if err != nil {
		e.t.Fatal(err)
	}
	return string(data)
}

func (e *env) write(s string) {
	e.t.Helper()
	if err := os.WriteFile(e.path, []byte(s), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// board parses the board file as an editor or agent would see it.
func (e *env) board() *domain.Board {
	e.t.Helper()
	b, err := markdown.Parse([]byte(e.file()))
	if err != nil {
		e.t.Fatalf("parse: %v", err)
	}
	return b
}

func (e *env) create(title, column string) app.CardView {
	e.t.Helper()
	v, err := e.svc.CreateCard(context.Background(), domain.NewCard{Title: title, Column: column})
	if err != nil {
		e.t.Fatalf("CreateCard %q: %v", title, err)
	}
	return v
}

func equal(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func noErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func str(s string) *string { return &s }

func TestExternalEditNotLost(t *testing.T) {
	e := newEnv(t)
	a := e.create("A", "")
	e.create("B", "")

	b := e.board()
	_, err := b.AddCard(domain.NewCard{Title: "C"}, epoch)
	noErr(t, err)
	e.write(string(markdown.Format(b)))

	_, err = e.svc.MoveCard(context.Background(), a.ID, "In progress", 0)
	noErr(t, err)

	got := e.board()
	var inProgress, todo []string
	for _, c := range got.CardsIn("In progress") {
		inProgress = append(inProgress, c.Title)
	}
	for _, c := range got.CardsIn("Todo") {
		todo = append(todo, c.Title)
	}
	equal(t, inProgress, []string{"A"})
	equal(t, todo, []string{"B", "C"})
}

func TestUpdateConflict(t *testing.T) {
	e := newEnv(t)
	a := e.create("A", "")
	got, err := e.svc.Get(context.Background(), a.ID)
	noErr(t, err)

	b := e.board()
	noErr(t, b.UpdateCard(a.ID, domain.CardPatch{Body: str("changed outside")}))
	e.write(string(markdown.Format(b)))
	before := e.file()

	_, err = e.svc.UpdateCard(context.Background(), a.ID, domain.CardPatch{Title: str("Mine")}, got.Hash)
	var ce *app.ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("got %v (%T), want *ConflictError", err, err)
	}
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("%v is not ErrConflict", err)
	}
	equal(t, ce.Current.Body, "changed outside")
	equal(t, ce.Current.Title, "A")
	equal(t, e.file(), before)

	// An empty ifMatch skips the check.
	v, err := e.svc.UpdateCard(context.Background(), a.ID, domain.CardPatch{Title: str("Mine")}, "")
	noErr(t, err)
	equal(t, v.Title, "Mine")
	equal(t, v.Body, "changed outside")

	// The fresh hash matches.
	_, err = e.svc.UpdateCard(context.Background(), a.ID, domain.CardPatch{Title: str("Again")}, v.Hash)
	noErr(t, err)
}

func TestUpdateConflictOnArchivedCard(t *testing.T) {
	e := newEnv(t)
	a := e.create("A", "Done")
	_, err := e.svc.ArchiveDone(context.Background())
	noErr(t, err)
	got, err := e.svc.Get(context.Background(), a.ID)
	noErr(t, err)
	equal(t, got.Archived, true)

	_, err = e.svc.UpdateCard(context.Background(), a.ID, domain.CardPatch{Body: str("new")}, "stale")
	var ce *app.ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("got %v, want *ConflictError", err)
	}
	equal(t, ce.Current.Archived, true)
	v, err := e.svc.UpdateCard(context.Background(), a.ID, domain.CardPatch{Body: str("new")}, got.Hash)
	noErr(t, err)
	equal(t, v.Body, "new")
	equal(t, v.Archived, true)
}

func TestParseErrorBlocksWrites(t *testing.T) {
	e := newEnv(t)
	e.write("this is not a board\n")
	before := e.file()

	if _, err := e.svc.CreateCard(context.Background(), domain.NewCard{Title: "X"}); err == nil {
		t.Fatal("CreateCard succeeded on an unparseable file")
	}
	if _, err := e.svc.Board(context.Background()); err == nil {
		t.Fatal("Board succeeded on an unparseable file")
	}
	equal(t, e.file(), before)
}

func TestFailedOpDoesNotSave(t *testing.T) {
	e := newEnv(t, func(s *domain.Settings) { s.Columns[0].MovesTo = []string{"In progress"} })
	a := e.create("A", "")
	// Reformat the file by hand: any Save would rewrite it canonically.
	e.write(e.file() + "\n\n\n")
	before := e.file()

	_, err := e.svc.MoveCard(context.Background(), a.ID, "Done", 0)
	if !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("got %v, want ErrMoveNotAllowed", err)
	}
	equal(t, e.file(), before)
}

func TestAutoArchiveOnWrite(t *testing.T) {
	e := newEnv(t, withArchive(domain.ArchiveAuto, 14))
	old := e.create("Old", "Done")
	e.now = e.now.AddDate(0, 0, 10)
	fresh := e.create("Fresh", "Done")
	if _, err := os.Stat(strings.TrimSuffix(e.path, ".md") + ".archive.md"); err == nil {
		t.Fatal("archive exists before anything is due")
	}

	e.now = epoch.AddDate(0, 0, 20) // Old is 20 days done, Fresh is 10
	other := e.create("Other", "")

	b, err := e.svc.Board(context.Background())
	noErr(t, err)
	var ids []domain.CardID
	for _, c := range b.Cards {
		ids = append(ids, c.ID)
	}
	equal(t, ids, []domain.CardID{other.ID, fresh.ID})
	if _, err := os.Stat(strings.TrimSuffix(e.path, ".md") + ".archive.md"); err != nil {
		t.Fatalf("archive file: %v", err)
	}
	v, err := e.svc.Get(context.Background(), old.ID)
	noErr(t, err)
	equal(t, v.Archived, true)
}

func TestWriteReturnsCardArchivedByThatWrite(t *testing.T) {
	e := newEnv(t, withArchive(domain.ArchiveAuto, 0))
	a := e.create("A", "")
	v, err := e.svc.Done(context.Background(), a.ID)
	noErr(t, err)
	equal(t, v.Archived, true)
	equal(t, v.Next, false)
	equal(t, len(v.Blockers), 0)
	equal(t, v.Column, "") // as the archive file holds it, so the hash matches

	v, err = e.svc.Restore(context.Background(), a.ID, "Todo")
	noErr(t, err)
	equal(t, v.Archived, false)
	equal(t, v.Next, true)
}

func TestArchiveDoneRefusedWhenOff(t *testing.T) {
	e := newEnv(t, withArchive(domain.ArchiveOff, 14))
	e.create("A", "Done")
	before := e.file()
	_, err := e.svc.ArchiveDone(context.Background())
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
	equal(t, e.file(), before)
}

func TestArchiveDoneAndRestore(t *testing.T) {
	e := newEnv(t)
	a := e.create("A", "Done")
	e.create("B", "")
	ids, err := e.svc.ArchiveDone(context.Background())
	noErr(t, err)
	equal(t, ids, []domain.CardID{a.ID})

	ids, err = e.svc.ArchiveDone(context.Background())
	noErr(t, err)
	equal(t, ids, []domain.CardID{})

	v, err := e.svc.Restore(context.Background(), a.ID, "In progress")
	noErr(t, err)
	equal(t, v.Column, "In progress")
	equal(t, v.Archived, false)
}

func TestIdlessCardPersistedOnRead(t *testing.T) {
	e := newEnv(t)
	e.write(strings.Replace(e.file(), "## Todo\n\n", "## Todo\n\n### Call the plumber\n\n", 1))
	if strings.Contains(e.file(), "T-1 Call") {
		t.Fatal("setup: card already has an id")
	}

	b, err := e.svc.Board(context.Background())
	noErr(t, err)
	equal(t, len(b.Cards), 1)
	id := b.Cards[0].ID
	if !strings.Contains(e.file(), "### "+string(id)+" Call the plumber") {
		t.Fatalf("file does not hold the assigned id %s:\n%s", id, e.file())
	}

	b2, err := e.svc.Board(context.Background())
	noErr(t, err)
	equal(t, b2.Cards[0].ID, id)
	v, err := e.svc.Get(context.Background(), id)
	noErr(t, err)
	equal(t, v.Title, "Call the plumber")
}

func TestReadWithoutAssignedIdsDoesNotWrite(t *testing.T) {
	e := newEnv(t)
	e.create("A", "")
	e.write(e.file() + "\n\n\n") // not canonical: a Save would change it
	before := e.file()

	_, err := e.svc.Board(context.Background())
	noErr(t, err)
	_, err = e.svc.List(context.Background(), "", true)
	noErr(t, err)
	_, err = e.svc.Get(context.Background(), "T-1")
	noErr(t, err)
	equal(t, e.file(), before)
}

func TestReadsNeverArchiveOrRewrite(t *testing.T) {
	e := newEnv(t, withArchive(domain.ArchiveAuto, 14))
	e.create("Old", "Done")
	e.now = epoch.AddDate(0, 0, 20) // the card is due for auto-archive
	e.write(e.file() + "\n\n\n")    // not canonical: a Save would change it
	before := e.file()
	archive := strings.TrimSuffix(e.path, ".md") + ".archive.md"

	ctx := context.Background()
	bv, err := e.svc.Board(ctx)
	noErr(t, err)
	equal(t, len(bv.Cards), 1)
	vs, err := e.svc.List(ctx, "", true)
	noErr(t, err)
	equal(t, len(vs), 1)
	v, err := e.svc.Get(ctx, "T-1")
	noErr(t, err)
	equal(t, v.Archived, false)

	equal(t, e.file(), before)
	if _, err := os.Stat(archive); err == nil {
		t.Fatal("a read created the archive file")
	}
}

func TestDoneMovesToFirstDoneColumn(t *testing.T) {
	e := newEnv(t)
	a := e.create("A", "")
	e.create("B", "Done")
	e.now = epoch.AddDate(0, 0, 1)

	v, err := e.svc.Done(context.Background(), a.ID)
	noErr(t, err)
	equal(t, v.Column, "Done")
	equal(t, v.Done, domain.Day(e.now))

	var titles []string
	for _, c := range e.board().CardsIn("Done") {
		titles = append(titles, c.Title)
	}
	equal(t, titles, []string{"B", "A"})
}

func TestDoneRespectsRules(t *testing.T) {
	e := newEnv(t, func(s *domain.Settings) { s.Columns[0].MovesTo = []string{"In progress"} })
	a := e.create("A", "")
	_, err := e.svc.Done(context.Background(), a.ID)
	if !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("got %v, want ErrMoveNotAllowed", err)
	}
}

func TestCardViewFields(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a, err := e.svc.CreateCard(ctx, domain.NewCard{Title: "A", Body: "- [ ] one\n- [x] two\n- [ ] three\n- [ ] four\n"})
	noErr(t, err)
	b, err := e.svc.CreateCard(ctx, domain.NewCard{Title: "B", Depends: []domain.CardID{a.ID, "T-99"}})
	noErr(t, err)

	got, err := e.svc.Get(ctx, a.ID)
	noErr(t, err)
	equal(t, got.Next, true)
	equal(t, got.Checklist.Done, 1)
	equal(t, got.Checklist.Total, 4)
	equal(t, got.Hash, domain.CardHash(got.Card))
	equal(t, len(got.Blockers), 0)
	equal(t, got.Archived, false)

	got, err = e.svc.Get(ctx, b.ID)
	noErr(t, err)
	equal(t, got.Next, false)
	equal(t, got.Blockers, []domain.Blocker{{ID: a.ID}, {ID: "T-99", Missing: true}})
	equal(t, got.Checklist.Total, 0)

	bv, err := e.svc.Board(ctx)
	noErr(t, err)
	equal(t, bv.Settings.Name, "Test")
	equal(t, len(bv.Cards), 2)
	equal(t, bv.Warnings, e.warnings())
	nexts := 0
	for _, c := range bv.Cards {
		if c.Next {
			nexts++
		}
	}
	equal(t, nexts, 1)
}

func (e *env) warnings() []string {
	b, _, err := e.repo.Load(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	return b.Warnings()
}

func TestCheckItem(t *testing.T) {
	e := newEnv(t)
	a, err := e.svc.CreateCard(context.Background(), domain.NewCard{Title: "A", Body: "- [ ] one\n- [ ] two\n"})
	noErr(t, err)
	v, err := e.svc.CheckItem(context.Background(), a.ID, 2)
	noErr(t, err)
	equal(t, v.Checklist.Done, 1)
	equal(t, v.Body, "- [ ] one\n- [x] two") // the file format trims the trailing newline
	_, err = e.svc.CheckItem(context.Background(), a.ID, 3)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestListQueryAndArchive(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a := e.create("Fix roof", "")
	e.create("Paint fence", "")
	d := e.create("Fix gutter", "Done")
	_, err := e.svc.ArchiveDone(ctx)
	noErr(t, err)

	titles := func(vs []app.CardView) []string {
		out := []string{}
		for _, v := range vs {
			out = append(out, v.Title)
		}
		return out
	}
	got, err := e.svc.List(ctx, "fix", false)
	noErr(t, err)
	equal(t, titles(got), []string{"Fix roof"})
	got, err = e.svc.List(ctx, "fix", true)
	noErr(t, err)
	equal(t, titles(got), []string{"Fix roof", "Fix gutter"})
	equal(t, got[1].Archived, true)
	equal(t, got[1].ID, d.ID)
	got, err = e.svc.List(ctx, "next", true)
	noErr(t, err)
	equal(t, got[0].ID, a.ID)
	equal(t, len(got), 1)
	got, err = e.svc.List(ctx, "", false)
	noErr(t, err)
	equal(t, len(got), 2)
}

func TestDeleteCard(t *testing.T) {
	e := newEnv(t)
	a := e.create("A", "")
	noErr(t, e.svc.DeleteCard(context.Background(), a.ID))
	if err := e.svc.DeleteCard(context.Background(), a.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	equal(t, e.create("B", "").ID, domain.CardID("T-2"))
}

func TestConcurrentWritesFromTwoServices(t *testing.T) {
	e := newEnv(t)
	other := app.NewService(markdown.New(e.path), func() time.Time { return epoch })
	const per = 20

	var wg sync.WaitGroup
	errs := make(chan error, 2*per)
	for i, svc := range []*app.Service{e.svc, other} {
		for j := 0; j < per; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := svc.CreateCard(context.Background(), domain.NewCard{Title: fmt.Sprintf("card %d-%d", i, j)})
				errs <- err
			}()
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		noErr(t, err)
	}

	b := e.board()
	equal(t, len(b.Cards), 2*per)
	ids := map[domain.CardID]bool{}
	titles := map[string]bool{}
	for _, c := range b.Cards {
		ids[c.ID] = true
		titles[c.Title] = true
	}
	equal(t, len(ids), 2*per)
	equal(t, len(titles), 2*per)
	equal(t, b.Settings.NextID, 2*per+1)
}

// failingUnlock wraps a Repository so its unlock fails.
type failingUnlock struct{ app.Repository }

func (f failingUnlock) Lock(ctx context.Context) (func() error, error) {
	unlock, err := f.Repository.Lock(ctx)
	if err != nil {
		return nil, err
	}
	return func() error {
		_ = unlock()
		return errors.New("unlock failed")
	}, nil
}

func TestUnlockErrorOnlyWhenNothingElseFailed(t *testing.T) {
	e := newEnv(t)
	svc := app.NewService(failingUnlock{e.repo}, func() time.Time { return epoch })

	_, err := svc.CreateCard(context.Background(), domain.NewCard{Title: "A"})
	if err == nil || err.Error() != "unlock failed" {
		t.Fatalf("got %v, want the unlock error", err)
	}
	_, err = svc.MoveCard(context.Background(), "T-404", "Done", 0)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound, not the unlock error", err)
	}
}

// Two paths to one file must share one lock, or their writes overwrite each
// other: every card added through either path has to survive.
func TestConcurrentWritesThroughTwoPathsLoseNothing(t *testing.T) {
	e := newEnv(t)
	link := filepath.Join(t.TempDir(), "link.md")
	if err := os.Symlink(e.path, link); err != nil {
		t.Fatal(err)
	}
	services := []*app.Service{e.svc, app.NewService(markdown.New(link), func() time.Time { return epoch })}

	const perPath = 25
	var wg sync.WaitGroup
	for i, svc := range services {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range perPath {
				if _, err := svc.CreateCard(context.Background(), domain.NewCard{Title: fmt.Sprintf("path %d card %d", i, n)}); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()

	if got := len(e.board().Cards); got != 2*perPath {
		t.Fatalf("%d cards survived, want %d", got, 2*perPath)
	}
}

// countingRepo counts Saves. afterLoad, if set, runs once after the first Load.
type countingRepo struct {
	app.Repository
	saves     int
	afterLoad func()
}

func (r *countingRepo) Save(ctx context.Context, b *domain.Board) error {
	r.saves++
	return r.Repository.Save(ctx, b)
}

func (r *countingRepo) Load(ctx context.Context) (*domain.Board, []domain.CardID, error) {
	b, assigned, err := r.Repository.Load(ctx)
	if f := r.afterLoad; f != nil {
		r.afterLoad = nil
		f()
	}
	return b, assigned, err
}

func TestUnchangedBoardIsNotSaved(t *testing.T) {
	e := newEnv(t)
	e.create("One", "")
	repo := &countingRepo{Repository: e.repo}
	svc := app.NewService(repo, func() time.Time { return e.now })

	if _, err := svc.ArchiveDone(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MoveCard(context.Background(), "T-1", "Todo", -1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateCard(context.Background(), "T-1", domain.CardPatch{Set: map[string]string{"note": ""}}, ""); err != nil {
		t.Fatal(err)
	}
	if repo.saves != 0 {
		t.Fatalf("%d saves for three no-ops", repo.saves)
	}
}

func TestAssignedIDsAreSavedEvenThoughFnChangedNothing(t *testing.T) {
	e := newEnv(t)
	e.write(strings.Replace(e.file(), "## Todo\n", "## Todo\n\n### Call the plumber\n", 1))
	repo := &countingRepo{Repository: e.repo}
	svc := app.NewService(repo, func() time.Time { return e.now })

	bv, err := svc.Board(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if repo.saves != 1 || bv.Cards[0].ID != "T-1" {
		t.Fatalf("saves = %d, cards = %v", repo.saves, bv.Cards)
	}
	if !strings.Contains(e.file(), "### T-1 Call the plumber") {
		t.Fatalf("the id was not written:\n%s", e.file())
	}
}

// Two readers can both see a card without an id. The second must not save
// again once the first has written the id.
func TestReadDoesNotResaveWhenAnotherReaderAssignedTheIDs(t *testing.T) {
	e := newEnv(t)
	e.write(strings.Replace(e.file(), "## Todo\n", "## Todo\n\n### Call the plumber\n", 1))
	other := app.NewService(e.repo, func() time.Time { return e.now })
	repo := &countingRepo{Repository: e.repo, afterLoad: func() {
		if _, err := other.Board(context.Background()); err != nil {
			t.Fatal(err)
		}
	}}
	svc := app.NewService(repo, func() time.Time { return e.now })

	if _, err := svc.Board(context.Background()); err != nil {
		t.Fatal(err)
	}
	if repo.saves != 0 {
		t.Fatalf("%d saves after the other reader already saved", repo.saves)
	}
}

func TestWriteReturnsTheCardAsTheFileHoldsIt(t *testing.T) {
	e := newEnv(t)
	a := e.create("A", "")
	body := "# Heading\n\ntext\n"

	v, err := e.svc.UpdateCard(context.Background(), a.ID, domain.CardPatch{Body: &body}, "")
	noErr(t, err)

	equal(t, v.Body, "#### Heading\n\ntext")
	got, err := e.svc.Get(context.Background(), a.ID)
	noErr(t, err)
	equal(t, v.Hash, got.Hash)
}
