package watch

import (
	"os"
	"path/filepath"
	"strings"
)

// fileState identifies one state of a file: which file it is, its size and its
// modification time. The zero value means there is no file there.
type fileState struct{ info os.FileInfo }

// stateOf follows symlinks, so a retargeted link reads as a different file.
func stateOf(path string) fileState {
	info, err := os.Stat(path)
	if err != nil {
		return fileState{}
	}
	return fileState{info}
}

func (s fileState) same(o fileState) bool {
	if s.info == nil || o.info == nil {
		return s.info == nil && o.info == nil
	}
	return os.SameFile(s.info, o.info) && s.info.Size() == o.info.Size() && s.info.ModTime().Equal(o.info.ModTime())
}

// fingerprint is the state of a board, its archive and its write-ahead log. Only a
// SQLite database has a log; while another program keeps the database open, a
// save lives in the log and the database file itself does not change.
type fingerprint struct{ board, archive, wal fileState }

func (f fingerprint) same(o fingerprint) bool {
	return f.board.same(o.board) && f.archive.same(o.archive) && f.wal.same(o.wal)
}

// boardFiles says where a project's files are. board is the path as registered,
// so that stating it goes through whatever now sits along that path; dir is where
// the real file lives, which is the directory the kernel watches.
type boardFiles struct{ board, archive, dir string }

func filesOf(t Target) boardFiles {
	dir, base := locate(t.Path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	return boardFiles{board: t.Path, archive: filepath.Join(dir, stem+".archive.md"), dir: dir}
}

func (b boardFiles) fingerprint() fingerprint {
	return fingerprint{stateOf(b.board), stateOf(b.archive), stateOf(b.board + "-wal")}
}
