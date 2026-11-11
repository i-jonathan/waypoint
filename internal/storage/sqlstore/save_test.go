package sqlstore_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"waypoint/internal/domain"
	"waypoint/internal/storage/sqlite"
	"waypoint/internal/storage/sqlstore"
)

// counting counts the statements Save sends, which over a network is the round trips.
type counting struct {
	sqlstore.Querier
	execs int
}

func (c *counting) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	c.execs++
	return c.Querier.ExecContext(ctx, q, args...)
}

type dialect struct{}

func (dialect) Rebind(q string) string { return q }
func (dialect) ValidText(string) error { return nil }

// A save must not cost a round trip per card: with 300 cards and two extra
// fields each, over a remote server, that was about a thousand sequential
// statements while the board lock was held.
func TestSaveBatchesItsInserts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b.db")
	if _, err := sqlite.Create(path, domain.NewBoard("B", "T").Settings); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	b := domain.NewBoard("B", "T")
	for i := 1; i <= 300; i++ {
		b.Cards = append(b.Cards, domain.Card{
			ID: domain.CardID(fmt.Sprintf("T-%d", i)), Title: fmt.Sprintf("card %d", i), Column: b.Settings.Columns[0].Name,
			Extra: []domain.Field{{Key: "a", Value: "1"}, {Key: "b", Value: "2"}},
		})
	}
	for i := 301; i <= 330; i++ {
		b.Archived = append(b.Archived, domain.Card{ID: domain.CardID(fmt.Sprintf("T-%d", i)), Title: "old", Column: "Done"})
	}
	b.Settings.NextID = 331

	q := &counting{Querier: conn}
	store := sqlstore.Store{D: dialect{}}
	if _, err := store.Save(context.Background(), q, b); err != nil {
		t.Fatal(err)
	}
	if q.execs > 30 {
		t.Fatalf("Save sent %d statements for 330 cards, want a few batches (at most 30)", q.execs)
	}
	got, _, err := store.Load(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Cards) != 300 || len(got.Archived) != 30 {
		t.Fatalf("%d cards, %d archived after the batched save", len(got.Cards), len(got.Archived))
	}
	if !reflect.DeepEqual(got.Cards[299].Extra, b.Cards[299].Extra) || got.Cards[0].ID != "T-1" || got.Cards[299].ID != "T-300" {
		t.Fatalf("order or fields changed: %+v", got.Cards[299])
	}
}
