package app_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"waypoint/internal/app"
	"waypoint/internal/domain"
)

func TestCardViewJSONShape(t *testing.T) {
	cv := app.CardView{
		Card: domain.Card{
			ID: "T-2", Title: "Second", Column: "Todo", Group: "g", Priority: "P1",
			Depends: []domain.CardID{"T-1"},
			Created: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
			Body:    "- [x] a\n",
			Extra:   []domain.Field{{Key: "owner", Value: "me"}, {Key: "area", Value: "ui"}},
		},
		Blockers: []domain.Blocker{{ID: "T-1"}, {ID: "T-99", Missing: true}},
		Next:     true,
		Hash:     "abc",
	}
	cv.Checklist.Done, cv.Checklist.Total = 1, 1
	got, err := json.Marshal(cv)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"T-2","title":"Second","column":"Todo","group":"g","priority":"P1","depends":["T-1"],` +
		`"created":"2026-11-01","done":null,"body":"- [x] a\n","extra":[{"key":"owner","value":"me"},{"key":"area","value":"ui"}],` +
		`"archived":false,"blockers":[{"id":"T-1","missing":false},{"id":"T-99","missing":true}],"next":true,` +
		`"checklist":{"done":1,"total":1},"hash":"abc"}`
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestCardViewJSONEmptyListsAreArrays(t *testing.T) {
	got, err := json.Marshal(app.CardView{Card: domain.Card{ID: "T-1", Title: "x", Column: "Todo"}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"T-1","title":"x","column":"Todo","group":"","priority":"","depends":[],"created":null,"done":null,` +
		`"body":"","extra":[],"archived":false,"blockers":[],"next":false,"checklist":{"done":0,"total":0},"hash":""}`
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestCardViewJSONInSlice(t *testing.T) {
	got, err := json.Marshal([]app.CardView{{Card: domain.Card{ID: "T-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	var back []map[string]any
	if err := json.Unmarshal(got, &back); err != nil || len(back) != 1 || back[0]["id"] != "T-1" {
		t.Fatalf("slice of CardView did not marshal as objects: %s (%v)", got, err)
	}
}

func TestCardViewJSONKeepsAngleBracketsReadable(t *testing.T) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(app.CardView{Card: domain.Card{ID: "T-1", Title: "a < b & c"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"a < b & c"`) {
		t.Fatalf("title was escaped: %s", buf.String())
	}
}
