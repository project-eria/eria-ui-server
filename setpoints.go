package main

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"time"

	zlog "github.com/rs/zerolog/log"
)

// Set points (Hex DESIGN.md, control hexagon, point 8): the last value an app
// asked for a property, until the real state reaches it. The relay carries every
// command of the apps, so it is the one to know them: it pushes them to every
// app that follows the property (same rights as its value), so all terminals
// show the outline and the dashed level, not only the one that sent. Commands
// from elsewhere (automations, a wall switch) have none: the real state only.
// In memory: a set point lives seconds or minutes, a restart may drop them.

// defaultSetpointTimeout is how long a set point waits for the real state, unless
// the admin chose another delay for the Thing (ERIA Admin › Appareils, via auth).
const defaultSetpointTimeout = 30 * time.Second

type setpoint struct {
	value json.RawMessage
	timer *time.Timer
}

// setpointOf is the property and value an action asks for, from the action
// names of eria-core's models; ok false when it asks for none. stop clears the
// set point of the position. current reads the last known value of a property.
func setpointOf(action string, input json.RawMessage, current func(string) json.RawMessage) (property string, value json.RawMessage, clear, ok bool) {
	switch action {
	case "setPosition":
		return "position", input, false, len(input) > 0
	case "fade":
		return "brightness", input, false, len(input) > 0
	case "open":
		return "open", json.RawMessage("true"), false, true
	case "close":
		return "open", json.RawMessage("false"), false, true
	case "toggle":
		var on bool
		if err := json.Unmarshal(current("on"), &on); err != nil {
			return "", nil, false, false // the current state unknown: no set point
		}
		v, _ := json.Marshal(!on)
		return "on", v, false, true
	case "stop":
		return "position", nil, true, true
	}
	return "", nil, false, false
}

// reached: the real state is the set point; a number within 1 (devices round).
func reached(target, real json.RawMessage) bool {
	var t, r any
	if json.Unmarshal(target, &t) != nil || json.Unmarshal(real, &r) != nil {
		return bytes.Equal(target, real)
	}
	tn, okT := t.(float64)
	rn, okR := r.(float64)
	if okT && okR {
		return math.Abs(tn-rn) <= 1
	}
	return reflect.DeepEqual(t, r)
}

// setSetpoint records the value asked for a followed property and pushes it.
// Nobody follows it: nothing to show, and nothing tells when it is reached.
func (h *Hub) setSetpoint(k feedKey, value json.RawMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f, ok := h.feeds[k]
	if !ok {
		return
	}
	h.dropSetpointLocked(k)
	if f.value != nil && reached(value, f.value) {
		return // already there
	}
	sp := &setpoint{value: value}
	sp.timer = time.AfterFunc(h.setpointTimeout(k.Thing), func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.setpoints[k] == sp {
			zlog.Info().Str("thing", k.Thing).Str("property", k.Name).RawJSON("setpoint", value).Msg("[hub:setpoint] not reached in time")
			h.clearSetpointLocked(k)
		}
	})
	h.setpoints[k] = sp
	h.sendSetpointLocked(f, k, value)
}

// clearSetpoint drops the set point of a property and tells the apps.
func (h *Hub) clearSetpoint(k feedKey) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clearSetpointLocked(k)
}

func (h *Hub) clearSetpointLocked(k feedKey) {
	if _, ok := h.setpoints[k]; !ok {
		return
	}
	h.dropSetpointLocked(k)
	if f, ok := h.feeds[k]; ok {
		h.sendSetpointLocked(f, k, nil)
	}
}

// dropSetpointLocked forgets a set point without telling anyone.
func (h *Hub) dropSetpointLocked(k feedKey) {
	if sp, ok := h.setpoints[k]; ok {
		sp.timer.Stop()
		delete(h.setpoints, k)
	}
}

// sendSetpointLocked pushes a set point (nil: cleared) to the apps allowed to read the property.
func (h *Hub) sendSetpointLocked(f *feed, k feedKey, value json.RawMessage) {
	data, _ := json.Marshal(pushMsg{Push: "setpoint", Thing: k.Thing, Property: k.Name, Value: value})
	for c := range f.subs {
		if c.allowed == nil || c.allowed(k) {
			h.sendLocked(c, data)
		}
	}
}
