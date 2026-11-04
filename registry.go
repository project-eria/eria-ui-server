package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	eria "github.com/project-eria/eria-core"
	"github.com/project-eria/go-wot/inventory"
	"github.com/project-eria/go-wot/protocolDnssd"
	zlog "github.com/rs/zerolog/log"
)

const (
	// A Thing is announced once per browse round (10 s): offline after 3 missed rounds.
	offlineAfter = 35 * time.Second
	// Re-read a TD from time to time to pick up app upgrades.
	tdRefresh = 5 * time.Minute
)

// upstream and upstreamWS reach the Things: the master over https is
// checked against the deployment roots (useMasterTLS).
var (
	upstream   = &http.Client{Timeout: 5 * time.Second}
	upstreamWS = &websocket.Dialer{Proxy: http.ProxyFromEnvironment, HandshakeTimeout: 45 * time.Second}
)

// useMasterTLS makes every https Thing be checked as the master (AUTH-DESIGN
// §4.7 5d): its certificate, from the deployment roots, names "maitre".
// ponytail: one name for every https Thing, the master being the only one;
// a name per role when the apps serve TLS.
func useMasterTLS(roots []byte) error {
	client, err := inventory.MasterClient(roots, 5*time.Second)
	if err != nil {
		return err
	}
	upstream = client
	upstreamWS.TLSClientConfig = client.Transport.(*http.Transport).TLSClientConfig
	return nil
}

type thingEntry struct {
	td      TD
	raw     json.RawMessage // the TD as received, for the explorer
	url     string
	node    string // machine name from the mDNS announce ("" when added by address)
	manual  bool   // added by address by the admin, not found by mDNS
	fetched time.Time
	seen    time.Time
}

// ThingInfo is what the explorer lists (admin).
type ThingInfo struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Node   string `json:"node,omitempty"` // machine announcing it (mDNS host name)
	Online bool   `json:"online"`
	Hidden bool   `json:"hidden"` // matched by a hidden pattern: never shown to people
	Manual bool   `json:"manual"`
}

// Registry holds the Things found on the LAN (or added by address), keyed by
// TD id. Hidden ones are kept for the admin and left out of the services.
// ponytail: in memory only, rebuilt by mDNS within ~10 s after a restart.
type Registry struct {
	mu     sync.RWMutex
	things map[string]*thingEntry
	hide   []string // glob patterns on TD ids
}

func NewRegistry(hide []string) *Registry {
	return &Registry{things: map[string]*thingEntry{}, hide: hide}
}

// Discover browses the server's network (`_wot._tcp` for the default one)
// until ctx is done: Things of other networks are never seen.
func (r *Registry) Discover(ctx context.Context) error {
	entries, err := protocolDnssd.Browse(ctx, protocolDnssd.BrowseOptions{Network: eria.Network(), PreferIPv4: true, AllowPrivate: true})
	if err != nil {
		return err
	}
	for e := range entries {
		r.seen(e.TDURL.String(), nodeOf(e), false)
	}
	return nil
}

// nodeOf is the name of the machine announcing a Thing: in the TXT record
// for go-wot apps, else the mDNS host (an ESP32's own name). A host built
// from the IP (go-wot before v1.10.0, "172-23-0-7") names no machine.
func nodeOf(e protocolDnssd.Entry) string {
	if node := e.TXT.Extra["node"]; node != "" {
		return node
	}
	host, _, _ := strings.Cut(e.Host, ".")
	if net.ParseIP(strings.ReplaceAll(host, "-", ".")) != nil {
		return ""
	}
	return host
}

// Poll re-reads the Things added by address (no mDNS announces them) until
// ctx is done.
func (r *Registry) Poll(ctx context.Context, urls func() []string) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		for _, u := range urls() {
			r.seen(u, "", true)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (r *Registry) seen(url, node string, manual bool) {
	now := time.Now()
	r.mu.Lock()
	for _, t := range r.things {
		if t.url == url && now.Sub(t.fetched) < tdRefresh {
			t.seen = now
			r.mu.Unlock()
			return
		}
	}
	r.mu.Unlock()

	// ponytail: TDs fetched one at a time; a dead host delays the browse
	// round by the client timeout. Fan out if the LAN grows.
	td, raw, err := fetchTD(url)
	if err != nil {
		zlog.Warn().Err(err).Str("url", url).Msg("[registry:seen] can't read TD")
		return
	}
	r.add(td, raw, url, node, manual, now)
}

func (r *Registry) add(td TD, raw json.RawMessage, url, node string, manual bool, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, known := r.things[td.ID]; !known {
		zlog.Info().Str("id", td.ID).Str("url", url).Msg("[registry:seen] new Thing")
	}
	r.things[td.ID] = &thingEntry{td: td, raw: raw, url: url, node: node, manual: manual, fetched: now, seen: now}
}

// SetHide replaces the hidden patterns.
func (r *Registry) SetHide(patterns []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hide = patterns
}

// Hidden tells whether a Thing is left out of what people see.
func (r *Registry) Hidden(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.hiddenLocked(id)
}

// Forget removes a Thing added by address.
func (r *Registry) Forget(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.things, id)
}

// Things lists every known Thing, hidden ones included (admin).
func (r *Registry) Things() []ThingInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := []ThingInfo{}
	for id, t := range r.things {
		list = append(list, ThingInfo{ID: id, Title: label(t.td), URL: t.url, Node: t.node, Online: time.Since(t.seen) < offlineAfter,
			Hidden: r.hiddenLocked(id), Manual: t.manual})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Title < list[j].Title })
	return list
}

// Raw returns a Thing's TD as received.
func (r *Registry) Raw(id string) (json.RawMessage, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.things[id]
	if !ok {
		return nil, false
	}
	return t.raw, true
}

func (r *Registry) hiddenLocked(id string) bool {
	for _, pattern := range r.hide {
		if ok, _ := path.Match(pattern, id); ok {
			return true
		}
	}
	return false
}

func fetchTD(url string) (TD, json.RawMessage, error) {
	var td TD
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return td, nil, err
	}
	setBearer(req.Header, tokenFor(url))
	resp, err := upstream.Do(req)
	if err != nil {
		return td, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return td, nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return td, nil, err
	}
	if err := json.Unmarshal(raw, &td); err != nil {
		return td, nil, err
	}
	if td.ID == "" {
		return td, nil, fmt.Errorf("TD without id")
	}
	return td, raw, nil
}

// Services lists every service of every known Thing, sorted for display.
func (r *Registry) Services() []Service {
	r.mu.RLock()
	defer r.mu.RUnlock()
	services := []Service{}
	for id, t := range r.things {
		if !r.hiddenLocked(id) {
			services = append(services, buildServices(t.td, time.Since(t.seen) < offlineAfter)...)
		}
	}
	sort.Slice(services, func(i, j int) bool {
		if services[i].Label != services[j].Label {
			return services[i].Label < services[j].Label
		}
		return services[i].ID < services[j].ID
	})
	return services
}

// Thing returns a copy of a known Thing's TD.
func (r *Registry) Thing(id string) (TD, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.things[id]
	if !ok {
		return TD{}, false
	}
	return t.td, true
}

// Status maps every known Thing to its online state.
func (r *Registry) Status() map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	status := make(map[string]bool, len(r.things))
	for id, t := range r.things {
		status[id] = time.Since(t.seen) < offlineAfter
	}
	// ponytail: hidden Things are in the status too (their ids reach every
	// app's on/offline pushes); filter per connection if that matters.
	return status
}
