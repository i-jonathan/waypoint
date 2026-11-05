package projects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"waypoint/internal/config"
	"waypoint/internal/domain"
	"waypoint/internal/storage"
)

var (
	octDay = time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC) // midnight: a date-only value
	novAt  = time.Date(2026, 11, 3, 12, 30, 15, 0, time.UTC)
)

// richProject registers a markdown project holding every concept Waypoint stores
// (groups, a WIP limit, depends, extra fields, date-only and timestamp dates,
// archived cards in two months, a body with a card heading inside a code fence)
// and returns its registry entry and the board as the markdown file now reads.
func richProject(t *testing.T, cfgPath string, env func(string) string, dir string) (config.ProjectRef, *domain.Board) {
	t.Helper()
	ref, err := Add(context.Background(), cfgPath, env, AddOptions{Name: "Rich", Path: filepath.Join(dir, "rich.md"), Prefix: "T", Storage: "markdown"})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := storage.Open(ref)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := repo.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b.Settings.Groups = []domain.Group{{Name: "Platform", Color: "blue"}, {Name: "Docs & help", Color: "amber"}}
	b.Settings.Archive = domain.ArchiveSettings{Mode: domain.ArchiveAuto, AfterDays: 30}
	b.Settings.Appearance = domain.Appearance{Accent: "#7c3aed"}
	b.Settings.Columns[1].WIP = 3
	first, _ := b.AddCard(domain.NewCard{Title: "First", Group: "Platform", Priority: "P0"}, octDay)
	b.AddCard(domain.NewCard{
		Title: "Second", Column: "In progress", Priority: "P1", Depends: []domain.CardID{first.ID},
		Extra: []domain.Field{{Key: "estimate", Value: "small"}},
		Body:  "Plan:\n\n- [ ] one\n- [x] two\n\n```sh\n### T-9 not a card\nmake test\n```\n\n#### Notes\n\nKeep it short.",
	}, novAt)
	b.AddCard(domain.NewCard{Title: "Third", Group: "Docs & help"}, novAt)
	old, _ := b.AddCard(domain.NewCard{Title: "Old news", Body: "Shipped."}, octDay)
	recent, _ := b.AddCard(domain.NewCard{Title: "Recent news"}, octDay)
	for _, step := range []error{
		b.MoveCard(old.ID, "Done", -1, octDay), b.Archive([]domain.CardID{old.ID}, octDay),
		b.MoveCard(recent.ID, "Done", -1, novAt), b.Archive([]domain.CardID{recent.ID}, novAt),
	} {
		if step != nil {
			t.Fatal(step)
		}
	}
	if err := repo.Save(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	b, _, err = repo.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return ref, b
}

func loadRef(t *testing.T, ref config.ProjectRef) *domain.Board {
	t.Helper()
	repo, err := storage.Open(ref)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := repo.Load(context.Background())
	if err != nil {
		t.Fatalf("Load %s: %v", ref.Path, err)
	}
	return b
}

func registered(t *testing.T, cfgPath string, env func(string) string, name string) config.ProjectRef {
	t.Helper()
	cfg, err := config.Load(cfgPath, env)
	if err != nil {
		t.Fatal(err)
	}
	ref, ok := cfg.Find(name)
	if !ok {
		t.Fatalf("project %q is not registered", name)
	}
	return ref
}

func fixedDay(t *testing.T) {
	t.Helper()
	old := now
	now = func() time.Time { return time.Date(2026, 11, 5, 9, 0, 0, 0, time.Local) }
	t.Cleanup(func() { now = old })
}

func TestConvertMarkdownToSQLiteAndBack(t *testing.T) {
	fixedDay(t)
	cfgPath, env := sandbox(t)
	dir := t.TempDir()
	ref, original := richProject(t, cfgPath, env, dir)
	ctx := context.Background()

	res, err := Convert(ctx, cfgPath, env, "Rich", "sqlite", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Ref.Storage != "sqlite" || res.Ref.Path != filepath.Join(dir, "rich.db") {
		t.Fatalf("converted ref = %+v", res.Ref)
	}
	if got := loadRef(t, registered(t, cfgPath, env, "Rich")); !got.Equal(original) {
		t.Fatalf("sqlite board differs from the markdown one:\n got %#v\nwant %#v", got, original)
	}

	back, err := Convert(ctx, cfgPath, env, "Rich", "markdown", filepath.Join(dir, "again.md"))
	if err != nil {
		t.Fatal(err)
	}
	if back.Ref.Storage != "markdown" {
		t.Fatalf("back = %+v", back.Ref)
	}
	if got := loadRef(t, registered(t, cfgPath, env, "Rich")); !got.Equal(original) {
		t.Fatalf("board after the round trip differs:\n got %#v\nwant %#v", got, original)
	}
	_ = ref
}

func TestConvertKeepsBackups(t *testing.T) {
	fixedDay(t)
	cfgPath, env := sandbox(t)
	dir := t.TempDir()
	richProject(t, cfgPath, env, dir)

	res, err := Convert(context.Background(), cfgPath, env, "Rich", "sqlite", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"rich.md.bak-20261105", "rich.archive.md.bak-20261105"} {
		if !exists(filepath.Join(dir, name)) {
			t.Errorf("%s is missing; folder holds %v", name, listing(t, dir))
		}
	}
	if exists(filepath.Join(dir, "rich.md")) || exists(filepath.Join(dir, "rich.archive.md")) {
		t.Errorf("the old files are still under their old names: %v", listing(t, dir))
	}
	if len(res.Backups) != 2 {
		t.Errorf("Backups = %v, want the two old files", res.Backups)
	}
	if registered(t, cfgPath, env, "Rich").Storage != "sqlite" {
		t.Error("registry still says markdown")
	}
}

func listing(t *testing.T, dir string) []string {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".lock") { // a lock file is left behind by design
			out = append(out, e.Name())
		}
	}
	return out
}

func TestConvertSQLiteBackupMovesTheDatabase(t *testing.T) {
	fixedDay(t)
	cfgPath, env := sandbox(t)
	dir := t.TempDir()
	richProject(t, cfgPath, env, dir)
	ctx := context.Background()
	if _, err := Convert(ctx, cfgPath, env, "Rich", "sqlite", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Convert(ctx, cfgPath, env, "Rich", "markdown", filepath.Join(dir, "second.md")); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(dir, "rich.db.bak-20261105")) || exists(filepath.Join(dir, "rich.db")) {
		t.Fatalf("the database was not kept as a backup: %v", listing(t, dir))
	}
}

func TestConvertRefusals(t *testing.T) {
	fixedDay(t)
	dir := t.TempDir()
	for name, c := range map[string]struct {
		to, path string
		prep     func(t *testing.T, dir string)
	}{
		"same storage":         {to: "markdown"},
		"target exists":        {to: "sqlite", path: "taken.db", prep: func(t *testing.T, d string) { os.WriteFile(filepath.Join(d, "taken.db"), []byte("mine"), 0o644) }},
		"target is the source": {to: "sqlite", path: "rich.md"},
		"extension disagrees":  {to: "sqlite", path: "x.md"},
		"relative path":        {to: "sqlite", path: "rel.db"},
		"unknown storage":      {to: "mongo"},
	} {
		t.Run(name, func(t *testing.T) {
			cfgPath, env := sandbox(t)
			d := filepath.Join(dir, strings.ReplaceAll(name, " ", "-"))
			os.MkdirAll(d, 0o755)
			richProject(t, cfgPath, env, d)
			if c.prep != nil {
				c.prep(t, d)
			}
			path := c.path
			if path != "" && name != "relative path" {
				path = filepath.Join(d, path)
			}
			cfgBefore, _ := os.ReadFile(cfgPath)
			filesBefore := listing(t, d)
			_, err := Convert(context.Background(), cfgPath, env, "Rich", c.to, path)
			if !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("Convert = %v, want domain.ErrInvalid", err)
			}
			cfgAfter, _ := os.ReadFile(cfgPath)
			if string(cfgBefore) != string(cfgAfter) {
				t.Error("a refused conversion changed the registry")
			}
			if got := listing(t, d); strings.Join(got, ",") != strings.Join(filesBefore, ",") {
				t.Errorf("a refused conversion changed the folder: %v -> %v", filesBefore, got)
			}
			if name == "target exists" {
				if data, _ := os.ReadFile(filepath.Join(d, "taken.db")); string(data) != "mine" {
					t.Error("the file at the target was touched")
				}
			}
		})
	}
}

func TestConvertToMarkdownRefusesAStrayArchive(t *testing.T) {
	fixedDay(t)
	cfgPath, env := sandbox(t)
	dir := t.TempDir()
	richProject(t, cfgPath, env, dir)
	ctx := context.Background()
	if _, err := Convert(ctx, cfgPath, env, "Rich", "sqlite", ""); err != nil {
		t.Fatal(err)
	}
	// Some other board's archive sits where the new markdown archive would go.
	stray := filepath.Join(dir, "elsewhere.archive.md")
	os.WriteFile(stray, []byte("someone else's cards"), 0o644)
	_, err := Convert(ctx, cfgPath, env, "Rich", "markdown", filepath.Join(dir, "elsewhere.md"))
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Convert = %v, want domain.ErrInvalid", err)
	}
	if data, _ := os.ReadFile(stray); string(data) != "someone else's cards" {
		t.Fatal("the stray archive was touched")
	}
	if exists(filepath.Join(dir, "elsewhere.md")) {
		t.Fatal("a board was created next to the stray archive")
	}
}

func TestConvertBackupNameTaken(t *testing.T) {
	fixedDay(t)
	cfgPath, env := sandbox(t)
	dir := t.TempDir()
	richProject(t, cfgPath, env, dir)
	os.WriteFile(filepath.Join(dir, "rich.md.bak-20261105"), []byte("an older backup"), 0o644)

	res, err := Convert(context.Background(), cfgPath, env, "Rich", "sqlite", "")
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "rich.md.bak-20261105")); string(data) != "an older backup" {
		t.Fatal("an existing backup was overwritten")
	}
	for _, name := range []string{"rich.md.bak-20261105-2", "rich.archive.md.bak-20261105-2"} {
		if !exists(filepath.Join(dir, name)) {
			t.Errorf("%s is missing: %v (backups %v)", name, listing(t, dir), res.Backups)
		}
	}
}

func TestConvertVerifyFailureRollsBack(t *testing.T) {
	fixedDay(t)
	cfgPath, env := sandbox(t)
	dir := t.TempDir()
	richProject(t, cfgPath, env, dir)
	cfgBefore, _ := os.ReadFile(cfgPath)
	filesBefore := listing(t, dir)

	realOpen := openTarget
	openTarget = func(ref config.ProjectRef) (boardReader, error) {
		r, err := realOpen(ref)
		return dropsACard{r}, err
	}
	t.Cleanup(func() { openTarget = realOpen })

	_, err := Convert(context.Background(), cfgPath, env, "Rich", "sqlite", "")
	if err == nil || !strings.Contains(err.Error(), "did not match") {
		t.Fatalf("Convert = %v, want a verification failure", err)
	}
	cfgAfter, _ := os.ReadFile(cfgPath)
	if string(cfgBefore) != string(cfgAfter) {
		t.Error("the registry changed")
	}
	if got := listing(t, dir); strings.Join(got, ",") != strings.Join(filesBefore, ",") {
		t.Errorf("folder changed: %v -> %v", filesBefore, got)
	}
}

type dropsACard struct{ boardReader }

func (d dropsACard) Load(ctx context.Context) (*domain.Board, []domain.CardID, error) {
	b, assigned, err := d.boardReader.Load(ctx)
	if err == nil && len(b.Cards) > 0 {
		b.Cards = b.Cards[1:]
	}
	return b, assigned, err
}

func TestConvertRegistrySaveFailureRestoresTheOldFiles(t *testing.T) {
	fixedDay(t)
	cfgPath, env := sandbox(t)
	dir := t.TempDir()
	richProject(t, cfgPath, env, dir)
	cfgBefore, _ := os.ReadFile(cfgPath)
	filesBefore := listing(t, dir)

	afterBackup = func() error { return errors.New("disk on fire") }
	t.Cleanup(func() { afterBackup = func() error { return nil } })

	_, err := Convert(context.Background(), cfgPath, env, "Rich", "sqlite", "")
	if err == nil || !strings.Contains(err.Error(), "disk on fire") {
		t.Fatalf("Convert = %v", err)
	}
	cfgAfter, _ := os.ReadFile(cfgPath)
	if string(cfgBefore) != string(cfgAfter) {
		t.Error("the registry changed")
	}
	if got := listing(t, dir); strings.Join(got, ",") != strings.Join(filesBefore, ",") {
		t.Errorf("the old files were not put back: %v -> %v", filesBefore, got)
	}
	// And the board still opens where the registry says.
	loadRef(t, registered(t, cfgPath, env, "Rich"))
}

func TestConvertAcrossDirectories(t *testing.T) {
	fixedDay(t)
	cfgPath, env := sandbox(t)
	src, dst := t.TempDir(), t.TempDir()
	_, original := richProject(t, cfgPath, env, src)
	if _, err := Convert(context.Background(), cfgPath, env, "Rich", "sqlite", filepath.Join(dst, "moved.db")); err != nil {
		t.Fatal(err)
	}
	if got := loadRef(t, registered(t, cfgPath, env, "Rich")); !got.Equal(original) {
		t.Fatal("board differs after moving to another folder")
	}
	if !exists(filepath.Join(src, "rich.md.bak-20261105")) {
		t.Fatalf("backup should stay beside the old board: %v", listing(t, src))
	}
}

func TestConvertUnknownProject(t *testing.T) {
	cfgPath, env := sandbox(t)
	if _, err := Convert(context.Background(), cfgPath, env, "Nope", "sqlite", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Convert of an unknown project = %v, want domain.ErrNotFound", err)
	}
}

func TestConvertSQLiteToMarkdownLosslessnessRefusal(t *testing.T) {
	// A card body Markdown cannot hold as written: Convert says so, changes nothing.
	fixedDay(t)
	cfgPath, env := sandbox(t)
	dir := t.TempDir()
	ref, err := Add(context.Background(), cfgPath, env, AddOptions{Name: "Lite", Path: filepath.Join(dir, "lite.db"), Prefix: "T"})
	if err != nil {
		t.Fatal(err)
	}
	repo, _ := storage.Open(ref)
	b, _, _ := repo.Load(context.Background())
	b.AddCard(domain.NewCard{Title: "Heading in body", Body: "## A heading the file format would turn into a column"}, novAt)
	if err := repo.Save(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	cfgBefore, _ := os.ReadFile(cfgPath)
	_, err = Convert(context.Background(), cfgPath, env, "Lite", "markdown", "")
	if err == nil {
		// Markdown saved it faithfully (it demotes the heading itself, and the
		// board matches once saved): that is fine too, as long as nothing was lost.
		if got := loadRef(t, registered(t, cfgPath, env, "Lite")); len(got.Cards) != 1 {
			t.Fatalf("cards after conversion: %d", len(got.Cards))
		}
		return
	}
	if !strings.Contains(err.Error(), "T-1") {
		t.Fatalf("the refusal should name the card: %v", err)
	}
	cfgAfter, _ := os.ReadFile(cfgPath)
	if string(cfgBefore) != string(cfgAfter) || exists(filepath.Join(dir, "lite.md")) {
		t.Fatal("a refused conversion left changes behind")
	}
}
