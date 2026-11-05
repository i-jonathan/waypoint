package projects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"waypoint/internal/config"
	"waypoint/internal/domain"
	"waypoint/internal/storage"
	"waypoint/internal/storage/markdown"
)

// Seams for tests; production leaves them alone.
var (
	// now dates the backup names.
	now = time.Now
	// openTarget opens the converted copy for the check that it matches.
	openTarget = func(ref config.ProjectRef) (boardReader, error) { return storage.Open(ref) }
	// afterBackup runs once the old files have been renamed and before the
	// registry is saved.
	afterBackup = func() error { return nil }
)

// boardReader is what the check of the converted copy needs.
type boardReader interface {
	Load(ctx context.Context) (*domain.Board, []domain.CardID, error)
}

// ConvertResult says what Convert did.
type ConvertResult struct {
	Ref     config.ProjectRef // the project as now registered
	Backups []string          // where the old files are now
}

// Convert moves project name to the other storage. to is "sqlite" or "markdown"
// and newPath an absolute path that must not exist yet; empty keeps the old
// board's folder and name with the new extension.
//
// It takes the project's lock and the config lock, then:
//  1. loads the whole project (settings, cards, archive);
//  2. writes it to newPath through the target storage;
//  3. loads the copy again and requires it to equal the original, and otherwise
//     removes the copy and stops;
//  4. renames the old files to <name>.bak-YYYYMMDD (-2, -3, ... if taken);
//  5. points the registry at the new path.
//
// The old files are never deleted. If any step fails, the registry is left as
// it was, the copy is removed and any renamed files are put back.
func Convert(ctx context.Context, cfgPath string, env func(string) string, name, to, newPath string) (ConvertResult, error) {
	if to != "sqlite" && to != "markdown" {
		return ConvertResult{}, fmt.Errorf("%w: storage %q must be sqlite or markdown", domain.ErrInvalid, to)
	}
	var res ConvertResult
	var undo func()
	err := config.Update(cfgPath, env, func(cfg *config.Config) error {
		ref, ok := cfg.Find(name)
		if !ok {
			return fmt.Errorf("%w: no project named %q", domain.ErrNotFound, name)
		}
		if ref.Storage == to {
			return fmt.Errorf("%w: project %q is already stored as %s", domain.ErrInvalid, ref.Name, to)
		}
		target, err := convertTarget(cfg, ref, to, newPath)
		if err != nil {
			return err
		}
		dstRef := config.ProjectRef{Name: ref.Name, Storage: to, Path: target}

		src, err := storage.Open(ref)
		if err != nil {
			return err
		}
		unlock, err := src.Lock(ctx)
		if err != nil {
			return err
		}
		// The lock is held through the renames, so no write can land in files
		// that are about to become backups. Releasing it is best effort: the
		// conversion is done by then.
		defer func() { _ = unlock() }()

		board, _, err := src.Load(ctx)
		if err != nil {
			return err
		}
		if err := writeCopy(ctx, dstRef, board); err != nil {
			return err
		}
		moved, restore, err := backUp(ref)
		if err != nil {
			removeBoardFiles(dstRef)
			return err
		}
		undo = func() {
			restore()
			removeBoardFiles(dstRef)
		}
		if err := afterBackup(); err != nil {
			return err
		}
		for i := range cfg.Projects {
			if cfg.Projects[i].Name == ref.Name {
				cfg.Projects[i].Path, cfg.Projects[i].Storage = target, to
				res = ConvertResult{Ref: cfg.Projects[i], Backups: moved}
			}
		}
		return nil
	})
	if err != nil {
		if undo != nil {
			undo()
		}
		return ConvertResult{}, err
	}
	return res, nil
}

// convertTarget validates newPath, or picks the default one. Nothing may exist
// there, and for markdown nothing may sit where its archive file would go.
func convertTarget(cfg *config.Config, ref config.ProjectRef, to, newPath string) (string, error) {
	if newPath == "" {
		ext := ".db"
		if to == "markdown" {
			ext = ".md"
		}
		base := filepath.Base(ref.Path)
		newPath = filepath.Join(filepath.Dir(ref.Path), strings.TrimSuffix(base, filepath.Ext(base))+ext)
	}
	if !filepath.IsAbs(newPath) {
		return "", fmt.Errorf("%w: the new path %q must be absolute", domain.ErrInvalid, newPath)
	}
	newPath = filepath.Clean(newPath)
	if _, err := os.Lstat(newPath); err == nil {
		return "", fmt.Errorf("%w: %s already exists; choose a path with nothing there", domain.ErrInvalid, newPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if _, err := chooseStorage(newPath, to); err != nil {
		return "", err
	}
	if q, ok := RegisteredAt(cfg, newPath); ok {
		return "", fmt.Errorf("%w: %s is already registered as %q", domain.ErrInvalid, newPath, q.Name)
	}
	if to == "markdown" {
		if err := checkArchive(cfg, newPath); err != nil {
			return "", err
		}
		if archive := markdown.ArchiveFile(newPath); exists(archive) {
			return "", fmt.Errorf("%w: %s already exists and would become this board's archive; choose another name", domain.ErrInvalid, archive)
		}
	}
	return newPath, nil
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// writeCopy creates the new board, saves board into it and checks that reading
// it back gives the same board. On any failure the copy is removed.
func writeCopy(ctx context.Context, dst config.ProjectRef, board *domain.Board) error {
	repo, err := storage.Create(dst, board.Settings)
	if err != nil {
		removeBoardFiles(dst)
		return err
	}
	if err := repo.Save(ctx, board); err != nil {
		removeBoardFiles(dst)
		return err
	}
	r, err := openTarget(dst)
	if err == nil {
		var got *domain.Board
		if got, _, err = r.Load(ctx); err == nil {
			if d := difference(board, got); d != "" {
				hint := ""
				if dst.Storage == "markdown" {
					hint = ". A Markdown board keeps only headings of #### or deeper inside a card, so edit that card's body and convert again"
				}
				err = fmt.Errorf("%w: the converted copy did not match the original (%s); nothing was changed%s", domain.ErrInvalid, d, hint)
			}
		}
	}
	if err != nil {
		removeBoardFiles(dst)
		return err
	}
	return nil
}

// difference names the first thing that differs between two boards, or "".
func difference(a, b *domain.Board) string {
	if !(&domain.Board{Settings: a.Settings}).Equal(&domain.Board{Settings: b.Settings}) {
		return "the project settings differ"
	}
	for _, list := range []struct {
		what string
		a, b []domain.Card
	}{{"card", a.Cards, b.Cards}, {"archived card", a.Archived, b.Archived}} {
		for i := range list.a {
			if i >= len(list.b) {
				return fmt.Sprintf("the %s %s is missing", list.what, list.a[i].ID)
			}
			if !list.a[i].Equal(list.b[i]) {
				return fmt.Sprintf("the %s %s differs", list.what, list.a[i].ID)
			}
		}
		if len(list.b) > len(list.a) {
			return fmt.Sprintf("the copy has an extra %s %s", list.what, list.b[len(list.a)].ID)
		}
	}
	return ""
}

// removeBoardFiles deletes the files of a board that Convert itself created.
func removeBoardFiles(ref config.ProjectRef) {
	paths := []string{ref.Path}
	if ref.Storage == "sqlite" {
		paths = append(paths, ref.Path+"-wal", ref.Path+"-shm", ref.Path+"-journal")
	} else {
		paths = append(paths, markdown.ArchiveFile(ref.Path), ref.Path+".lock")
	}
	for _, p := range paths {
		os.Remove(p)
	}
}

// boardFilesOf lists the files that hold a project now, as they really are on
// disk (symlinks resolved), keeping the ones that exist.
func boardFilesOf(ref config.ProjectRef) []string {
	var paths []string
	if ref.Storage == "markdown" {
		board, archive := markdown.New(ref.Path).Files()
		paths = []string{board, archive}
	} else {
		real := ref.Path
		if r, err := filepath.EvalSymlinks(ref.Path); err == nil {
			real = r
		}
		paths = []string{real, real + "-wal", real + "-shm"}
	}
	var out []string
	for _, p := range paths {
		if p != "" && exists(p) {
			out = append(out, p)
		}
	}
	return out
}

// backUp renames the old project's files to <name>.bak-YYYYMMDD, using -2, -3,
// ... for the whole set when any of those names is taken. It returns the new
// names, and a function that puts everything back. If a rename fails, the ones
// done are undone before the error returns.
func backUp(ref config.ProjectRef) (moved []string, restore func(), err error) {
	files := boardFilesOf(ref)
	stamp := ".bak-" + now().Format("20060102")
	suffix := ""
	for n := 2; ; n++ {
		taken := false
		for _, f := range files {
			if exists(f + stamp + suffix) {
				taken = true
			}
		}
		if !taken {
			break
		}
		suffix = fmt.Sprintf("-%d", n)
	}
	var from []string
	restore = func() {
		for i := len(from) - 1; i >= 0; i-- {
			os.Rename(moved[i], from[i])
		}
	}
	for _, f := range files {
		to := f + stamp + suffix
		if err := os.Rename(f, to); err != nil {
			restore()
			return nil, func() {}, fmt.Errorf("keep the old file %s as %s: %w", f, to, err)
		}
		from, moved = append(from, f), append(moved, to)
	}
	return moved, restore, nil
}
