package main

import (
	"path"
	"strings"
)

// Contexts offered to people and Things never shown live in the database,
// edited by the admin (PLAN §10). config.yml only gives their first values.

func (a *Auth) migrateSettings() error {
	_, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS contexts (
			id       TEXT PRIMARY KEY,
			label    TEXT NOT NULL,
			position INTEGER NOT NULL
		);
		CREATE TABLE IF NOT EXISTS hidden (pattern TEXT PRIMARY KEY);
		CREATE TABLE IF NOT EXISTS seeded (name TEXT PRIMARY KEY);`)
	return err
}

// SeedSettings copies the first values from config.yml, once.
func (a *Auth) SeedSettings(contexts []Context, hidden []string) error {
	var n int
	a.db.QueryRow(`SELECT count(*) FROM seeded WHERE name = 'settings'`).Scan(&n)
	if n > 0 {
		return nil
	}
	if err := a.SetContexts(contexts); err != nil {
		return err
	}
	if err := a.SetHidden(hidden); err != nil {
		return err
	}
	_, err := a.db.Exec(`INSERT INTO seeded (name) VALUES ('settings')`)
	return err
}

func (a *Auth) Contexts() ([]Context, error) {
	rows, err := a.db.Query(`SELECT id, label FROM contexts ORDER BY position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Context{}
	for rows.Next() {
		var c Context
		if err := rows.Scan(&c.ID, &c.Label); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

// SetContexts replaces the list, in display order.
func (a *Auth) SetContexts(list []Context) error {
	seen := map[string]bool{}
	for _, c := range list {
		if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.Label) == "" || seen[c.ID] {
			return errInvalid
		}
		seen[c.ID] = true
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	tx.Exec(`DELETE FROM contexts`)
	for i, c := range list {
		if _, err := tx.Exec(`INSERT INTO contexts (id, label, position) VALUES (?, ?, ?)`, c.ID, strings.TrimSpace(c.Label), i); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (a *Auth) Hidden() ([]string, error) {
	rows, err := a.db.Query(`SELECT pattern FROM hidden ORDER BY pattern`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []string{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// SetHidden replaces the glob patterns of the Things never shown.
func (a *Auth) SetHidden(patterns []string) error {
	for _, p := range patterns {
		if _, err := path.Match(p, ""); err != nil || p == "" {
			return errInvalid
		}
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	tx.Exec(`DELETE FROM hidden`)
	for _, p := range patterns {
		tx.Exec(`INSERT OR IGNORE INTO hidden (pattern) VALUES (?)`, p)
	}
	return tx.Commit()
}
