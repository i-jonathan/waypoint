package markdown

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gofrs/flock"

	"waypoint/internal/app"
	"waypoint/internal/domain"
)

// lockRetry is how often a waiting Lock tries again.
const lockRetry = 50 * time.Millisecond

// Repo stores a board in a Markdown file, and its archive in the file beside
// it: X.md and X.archive.md.
type Repo struct {
	path string
}

var _ app.Repository = (*Repo)(nil)

// New returns a repository for the board file at path. It does not touch the
// disk.
func New(path string) *Repo { return &Repo{path: path} }

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
	if err := writeFileAtomic(path, data); err != nil {
		return nil, err
	}
	return New(path), nil
}

// Path returns the board file's path.
func (r *Repo) Path() string { return r.path }

// archivePath is X.archive.md for X.md.
func (r *Repo) archivePath() string {
	return strings.TrimSuffix(r.path, filepath.Ext(r.path)) + ".archive.md"
}

// Load reads the board and, if there is one, its archive. It never writes.
// The archive is read first, so that its ids stay reserved: a new card never
// takes the id of an archived one. A card found in both files is kept on the
// board and dropped from the archive in memory, with a notice, so the next
// Save repairs the file; this is what a crash between Save's writes leaves
// behind.
func (r *Repo) Load(ctx context.Context) (*domain.Board, []domain.CardID, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	archived, err := r.readArchive()
	if err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(r.path)
	if err != nil {
		return nil, nil, err
	}
	b, assigned, err := ParseAssignedReserved(data, cardIDs(archived))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", r.path, err)
	}
	live := make(map[domain.CardID]bool, len(b.Cards))
	for _, c := range b.Cards {
		live[c.ID] = true
	}
	for _, c := range archived {
		if live[c.ID] {
			b.Notices = append(b.Notices, fmt.Sprintf("%s is on the board and in the archive; keeping the board copy", c.ID))
			continue
		}
		b.Archived = append(b.Archived, c)
	}
	return b, assigned, nil
}

// readArchive parses the archive file; a missing file is an empty archive.
func (r *Repo) readArchive() ([]domain.Card, error) {
	data, err := os.ReadFile(r.archivePath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cards, err := ParseArchive(data, domain.Settings{})
	if err != nil {
		return nil, fmt.Errorf("archive %s: %w", r.archivePath(), err)
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
// should, Save returns an error starting "refusing to write" and changes no
// file.
//
// A card must be in at least one file at every point where a write can fail.
// Archiving a card writes the archive first, then the board. Restoring one
// reverses that, so the archive written first still holds the card: Save
// writes an archive that keeps the restored cards, then the board, then the
// final archive. A card in both files is resolved by Load.
func (r *Repo) Save(ctx context.Context, b *domain.Board) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	boardData := Format(b)
	if err := checkBoardOutput(boardData, b); err != nil {
		return err
	}
	if !exists(r.archivePath()) && len(b.Archived) == 0 {
		return r.writeBoard(boardData)
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
	if len(union) == len(finalCards) {
		if err := r.writeArchive(finalData); err != nil {
			return err
		}
		return r.writeBoard(boardData)
	}

	unionData := FormatArchive(union)
	if err := checkArchiveOutput(unionData, union, b.Settings); err != nil {
		return err
	}
	if err := r.writeArchive(unionData); err != nil {
		return err
	}
	if err := r.writeBoard(boardData); err != nil {
		return err
	}
	return r.writeArchive(finalData)
}

func (r *Repo) writeArchive(data []byte) error {
	if err := writeFileAtomic(r.archivePath(), data); err != nil {
		return fmt.Errorf("write archive: %w", err)
	}
	return nil
}

func (r *Repo) writeBoard(data []byte) error {
	if err := writeFileAtomic(r.path, data); err != nil {
		return fmt.Errorf("write board: %w", err)
	}
	return nil
}

// Lock takes an exclusive lock on <board path>.lock, waiting until ctx is
// done. The lock belongs to the operating system, so a lock file left behind
// by a crashed process holds nothing, and unlock does not delete the file.
func (r *Repo) Lock(ctx context.Context) (func() error, error) {
	fl := flock.New(r.path + ".lock")
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

// resolveLink follows symlinks at path, so that writing replaces the file the
// link points to and not the link. A path that does not exist is returned as
// it is.
func resolveLink(path string) (string, error) {
	for range 40 {
		fi, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			return path, nil
		}
		if err != nil {
			return "", err
		}
		if fi.Mode()&fs.ModeSymlink == 0 {
			return path, nil
		}
		target, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		path = target
	}
	return "", fmt.Errorf("%s: too many levels of symbolic links", path)
}

// writeFileAtomic replaces path with data: it writes a temp file in the same
// directory, syncs it, gives it the old file's permissions (0644 for a new
// file) and renames it into place. The temp file is removed on any error.
func writeFileAtomic(path string, data []byte) error {
	target, err := resolveLink(path)
	if err != nil {
		return err
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(target); err == nil {
		mode = fi.Mode().Perm()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), ".waypoint-*")
	if err != nil {
		return err
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
	if err == nil {
		err = os.Rename(tmp.Name(), target)
	}
	if err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
