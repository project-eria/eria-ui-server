package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Pages (PLAN §5.00): a page is a flat list of functions to show, independent
// of any theme; the theme of the terminal renders it. A shared page (no
// owner) is made by the admin and assigned to terminals; a personal page
// belongs to a profile and follows it on every terminal.

type Page struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	Shared   bool            `json:"shared"`
	Version  int             `json:"version"` // +1 at each save, checked against conflicts
	Elements []Element       `json:"elements"`
	Options  json.RawMessage `json:"options"` // {<theme>: {…}}
}

// Element is one function of a page; its fields depend on its type.
type Element struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"` // service | property | action | event | app
	Thing      string          `json:"thing,omitempty"`
	Capability string          `json:"capability,omitempty"` // service
	Name       string          `json:"name,omitempty"`       // property, action, event or app data
	Input      json.RawMessage `json:"input,omitempty"`      // fixed action input
	Label      string          `json:"label,omitempty"`
	Hidden     bool            `json:"hidden,omitempty"`
	Widget     string          `json:"widget,omitempty"` // forced widget tag
	Options    json.RawMessage `json:"options,omitempty"`
}

// PageRef is what the resource manifest lists.
type PageRef struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

const (
	maxElements  = 200
	maxPageTitle = 100
)

var (
	errPageNotFound = errors.New("page not found")
	errPageStale    = errors.New("this page was changed meanwhile: reload it")
	errPageInvalid  = errors.New("invalid page")
)

func (a *Auth) migratePages() error {
	_, err := a.db.Exec(`
		DROP TABLE IF EXISTS screens;
		CREATE TABLE IF NOT EXISTS pages (
			id       TEXT PRIMARY KEY,
			owner    INTEGER REFERENCES profiles(id) ON DELETE CASCADE,
			title    TEXT NOT NULL,
			version  INTEGER NOT NULL,
			elements TEXT NOT NULL,
			options  TEXT NOT NULL
		)`)
	return err
}

func validPage(p Page) error {
	if t := strings.TrimSpace(p.Title); t == "" || len(t) > maxPageTitle {
		return fmt.Errorf("%w: a title is needed (100 characters at most)", errPageInvalid)
	}
	if len(p.Elements) > maxElements {
		return fmt.Errorf("%w: too many elements", errPageInvalid)
	}
	if !isObject(p.Options) {
		return fmt.Errorf("%w: options must be an object", errPageInvalid)
	}
	ids := map[string]bool{}
	for _, e := range p.Elements {
		ok := e.ID != "" && !ids[e.ID] && isObject(e.Options) && (len(e.Input) == 0 || json.Valid(e.Input))
		ids[e.ID] = true
		switch e.Type {
		case "service":
			ok = ok && e.Thing != "" && e.Capability != ""
		case "property", "action", "event":
			ok = ok && e.Thing != "" && e.Name != ""
		case "app":
			ok = ok && e.Name != ""
		default:
			ok = false
		}
		if !ok {
			return fmt.Errorf("%w: element %q", errPageInvalid, e.ID)
		}
	}
	return nil
}

// isObject accepts a JSON object, or nothing.
func isObject(raw json.RawMessage) bool {
	var v map[string]json.RawMessage
	return len(raw) == 0 || json.Unmarshal(raw, &v) == nil && v != nil
}

func (a *Auth) scanPage(row interface{ Scan(...any) error }) (Page, sql.NullInt64, error) {
	var p Page
	var owner sql.NullInt64
	var elements, options string
	if err := row.Scan(&p.ID, &owner, &p.Title, &p.Version, &elements, &options); err != nil {
		return p, owner, err
	}
	json.Unmarshal([]byte(elements), &p.Elements)
	p.Options, p.Shared = json.RawMessage(options), !owner.Valid
	return p, owner, nil
}

const pageColumns = `id, owner, title, version, elements, options`

// Page returns a page if the viewer may see it: the admin sees every page,
// others the shared pages and their own.
func (a *Auth) Page(viewer *Profile, id string) (Page, error) {
	p, owner, err := a.scanPage(a.db.QueryRow(`SELECT `+pageColumns+` FROM pages WHERE id = ?`, id))
	if err != nil || (owner.Valid && !isAdmin(viewer) && (viewer == nil || viewer.ID != owner.Int64)) {
		return Page{}, errPageNotFound
	}
	return p, nil
}

// Pages lists the pages the viewer may edit: every page for the admin, its
// own pages for anyone else.
func (a *Auth) Pages(viewer *Profile) ([]Page, error) {
	if viewer == nil {
		return []Page{}, nil
	}
	query, args := `SELECT `+pageColumns+` FROM pages ORDER BY title`, []any{}
	if !isAdmin(viewer) {
		query, args = `SELECT `+pageColumns+` FROM pages WHERE owner = ? ORDER BY title`, []any{viewer.ID}
	}
	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Page{}
	for rows.Next() {
		p, _, err := a.scanPage(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// SavePage creates a page (empty id) or updates it (same version as stored).
// Only the admin makes shared pages; anyone signed in makes personal ones.
func (a *Auth) SavePage(editor *Profile, in Page) (Page, error) {
	if editor == nil || editor.Level >= notSignedIn {
		return Page{}, errForbidden
	}
	if err := validPage(in); err != nil {
		return Page{}, err
	}
	if in.Elements == nil {
		in.Elements = []Element{}
	}
	if len(in.Options) == 0 {
		in.Options = json.RawMessage("{}")
	}
	elements, _ := json.Marshal(in.Elements)
	title := strings.TrimSpace(in.Title)
	if in.ID == "" {
		if in.Shared && !isAdmin(editor) {
			return Page{}, errForbidden
		}
		owner := sql.NullInt64{Int64: editor.ID, Valid: !in.Shared}
		raw := make([]byte, 10)
		rand.Read(raw)
		in.ID = publicIDs.EncodeToString(raw)
		_, err := a.db.Exec(`INSERT INTO pages (id, owner, title, version, elements, options) VALUES (?, ?, ?, 1, ?, ?)`,
			in.ID, owner, title, string(elements), string(in.Options))
		if err != nil {
			return Page{}, err
		}
		return a.Page(editor, in.ID)
	}
	stored, err := a.Page(editor, in.ID)
	switch {
	case err != nil:
		return Page{}, err
	case stored.Shared && !isAdmin(editor):
		return Page{}, errForbidden
	case stored.Version != in.Version:
		return Page{}, errPageStale
	}
	res, err := a.db.Exec(`UPDATE pages SET title = ?, version = version + 1, elements = ?, options = ? WHERE id = ? AND version = ?`,
		title, string(elements), string(in.Options), in.ID, in.Version)
	if err != nil {
		return Page{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Page{}, errPageStale
	}
	return a.Page(editor, in.ID)
}

// DeletePage removes a page, and from the terminals it was assigned to.
func (a *Auth) DeletePage(editor *Profile, id string) error {
	stored, err := a.Page(editor, id)
	switch {
	case err != nil:
		return err
	case editor.Level >= notSignedIn || stored.Shared && !isAdmin(editor):
		return errForbidden
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	tx.Exec(`DELETE FROM pages WHERE id = ?`, id)
	tx.Exec(`UPDATE terminals SET pages = (SELECT coalesce(json_group_array(value), '[]') FROM json_each(terminals.pages) WHERE value != ?)
		WHERE EXISTS (SELECT 1 FROM json_each(terminals.pages) WHERE value = ?)`, id, id)
	return tx.Commit()
}

// Manifest lists the pages shown on a terminal: its own (assigned by the
// admin, in order), then those of the signed-in profile.
func (a *Auth) Manifest(terminal string, viewer *Profile) ([]PageRef, error) {
	t, err := a.terminal(`id = ?`, terminal)
	if err != nil {
		return nil, err
	}
	refs := []PageRef{}
	for _, id := range t.Pages {
		var version int
		if a.db.QueryRow(`SELECT version FROM pages WHERE id = ? AND owner IS NULL`, id).Scan(&version) == nil {
			refs = append(refs, PageRef{id, version})
		}
	}
	if viewer != nil {
		rows, err := a.db.Query(`SELECT id, version FROM pages WHERE owner = ? ORDER BY title`, viewer.ID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var r PageRef
			rows.Scan(&r.ID, &r.Version)
			refs = append(refs, r)
		}
	}
	return refs, nil
}

// sharedPagesExist tells whether every id names a shared page (terminal
// assignment).
func (a *Auth) sharedPagesExist(ids []string) bool {
	for _, id := range ids {
		var n int
		if a.db.QueryRow(`SELECT count(*) FROM pages WHERE id = ? AND owner IS NULL`, id).Scan(&n); n == 0 {
			return false
		}
	}
	return true
}

func isAdmin(p *Profile) bool { return p != nil && p.Level == 0 }

var errForbidden = errors.New("not allowed to change this page")
