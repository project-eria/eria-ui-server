package main

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAuth(t *testing.T) {
	auth, err := OpenAuth(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	auth.now = func() time.Time { return now }
	if err := auth.Bootstrap(""); err == nil {
		t.Fatal("bootstrap without PIN must fail on an empty database")
	}
	if err := auth.Bootstrap("1234"); err != nil {
		t.Fatal(err)
	}
	guest, _ := auth.CreateProfile("Invité", 9, "0000")
	member, _ := auth.CreateProfile("Marlène", 5, "5678")
	if guest.Level != 9 || member.Level != 5 {
		t.Errorf("levels = %d / %d, want 9 / 5", guest.Level, member.Level)
	}
	login := func(id int64, pin string) string {
		t.Helper()
		token, _, err := auth.Login(id, pin)
		if err != nil {
			t.Fatalf("login %d: %v", id, err)
		}
		return token
	}

	// Wrong PINs: locked after 5, longer each time, unlocked by time.
	for range maxFailures {
		if _, _, err := auth.Login(member.ID, "9999"); !errors.Is(err, errBadPIN) {
			t.Fatalf("wrong PIN: %v", err)
		}
	}
	if _, wait, err := auth.Login(member.ID, "5678"); !errors.Is(err, errLocked) || wait <= 0 {
		t.Fatalf("locked profile, right PIN: %v (wait %v)", err, wait)
	}
	now = now.Add(lockBase + time.Second)
	mb := login(member.ID, "5678")

	// Profiles: unique names, valid PINs, the last admin stays; a new PIN
	// signs the profile out everywhere.
	a := login(1, "1234")
	if _, err := auth.CreateProfile("Lia", 5, "2468"); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.CreateProfile("Lia", 5, "2468"); !errors.Is(err, errConflict) {
		t.Errorf("duplicate name: %v", err)
	}
	if _, err := auth.CreateProfile("Érine", 5, "12"); !errors.Is(err, errInvalid) {
		t.Errorf("PIN too short: %v", err)
	}
	if err := auth.DeleteProfile(1); !errors.Is(err, errLastAdmin) {
		t.Errorf("delete last admin: %v", err)
	}
	demote := 5
	if err := auth.UpdateProfile(1, nil, &demote, nil); !errors.Is(err, errLastAdmin) {
		t.Errorf("demote last admin: %v", err)
	}
	if p, ok := auth.Session(mb); !ok || p.Level != 5 {
		t.Errorf("member session = %+v, %v", p, ok)
	}
	pin := "1357"
	auth.UpdateProfile(member.ID, nil, nil, &pin)
	if _, ok := auth.Session(mb); ok {
		t.Error("a new PIN must sign the member out")
	}
	auth.Logout(a)
	if _, ok := auth.Session(a); ok {
		t.Error("session still valid after logout")
	}
}

// Databases from before the levels: roles become levels, sessions stay.
func TestMigrateLevels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE profiles (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE,
			role TEXT NOT NULL CHECK (role IN ('admin', 'member', 'guest')), pin_hash TEXT NOT NULL,
			failures INTEGER NOT NULL DEFAULT 0, locked_until INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE sessions (token_hash TEXT PRIMARY KEY, profile_id INTEGER NOT NULL REFERENCES profiles(id) ON DELETE CASCADE, expires INTEGER NOT NULL)`,
		`INSERT INTO profiles (id, name, role, pin_hash) VALUES (1, 'Admin', 'admin', 'x'), (2, 'Lia', 'member', 'x'), (3, 'Invité', 'guest', 'x')`,
		`INSERT INTO sessions VALUES ('` + tokenHash("t") + `', 2, 9999999999)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	auth, err := OpenAuth(path)
	if err != nil {
		t.Fatal(err)
	}
	list, _ := auth.Profiles()
	got := map[string]int{}
	for _, p := range list {
		got[p.Name] = p.Level
	}
	if got["Admin"] != 0 || got["Lia"] != 5 || got["Invité"] != 9 {
		t.Errorf("levels = %v", got)
	}
	if p, ok := auth.Session("t"); !ok || p.Name != "Lia" {
		t.Errorf("session lost by the migration: %+v, %v", p, ok)
	}
}
