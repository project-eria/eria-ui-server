package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetpointOf(t *testing.T) {
	on := func(string) json.RawMessage { return json.RawMessage("true") }
	unknown := func(string) json.RawMessage { return nil }
	cases := []struct {
		action, input, property, value string
		clear, ok                      bool
		current                        func(string) json.RawMessage
	}{
		{"setPosition", "40", "position", "40", false, true, unknown},
		{"fade", "70", "brightness", "70", false, true, unknown},
		{"open", "", "open", "true", false, true, unknown},
		{"close", "", "open", "false", false, true, unknown},
		{"toggle", "", "on", "false", false, true, on},
		{"toggle", "", "", "", false, false, unknown}, // current state unknown
		{"stop", "", "position", "", true, true, unknown},
		{"snapshot", "", "", "", false, false, unknown},
	}
	for _, c := range cases {
		p, v, clear, ok := setpointOf(c.action, json.RawMessage(c.input), c.current)
		assert.Equal(t, c.ok, ok, c.action)
		assert.Equal(t, c.property, p, c.action)
		assert.Equal(t, c.value, string(v), c.action)
		assert.Equal(t, c.clear, clear, c.action)
	}
}

func TestReached(t *testing.T) {
	assert.True(t, reached(json.RawMessage("70"), json.RawMessage("70.6")), "within 1")
	assert.False(t, reached(json.RawMessage("70"), json.RawMessage("68")))
	assert.True(t, reached(json.RawMessage("true"), json.RawMessage("true")))
	assert.False(t, reached(json.RawMessage("true"), json.RawMessage("false")))
}

// A set point goes to the followers of the property, is cleared once the real
// state reaches it, or after the Thing's delay.
func TestSetpointLifecycle(t *testing.T) {
	h := NewHub(NewRegistry(nil))
	h.setpointTimeout = func(string) time.Duration { return 50 * time.Millisecond }
	k := feedKey{"urn:eria:lampadaire", kindProperty, "brightness"}
	c := h.attach("t1")
	h.feeds[k] = &feed{subs: map[*client]bool{c: true}, cancel: func() {}, value: json.RawMessage("30")}
	next := func() pushMsg {
		select {
		case data := <-c.send:
			var m pushMsg
			require.NoError(t, json.Unmarshal(data, &m))
			return m
		case <-time.After(time.Second):
			t.Fatal("no push")
			return pushMsg{}
		}
	}

	h.setSetpoint(k, json.RawMessage("70"))
	assert.Equal(t, pushMsg{Push: "setpoint", Thing: k.Thing, Property: "brightness", Value: json.RawMessage("70")}, next())

	h.publish(k, json.RawMessage("50")) // on its way: the set point stays
	assert.Equal(t, "value", next().Push)
	h.publish(k, json.RawMessage("70")) // reached: cleared
	assert.Equal(t, "value", next().Push)
	cleared := next()
	assert.Equal(t, "setpoint", cleared.Push)
	assert.Nil(t, cleared.Value)

	h.setSetpoint(k, json.RawMessage("70")) // already there: nothing
	h.setSetpoint(k, json.RawMessage("20"))
	assert.Equal(t, json.RawMessage("20"), next().Value)
	timedOut := next() // never reached: cleared after the delay
	assert.Equal(t, "setpoint", timedOut.Push)
	assert.Nil(t, timedOut.Value)

	// Nobody follows the property: no set point.
	h.setSetpoint(feedKey{"urn:eria:volet", kindProperty, "position"}, json.RawMessage("40"))
	assert.Empty(t, h.setpoints)
}
