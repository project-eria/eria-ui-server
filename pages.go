package main

import "encoding/json"

// Pages (eria-ui-app PLAN §5.000): every page is shared and shown on every
// terminal; kept by auth (AUTH-DESIGN §3.6), served by the relay. A page is a flat list of functions,
// rendered by the theme, aspect and layout chosen at its creation (fixed
// from then on: another look means another page).

type Page struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	Theme    string          `json:"theme"`   // "" = Base; fixed at creation
	Aspect   string          `json:"aspect"`  // one of the theme's aspects; fixed at creation
	Layout   string          `json:"layout"`  // a page type of the theme, or a Base layout; fixed at creation
	Version  int             `json:"version"` // +1 at each save, checked against conflicts
	Elements []Element       `json:"elements"`
	Options  json.RawMessage `json:"options"` // {<theme>: {…}}
}

// Element is one function of a page; its fields depend on its type.
type Element struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"` // service | property | action | event | app
	Thing      string          `json:"thing,omitempty"`
	Capability string          `json:"capability,omitempty"` // service
	Name       string          `json:"name,omitempty"`       // property, action, event or app data
	Input      json.RawMessage `json:"input,omitempty"`      // fixed action input
	Label      string          `json:"label,omitempty"`
	Hidden     bool            `json:"hidden,omitempty"`
	Widget     string          `json:"widget,omitempty"` // forced widget tag
	Options    json.RawMessage `json:"options,omitempty"`
}

// PageRef is what the resource manifest lists.
type PageRef struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}
