package markdown_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"waypoint/internal/app"
	"waypoint/internal/domain"
	"waypoint/internal/storage/markdown"
)

// editOnce returns a hook that runs edit the first time path is about to be
// replaced, and a counter of how many times that replacement was attempted.
func editOnce(path string, edit func()) (func(string), *int) {
	n := 0
	return func(p string) {
		if filepath.Base(p) != filepath.Base(path) { // the repo holds the resolved path
			return
		}
		n++
		if n == 1 {
			edit()
		}
	}, &n
}

// atomicEdit changes a file the way most editors do: a new file renamed over it.
func atomicEdit(t *testing.T, path, from, to string) {
	t.Helper()
	data := readFile(t, path)
	if !strings.Contains(data, from) {
		t.Fatalf("%q is not in %s:\n%s", from, path, data)
	}
	tmp := path + ".editor"
	if err := os.WriteFile(tmp, []byte(strings.Replace(data, from, to, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

// inPlaceEdit changes a file by rewriting it, keeping its inode.
func inPlaceEdit(t *testing.T, path, from, to string) {
	t.Helper()
	data := readFile(t, path)
	if !strings.Contains(data, from) {
		t.Fatalf("%q is not in %s:\n%s", from, path, data)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(data, from, to, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newService(t *testing.T) (*app.Service, *markdown.Repo, string) {
	t.Helper()
	r, path := newBoardRepo(t)
	return app.NewService(r, func() time.Time { return day }), r, path
}

func seedCard(t *testing.T, svc *app.Service, title, body string) app.CardView {
	t.Helper()
	cv, err := svc.CreateCard(context.Background(), domain.NewCard{Title: title, Body: body})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	return cv
}

func noTempFiles(t *testing.T, path string) {
	t.Helper()
	for _, name := range entries(t, filepath.Dir(path)) {
		if strings.HasPrefix(name, ".waypoint-") {
			t.Fatalf("temp file %s left behind: %v", name, entries(t, filepath.Dir(path)))
		}
	}
}

func TestExternalEditBeforeRenameSurvivesAlongsideTheOperation(t *testing.T) {
	for name, edit := range map[string]func(*testing.T, string, string, string){
		"rename": atomicEdit, "in place": inPlaceEdit,
	} {
		t.Run(name, func(t *testing.T) {
			svc, r, path := newService(t)
			seedCard(t, svc, "Theirs", "before")

			hook, tries := editOnce(path, func() { edit(t, path, "before", "edited by hand") })
			markdown.SetBeforeRename(r, hook)

			if _, err := svc.CreateCard(context.Background(), domain.NewCard{Title: "Ours"}); err != nil {
				t.Fatalf("CreateCard: %v", err)
			}
			got := readFile(t, path)
			if !strings.Contains(got, "edited by hand") {
				t.Fatalf("the external edit was lost:\n%s", got)
			}
			if !strings.Contains(got, "Ours") {
				t.Fatalf("the operation was lost:\n%s", got)
			}
			if *tries != 2 {
				t.Fatalf("board replaced in %d tries, want 2", *tries)
			}
			noTempFiles(t, path)
		})
	}
}

func TestSaveRefusesAFileChangedSinceLoad(t *testing.T) {
	r, path := newBoardRepo(t)
	b := mustLoad(t, r)
	addCard(t, b, "Ours", "Todo")
	inPlaceEdit(t, path, "name: Contract", "name: By hand")

	if err := r.Save(context.Background(), b); !errors.Is(err, app.ErrChangedDuringWrite) {
		t.Fatalf("Save: %v, want ErrChangedDuringWrite", err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "By hand") || strings.Contains(got, "Ours") {
		t.Fatalf("the file was replaced:\n%s", got)
	}
	noTempFiles(t, path)
}

func TestSaveWithNoLoadSkipsTheCheck(t *testing.T) {
	r, path := newBoardRepo(t)
	b := mustLoad(t, r)
	addCard(t, b, "Ours", "Todo")
	inPlaceEdit(t, path, "name: Contract", "name: By hand")

	mustSave(t, markdown.New(path), b) // a new instance has loaded nothing
	if got := readFile(t, path); !strings.Contains(got, "Ours") {
		t.Fatalf("the save did not happen:\n%s", got)
	}
}

func TestSaveNoticesTheBoardDeleted(t *testing.T) {
	svc, r, path := newService(t)
	seedCard(t, svc, "Theirs", "")
	hook, _ := editOnce(path, func() {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	})
	markdown.SetBeforeRename(r, hook)

	if _, err := svc.CreateCard(context.Background(), domain.NewCard{Title: "Ours"}); err == nil {
		t.Fatal("CreateCard succeeded on a deleted board")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the deleted board came back: %v", err)
	}
}

func TestSaveNoticesAnArchiveThatAppeared(t *testing.T) {
	r, path := newBoardRepo(t)
	b := mustLoad(t, r)
	addCard(t, b, "Ours", "Todo")
	archive := archivePathOf(path)
	if err := os.WriteFile(archive, []byte("### T-99 Filed by hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(context.Background(), b); !errors.Is(err, app.ErrChangedDuringWrite) {
		t.Fatalf("Save: %v, want ErrChangedDuringWrite", err)
	}
	if got := readFile(t, archive); !strings.Contains(got, "Filed by hand") {
		t.Fatalf("the archive was replaced:\n%s", got)
	}
}

// doneCards makes two cards and moves them to a done column, returning their ids.
func doneCards(t *testing.T, svc *app.Service) []domain.CardID {
	t.Helper()
	ctx := context.Background()
	var ids []domain.CardID
	for _, title := range []string{"First", "Second"} {
		cv := seedCard(t, svc, title, "note "+title)
		if _, err := svc.Done(ctx, cv.ID); err != nil {
			t.Fatalf("Done: %v", err)
		}
		ids = append(ids, cv.ID)
	}
	return ids
}

func TestArchiveEditedBeforeRenameKeepsEverything(t *testing.T) {
	svc, r, path := newService(t)
	ids := doneCards(t, svc)
	if _, err := svc.ArchiveDone(context.Background()); err != nil {
		t.Fatal(err)
	}
	third := seedCard(t, svc, "Third", "")
	if _, err := svc.Done(context.Background(), third.ID); err != nil {
		t.Fatal(err)
	}
	archive := archivePathOf(path)
	hook, _ := editOnce(archive, func() { atomicEdit(t, archive, "note First", "note edited in the archive") })
	markdown.SetBeforeRename(r, hook)

	got, err := svc.ArchiveDone(context.Background())
	if err != nil {
		t.Fatalf("ArchiveDone: %v", err)
	}
	if !slices.Equal(got, []domain.CardID{third.ID}) {
		t.Fatalf("archived %v, want just %s", got, third.ID)
	}
	data := readFile(t, archive)
	if !strings.Contains(data, "note edited in the archive") {
		t.Fatalf("the archive edit was lost:\n%s", data)
	}
	for _, id := range append(ids, third.ID) {
		if !strings.Contains(data, string(id)) {
			t.Fatalf("%s is missing from the archive:\n%s", id, data)
		}
	}
}

// A retry runs the operation again from the top, so what it reports must be
// what the last attempt did, not everything every attempt did.
func TestRetriedArchiveDoneReportsEachCardOnce(t *testing.T) {
	svc, r, path := newService(t)
	ids := doneCards(t, svc)
	hook, tries := editOnce(archivePathOf(path), func() { inPlaceEdit(t, path, "name: Contract", "name: By hand") })
	markdown.SetBeforeRename(r, hook)

	got, err := svc.ArchiveDone(context.Background())
	if err != nil {
		t.Fatalf("ArchiveDone: %v", err)
	}
	if !slices.Equal(got, ids) {
		t.Fatalf("archived %v, want %v", got, ids)
	}
	if *tries != 2 {
		t.Fatalf("archive replaced in %d tries, want 2", *tries)
	}
	if data := readFile(t, path); !strings.Contains(data, "By hand") {
		t.Fatalf("the external edit was lost:\n%s", data)
	}
}

// The archive is replaced before the board, so an edit can land between the
// two. The card is then in both files, which Load already resolves.
func TestBoardEditedBetweenTheTwoRenames(t *testing.T) {
	svc, r, path := newService(t)
	ids := doneCards(t, svc)
	other := seedCard(t, svc, "Other", "untouched")
	hook, _ := editOnce(path, func() { atomicEdit(t, path, "untouched", "edited by hand") })
	markdown.SetBeforeRename(r, hook)

	got, err := svc.ArchiveDone(context.Background())
	if err != nil {
		t.Fatalf("ArchiveDone: %v", err)
	}
	if !slices.Equal(got, ids) {
		t.Fatalf("archived %v, want %v", got, ids)
	}
	board := readFile(t, path)
	if !strings.Contains(board, "edited by hand") || !strings.Contains(board, string(other.ID)) {
		t.Fatalf("the external edit was lost:\n%s", board)
	}
	if strings.Contains(board, string(ids[0])+" ") {
		t.Fatalf("%s is still on the board:\n%s", ids[0], board)
	}
}

// The one case a retry cannot settle: the card being archived was itself edited
// between the two renames. Both copies stay on disk and the board reports it.
func TestCardEditedBetweenTheTwoRenamesIsNeverLost(t *testing.T) {
	svc, r, path := newService(t)
	doneCards(t, svc)
	hook, _ := editOnce(path, func() { atomicEdit(t, path, "note First", "note edited by hand") })
	markdown.SetBeforeRename(r, hook)

	_, err := svc.ArchiveDone(context.Background())
	var pe *markdown.ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("ArchiveDone: %v, want a ParseError naming both copies", err)
	}
	if got := readFile(t, path); !strings.Contains(got, "note edited by hand") {
		t.Fatalf("the external edit was lost:\n%s", got)
	}
	if got := readFile(t, archivePathOf(path)); !strings.Contains(got, "note First") {
		t.Fatalf("the archive copy was lost:\n%s", got)
	}
}

// A restore writes the archive twice. The second write must compare against the
// first, not against what Load saw.
func TestRestoreStillWritesTheArchiveTwice(t *testing.T) {
	svc, _, path := newService(t)
	ids := doneCards(t, svc)
	ctx := context.Background()
	if _, err := svc.ArchiveDone(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Restore(ctx, ids[0], "Todo"); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := readFile(t, archivePathOf(path)); strings.Contains(got, string(ids[0])+" ") {
		t.Fatalf("%s is still in the archive:\n%s", ids[0], got)
	}
	if got := readFile(t, path); !strings.Contains(got, string(ids[0])) {
		t.Fatalf("%s is not on the board:\n%s", ids[0], got)
	}
}

// A file with nothing to install is not renamed, so a change to it while the
// other file is written cannot be a lost write, and must not fail the save.
func TestSaveDoesNotRetryOverAFileItDoesNotWrite(t *testing.T) {
	r, _, board, archive := boardWithArchive(t)
	b := mustLoad(t, r)
	b.Archived[0].Title = "Finished, renamed"
	hook, tries := editOnce(board, func() { inPlaceEdit(t, board, "name: Contract", "name: By hand") })
	markdown.SetBeforeRename(r, hook)

	if err := r.Save(context.Background(), b); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if *tries != 1 {
		t.Fatalf("the board was reached %d times, want 1", *tries)
	}
	if !strings.Contains(readFile(t, archive), "Finished, renamed") {
		t.Fatal("the archive was not written")
	}
	if !strings.Contains(readFile(t, board), "name: By hand") {
		t.Fatal("the edit of the board was lost")
	}
}
