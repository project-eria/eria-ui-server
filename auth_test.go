package main

import (
	"errors"
	"fmt"
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
	expect("member can't manage profiles", call(mb, "GET", "/api/profiles", ""), 403)
	expect("Standard pack chosen", call(mb, "PUT", "/api/me", `{"pack":"standard"}`), 204)
	expect("unknown theme refused", call(mb, "PUT", "/api/me", `{"theme":"neon"}`), 400)

	// Admin: manages profiles, but the last admin stays.
	a := login(1, "1234")
	expect("create profile", call(a, "POST", "/api/profiles", `{"name":"Lia","role":"member","pin":"2468"}`), 201)
	expect("duplicate name", call(a, "POST", "/api/profiles", `{"name":"Lia","role":"member","pin":"2468"}`), 409)
	expect("PIN too short", call(a, "POST", "/api/profiles", `{"name":"Érine","role":"member","pin":"12"}`), 400)
	expect("delete last admin", call(a, "DELETE", "/api/profiles/1", ""), 409)
	expect("demote last admin", call(a, "PUT", "/api/profiles/1", `{"role":"member"}`), 409)
	expect("new PIN for the member", call(a, "PUT", fmt.Sprintf("/api/profiles/%d", member.ID), `{"pin":"1357"}`), 204)
	expect("new PIN signs the member out", call(mb, "GET", "/api/me", ""), 401)

	auth.Logout(a)
	expect("after logout", call(a, "GET", "/api/me", ""), 401)
}
