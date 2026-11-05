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
	"waypoint/internal/storage/storagetest"
)

var day = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

func newBoardRepo(t *testing.T) (*markdown.Repo, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "board.md")
	r, err := markdown.Create(path, domain.NewBoard("Contract", "T").Settings)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return r, path
}

func TestMarkdownRepoContract(t *testing.T) {
	storagetest.RunContract(t, func(t *testing.T) app.Repository {
		r, _ := newBoardRepo(t)
		return r
	})
}

func archivePathOf(path string) string { return strings.TrimSuffix(path, ".md") + ".archive.md" }

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range list {
		names = append(names, e.Name())
	}
	return names
}

func mustLoad(t *testing.T, r *markdown.Repo) *domain.Board {
	t.Helper()
	b, _, err := r.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return b
}

func mustSave(t *testing.T, r *markdown.Repo, b *domain.Board) {
	t.Helper()
	if err := r.Save(context.Background(), b); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func addCard(t *testing.T, b *domain.Board, title, column string) domain.Card {
	t.Helper()
	c, err := b.AddCard(domain.NewCard{Title: title, Column: column}, day)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// boardWithArchive returns a saved board with one live card and one archived
// card, and the paths of its two files.
func boardWithArchive(t *testing.T) (r *markdown.Repo, b *domain.Board, board, archive string) {
	t.Helper()
	r, board = newBoardRepo(t)
	b = mustLoad(t, r)
	addCard(t, b, "Live", "")
	done := addCard(t, b, "Finished", "Done")
	if err := b.Archive([]domain.CardID{done.ID}, day); err != nil {
		t.Fatal(err)
	}
	mustSave(t, r, b)
	return r, b, board, archivePathOf(board)
}

func TestSaveIsAtomic(t *testing.T) {
	r, path := newBoardRepo(t)
	b := mustLoad(t, r)
	addCard(t, b, "One", "")
	done := addCard(t, b, "Two", "Done")
	if err := b.Archive([]domain.CardID{done.ID}, day); err != nil {
		t.Fatal(err)
	}
	mustSave(t, r, b)
	mustSave(t, r, b)

	want := []string{"board.archive.md", "board.md"}
	if got := entries(t, filepath.Dir(path)); !slices.Equal(got, want) {
		t.Fatalf("directory holds %v, want %v", got, want)
	}
}

func TestSaveLeavesNoTempFileWhenItFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "board.md")
	// A non-empty directory in the board's place makes the final rename fail.
	if err := os.MkdirAll(filepath.Join(path, "inside"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := markdown.New(path).Save(context.Background(), domain.NewBoard("X", "T"))
	if err == nil {
		t.Fatal("Save succeeded over a directory")
	}
	if got := entries(t, dir); !slices.Equal(got, []string{"board.md"}) {
		t.Fatalf("directory holds %v, want only board.md", got)
	}
}

func TestCreateRefusesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.md")
	if err := os.WriteFile(path, []byte("my notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := markdown.Create(path, domain.NewBoard("X", "T").Settings)
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("Create error = %v, want os.ErrExist", err)
	}
	if got := readFile(t, path); got != "my notes\n" {
		t.Fatalf("existing file was changed to %q", got)
	}
}

func TestCreateWritesCanonicalEmptyBoard(t *testing.T) {
	s := domain.NewBoard("Home", "H").Settings
	path := filepath.Join(t.TempDir(), "home.md")
	if _, err := markdown.Create(path, s); err != nil {
		t.Fatal(err)
	}
	if got, want := readFile(t, path), string(markdown.Format(&domain.Board{Settings: s})); got != want {
		t.Fatalf("file = %q, want %q", got, want)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v (%v), want 0644", fi.Mode().Perm(), err)
	}
	if _, err := markdown.Create(filepath.Join(t.TempDir(), "bad.md"), domain.Settings{}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Create with empty settings: %v, want ErrInvalid", err)
	}
}

func TestNoArchiveFileWhenEmpty(t *testing.T) {
	r, path := newBoardRepo(t)
	b := mustLoad(t, r)
	addCard(t, b, "One", "")
	mustSave(t, r, b)
	if _, err := os.Stat(archivePathOf(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("archive file exists for a board with no archived cards (stat error %v)", err)
	}
}

func TestEmptiedArchiveFileIsKept(t *testing.T) {
	r, b, _, archive := boardWithArchive(t)
	if err := b.Restore("T-2", "Todo", day); err != nil {
		t.Fatal(err)
	}
	mustSave(t, r, b)
	if got := readFile(t, archive); got != "# Archive\n" {
		t.Fatalf("archive file = %q, want just its title", got)
	}
}

func TestLoadReturnsParseErrorAndDoesNotWrite(t *testing.T) {
	r, path := newBoardRepo(t)
	b := mustLoad(t, r)
	addCard(t, b, "One", "")
	mustSave(t, r, b)

	corrupt := readFile(t, path) + "\nstray text outside any card\n"
	corrupt = strings.Replace(corrupt, "## Done\n", "## Nowhere\n", 1)
	if err := os.WriteFile(path, []byte(corrupt), 0o644); err != nil {
		t.Fatal(err)
	}
	before := entries(t, filepath.Dir(path))

	_, _, err := r.Load(context.Background())
	var pe *markdown.ParseError
	if !errors.As(err, &pe) || pe.Line < 1 {
		t.Fatalf("Load error = %v, want a *ParseError with a line", err)
	}
	if got := readFile(t, path); got != corrupt {
		t.Fatalf("Load changed the file:\n%s", got)
	}
	if got := entries(t, filepath.Dir(path)); !slices.Equal(got, before) {
		t.Fatalf("directory changed from %v to %v", before, got)
	}
}

func TestLoadReturnsArchiveParseErrorAndDoesNotWrite(t *testing.T) {
	r, _, board, archive := boardWithArchive(t)
	bad := "# Archive\n\n## not a month\n"
	if err := os.WriteFile(archive, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	boardBefore := readFile(t, board)
	_, _, err := r.Load(context.Background())
	var pe *markdown.ParseError
	if !errors.As(err, &pe) || !strings.Contains(err.Error(), "archive") {
		t.Fatalf("Load error = %v, want an archive *ParseError", err)
	}
	if readFile(t, archive) != bad || readFile(t, board) != boardBefore {
		t.Fatal("Load changed a file")
	}
}

func TestLoadMissingBoard(t *testing.T) {
	_, _, err := markdown.New(filepath.Join(t.TempDir(), "none.md")).Load(context.Background())
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load error = %v, want os.ErrNotExist", err)
	}
}

func TestLoadReportsAssignedIDs(t *testing.T) {
	r, path := newBoardRepo(t)
	data := strings.Replace(readFile(t, path), "## Todo\n\n", "## Todo\n\n### Hand written\n\nNo id yet.\n\n", 1)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	b, assigned, err := r.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(assigned, []domain.CardID{"T-1"}) || len(b.Cards) != 1 || b.Cards[0].ID != "T-1" {
		t.Fatalf("assigned %v, cards %#v", assigned, b.Cards)
	}
	if readFile(t, path) != data {
		t.Fatal("Load wrote the assigned id back")
	}
}

func TestSaveRefusesUnreadableOutput(t *testing.T) {
	r, b, board, archive := boardWithArchive(t)
	boardBefore, archiveBefore := readFile(t, board), readFile(t, archive)

	// Format drops a card whose column is not in the settings.
	b.Cards[0].Column = "Nowhere"
	err := r.Save(context.Background(), b)
	if err == nil || !strings.HasPrefix(err.Error(), "refusing to write") {
		t.Fatalf("Save error = %v, want one starting %q", err, "refusing to write")
	}
	if readFile(t, board) != boardBefore || readFile(t, archive) != archiveBefore {
		t.Fatal("Save changed a file it refused to write")
	}
	if got := entries(t, filepath.Dir(board)); !slices.Equal(got, []string{"board.archive.md", "board.md"}) {
		t.Fatalf("directory holds %v", got)
	}
}

func TestSaveRefusesUnreadableArchiveAndWritesNothing(t *testing.T) {
	r, b, board, archive := boardWithArchive(t)
	boardBefore, archiveBefore := readFile(t, board), readFile(t, archive)

	// The board half is fine; the archive half has a card with no done date,
	// which could not be read back. Neither file may change.
	addCard(t, b, "Would be saved", "")
	b.Archived[0].Done = time.Time{}
	err := r.Save(context.Background(), b)
	if err == nil || !strings.HasPrefix(err.Error(), "refusing to write") {
		t.Fatalf("Save error = %v, want one starting %q", err, "refusing to write")
	}
	if readFile(t, board) != boardBefore || readFile(t, archive) != archiveBefore {
		t.Fatal("Save changed a file although one half was refused")
	}
}

func TestSaveRefusesCardWithoutID(t *testing.T) {
	r, b, board, _ := boardWithArchive(t)
	before := readFile(t, board)
	b.Cards[0].ID = ""
	if err := r.Save(context.Background(), b); err == nil || !strings.HasPrefix(err.Error(), "refusing to write") {
		t.Fatalf("Save error = %v", err)
	}
	if readFile(t, board) != before {
		t.Fatal("board changed")
	}
}

func TestLoadDropsArchivedCopyOfLiveCard(t *testing.T) {
	r, b, board, archive := boardWithArchive(t)
	// The crash window: the archive was written, the board was not, so the
	// card is in both files.
	live := b.Cards[0]
	both := append([]domain.Card{live}, b.Archived...)
	both[0].Done = day
	if err := os.WriteFile(archive, markdown.FormatArchive(both), 0o644); err != nil {
		t.Fatal(err)
	}
	boardBefore := readFile(t, board)

	got := mustLoad(t, r)
	if len(got.Cards) != 1 || got.Cards[0].ID != live.ID {
		t.Fatalf("cards = %#v, want %s on the board", got.Cards, live.ID)
	}
	if len(got.Archived) != 1 || got.Archived[0].ID != "T-2" {
		t.Fatalf("archived = %#v, want only T-2", got.Archived)
	}
	if readFile(t, board) != boardBefore || !strings.Contains(readFile(t, archive), string(live.ID)) {
		t.Fatal("Load wrote a file")
	}

	mustSave(t, r, got)
	if strings.Contains(readFile(t, archive), "### "+string(live.ID)) {
		t.Fatalf("Save did not repair the archive:\n%s", readFile(t, archive))
	}
}

// boardBehindLockedDir returns a repository whose board file is reached
// through a symlink into a directory, plus a function that makes that
// directory refuse new files. The archive beside the real board is itself a
// link to a file outside that directory, so the board write fails while the
// archive can still be written.
func boardBehindLockedDir(t *testing.T) (r *markdown.Repo, board, realBoard string, lock func()) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("directory permissions do not stop root")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	realBoard = filepath.Join(locked, "board.md")
	board = filepath.Join(root, "board.md")
	if err := os.Symlink(realBoard, board); err != nil {
		t.Fatal(err)
	}
	if _, err := markdown.Create(realBoard, domain.NewBoard("X", "T").Settings); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePathOf(board), []byte("# Archive\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(archivePathOf(board), archivePathOf(realBoard)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	return markdown.New(board), board, realBoard, func() {
		if err := os.Chmod(locked, 0o555); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSaveWritesArchiveBeforeBoard(t *testing.T) {
	r, board, realBoard, lock := boardBehindLockedDir(t)
	b := mustLoad(t, r)
	done := addCard(t, b, "Finished", "Done")
	mustSave(t, r, b)

	if err := b.Archive([]domain.CardID{done.ID}, day); err != nil {
		t.Fatal(err)
	}
	// Temp files are all written before the first rename, so it takes a failure
	// at the board's own rename to stop a save between the two files.
	markdown.SetBeforeRename(r, func(path string) {
		if filepath.Base(path) == filepath.Base(realBoard) {
			lock()
		}
	})
	if err := r.Save(context.Background(), b); err == nil {
		t.Fatal("Save succeeded although the board could not be written")
	}
	if !strings.Contains(readFile(t, archivePathOf(board)), "### T-1 Finished") {
		t.Fatal("archive was not written first")
	}
	if !strings.Contains(readFile(t, realBoard), "### T-1 Finished") {
		t.Fatal("board changed although its write failed")
	}

	// A card in both files, rather than in neither, and Load copes.
	got := mustLoad(t, r)
	if len(got.Cards) != 1 || len(got.Archived) != 0 {
		t.Fatalf("after the failed save: %d cards, %d archived; want the board copy to win", len(got.Cards), len(got.Archived))
	}
	if len(got.Notices) != 1 || got.Notices[0] != "T-1 is on the board and in the archive; keeping the board copy" {
		t.Fatalf("notices = %q", got.Notices)
	}
}

// holdsCard reports whether the card is in the file as a card heading.
func holdsCard(t *testing.T, path string, id domain.CardID) bool {
	t.Helper()
	return strings.Contains(readFile(t, path), "### "+string(id)+" ")
}

func TestFailedSaveOfRestoreKeepsCardInArchive(t *testing.T) {
	r, board, _, lock := boardBehindLockedDir(t)
	b := mustLoad(t, r)
	done := addCard(t, b, "Finished", "Done")
	if err := b.Archive([]domain.CardID{done.ID}, day); err != nil {
		t.Fatal(err)
	}
	mustSave(t, r, b)

	b = mustLoad(t, r)
	if err := b.Restore(done.ID, "Todo", day); err != nil {
		t.Fatal(err)
	}
	lock()
	if err := r.Save(context.Background(), b); err == nil {
		t.Fatal("Save succeeded although the board could not be written")
	}
	if !holdsCard(t, archivePathOf(board), done.ID) {
		t.Fatalf("restored card is in neither file; archive:\n%s", readFile(t, archivePathOf(board)))
	}
	got := mustLoad(t, r)
	if _, _, ok := got.Find(done.ID); !ok {
		t.Fatal("a fresh Load cannot find the card")
	}
}

func TestFailedSaveThatArchivesAndRestoresLosesNoCard(t *testing.T) {
	r, board, _, lock := boardBehindLockedDir(t)
	b := mustLoad(t, r)
	old := addCard(t, b, "Old", "Done")
	fresh := addCard(t, b, "Fresh", "Done")
	if err := b.Archive([]domain.CardID{old.ID}, day); err != nil {
		t.Fatal(err)
	}
	mustSave(t, r, b)

	b = mustLoad(t, r)
	if err := b.Restore(old.ID, "Todo", day); err != nil {
		t.Fatal(err)
	}
	if err := b.Archive([]domain.CardID{fresh.ID}, day); err != nil {
		t.Fatal(err)
	}
	lock()
	if err := r.Save(context.Background(), b); err == nil {
		t.Fatal("Save succeeded although the board could not be written")
	}
	for _, id := range []domain.CardID{old.ID, fresh.ID} {
		if !holdsCard(t, board, id) && !holdsCard(t, archivePathOf(board), id) {
			t.Fatalf("%s is in neither file", id)
		}
	}
	got := mustLoad(t, r)
	for _, id := range []domain.CardID{old.ID, fresh.ID} {
		if _, _, ok := got.Find(id); !ok {
			t.Fatalf("a fresh Load cannot find %s", id)
		}
	}
}

func TestSaveThatArchivesAndRestoresEndsWithFinalFiles(t *testing.T) {
	r, _, _, archive := boardWithArchive(t) // T-1 live, T-2 archived
	b := mustLoad(t, r)
	if err := b.Restore("T-2", "Done", day); err != nil {
		t.Fatal(err)
	}
	moved := addCard(t, b, "Third", "Done")
	if err := b.Archive([]domain.CardID{moved.ID}, day); err != nil {
		t.Fatal(err)
	}
	mustSave(t, r, b)

	got := mustLoad(t, r)
	if len(got.Cards) != 2 || len(got.Archived) != 1 || got.Archived[0].ID != moved.ID {
		t.Fatalf("cards %v, archived %v", got.Cards, got.Archived)
	}
	if holdsCard(t, archive, "T-2") || !holdsCard(t, archive, moved.ID) {
		t.Fatalf("archive file is not the final archive:\n%s", readFile(t, archive))
	}
}

func TestSaveChangesNothingWhenATempFileCannotBeWritten(t *testing.T) {
	r, board, realBoard, lock := boardBehindLockedDir(t)
	b := mustLoad(t, r)
	done := addCard(t, b, "Finished", "Done")
	mustSave(t, r, b)
	boardBefore := readFile(t, realBoard)

	if err := b.Archive([]domain.CardID{done.ID}, day); err != nil {
		t.Fatal(err)
	}
	lock()
	if err := r.Save(context.Background(), b); err == nil {
		t.Fatal("Save succeeded although the board could not be written")
	}
	if strings.Contains(readFile(t, archivePathOf(board)), "### T-1 Finished") {
		t.Fatal("the archive was replaced although the board's temp file failed")
	}
	if readFile(t, realBoard) != boardBefore {
		t.Fatal("the board changed")
	}
}

func TestSaveRefusesWhenOnDiskArchiveIsUnreadable(t *testing.T) {
	_, b, board, archive := boardWithArchive(t)
	bad := "# Archive\n\n## not a month\n"
	if err := os.WriteFile(archive, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, board)
	// A Repo that has not loaded: one that has would stop at the changed file.
	if err := markdown.New(board).Save(context.Background(), b); err == nil || !strings.HasPrefix(err.Error(), "refusing to write") {
		t.Fatalf("Save error = %v, want one starting %q", err, "refusing to write")
	}
	if readFile(t, archive) != bad || readFile(t, board) != before {
		t.Fatal("Save changed a file")
	}
}

func TestLoadReservesArchivedIDs(t *testing.T) {
	r, _, board, archive := boardWithArchive(t) // T-1 live, T-2 archived
	// next_id was reverted by hand to 5, and the archive holds T-9.
	data := strings.Replace(readFile(t, board), "next_id: 3", "next_id: 5", 1)
	data = strings.Replace(data, "## Todo\n\n", "## Todo\n\n### No id yet\n\n", 1)
	if err := os.WriteFile(board, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	arch := strings.Replace(readFile(t, archive), "### T-2 ", "### T-9 ", 1)
	if err := os.WriteFile(archive, []byte(arch), 0o644); err != nil {
		t.Fatal(err)
	}

	got, assigned, err := r.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(assigned, []domain.CardID{"T-10"}) {
		t.Fatalf("assigned %v, want [T-10]", assigned)
	}
	if got.Settings.NextID != 11 {
		t.Fatalf("next id = %d, want 11", got.Settings.NextID)
	}
	if c, err := got.AddCard(domain.NewCard{Title: "New"}, day); err != nil || c.ID != "T-11" {
		t.Fatalf("AddCard = %v, %v; want T-11", c.ID, err)
	}
	mustSave(t, r, got)
	again := mustLoad(t, r)
	if _, archived, ok := again.Find("T-9"); !ok || !archived {
		t.Fatalf("archived T-9 was lost: cards %v, archived %v", got.Cards, again.Archived)
	}
}

func TestParseAssignedReservedKeepsOtherPrefixes(t *testing.T) {
	data := markdown.Format(domain.NewBoard("X", "T"))
	b, _, err := markdown.ParseAssignedReserved(data, []domain.CardID{"Z-40", "T-4", "junk"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Settings.NextID != 5 {
		t.Fatalf("next id = %d, want 5", b.Settings.NextID)
	}
}

func TestSaveKeepsSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	realPath := filepath.Join(root, "real", "board.md")
	link := filepath.Join(root, "board.md")
	if _, err := markdown.Create(realPath, domain.NewBoard("X", "T").Settings); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("real", "board.md"), link); err != nil {
		t.Fatal(err)
	}

	r := markdown.New(link)
	b := mustLoad(t, r)
	addCard(t, b, "Through the link", "")
	mustSave(t, r, b)

	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link was replaced by a regular file (%v)", err)
	}
	if !strings.Contains(readFile(t, realPath), "Through the link") {
		t.Fatal("the real file was not updated")
	}
	if got := entries(t, filepath.Join(root, "real")); !slices.Equal(got, []string{"board.md"}) {
		t.Fatalf("real directory holds %v", got)
	}
}

func TestSaveKeepsFileMode(t *testing.T) {
	r, path := newBoardRepo(t)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	b := mustLoad(t, r)
	addCard(t, b, "One", "")
	mustSave(t, r, b)
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v (%v), want 0600", fi.Mode().Perm(), err)
	}

	done := addCard(t, b, "Done", "Done")
	if err := b.Archive([]domain.CardID{done.ID}, day); err != nil {
		t.Fatal(err)
	}
	mustSave(t, r, b)
	if fi, err := os.Stat(archivePathOf(path)); err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("new archive mode = %v (%v), want 0644", fi.Mode().Perm(), err)
	}
}

func TestStaleLockFileIsIgnoredAndKept(t *testing.T) {
	r, path := newBoardRepo(t)
	lock := path + ".lock"
	if err := os.WriteFile(lock, []byte("12345 left over from a crash\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	unlock, err := r.Lock(ctx)
	if err != nil {
		t.Fatalf("Lock with a stale lock file: %v", err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("lock file was removed on unlock: %v", err)
	}
}

func TestLockStopsWhenContextIsCancelled(t *testing.T) {
	r, _ := newBoardRepo(t)
	unlock, err := r.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(30 * time.Millisecond); cancel() }()
	if _, err := r.Lock(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Lock error = %v, want context.Canceled", err)
	}
}

// linkTo makes link a symlink to target, creating link's directory.
func linkTo(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func TestPathKeepsTheRegisteredPathOfALink(t *testing.T) {
	_, board := newBoardRepo(t)
	link := filepath.Join(t.TempDir(), "link.md")
	linkTo(t, board, link)
	if got := markdown.New(link).Path(); got != link {
		t.Fatalf("Path = %q, want %q", got, link)
	}
}

func TestTwoPathsToOneFileShareALock(t *testing.T) {
	r, board := newBoardRepo(t)
	link := filepath.Join(t.TempDir(), "link.md")
	linkTo(t, board, link)

	byLink := markdown.New(link)
	unlock, err := byLink.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := r.Lock(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second path took the lock: %v", err)
	}
	if got := entries(t, filepath.Dir(link)); !slices.Equal(got, []string{"link.md"}) {
		t.Fatalf("beside the link: %v", got)
	}
}

func TestDanglingLinkIsAnErrorAndStaysALink(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "gone", "board.md")
	link := filepath.Join(dir, "board.md")
	linkTo(t, missing, link)

	r := markdown.New(link)
	if err := r.Save(context.Background(), domain.NewBoard("X", "T")); err == nil {
		t.Fatal("Save wrote through a dangling link")
	}
	if _, err := r.Lock(context.Background()); err == nil {
		t.Fatal("Lock succeeded on a dangling link")
	}
	if _, _, err := r.Load(context.Background()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load: %v", err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced (%v)", err)
	}
	if got := entries(t, dir); !slices.Equal(got, []string{"board.md"}) {
		t.Fatalf("directory holds %v", got)
	}
}

func TestDanglingArchiveLinkIsAnError(t *testing.T) {
	_, board := newBoardRepo(t)
	linkTo(t, filepath.Join(t.TempDir(), "gone.md"), archivePathOf(board))

	r := markdown.New(board)
	if _, _, err := r.Load(context.Background()); err == nil {
		t.Fatal("Load ignored a dangling archive link")
	}
	if err := r.Save(context.Background(), domain.NewBoard("X", "T")); err == nil {
		t.Fatal("Save wrote through a dangling archive link")
	}
}

func TestSymlinkedArchiveStaysALink(t *testing.T) {
	_, board := newBoardRepo(t)
	elsewhere := filepath.Join(t.TempDir(), "kept.archive.md")
	if err := os.WriteFile(elsewhere, []byte("# Archive\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkTo(t, elsewhere, archivePathOf(board))

	r := markdown.New(board)
	b := mustLoad(t, r)
	done := addCard(t, b, "Finished", "Done")
	if err := b.Archive([]domain.CardID{done.ID}, day); err != nil {
		t.Fatal(err)
	}
	mustSave(t, r, b)

	if !strings.Contains(readFile(t, elsewhere), "Finished") {
		t.Fatal("the archive's real file was not updated")
	}
	if fi, err := os.Lstat(archivePathOf(board)); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the archive link was replaced (%v)", err)
	}
}

func TestCreateThroughADirectoryLink(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkTo(t, filepath.Join(root, "real"), filepath.Join(root, "dir"))

	r, err := markdown.Create(filepath.Join(root, "dir", "board.md"), domain.NewBoard("X", "T").Settings)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := r.Path(), filepath.Join(root, "dir", "board.md"); got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
	if got := entries(t, filepath.Join(root, "real")); !slices.Equal(got, []string{"board.md"}) {
		t.Fatalf("real directory holds %v", got)
	}
}

func TestPath(t *testing.T) {
	if got := markdown.New("/x/board.md").Path(); got != "/x/board.md" {
		t.Fatalf("Path = %q", got)
	}
}

func TestLoadRefusesACardInBothFilesWithDifferentContent(t *testing.T) {
	archived := domain.Card{
		ID: "T-1", Title: "Same", Group: "G", Priority: "P1",
		Depends: []domain.CardID{"T-9"},
		Created: day, Done: day, Body: "Body",
		Extra: []domain.Field{{Key: "note", Value: "x"}},
	}
	cases := map[string]func(*domain.Card){
		"title":    func(c *domain.Card) { c.Title = "Other" },
		"body":     func(c *domain.Card) { c.Body = "Other" },
		"group":    func(c *domain.Card) { c.Group = "Other" },
		"priority": func(c *domain.Card) { c.Priority = "P2" },
		"depends":  func(c *domain.Card) { c.Depends = nil },
		"extra":    func(c *domain.Card) { c.Extra = []domain.Field{{Key: "note", Value: "y"}} },
		"created":  func(c *domain.Card) { c.Created = day.AddDate(0, 0, -1) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			r, board := newBoardRepo(t)
			b := mustLoad(t, r)
			live := archived
			live.Column = "Todo"
			live.Done = time.Time{}
			change(&live)
			b.Cards = []domain.Card{live}
			if err := os.WriteFile(board, markdown.Format(b), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(archivePathOf(board), markdown.FormatArchive([]domain.Card{archived}), 0o644); err != nil {
				t.Fatal(err)
			}

			_, _, err := r.Load(context.Background())

			var pe *markdown.ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("Load = %v, want a ParseError", err)
			}
		})
	}
}

func sameInode(t *testing.T, before os.FileInfo, path string) bool {
	t.Helper()
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return os.SameFile(before, after)
}

func TestSaveSkipsAFileWhoseBytesAreIdentical(t *testing.T) {
	r, b, board, archive := boardWithArchive(t)
	boardBefore, err := os.Stat(board)
	if err != nil {
		t.Fatal(err)
	}
	archiveBefore, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}

	mustSave(t, r, b)
	if !sameInode(t, boardBefore, board) || !sameInode(t, archiveBefore, archive) {
		t.Fatal("Save replaced a file with identical bytes")
	}

	// Only the board changes, so the archive stays the same file.
	addCard(t, b, "New", "")
	mustSave(t, r, b)
	if sameInode(t, boardBefore, board) {
		t.Fatal("the board was not written")
	}
	if !sameInode(t, archiveBefore, archive) {
		t.Fatal("Save replaced the unchanged archive")
	}
}
