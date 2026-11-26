package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Step 5g-2: the updater's manifest and the package, from the coordinator's
// artifact server; nothing from the outer server.
func TestServeRelease(t *testing.T) {
	artifacts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app-releases/1.2.0/linux-x86_64/ERIA.AppImage" {
			w.Write([]byte("package"))
			return
		}
		http.NotFound(w, r)
	}))
	defer artifacts.Close()
	_release.Store(&Release{Version: "1.2.0", Targets: map[string]releaseTarget{"linux-x86_64": {
		URL: artifacts.URL + "/app-releases/1.2.0/linux-x86_64/ERIA.AppImage", Signature: "minisig", Descriptor: json.RawMessage(`{"payload":"p","sig":"s"}`)}}})
	defer _release.Store(nil)

	inner, outer := httptest.NewServer(newMux(&Channel{})), httptest.NewServer(newMux(&Channel{outer: true}))
	defer inner.Close()
	defer outer.Close()
	get := func(url string) (int, string) {
		resp, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	status, body := get(inner.URL + "/api/release/linux-x86_64")
	var m map[string]string
	json.Unmarshal([]byte(body), &m)
	if status != 200 || m["version"] != "1.2.0" || m["signature"] != "minisig" || m["url"] != "https://"+inner.Listener.Addr().String()+"/api/release/linux-x86_64/package" {
		t.Errorf("manifest = %d %s", status, body)
	}
	if status, body := get(inner.URL + "/api/release/linux-x86_64/package"); status != 200 || body != "package" {
		t.Errorf("package = %d %q", status, body)
	}
	for _, url := range []string{inner.URL + "/api/release/darwin-aarch64", outer.URL + "/api/release/linux-x86_64"} {
		if status, _ := get(url); status != http.StatusNoContent {
			t.Errorf("%s = %d, want 204", url, status)
		}
	}
	if status, _ := get(inner.URL + "/api/release/linux-x86_64/other"); status != http.StatusNotFound {
		t.Errorf("other = %d", status)
	}
	if p := releasePush(); p["version"] != "1.2.0" {
		t.Errorf("push = %v", p)
	}
}

// « Rechercher une mise à jour »: the loop reads at once when asked; without it
// (a bench without coordinator), what is known after a second.
func TestCheckRelease(t *testing.T) {
	defer _release.Store(nil)
	_release.Store(&Release{Version: "0.5.0"})
	assert.Equal(t, "0.5.0", checkRelease()["version"], "no loop: what is known")

	read := 0
	go func() {
		done := <-releaseNow // what followRelease does
		read++
		_release.Store(&Release{Version: "0.6.0"})
		close(done)
	}()
	assert.Equal(t, "0.6.0", checkRelease()["version"])
	assert.Equal(t, 1, read)
}
