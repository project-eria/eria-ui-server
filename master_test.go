package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMasterAction(t *testing.T) {
	var got map[string]any
	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &got)
		w.Write([]byte(`"ok"`))
	}))
	defer master.Close()
	registry := NewRegistry([]string{"urn:eria:manager:*"})
	registry.things["urn:eria:manager:1"] = &thingEntry{seen: time.Now(), td: TD{
		ID:      "urn:eria:manager:1",
		Actions: map[string]Action{"acceptNode": {Forms: []Form{{Href: master.URL + "/acceptNode", Op: "invokeaction"}}}},
	}}
	s := &session{ch: &Channel{registry: registry, master: "urn:eria:manager:1"}, profile: &Profile{Level: 0}}

	if _, err := s.masterAction(request{Name: "acceptNode", Value: json.RawMessage(`{"node":"pi"}`)}); err == nil || err.Type != failRefused {
		t.Errorf("no token = %v, want refused", err)
	}
	if out, err := s.masterAction(request{Name: "acceptNode", Value: json.RawMessage(`{"node":"pi"}`), Token: "secret"}); err != nil || string(out.(json.RawMessage)) != `"ok"` {
		t.Fatalf("masterAction = %v, %v", out, err)
	}
	if got["node"] != "pi" || got["token"] != "secret" {
		t.Errorf("master received %v, want the input plus the token", got)
	}
	if _, err := s.masterAction(request{Name: "format", Token: "secret"}); err == nil || err.Type != failInvalid {
		t.Errorf("unknown action = %v, want invalid", err)
	}
}
