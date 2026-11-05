package httpapi

import (
	"fmt"
	"net/http"

	"waypoint/internal/app"
	"waypoint/internal/config"
	"waypoint/internal/domain"
)

// boardResponse is a board with the appearance it is shown in. Readonly is
// false for every board that can be read at all: a board with a parse error is
// answered with 423 instead.
type boardResponse struct {
	Board      app.BoardView             `json:"board"`
	Appearance config.ResolvedAppearance `json:"appearance"`
	Readonly   bool                      `json:"readonly"`
}

func (p project) boardResponse(v app.BoardView) boardResponse {
	return boardResponse{Board: v, Appearance: config.Resolve(p.cfg.Appearance, v.Settings.Appearance)}
}

func cardID(r *http.Request) (domain.CardID, error) {
	id := r.PathValue("id")
	if !domain.ValidCardID(id) {
		return "", fmt.Errorf("%w: card %q", domain.ErrNotFound, id)
	}
	return domain.CardID(id), nil
}

func (a *api) getBoard(w http.ResponseWriter, r *http.Request) error {
	p, err := a.openProject(r)
	if err != nil {
		return err
	}
	a.autoArchiveOnce(r, p)
	v, err := p.svc.BoardMatching(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		return err
	}
	a.writeJSON(w, r, http.StatusOK, p.boardResponse(v))
	return nil
}

func (a *api) getArchive(w http.ResponseWriter, r *http.Request) error {
	p, err := a.openProject(r)
	if err != nil {
		return err
	}
	all, err := p.svc.List(r.Context(), r.URL.Query().Get("q"), true)
	if err != nil {
		return err
	}
	out := []app.CardView{}
	for _, c := range all {
		if c.Archived {
			out = append(out, c)
		}
	}
	a.writeJSON(w, r, http.StatusOK, out)
	return nil
}

type fieldIn struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (a *api) createCard(w http.ResponseWriter, r *http.Request) error {
	p, err := a.openProject(r)
	if err != nil {
		return err
	}
	var in struct {
		Title    string          `json:"title"`
		Column   string          `json:"column"`
		Group    string          `json:"group"`
		Priority string          `json:"priority"`
		Body     string          `json:"body"`
		Depends  []domain.CardID `json:"depends"`
		Extra    []fieldIn       `json:"extra"`
	}
	if err := decodeBody(w, r, &in, false); err != nil {
		return err
	}
	n := domain.NewCard{Title: in.Title, Column: in.Column, Group: in.Group, Priority: in.Priority, Body: in.Body, Depends: in.Depends}
	for _, f := range in.Extra {
		n.Extra = append(n.Extra, domain.Field{Key: f.Key, Value: f.Value})
	}
	cv, err := p.svc.CreateCard(r.Context(), n)
	if err != nil {
		return err
	}
	a.changed(p)
	a.writeJSON(w, r, http.StatusCreated, cv)
	return nil
}

func (a *api) patchCard(w http.ResponseWriter, r *http.Request) error {
	p, err := a.openProject(r)
	if err != nil {
		return err
	}
	id, err := cardID(r)
	if err != nil {
		return err
	}
	var in struct {
		Title    *string           `json:"title"`
		Group    *string           `json:"group"`
		Priority *string           `json:"priority"`
		Depends  *[]domain.CardID  `json:"depends"`
		Body     *string           `json:"body"`
		Set      map[string]string `json:"set"`
		IfMatch  string            `json:"if_match"`
	}
	if err := decodeBody(w, r, &in, false); err != nil {
		return err
	}
	cv, err := p.svc.UpdateCard(r.Context(), id, domain.CardPatch{
		Title: in.Title, Group: in.Group, Priority: in.Priority, Body: in.Body, Depends: in.Depends, Set: in.Set,
	}, in.IfMatch)
	if err != nil {
		return err
	}
	a.changed(p)
	a.writeJSON(w, r, http.StatusOK, cv)
	return nil
}

func (a *api) moveCard(w http.ResponseWriter, r *http.Request) error {
	p, err := a.openProject(r)
	if err != nil {
		return err
	}
	id, err := cardID(r)
	if err != nil {
		return err
	}
	var in struct {
		Column string `json:"column"`
		Index  *int   `json:"index"` // missing means the bottom
	}
	if err := decodeBody(w, r, &in, false); err != nil {
		return err
	}
	index := -1
	if in.Index != nil {
		index = *in.Index
	}
	cv, err := p.svc.MoveCard(r.Context(), id, in.Column, index)
	if err != nil {
		return err
	}
	a.changed(p)
	a.writeJSON(w, r, http.StatusOK, cv)
	return nil
}

func (a *api) checkItem(w http.ResponseWriter, r *http.Request) error {
	p, err := a.openProject(r)
	if err != nil {
		return err
	}
	id, err := cardID(r)
	if err != nil {
		return err
	}
	var in struct {
		N       int    `json:"n"`
		IfMatch string `json:"if_match"`
	}
	if err := decodeBody(w, r, &in, false); err != nil {
		return err
	}
	cv, err := p.svc.CheckItem(r.Context(), id, in.N, in.IfMatch)
	if err != nil {
		return err
	}
	a.changed(p)
	a.writeJSON(w, r, http.StatusOK, cv)
	return nil
}

func (a *api) restoreCard(w http.ResponseWriter, r *http.Request) error {
	p, err := a.openProject(r)
	if err != nil {
		return err
	}
	id, err := cardID(r)
	if err != nil {
		return err
	}
	var in struct {
		Column string `json:"column"`
	}
	if err := decodeBody(w, r, &in, true); err != nil {
		return err
	}
	cv, err := p.svc.Restore(r.Context(), id, in.Column)
	if err != nil {
		return err
	}
	a.changed(p)
	a.writeJSON(w, r, http.StatusOK, cv)
	return nil
}

func (a *api) deleteCard(w http.ResponseWriter, r *http.Request) error {
	p, err := a.openProject(r)
	if err != nil {
		return err
	}
	id, err := cardID(r)
	if err != nil {
		return err
	}
	var in struct {
		IfMatch string `json:"if_match"`
	}
	if err := decodeBody(w, r, &in, true); err != nil {
		return err
	}
	if err := p.svc.DeleteCard(r.Context(), id, in.IfMatch); err != nil {
		return err
	}
	a.changed(p)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *api) archiveDone(w http.ResponseWriter, r *http.Request) error {
	p, err := a.openProject(r)
	if err != nil {
		return err
	}
	ids, err := p.svc.ArchiveDone(r.Context())
	if err != nil {
		return err
	}
	a.changed(p)
	a.writeJSON(w, r, http.StatusOK, struct {
		Archived []domain.CardID `json:"archived"`
	}{ids})
	return nil
}

// putProjectSettings replaces a board's settings. if_match is required: the
// settings screen must say which settings it was editing, so that it cannot
// overwrite a change made since.
func (a *api) putProjectSettings(w http.ResponseWriter, r *http.Request) error {
	p, err := a.openProject(r)
	if err != nil {
		return err
	}
	var in struct {
		Settings        domain.Settings   `json:"settings"`
		ColumnRenames   map[string]string `json:"column_renames"`
		GroupRenames    map[string]string `json:"group_renames"`
		PriorityRenames map[string]string `json:"priority_renames"`
		IfMatch         string            `json:"if_match"`
	}
	if err := decodeBody(w, r, &in, false); err != nil {
		return err
	}
	if in.IfMatch == "" {
		return invalidRequest("if_match is required: send the settings_hash of the settings you edited")
	}
	v, err := p.svc.UpdateSettings(r.Context(), domain.SettingsChange{
		Settings:        in.Settings,
		ColumnRenames:   in.ColumnRenames,
		GroupRenames:    in.GroupRenames,
		PriorityRenames: in.PriorityRenames,
	}, in.IfMatch)
	if err != nil {
		return err
	}
	a.changed(p)
	a.writeJSON(w, r, http.StatusOK, p.boardResponse(v))
	return nil
}
