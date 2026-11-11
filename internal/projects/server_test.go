package projects

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"waypoint/internal/config"
	"waypoint/internal/dbconn"
	"waypoint/internal/domain"
	"waypoint/internal/storage"
)

var bg = context.Background()

func optsFor(ref config.ProjectRef) *ServerOptions {
	return &ServerOptions{Engine: ref.Storage, Host: ref.Host, Port: ref.Port, User: ref.User, Database: ref.Database}
}

func serverAdd(ref config.ProjectRef, mutate ...func(*AddOptions)) AddOptions {
	o := AddOptions{Name: ref.Name, Prefix: "T", Server: optsFor(ref)}
	for _, m := range mutate {
		m(&o)
	}
	return o
}

func configBytes(t *testing.T, cfgPath string) string {
	t.Helper()
	data, _ := os.ReadFile(cfgPath)
	return string(data)
}

func TestAddServerNewBoard(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		ref := e.fresh(t)
		cfgPath, env := sandbox(t)
		out, err := Add(bg, cfgPath, env, serverAdd(ref))
		if err != nil {
			t.Fatal(err)
		}
		if out.Path != "" || out.Storage != e.name || out.Host != ref.Host || out.SSL == "" || out.Port != ref.Port {
			t.Fatalf("ref = %+v", out)
		}
		if strings.Contains(configBytes(t, cfgPath), dbconn.Password(os.Getenv, ref.Name)) {
			t.Fatal("the config holds the password")
		}
		if strings.Contains(configBytes(t, cfgPath), "path:") {
			t.Fatal("the config holds a path for a server project")
		}
		repo, err := storage.Open(out)
		if err != nil {
			t.Fatal(err)
		}
		if b, _, err := repo.Load(bg); err != nil || b.Settings.Name != ref.Name {
			t.Fatalf("Load = %v, %v", b, err)
		}
	})
}

func TestAddAdoptsExistingBoard(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		ref := e.fresh(t)
		cfgPath, env := sandbox(t)
		if _, err := Add(bg, cfgPath, env, serverAdd(ref)); err != nil {
			t.Fatal(err)
		}
		repo, _ := storage.Open(ref)
		b, _, _ := repo.Load(bg)
		b.Cards = []domain.Card{{ID: "T-1", Title: "keep me", Column: b.Settings.Columns[0].Name}}
		b.Settings.NextID = 2
		if err := repo.Save(bg, b); err != nil {
			t.Fatal(err)
		}
		// rm keeps the data; add on the same database adopts the board.
		if err := config.Update(cfgPath, env, func(c *config.Config) error { return c.Remove(ref.Name) }); err != nil {
			t.Fatal(err)
		}
		out, err := Add(bg, cfgPath, env, serverAdd(ref, func(o *AddOptions) { o.Prefix = "ZZ" }))
		if err != nil {
			t.Fatal(err)
		}
		again, _ := storage.Open(out)
		got, _, err := again.Load(bg)
		if err != nil || len(got.Cards) != 1 || got.Cards[0].Title != "keep me" || got.Settings.Prefix != "T" {
			t.Fatalf("adopted board = %+v, %v", got, err)
		}
	})
}

func TestAddRefusesForeignDatabase(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		ref := e.fresh(t)
		if _, err := e.raw(t, ref).Exec(`CREATE TABLE somebody_elses (id int)`); err != nil {
			t.Fatal(err)
		}
		cfgPath, env := sandbox(t)
		_, err := Add(bg, cfgPath, env, serverAdd(ref))
		if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "somebody_elses") {
			t.Fatalf("Add = %v, want ErrInvalid naming the table", err)
		}
		if _, statErr := os.Stat(cfgPath); statErr == nil {
			t.Fatal("a refused Add wrote the config")
		}
		if n := e.tableCount(t, ref); n != 1 {
			t.Fatalf("%d tables after a refused Add, want 1", n)
		}
	})
}

func TestAddRefusesRetired(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		ref := e.fresh(t)
		cfgPath, env := sandbox(t)
		if _, err := Add(bg, cfgPath, env, serverAdd(ref)); err != nil {
			t.Fatal(err)
		}
		e.markRetired(t, ref)
		if err := config.Update(cfgPath, env, func(c *config.Config) error { return c.Remove(ref.Name) }); err != nil {
			t.Fatal(err)
		}
		_, err := Add(bg, cfgPath, env, serverAdd(ref))
		if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "waypoint_meta") {
			t.Fatalf("Add = %v, want the retired refusal", err)
		}
	})
}

func TestAddMissingDatabaseSuggestsCreate(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		ref := e.fresh(t)
		ref.Database = ref.Database + "_missing"
		cfgPath, env := sandbox(t)
		_, err := Add(bg, cfgPath, env, serverAdd(ref))
		if err == nil || !strings.Contains(err.Error(), "--create-database") {
			t.Fatalf("Add = %v, want a hint about --create-database", err)
		}
	})
}

func TestAddUnreachableDoesNotRegister(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		cfgPath, env := sandbox(t)
		ref := config.ProjectRef{Name: "nowhere", Storage: e.name, Host: "127.0.0.1", Port: 1, User: "u", Database: "d"}
		start := time.Now()
		_, err := Add(bg, cfgPath, env, serverAdd(ref))
		if time.Since(start) > 12*time.Second {
			t.Fatalf("Add took %v", time.Since(start))
		}
		if !errors.Is(err, dbconn.ErrUnreachable) || !strings.Contains(err.Error(), "127.0.0.1") || !strings.Contains(err.Error(), ":1") {
			t.Fatalf("Add = %v, want unreachable naming host and port", err)
		}
		if _, statErr := os.Stat(cfgPath); statErr == nil {
			t.Fatal("an unreachable server was registered")
		}
	})
}

func TestAddMissingPasswordMessage(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		ref := e.fresh(t)
		t.Setenv(dbconn.PasswordVar(ref.Name), "")
		t.Setenv(dbconn.GlobalPasswordVar, "")
		cfgPath, env := sandbox(t)
		_, err := Add(bg, cfgPath, env, serverAdd(ref))
		if !errors.Is(err, dbconn.ErrPassword) || !strings.Contains(err.Error(), dbconn.PasswordHelp(ref.Name)) {
			t.Fatalf("Add = %v, want the password help sentence", err)
		}
	})
}

// second makes a project ref for another empty database on the same server.
func second(t *testing.T, e engine, first config.ProjectRef, name string) config.ProjectRef {
	t.Helper()
	pw := os.Getenv(dbconn.PasswordVar(first.Name))
	other := e.fresh(t) // resets the servertest password variable to the same value
	other.Name = name
	t.Setenv(dbconn.PasswordVar(first.Name), pw)
	return other
}

func TestGlobalPasswordServesTwoProjects(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		a := e.fresh(t)
		pw := os.Getenv(dbconn.PasswordVar(a.Name))
		b := second(t, e, a, "other")
		t.Setenv(dbconn.PasswordVar(a.Name), "")
		t.Setenv(dbconn.GlobalPasswordVar, pw)
		cfgPath, env := sandbox(t)
		for _, ref := range []config.ProjectRef{a, b} {
			if _, err := Add(bg, cfgPath, env, serverAdd(ref)); err != nil {
				t.Fatalf("Add %s with only the global password set: %v", ref.Name, err)
			}
		}
	})
}

func TestProjectPasswordOverridesGlobal(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		ref := e.fresh(t) // per-project variable holds the right password
		t.Setenv(dbconn.GlobalPasswordVar, "wrong-global")
		cfgPath, env := sandbox(t)
		if _, err := Add(bg, cfgPath, env, serverAdd(ref)); err != nil {
			t.Fatalf("Add with a right per-project and a wrong global password: %v", err)
		}
	})
}

func TestAddWarnsOnceNonLocal(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		cfgPath, env := sandbox(t)
		var warned []string
		remote := config.ProjectRef{Name: "remote", Storage: e.name, Host: "waypoint-test.invalid", Port: 5432, User: "u", Database: "d"}
		Add(bg, cfgPath, env, serverAdd(remote, func(o *AddOptions) { o.Warn = func(s string) { warned = append(warned, s) } }))
		if len(warned) != 1 || warned[0] != dbconn.NonLocalWarning {
			t.Fatalf("warnings = %q, want exactly the non-local warning", warned)
		}
		warned = nil
		local := config.ProjectRef{Name: "local", Storage: e.name, Host: "localhost", Port: 1, User: "u", Database: "d"}
		Add(bg, cfgPath, env, serverAdd(local, func(o *AddOptions) { o.Warn = func(s string) { warned = append(warned, s) } }))
		if len(warned) != 0 {
			t.Fatalf("a local server warned: %q", warned)
		}
	})
}

func TestDisableSSLNonLocalNote(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		cfgPath, env := sandbox(t)
		var warned []string
		remote := config.ProjectRef{Name: "remote", Storage: e.name, Host: "waypoint-test.invalid", Port: 5432, User: "u", Database: "d"}
		Add(bg, cfgPath, env, serverAdd(remote, func(o *AddOptions) {
			o.Server.SSL = "disable"
			o.Warn = func(s string) { warned = append(warned, s) }
		}))
		if len(warned) != 2 || !strings.Contains(warned[1], "unencrypted") {
			t.Fatalf("warnings = %q, want the warning and an unencrypted-password note", warned)
		}
	})
}

func TestAddRefusesCollidingPasswordVar(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		ref := e.fresh(t)
		ref.Name = "a-b"
		t.Setenv(dbconn.GlobalPasswordVar, os.Getenv(dbconn.PasswordVar("servertest")))
		cfgPath, env := sandbox(t)
		if _, err := Add(bg, cfgPath, env, serverAdd(ref)); err != nil {
			t.Fatal(err)
		}
		clash := ref
		clash.Name = "a_b"
		clash.Database += "_x"
		_, err := Add(bg, cfgPath, env, serverAdd(clash))
		if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "WAYPOINT_DB_PASSWORD_A_B") {
			t.Fatalf("Add = %v, want the variable collision", err)
		}
		_, err = Add(bg, cfgPath, env, serverAdd(config.ProjectRef{Name: "日本語", Storage: e.name, Host: "localhost", Port: 1, User: "u", Database: "d"}))
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("a name that gives no variable should be refused: %v", err)
		}
	})
}

func TestAddRefusesSameDatabaseTwice(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		ref := e.fresh(t)
		cfgPath, env := sandbox(t)
		if _, err := Add(bg, cfgPath, env, serverAdd(ref)); err != nil {
			t.Fatal(err)
		}
		dup := ref
		dup.Name = "twin"
		t.Setenv(dbconn.GlobalPasswordVar, os.Getenv(dbconn.PasswordVar(ref.Name)))
		_, err := Add(bg, cfgPath, env, serverAdd(dup))
		if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), ref.Name) {
			t.Fatalf("Add = %v, want a refusal naming %q", err, ref.Name)
		}
	})
}

func TestAddServerOptionDefaults(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		cfgPath, env := sandbox(t)
		_, err := Add(bg, cfgPath, env, AddOptions{Name: "My Proj", Prefix: "T", Server: &ServerOptions{Engine: e.name, User: ""}})
		if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "user") {
			t.Fatalf("Add without a user = %v", err)
		}
		if got := defaultDatabase("My Proj"); got != "my-proj" {
			t.Fatalf("default database = %q", got)
		}
	})
}
