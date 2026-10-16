package main

import (
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
	guest, _ := auth.CreateProfile("Invité", RoleGuest, "0000")
	member, _ := auth.CreateProfile("Marlène", RoleMember, "5678")
	if guest.Level != 9 || member.Level != 1 {
		t.Errorf("levels = %d / %d, want 9 / 1", guest.Level, member.Level)
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
	if _, err := auth.CreateProfile("Lia", RoleMember, "2468"); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.CreateProfile("Lia", RoleMember, "2468"); !errors.Is(err, errConflict) {
		t.Errorf("duplicate name: %v", err)
	}
	if _, err := auth.CreateProfile("Érine", RoleMember, "12"); !errors.Is(err, errInvalid) {
		t.Errorf("PIN too short: %v", err)
	}
	if err := auth.DeleteProfile(1); !errors.Is(err, errLastAdmin) {
		t.Errorf("delete last admin: %v", err)
	}
	demote := RoleMember
	if err := auth.UpdateProfile(1, nil, &demote, nil); !errors.Is(err, errLastAdmin) {
		t.Errorf("demote last admin: %v", err)
	}
	if p, ok := auth.Session(mb); !ok || p.Level != 1 {
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
