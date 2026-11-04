package domain

import (
	"slices"
	"testing"
)

// filterBoard returns a 4-card board in rank order:
//
//	T-020 Write docs   Todo         Docs           P0  (next up)
//	T-018 Rotate keys  Todo         Security hard  P0  depends on T-019 (blocked)
//	T-019 Audit logs   In progress  Security       P1
//	T-021 Ship it      Done
func filterBoard() *Board {
	b := NewBoard("x", "T")
	b.Settings.Groups = []Group{{Name: "Docs"}, {Name: "Security hard"}, {Name: "Security"}}
	b.Cards = []Card{
		{ID: "T-020", Title: "Write docs", Column: "Todo", Group: "Docs", Priority: "P0",
			Body:  "Mention http://example.test/x and the Vault.\n",
			Extra: []Field{{"owner", "Alice Smith"}}},
		{ID: "T-018", Title: "Rotate keys", Column: "Todo", Group: "Security hard", Priority: "P0",
			Depends: []CardID{"T-019"}, Body: "touches vault, ratio 3:4\n",
			Extra: []Field{{"owner", "Bob"}, {"sprint", "12"}}},
		{ID: "T-019", Title: "Audit logs", Column: "In progress", Group: "Security", Priority: "P1",
			Extra: []Field{{"owner", "Bob"}}},
		{ID: "T-021", Title: "Ship it", Column: "Done", Done: today},
	}
	return b
}

func TestFilterMatch(t *testing.T) {
	tests := []struct {
		name, query string
		want        []CardID
	}{
		{"empty matches all", "", []CardID{"T-020", "T-018", "T-019", "T-021"}},
		{"blank matches all", "  \t ", []CardID{"T-020", "T-018", "T-019", "T-021"}},
		{"priority", "p0", []CardID{"T-020", "T-018"}},
		{"priority exact case", "P1", []CardID{"T-019"}},
		{"p is free text, not a priority", "p", []CardID{"T-020", "T-021"}},
		{"blocked", "blocked", []CardID{"T-018"}},
		{"blocked ignores case", "BLOCKED", []CardID{"T-018"}},
		{"next", "next", []CardID{"T-020"}},
		{"priority and blocked", "p0 blocked", []CardID{"T-018"}},
		{"priority and next", "p0 next", []CardID{"T-020"}},
		{"blocked and next", "blocked next", nil},
		{"group contains", "group:sec", []CardID{"T-018", "T-019"}},
		{"group quoted value", `group:"security hard"`, []CardID{"T-018"}},
		{"group ignores case", "GROUP:DOCS", []CardID{"T-020"}},
		{"column contains", "column:progress", []CardID{"T-019"}},
		{"column todo", "column:todo", []CardID{"T-020", "T-018"}},
		{"extra field", "owner:bob", []CardID{"T-018", "T-019"}},
		{"extra field contains", "owner:ali", []CardID{"T-020"}},
		{"extra field quoted value", `owner:"alice smith"`, []CardID{"T-020"}},
		{"extra field missing key", "sprint:12 owner:bob", []CardID{"T-018"}},
		{"extra field wrong value", "owner:carol", nil},
		{"unknown extra key", "nokey:x", nil},
		{"id substring", "t-019", []CardID{"T-019"}},
		{"id prefix", "t-0", []CardID{"T-020", "T-018", "T-019", "T-021"}},
		{"title substring", "rotate", []CardID{"T-018"}},
		{"body substring", "vault", []CardID{"T-020", "T-018"}},
		{"quoted phrase", `"write docs"`, []CardID{"T-020"}},
		{"quoted phrase in body", `"touches vault"`, []CardID{"T-018"}},
		{"unquoted words are anded", "touches keys", []CardID{"T-018"}},
		{"no match", "zzz", nil},
		{"empty value is free text", "owner:", nil},
		{"invalid key is free text", "3:4", []CardID{"T-018"}},
		{"url is free text", "http://example", []CardID{"T-020"}},
		{"url with path is free text", "http://example.test/x", []CardID{"T-020"}},
		{"uppercase key is case-insensitive", "Owner:bob", []CardID{"T-018", "T-019"}},
		{"unterminated quote runs to end", `"write docs`, []CardID{"T-020"}},
		{"unterminated quoted value", `group:"security hard`, []CardID{"T-018"}},
		{"lone quote matches all", `"`, []CardID{"T-020", "T-018", "T-019", "T-021"}},
		{"empty quotes match all", `""`, []CardID{"T-020", "T-018", "T-019", "T-021"}},
	}
	b := filterBoard()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := ParseFilter(tc.query)
			var got []CardID
			for _, c := range b.Cards {
				if f.Match(b, c) {
					got = append(got, c.ID)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("ParseFilter(%q) matched %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

func TestFilterEmpty(t *testing.T) {
	assertTrue(t, ParseFilter("").Empty())
	assertTrue(t, ParseFilter("   ").Empty())
	assertTrue(t, ParseFilter(`""`).Empty())
	assertTrue(t, !ParseFilter("p0").Empty())
	assertTrue(t, !ParseFilter(`"a b"`).Empty())
}

func TestFilterPriorityWinsOverKeyword(t *testing.T) {
	b := filterBoard()
	b.Settings.Priorities = []string{"next", "P1"}
	b.Cards[2].Priority = "next"
	f := ParseFilter("next")
	var got []CardID
	for _, c := range b.Cards {
		if f.Match(b, c) {
			got = append(got, c.ID)
		}
	}
	assertEqual(t, got, []CardID{"T-019"})
}

func TestFilterReadsTheBoardPassedIn(t *testing.T) {
	b := filterBoard()
	f := ParseFilter("blocked")
	assertTrue(t, f.Match(b, b.Cards[1]))
	b.Cards[2].Column = "Done" // T-019 done: T-018 no longer blocked
	assertTrue(t, !f.Match(b, b.Cards[1]))
}
