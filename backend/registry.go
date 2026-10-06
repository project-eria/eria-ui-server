package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"sort"
	"sync"
	"time"

	"github.com/project-eria/go-wot/protocolDnssd"
	zlog "github.com/rs/zerolog/log"
)

const (
	// A Thing is announced once per browse round (10 s): offline after 3 missed rounds.
	offlineAfter = 35 * time.Second
	// Re-read a TD from time to time to pick up app upgrades.
	tdRefresh = 5 * time.Minute
)

var upstream = &http.Client{Timeout: 5 * time.Second}

type thingEntry struct {
	td      TD
	url     string
	fetched time.Time
	seen    time.Time
}

// Registry holds the Things found on the LAN, keyed by TD id.
// ponytail: in memory only, rebuilt by mDNS within ~10 s after a restart.
type Registry struct {
	mu     sync.RWMutex
	things map[string]*thingEntry
	hide   []string // glob patterns on TD ids
}

func NewRegistry(hide []string) *Registry {
	return &Registry{things: map[string]*thingEntry{}, hide: hide}
}

// Discover browses `_wot._tcp` until ctx is done.
func (r *Registry) Discover(ctx context.Context) error {
	entries, err := protocolDnssd.Browse(ctx, protocolDnssd.BrowseOptions{PreferIPv4: true, AllowPrivate: true})
	if err != nil {
		return err
	}
	for e := range entries {
		r.seen(e.TDURL.String())
	}
	return nil
}

func (r *Registry) seen(url string) {
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
	td, err := fetchTD(url)
	if err != nil {
		zlog.Warn().Err(err).Str("url", url).Msg("[registry:seen] can't read TD")
		return
	}
	if r.hidden(td.ID) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, known := r.things[td.ID]; !known {
		zlog.Info().Str("id", td.ID).Str("url", url).Msg("[registry:seen] new Thing")
	}
	r.things[td.ID] = &thingEntry{td: td, url: url, fetched: now, seen: now}
}

func (r *Registry) hidden(id string) bool {
	for _, pattern := range r.hide {
		if ok, _ := path.Match(pattern, id); ok {
			return true
		}
	}
	return false
}

func fetchTD(url string) (TD, error) {
	var td TD
	resp, err := upstream.Get(url)
	if err != nil {
		return td, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return td, fmt.Errorf("status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&td); err != nil {
		return td, err
	}
	if td.ID == "" {
		return td, fmt.Errorf("TD without id")
	}
	return td, nil
}

// Services lists every service of every known Thing, sorted for display.
func (r *Registry) Services() []Service {
	r.mu.RLock()
	defer r.mu.RUnlock()
	services := []Service{}
	for _, t := range r.things {
		services = append(services, buildServices(t.td, time.Since(t.seen) < offlineAfter)...)
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
	return status
}
