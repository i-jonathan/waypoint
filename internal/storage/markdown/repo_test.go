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

func TestSaveWritesArchiveBeforeBoard(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("directory permissions do not stop root")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	realBoard := filepath.Join(locked, "board.md")
	board := filepath.Join(root, "board.md")
	if err := os.Symlink(realBoard, board); err != nil {
		t.Fatal(err)
	}
	s := domain.NewBoard("X", "T").Settings
	if _, err := markdown.Create(realBoard, s); err != nil {
		t.Fatal(err)
	}
	r := markdown.New(board)
	b := mustLoad(t, r)
	done := addCard(t, b, "Finished", "Done")
	mustSave(t, r, b)

	if err := b.Archive([]domain.CardID{done.ID}, day); err != nil {
		t.Fatal(err)
	}
	// The archive goes next to the link and can be written. The board is
	// behind a directory that no longer takes new files.
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0o755)
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

func TestPath(t *testing.T) {
	if got := markdown.New("/x/board.md").Path(); got != "/x/board.md" {
		t.Fatalf("Path = %q", got)
	}
}
