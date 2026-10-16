package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	zlog "github.com/rs/zerolog/log"
)

// Hub multiplexes observations: every app holds one WebSocket (channel.go),
// and each observed property or subscribed event of a Thing has a single
// upstream WebSocket, shared by all apps and closed when nobody follows it.
type Hub struct {
	registry *Registry
	mu       sync.Mutex
	feeds    map[feedKey]*feed
	clients  map[*client]bool
}

const (
	kindProperty = "property"
	kindEvent    = "event"
)

type feedKey struct{ Thing, Kind, Name string }

type feed struct {
	subs   map[*client]bool
	value  json.RawMessage // last known property value, sent to late subscribers
	cancel context.CancelFunc
}

type client struct {
	send      chan []byte
	feeds     map[feedKey]bool
	terminal  string // public id of the terminal on the other end
	resources func() // pushes the resource manifest again (channel.go)
}

// pushMsg is a message the server sends without being asked.
type pushMsg struct {
	Push     string          `json:"push"` // value | event | online | things-changed | …
	Thing    string          `json:"thing,omitempty"`
	Property string          `json:"property,omitempty"`
	Event    string          `json:"event,omitempty"`
	Value    json.RawMessage `json:"value,omitempty"` // property value or event data
	Online   *bool           `json:"online,omitempty"`
}

const (
	pingEvery     = 30 * time.Second
	pongWait      = 70 * time.Second
	maxClientMsg  = 64 << 10
	maxClientSubs = 500
	sendBuffer    = 256
)

// Overridable by tests.
var (
	retryMin    = time.Second
	retryMax    = 30 * time.Second
	statusEvery = 5 * time.Second
)

// Default CheckOrigin rejects cross-origin pages (CSWSH); the app's Rust
// client sends no Origin.
var upgrader = websocket.Upgrader{}

var (
	errUnknownThing = errors.New("unknown Thing")
	errBadValue     = errors.New("can't read current value")
)

func NewHub(registry *Registry) *Hub {
	return &Hub{registry: registry, feeds: map[feedKey]*feed{}, clients: map[*client]bool{}}
}

func (h *Hub) attach(terminal string) *client {
	c := &client{send: make(chan []byte, sendBuffer), feeds: map[feedKey]bool{}, terminal: terminal}
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
	return c
}

func (h *Hub) writeLoop(conn *websocket.Conn, c *client) {
	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	defer conn.Close()
	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ping.C:
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// follow subscribes a client to a property or an event.
func (h *Hub) follow(c *client, k feedKey) *opError {
	// A Thing not discovered yet (server just restarted...) is accepted:
	// keepFollowing() retries until it shows up.
	if td, ok := h.registry.Thing(k.Thing); ok {
		if err := checkFollow(td, k); err != nil {
			return err
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.clients[c] || c.feeds[k] {
		return nil // dropped client, or already following
	}
	if len(c.feeds) >= maxClientSubs {
		return fail(failInvalid, "too many observations")
	}
	f, running := h.feeds[k]
	if !running {
		ctx, cancel := context.WithCancel(context.Background())
		f = &feed{subs: map[*client]bool{}, cancel: cancel}
		h.feeds[k] = f
		go h.keepFollowing(ctx, k)
	}
	f.subs[c] = true
	c.feeds[k] = true
	if f.value != nil {
		h.pushLocked(c, pushMsg{Push: "value", Thing: k.Thing, Property: k.Name, Value: f.value})
	}
	return nil
}

func checkFollow(td TD, k feedKey) *opError {
	if k.Kind == kindEvent {
		e, exists := td.Events[k.Name]
		switch {
		case !exists:
			return fail(failInvalid, "unknown event")
		case href(e.Forms, "subscribeevent") == "":
			return fail(failInvalid, "event without WebSocket form")
		}
		return nil
	}
	p, exists := td.Properties[k.Name]
	switch {
	case !exists:
		return fail(failInvalid, "unknown property")
	case !p.Observable || href(p.Forms, "observeproperty") == "":
		return fail(failInvalid, "property not observable")
	case len(p.UriVariables) > 0:
		return fail(failInvalid, "uriVariables not supported")
	}
	return nil
}

func (h *Hub) unfollow(c *client, k feedKey) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.unfollowLocked(c, k)
}

func (h *Hub) unfollowLocked(c *client, k feedKey) {
	delete(c.feeds, k)
	f, ok := h.feeds[k]
	if !ok {
		return
	}
	delete(f.subs, c)
	if len(f.subs) == 0 {
		f.cancel()
		delete(h.feeds, k)
	}
}

func (h *Hub) drop(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dropLocked(c)
}

func (h *Hub) dropLocked(c *client) {
	for k := range c.feeds {
		h.unfollowLocked(c, k)
	}
	if h.clients[c] {
		delete(h.clients, c)
		close(c.send)
	}
}

// keepFollowing keeps one upstream WebSocket alive until ctx is cancelled.
// The TD is looked up at each attempt, so a Thing that moved is followed.
func (h *Hub) keepFollowing(ctx context.Context, k feedKey) {
	wait := retryMin
	for ctx.Err() == nil {
		start := time.Now()
		if err := h.followUpstream(ctx, k); err != nil && ctx.Err() == nil {
			zlog.Debug().Err(err).Str("thing", k.Thing).Str(k.Kind, k.Name).Msg("[hub:keepFollowing] upstream lost")
		}
		if time.Since(start) > retryMax {
			wait = retryMin // it was up for a while: retry quickly
		}
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
		wait = min(wait*2, retryMax)
	}
}

func (h *Hub) followUpstream(ctx context.Context, k feedKey) error {
	td, ok := h.registry.Thing(k.Thing)
	if !ok {
		return errUnknownThing
	}
	var url string
	if k.Kind == kindEvent {
		url = href(td.Events[k.Name].Forms, "subscribeevent")
	} else {
		url = href(td.Properties[k.Name].Forms, "observeproperty")
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(pongWait)) })
	go keepAlive(ctx, conn)

	// go-wot sends changes only: read the current value once connected.
	if k.Kind == kindProperty {
		if v, err := readValue(ctx, href(td.Properties[k.Name].Forms, "readproperty")); err == nil {
			h.publish(k, v)
		}
	}
	for {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var upstreamErr struct{ Error string }
		if json.Unmarshal(data, &upstreamErr) == nil && upstreamErr.Error != "" {
			zlog.Warn().Str("thing", k.Thing).Str(k.Kind, k.Name).Str("error", upstreamErr.Error).Msg("[hub:followUpstream]")
			continue
		}
		h.publish(k, data)
	}
}

func keepAlive(ctx context.Context, conn *websocket.Conn) {
	t := time.NewTicker(pingEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)) != nil {
				return
			}
		}
	}
}

func readValue(ctx context.Context, url string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := upstream.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil || resp.StatusCode != http.StatusOK || !json.Valid(body) {
		return nil, errBadValue
	}
	return body, nil
}

// publish sends a property value (kept for late subscribers) or an event
// (never replayed) to everyone following it.
func (h *Hub) publish(k feedKey, value json.RawMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f, ok := h.feeds[k]
	if !ok {
		return // nobody follows anymore
	}
	msg := pushMsg{Push: "value", Thing: k.Thing, Property: k.Name, Value: value}
	if k.Kind == kindEvent {
		msg = pushMsg{Push: "event", Thing: k.Thing, Event: k.Name, Value: value}
	} else {
		f.value = value
	}
	data, _ := json.Marshal(msg)
	for c := range f.subs {
		h.sendLocked(c, data)
	}
}

func (h *Hub) push(c *client, v any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pushLocked(c, v)
}

func (h *Hub) pushLocked(c *client, v any) {
	msg, _ := json.Marshal(v)
	h.sendLocked(c, msg)
}

// broadcast pushes to every app, or to the apps of one terminal.
func (h *Hub) broadcast(terminal string, v any) {
	msg, _ := json.Marshal(v)
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if terminal == "" || c.terminal == terminal {
			h.sendLocked(c, msg)
		}
	}
}

// refresh pushes the resource manifest again to every app, or to the apps
// of one terminal.
func (h *Hub) refresh(terminal string) {
	h.mu.Lock()
	var todo []func()
	for c := range h.clients {
		if c.resources != nil && (terminal == "" || c.terminal == terminal) {
			todo = append(todo, c.resources)
		}
	}
	h.mu.Unlock()
	for _, f := range todo {
		f() // outside the lock: it reads the database, then pushes
	}
}

// disconnect closes every connection of a terminal (forgotten by the admin).
func (h *Hub) disconnect(terminal string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.terminal == terminal {
			h.dropLocked(c)
		}
	}
}

// sendLocked never blocks: an app too slow to keep up is disconnected.
func (h *Hub) sendLocked(c *client, msg []byte) {
	if !h.clients[c] {
		return
	}
	select {
	case c.send <- msg:
	default:
		zlog.Warn().Str("terminal", c.terminal).Msg("[hub:send] slow client dropped")
		h.dropLocked(c)
	}
}

// WatchStatus tells apps when a Thing goes on/offline, and when the list of
// Things changes (they then ask the services again).
func (h *Hub) WatchStatus(ctx context.Context) {
	t := time.NewTicker(statusEvery)
	defer t.Stop()
	prev := h.registry.Status()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		cur := h.registry.Status()
		var msgs [][]byte
		for id, online := range cur {
			if was, known := prev[id]; known && was != online {
				b, _ := json.Marshal(pushMsg{Push: "online", Thing: id, Online: &online})
				msgs = append(msgs, b)
			}
		}
		if !maps.EqualFunc(prev, cur, func(bool, bool) bool { return true }) {
			msgs = append(msgs, []byte(`{"push":"things-changed"}`))
		}
		prev = cur
		if len(msgs) == 0 {
			continue
		}
		h.mu.Lock()
		for c := range h.clients {
			for _, m := range msgs {
				h.sendLocked(c, m)
			}
		}
		h.mu.Unlock()
	}
}
