package main

import (
	"slices"
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/project-eria/go-wot/auth"
)

// fakeThing mimics a go-wot Thing: HTTP read / write / invoke, and raw-JSON
// WebSocket frames for observed properties and subscribed events.
type fakeThing struct {
	open      atomic.Int32 // upstream WebSockets currently open
	mu        sync.Mutex
	conns     []*websocket.Conn
	connected chan struct{}
	// refuse plays the Thing's decision on a command; confirm is the last
	// X-Eria-Confirm received.
	refuse  func(r *http.Request) bool
	confirm atomic.Value
	// refuseObserve plays a Thing in minimal mode: no observation; bearer
	// is the token of the last read.
	refuseObserve atomic.Bool
	bearer        atomic.Value
}

func (f *fakeThing) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !websocket.IsWebSocketUpgrade(r) {
		f.confirm.Store(r.Header.Get("X-Eria-Confirm"))
		if r.Method == http.MethodGet {
			f.bearer.Store(r.Header.Get("Authorization"))
		}
		switch {
		case r.Method != http.MethodGet && f.refuse != nil && f.refuse(r):
			http.Error(w, "denied", http.StatusForbidden)
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
	if f.refuseObserve.Load() {
		http.Error(w, "minimal mode", http.StatusUnauthorized)
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

	fa := newFakeAuth(t)
	var linesMu sync.Mutex
	var lines []string // what the relay hands auth's journal (step 5e-3)
	fa.client.journal = auth.NewJournal(func(b auth.JournalBatch) error {
		linesMu.Lock()
		defer linesMu.Unlock()
		for _, l := range b.Lines {
			lines = append(lines, strings.TrimSpace(l.Type+" "+l.Reason+" "+l.Object))
		}
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go fa.client.journal.Run(ctx)
	fa.tables["urn:test"] = auth.Table{
		"property/temp/read":   {Levels: [3]int{9, 9, 9}},
		"property/hum/read":    {Levels: [3]int{9, 9, 9}},
		"property/mode/read":   {Levels: [3]int{9, 9, 9}},
		"property/mode/write":  {Levels: [3]int{1, 1, 0}, Confirm: auth.Never},
		"action/toggle/invoke": {Levels: [3]int{1, 1, 0}, Confirm: auth.Never},
		"action/secret/invoke": {Levels: [3]int{1, 1, 1}, Confirm: auth.OutsideRoom},
		"event/ring/read":      {Levels: [3]int{1, 1, 0}},
	}
	// The Thing refuses commands below level 1.
	thing.refuse = func(r *http.Request) bool {
		c, err := auth.Parse(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), fa.client.keys)
		return err != nil || *c.Lvl > 1
	}
	registry := NewRegistry(nil)
	td := TD{
		ID: "urn:test",
		Properties: map[string]Property{
			"temp": {Observable: true, ReadOnly: true, Forms: []Form{
				{Href: ws + "/temp", Op: []any{"observeproperty"}},
				{Href: up.URL + "/temp", Op: []any{"readproperty"}},
			}},
			"mode": {Forms: []Form{{Href: up.URL + "/mode", Op: []any{"readproperty", "writeproperty"}}}},
			"hum": {Observable: true, ReadOnly: true, Forms: []Form{
				{Href: ws + "/hum", Op: []any{"observeproperty"}},
				{Href: up.URL + "/hum", Op: []any{"readproperty"}},
			}},
		},
		Actions: map[string]Action{
			"toggle": {Forms: []Form{{Href: up.URL + "/toggle", Op: "invokeaction"}}},
			"secret": {Forms: []Form{{Href: up.URL + "/forbidden", Op: "invokeaction"}}},
		},
		Events: map[string]Event{"ring": {Forms: []Form{{Href: ws + "/ring", Op: []any{"subscribeevent"}}}}},
	}
	registry.things["urn:test"] = &thingEntry{seen: time.Now(), td: td}
	hub := NewHub(registry)
	srv := httptest.NewServer(newMux(&Channel{hub: hub, auth: fa.client, registry: registry}))
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/ws"

	dial := func(token string) *testApp {
		t.Helper()
		header := http.Header{}
		if token != "" {
			header.Set("Authorization", "Bearer "+token)
		}
		conn, _, err := websocket.DefaultDialer.Dial(url, header)
		if err != nil {
			t.Fatal(err)
		}
		return &testApp{t: t, conn: conn}
	}
	salon := fa.token("salon", "", 9, time.Hour)
	parent := fa.token("salon", "profil:1", 1, 5*time.Minute)

	// A refused token closes the door; no token opens the tunnel only.
	if _, resp, err := websocket.DefaultDialer.Dial(url, http.Header{"Authorization": {"Bearer nope"}}); err == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("handshake with a bad token accepted (%v)", err)
	}
	n := dial("")
	n.push("welcome")
	if r := n.call("read", map[string]any{"thing": "urn:test", "name": "temp"}); errType(r) != failRefused {
		t.Errorf("read without a token = %v, want refused", r)
	}
	r := n.call("tunnelOpen", nil)
	tun, _ := r["result"].(map[string]any)["tunnel"].(float64)
	n.call("tunnelData", map[string]any{"tunnel": tun, "value": base64.StdEncoding.EncodeToString([]byte("hello"))})
	if got := n.push("tunnel"); got["data"] != base64.StdEncoding.EncodeToString([]byte("hello")) {
		t.Errorf("tunnel echo = %v", got)
	}
	// The token obtained through the tunnel opens the rest.
	if r := n.call("token", map[string]any{"value": salon}); r["result"] != true {
		t.Fatalf("token = %v", r)
	}
	if w := n.push("welcome"); w["level"] != float64(9) {
		t.Errorf("welcome after token = %v", w)
	}
	n.push("resources")
	if r := n.call("token", map[string]any{"value": fa.token("cuisine", "", 9, time.Hour)}); errType(r) != failRefused {
		t.Errorf("another terminal's token = %v, want refused", r)
	}
	if r := n.call("subscribe", map[string]any{"thing": "urn:test", "name": "ring"}); errType(r) != failRefused {
		t.Errorf("subscribe at level 9 = %v, want refused", r)
	}
	journaled := func() []string { linesMu.Lock(); defer linesMu.Unlock(); return slices.Clone(lines) }
	want := []string{"relay.refused token", "relay.connect", "relay.refused terminal", "relay.denied step 7 urn:test"}
	for deadline := time.Now().Add(time.Second); len(journaled()) < len(want) && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	if got := journaled(); !slices.Equal(got[:min(len(got), len(want))], want) {
		t.Errorf("journal = %v, want %v first", got, want)
	}

	// Terminal token: reads, never acts (the Thing refuses, on its token).
	a := dial(salon)
	a.push("welcome")
	if r := a.call("read", map[string]any{"thing": "urn:test", "name": "temp"}); r["result"] != float64(42) {
		t.Errorf("read = %v", r)
	}
	if r := a.call("invoke", map[string]any{"thing": "urn:test", "name": "toggle"}); errType(r) != failRefused {
		t.Errorf("invoke at level 9 = %v, want refused", r)
	}

	// Signed in (the app sends its new token): commands, with typed errors.
	a.call("token", map[string]any{"value": parent})
	a.push("welcome")
	if r := a.call("invoke", map[string]any{"thing": "urn:test", "name": "toggle"}); r["result"] != "done" {
		t.Errorf("invoke = %v", r)
	}
	if r := a.call("write", map[string]any{"thing": "urn:test", "name": "mode", "value": 1}); r["result"] != nil || r["error"] != nil {
		t.Errorf("write = %v", r)
	}
	if r := a.call("write", map[string]any{"thing": "urn:test", "name": "temp", "value": 1}); errType(r) != failRefused {
		t.Errorf("write read-only = %v, want refused", r)
	}
	if r := a.call("invoke", map[string]any{"thing": "urn:test", "name": "secret", "confirm": "c"}); errType(r) != failRefused {
		t.Errorf("Thing answering 403 = %v, want refused", r)
	}
	if thing.confirm.Load() != "c" {
		t.Errorf("X-Eria-Confirm not passed on")
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
	a.next(isValue(42))
	entree := fa.token("entree", "", 9, time.Hour)
	b := dial(entree)
	b.push("welcome")
	b.call("observe", map[string]any{"thing": "urn:test", "name": "temp"})
	b.next(isValue(42))
	if r := b.call("subscribe", map[string]any{"thing": "urn:test", "name": "ring"}); errType(r) != failRefused {
		t.Errorf("level 9 subscribes to a level 1 event = %v, want refused", r)
	}
	thing.emit("43")
	a.next(isValue(43))
	b.next(isValue(43))
	if n := thing.open.Load(); n != 1 {
		t.Fatalf("%d upstream sockets for 2 apps, want 1", n)
	}

	// Events: pushed, never replayed.
	a.call("subscribe", map[string]any{"thing": "urn:test", "name": "ring"})
	<-thing.connected
	thing.emit(`"ding"`)
	a.next(func(m map[string]any) bool {
		return m["push"] == "event" && m["event"] == "ring" && m["value"] == "ding"
	})

	// The Thing refuses the observation (minimal mode, step 5b-2): read
	// periodically with the app's own token, marked polled; observed again
	// once the Thing takes it.
	pollEvery, retryObserve = 20*time.Millisecond, 150*time.Millisecond
	thing.refuseObserve.Store(true)
	b.call("observe", map[string]any{"thing": "urn:test", "name": "hum"})
	b.next(func(m map[string]any) bool { return m["property"] == "hum" && m["polled"] == true && m["value"] == float64(42) })
	if got := thing.bearer.Load(); got != "Bearer "+entree {
		t.Errorf("polled with %v, want the app's token", got)
	}
	thing.refuseObserve.Store(false)
	<-thing.connected
	thing.emit("44")
	b.next(func(m map[string]any) bool { return m["property"] == "hum" && m["polled"] == nil && m["value"] == float64(44) })

	// Pages: auth decides on the person's token, every app gets the new version.
	page := map[string]any{"title": "Entrée", "layout": "grid", "elements": []map[string]any{
		{"id": "e1", "type": "action", "thing": "urn:test", "name": "toggle"}}}
	if r := b.call("savePage", map[string]any{"value": page}); errType(r) != failRefused {
		t.Errorf("terminal token saves a page = %v, want refused", r)
	}
	if r := a.call("savePage", map[string]any{"value": page}); r["result"] == nil {
		t.Fatalf("savePage = %v", r)
	}
	if len(fa.saved) != 1 || fa.saved[0] != parent {
		t.Errorf("auth got %d saves, not with the person's token", len(fa.saved))
	}
	fa.sync()
	if r := b.call("page", map[string]any{"value": "p1"}); r["result"].(map[string]any)["title"] != "Entrée" {
		t.Errorf("page = %v", r)
	}
	if r := b.call("themes", nil); errType(r) != failRefused {
		t.Errorf("level 9 lists themes = %v, want refused", r)
	}
	if r := a.call("things", nil); r["error"] != nil {
		t.Errorf("parent lists Things = %v", r)
	}

	// can follows the level.
	invokes := func(app *testApp) int {
		services, _ := app.call("services", nil)["result"].([]any)
		svc, _ := services[0].(map[string]any)
		can, _ := svc["can"].(map[string]any)
		list, _ := can["invoke"].([]any)
		return len(list)
	}
	if n := invokes(b); n != 0 {
		t.Errorf("level 9 may invoke %d actions, want 0", n)
	}
	if n := invokes(a); n != 2 {
		t.Errorf("parent may invoke %d actions, want 2 (one with a confirmation)", n)
	}

	// Last app gone: the upstream sockets are closed.
	a.conn.Close()
	b.call("unobserve", map[string]any{"thing": "urn:test", "name": "temp"})
	b.call("unobserve", map[string]any{"thing": "urn:test", "name": "hum"})
	deadline := time.Now().Add(2 * time.Second)
	for thing.open.Load() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("upstream socket still open after the last unobserve")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Hidden Things are not offered.
	registry.SetHide([]string{"urn:test"})
	if r := b.call("read", map[string]any{"thing": "urn:test", "name": "temp"}); errType(r) != failInvalid {
		t.Errorf("read a hidden Thing = %v, want invalid", r)
	}

	// A revoked terminal is disconnected.
	fa.mu.Lock()
	fa.revoke = []auth.Revocation{{Subject: "term:entree", Before: time.Now().Unix() + 1}}
	fa.mu.Unlock()
	fa.client.pollRevocations(hub.revalidate)
	closed(t, b)

	// A token not renewed closes the connection at its exp (+ grace).
	expiryGrace = 0
	short := dial(fa.token("bureau", "", 9, time.Second))
	short.push("welcome")
	closed(t, short)
}

// closed waits until the relay closes the app's connection.
func closed(t *testing.T, a *testApp) {
	t.Helper()
	a.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, _, err := a.conn.ReadMessage()
		if err == nil {
			continue
		}
		if ne, ok := err.(interface{ Timeout() bool }); ok && ne.Timeout() {
			t.Fatal("connection still open")
		}
		return
	}
}
