// Package servertest gives tests a fresh, empty database on a real Postgres or
// MySQL server, or skips them when no server is configured. The servers come
// from the environment: WAYPOINT_TEST_POSTGRES and WAYPOINT_TEST_MYSQL, each
// "host:port:user:password". `make test-servers` starts both in Docker.
package servertest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"waypoint/internal/config"
	"waypoint/internal/dbconn"
)

// ProjectName is the name every returned ProjectRef has.
const ProjectName = "servertest"

type conn struct {
	host     string
	port     int
	user     string
	password string
}

func lookup(name string) (conn, bool, error) {
	v := os.Getenv(name)
	if v == "" {
		return conn{}, false, nil
	}
	parts := strings.SplitN(v, ":", 4)
	if len(parts) != 4 {
		return conn{}, false, fmt.Errorf("%s must be host:port:user:password", name)
	}
	port, err := strconv.Atoi(parts[1])
	if err != nil || port < 1 || port > 65535 {
		return conn{}, false, fmt.Errorf("%s: bad port %q", name, parts[1])
	}
	return conn{parts[0], port, parts[2], parts[3]}, true, nil
}

func dbName() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "waypoint_test_" + hex.EncodeToString(b)
}

// Docker reports whether the docker command can reach a daemon.
func Docker(t *testing.T) bool {
	t.Helper()
	return exec.Command("docker", "info").Run() == nil
}

func setup(t *testing.T, env, engine string) (config.ProjectRef, conn) {
	t.Helper()
	c, ok, err := lookup(env)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Skipf("%s is not set; skipping the %s server test", env, engine)
	}
	ref := config.ProjectRef{
		Name: ProjectName, Storage: engine, Host: c.host, Port: c.port,
		Database: dbName(), User: c.user,
	}
	t.Setenv(dbconn.PasswordVar(ProjectName), c.password)
	return ref, c
}

// Postgres returns a project ref for a new empty Postgres database, dropped at
// the end of the test, or skips the test when WAYPOINT_TEST_POSTGRES is unset.
func Postgres(t *testing.T) config.ProjectRef {
	t.Helper()
	ref, c := setup(t, "WAYPOINT_TEST_POSTGRES", "postgres")
	db := openPostgres(t, c, "postgres")
	defer db.Close()
	exec1(t, db, `CREATE DATABASE "`+ref.Database+`"`)
	t.Cleanup(func() {
		db := openPostgres(t, c, "postgres")
		defer db.Close()
		_, _ = db.Exec(`DROP DATABASE IF EXISTS "` + ref.Database + `" WITH (FORCE)`)
	})
	return ref
}

// MySQL is Postgres for MySQL (WAYPOINT_TEST_MYSQL).
func MySQL(t *testing.T) config.ProjectRef {
	t.Helper()
	ref, c := setup(t, "WAYPOINT_TEST_MYSQL", "mysql")
	db := openMySQL(t, c)
	defer db.Close()
	exec1(t, db, "CREATE DATABASE `"+ref.Database+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin")
	t.Cleanup(func() {
		db := openMySQL(t, c)
		defer db.Close()
		_, _ = db.Exec("DROP DATABASE IF EXISTS `" + ref.Database + "`")
	})
	return ref
}

func exec1(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, q); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func openPostgres(t *testing.T, c conn, database string) *sql.DB {
	t.Helper()
	cfg, err := pgx.ParseConfig("")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database = c.host, uint16(c.port), c.user, c.password, database
	return sql.OpenDB(stdlib.GetConnector(*cfg))
}

func openMySQL(t *testing.T, c conn) *sql.DB {
	t.Helper()
	cfg := mysql.NewConfig()
	cfg.User, cfg.Passwd, cfg.Net = c.user, c.password, "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", c.host, c.port)
	cfg.Timeout = 10 * time.Second
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return sql.OpenDB(connector)
}
