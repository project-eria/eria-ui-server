package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestNotices(t *testing.T) {
	retryMin = 20 * time.Millisecond
	marked := make(chan string, 1)
	upstream := make(chan *websocket.Conn, 1)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case websocket.IsWebSocketUpgrade(r):
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			upstream <- conn
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		case r.URL.Path == "/notices":
			w.Write([]byte(`[{"id":"2","key":"garage","audience":"profile","title":"Garage"},{"id":"1","key":"eau","audience":"all","title":"Eau"}]`))
		case r.URL.Path == "/read/1":
			w.Write([]byte(`["2"]`))
		case r.URL.Path == "/markRead":
			body, _ := io.ReadAll(r.Body)
			marked <- string(body)
		}
	}))
	defer up.Close()
	ws := "ws" + strings.TrimPrefix(up.URL, "http")

	auth, err := OpenAuth(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	auth.Bootstrap("1234")
	registry := NewRegistry(nil)
	registry.things["urn:eria:service:notices"] = &thingEntry{seen: time.Now(), td: TD{
		ID: "urn:eria:service:notices",
		Properties: map[string]Property{
			"notices": {Observable: true, ReadOnly: true, Forms: []Form{
				{Href: ws + "/notices", Op: []any{"observeproperty"}},
				{Href: up.URL + "/notices", Op: []any{"readproperty"}},
			}},
			"read": {ReadOnly: true, Forms: []Form{{Href: up.URL + "/read/{profile}", Op: []any{"readproperty"}}}},
		},
		Actions: map[string]Action{"markRead": {Forms: []Form{{Href: up.URL + "/markRead", Op: "invokeaction"}}}},
	}}
	hub := NewHub(registry)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go hub.watchNotices(ctx, "urn:eria:service:notices")
	srv := httptest.NewServer(newMux(&Channel{hub: hub, auth: auth, registry: registry, noticesThing: "urn:eria:service:notices"}))
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/api/ws", http.Header{"X-Eria-Terminal": {"terminal-one-secret-0000000"}})
	if err != nil {
		t.Fatal(err)
	}
	a := &testApp{t: t, conn: conn}
	a.push("welcome")

	titles := func(r map[string]any) (out []string) {
		list, _ := r["result"].([]any)
		for _, n := range list {
			m := n.(map[string]any)
			if m["key"] != nil {
				t.Errorf("key given to the app: %v", m)
			}
			out = append(out, m["title"].(string)+map[bool]string{true: "+read"}[m["read"] == true])
		}
		return out
	}

	// Not signed in: the "all" notices only, never read, and no markRead.
	if got := titles(a.call("notices", nil)); len(got) != 1 || got[0] != "Eau" {
		t.Errorf("notices without session = %v", got)
	}
	if r := a.call("markRead", map[string]any{"value": []string{"1"}}); errType(r) != failRefused {
		t.Errorf("markRead without session = %v, want refused", r)
	}

	a.call("login", map[string]any{"profile": 1, "pin": "1234"})
	if got := titles(a.call("notices", nil)); len(got) != 2 || got[0] != "Garage+read" || got[1] != "Eau" {
		t.Errorf("notices of profile 1 = %v", got)
	}
	if r := a.call("markRead", map[string]any{"value": []string{"1"}}); r["result"] != true {
		t.Fatalf("markRead = %v", r)
	}
	var body struct {
		Profile string
		IDs     []string
	}
	json.Unmarshal([]byte(<-marked), &body)
	if body.Profile != "1" || len(body.IDs) != 1 || body.IDs[0] != "1" {
		t.Errorf("markRead sent %+v", body)
	}
	a.push("notices-changed")

	// A change of the service's notices reaches every app.
	(<-upstream).WriteMessage(websocket.TextMessage, []byte(`[]`))
	a.push("notices-changed")
}
