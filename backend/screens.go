package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// Custom screens: shared (owner NULL, editable from editRole up) or personal
// (visible and editable by their owner only). The layout is the list of
// cards, stored as JSON and only checked for shape: the services it names
// may be offline or gone, the UI shows them as unavailable.

type Screen struct {
	ID       int64           `json:"id"`
	Title    string          `json:"title"`
	Shared   bool            `json:"shared"`
	EditRole string          `json:"editRole,omitempty"`
	Pack     string          `json:"pack"`   // widget pack of the screen, "" = the profile's
	Theme    string          `json:"theme"`  // theme of the screen, "" = the profile's
	Scheme   string          `json:"scheme"` // auto | light | dark, "" = the profile's
	Position int             `json:"position"`
	Version  int             `json:"version"`
	CanEdit  bool            `json:"canEdit"`
	Layout   json.RawMessage `json:"layout"`
}

// Card mirrors the frontend model (PLAN §5.1); only what is checked here.
type Card struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"` // service | section | title
	Service string `json:"service,omitempty"`
	Section string `json:"section,omitempty"`
	Text    string `json:"text,omitempty"`
	W       int    `json:"w,omitempty"`
	H       int    `json:"h,omitempty"`
}

const maxCards = 200

var rank = map[string]int{RoleGuest: 0, RoleMember: 1, RoleAdmin: 2}

var errNotFound = errors.New("screen not found")
var errForbidden = errors.New("not allowed to change this screen")
var errStale = errors.New("this screen was changed meanwhile: reload it")

func (a *Auth) migrateScreens() error {
	_, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS screens (
			id        INTEGER PRIMARY KEY,
			owner     INTEGER REFERENCES profiles(id) ON DELETE CASCADE,
			title     TEXT NOT NULL,
			edit_role TEXT NOT NULL DEFAULT 'admin' CHECK (edit_role IN ('admin', 'member')),
			position  INTEGER NOT NULL DEFAULT 0,
			version   INTEGER NOT NULL DEFAULT 1,
			layout    TEXT NOT NULL DEFAULT '[]'
		)`)
	return err
}

func validLayout(layout json.RawMessage) bool {
	var cards []Card
	if json.Unmarshal(layout, &cards) != nil || len(cards) > maxCards {
		return false
	}
	for _, c := range cards {
		ok := c.ID != "" && c.W >= 0 && c.W <= 4 && c.H >= 0 && c.H <= 8
		switch c.Kind {
		case "service":
			ok = ok && c.Service != ""
		case "section":
			ok = ok && c.Section != ""
		case "title":
			ok = ok && strings.TrimSpace(c.Text) != ""
		default:
			ok = false
		}
		if !ok {
			return false
		}
	}
	return true
}

func canEdit(p Profile, owner sql.NullInt64, editRole string) bool {
	if owner.Valid {
		return owner.Int64 == p.ID
	}
	return rank[p.Role] >= rank[editRole]
}

// Screens lists what p can see: shared first, then p's own, each by position.
func (a *Auth) Screens(p Profile) ([]Screen, error) {
	rows, err := a.db.Query(`SELECT id, owner, title, edit_role, pack, theme, scheme, position, version, layout FROM screens
		WHERE owner IS NULL OR owner = ? ORDER BY owner IS NOT NULL, position, id`, p.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Screen{}
	for rows.Next() {
		var s Screen
		var owner sql.NullInt64
		var layout string
		if err := rows.Scan(&s.ID, &owner, &s.Title, &s.EditRole, &s.Pack, &s.Theme, &s.Scheme, &s.Position, &s.Version, &layout); err != nil {
			return nil, err
		}
		s.Shared, s.CanEdit, s.Layout = !owner.Valid, canEdit(p, owner, s.EditRole), json.RawMessage(layout)
		if !s.Shared {
			s.EditRole = ""
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

func (a *Auth) CreateScreen(p Profile, title string, shared bool) (int64, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return 0, errInvalid
	}
	if shared && p.Role != RoleAdmin {
		return 0, errForbidden
	}
	owner := sql.NullInt64{Int64: p.ID, Valid: !shared}
	res, err := a.db.Exec(`INSERT INTO screens (owner, title, position)
		VALUES (?, ?, (SELECT coalesce(max(position), 0) + 1 FROM screens))`, owner, title)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateScreen saves a whole draft at once, if nobody saved since `version`.
func (a *Auth) UpdateScreen(p Profile, id int64, in Screen) error {
	if strings.TrimSpace(in.Title) == "" || !validLayout(in.Layout) || !packExists(a.PacksDir, in.Pack) ||
		!themes[in.Theme] || !schemes[in.Scheme] {
		return errInvalid
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner sql.NullInt64
	var editRole string
	var version int
	if err := tx.QueryRow(`SELECT owner, edit_role, version FROM screens WHERE id = ?`, id).Scan(&owner, &editRole, &version); err != nil {
		return errNotFound
	}
	switch {
	case owner.Valid && owner.Int64 != p.ID:
		return errNotFound // someone else's personal screen: not even visible
	case !canEdit(p, owner, editRole):
		return errForbidden
	case in.Version != version:
		return errStale
	}
	if !owner.Valid && in.EditRole != "" && in.EditRole != editRole {
		if p.Role != RoleAdmin || (in.EditRole != RoleAdmin && in.EditRole != RoleMember) {
			return errForbidden
		}
		editRole = in.EditRole
	}
	_, err = tx.Exec(`UPDATE screens SET title = ?, edit_role = ?, pack = ?, theme = ?, scheme = ?, position = ?, layout = ?,
		version = version + 1 WHERE id = ?`,
		strings.TrimSpace(in.Title), editRole, in.Pack, in.Theme, in.Scheme, in.Position, string(in.Layout), id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (a *Auth) DeleteScreen(p Profile, id int64) error {
	var owner sql.NullInt64
	if err := a.db.QueryRow(`SELECT owner FROM screens WHERE id = ?`, id).Scan(&owner); err != nil {
		return errNotFound
	}
	switch {
	case owner.Valid && owner.Int64 != p.ID:
		return errNotFound
	case !owner.Valid && p.Role != RoleAdmin:
		return errForbidden
	}
	_, err := a.db.Exec(`DELETE FROM screens WHERE id = ?`, id)
	return err
}

func (a *Auth) ScreenRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/screens", func(w http.ResponseWriter, r *http.Request) {
		list, err := a.Screens(profileFrom(r.Context()))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, list)
	})

	mux.HandleFunc("POST /api/screens", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Title  string `json:"title"`
			Shared bool   `json:"shared"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		id, err := a.CreateScreen(profileFrom(r.Context()), in.Title, in.Shared)
		if err != nil {
			screenError(w, err)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, map[string]int64{"id": id})
	})

	mux.HandleFunc("PUT /api/screens/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var in Screen
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&in) != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if err := a.UpdateScreen(profileFrom(r.Context()), id, in); err != nil {
			screenError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /api/screens/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err := a.DeleteScreen(profileFrom(r.Context()), id); err != nil {
			screenError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func screenError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, errForbidden):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, errStale):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		httpError(w, err)
	}
}
