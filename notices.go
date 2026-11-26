package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	zlog "github.com/rs/zerolog/log"
)

// Notifications come from the service-notices Thing (config `notices`): the
// server follows its "notices" property, tells every app when it changes
// (push notices-changed), and answers each app with the notices its person
// may see, with their read state.

// watchNotices follows the notices property through the hub (same upstream
// WebSocket and retries as the apps' observations) with an internal client.
func (h *Hub) watchNotices(ctx context.Context, thing string) {
	c := h.attach("")
	if err := h.follow(c, feedKey{thing, kindProperty, "notices"}); err != nil {
		zlog.Warn().Str("thing", thing).Str("error", err.Message).Msg("[notices:watch] can't follow the notices")
	}
	go func() {
		<-ctx.Done()
		h.drop(c)
	}()
	for msg := range c.send {
		var m pushMsg
		if json.Unmarshal(msg, &m) == nil && m.Push == "value" {
			h.broadcast("", map[string]string{"push": "notices-changed"})
			h.noticesChanged(m.Value) // an alert is an incident (situation.go)
		}
	}
}

// keepResolved is how long a resolved notice stays in a person's list once
// read (without a person, read or not: nobody can mark it).
const keepResolved = 24 * time.Hour

// notices gives the notices this connection may see, newest first: "profile"
// ones only to a signed-in person, each with its read state; a resolved and
// read one leaves the list keepResolved after its resolution.
func (s *session) notices() (any, *opError) {
	td, ok := s.ch.registry.Thing(s.ch.noticesThing)
	if !ok {
		return []any{}, nil // no notices service on the LAN
	}
	_, token := s.current()
	raw, err := callThing(td, http.MethodGet, href(td.Properties["notices"].Forms, "readproperty"), nil, token, "")
	if err != nil {
		return nil, err
	}
	var list []map[string]any
	if json.Unmarshal(raw, &list) != nil {
		return nil, fail(failFailed, "bad notices from the service")
	}

	read := map[string]bool{}
	p, _ := s.current()
	signedIn := p != nil && p.Sub != "" && profileKey(p) != ""
	if signedIn {
		form := href(td.Properties["read"].Forms, "readproperty")
		raw, err := callThing(td, http.MethodGet, strings.Replace(form, "{profile}", url.PathEscape(profileKey(p)), 1), nil, token, "")
		if err != nil {
			return nil, err
		}
		var ids []string
		json.Unmarshal(raw, &ids)
		for _, id := range ids {
			read[id] = true
		}
	}

	out := make([]map[string]any, 0, len(list))
	old := time.Now().Add(-keepResolved)
	for _, n := range list {
		if n["audience"] != "all" && !signedIn {
			continue
		}
		id, _ := n["id"].(string)
		resolved, _ := n["resolved"].(string)
		if at, err := time.Parse(time.RFC3339, resolved); err == nil && at.Before(old) && (read[id] || !signedIn) {
			continue
		}
		n["read"] = read[id]
		delete(n, "key") // the service's own handle
		out = append(out, n)
	}
	return out, nil
}

// markRead records the notices as read for the signed-in person, then tells
// every app (the same person may be on another terminal).
func (s *session) markRead(req request) (any, *opError) {
	p, token := s.current()
	if p == nil || p.Sub == "" || profileKey(p) == "" {
		return nil, fail(failRefused, "sign in to mark notices as read")
	}
	var ids []string
	if json.Unmarshal(req.Value, &ids) != nil {
		return nil, fail(failInvalid, "ids expected")
	}
	td, ok := s.ch.registry.Thing(s.ch.noticesThing)
	if !ok {
		return nil, fail(failOffline, "no notices service")
	}
	body, _ := json.Marshal(map[string]any{"profile": profileKey(p), "ids": ids})
	if _, err := callThing(td, http.MethodPost, href(td.Actions["markRead"].Forms, "invokeaction"), body, token, ""); err != nil {
		return nil, err
	}
	// ponytail: every app re-reads its notices; fine for a household
	s.ch.hub.broadcast("", map[string]string{"push": "notices-changed"})
	return true, nil
}
