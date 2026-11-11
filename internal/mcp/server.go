// Package mcp serves a board to agents over the Model Context Protocol. Its
// seven tools are thin adapters over app.Service and return the same JSON as
// the CLI's --json output. A rule the board enforces (a refused move, a WIP
// limit, an unknown card or column, a stale if_match) comes back as a tool
// error with the domain message, not as a protocol error, so the agent can
// read it and adjust.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"waypoint/internal/app"
	"waypoint/internal/domain"
	"waypoint/internal/errcode"
)

// New returns an MCP server whose tools act on the board behind svc.
func New(svc *app.Service) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "waypoint", Version: "1"}, nil)
	a := &adapter{svc: svc}
	sdk.AddTool(s, &sdk.Tool{
		Name:        "list_cards",
		Description: "List board cards as JSON, optionally filtered. query uses the board search syntax (for example \"auth priority:P1\"). Prefer the waypoint CLI if you have a shell.",
	}, a.listCards)
	sdk.AddTool(s, &sdk.Tool{
		Name:        "get_card",
		Description: "Get one card, with its body, by id. Prefer the waypoint CLI (waypoint show) if you have a shell.",
	}, a.getCard)
	sdk.AddTool(s, &sdk.Tool{
		Name:        "create_card",
		Description: "Add a card at the bottom of a column (the first column by default). Prefer the waypoint CLI (waypoint add) if you have a shell.",
	}, a.createCard)
	sdk.AddTool(s, &sdk.Tool{
		Name:        "move_card",
		Description: "Move a card to a column, at the bottom unless index is given. The board's move rules and WIP limits apply. Prefer the waypoint CLI (waypoint move) if you have a shell.",
	}, a.moveCard)
	sdk.AddTool(s, &sdk.Tool{
		Name:        "update_card",
		Description: "Change a card's title, group, priority, depends, body or extra fields. Pass the hash from an earlier read as if_match to refuse the change if the card has moved on. Prefer the waypoint CLI (waypoint set) if you have a shell.",
	}, a.updateCard)
	sdk.AddTool(s, &sdk.Tool{
		Name:        "next_up",
		Description: "The card to work on next, as a JSON list (empty if none). Prefer the waypoint CLI (waypoint ls --next) if you have a shell.",
	}, a.nextUp)
	sdk.AddTool(s, &sdk.Tool{
		Name:        "list_blocked",
		Description: "Cards blocked by unfinished dependencies, as JSON. Prefer the waypoint CLI (waypoint ls --blocked) if you have a shell.",
	}, a.listBlocked)
	return s
}

type adapter struct{ svc *app.Service }

// result is what every handler returns: a tool result and no structured
// output. The text is the CLI's --json output for the same operation.
type result = *sdk.CallToolResult

func ok(v any) (result, any, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fail(err)
	}
	return text(strings.TrimRight(buf.String(), "\n"), false), nil, nil
}

// fail reports err as a tool error: {"error","code"} as the CLI prints it, plus
// "current" (the card as it is now) for a stale if_match.
func fail(err error) (result, any, error) {
	out := struct {
		Error   string        `json:"error"`
		Code    string        `json:"code"`
		Current *app.CardView `json:"current,omitempty"`
	}{Error: err.Error(), Code: errcode.Code(err)}
	var ce *app.ConflictError
	if errors.As(err, &ce) {
		out.Current = &ce.Current
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.Encode(out)
	return text(strings.TrimRight(buf.String(), "\n"), true), nil, nil
}

func text(s string, isErr bool) result {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: s}}, IsError: isErr}
}

type listCardsIn struct {
	Column  string `json:"column,omitempty" jsonschema:"only cards in this column"`
	Group   string `json:"group,omitempty" jsonschema:"only cards in this group"`
	Blocked bool   `json:"blocked,omitempty" jsonschema:"only blocked cards"`
	Query   string `json:"query,omitempty" jsonschema:"board search text"`
}

// quote wraps s in quotes so a value with spaces stays one search term.
func quote(s string) string { return `"` + strings.ReplaceAll(s, `"`, "") + `"` }

func (a *adapter) list(ctx context.Context, query string) (result, any, error) {
	cards, err := a.svc.List(ctx, query, false)
	if err != nil {
		return fail(err)
	}
	return ok(cards)
}

func (a *adapter) listCards(ctx context.Context, _ *sdk.CallToolRequest, in listCardsIn) (result, any, error) {
	var terms []string
	if q := strings.TrimSpace(in.Query); q != "" {
		terms = append(terms, q)
	}
	if in.Column != "" {
		terms = append(terms, "column:"+quote(in.Column))
	}
	if in.Group != "" {
		terms = append(terms, "group:"+quote(in.Group))
	}
	if in.Blocked {
		terms = append(terms, "blocked")
	}
	return a.list(ctx, strings.Join(terms, " "))
}

func (a *adapter) nextUp(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (result, any, error) {
	return a.list(ctx, "next")
}

func (a *adapter) listBlocked(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (result, any, error) {
	return a.list(ctx, "blocked")
}

type idIn struct {
	ID string `json:"id" jsonschema:"card id, for example T-12"`
}

func (a *adapter) getCard(ctx context.Context, _ *sdk.CallToolRequest, in idIn) (result, any, error) {
	cv, err := a.svc.Get(ctx, domain.CardID(in.ID))
	if err != nil {
		return fail(err)
	}
	return ok(cv)
}

type createCardIn struct {
	Title    string   `json:"title" jsonschema:"card title"`
	Column   string   `json:"column,omitempty" jsonschema:"column name; the first column by default"`
	Group    string   `json:"group,omitempty" jsonschema:"an existing group"`
	Priority string   `json:"priority,omitempty" jsonschema:"an existing priority"`
	Depends  []string `json:"depends,omitempty" jsonschema:"ids of cards this one waits for"`
	Body     string   `json:"body,omitempty" jsonschema:"markdown body"`
}

func (a *adapter) createCard(ctx context.Context, _ *sdk.CallToolRequest, in createCardIn) (result, any, error) {
	col, err := a.svc.ResolveColumn(ctx, in.Column)
	if err != nil {
		return fail(err)
	}
	n := domain.NewCard{Title: in.Title, Column: col, Group: in.Group, Priority: in.Priority, Body: in.Body}
	n.Depends = ids(in.Depends)
	cv, err := a.svc.CreateCard(ctx, n)
	if err != nil {
		return fail(err)
	}
	return ok(cv)
}

type moveCardIn struct {
	idIn
	Column string `json:"column" jsonschema:"destination column"`
	Index  *int   `json:"index,omitempty" jsonschema:"position in the column from 0; bottom by default"`
}

func (a *adapter) moveCard(ctx context.Context, _ *sdk.CallToolRequest, in moveCardIn) (result, any, error) {
	col, err := a.svc.ResolveColumn(ctx, in.Column)
	if err != nil {
		return fail(err)
	}
	index := -1
	if in.Index != nil {
		if *in.Index < 0 {
			return fail(fmt.Errorf("%w: index must be a number from 0", domain.ErrInvalid))
		}
		index = *in.Index
	}
	cv, err := a.svc.MoveCard(ctx, domain.CardID(in.ID), col, index)
	if err != nil {
		return fail(err)
	}
	return ok(cv)
}

type updateCardIn struct {
	idIn
	Title    *string           `json:"title,omitempty"`
	Group    *string           `json:"group,omitempty" jsonschema:"empty clears it"`
	Priority *string           `json:"priority,omitempty" jsonschema:"empty clears it"`
	Body     *string           `json:"body,omitempty"`
	Depends  *[]string         `json:"depends,omitempty" jsonschema:"replaces the list; [] clears it"`
	Extra    map[string]string `json:"extra,omitempty" jsonschema:"other fields by key; an empty value deletes the key"`
	IfMatch  string            `json:"if_match,omitempty" jsonschema:"hash from an earlier read; the change is refused if the card differs"`
}

func (a *adapter) updateCard(ctx context.Context, _ *sdk.CallToolRequest, in updateCardIn) (result, any, error) {
	p := domain.CardPatch{Title: in.Title, Group: in.Group, Priority: in.Priority, Body: in.Body}
	if in.Depends != nil {
		d := ids(*in.Depends)
		p.Depends = &d
	}
	for k, v := range in.Extra {
		switch k {
		case "done", "created", "column", "rank", "id":
			return fail(fmt.Errorf("%w: %s cannot be set here; use move_card for column and position, and ids and dates are fixed", domain.ErrInvalid, k))
		}
		if p.Set == nil {
			p.Set = map[string]string{}
		}
		p.Set[k] = v
	}
	cv, err := a.svc.UpdateCard(ctx, domain.CardID(in.ID), p, in.IfMatch)
	if err != nil {
		return fail(err)
	}
	return ok(cv)
}

// ids converts strings to card ids, trimmed, dropping empty ones. Never nil.
func ids(in []string) []domain.CardID {
	out := []domain.CardID{}
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, domain.CardID(s))
		}
	}
	return out
}
