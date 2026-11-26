package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIncidentOf(t *testing.T) {
	notices := []map[string]any{
		{"id": "1", "severity": "alert", "title": "Fumée", "at": "2026-11-26T20:00:00+01:00", "resolved": "2026-11-26T20:05:00+01:00"},
		{"id": "2", "severity": "warning", "title": "Fenêtre ouverte", "at": "2026-11-26T22:30:00+01:00"},
		{"id": "3", "severity": "alert", "title": "Intrusion", "body": "Salon", "at": "2026-11-26T22:14:00+01:00"},
		{"id": "4", "severity": "alert", "title": "Fuite", "at": "2026-11-26T21:00:00+01:00"},
	}
	assert.Equal(t, &incident{Notice: "3", Title: "Intrusion", Body: "Salon", At: "2026-11-26T22:14:00+01:00"}, incidentOf(notices),
		"the most recent unresolved alert; warnings and resolved alerts are no incident")
	assert.Nil(t, incidentOf(notices[:2]))
}

// The apps get the situation when it changes, never twice the same.
func TestSituationPush(t *testing.T) {
	h := NewHub(NewRegistry(nil))
	c := h.attach("t1")
	next := func() map[string]any {
		select {
		case data := <-c.send:
			var m map[string]any
			require.NoError(t, json.Unmarshal(data, &m))
			return m
		case <-time.After(time.Second):
			t.Fatal("no push")
			return nil
		}
	}
	none := func() {
		select {
		case data := <-c.send:
			t.Fatalf("unexpected push %s", data)
		case <-time.After(50 * time.Millisecond):
		}
	}

	assert.Nil(t, h.situation.message()["actives"], "not read yet")
	assert.True(t, h.situation.setActives([]string{"evening"}))
	assert.False(t, h.situation.setActives([]string{"evening"}))
	assert.True(t, h.situation.setActives(nil), "none active: [] once read")
	assert.Equal(t, []string{}, h.situation.message()["actives"])

	h.noticesChanged(json.RawMessage(`[{"id":"3","severity":"alert","title":"Intrusion","at":"2026-11-26T22:14:00+01:00"}]`))
	m := next()
	assert.Equal(t, "situation", m["push"])
	assert.Equal(t, "Intrusion", m["incident"].(map[string]any)["title"])
	h.noticesChanged(json.RawMessage(`[{"id":"3","severity":"alert","title":"Intrusion","at":"2026-11-26T22:14:00+01:00"}]`))
	none()
	h.noticesChanged(json.RawMessage(`[{"id":"3","severity":"alert","title":"Intrusion","at":"2026-11-26T22:14:00+01:00","resolved":"2026-11-26T22:20:00+01:00"}]`))
	assert.Nil(t, next()["incident"], "resolved: no incident")
}
