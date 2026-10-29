package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The orchestrator's agent rolls a deployment back unless the probe answers
// 200 with valid JSON (eria-agent probeWoT).
func TestHealth(t *testing.T) {
	srv := httptest.NewServer(newMux(&Channel{}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !json.Valid(body) {
		t.Fatalf("health: %d %q, want 200 and JSON", resp.StatusCode, body)
	}
}
