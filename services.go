package main

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"

	"github.com/project-eria/eria-core/model"
)

// TD is the subset of a W3C WoT Thing Description the UI needs.
type TD struct {
	ID          string              `json:"id"`
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Types       any                 `json:"@type"` // string or []string
	Properties  map[string]Property `json:"properties"`
	Actions     map[string]Action   `json:"actions"`
	Events      map[string]Event    `json:"events"`
}

type Form struct {
	Href string `json:"href"`
	Op   any    `json:"op"` // string or []string
}

type Property struct {
	Title        string          `json:"title,omitempty"`
	Type         string          `json:"type,omitempty"`
	Unit         string          `json:"unit,omitempty"`
	Minimum      *float64        `json:"minimum,omitempty"`
	Maximum      *float64        `json:"maximum,omitempty"`
	Enum         []any           `json:"enum,omitempty"`
	OneOf        json.RawMessage `json:"oneOf,omitempty"`
	Items        json.RawMessage `json:"items,omitempty"`
	ReadOnly     bool            `json:"readOnly"`
	Observable   bool            `json:"observable"`
	UriVariables json.RawMessage `json:"uriVariables,omitempty"`
	Forms        []Form          `json:"forms,omitempty"`
}

type Action struct {
	Title string          `json:"title,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	Forms []Form          `json:"forms,omitempty"`
}

type Event struct {
	Title string          `json:"title,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
	Forms []Form          `json:"forms,omitempty"`
}

// Service is what the UI displays: one capability of one Thing.
type Service struct {
	ID         string              `json:"id"` // <thing id>#<capability>
	Thing      string              `json:"thing"`
	Capability string              `json:"capability"`
	Label      string              `json:"label"`
	Online     bool                `json:"online"`
	Properties map[string]Property `json:"properties"`
	Actions    map[string]Action   `json:"actions"`
	Events     map[string]Event    `json:"events,omitempty"`
	Can        *Can                `json:"can,omitempty"` // what the viewer may do, per connection
	// Rooms of the Thing (its zones in the table signed by auth): the page
	// editor lists the devices by room.
	Rooms []string `json:"rooms,omitempty"`
}

// Can lists the operations a viewer may do on a service (PLAN §5.00): the
// widgets adapt to it. Display only: the Thing decides.
type Can struct {
	Read   []string `json:"read"`
	Write  []string `json:"write"`
	Invoke []string `json:"invoke"`
	// Confirm: allowed with a confirmation (Face ID on a phone, §2.8).
	Confirm []string `json:"confirm"`
}

const genericCapability = "generic"

type capability struct {
	properties []string
	actions    []string
	events     []string
	// needActions: recognised only if its actions are present too. Sensors'
	// calibrate actions are optional (absent from the weather service).
	needActions bool
}

// capabilities is the eria-core model catalog, plus the contexts manager
// which is not part of it.
var capabilities = func() map[string]capability {
	caps := map[string]capability{
		"Contexts": {properties: []string{"actives", "isActive"}, actions: []string{"setContext", "unsetContext"}, needActions: true},
	}
	for name, m := range model.Models {
		if name == "EriaAppController" {
			continue // apps are never shown to the user
		}
		c := capability{}
		for p := range m.Properties {
			c.properties = append(c.properties, p)
		}
		for a := range m.Actions {
			c.actions = append(c.actions, a)
		}
		for e := range m.Events {
			c.events = append(c.events, e)
		}
		caps[name] = c
	}
	return caps
}()

var unitAliases = map[string]string{"celsius": "°C", "percent": "%"}

// detect returns the capabilities of a Thing: its declared @type when known,
// otherwise every capability whose properties are all present, minus those
// subsumed by a more specific match (LightBasic inside LightDimmer...).
func detect(td TD) []string {
	var found []string
	for _, t := range atTypes(td.Types) {
		if _, ok := capabilities[t]; ok {
			found = append(found, t)
		}
	}
	if len(found) > 0 {
		return found
	}
	for name, c := range capabilities {
		if hasAll(td.Properties, c.properties) && (!c.needActions || hasAll(td.Actions, c.actions)) {
			found = append(found, name)
		}
	}
	found = slices.DeleteFunc(found, func(a string) bool {
		for _, b := range found {
			if a != b && isSubset(capabilities[a].properties, capabilities[b].properties) {
				return true
			}
		}
		return false
	})
	sort.Strings(found)
	return found
}

func atTypes(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, s := range t {
			if str, ok := s.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

func hasAll[V any](m map[string]V, keys []string) bool {
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}

// isSubset reports whether a is a strict subset of b.
func isSubset(a, b []string) bool {
	if len(a) >= len(b) {
		return false
	}
	for _, x := range a {
		if !slices.Contains(b, x) {
			return false
		}
	}
	return true
}

// label picks a human name: KNX Things use their ref as title and the
// human name as description.
func label(td TD) string {
	ref := td.ID[strings.LastIndex(td.ID, ":")+1:]
	if (td.Title == "" || td.Title == ref) && td.Description != "" {
		return td.Description
	}
	if td.Title == "" {
		return td.ID
	}
	return td.Title
}

// buildServices splits a Thing into services: one per detected capability,
// plus a generic one for the leftover properties and actions.
func buildServices(td TD, online bool) []Service {
	newService := func(capa string) Service {
		return Service{
			ID: td.ID + "#" + capa, Thing: td.ID, Capability: capa, Label: label(td), Online: online,
			Properties: map[string]Property{}, Actions: map[string]Action{}, Events: map[string]Event{},
		}
	}
	used := map[string]bool{}
	var services []Service
	for _, capa := range detect(td) {
		s := newService(capa)
		for _, name := range capabilities[capa].properties {
			// Capability properties are state only: gateway-knx's default write
			// handlers just overwrite the cached value without driving the device.
			// Commands go through the actions (toggle, fade, setPosition...).
			p := publicProperty(td.Properties[name])
			p.ReadOnly = true
			s.Properties[name] = p
			used["p:"+name] = true
		}
		for _, name := range capabilities[capa].actions {
			if a, ok := td.Actions[name]; ok {
				a.Forms = nil
				s.Actions[name] = a
				used["a:"+name] = true
			}
		}
		for _, name := range capabilities[capa].events {
			if e, ok := td.Events[name]; ok {
				e.Forms = nil
				s.Events[name] = e
				used["e:"+name] = true
			}
		}
		services = append(services, s)
	}
	rest := newService(genericCapability)
	for name, p := range td.Properties {
		if !used["p:"+name] {
			rest.Properties[name] = publicProperty(p)
		}
	}
	for name, a := range td.Actions {
		if !used["a:"+name] {
			a.Forms = nil
			rest.Actions[name] = a
		}
	}
	for name, e := range td.Events {
		if !used["e:"+name] {
			e.Forms = nil
			rest.Events[name] = e
		}
	}
	if len(rest.Properties)+len(rest.Actions)+len(rest.Events) > 0 {
		services = append(services, rest)
	}
	return services
}

func publicProperty(p Property) Property {
	p.Forms = nil
	if u, ok := unitAliases[p.Unit]; ok {
		p.Unit = u
	}
	return p
}

// isWritable tells whether the UI may write this property of the Thing.
func isWritable(td TD, name string) bool {
	for _, s := range buildServices(td, true) {
		if p, ok := s.Properties[name]; ok {
			return !p.ReadOnly
		}
	}
	return false
}

// href returns the form href of an interaction for the given op: the
// WebSocket form for observations, the HTTP one for everything else.
func href(forms []Form, op string) string {
	scheme := "http"
	if op == "observeproperty" || op == "subscribeevent" {
		scheme = "ws"
	}
	for _, f := range forms {
		if !strings.HasPrefix(f.Href, scheme) {
			continue
		}
		switch o := f.Op.(type) {
		case string:
			if o == op {
				return f.Href
			}
		case []any:
			if slices.Contains(o, any(op)) {
				return f.Href
			}
		}
	}
	return ""
}
