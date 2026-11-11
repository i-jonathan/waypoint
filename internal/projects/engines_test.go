package projects

import (
	"database/sql"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"waypoint/internal/config"
	"waypoint/internal/dbconn"
	"waypoint/internal/storage/servertest"
)

// engine is one database server kind the server tests run against. A test that
// needs a server for one engine skips when that engine's server is not set up.
type engine struct {
	name  string
	fresh func(*testing.T) config.ProjectRef
	open  func(*testing.T, config.ProjectRef) *sql.DB
	// retireSQL marks a board retired, and tables counts what the schema holds.
	retireSQL string
	tablesSQL string
	// killSQL ends every other connection to the database.
	killSQL string
}

var engines = []engine{
	{
		name: "postgres", fresh: servertest.Postgres, open: openPostgres,
		retireSQL: `UPDATE waypoint_meta SET value = 'true' WHERE key = 'retired'`,
		tablesSQL: `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND c.relkind IN ('r','p','v','m','f','S')`,
		killSQL:   `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = current_database() AND pid <> pg_backend_pid()`,
	},
}

// eachEngine runs a test once per engine, as a subtest named for it.
func eachEngine(t *testing.T, fn func(t *testing.T, e engine)) {
	for _, e := range engines {
		t.Run(e.name, func(t *testing.T) { fn(t, e) })
	}
}

func openPostgres(t *testing.T, ref config.ProjectRef) *sql.DB {
	t.Helper()
	cfg, err := pgx.ParseConfig("")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Host, cfg.Port, cfg.User, cfg.Database = ref.Host, uint16(ref.Port), ref.User, ref.Database
	cfg.Password = dbconn.Password(os.Getenv, ref.Name)
	db := sql.OpenDB(stdlib.GetConnector(*cfg))
	t.Cleanup(func() { db.Close() })
	return db
}

// raw is a plain connection to ref's database.
func (e engine) raw(t *testing.T, ref config.ProjectRef) *sql.DB { return e.open(t, ref) }

// markRetired flags the board in ref's database as retired.
func (e engine) markRetired(t *testing.T, ref config.ProjectRef) {
	t.Helper()
	if _, err := e.raw(t, ref).Exec(e.retireSQL); err != nil {
		t.Fatal(err)
	}
}

// tableCount counts the tables, views and sequences in ref's database.
func (e engine) tableCount(t *testing.T, ref config.ProjectRef) int {
	t.Helper()
	var n int
	if err := e.raw(t, ref).QueryRow(e.tablesSQL).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// killConnections ends every other connection to ref's database, as a network
// failure or a server restart would.
func (e engine) killConnections(t *testing.T, ref config.ProjectRef) {
	t.Helper()
	if _, err := e.raw(t, ref).Exec(e.killSQL); err != nil {
		t.Fatal(err)
	}
}
