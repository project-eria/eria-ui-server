package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeThing mimics a go-wot Thing: HTTP read / write / invoke, and raw-JSON
// WebSocket frames for observed properties and subscribed events.
type fakeThing struct {
	open      atomic.Int32 // upstream WebSockets currently open
	mu        sync.Mutex
	conns     []*websocket.Conn
	connected chan struct{}
}

func (f *fakeThing) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !websocket.IsWebSocketUpgrade(r) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/forbidden"):
			http.Error(w, "level too high", http.StatusForbidden)
		case r.Method == http.MethodPut:
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost:
			w.Write([]byte(`"done"`))
		default:
			w.Write([]byte("42"))
		}
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	f.open.Add(1)
	f.mu.Lock()
	f.conns = append(f.conns, conn)
	f.mu.Unlock()
	f.connected <- struct{}{}
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			f.open.Add(-1)
			return
		}
	}
}

func (f *fakeThing) emit(v string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.conns {
		c.WriteMessage(websocket.TextMessage, []byte(v))
	}
}

// testApp is one app connected to the channel.
type testApp struct {
	t       *testing.T
	conn    *websocket.Conn
	id      int
	pending []map[string]any // received but not consumed yet
}

// next returns the first message (reply or push) accepted by keep; the
// others stay pending for later calls.
func (a *testApp) next(keep func(map[string]any) bool) map[string]any {
	a.t.Helper()
	for i, msg := range a.pending {
		if keep(msg) {
			a.pending = append(a.pending[:i], a.pending[i+1:]...)
			return msg
		}
	}
	a.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var msg map[string]any
		if err := a.conn.ReadJSON(&msg); err != nil {
			a.t.Fatalf("no expected message: %v", err)
		}
		if keep(msg) {
			return msg
		}
		a.pending = append(a.pending, msg)
	}
}

func (a *testApp) push(kind string) map[string]any {
	a.t.Helper()
	return a.next(func(m map[string]any) bool { return m["push"] == kind })
}

// call sends a request and returns its reply.
func (a *testApp) call(op string, fields map[string]any) map[string]any {
	a.t.Helper()
	a.id++
	req := map[string]any{"id": a.id, "op": op}
	for k, v := range fields {
		req[k] = v
	}
	a.conn.WriteJSON(req)
	id := float64(a.id)
	return a.next(func(m map[string]any) bool { return m["id"] == id })
}

func errType(reply map[string]any) string {
	e, _ := reply["error"].(map[string]any)
	s, _ := e["type"].(string)
	return s
}

func TestChannel(t *testing.T) {
	retryMin = 20 * time.Millisecond
	statusEvery = 50 * time.Millisecond
	thing := &fakeThing{connected: make(chan struct{}, 10)}
	up := httptest.NewServer(thing)
	defer up.Close()
	ws := "ws" + strings.TrimPrefix(up.URL, "http")

	auth, err := OpenAuth(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	auth.Bootstrap("1234")
	guest, _ := auth.CreateProfile("Invité", RoleGuest, "0000")

	registry := NewRegistry(nil)
	td := TD{
		ID: "urn:test",
		Properties: map[string]Property{
			"temp": {Observable: true, ReadOnly: true, Forms: []Form{
				{Href: ws + "/temp", Op: []any{"observeproperty"}},
				{Href: up.URL + "/temp", Op: []any{"readproperty"}},
			}},
			"mode": {Forms: []Form{{Href: up.URL + "/mode", Op: []any{"readproperty", "writeproperty"}}}},
		},
		Actions: map[string]Action{
			"toggle": {Forms: []Form{{Href: up.URL + "/toggle", Op: "invokeaction"}}},
			"secret": {Forms: []Form{{Href: up.URL + "/forbidden", Op: "invokeaction"}}},
		},
		Events: map[string]Event{"ring": {Forms: []Form{{Href: ws + "/ring", Op: []any{"subscribeevent"}}}}},
	}
	registry.things["urn:test"] = &thingEntry{seen: time.Now(), td: td}
	hub := NewHub(registry)
	srv := httptest.NewServer(withAuth(newMux(&Channel{hub: hub, auth: auth, registry: registry}), auth))
	defer srv.Close()

	dial := func(secret, token string) *testApp {
		t.Helper()
		header := http.Header{"X-Eria-Terminal": {secret}}
		if token != "" {
			header.Set("Authorization", "Bearer "+token)
		}
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/api/ws", header)
		if err != nil {
			t.Fatal(err)
		}
		return &testApp{t: t, conn: conn}
	}
	const secret1, secret2 = "terminal-one-secret-0000000", "terminal-two-secret-0000000"

	// A terminal must identify itself; an unknown one is registered.
	if _, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/api/ws", nil); err == nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("handshake without terminal secret accepted (%v)", err)
	}
	a := dial(secret1, "")
	welcome := a.push("welcome")
	term, _ := welcome["terminal"].(map[string]any)
	if code, _ := term["code"].(string); len(code) != 6 || welcome["profile"] != nil {
		t.Fatalf("welcome = %v, want a 6-character code and no profile", welcome)
	}
	if list, _ := auth.Terminals(); len(list) != 1 {
		t.Fatalf("%d terminals registered, want 1", len(list))
	}

	// Not signed in: reads, never acts.
	if r := a.call("read", map[string]any{"thing": "urn:test", "name": "temp"}); r["result"] != float64(42) {
		t.Errorf("read = %v", r)
	}
	if r := a.call("invoke", map[string]any{"thing": "urn:test", "name": "toggle"}); errType(r) != failRefused {
		t.Errorf("invoke without session = %v, want refused", r)
	}
	if r := a.call("login", map[string]any{"profile": 1, "pin": "9999"}); errType(r) != failRefused {
		t.Errorf("wrong PIN = %v, want refused", r)
	}
	r := a.call("login", map[string]any{"profile": 1, "pin": "1234"})
	result, _ := r["result"].(map[string]any)
	token, _ := result["token"].(string)
	if token == "" {
		t.Fatalf("login = %v", r)
	}

	// Signed in: commands, with typed errors.
	if r := a.call("invoke", map[string]any{"thing": "urn:test", "name": "toggle"}); r["result"] != "done" {
		t.Errorf("invoke = %v", r)
	}
	if r := a.call("write", map[string]any{"thing": "urn:test", "name": "mode", "value": 1}); r["result"] != nil || r["error"] != nil {
		t.Errorf("write = %v", r)
	}
	if r := a.call("write", map[string]any{"thing": "urn:test", "name": "temp", "value": 1}); errType(r) != failRefused {
		t.Errorf("write read-only = %v, want refused", r)
	}
	if r := a.call("invoke", map[string]any{"thing": "urn:test", "name": "secret"}); errType(r) != failRefused {
		t.Errorf("Thing answering 403 = %v, want refused", r)
	}
	if r := a.call("invoke", map[string]any{"thing": "urn:nope", "name": "toggle"}); errType(r) != failInvalid {
		t.Errorf("unknown Thing = %v, want invalid", r)
	}
	if r := a.call("dance", nil); errType(r) != failInvalid {
		t.Errorf("unknown op = %v, want invalid", r)
	}

	// Observations: one upstream socket shared by every app, cached value.
	isValue := func(v float64) func(map[string]any) bool {
		return func(m map[string]any) bool { return m["push"] == "value" && m["value"] == v }
	}
	a.call("observe", map[string]any{"thing": "urn:test", "name": "temp"})
	<-thing.connected
	a.next(isValue(42)) // current value read over HTTP once connected
	b := dial(secret2, "")
	b.push("welcome")
	b.call("observe", map[string]any{"thing": "urn:test", "name": "temp"})
	b.next(isValue(42)) // late subscriber gets the cached value
	thing.emit("43")
	a.next(isValue(43))
	b.next(isValue(43))
	if n := thing.open.Load(); n != 1 {
		t.Fatalf("%d upstream sockets for 2 apps, want 1", n)
	}
	if r := a.call("observe", map[string]any{"thing": "urn:test", "name": "nope"}); errType(r) != failInvalid {
		t.Errorf("unknown property = %v, want invalid", r)
	}

	// Events: pushed, never replayed.
	a.call("subscribe", map[string]any{"thing": "urn:test", "name": "ring"})
	<-thing.connected
	thing.emit(`"ding"`)
	a.next(func(m map[string]any) bool {
		return m["push"] == "event" && m["event"] == "ring" && m["value"] == "ding"
	})

	// The token signs the app in again when it reconnects.
	again := dial(secret1, token)
	if p, _ := again.push("welcome")["profile"].(map[string]any); p["name"] != "Admin" {
		t.Errorf("welcome after reconnect: profile = %v", p)
	}
	again.conn.Close()

	// Administration: level 0 only; the configured terminal is told.
	g := dial("terminal-guest-secret-00000", "")
	g.push("welcome")
	g.call("login", map[string]any{"profile": guest.ID, "pin": "0000"})
	if r := g.call("terminals", nil); errType(r) != failRefused {
		t.Errorf("guest lists terminals = %v, want refused", r)
	}
	id2 := idOfSecret(t, auth, secret2)
	if r := a.call("configureTerminal", map[string]any{"value": map[string]any{"id": id2, "name": "Entrée", "type": "wall-tablet"}}); r["error"] != nil {
		t.Fatalf("configure = %v", r)
	}
	if term, _ := b.push("terminal")["terminal"].(map[string]any); term["name"] != "Entrée" || term["type"] != "wall-tablet" {
		t.Errorf("configured terminal told %v", term)
	}
	if r := a.call("configureTerminal", map[string]any{"value": map[string]any{"id": id2, "type": "fridge"}}); errType(r) != failInvalid {
		t.Errorf("unknown type = %v, want invalid", r)
	}
	if r := a.call("setContexts", map[string]any{"value": []map[string]string{{"id": "away", "label": "Absent"}}}); r["error"] != nil {
		t.Errorf("setContexts = %v", r)
	}
	b.push("contexts-changed")
	if r := a.call("contexts", nil); len(r["result"].([]any)) != 1 {
		t.Errorf("contexts = %v", r)
	}

	// Pages: a saved page is pushed to the terminal it is assigned to.
	hasPage := func(version float64) func(map[string]any) bool {
		return func(m map[string]any) bool {
			pages, _ := m["pages"].([]any)
			if m["push"] != "resources" || len(pages) != 1 {
				return false
			}
			ref, _ := pages[0].(map[string]any)
			return ref["version"] == version
		}
	}
	page := map[string]any{"title": "Entrée", "shared": true, "elements": []map[string]any{
		{"id": "e1", "type": "action", "thing": "urn:test", "name": "toggle"}}}
	r = a.call("savePage", map[string]any{"value": page})
	saved, _ := r["result"].(map[string]any)
	if saved["id"] == nil {
		t.Fatalf("savePage = %v", r)
	}
	a.call("configureTerminal", map[string]any{"value": map[string]any{"id": id2, "pages": []any{saved["id"]}}})
	b.next(hasPage(1))
	saved["title"] = "Entrée principale"
	a.call("savePage", map[string]any{"value": saved})
	b.next(hasPage(2))
	if r := b.call("page", map[string]any{"value": saved["id"]}); r["result"].(map[string]any)["title"] != "Entrée principale" {
		t.Errorf("page = %v", r)
	}

	// can follows the level: the guest reads, the admin acts.
	invokes := func(app *testApp) int {
		services, _ := app.call("services", nil)["result"].([]any)
		svc, _ := services[0].(map[string]any)
		can, _ := svc["can"].(map[string]any)
		list, _ := can["invoke"].([]any)
		return len(list)
	}
	if n := invokes(g); n != 0 {
		t.Errorf("guest may invoke %d actions, want 0", n)
	}
	if n := invokes(a); n != 2 {
		t.Errorf("admin may invoke %d actions, want 2", n)
	}

	// Last app gone: the upstream sockets are closed.
	a.conn.Close()
	b.call("unobserve", map[string]any{"thing": "urn:test", "name": "temp"})
	deadline := time.Now().Add(2 * time.Second)
	for thing.open.Load() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("upstream socket still open after the last unobserve")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Hiding a Thing forgets it; forgetting a terminal disconnects it.
	admin := dial(secret1, token)
	admin.push("welcome")
	admin.call("setHidden", map[string]any{"value": []string{"urn:test"}})
	if _, ok := registry.Thing("urn:test"); ok {
		t.Error("hidden Thing still known")
	}
	admin.call("forgetTerminal", map[string]any{"value": id2})
	b.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		_, _, err := b.conn.ReadMessage()
		if err == nil {
			continue
		}
		if ne, ok := err.(interface{ Timeout() bool }); ok && ne.Timeout() {
			t.Fatal("forgotten terminal still connected")
		}
		break // closed by the server
	}
}

func idOfSecret(t *testing.T, auth *Auth, secret string) string {
	t.Helper()
	term, err := auth.Identify(secret)
	if err != nil {
		t.Fatal(err)
	}
	return term.ID
}
