package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Pages (eria-ui-app PLAN §5.000): every page is shared and shown on every
// terminal; the admin creates them. A page is a flat list of functions,
// rendered by the theme, aspect and layout chosen at its creation (fixed
// from then on: another look means another page).

type Page struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	Theme    string          `json:"theme"`   // "" = Base; fixed at creation
	Aspect   string          `json:"aspect"`  // one of the theme's aspects; fixed at creation
	Layout   string          `json:"layout"`  // a page type of the theme, or a Base layout; fixed at creation
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
			title    TEXT NOT NULL,
			theme    TEXT NOT NULL DEFAULT '',
			aspect   TEXT NOT NULL DEFAULT '',
			layout   TEXT NOT NULL DEFAULT '',
			version  INTEGER NOT NULL,
			elements TEXT NOT NULL,
			options  TEXT NOT NULL
		)`)
	if err != nil {
		return err
	}
	// Older databases (lot 15): theme, aspect and layout columns added.
	for _, column := range []string{"theme", "aspect", "layout"} {
		var n int
		a.db.QueryRow(`SELECT count(*) FROM pragma_table_info('pages') WHERE name = ?`, column).Scan(&n)
		if n == 0 {
			if _, err := a.db.Exec(`ALTER TABLE pages ADD COLUMN ` + column + ` TEXT NOT NULL DEFAULT ''`); err != nil {
				return err
			}
		}
	}
	return nil
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

func scanPage(row interface{ Scan(...any) error }) (Page, error) {
	var p Page
	var elements, options string
	if err := row.Scan(&p.ID, &p.Title, &p.Theme, &p.Aspect, &p.Layout, &p.Version, &elements, &options); err != nil {
		return p, err
	}
	json.Unmarshal([]byte(elements), &p.Elements)
	p.Options = json.RawMessage(options)
	return p, nil
}

const pageColumns = `id, title, theme, aspect, layout, version, elements, options`

// Page returns one page (every page is visible to everyone).
func (a *Auth) Page(id string) (Page, error) {
	p, err := scanPage(a.db.QueryRow(`SELECT `+pageColumns+` FROM pages WHERE id = ?`, id))
	if err != nil {
		return Page{}, errPageNotFound
	}
	return p, nil
}

// Pages lists every page, by title.
func (a *Auth) Pages() ([]Page, error) {
	rows, err := a.db.Query(`SELECT ` + pageColumns + ` FROM pages ORDER BY title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Page{}
	for rows.Next() {
		p, err := scanPage(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// SavePage creates a page (empty id) or updates it (same version as stored;
// theme, aspect and layout kept as created). Level 0 only.
func (a *Auth) SavePage(editor *Profile, in Page) (Page, error) {
	if !isAdmin(editor) {
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
		raw := make([]byte, 10)
		rand.Read(raw)
		in.ID = publicIDs.EncodeToString(raw)
		_, err := a.db.Exec(`INSERT INTO pages (id, title, theme, aspect, layout, version, elements, options) VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
			in.ID, title, in.Theme, in.Aspect, in.Layout, string(elements), string(in.Options))
		if err != nil {
			return Page{}, err
		}
		return a.Page(in.ID)
	}
	stored, err := a.Page(in.ID)
	switch {
	case err != nil:
		return Page{}, err
	case stored.Version != in.Version:
		return Page{}, errPageStale
	case in.Theme != stored.Theme || in.Aspect != stored.Aspect || in.Layout != stored.Layout:
		return Page{}, fmt.Errorf("%w: theme, aspect and layout are fixed at creation", errPageInvalid)
	}
	res, err := a.db.Exec(`UPDATE pages SET title = ?, version = version + 1, elements = ?, options = ? WHERE id = ? AND version = ?`,
		title, string(elements), string(in.Options), in.ID, in.Version)
	if err != nil {
		return Page{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Page{}, errPageStale
	}
	return a.Page(in.ID)
}

// DeletePage removes a page. Level 0 only.
func (a *Auth) DeletePage(editor *Profile, id string) error {
	if !isAdmin(editor) {
		return errForbidden
	}
	if _, err := a.Page(id); err != nil {
		return err
	}
	_, err := a.db.Exec(`DELETE FROM pages WHERE id = ?`, id)
	return err
}

// Manifest lists every page with its version, and the themes they use.
func (a *Auth) Manifest() (pages []PageRef, themes []string, err error) {
	rows, err := a.db.Query(`SELECT id, version, theme FROM pages ORDER BY title`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	pages, seen := []PageRef{}, map[string]bool{}
	for rows.Next() {
		var r PageRef
		var theme string
		rows.Scan(&r.ID, &r.Version, &theme)
		pages = append(pages, r)
		if theme != "" && !seen[theme] {
			seen[theme] = true
			themes = append(themes, theme)
		}
	}
	return pages, themes, rows.Err()
}

func isAdmin(p *Profile) bool { return p != nil && p.Level == levelAdmin }

var errForbidden = errors.New("not allowed to change pages")
