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

// Hub multiplexes property observations: every browser holds one WebSocket,
// and each observed (Thing, property) has a single upstream WebSocket,
// shared by all browsers and closed when nobody watches it anymore.
type Hub struct {
	registry *Registry
	mu       sync.Mutex
	feeds    map[feedKey]*feed
	clients  map[*client]bool
}

type feedKey struct{ Thing, Property string }

type feed struct {
	subs   map[*client]bool
	value  json.RawMessage // last known value, sent to late subscribers
	cancel context.CancelFunc
}

type client struct {
	send  chan []byte
	feeds map[feedKey]bool
}

// Browser <-> backend messages.
type clientMsg struct {
	Op       string `json:"op"` // observe | unobserve
	Thing    string `json:"thing"`
	Property string `json:"property"`
}

type valueMsg struct {
	Thing    string          `json:"thing"`
	Property string          `json:"property"`
	Value    json.RawMessage `json:"value"`
}

type onlineMsg struct {
	Thing  string `json:"thing"`
	Online bool   `json:"online"`
}

const (
	pingEvery     = 30 * time.Second
	pongWait      = 70 * time.Second
	maxClientMsg  = 4 << 10
	maxClientSubs = 500
	sendBuffer    = 256
)

// Overridable by tests.
var (
	retryMin    = time.Second
	retryMax    = 30 * time.Second
	statusEvery = 5 * time.Second
)

// Default CheckOrigin rejects cross-origin pages (CSWSH).
var upgrader = websocket.Upgrader{}

var (
	errUnknownThing = errors.New("unknown Thing")
	errBadValue     = errors.New("can't read current value")
)

func NewHub(registry *Registry) *Hub {
	return &Hub{registry: registry, feeds: map[feedKey]*feed{}, clients: map[*client]bool{}}
}

// ServeWS handles one browser connection.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already answered the request
	}
	c := &client{send: make(chan []byte, sendBuffer), feeds: map[feedKey]bool{}}
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
	go h.writeLoop(conn, c)
	h.readLoop(conn, c)
	h.drop(c)
}

func (h *Hub) readLoop(conn *websocket.Conn, c *client) {
	conn.SetReadLimit(maxClientMsg)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(pongWait)) })
	for {
		var msg clientMsg
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		k := feedKey{msg.Thing, msg.Property}
		switch msg.Op {
		case "observe":
			if errMsg := h.observe(c, k); errMsg != "" {
				h.push(c, map[string]string{"error": errMsg, "thing": k.Thing, "property": k.Property})
			}
		case "unobserve":
			h.unobserve(c, k)
		default:
			h.push(c, map[string]string{"error": "unknown op"})
		}
	}
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

// observe subscribes a client; it returns an error text for the browser.
func (h *Hub) observe(c *client, k feedKey) string {
	// A Thing not discovered yet (backend just restarted...) is accepted:
	// follow() keeps retrying until it shows up.
	td, ok := h.registry.Thing(k.Thing)
	p, exists := td.Properties[k.Property]
	switch {
	case !ok:
	case !exists:
		return "unknown property"
	case !p.Observable || href(p.Forms, "observeproperty") == "":
		return "property not observable"
	case len(p.UriVariables) > 0:
		return "uriVariables not supported"
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.clients[c] || c.feeds[k] {
		return "" // dropped client, or already observing
	}
	if len(c.feeds) >= maxClientSubs {
		return "too many observations"
	}
	f, running := h.feeds[k]
	if !running {
		ctx, cancel := context.WithCancel(context.Background())
		f = &feed{subs: map[*client]bool{}, cancel: cancel}
		h.feeds[k] = f
		go h.follow(ctx, k)
	}
	f.subs[c] = true
	c.feeds[k] = true
	if f.value != nil {
		h.pushLocked(c, valueMsg{k.Thing, k.Property, f.value})
	}
	return ""
}

func (h *Hub) unobserve(c *client, k feedKey) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.unobserveLocked(c, k)
}

func (h *Hub) unobserveLocked(c *client, k feedKey) {
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
	for k := range c.feeds {
		h.unobserveLocked(c, k)
	}
	if h.clients[c] {
		delete(h.clients, c)
		close(c.send)
	}
}

// follow keeps one upstream observation alive until ctx is cancelled.
// The TD is looked up at each attempt, so a Thing that moved is followed.
func (h *Hub) follow(ctx context.Context, k feedKey) {
	wait := retryMin
	for ctx.Err() == nil {
		start := time.Now()
		if err := h.observeUpstream(ctx, k); err != nil && ctx.Err() == nil {
			zlog.Debug().Err(err).Str("thing", k.Thing).Str("property", k.Property).Msg("[hub:follow] upstream lost")
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

func (h *Hub) observeUpstream(ctx context.Context, k feedKey) error {
	td, ok := h.registry.Thing(k.Thing)
	if !ok {
		return errUnknownThing
	}
	p := td.Properties[k.Property]
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, href(p.Forms, "observeproperty"), nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(pongWait)) })
	go keepAlive(ctx, conn)

	// go-wot sends changes only: read the current value once connected.
	if v, err := readValue(ctx, href(p.Forms, "readproperty")); err == nil {
		h.publish(k, v)
	}
	for {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var upstreamErr struct{ Error string }
		if json.Unmarshal(data, &upstreamErr) == nil && upstreamErr.Error != "" {
			zlog.Warn().Str("thing", k.Thing).Str("property", k.Property).Str("error", upstreamErr.Error).Msg("[hub:observeUpstream]")
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

func (h *Hub) publish(k feedKey, value json.RawMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f, ok := h.feeds[k]
	if !ok {
		return // nobody watches anymore
	}
	f.value = value
	msg, _ := json.Marshal(valueMsg{k.Thing, k.Property, value})
	for c := range f.subs {
		h.sendLocked(c, msg)
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

// sendLocked never blocks: a browser too slow to keep up is disconnected.
func (h *Hub) sendLocked(c *client, msg []byte) {
	if !h.clients[c] {
		return
	}
	select {
	case c.send <- msg:
	default:
		zlog.Warn().Msg("[hub:send] slow client dropped")
		for k := range c.feeds {
			h.unobserveLocked(c, k)
		}
		delete(h.clients, c)
		close(c.send)
	}
}

// WatchStatus tells browsers when a Thing goes on/offline, and when the
// list of Things changes (they then re-read /api/services).
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
				b, _ := json.Marshal(onlineMsg{id, online})
				msgs = append(msgs, b)
			}
		}
		if !maps.EqualFunc(prev, cur, func(bool, bool) bool { return true }) {
			msgs = append(msgs, []byte(`{"op":"things-changed"}`))
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
