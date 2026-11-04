package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"waypoint/internal/app"
	"waypoint/internal/domain"
)

const (
	usageList    = "waypoint ls [QUERY...] [--column X] [--group G] [--blocked] [--next] [--archived] [--json]"
	usageShow    = "waypoint show ID [--json]"
	usageNew     = `waypoint add TITLE [--column X] [--group G] [--priority P] [--depends T-1,T-2] [--body TEXT|-] [--json]`
	usageMove    = "waypoint move ID COLUMN [--top | --index N] [--json]"
	usageSet     = "waypoint set ID key=value... [--if-match HASH] [--json]"
	usageCheck   = "waypoint check ID N [--json]"
	usageDone    = "waypoint done ID [--json]"
	usageArchive = "waypoint archive [--json]"
	usageRestore = "waypoint restore ID [--column X] [--json]"
	usageDelete  = "waypoint rm ID [--json]"
)

// usageErr is a mistake in how a command was called, found while building its
// request. cmd methods turn it into a usage error (exit 2).
type usageErr string

func (e usageErr) Error() string { return string(e) }

// failCmd reports err: as a usage error if it is one, else as a failure.
func (r *runner) failCmd(err error, usage string) int {
	var ue usageErr
	if errors.As(err, &ue) {
		return r.usageError(string(ue), usage)
	}
	return r.fail(err)
}

// readStdin returns all of standard input. It can be called once per command.
func (r *runner) readStdin() (string, error) {
	if r.stdinRead {
		return "", usageErr("standard input can only be read once per command")
	}
	r.stdinRead = true
	if r.env.Stdin == nil {
		return "", nil
	}
	data, err := io.ReadAll(r.env.Stdin)
	if err != nil {
		return "", fmt.Errorf("reading standard input: %w", err)
	}
	return string(data), nil
}

// bodyText is v, or all of standard input when v is "-".
func (r *runner) bodyText(v string) (string, error) {
	if v == "-" {
		return r.readStdin()
	}
	return v, nil
}

// splitList splits a comma list, trims each entry and drops empty ones. The
// result is never nil.
func splitList(s string) []domain.CardID {
	ids := []domain.CardID{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			ids = append(ids, domain.CardID(p))
		}
	}
	return ids
}

// oneID checks that exactly one card ID was given.
func oneID(pos []string) (domain.CardID, error) {
	if len(pos) != 1 {
		return "", usageErr("expected one card ID")
	}
	return domain.CardID(pos[0]), nil
}

// resolveColumn maps what the user typed to a column of the board: an exact
// name first, then a case-insensitive match that must be unique. An empty
// name stays empty (the caller's default). Anything else is ErrUnknownColumn.
func resolveColumn(ctx context.Context, svc *app.Service, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	bv, err := svc.Board(ctx)
	if err != nil {
		return "", err
	}
	var folded []string
	for _, c := range bv.Settings.Columns {
		if c.Name == name {
			return c.Name, nil
		}
		if strings.EqualFold(c.Name, name) {
			folded = append(folded, c.Name)
		}
	}
	switch len(folded) {
	case 1:
		return folded[0], nil
	case 0:
		return "", fmt.Errorf("%w: %q", domain.ErrUnknownColumn, name)
	}
	return "", fmt.Errorf("%w: %q matches %s", domain.ErrUnknownColumn, name, strings.Join(folded, ", "))
}

// quoteTerm wraps s in quotes when it holds whitespace, so a query term with
// spaces stays one term. Quote characters inside s are dropped.
func quoteTerm(s string) string {
	if !strings.ContainsAny(s, " \t\r\n") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, "") + `"`
}

func (r *runner) cmdLs(args []string) int {
	var column, group string
	var blocked, next, archived bool
	pos, code, done := r.parse(usageList, args, func(fs *flag.FlagSet) {
		fs.StringVar(&column, "column", "", "")
		fs.StringVar(&group, "group", "", "")
		fs.BoolVar(&blocked, "blocked", false, "")
		fs.BoolVar(&next, "next", false, "")
		fs.BoolVar(&archived, "archived", false, "")
	})
	if done {
		return code
	}
	terms := make([]string, 0, len(pos)+4)
	for _, p := range pos {
		terms = append(terms, quoteTerm(p))
	}
	if column != "" {
		terms = append(terms, "column:"+quoteTermValue(column))
	}
	if group != "" {
		terms = append(terms, "group:"+quoteTermValue(group))
	}
	if blocked {
		terms = append(terms, "blocked")
	}
	if next {
		terms = append(terms, "next")
	}
	query := strings.Join(terms, " ")

	svc, _, err := r.service(r.project)
	if err != nil {
		return r.fail(err)
	}
	cards, err := svc.List(r.ctx, query, archived)
	if err != nil {
		return r.fail(err)
	}
	if r.json {
		r.printJSON(cards)
	} else if len(cards) > 0 {
		writeCardLines(r.env.Stdout, cards)
	} else {
		text := noMatchText
		if query == "" {
			// Nothing matched an empty query: say so only if the board, archive
			// included, really holds no cards.
			if all, err := svc.List(r.ctx, "", true); err == nil && len(all) == 0 {
				text = emptyBoardText
			}
		}
		fmt.Fprint(r.env.Stdout, text)
	}
	r.warnAboutBoard(svc)
	return exitOK
}

// quoteTermValue always quotes a flag value, so "In progress" is one value.
func quoteTermValue(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, "") + `"`
}

func (r *runner) cmdShow(args []string) int {
	pos, code, done := r.parse(usageShow, args, nil)
	if done {
		return code
	}
	id, err := oneID(pos)
	if err != nil {
		return r.failCmd(err, usageShow)
	}
	svc, _, err := r.service(r.project)
	if err != nil {
		return r.fail(err)
	}
	cv, err := svc.Get(r.ctx, id)
	if err != nil {
		return r.fail(err)
	}
	if r.json {
		r.printJSON(cv)
	} else {
		writeCardDetail(r.env.Stdout, cv)
	}
	return exitOK
}

func (r *runner) cmdAdd(args []string) int {
	var column, group, priority, depends, body string
	pos, code, done := r.parse(usageNew, args, func(fs *flag.FlagSet) {
		fs.StringVar(&column, "column", "", "")
		fs.StringVar(&group, "group", "", "")
		fs.StringVar(&priority, "priority", "", "")
		fs.StringVar(&depends, "depends", "", "")
		fs.StringVar(&body, "body", "", "")
	})
	if done {
		return code
	}
	if len(pos) != 1 {
		return r.usageError("expected one TITLE (quote it if it has spaces)", usageNew)
	}
	text, err := r.bodyText(body)
	if err != nil {
		return r.failCmd(err, usageNew)
	}
	svc, _, err := r.service(r.project)
	if err != nil {
		return r.fail(err)
	}
	col, err := resolveColumn(r.ctx, svc, column)
	if err != nil {
		return r.fail(err)
	}
	n := domain.NewCard{Title: pos[0], Column: col, Group: group, Priority: priority, Body: text}
	if depends != "" {
		n.Depends = splitList(depends)
	}
	cv, err := svc.CreateCard(r.ctx, n)
	if err != nil {
		return r.fail(err)
	}
	return r.wrote(svc, cv, fmt.Sprintf("Created %s in %s", cv.ID, cv.Column))
}

func (r *runner) cmdMove(args []string) int {
	var top bool
	index, indexSet := -1, false
	pos, code, done := r.parse(usageMove, args, func(fs *flag.FlagSet) {
		fs.BoolVar(&top, "top", false, "")
		fs.Func("index", "", func(s string) error {
			n, err := strconv.Atoi(s)
			if err != nil || n < 0 {
				return errors.New("must be a number from 0")
			}
			index, indexSet = n, true
			return nil
		})
	})
	if done {
		return code
	}
	if len(pos) != 2 {
		return r.usageError("expected a card ID and a COLUMN", usageMove)
	}
	if top && indexSet {
		return r.usageError("--top and --index cannot be used together", usageMove)
	}
	if top {
		index = 0
	}
	svc, _, err := r.service(r.project)
	if err != nil {
		return r.fail(err)
	}
	col, err := resolveColumn(r.ctx, svc, pos[1])
	if err != nil {
		return r.fail(err)
	}
	cv, err := svc.MoveCard(r.ctx, domain.CardID(pos[0]), col, index)
	if err != nil {
		return r.fail(err)
	}
	return r.wrote(svc, cv, fmt.Sprintf("Moved %s to %s", cv.ID, cv.Column))
}

func (r *runner) cmdSet(args []string) int {
	var ifMatch string
	pos, code, done := r.parse(usageSet, args, func(fs *flag.FlagSet) {
		fs.StringVar(&ifMatch, "if-match", "", "")
	})
	if done {
		return code
	}
	if len(pos) < 2 {
		return r.usageError("expected a card ID and at least one key=value", usageSet)
	}
	patch, err := r.buildPatch(pos[1:])
	if err != nil {
		return r.failCmd(err, usageSet)
	}
	svc, _, err := r.service(r.project)
	if err != nil {
		return r.fail(err)
	}
	cv, err := svc.UpdateCard(r.ctx, domain.CardID(pos[0]), patch, ifMatch)
	if err != nil {
		return r.fail(err)
	}
	return r.wrote(svc, cv, "Updated "+string(cv.ID))
}

// buildPatch turns key=value arguments, in order, into one CardPatch. A later
// argument for the same key replaces an earlier one.
func (r *runner) buildPatch(assignments []string) (domain.CardPatch, error) {
	var p domain.CardPatch
	for _, a := range assignments {
		key, value, ok := strings.Cut(a, "=")
		if !ok {
			return p, usageErr(fmt.Sprintf("%q is not key=value", a))
		}
		switch key {
		case "title":
			p.Title = &value
		case "group":
			p.Group = &value
		case "priority":
			p.Priority = &value
		case "depends":
			ids := splitList(value)
			p.Depends = &ids
		case "body":
			text, err := r.bodyText(value)
			if err != nil {
				return p, err
			}
			p.Body = &text
		case "done":
			return p, fmt.Errorf("%w: done is set by moving the card", domain.ErrInvalid)
		case "created":
			return p, fmt.Errorf("%w: created is set when the card is added", domain.ErrInvalid)
		default:
			if p.Set == nil {
				p.Set = map[string]string{}
			}
			p.Set[key] = value
		}
	}
	return p, nil
}

func (r *runner) cmdCheck(args []string) int {
	pos, code, done := r.parse(usageCheck, args, nil)
	if done {
		return code
	}
	if len(pos) != 2 {
		return r.usageError("expected a card ID and an item number N", usageCheck)
	}
	n, err := strconv.Atoi(pos[1])
	if err != nil || n < 1 {
		return r.usageError(fmt.Sprintf("N must be a positive integer, got %q", pos[1]), usageCheck)
	}
	svc, _, err := r.service(r.project)
	if err != nil {
		return r.fail(err)
	}
	cv, err := svc.CheckItem(r.ctx, domain.CardID(pos[0]), n)
	if err != nil {
		return r.fail(err)
	}
	verb := "Unchecked"
	if items := domain.Checklist(cv.Body); n <= len(items) && items[n-1].Checked {
		verb = "Checked"
	}
	return r.wrote(svc, cv, fmt.Sprintf("%s item %d of %s", verb, n, cv.ID))
}

func (r *runner) cmdDone(args []string) int {
	pos, code, done := r.parse(usageDone, args, nil)
	if done {
		return code
	}
	id, err := oneID(pos)
	if err != nil {
		return r.failCmd(err, usageDone)
	}
	svc, _, err := r.service(r.project)
	if err != nil {
		return r.fail(err)
	}
	cv, err := svc.Done(r.ctx, id)
	if err != nil {
		return r.fail(err)
	}
	return r.wrote(svc, cv, fmt.Sprintf("Moved %s to %s", cv.ID, cv.Column))
}

func (r *runner) cmdArchive(args []string) int {
	pos, code, done := r.parse(usageArchive, args, nil)
	if done {
		return code
	}
	if len(pos) != 0 {
		return r.usageError(fmt.Sprintf("unexpected argument %q", pos[0]), usageArchive)
	}
	svc, _, err := r.service(r.project)
	if err != nil {
		return r.fail(err)
	}
	ids, err := svc.ArchiveDone(r.ctx)
	if err != nil {
		return r.fail(err)
	}
	if r.json {
		out := make([]string, len(ids))
		for i, id := range ids {
			out[i] = string(id)
		}
		r.printJSON(struct {
			Archived []string `json:"archived"`
		}{out})
	} else {
		switch len(ids) {
		case 0:
			fmt.Fprintln(r.env.Stdout, "Nothing to archive")
		case 1:
			fmt.Fprintln(r.env.Stdout, "Archived 1 card")
		default:
			fmt.Fprintf(r.env.Stdout, "Archived %d cards\n", len(ids))
		}
	}
	r.warnAboutBoard(svc)
	return exitOK
}

func (r *runner) cmdRestore(args []string) int {
	var column string
	pos, code, done := r.parse(usageRestore, args, func(fs *flag.FlagSet) {
		fs.StringVar(&column, "column", "", "")
	})
	if done {
		return code
	}
	id, err := oneID(pos)
	if err != nil {
		return r.failCmd(err, usageRestore)
	}
	svc, _, err := r.service(r.project)
	if err != nil {
		return r.fail(err)
	}
	col, err := resolveColumn(r.ctx, svc, column)
	if err != nil {
		return r.fail(err)
	}
	cv, err := svc.Restore(r.ctx, id, col)
	if err != nil {
		return r.fail(err)
	}
	return r.wrote(svc, cv, fmt.Sprintf("Restored %s to %s", cv.ID, cv.Column))
}

func (r *runner) cmdRm(args []string) int {
	pos, code, done := r.parse(usageDelete, args, nil)
	if done {
		return code
	}
	id, err := oneID(pos)
	if err != nil {
		return r.failCmd(err, usageDelete)
	}
	svc, _, err := r.service(r.project)
	if err != nil {
		return r.fail(err)
	}
	if err := svc.DeleteCard(r.ctx, id); err != nil {
		return r.fail(err)
	}
	if r.json {
		r.printJSON(struct {
			Deleted string `json:"deleted"`
		}{string(id)})
	} else {
		fmt.Fprintf(r.env.Stdout, "Deleted %s\n", id)
	}
	r.warnAboutBoard(svc)
	return exitOK
}
