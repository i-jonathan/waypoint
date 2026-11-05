package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"waypoint/internal/domain"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type migration struct {
	version int
	sql     string
}

// migrations are the embedded files in version order. A file named
// NNN_name.sql takes the database to user_version NNN.
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
		out = append(out, migration{v, string(data)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// migrate brings a database Waypoint made up to the newest schema, in one
// transaction. Only Create and Open of a validated file call it.
func migrate(ctx context.Context, db *sql.DB) error {
	ms, err := migrations()
	if err != nil {
		return err
	}
	return inTx(ctx, db, true, func(q execer) error {
		var cur int
		if err := q.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&cur); err != nil {
			return err
		}
		for _, m := range ms {
			if m.version <= cur {
				continue
			}
			if _, err := q.ExecContext(ctx, m.sql); err != nil {
				return fmt.Errorf("migration %d: %w", m.version, err)
			}
			if _, err := q.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, m.version)); err != nil {
				return err
			}
		}
		return nil
	})
}

// latest is the schema version this build writes.
func latest() int {
	ms, err := migrations()
	if err != nil || len(ms) == 0 {
		return 0
	}
	return ms[len(ms)-1].version
}

// validate proves db is a Waypoint database this build can use, reading only.
func validate(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("%w: not a readable SQLite database: %v", domain.ErrInvalid, err)
	}
	var tables int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'project'`).Scan(&tables); err != nil {
		return fmt.Errorf("%w: not a readable SQLite database: %v", domain.ErrInvalid, err)
	}
	switch {
	case version == 0 || tables == 0:
		return fmt.Errorf("%w: this file is not a Waypoint database", domain.ErrInvalid)
	case version > latest():
		return fmt.Errorf("%w: this database was made by a newer Waypoint (schema %d, this build knows %d)", domain.ErrInvalid, version, latest())
	}
	return nil
}
