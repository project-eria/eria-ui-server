package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/project-eria/go-wot/auth"
	"github.com/project-eria/go-wot/auth/authz"
	zlog "github.com/rs/zerolog/log"
)

// The channel is the single WebSocket between an app and the relay (PLAN
// §2.2, AUTH-DESIGN §3.6): commands, observations, events, settings, and the
// tunnel to auth all go through it.
//
// Handshake: Authorization: Bearer <token> (terminal or person, issued by
// auth). Without a token, only the tunnel to auth is open: the app enrolls
// and gets its first token there, then sends it with op "token". The app
// sends requests {id, op, …}; each gets one reply {id, result} or
// {id, error: {type, message}}. Values, events and status are pushed
// ({push: …}), starting with {push: "welcome", terminal}.

// request is one message from the app; fields depend on the op.
type request struct {
	ID     json.RawMessage `json:"id"`
	Op     string          `json:"op"`
	Thing  string          `json:"thing,omitempty"`
	Name   string          `json:"name,omitempty"`  // property, action or event
	Value  json.RawMessage `json:"value,omitempty"` // value to write, action input, op argument
	Tunnel int             `json:"tunnel,omitempty"`
	// Confirm is the confirm token of a confirmed command (§2.8), passed to
	// the Thing as X-Eria-Confirm.
	Confirm string `json:"confirm,omitempty"`
}

type reply struct {
	ID     json.RawMessage `json:"id"`
	Result any             `json:"result,omitempty"`
	Error  *opError        `json:"error,omitempty"`
}

// Error types the app reacts to (PLAN §2.2).
const (
	failOffline = "offline" // the Thing does not answer
	failRefused = "refused" // not allowed (token, level, Thing)
	failTimeout = "timeout"
	failInvalid = "invalid" // unknown op, Thing or name, bad value
	failFailed  = "failed"  // the Thing answered with an error
)

type opError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func fail(kind, message string) *opError { return &opError{Type: kind, Message: message} }

const (
	maxInflight = 16 // requests an app runs at once
	maxTunnels  = 4  // tunnels to auth an app holds at once
	notSignedIn = 9
)

// expiryGrace is the tolerance after a token's exp before the connection
// closes (§3.6): the app renews its token before. Overridable by tests.
var expiryGrace = 30 * time.Second

type Channel struct {
	hub      *Hub
	auth     *AuthClient
	registry *Registry
	themes   string // folder of the signed theme archives
	// TD id of service-notices
	noticesThing string
}

// session is the state of one connection.
type session struct {
	ch       *Channel
	client   *client
	conn     *websocket.Conn
	mu       sync.Mutex
	claims   *auth.Claims // nil until a token is accepted
	token    string
	expiry   *time.Timer
	tunnels  map[int]net.Conn
	nextTun  int
	inflight chan struct{}
	rate     *bucket
}

func (ch *Channel) ServeWS(w http.ResponseWriter, r *http.Request) {
	s := &session{ch: ch, inflight: make(chan struct{}, maxInflight), tunnels: map[int]net.Conn{}, rate: newBucket(20, 40)}
	if token := bearer(r); token != "" {
		c, err := ch.auth.Authenticate(token)
		if err != nil {
			http.Error(w, "token refused", http.StatusUnauthorized)
			return
		}
		s.claims, s.token = &c, token
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already answered the request
	}
	s.conn = conn
	term := ""
	if s.claims != nil {
		term = s.claims.Term
	}
	s.client = ch.hub.attach(term)
	ch.hub.mu.Lock()
	s.client.revalidate = s.revalidate
	s.client.token = func() string { _, t := s.current(); return t }
	ch.hub.mu.Unlock()
	go ch.hub.writeLoop(conn, s.client)
	s.welcome()
	s.readLoop(conn)
	s.closeTunnels()
	if s.expiry != nil {
		s.expiry.Stop()
	}
	ch.hub.drop(s.client)
}

// welcome tells the app who it is, then gives its resources once it holds a
// token.
func (s *session) welcome() {
	s.mu.Lock()
	c := s.claims
	if c != nil {
		if s.expiry != nil {
			s.expiry.Stop()
		}
		s.expiry = time.AfterFunc(time.Until(time.Unix(c.Exp, 0))+expiryGrace, func() { s.conn.Close() })
	}
	s.mu.Unlock()
	if c == nil {
		s.ch.hub.push(s.client, map[string]any{"push": "welcome"})
		return
	}
	t := s.ch.auth.Terminal(c.Term)
	s.ch.hub.push(s.client, map[string]any{"push": "welcome", "terminal": t, "level": *c.Lvl, "signedIn": c.Sub != ""})
	s.ch.hub.mu.Lock()
	s.client.resources = s.pushResources
	s.ch.hub.mu.Unlock()
	s.pushResources()
}

// revalidate closes the connection whose token the new revocation list makes
// fall (§2.10).
func (s *session) revalidate() {
	s.mu.Lock()
	c := s.claims
	s.mu.Unlock()
	if c != nil && s.ch.auth.Revokes(*c) {
		s.conn.Close()
	}
}

func (s *session) readLoop(conn *websocket.Conn) {
	conn.SetReadLimit(maxClientMsg)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(pongWait)) })
	for {
		var req request
		if err := conn.ReadJSON(&req); err != nil {
			var syntax *json.SyntaxError
			if errors.As(err, &syntax) {
				continue // ponytail: a malformed frame is dropped without reply
			}
			return
		}
		select {
		case s.inflight <- struct{}{}:
			go func() {
				defer func() { <-s.inflight }()
				result, err := s.handle(req)
				if err != nil {
					result = nil
				}
				s.ch.hub.push(s.client, reply{ID: req.ID, Result: result, Error: err})
			}()
		default:
			s.ch.hub.push(s.client, reply{ID: req.ID, Error: fail(failInvalid, "too many requests at once")})
		}
	}
}

// pushResources sends the resource manifest (PLAN §5.000): every page with
// its version, and the themes they use with their archive hash. The app
// downloads what changed and keeps a local copy.
func (s *session) pushResources() {
	pages, used := s.ch.auth.Manifest()
	themes := []map[string]string{}
	for _, id := range used {
		themes = append(themes, map[string]string{"id": id, "hash": themeHash(s.ch.themes, id)})
	}
	s.ch.hub.push(s.client, map[string]any{"push": "resources", "pages": pages, "themes": themes,
		"homePage": s.ch.auth.HomePage()})
}

// current returns the accepted claims and token, nil when the app holds
// none.
func (s *session) current() (*auth.Claims, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.claims, s.token
}

// level is the access level of the connection: the token's, or 9.
func (s *session) level() int {
	c, _ := s.current()
	if c == nil {
		return notSignedIn
	}
	return *c.Lvl
}

func (s *session) handle(req request) (any, *opError) {
	// --- Tunnel to auth and token: before any token ---
	switch req.Op {
	case "tunnelOpen":
		return s.tunnelOpen()
	case "tunnelData":
		return s.tunnelData(req)
	case "tunnelClose":
		s.tunnelClose(req.Tunnel)
		return true, nil
	case "token":
		return s.setToken(req)
	}
	c, token := s.current()
	if c == nil {
		return nil, fail(failRefused, "a token is required")
	}
	// Rate limit per connection (G40): the requests that reach a Thing.
	switch req.Op {
	case "read", "write", "invoke", "observe", "subscribe":
		if !s.rate.take() {
			return nil, fail(failRefused, "too many requests")
		}
	}

	switch req.Op {
	case "reportType":
		var detected string
		json.Unmarshal(req.Value, &detected)
		return true, s.authError(s.ch.auth.ReportScreen(token, detected))

	// --- Services ---
	case "services":
		services := s.ch.registry.Services()
		for i := range services {
			services[i].Can = s.can(services[i])
		}
		return services, nil
	case "contexts":
		return s.ch.auth.Contexts(), nil
	case "read":
		td, p, err := s.property(req)
		if err != nil {
			return nil, err
		}
		return callThing(td, http.MethodGet, href(p.Forms, "readproperty"), nil, token, "")
	case "write":
		td, p, err := s.property(req)
		if err != nil {
			return nil, err
		}
		// People write only what the services show as writable (capability
		// state is read-only).
		if !isWritable(td, req.Name) {
			return nil, fail(failRefused, "read-only property")
		}
		if len(req.Value) == 0 {
			return nil, fail(failInvalid, "no value")
		}
		return callThing(td, http.MethodPut, href(p.Forms, "writeproperty"), req.Value, token, req.Confirm)
	case "invoke":
		td, ok := s.ch.registry.Thing(req.Thing)
		a, exists := td.Actions[req.Name]
		if !ok || !exists || !s.visible(req.Thing) {
			return nil, fail(failInvalid, "unknown Thing or action")
		}
		return callThing(td, http.MethodPost, href(a.Forms, "invokeaction"), req.Value, token, req.Confirm)

	// --- Themes (signed: the app checks them, the relay only serves them) ---
	case "theme":
		var id string
		json.Unmarshal(req.Value, &id)
		archive, err := themeArchive(s.ch.themes, id)
		return archive, invalid(err)

	// --- Pages, kept by auth ---
	case "page":
		var id string
		json.Unmarshal(req.Value, &id)
		p, err := s.ch.auth.Page(id)
		return p, invalid(err)
	case "pages":
		return s.ch.auth.Pages(), nil
	case "savePage":
		var in Page
		if json.Unmarshal(req.Value, &in) != nil {
			return nil, fail(failInvalid, "bad page")
		}
		if in.ID == "" && in.Theme != "" && themeHash(s.ch.themes, in.Theme) == "" {
			return nil, fail(failInvalid, "unknown or unreadable theme")
		}
		p, err := s.ch.auth.SavePage(token, in)
		if err != nil {
			return nil, s.authError(err)
		}
		return p, nil
	case "deletePage":
		var id string
		json.Unmarshal(req.Value, &id)
		if err := s.ch.auth.DeletePage(token, id); err != nil {
			return nil, s.authError(err)
		}
		return true, nil
	case "setHomePage":
		var id string
		json.Unmarshal(req.Value, &id)
		if err := s.ch.auth.SetHomePage(token, id); err != nil {
			return nil, s.authError(err)
		}
		return true, nil

	// --- Notifications (notices.go) ---
	case "notices":
		return s.notices()
	case "markRead":
		return s.markRead(req)

	case "observe", "unobserve", "subscribe", "unsubscribe":
		k := feedKey{req.Thing, kindProperty, req.Name}
		if req.Op == "subscribe" || req.Op == "unsubscribe" {
			k.Kind = kindEvent
		}
		if strings.HasPrefix(req.Op, "un") {
			s.ch.hub.unfollow(s.client, k)
			return true, nil
		}
		if !s.visible(req.Thing) {
			return nil, fail(failInvalid, "unknown Thing")
		}
		if !s.ch.auth.Decide(token, k.Thing, k.function(), true).Allow {
			return nil, fail(failRefused, "access denied")
		}
		if err := s.ch.hub.follow(s.client, k); err != nil {
			return nil, err
		}
		return true, nil
	}

	// --- Page editing: level 1 (the parents) ---
	if s.level() > 1 {
		return nil, fail(failRefused, "unknown op, or not allowed for this profile")
	}
	switch req.Op {
	case "themes":
		return Themes(s.ch.themes), nil
	case "things":
		return s.ch.registry.Things(), nil
	case "td":
		raw, ok := s.ch.registry.Raw(req.Thing)
		if !ok || !s.visible(req.Thing) {
			return nil, fail(failInvalid, "unknown Thing")
		}
		return raw, nil
	}
	return nil, fail(failInvalid, "unknown op")
}

// setToken takes the app's new token: the first one after enrollment or
// sign-in, then each renewal. A connection stays with one terminal.
func (s *session) setToken(req request) (any, *opError) {
	var token string
	json.Unmarshal(req.Value, &token)
	c, err := s.ch.auth.Authenticate(token)
	if err != nil {
		return nil, fail(failRefused, "token refused")
	}
	s.mu.Lock()
	first := s.claims == nil
	if !first && s.claims.Term != c.Term {
		s.mu.Unlock()
		return nil, fail(failRefused, "another terminal's token")
	}
	changed := first || s.claims.Sub != c.Sub || *s.claims.Lvl != *c.Lvl
	s.claims, s.token = &c, token
	s.mu.Unlock()
	if first {
		s.ch.hub.mu.Lock()
		s.client.terminal = c.Term
		s.ch.hub.mu.Unlock()
	}
	if changed {
		s.welcome()
	} else {
		s.mu.Lock()
		s.expiry.Reset(time.Until(time.Unix(c.Exp, 0)) + expiryGrace)
		s.mu.Unlock()
	}
	return true, nil
}

// authError turns auth's refusal of a change into the app's error.
func (s *session) authError(err error) *opError {
	var e *authError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &e) && (e.Status == http.StatusForbidden || e.Status == http.StatusUnauthorized):
		return fail(failRefused, "not allowed for this profile")
	case errors.As(err, &e):
		return fail(failInvalid, e.Reason)
	}
	return internal(err)
}

// --- Tunnel to auth: bytes of the app's own TLS session, never read here ---

func (s *session) tunnelOpen() (any, *opError) {
	s.mu.Lock()
	if len(s.tunnels) >= maxTunnels {
		s.mu.Unlock()
		return nil, fail(failInvalid, "too many tunnels")
	}
	s.nextTun++
	id := s.nextTun
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := s.ch.auth.Tunnel(ctx)
	if err != nil {
		zlog.Warn().Err(err).Msg("[channel:tunnel] auth unreachable")
		return nil, fail(failOffline, "auth unreachable")
	}
	s.mu.Lock()
	s.tunnels[id] = conn
	s.mu.Unlock()
	go func() {
		buf := make([]byte, 16<<10)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				s.ch.hub.push(s.client, map[string]any{"push": "tunnel", "tunnel": id,
					"data": base64.StdEncoding.EncodeToString(buf[:n])})
			}
			if err != nil {
				s.tunnelClose(id)
				s.ch.hub.push(s.client, map[string]any{"push": "tunnel-closed", "tunnel": id})
				return
			}
		}
	}()
	return map[string]int{"tunnel": id}, nil
}

func (s *session) tunnelData(req request) (any, *opError) {
	var data string
	json.Unmarshal(req.Value, &data)
	raw, err := base64.StdEncoding.DecodeString(data)
	s.mu.Lock()
	conn := s.tunnels[req.Tunnel]
	s.mu.Unlock()
	if err != nil || conn == nil {
		return nil, fail(failInvalid, "unknown tunnel or bad data")
	}
	if _, err := conn.Write(raw); err != nil {
		return nil, fail(failOffline, "tunnel closed")
	}
	return true, nil
}

func (s *session) tunnelClose(id int) {
	s.mu.Lock()
	conn := s.tunnels[id]
	delete(s.tunnels, id)
	s.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
}

func (s *session) closeTunnels() {
	s.mu.Lock()
	ids := make([]int, 0, len(s.tunnels))
	for id := range s.tunnels {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.tunnelClose(id)
	}
}

// profileKey is how service-notices names a person: the profile number of
// the token's sub ("profil:3" → "3").
func profileKey(c *auth.Claims) string {
	id, _ := strings.CutPrefix(c.Sub, "profil:")
	if _, err := strconv.Atoi(id); err != nil {
		return ""
	}
	return id
}

// visible tells whether a Thing is offered to the apps: the hidden ones are
// for the administration, which reaches the LAN itself.
func (s *session) visible(id string) bool {
	return !s.ch.registry.Hidden(id)
}

func (s *session) property(req request) (TD, Property, *opError) {
	td, ok := s.ch.registry.Thing(req.Thing)
	if !s.visible(req.Thing) {
		ok = false
	}
	p, exists := td.Properties[req.Name]
	switch {
	case !ok || !exists:
		return td, p, fail(failInvalid, "unknown Thing or property")
	case len(p.UriVariables) > 0:
		// ponytail: properties with uriVariables (contexts isActive/{context})
		// aren't relayed; the "actives" array covers them for now.
		return td, p, fail(failInvalid, "uriVariables not supported")
	}
	return td, p, nil
}

// callThing is the single path from the apps to the Things, with the app's
// token (and confirm token): the Thing decides (§3.6).
func callThing(td TD, method, url string, body []byte, token, confirm string) (json.RawMessage, *opError) {
	if url == "" {
		return nil, fail(failInvalid, "no HTTP form for this operation")
	}
	if len(body) > 0 && !json.Valid(body) {
		return nil, fail(failInvalid, "invalid JSON")
	}
	req, err := http.NewRequestWithContext(context.Background(), method, url, bytes.NewReader(body))
	if err != nil {
		return nil, fail(failInvalid, err.Error())
	}
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json") // required by go-wot
	}
	setBearer(req.Header, token)
	if confirm != "" {
		req.Header.Set("X-Eria-Confirm", confirm)
	}
	resp, err := upstream.Do(req)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return nil, fail(failTimeout, "the Thing did not answer in time")
		}
		zlog.Warn().Err(err).Str("thing", td.ID).Msg("[channel:callThing] Thing unreachable")
		return nil, fail(failOffline, "Thing unreachable")
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fail(failRefused, "access denied") // the Thing says no more (§2.9)
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return nil, fail(failInvalid, strings.TrimSpace(string(data)))
	case resp.StatusCode >= 500:
		return nil, fail(failFailed, strings.TrimSpace(string(data)))
	case len(bytes.TrimSpace(data)) == 0 || !json.Valid(data):
		return json.RawMessage("null"), nil
	}
	return data, nil
}

// can tells the app what the viewer may do on a service (display only: the
// Thing decides), by the same decision on the viewer's token; Confirm lists
// the commands allowed with a confirmation (§2.8).
func (s *session) can(svc Service) *Can {
	_, token := s.current()
	c := &Can{Read: []string{}, Write: []string{}, Invoke: []string{}, Confirm: []string{}}
	check := func(f authz.Function, list *[]string) {
		d := s.ch.auth.Decide(token, svc.Thing, f, true)
		if d.Allow || d.Confirm {
			*list = append(*list, f.Name)
		}
		if d.Confirm {
			c.Confirm = append(c.Confirm, f.Name)
		}
	}
	for name, p := range svc.Properties {
		check(authz.Function{Kind: "property", Name: name, Op: authz.Read}, &c.Read)
		if !p.ReadOnly {
			check(authz.Function{Kind: "property", Name: name, Op: authz.Write}, &c.Write)
		}
	}
	for name := range svc.Actions {
		check(authz.Function{Kind: "action", Name: name, Op: authz.Invoke}, &c.Invoke)
	}
	slices.Sort(c.Read)
	slices.Sort(c.Write)
	slices.Sort(c.Invoke)
	slices.Sort(c.Confirm)
	return c
}

func internal(err error) *opError {
	if err == nil {
		return nil
	}
	zlog.Error().Err(err).Msg("[channel]")
	return fail(failFailed, "server error")
}

func invalid(err error) *opError {
	if err == nil {
		return nil
	}
	return fail(failInvalid, err.Error())
}

// bearer returns the session token sent by the app (Authorization header).
func bearer(r *http.Request) string {
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return token
}
