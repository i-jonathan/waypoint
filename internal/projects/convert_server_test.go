package projects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"waypoint/internal/config"
	"waypoint/internal/dbconn"
	"waypoint/internal/domain"
	"waypoint/internal/storage"
)

// serverTarget makes an empty database on e's server and returns a conversion
// target for project name, with the password set for that name.
func serverTarget(t *testing.T, e engine, name string) (Target, config.ProjectRef) {
	t.Helper()
	ref := e.fresh(t)
	t.Setenv(dbconn.PasswordVar(name), os.Getenv(dbconn.PasswordVar(ref.Name)))
	return Target{Storage: e.name, Server: &ServerOptions{
		Engine: e.name, Host: ref.Host, Port: ref.Port, User: ref.User, Database: ref.Database,
	}}, ref
}

// addOnServer registers project name on a new database on e's server.
func addOnServer(t *testing.T, e engine, cfgPath string, env func(string) string, name string) (config.ProjectRef, config.ProjectRef) {
	t.Helper()
	target, db := serverTarget(t, e, name)
	ref, err := Add(bg, cfgPath, env, AddOptions{Name: name, Prefix: "T", Server: target.Server})
	if err != nil {
		t.Fatal(err)
	}
	return ref, db
}

// retiredFlag reads the retired mark of the board in ref's database.
func (e engine) retiredFlag(t *testing.T, ref config.ProjectRef) string {
	t.Helper()
	q := "SELECT value FROM waypoint_meta WHERE key = 'retired'"
	var v string
	if err := e.raw(t, ref).QueryRow(q).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestConvertSQLiteToPostgresAndBack(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		fixedDay(t)
		cfgPath, env := sandbox(t)
		dir := t.TempDir()
		_, original := richProject(t, cfgPath, env, dir)
		if _, err := Convert(bg, cfgPath, env, "Rich", Target{Storage: "sqlite"}); err != nil {
			t.Fatal(err)
		}
		target, db := serverTarget(t, e, "Rich")
		res, err := Convert(bg, cfgPath, env, "Rich", target)
		if err != nil {
			t.Fatal(err)
		}
		if res.Ref.Storage != e.name || res.Ref.Path != "" || res.Ref.Database != db.Database {
			t.Fatalf("ref = %+v", res.Ref)
		}
		if !loadRef(t, registered(t, cfgPath, env, "Rich")).Equal(original) {
			t.Fatal("the Postgres copy differs from the original")
		}
		back, err := Convert(bg, cfgPath, env, "Rich", Target{Storage: "sqlite", Path: filepath.Join(dir, "back.db")})
		if err != nil {
			t.Fatal(err)
		}
		if !loadRef(t, back.Ref).Equal(original) {
			t.Fatal("the board that came back differs from the original")
		}
	})
}

func TestConvertMarkdownToPostgresAndBack(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		fixedDay(t)
		cfgPath, env := sandbox(t)
		dir := t.TempDir()
		_, original := richProject(t, cfgPath, env, dir)
		target, _ := serverTarget(t, e, "Rich")
		if _, err := Convert(bg, cfgPath, env, "Rich", target); err != nil {
			t.Fatal(err)
		}
		if !loadRef(t, registered(t, cfgPath, env, "Rich")).Equal(original) {
			t.Fatal("the Postgres copy differs from the original")
		}
		back, err := Convert(bg, cfgPath, env, "Rich", Target{Storage: "markdown", Path: filepath.Join(dir, "again.md")})
		if err != nil {
			t.Fatal(err)
		}
		if !loadRef(t, back.Ref).Equal(original) {
			t.Fatal("the markdown board that came back differs from the original")
		}
	})
}

func TestConvertTargetMustBeEmpty(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		fixedDay(t)
		cfgPath, env := sandbox(t)
		richProject(t, cfgPath, env, t.TempDir())
		cfgBefore, _ := os.ReadFile(cfgPath)

		target, db := serverTarget(t, e, "Rich")
		raw := e.raw(t, db)
		raw.Exec(`CREATE TABLE somebody_elses (id int)`)
		_, err := Convert(bg, cfgPath, env, "Rich", target)
		if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "somebody_elses") {
			t.Fatalf("Convert into a non-empty database = %v", err)
		}
		if n := e.tableCount(t, db); n != 1 {
			t.Fatalf("%d tables after the refusal, want 1", n)
		}

		// A retired Waypoint database is not a live board and not empty: refused too.
		target2, db2 := serverTarget(t, e, "Rich")
		if _, err := Add(bg, cfgPath+".other", env, AddOptions{Name: "Rich", Prefix: "T", Server: target2.Server}); err != nil {
			t.Fatal(err)
		}
		e.markRetired(t, db2)
		if _, err := Convert(bg, cfgPath, env, "Rich", target2); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("Convert into a retired database = %v", err)
		}
		cfgAfter, _ := os.ReadFile(cfgPath)
		if string(cfgBefore) != string(cfgAfter) {
			t.Error("the registry changed")
		}
	})
}

func TestConvertFromServerRetiresAndKeeps(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		cfgPath, env := sandbox(t)
		ref, db := addOnServer(t, e, cfgPath, env, "Pg")
		svcAdd(t, ref, "keep me")
		raw := e.raw(t, db)
		tablesBefore := e.tableCount(t, db)

		dir := t.TempDir()
		res, err := Convert(bg, cfgPath, env, "Pg", Target{Storage: "markdown", Path: filepath.Join(dir, "pg.md")})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Backups) != 0 || res.Retired != ref.Display() {
			t.Fatalf("Backups = %v, Retired = %q", res.Backups, res.Retired)
		}
		var cards int
		raw.QueryRow(`SELECT count(*) FROM cards`).Scan(&cards)
		retired := e.retiredFlag(t, db)
		if cards != 1 || retired != "true" || e.tableCount(t, db) != tablesBefore {
			t.Fatalf("source database: %d cards, retired=%q, %d tables (was %d)", cards, retired, e.tableCount(t, db), tablesBefore)
		}
		if got := loadRef(t, res.Ref); len(got.Cards) != 1 || got.Cards[0].Title != "keep me" {
			t.Fatalf("converted board = %+v", got.Cards)
		}
	})
}

func TestConvertVerifyFailureCleansUp(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		fixedDay(t)
		cfgPath, env := sandbox(t)
		richProject(t, cfgPath, env, t.TempDir())
		cfgBefore, _ := os.ReadFile(cfgPath)
		target, db := serverTarget(t, e, "Rich")

		realOpen := openTarget
		openTarget = func(ref config.ProjectRef) (boardReader, error) {
			r, err := realOpen(ref)
			return dropsACard{r}, err
		}
		t.Cleanup(func() { openTarget = realOpen })

		_, err := Convert(bg, cfgPath, env, "Rich", target)
		if err == nil || !strings.Contains(err.Error(), "did not match") {
			t.Fatalf("Convert = %v, want a verification failure", err)
		}
		if n := e.tableCount(t, db); n != 0 {
			t.Fatalf("%d tables left in the target after a failed conversion", n)
		}
		if cfgAfter, _ := os.ReadFile(cfgPath); string(cfgBefore) != string(cfgAfter) {
			t.Error("the registry changed")
		}
		openTarget = realOpen
		ref := registered(t, cfgPath, env, "Rich")
		ctx, cancel := context.WithTimeout(bg, 5*time.Second)
		defer cancel()
		repo, _ := storage.Open(ref)
		unlock, err := repo.Lock(ctx)
		if err != nil {
			t.Fatalf("the source was left locked: %v", err)
		}
		unlock()
	})
}

// svcAdd adds one card to the board of ref through the repository.
func svcAdd(t *testing.T, ref config.ProjectRef, title string) {
	t.Helper()
	repo, err := storage.Open(ref)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := repo.Load(bg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddCard(domain.NewCard{Title: title}, novAt); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(bg, b); err != nil {
		t.Fatal(err)
	}
}

func TestConvertWritersDuringServerConversions(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		cfgPath, env := sandbox(t)
		dir := t.TempDir()
		if _, err := Add(bg, cfgPath, env, AddOptions{Name: "Busy", Path: filepath.Join(dir, "busy.db"), Prefix: "T"}); err != nil {
			t.Fatal(err)
		}
		w := startWriters(t, cfgPath, env, "Busy", 4) // more starve the SQLite file lock, which polls (a limit older than the server storages)
		time.Sleep(150 * time.Millisecond)
		for round := 0; round < 5; round++ {
			var to Target
			switch round {
			case 0, 2:
				to, _ = serverTarget(t, e, "Busy")
			case 1:
				to = Target{Storage: "markdown", Path: filepath.Join(dir, "r1.md")}
			case 3:
				to = Target{Storage: "sqlite", Path: filepath.Join(dir, "r3.db")}
			default:
				to, _ = serverTarget(t, e, "Busy")
			}
			if _, err := Convert(bg, cfgPath, env, "Busy", to); err != nil {
				t.Fatalf("round %d: %v", round, err)
			}
			time.Sleep(100 * time.Millisecond)
		}
		acked := w.finish()
		if len(acked) < 20 {
			t.Fatalf("only %d writes were acknowledged; the test did not exercise anything", len(acked))
		}
		board := loadRef(t, registered(t, cfgPath, env, "Busy"))
		have := map[string]bool{}
		for _, c := range board.Cards {
			have[c.Title] = true
		}
		var lost []string
		for _, title := range acked {
			if !have[title] {
				lost = append(lost, title)
			}
		}
		if len(lost) > 0 {
			t.Fatalf("%d of %d acknowledged cards are not on the board, for example %v", len(lost), len(acked), lost[:min(5, len(lost))])
		}
	})
}

func TestConvertServerCrashSteps(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		for name, seam := range map[string]*func() error{"after the copy": &afterCopy, "after the registry": &afterRegistry} {
			t.Run(name, func(t *testing.T) {
				fixedDay(t)
				cfgPath, env := sandbox(t)
				_, original := richProject(t, cfgPath, env, t.TempDir())
				target, db := serverTarget(t, e, "Rich")
				old := *seam
				*seam = func() error { return errors.New("power cut") }
				t.Cleanup(func() { *seam = old })

				if _, err := Convert(bg, cfgPath, env, "Rich", target); err == nil {
					t.Fatal("Convert hid the failure")
				}
				ref := registered(t, cfgPath, env, "Rich")
				if got := loadRef(t, ref); !got.Equal(original) {
					t.Fatal("the registered board is not the original")
				}
				if name == "after the copy" && e.tableCount(t, db) != 0 {
					t.Error("the abandoned copy was left in the database")
				}
				if name == "after the registry" && ref.Storage != e.name {
					t.Errorf("the registry should already point at Postgres, has %s", ref.Storage)
				}
			})
		}
	})
}

func TestRetireFailsAfterSwitch(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		cfgPath, env := sandbox(t)
		ref, _ := addOnServer(t, e, cfgPath, env, "Pg")
		old := retireSource
		retireSource = func(ctx context.Context, r retirer) error { return errors.New("connection reset") }
		t.Cleanup(func() { retireSource = old })

		res, err := Convert(bg, cfgPath, env, "Pg", Target{Storage: "markdown", Path: filepath.Join(t.TempDir(), "pg.md")})
		if err == nil {
			t.Fatal("a failed retire was hidden")
		}
		if res.Ref.Storage != "markdown" || registered(t, cfgPath, env, "Pg").Storage != "markdown" {
			t.Fatal("the conversion should count as done")
		}
		for _, want := range []string{ref.Display(), "UPDATE waypoint_meta", "retired"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("message %q lacks %q", err, want)
			}
		}
	})
}

func TestConvertHardBoards(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		cfgPath, env := sandbox(t)
		dir := t.TempDir()
		ref, err := Add(bg, cfgPath, env, AddOptions{Name: "Hard", Path: filepath.Join(dir, "hard.db"), Prefix: "T"})
		if err != nil {
			t.Fatal(err)
		}
		repo, _ := storage.Open(ref)
		b, _, _ := repo.Load(bg)
		big := strings.Repeat("0123456789abcdef", 64*1024) // 1 MiB
		bodies := []string{big, "emoji 🎉👩‍👩‍👧 \"double\" 'single' \\ back\\slash `tick`", "line one\r\nline two\r\n", "日本語 text", ""}
		b.Settings.Columns = append(b.Settings.Columns, domain.Column{Name: "todo"}) // case-only twin of an existing column? Todo/todo
		col := b.Settings.Columns[0].Name
		for i := 1; i <= 6000; i++ {
			c := domain.Card{ID: domain.CardID(fmt.Sprintf("T-%d", i)), Title: fmt.Sprintf("card %d 🎉", i), Column: col, Created: novAt,
				Extra: []domain.Field{{Key: "n", Value: fmt.Sprint(i)}}}
			if i <= len(bodies) {
				c.Body = bodies[i-1]
			}
			b.Cards = append(b.Cards, c)
		}
		b.Settings.NextID = 6001
		if err := repo.Save(bg, b); err != nil {
			t.Fatal(err)
		}
		original := loadRef(t, ref)

		target, _ := serverTarget(t, e, "Hard")
		if _, err := Convert(bg, cfgPath, env, "Hard", target); err != nil {
			t.Fatalf("to Postgres: %v", err)
		}
		if !loadRef(t, registered(t, cfgPath, env, "Hard")).Equal(original) {
			t.Fatal("the Postgres copy differs from the original")
		}
		back, err := Convert(bg, cfgPath, env, "Hard", Target{Storage: "sqlite", Path: filepath.Join(dir, "back.db")})
		if err != nil {
			t.Fatalf("back to SQLite: %v", err)
		}
		got := loadRef(t, back.Ref)
		if !got.Equal(original) || len(got.Cards) != 6000 || len(got.Cards[0].Body) != len(big) {
			t.Fatal("the board that came back differs from the original")
		}
	})
}

func TestConvertNULToServer(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		cfgPath, env := sandbox(t)
		ref, err := Add(bg, cfgPath, env, AddOptions{Name: "Nul", Path: filepath.Join(t.TempDir(), "nul.db"), Prefix: "T"})
		if err != nil {
			t.Fatal(err)
		}
		repo, _ := storage.Open(ref)
		b, _, _ := repo.Load(bg)
		b.AddCard(domain.NewCard{Title: "bad", Body: "has a \x00 nul"}, novAt)
		if err := repo.Save(bg, b); err != nil {
			t.Fatal(err)
		}
		cfgBefore, _ := os.ReadFile(cfgPath)
		target, db := serverTarget(t, e, "Nul")
		_, err = Convert(bg, cfgPath, env, "Nul", target)
		if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "T-1") {
			t.Fatalf("Convert = %v, want ErrInvalid naming T-1", err)
		}
		if n := e.tableCount(t, db); n != 0 {
			t.Fatalf("%d tables left behind", n)
		}
		if cfgAfter, _ := os.ReadFile(cfgPath); string(cfgBefore) != string(cfgAfter) {
			t.Error("the registry changed")
		}
	})
}

func TestConvertPasswordNeverInOutput(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		cfgPath, env := sandbox(t)
		richProject(t, cfgPath, env, t.TempDir())
		target, _ := serverTarget(t, e, "Rich")
		real := os.Getenv(dbconn.PasswordVar("Rich"))
		t.Setenv(dbconn.PasswordVar("Rich"), "s3cr3t-marker")
		_, err := Convert(bg, cfgPath, env, "Rich", target)
		if err == nil {
			t.Fatal("a wrong password should fail")
		}
		if strings.Contains(err.Error(), "s3cr3t-marker") || strings.Contains(err.Error(), real) {
			t.Fatalf("error contains a password: %v", err)
		}
		if !strings.Contains(err.Error(), dbconn.PasswordHelp("Rich")) {
			t.Errorf("error should say how to set the password: %v", err)
		}
		if b, _ := os.ReadFile(cfgPath); strings.Contains(string(b), "s3cr3t-marker") || strings.Contains(string(b), real) {
			t.Error("the registry holds a password")
		}
	})
}

func TestConvertServerToSameDatabaseRefused(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		cfgPath, env := sandbox(t)
		ref, _ := addOnServer(t, e, cfgPath, env, "Pg")
		_, err := Convert(bg, cfgPath, env, "Pg", Target{Storage: e.name, Server: &ServerOptions{
			Engine: e.name, Host: ref.Host, Port: ref.Port, User: ref.User, Database: ref.Database,
		}})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("Convert onto itself = %v", err)
		}
	})
}

func TestConvertWarnsBeforeConnectingToANonLocalServer(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		fixedDay(t)
		cfgPath, env := sandbox(t)
		richProject(t, cfgPath, env, t.TempDir())
		var warned []string
		_, err := Convert(bg, cfgPath, env, "Rich", Target{
			Storage: e.name, Warn: func(s string) { warned = append(warned, s) },
			Server: &ServerOptions{Engine: e.name, Host: "waypoint-test.invalid", User: "u", Database: "d"},
		})
		if err == nil {
			t.Fatal("an unreachable server should fail")
		}
		if len(warned) != 1 || warned[0] != dbconn.NonLocalWarning {
			t.Fatalf("warnings = %q", warned)
		}
	})
}
