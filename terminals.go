package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"errors"
	"slices"
	"strings"
)

// Terminal is one install of the app (PLAN §5.5). The app generates a secret
// and sends it when it connects; the server keeps only its hash, and shows a
// separate public id and a short code derived from it.
type Terminal struct {
	ID           string `json:"id"`   // public
	Code         string `json:"code"` // to find the terminal in the administration
	Name         string `json:"name"`
	Type         string `json:"type"` // "" = detected type
	DetectedType string `json:"detectedType"`
	LastSeen     int64  `json:"lastSeen"`
	Online       bool   `json:"online"` // an app of this terminal is connected now
}

// Terminal types, smallest to largest (PLAN §5.4).
var terminalTypes = []string{"watch", "phone", "tablet", "wall-tablet", "desktop", "tv"}

var errBadTerminal = errors.New("missing or malformed terminal secret")

var publicIDs = base32.NewEncoding("abcdefghijkmnpqrstuvwxyz23456789").WithPadding(base32.NoPadding)

func (a *Auth) migrateTerminals() error {
	_, err := a.db.Exec(`CREATE TABLE IF NOT EXISTS terminals (
		id            TEXT PRIMARY KEY,
		secret_hash   TEXT NOT NULL UNIQUE,
		name          TEXT NOT NULL DEFAULT '',
		type          TEXT NOT NULL DEFAULT '',
		detected_type TEXT NOT NULL DEFAULT '',
		theme         TEXT NOT NULL DEFAULT '',
		aspect        TEXT NOT NULL DEFAULT '',
		pages         TEXT NOT NULL DEFAULT '[]',
		first_seen    INTEGER NOT NULL,
		last_seen     INTEGER NOT NULL
	)`)
	return err
}

// Identify returns the terminal holding this secret, registering it on its
// first connection.
func (a *Auth) Identify(secret string) (Terminal, error) {
	if len(secret) < 22 || len(secret) > 128 { // at least 128 bits, base64url
		return Terminal{}, errBadTerminal
	}
	hash, now := tokenHash(secret), a.now().Unix()
	res, err := a.db.Exec(`UPDATE terminals SET last_seen = ? WHERE secret_hash = ?`, now, hash)
	if err != nil {
		return Terminal{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		raw := make([]byte, 10)
		rand.Read(raw)
		_, err = a.db.Exec(`INSERT INTO terminals (id, secret_hash, first_seen, last_seen) VALUES (?, ?, ?, ?)`,
			publicIDs.EncodeToString(raw), hash, now, now)
		if err != nil {
			return Terminal{}, err
		}
	}
	return a.terminal(`secret_hash = ?`, hash)
}

// ponytail: the theme, aspect and pages columns of older databases stay
// unused (PLAN §5.000: pages are on every terminal, their look is theirs).
const terminalColumns = `id, name, type, detected_type, last_seen`

func scanTerminal(row interface{ Scan(...any) error }) (Terminal, error) {
	var t Terminal
	if err := row.Scan(&t.ID, &t.Name, &t.Type, &t.DetectedType, &t.LastSeen); err != nil {
		return t, err
	}
	t.Code = strings.ToUpper(t.ID[:6])
	return t, nil
}

func (a *Auth) terminal(where string, arg any) (Terminal, error) {
	return scanTerminal(a.db.QueryRow(`SELECT `+terminalColumns+` FROM terminals WHERE `+where, arg))
}

func (a *Auth) Terminals() ([]Terminal, error) {
	rows, err := a.db.Query(`SELECT ` + terminalColumns + ` FROM terminals ORDER BY last_seen DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Terminal{}
	for rows.Next() {
		t, err := scanTerminal(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

// ReportType records the type the app deduced from its screen.
func (a *Auth) ReportType(id, detected string) error {
	if !slices.Contains(terminalTypes, detected) {
		return errInvalid
	}
	_, err := a.db.Exec(`UPDATE terminals SET detected_type = ? WHERE id = ?`, detected, id)
	return err
}

// TerminalChange holds the settings an admin changes; nil fields are kept.
type TerminalChange struct {
	ID   string  `json:"id"`
	Name *string `json:"name"`
	Type *string `json:"type"` // "" = detected
}

func (a *Auth) ConfigureTerminal(in TerminalChange) (Terminal, error) {
	if in.Type != nil && *in.Type != "" && !slices.Contains(terminalTypes, *in.Type) {
		return Terminal{}, errInvalid
	}
	if in.Name != nil {
		if _, err := a.db.Exec(`UPDATE terminals SET name = ? WHERE id = ?`, strings.TrimSpace(*in.Name), in.ID); err != nil {
			return Terminal{}, err
		}
	}
	if in.Type != nil {
		if _, err := a.db.Exec(`UPDATE terminals SET type = ? WHERE id = ?`, *in.Type, in.ID); err != nil {
			return Terminal{}, err
		}
	}
	t, err := a.terminal(`id = ?`, in.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return t, errUnknown
	}
	return t, err
}

// ForgetTerminal deletes a terminal: it registers again, unconfigured, at its
// next connection.
func (a *Auth) ForgetTerminal(id string) error {
	_, err := a.db.Exec(`DELETE FROM terminals WHERE id = ?`, id)
	return err
}
