package mysql

import (
	"context"
	"embed"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"waypoint/internal/domain"
	"waypoint/internal/storage/sqlstore"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// waypointTables are the tables a Waypoint board owns, in the order they are
// dropped.
var waypointTables = []string{"card_fields", "cards", "board_groups", "board_columns", "project", "waypoint_meta"}

type migration struct {
	version    int
	statements []string
}

// migrations are the embedded files in version order. A file named NNN_name.sql
// takes the schema to version NNN. Statements are split on ";", so a migration
// must not hold a semicolon inside a string.
func migrations() ([]migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		n, _, _ := strings.Cut(e.Name(), "_")
		v, err := strconv.Atoi(n)
		if err != nil {
			return nil, fmt.Errorf("migration %s has no version number", e.Name())
		}
		data, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, err
		}
		var stmts []string
		for _, s := range strings.Split(string(data), ";") {
			if s = strings.TrimSpace(s); s != "" {
				stmts = append(stmts, s)
			}
		}
		out = append(out, migration{v, stmts})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// latest is the schema version this build writes.
func latest() int {
	ms, err := migrations()
	if err != nil || len(ms) == 0 {
		return 0
	}
	return ms[len(ms)-1].version
}

// afterFirstTable is a test seam: Create calls it after its first statement.
var afterFirstTable = func() error { return nil }

var createTable = regexp.MustCompile(`(?i)^CREATE TABLE (\w+)`)

// migrate creates the schema on an empty database and records the version.
// MySQL cannot roll back DDL, so a failure drops the tables this call made,
// by name; the caller has already proved the schema was empty, so none of them
// can be somebody else's.
func migrate(ctx context.Context, q sqlstore.Querier) (err error) {
	ms, err := migrations()
	if err != nil {
		return err
	}
	var made []string
	defer func() {
		if err == nil {
			return
		}
		for i := len(made) - 1; i >= 0; i-- {
			_, _ = q.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+made[i])
		}
	}()
	first := true
	for _, m := range ms {
		for _, s := range m.statements {
			if _, err := q.ExecContext(ctx, s); err != nil {
				return fmt.Errorf("migration %d: %w", m.version, err)
			}
			if t := createTable.FindStringSubmatch(s); t != nil {
				made = append(made, t[1])
			}
			if first {
				first = false
				if err := afterFirstTable(); err != nil {
					return err
				}
			}
		}
	}
	for _, kv := range [][2]string{{"schema_version", strconv.Itoa(latest())}, {"retired", "false"}} {
		if _, err := q.ExecContext(ctx, "INSERT INTO waypoint_meta (`key`, value) VALUES (?, ?)", kv[0], kv[1]); err != nil {
			return err
		}
	}
	return nil
}

// RetireStatement is the SQL that marks a board retired, and (with 'false') brings it back.
const retireSQL = "UPDATE waypoint_meta SET value = 'false' WHERE `key` = 'retired'"

// errRetired is the error for a database a conversion has retired: its project
// now lives elsewhere.
func errRetired() error {
	return fmt.Errorf("%w: this database was converted to another storage and retired, and the project now lives elsewhere; run the command again. To use this database as a board again, run: %s", domain.ErrInvalid, retireSQL)
}

// relations lists the tables and views in the current database.
func relations(ctx context.Context, q sqlstore.Querier) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT table_name AS name FROM information_schema.tables WHERE table_schema = DATABASE() ORDER BY table_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// validate proves the database is a Waypoint board this build can use, reading
// only.
func validate(ctx context.Context, q sqlstore.Querier) error {
	rels, err := relations(ctx, q)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, r := range rels {
		have[r] = true
	}
	own := map[string]bool{}
	for _, t := range waypointTables {
		own[t] = true
	}
	var foreign []string
	for _, r := range rels {
		if !own[r] {
			foreign = append(foreign, r)
		}
	}
	missing := 0
	for _, t := range waypointTables {
		if !have[t] {
			missing++
		}
	}
	switch {
	case len(rels) == 0 || missing == len(waypointTables):
		return fmt.Errorf("%w: this database is not a Waypoint board", domain.ErrInvalid)
	case len(foreign) > 0:
		return fmt.Errorf("%w: this database also holds %s, which Waypoint did not make; a board needs a database of its own", domain.ErrInvalid, foreign[0])
	case missing > 0:
		return fmt.Errorf("%w: this Waypoint database is missing tables; it is damaged", domain.ErrInvalid)
	}
	meta := map[string]string{}
	rows, err := q.QueryContext(ctx, "SELECT `key`, value FROM waypoint_meta")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return err
		}
		meta[k] = v
	}
	if err := rows.Err(); err != nil {
		return err
	}
	version, err := strconv.Atoi(meta["schema_version"])
	switch {
	case err != nil:
		return fmt.Errorf("%w: this Waypoint database has no readable schema version", domain.ErrInvalid)
	case version > latest():
		return fmt.Errorf("%w: this database was made by a newer Waypoint (schema %d, this build knows %d)", domain.ErrInvalid, version, latest())
	case meta["retired"] == "true":
		return errRetired()
	}
	return nil
}
