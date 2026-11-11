// Package sqlstore turns a domain.Board into rows and back, over database/sql.
// It holds nothing specific to one database engine: the engine's package
// supplies the driver, the connection, the transaction and the schema, and a
// Dialect for the few spelling differences. Transactions belong to the caller,
// since how to begin one (and how to lock) differs between engines.
package sqlstore

import (
	"context"
	"database/sql"
)

// Querier is what a *sql.DB, *sql.Tx and *sql.Conn all offer.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Dialect covers how an engine spells things. Statements in this package are
// written with ? placeholders.
type Dialect interface {
	Rebind(query string) string
	// ValidText says whether the engine can store s as text. The error says
	// why not, without naming where s came from. Save calls it on every
	// string it binds.
	ValidText(s string) error
}

// Store reads and writes boards through a Dialect.
type Store struct{ D Dialect }
