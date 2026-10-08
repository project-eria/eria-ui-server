package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeThing mimics a go-wot property: HTTP read + raw-JSON WebSocket frames.
type fakeThing struct {
	open      atomic.Int32 // upstream WebSockets currently open
	mu        sync.Mutex
	conns     []*websocket.Conn
	connected chan struct{}
}

func (f *fakeThing) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !websocket.IsWebSocketUpgrade(r) {
		w.Write([]byte("42"))
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

func TestHubSharesUpstream(t *testing.T) {
	retryMin = 20 * time.Millisecond
	thing := &fakeThing{connected: make(chan struct{}, 10)}
	up := httptest.NewServer(thing)
	defer up.Close()

	registry := NewRegistry(nil)
	registry.things["urn:test"] = &thingEntry{seen: time.Now(), td: TD{
		ID: "urn:test",
		Properties: map[string]Property{"temp": {Observable: true, Forms: []Form{
			{Href: "ws" + strings.TrimPrefix(up.URL, "http") + "/temp", Op: []any{"observeproperty"}},
			{Href: up.URL + "/temp", Op: []any{"readproperty"}},
		}}},
	}}
	hub := NewHub(registry)
	srv := httptest.NewServer(newMux(registry, hub))
	defer srv.Close()

	dial := func() *websocket.Conn {
		c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/api/ws", nil)
		if err != nil {
			t.Fatal(err)
		}
		c.WriteJSON(clientMsg{Op: "observe", Thing: "urn:test", Property: "temp"})
		return c
	}
	expect := func(c *websocket.Conn, want string) {
		t.Helper()
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, got, err := c.ReadMessage()
		if err != nil || string(got) != want {
			t.Fatalf("got %s (%v), want %s", got, err, want)
		}
	}
	const v42 = `{"thing":"urn:test","property":"temp","value":42}`

	a := dial()
	<-thing.connected
	expect(a, v42) // current value read over HTTP once connected
	b := dial()
	expect(b, v42) // late subscriber gets the cached value

	thing.emit("43")
	expect(a, `{"thing":"urn:test","property":"temp","value":43}`)
	expect(b, `{"thing":"urn:test","property":"temp","value":43}`)
	if n := thing.open.Load(); n != 1 {
		t.Fatalf("%d upstream sockets for 2 browsers, want 1", n)
	}

	// Unknown property of a known Thing: error, no upstream socket.
	a.WriteJSON(clientMsg{Op: "observe", Thing: "urn:test", Property: "nope"})
	expect(a, `{"error":"unknown property","property":"nope","thing":"urn:test"}`)

	// Thing not discovered yet: accepted, followed once it shows up.
	late := dial()
	late.WriteJSON(clientMsg{Op: "observe", Thing: "urn:later", Property: "temp"})
	time.Sleep(50 * time.Millisecond)
	registry.mu.Lock()
	registry.things["urn:later"] = &thingEntry{seen: time.Now(), td: registry.things["urn:test"].td}
	registry.mu.Unlock()
	<-thing.connected
	late.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, got, err := late.ReadMessage()
		if err != nil {
			t.Fatalf("no value for the late Thing: %v", err)
		}
		if strings.Contains(string(got), `"urn:later"`) {
			break
		}
	}
	late.Close()

	// Last browser gone: the upstream socket is closed.
	a.Close()
	b.WriteJSON(clientMsg{Op: "unobserve", Thing: "urn:test", Property: "temp"})
	deadline := time.Now().Add(2 * time.Second)
	for thing.open.Load() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("upstream socket still open after the last unobserve")
		}
		time.Sleep(10 * time.Millisecond)
	}
	b.Close()
}
