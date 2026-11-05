package markdown

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"

	"waypoint/internal/app"
	"waypoint/internal/domain"
)

// lockRetry is how often a waiting Lock tries again.
const lockRetry = 50 * time.Millisecond

// Repo stores a board in a Markdown file, and its archive in the file beside
// it: X.md and X.archive.md.
//
// Everything on disk derives from the real path of the board file, so that two
// paths to one file share one lock and one archive. Only Path keeps the path
// the caller gave.
type Repo struct {
	path    string // as given to New
	real    string // the board file, with every symlink resolved
	archive string // the archive file, resolved the same way
	err     error  // why those paths could not be worked out

	mu   sync.Mutex
	seen *versions // the files as the last Load found them; nil before any Load

	// beforeRename, when set, runs just before each file is renamed into place,
	// with the temp file already synced. It is a test seam.
	beforeRename func(path string)
}

var _ app.Repository = (*Repo)(nil)

// New returns a repository for the board file at path. It resolves symlinks in
// path and writes nothing. A path that cannot be resolved, such as a dangling
// symlink, makes every later Load, Save and Lock fail, rather than replace the
// link with a new file.
func New(path string) *Repo {
	r := &Repo{path: path}
	if r.real, r.err = canonical(path); r.err != nil {
		return r
	}
	r.archive, r.err = canonical(strings.TrimSuffix(r.real, filepath.Ext(r.real)) + ".archive.md")
	return r
}

// canonical returns path with every symlink resolved, the way the kernel
// resolves them. A file that does not exist yet resolves through its parent
// directory. A dangling symlink is an error.
func canonical(path string) (string, error) {
	real, err := filepath.EvalSymlinks(path)
	if err == nil {
		return real, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if _, err := os.Lstat(path); err == nil {
		return "", fmt.Errorf("%s is a symlink to a file that does not exist: %w", path, fs.ErrNotExist)
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filepath.Base(path)), nil
}

// Create writes a new, empty board with the given settings and returns its
// repository. It refuses to overwrite: an existing file gives os.ErrExist.
func Create(path string, s domain.Settings) (*Repo, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(path); err == nil {
		return nil, fmt.Errorf("%s: %w", path, os.ErrExist)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	b := &domain.Board{Settings: s}
	data := Format(b)
	if err := checkBoardOutput(data, b); err != nil {
		return nil, err
	}
	r := New(path)
	if r.err != nil {
		return nil, r.err
	}
	if err := writeFileAtomic(r.real, data); err != nil {
		return nil, err
	}
	return r, nil
}

// Path returns the board file's path as it was given to New.
func (r *Repo) Path() string { return r.path }

// Load reads the board and, if there is one, its archive. It never writes.
// The archive is read first, so that its ids stay reserved: a new card never
// takes the id of an archived one.
//
// A card found in both files with the same content, as a save would write it,
// is what a crash between Save's writes leaves behind: it is kept on the board
// and dropped from the archive in memory, with a notice, so the next Save
// repairs the file. Column and Done are not compared, because those are what
// the interrupted Save was changing. With any other difference these are two
// cards sharing an id, such as a hand-added one, so Load fails rather than let
// a Save delete one of them.
func (r *Repo) Load(ctx context.Context) (*domain.Board, []domain.CardID, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if r.err != nil {
		return nil, nil, r.err
	}
	seen, err := r.current()
	if err != nil {
		return nil, nil, err
	}
	archived, err := r.readArchive()
	if err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(r.real)
	if err != nil {
		return nil, nil, err
	}
	b, assigned, err := ParseAssignedReserved(data, cardIDs(archived))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", r.path, err)
	}
	live := make(map[domain.CardID]domain.Card, len(b.Cards))
	for _, c := range b.Cards {
		live[c.ID] = c
	}
	for _, c := range archived {
		onBoard, both := live[c.ID]
		if !both {
			b.Archived = append(b.Archived, c)
			continue
		}
		if !sameOnceSaved(onBoard, c) {
			return nil, nil, &ParseError{Msg: fmt.Sprintf("%s is on the board and in the archive with different content: delete the copy you don't want, or if they are separate cards, give one a new id (board: %s, archive: %s)", c.ID, r.path, r.archive)}
		}
		b.Notices = append(b.Notices, fmt.Sprintf("%s is on the board and in the archive; keeping the board copy", c.ID))
	}
	r.remember(seen)
	return b, assigned, nil
}

// sameOnceSaved reports whether two cards have the same content as Format would
// write it. A save normalises a body, so the copy written by an interrupted Save
// can differ from one no save has touched yet, and still be the same card.
func sameOnceSaved(a, b domain.Card) bool {
	a.Body, b.Body = formatBody(a.Body), formatBody(b.Body)
	return a.SameContent(b)
}

// readArchive parses the archive file; a missing file is an empty archive.
func (r *Repo) readArchive() ([]domain.Card, error) {
	data, err := os.ReadFile(r.archive)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cards, err := ParseArchive(data, domain.Settings{})
	if err != nil {
		return nil, fmt.Errorf("archive %s: %w", r.archive, err)
	}
	return cards, nil
}

func cardIDs(cards []domain.Card) []domain.CardID {
	out := make([]domain.CardID, len(cards))
	for i, c := range cards {
		out[i] = c.ID
	}
	return out
}

// Save writes the board, and the archive when there are archived cards or an
// archive file already exists. Before replacing anything it reads back every
// byte slice it is about to write; if one does not hold exactly the cards it
// should, or if a file it would replace has no owner write bit, Save returns an
// error starting "refusing to write" and changes no file. A rename would
// replace a read-only file without complaint, so that bit is checked here.
//
// A card must be in at least one file at every point where a write can fail.
// Archiving a card writes the archive first, then the board. Restoring one
// reverses that, so the archive written first still holds the card: Save
// writes an archive that keeps the restored cards, then the board, then the
// final archive. A card in both files is resolved by Load.
//
// Every temp file is written and synced first, so the renames follow each other
// at once. Just before each rename Save compares both files with what Load
// found, and on any difference it returns app.ErrChangedDuringWrite and writes
// nothing further. That leaves the file someone else wrote in place. If the
// archive was already replaced, the card it took is in both files, which Load
// resolves as above.
func (r *Repo) Save(ctx context.Context, b *domain.Board) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.err != nil {
		return r.err
	}
	if err := r.checkUnchanged(); err != nil {
		return err
	}
	boardData := Format(b)
	if err := checkBoardOutput(boardData, b); err != nil {
		return err
	}
	writesArchive := exists(r.archive) || len(b.Archived) > 0
	if err := checkWritable(r.real, r.path); err != nil {
		return err
	}
	board := fileWrite{r.real, "board", boardData}
	if !writesArchive {
		return r.commit(board)
	}
	if err := checkWritable(r.archive, r.archive); err != nil {
		return err
	}

	onDisk, err := r.readArchive()
	if err != nil {
		return fmt.Errorf("refusing to write: the existing archive cannot be read: %w", err)
	}
	finalCards := b.Archived
	finalData := FormatArchive(finalCards)
	if err := checkArchiveOutput(finalData, finalCards, b.Settings); err != nil {
		return err
	}

	// Cards that are archived on disk and live now: the archive keeps them
	// until the board has them.
	union := slices.Clone(finalCards)
	have := make(map[domain.CardID]bool, len(union))
	for _, c := range union {
		have[c.ID] = true
	}
	live := make(map[domain.CardID]bool, len(b.Cards))
	for _, c := range b.Cards {
		live[c.ID] = true
	}
	for _, c := range onDisk {
		if live[c.ID] && !have[c.ID] {
			union = append(union, c)
		}
	}
	final := fileWrite{r.archive, "archive", finalData}
	if len(union) == len(finalCards) {
		return r.commit(final, board)
	}

	unionData := FormatArchive(union)
	if err := checkArchiveOutput(unionData, union, b.Settings); err != nil {
		return err
	}
	return r.commit(fileWrite{r.archive, "archive", unionData}, board, final)
}

// fileWrite is one file for commit to replace. name is how an error refers to it.
type fileWrite struct {
	path, name string
	data       []byte
}

// commit replaces the files in order. Nothing is renamed until every temp file
// is on disk, and each rename is preceded by checkUnchanged, as close to it as
// the file system allows.
func (r *Repo) commit(writes ...fileWrite) error {
	files := make([]*stagedFile, 0, len(writes))
	defer func() {
		for _, f := range files {
			f.discard()
		}
	}()
	for _, w := range writes {
		f, err := stage(w.path, w.data)
		if err != nil {
			return fmt.Errorf("write %s: %w", w.name, err)
		}
		files = append(files, f)
	}
	for i, f := range files {
		if r.beforeRename != nil {
			r.beforeRename(f.path)
		}
		// A file with nothing to rename is not at risk of losing a write, and a
		// change to it must not fail an operation whose other files are done.
		if f.tmp != "" {
			if err := r.checkUnchanged(); err != nil {
				return err
			}
		}
		if err := f.install(); err != nil {
			return fmt.Errorf("write %s: %w", writes[i].name, err)
		}
		if f.tmp != "" {
			r.recordWrite(f.path, f.version)
			f.tmp = ""
		}
	}
	return nil
}

// Lock takes an exclusive lock on <board path>.lock beside the real board
// file, waiting until ctx is done. The lock belongs to the operating system, so
// a lock file left behind by a crashed process holds nothing, and unlock does
// not delete the file.
func (r *Repo) Lock(ctx context.Context) (func() error, error) {
	if r.err != nil {
		return nil, r.err
	}
	fl := flock.New(r.real + ".lock")
	locked, err := fl.TryLockContext(ctx, lockRetry)
	if err != nil {
		return nil, fmt.Errorf("lock %s: %w", r.path, err)
	}
	if !locked {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("lock %s: %w", r.path, err)
		}
		return nil, fmt.Errorf("lock %s: not acquired", r.path)
	}
	return fl.Unlock, nil
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// checkWritable refuses a file that exists with no owner write bit. name is how
// the error refers to it.
func checkWritable(path, name string) error {
	fi, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode().Perm()&0o200 == 0 {
		return fmt.Errorf("refusing to write: %s is read-only", name)
	}
	return nil
}

// checkBoardOutput reads back the board bytes and compares their cards with b.
func checkBoardOutput(data []byte, b *domain.Board) error {
	got, err := Parse(data)
	if err != nil {
		return fmt.Errorf("refusing to write the board: it would not read back: %w", err)
	}
	if err := sameCards(b.Cards, got.Cards); err != nil {
		return fmt.Errorf("refusing to write the board: %w", err)
	}
	return nil
}

// checkArchiveOutput does the same for the archive bytes.
func checkArchiveOutput(data []byte, want []domain.Card, settings domain.Settings) error {
	got, err := ParseArchive(data, settings)
	if err != nil {
		return fmt.Errorf("refusing to write the archive: it would not read back: %w", err)
	}
	if err := sameCards(want, got); err != nil {
		return fmt.Errorf("refusing to write the archive: %w", err)
	}
	return nil
}

// sameCards reports whether got holds as many cards as want, with the same ids.
func sameCards(want, got []domain.Card) error {
	if len(want) != len(got) {
		return fmt.Errorf("%d cards would be written for %d in memory", len(got), len(want))
	}
	left := make(map[domain.CardID]int, len(want))
	for _, c := range want {
		left[c.ID]++
	}
	for _, c := range got {
		if left[c.ID] == 0 {
			return fmt.Errorf("card %q would be written, but is not in memory", c.ID)
		}
		left[c.ID]--
	}
	for id, n := range left {
		if n > 0 {
			return fmt.Errorf("card %q would be lost", id)
		}
	}
	return nil
}

// writeFileAtomic replaces path, which must already be resolved (see canonical),
// with data.
func writeFileAtomic(path string, data []byte) error {
	f, err := stage(path, data)
	if err != nil {
		return err
	}
	defer f.discard()
	return f.install()
}

// stagedFile is a synced temp file waiting to replace path. An empty tmp means
// path already holds the data, so there is nothing to do.
type stagedFile struct {
	path, tmp string
	version   version // the temp file's, which a rename keeps
}

// stage writes data to a temp file in path's directory, syncs it and gives it the
// old file's permissions (0644 for a new file). A file that already holds exactly
// data is left alone. An archive written twice in one Save cannot be caught out
// by that: the second write always drops a card the file on disk still has.
func stage(path string, data []byte) (*stagedFile, error) {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return &stagedFile{path: path}, nil
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".waypoint-*")
	if err != nil {
		return nil, err
	}
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), mode)
	}
	var v version
	if err == nil {
		v, err = versionOf(tmp.Name())
	}
	if err != nil {
		os.Remove(tmp.Name())
		return nil, err
	}
	return &stagedFile{path: path, tmp: tmp.Name(), version: v}, nil
}

// install renames the temp file over path.
func (f *stagedFile) install() error {
	if f.tmp == "" {
		return nil
	}
	return os.Rename(f.tmp, f.path)
}

// discard removes the temp file unless it was installed.
func (f *stagedFile) discard() {
	if f.tmp != "" {
		os.Remove(f.tmp)
	}
}
