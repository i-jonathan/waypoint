package markdown

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"waypoint/internal/app"
)

// version identifies one state of a file: which file it is, its size and its
// modification time. The zero version means there is no file.
type version struct{ info fs.FileInfo }

func versionOf(path string) (version, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return version{}, nil
	}
	if err != nil {
		return version{}, err
	}
	return version{info}, nil
}

func (v version) same(o version) bool {
	if v.info == nil || o.info == nil {
		return v.info == nil && o.info == nil
	}
	return os.SameFile(v.info, o.info) && v.info.Size() == o.info.Size() && v.info.ModTime().Equal(o.info.ModTime())
}

// versions is the state of a repository's two files.
type versions struct{ board, archive version }

// current reads the state of both files. Load calls it before it reads them: a
// write landing in between then looks like a change, which costs one retry. The
// other order would take the new state for the bytes it read before the write,
// and the write would be lost.
func (r *Repo) current() (versions, error) {
	board, err := versionOf(r.real)
	if err != nil {
		return versions{}, err
	}
	archive, err := versionOf(r.archive)
	if err != nil {
		return versions{}, err
	}
	return versions{board, archive}, nil
}

// remember records the state Save must find. A Repo serves one operation at a
// time: every request and command opens its own.
func (r *Repo) remember(v versions) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = &v
}

// recordWrite notes that path now holds the file Save put there, so that Save
// does not take its own write for someone else's.
func (r *Repo) recordWrite(path string, v version) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case r.seen == nil:
	case path == r.real:
		r.seen.board = v
	case path == r.archive:
		r.seen.archive = v
	}
}

// checkUnchanged returns app.ErrChangedDuringWrite if either file is not as Load
// left it. With no Load on this Repo there is nothing to compare with.
func (r *Repo) checkUnchanged() error {
	r.mu.Lock()
	seen := r.seen
	r.mu.Unlock()
	if seen == nil {
		return nil
	}
	now, err := r.current()
	if err != nil {
		return err
	}
	switch {
	case !now.board.same(seen.board):
		return fmt.Errorf("%w: %s", app.ErrChangedDuringWrite, r.path)
	case !now.archive.same(seen.archive):
		return fmt.Errorf("%w: %s", app.ErrChangedDuringWrite, r.archive)
	}
	return nil
}
