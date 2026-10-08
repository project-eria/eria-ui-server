package main

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
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

	// A stand-in for the Thing routes: reached means allowed.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/things/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	srv := httptest.NewServer(withAuth(mux, auth))
	defer srv.Close()

	type browser struct{ *http.Client }
	newBrowser := func() browser {
		jar, _ := cookiejar.New(nil)
		return browser{&http.Client{Jar: jar}}
	}
	call := func(b browser, method, path, body string) int {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if method != http.MethodGet {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := b.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	login := func(b browser, id int64, pin string) int {
		return call(b, "POST", "/api/auth/login", fmt.Sprintf(`{"profile":%d,"pin":%q}`, id, pin))
	}
	expect := func(what string, got, want int) {
		t.Helper()
		if got != want {
			t.Errorf("%s: status %d, want %d", what, got, want)
		}
	}

	anon := newBrowser()
	expect("profiles list is public", call(anon, "GET", "/api/auth/profiles", ""), 200)
	expect("health probe is public", call(anon, "GET", "/api/health", ""), 404) // reached: no route in this mux
	expect("services need a session", call(anon, "GET", "/api/services", ""), 401)
	expect("the app itself is public", call(anon, "GET", "/", ""), 404) // no "/" route in this mux

	// Guest: reads, never writes, no profile management.
	g := newBrowser()
	expect("guest login", login(g, guest.ID, "0000"), 204)
	expect("guest reads", call(g, "GET", "/api/things/x/properties/on", ""), 418)
	expect("guest can't act", call(g, "POST", "/api/things/x/actions/toggle", ""), 403)
	expect("guest can't manage profiles", call(g, "GET", "/api/profiles", ""), 403)

	// Member: acts; CSRF: a non-JSON write is refused before anything else.
	mb := newBrowser()
	expect("member login", login(mb, member.ID, "5678"), 204)
	expect("member acts", call(mb, "POST", "/api/things/x/actions/toggle", ""), 418)
	expect("unknown pack refused", call(mb, "PUT", "/api/me", `{"pack":"nope"}`), 400)
	expect("Standard pack chosen", call(mb, "PUT", "/api/me", `{"pack":"standard"}`), 204)
	expect("guest chooses a pack too", call(g, "PUT", "/api/me", `{"pack":""}`), 204)
	expect("theme and scheme", call(mb, "PUT", "/api/me", `{"theme":"contraste","scheme":"dark"}`), 204)
	expect("unknown theme refused", call(mb, "PUT", "/api/me", `{"theme":"neon"}`), 400)
	expect("unknown scheme refused", call(mb, "PUT", "/api/me", `{"scheme":"sepia"}`), 400)
	if p, _ := auth.session(func() *http.Request {
		r, _ := http.NewRequest("GET", "/", nil)
		u, _ := url.Parse(srv.URL)
		for _, c := range mb.Jar.Cookies(u) {
			r.AddCookie(c)
		}
		return r
	}()); p.Theme != "contraste" || p.Scheme != "dark" || p.Pack != "standard" {
		t.Errorf("preferences = %+v, want contraste/dark kept with pack standard", p)
	}
	req, _ := http.NewRequest("POST", srv.URL+"/api/things/x/actions/toggle", strings.NewReader(""))
	req.Header.Set("Content-Type", "text/plain")
	resp, _ := mb.Do(req)
	expect("form-like POST (CSRF)", resp.StatusCode, 415)

	// Wrong PINs: locked after 5, longer each time, unlocked by time.
	x := newBrowser()
	for i := range maxFailures {
		expect(fmt.Sprintf("wrong PIN %d", i+1), login(x, member.ID, "9999"), 401)
	}
	expect("locked, even with the right PIN", login(x, member.ID, "5678"), 429)
	now = now.Add(lockBase + time.Second)
	expect("unlocked after the delay", login(x, member.ID, "5678"), 204)
	expect("member session still valid", call(mb, "GET", "/api/me", ""), 200)

	// Admin: manages profiles, but the last admin stays.
	a := newBrowser()
	expect("admin login", login(a, 1, "1234"), 204)
	expect("create profile", call(a, "POST", "/api/profiles", `{"name":"Lia","role":"member","pin":"2468"}`), 201)
	expect("duplicate name", call(a, "POST", "/api/profiles", `{"name":"Lia","role":"member","pin":"2468"}`), 409)
	expect("PIN too short", call(a, "POST", "/api/profiles", `{"name":"Érine","role":"member","pin":"12"}`), 400)
	expect("delete last admin", call(a, "DELETE", "/api/profiles/1", ""), 409)
	expect("demote last admin", call(a, "PUT", "/api/profiles/1", `{"role":"member"}`), 409)
	expect("new PIN for the member", call(a, "PUT", fmt.Sprintf("/api/profiles/%d", member.ID), `{"pin":"1357"}`), 204)
	expect("new PIN signs the member out", call(mb, "GET", "/api/me", ""), 401)
	expect("delete guest", call(a, "DELETE", fmt.Sprintf("/api/profiles/%d", guest.ID), ""), 204)
	expect("deleted guest signed out", call(g, "GET", "/api/me", ""), 401)

	expect("logout", call(a, "POST", "/api/auth/logout", ""), 204)
	expect("after logout", call(a, "GET", "/api/me", ""), 401)
}
