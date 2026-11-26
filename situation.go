package main

import (
	"context"
	"encoding/json"
	"slices"
	"sync"

	zlog "github.com/rs/zerolog/log"
)

// The situation (CONTEXT-PAGES-DESIGN §3.1): the active contexts of
// manager-contexts and the incident in progress, pushed to every app at each
// change and to each app that connects. Each terminal chooses its page from it;
// what the page shows stays filtered by the rights, so knowing it needs none.

// incident is the most recent unresolved alert of the house (dark cockpit,
// Alert level): its title and body are the cause shown at the centre.
type incident struct {
	Notice string `json:"notice"`
	Title  string `json:"title"`
	Body   string `json:"body,omitempty"`
	At     string `json:"at"`
}

type situation struct {
	mu       sync.Mutex
	actives  []string // nil: not read (yet), or unreadable
	incident *incident
}

// message is the push the apps get.
func (s *situation) message() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{"push": "situation", "actives": s.actives, "incident": s.incident}
}

// setActives records manager-contexts' active contexts; true when they changed.
func (s *situation) setActives(actives []string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if actives == nil {
		actives = []string{}
	}
	if s.actives != nil && slices.Equal(s.actives, actives) {
		return false
	}
	s.actives = actives
	return true
}

// setIncident records the incident in progress (nil: none); true when it changed.
func (s *situation) setIncident(i *incident) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if (s.incident == nil && i == nil) || (s.incident != nil && i != nil && *s.incident == *i) {
		return false
	}
	s.incident = i
	return true
}

// incidentOf is the most recent unresolved notice of severity alert, nil when none.
func incidentOf(notices []map[string]any) *incident {
	var found *incident
	for _, n := range notices {
		if n["severity"] != "alert" {
			continue
		}
		if resolved, _ := n["resolved"].(string); resolved != "" {
			continue
		}
		at, _ := n["at"].(string)
		if found != nil && at <= found.At { // RFC 3339 strings of one zone sort as times
			continue
		}
		id, _ := n["id"].(string)
		title, _ := n["title"].(string)
		body, _ := n["body"].(string)
		found = &incident{Notice: id, Title: title, Body: body, At: at}
	}
	return found
}

// watchContexts follows manager-contexts' `actives` (all the active contexts,
// those its automations set too) and tells every app when they change.
func (h *Hub) watchContexts(ctx context.Context, thing string) {
	c := h.attach("")
	if err := h.follow(c, feedKey{thing, kindProperty, "actives"}); err != nil {
		zlog.Warn().Str("thing", thing).Str("error", err.Message).Msg("[situation:watch] can't follow the active contexts")
	}
	go func() {
		<-ctx.Done()
		h.drop(c)
	}()
	for msg := range c.send {
		var m pushMsg
		if json.Unmarshal(msg, &m) != nil || m.Push != "value" {
			continue
		}
		var actives []string
		if err := json.Unmarshal(m.Value, &actives); err != nil {
			zlog.Warn().Err(err).Msg("[situation:watch] bad active contexts")
			continue
		}
		if h.situation.setActives(actives) {
			h.broadcast("", h.situation.message())
		}
	}
}

// noticesChanged updates the incident from the notices' new value.
func (h *Hub) noticesChanged(value json.RawMessage) {
	var list []map[string]any
	if json.Unmarshal(value, &list) != nil {
		return
	}
	if h.situation.setIncident(incidentOf(list)) {
		h.broadcast("", h.situation.message())
	}
}
