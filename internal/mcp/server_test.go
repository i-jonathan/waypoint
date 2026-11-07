package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"waypoint/internal/app"
	"waypoint/internal/domain"
	"waypoint/internal/storage/markdown"
	"waypoint/internal/storage/sqlite"
)

func now() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) }

func markdownService(t *testing.T) *app.Service {
	t.Helper()
	repo, err := markdown.Create(filepath.Join(t.TempDir(), "BOARD.md"), domain.NewBoard("Test", "T").Settings)
	if err != nil {
		t.Fatal(err)
	}
	return app.NewService(repo, now)
}

func sqliteService(t *testing.T) *app.Service {
	t.Helper()
	repo, err := sqlite.Create(filepath.Join(t.TempDir(), "waypoint.db"), domain.NewBoard("Test", "T").Settings)
	if err != nil {
		t.Fatal(err)
	}
	return app.NewService(repo, now)
}

// connect serves svc over an in-memory pair and returns the client session.
func connect(t *testing.T, svc *app.Service) *sdk.ClientSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st, ct := sdk.NewInMemoryTransports()
	ss, err := New(svc).Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// call runs a tool and returns its text and whether it was a tool error. A
// protocol error fails the test.
func call(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("%s: want one content item, got %d", name, len(res.Content))
	}
	return res.Content[0].(*sdk.TextContent).Text, res.IsError
}

func TestToolsListed(t *testing.T) {
	cs := connect(t, markdownService(t))
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tool := range res.Tools {
		got = append(got, tool.Name)
		if tool.Description == "" {
			t.Errorf("%s has no description", tool.Name)
		}
	}
	slices.Sort(got)
	want := []string{"create_card", "get_card", "list_blocked", "list_cards", "move_card", "next_up", "update_card"}
	if !slices.Equal(got, want) {
		t.Fatalf("tools = %v, want %v", got, want)
	}
}

func TestCreateThenMove(t *testing.T) {
	for name, mk := range map[string]func(*testing.T) *app.Service{"markdown": markdownService, "sqlite": sqliteService} {
		t.Run(name, func(t *testing.T) {
			cs := connect(t, mk(t))
			text, isErr := call(t, cs, "create_card", map[string]any{"title": "First", "priority": "P1", "body": "hello"})
			if isErr {
				t.Fatalf("create: %s", text)
			}
			var card struct {
				ID, Title, Column, Priority, Body, Hash string
			}
			if err := json.Unmarshal([]byte(text), &card); err != nil {
				t.Fatalf("create output is not a card: %v: %s", err, text)
			}
			if card.ID != "T-1" || card.Title != "First" || card.Column != "Todo" || card.Priority != "P1" {
				t.Fatalf("created card = %+v", card)
			}
			text, isErr = call(t, cs, "move_card", map[string]any{"id": "T-1", "column": "in progress"})
			if isErr || !strings.Contains(text, `"column":"In progress"`) {
				t.Fatalf("move (isErr=%v): %s", isErr, text)
			}
			text, _ = call(t, cs, "get_card", map[string]any{"id": "T-1"})
			if !strings.Contains(text, `"column":"In progress"`) || !strings.Contains(text, `"body":"hello"`) {
				t.Fatalf("get: %s", text)
			}
			text, _ = call(t, cs, "list_cards", map[string]any{"column": "In progress"})
			if !strings.HasPrefix(text, "[") || !strings.Contains(text, `"id":"T-1"`) {
				t.Fatalf("list: %s", text)
			}
			text, _ = call(t, cs, "list_cards", map[string]any{"column": "Todo"})
			if text != "[]" {
				t.Fatalf("list Todo = %s", text)
			}
			text, _ = call(t, cs, "next_up", nil)
			if !strings.Contains(text, `"next":true`) {
				t.Fatalf("next_up: %s", text)
			}
		})
	}
}

func TestListBlocked(t *testing.T) {
	cs := connect(t, sqliteService(t))
	call(t, cs, "create_card", map[string]any{"title": "A"})
	call(t, cs, "create_card", map[string]any{"title": "B", "depends": []string{"T-1"}})
	text, isErr := call(t, cs, "list_blocked", nil)
	if isErr || !strings.Contains(text, `{"id":"T-2","title"`) || strings.Contains(text, `{"id":"T-1","title"`) {
		t.Fatalf("list_blocked (isErr=%v): %s", isErr, text)
	}
}

func TestMoveRefusedByRules(t *testing.T) {
	cs := connect(t, markdownService(t))
	call(t, cs, "create_card", map[string]any{"title": "A", "column": "Done"})
	text, isErr := call(t, cs, "move_card", map[string]any{"id": "T-1", "column": "Todo"})
	if !isErr {
		t.Fatalf("want a tool error, got %s", text)
	}
	assertContains(t, text, domain.ErrMoveNotAllowed.Error())
	assertContains(t, text, `"code"`)
}

func TestUpdateConflict(t *testing.T) {
	cs := connect(t, sqliteService(t))
	text, _ := call(t, cs, "create_card", map[string]any{"title": "A"})
	var card struct{ Hash string }
	json.Unmarshal([]byte(text), &card)

	text, isErr := call(t, cs, "update_card", map[string]any{"id": "T-1", "title": "B", "if_match": card.Hash})
	if isErr || !strings.Contains(text, `"title":"B"`) {
		t.Fatalf("update (isErr=%v): %s", isErr, text)
	}
	text, isErr = call(t, cs, "update_card", map[string]any{"id": "T-1", "title": "C", "if_match": card.Hash})
	if !isErr {
		t.Fatalf("stale if_match should be a tool error, got %s", text)
	}
	assertContains(t, text, "changed since it was read")
	assertContains(t, text, `"title":"B"`) // the current card
	assertContains(t, text, `"hash"`)

	text, _ = call(t, cs, "update_card", map[string]any{"id": "T-1", "extra": map[string]string{"owner": "sam"}, "depends": []string{}})
	assertContains(t, text, `"key":"owner"`)
}

func TestUnknownCardAndColumnAreToolErrors(t *testing.T) {
	cs := connect(t, markdownService(t))
	for name, args := range map[string]map[string]any{
		"get_card":    {"id": "T-99"},
		"move_card":   {"id": "T-99", "column": "Done"},
		"update_card": {"id": "T-99", "title": "x"},
	} {
		text, isErr := call(t, cs, name, args)
		if !isErr {
			t.Errorf("%s: want a tool error, got %s", name, text)
		}
		assertContains(t, text, "not found")
	}
	call(t, cs, "create_card", map[string]any{"title": "A"})
	text, isErr := call(t, cs, "move_card", map[string]any{"id": "T-1", "column": "Nowhere"})
	if !isErr {
		t.Fatalf("unknown column: want a tool error, got %s", text)
	}
	assertContains(t, text, "unknown column")
}

func assertContains(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Fatalf("%q does not contain %q", s, sub)
	}
}
