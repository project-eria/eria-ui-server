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
		CREATE TABLE IF NOT EXISTS manual_things (id TEXT PRIMARY KEY, url TEXT NOT NULL);
		CREATE TABLE IF NOT EXISTS seeded (name TEXT PRIMARY KEY);
		CREATE TABLE IF NOT EXISTS options (key TEXT PRIMARY KEY, value TEXT NOT NULL);`)
	return err
}

// HomePage is the house's start page, for the terminals that have none of
// their own ("" = the first page by title).
func (a *Auth) HomePage() string {
	var id string
	a.db.QueryRow(`SELECT value FROM options WHERE key = 'homePage'`).Scan(&id)
	return id
}

func (a *Auth) SetHomePage(id string) error {
	if err := a.knownPage(id); err != nil {
		return err
	}
	_, err := a.db.Exec(`INSERT INTO options (key, value) VALUES ('homePage', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, id)
	return err
}

// knownPage accepts "" (no choice) or the id of an existing page. A start
// page deleted later is simply skipped by the app.
func (a *Auth) knownPage(id string) error {
	if id == "" {
		return nil
	}
	if _, err := a.Page(id); err != nil {
		return errInvalid
	}
	return nil
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

// ManualURLs lists the TD addresses of the Things the admin added by hand.
func (a *Auth) ManualURLs() []string {
	rows, err := a.db.Query(`SELECT url FROM manual_things`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var urls []string
	for rows.Next() {
		var u string
		rows.Scan(&u)
		urls = append(urls, u)
	}
	return urls
}

func (a *Auth) AddManual(id, url string) error {
	_, err := a.db.Exec(`INSERT INTO manual_things (id, url) VALUES (?, ?) ON CONFLICT (id) DO UPDATE SET url = excluded.url`, id, url)
	return err
}

// RemoveManual forgets a Thing added by hand; false when it was not one.
func (a *Auth) RemoveManual(id string) bool {
	res, err := a.db.Exec(`DELETE FROM manual_things WHERE id = ?`, id)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}
