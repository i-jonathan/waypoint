package projects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"waypoint/internal/config"
	"waypoint/internal/dbconn"
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
	// afterCopy runs once the verified copy is in place, before the registry is saved.
	afterCopy = func() error { return nil }
	// afterRegistry runs once the registry points at the copy, before the old files move.
	afterRegistry = func() error { return nil }
	// retireSource marks the old storage as left behind.
	retireSource = func(ctx context.Context, r retirer) error { return r.Retire(ctx) }
)

// boardReader is what the check of the converted copy needs.
type boardReader interface {
	Load(ctx context.Context) (*domain.Board, []domain.CardID, error)
}

// retirer is implemented by a repository that can mark its storage as left
// behind (SQLite and the database servers).
type retirer interface {
	Retire(ctx context.Context) error
}

// ConvertResult says what Convert did.
type ConvertResult struct {
	Ref     config.ProjectRef // the project as now registered
	Backups []string          // where the old files are now
	Retired string            // the old database on a server, marked retired and left in place
}

// Target says where a project goes: a storage, and for a file storage a path
// ("" keeps the old name and folder with the new extension), for a server the
// connection details.
type Target struct {
	Storage string // "sqlite", "markdown" or "postgres"
	Path    string
	Server  *ServerOptions
	// Warn receives the notices for a server that is not on this computer, before
	// anything connects.
	Warn func(string)
}

// Convert moves project name to another storage. For a file target Path must
// not exist yet; empty keeps the old board's folder and name with the new
// extension (or the data folder, when the old board is on a server). A server
// target must be an empty database.
//
// It takes the project's lock and keeps it until the old board is out of the
// way, so no write can land in a board that is about to be left. Then:
//  1. load the whole project (settings, cards, archive);
//  2. write it under a temporary name beside the target (a file) or into the
//     empty database (a server), load it again and require it to equal the
//     original, and only then give a file its real name;
//  3. point the registry at it;
//  4. mark a SQLite or server original as retired (so a writer that was waiting
//     for it fails instead of saving into it); rename an old file to
//     <name>.bak-YYYYMMDD (-2, -3, ... if taken). A server database is never
//     renamed, emptied or dropped.
//
// The registry changes before the old board is touched, so a crash at any
// point leaves it pointing at a board that exists. If a step before 3 fails,
// the copy is removed and nothing else has changed. If step 4 fails, the
// project is already converted: Convert returns the result with an error
// saying what is left behind.
func Convert(ctx context.Context, cfgPath string, env func(string) string, name string, to Target) (ConvertResult, error) {
	switch to.Storage {
	case "sqlite", "markdown":
		if to.Server != nil {
			return ConvertResult{}, fmt.Errorf("%w: server settings need storage %s", domain.ErrInvalid, strings.Join(serverEngines, " or "))
		}
	default:
		if !slices.Contains(serverEngines, to.Storage) {
			return ConvertResult{}, fmt.Errorf("%w: storage %q must be sqlite, markdown, %s", domain.ErrInvalid, to.Storage, strings.Join(serverEngines, " or "))
		}
		if to.Server == nil {
			return ConvertResult{}, fmt.Errorf("%w: storage %s needs the server settings", domain.ErrInvalid, to.Storage)
		}
		if to.Path != "" {
			return ConvertResult{}, fmt.Errorf("%w: a %s project has no path", domain.ErrInvalid, to.Storage)
		}
	}
	cfg0, err := config.Load(cfgPath, env)
	if err != nil {
		return ConvertResult{}, err
	}
	ref, ok := cfg0.Find(name)
	if !ok {
		return ConvertResult{}, fmt.Errorf("%w: no project named %q", domain.ErrNotFound, name)
	}
	if ref.Storage == to.Storage && !ref.IsServer() {
		return ConvertResult{}, fmt.Errorf("%w: project %q is already stored as %s", domain.ErrInvalid, ref.Name, to.Storage)
	}
	var dstServer config.ProjectRef
	if to.Server != nil {
		so := *to.Server
		so.Engine = to.Storage
		if dstServer, err = serverRef(ref.Name, so); err != nil {
			return ConvertResult{}, err
		}
		if to.Warn != nil && !dbconn.IsLocal(dstServer.Host) {
			to.Warn(dbconn.NonLocalWarning)
			if dstServer.SSL == "disable" {
				to.Warn(dbconn.UnencryptedNote)
			}
		}
	}
	src, err := storage.Open(ref)
	if err != nil {
		return ConvertResult{}, err
	}
	unlock, err := src.Lock(ctx)
	if err != nil {
		return ConvertResult{}, err
	}
	released := false
	release := func() {
		if !released {
			released = true
			_ = unlock() // the conversion is done or abandoned by now
		}
	}
	defer release()

	board, _, err := src.Load(ctx)
	if err != nil {
		return ConvertResult{}, err
	}
	if to.Storage == "markdown" {
		// Markdown drops blank lines at both ends of a body, so a board made by an
		// older Waypoint, or by a program, could never match its copy. Leave them
		// out of the copy and the comparison: nothing but those blank lines changes.
		board = board.Clone()
		for _, list := range [][]domain.Card{board.Cards, board.Archived} {
			for i := range list {
				list[i].Body = domain.NormalizeBody(list[i].Body)
			}
		}
	}

	var res ConvertResult
	var removeCopy func()
	err = config.Update(cfgPath, env, func(cfg *config.Config) error {
		cur, ok := cfg.Find(name)
		if !ok || cur != ref {
			return fmt.Errorf("%w: project %q changed while it was being converted; try again", domain.ErrInvalid, name)
		}
		var dstRef config.ProjectRef
		if to.Server != nil {
			// The new entry must be valid beside the other projects, as if it were added.
			others := &config.Config{}
			for _, q := range cfg.Projects {
				if q.Name != cur.Name {
					others.Projects = append(others.Projects, q)
				}
			}
			if err := others.Add(dstServer); err != nil {
				return err
			}
			dstRef = dstServer
		} else {
			target, err := convertTarget(cfg, cur, to.Storage, to.Path)
			if err != nil {
				return err
			}
			dstRef = config.ProjectRef{Name: cur.Name, Storage: to.Storage, Path: target}
		}
		discard, err := writeCopy(ctx, dstRef, board, storage.CreateOptions{CreateDatabase: to.Server != nil && to.Server.CreateDatabase})
		if err != nil {
			return err
		}
		removeCopy = discard
		if err := afterCopy(); err != nil {
			return err
		}
		for i := range cfg.Projects {
			if cfg.Projects[i].Name == cur.Name {
				cfg.Projects[i] = dstRef
				res.Ref = dstRef
			}
		}
		return nil
	})
	if err != nil {
		if removeCopy != nil { // the copy was made but the registry was not saved
			removeCopy()
		}
		return ConvertResult{}, err
	}

	// The registry now points at the copy. From here the project is converted.
	if err := afterRegistry(); err != nil {
		return res, err
	}
	var problems []string
	retireFailed := false
	if r, ok := src.(retirer); ok { // SQLite or a server: retire, let go, then move the files
		if err := retireSource(ctx, r); err != nil {
			retireFailed = true
			problems = append(problems, "could not mark the old database as retired: "+err.Error())
		} else if ref.IsServer() {
			res.Retired = ref.Display()
		}
		release()
	}
	if !ref.IsServer() {
		moved, err := backUp(ref)
		release() // markdown: the files moved while the lock was held
		res.Backups = moved
		if err != nil {
			problems = append(problems, err.Error())
		}
	}
	release()
	if len(problems) > 0 {
		if ref.IsServer() && retireFailed {
			return res, fmt.Errorf("project %q now uses %s, but the old database %s could not be marked retired (%s), so it is still a live board that nothing uses. To retire it, run: %s",
				ref.Name, res.Ref.Display(), ref.Display(), strings.TrimPrefix(problems[0], "could not mark the old database as retired: "), storage.RetireStatement(ref))
		}
		return res, fmt.Errorf("project %q now uses %s, but %s; the old files stayed where they were", ref.Name, res.Ref.Display(), strings.Join(problems, " and "))
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
		if ref.IsServer() { // no folder of its own: the data folder, like a new project
			s := slug(ref.Name)
			if s == "" {
				return "", fmt.Errorf("%w: cannot make a file name from %q; give a path", domain.ErrInvalid, ref.Name)
			}
			newPath = filepath.Join(cfg.DataDir, s+ext)
		} else {
			base := filepath.Base(ref.Path)
			newPath = filepath.Join(filepath.Dir(ref.Path), strings.TrimSuffix(base, filepath.Ext(base))+ext)
		}
	}
	if !filepath.IsAbs(newPath) {
		return "", fmt.Errorf("%w: the new path %q must be absolute", domain.ErrInvalid, newPath)
	}
	newPath = filepath.Clean(newPath)
	if _, err := os.Lstat(newPath); err == nil {
		return "", fmt.Errorf("%w: %s already exists; choose a path with nothing there", domain.ErrInvalid, newPath)
	} else if !errors.Is(err, os.ErrNotExist) { // for example, a folder of the path is a file
		return "", fmt.Errorf("%w: cannot use %s: %v", domain.ErrInvalid, newPath, err)
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

// writeCopy writes board to dst, checks that reading the copy back gives the
// same board, and returns a func that takes the copy away again. A file goes by
// way of a temporary name beside it and gets its real name only when it
// checks out; a server board is written into the empty database, and the
// tables this run made are dropped if anything fails. A crash leaves at worst a
// .waypoint-convert-* file to delete by hand, or tables Waypoint made in the
// target. Nothing that was there before is ever overwritten.
func writeCopy(ctx context.Context, dst config.ProjectRef, board *domain.Board, opts storage.CreateOptions) (discard func(), err error) {
	if dst.IsServer() {
		return writeServerCopy(ctx, dst, board, opts)
	}
	tmp := dst
	tmp.Path = filepath.Join(filepath.Dir(dst.Path), fmt.Sprintf(".waypoint-convert-%d-%d%s", os.Getpid(), time.Now().UnixNano(), filepath.Ext(dst.Path)))
	defer removeBoardFiles(tmp) // whatever is left under the temporary name

	repo, err := storage.Create(tmp, board.Settings)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot create the converted copy in %s: %v", domain.ErrInvalid, filepath.Dir(dst.Path), err)
	}
	if err := repo.Save(ctx, board); err != nil {
		return nil, err
	}
	if err := verifyCopy(ctx, tmp, dst, board); err != nil {
		return nil, err
	}
	if err := install(tmp, dst); err != nil {
		return nil, err
	}
	return func() { removeBoardFiles(dst) }, nil
}

// verifyCopy loads the copy at at and requires it to equal board.
func verifyCopy(ctx context.Context, at, dst config.ProjectRef, board *domain.Board) error {
	r, err := openTarget(at)
	if err != nil {
		return err
	}
	got, _, err := r.Load(ctx)
	if err != nil {
		return err
	}
	if d := difference(board, got); d != "" {
		hint := ""
		if dst.Storage == "markdown" && hasShallowHeading(board) {
			hint = ". A Markdown board keeps only headings of #### or deeper inside a card, so edit that card's body and convert again"
		}
		return fmt.Errorf("%w: the converted copy did not match the original (%s); nothing was changed%s", domain.ErrInvalid, d, hint)
	}
	return nil
}

func writeServerCopy(ctx context.Context, dst config.ProjectRef, board *domain.Board, opts storage.CreateOptions) (func(), error) {
	repo, err := storage.CreateWith(ctx, dst, board.Settings, opts) // refuses a database that is not empty
	if err != nil {
		return nil, err
	}
	discard := func() {
		dctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = storage.Discard(dctx, dst) // only the tables Create just made
	}
	if err := repo.Save(ctx, board); err != nil {
		discard()
		return nil, err
	}
	if err := verifyCopy(ctx, dst, dst, board); err != nil {
		discard()
		return nil, err
	}
	return discard, nil
}

var shallowHeading = regexp.MustCompile(`(?m)^#{1,3}[ \t]`)

// hasShallowHeading reports whether any card body has a heading a Markdown
// board would read as structure.
func hasShallowHeading(b *domain.Board) bool {
	for _, list := range [][]domain.Card{b.Cards, b.Archived} {
		for _, c := range list {
			if shallowHeading.MatchString(c.Body) {
				return true
			}
		}
	}
	return false
}

// install gives the copy at from its real name at to, without overwriting
// anything: a hard link fails if the name is taken, and the temporary name is
// removed after. A markdown archive follows its board.
func install(from, to config.ProjectRef) error {
	pairs := [][2]string{{from.Path, to.Path}}
	if to.Storage == "markdown" {
		if exists(markdown.ArchiveFile(from.Path)) {
			pairs = append(pairs, [2]string{markdown.ArchiveFile(from.Path), markdown.ArchiveFile(to.Path)})
		}
	} else {
		for _, side := range []string{"-wal", "-shm"} {
			if exists(from.Path + side) {
				pairs = append(pairs, [2]string{from.Path + side, to.Path + side})
			}
		}
	}
	var linked []string
	for _, p := range pairs {
		if err := os.Link(p[0], p[1]); err != nil {
			for _, l := range linked {
				os.Remove(l)
			}
			return fmt.Errorf("give the converted copy its name %s: %w", p[1], err)
		}
		linked = append(linked, p[1])
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

// oldFile is one file of the project being left, and where its backup goes.
type oldFile struct {
	path string
	side string // "-wal" or "-shm" for a SQLite side file, else ""
}

// boardFilesOf lists the files that hold a project now, as they really are on
// disk (symlinks resolved), keeping the ones that exist. The main file comes first.
func boardFilesOf(ref config.ProjectRef) []oldFile {
	var files []oldFile
	if ref.Storage == "markdown" {
		board, archive := markdown.New(ref.Path).Files()
		files = []oldFile{{path: board}, {path: archive}}
	} else {
		real := ref.Path
		if r, err := filepath.EvalSymlinks(ref.Path); err == nil {
			real = r
		}
		files = []oldFile{{path: real}, {path: real + "-wal", side: "-wal"}, {path: real + "-shm", side: "-shm"}}
	}
	var out []oldFile
	for _, f := range files {
		if f.path != "" && exists(f.path) {
			out = append(out, f)
		}
	}
	return out
}

// backUp renames the old project's files to <name>.bak-YYYYMMDD, using -2, -3,
// ... for the whole set when any of those names is taken. A SQLite database's
// -wal and -shm files are named after its backup (X.db.bak-DATE-wal), which is
// where SQLite looks for them, so the backup opens on its own. It returns the
// new names. If a rename fails, the ones done are undone first.
func backUp(ref config.ProjectRef) (moved []string, err error) {
	files := boardFilesOf(ref)
	if len(files) == 0 {
		return nil, nil
	}
	stamp := ".bak-" + now().Format("20060102")
	dest := func(suffix string) []string {
		main := files[0].path + stamp + suffix
		out := make([]string, len(files))
		for i, f := range files {
			if f.side != "" {
				out[i] = main + f.side
			} else if i == 0 {
				out[i] = main
			} else {
				out[i] = f.path + stamp + suffix
			}
		}
		return out
	}
	suffix := ""
	for n := 2; ; n++ {
		taken := false
		for _, d := range dest(suffix) {
			taken = taken || exists(d)
		}
		if !taken {
			break
		}
		suffix = fmt.Sprintf("-%d", n)
	}
	to := dest(suffix)
	for i, f := range files {
		if err := os.Rename(f.path, to[i]); err != nil {
			for j := i - 1; j >= 0; j-- {
				os.Rename(to[j], files[j].path)
			}
			return nil, fmt.Errorf("could not keep the old file %s as %s: %v", f.path, to[i], err)
		}
		moved = append(moved, to[i])
	}
	return moved, nil
}
