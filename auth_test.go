package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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

	srv := httptest.NewServer(withAuth(http.NewServeMux(), auth))
	defer srv.Close()
	call := func(token, method, path, body string) int {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	expect := func(what string, got, want int) {
		t.Helper()
		if got != want {
			t.Errorf("%s: status %d, want %d", what, got, want)
		}
	}

	expect("health probe is public", call("", "GET", "/api/health", ""), 404) // reached: no route in this mux
	expect("API needs a token", call("", "GET", "/api/me", ""), 401)
	expect("unknown token", call("nope", "GET", "/api/me", ""), 401)
	expect("member reads own profile", call(mb, "GET", "/api/me", ""), 200)
	expect("Standard pack chosen", call(mb, "PUT", "/api/me", `{"pack":"standard"}`), 204)
	expect("unknown theme refused", call(mb, "PUT", "/api/me", `{"theme":"neon"}`), 400)

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
	pin := "1357"
	auth.UpdateProfile(member.ID, nil, nil, &pin)
	expect("new PIN signs the member out", call(mb, "GET", "/api/me", ""), 401)

	auth.Logout(a)
	expect("after logout", call(a, "GET", "/api/me", ""), 401)
}
