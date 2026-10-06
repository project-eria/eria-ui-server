package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePack(t *testing.T, dir, name, packJSON string, files ...string) {
	t.Helper()
	os.MkdirAll(filepath.Join(dir, name), 0o755)
	os.WriteFile(filepath.Join(dir, name, "pack.json"), []byte(packJSON), 0o644)
	for _, f := range files {
		os.WriteFile(filepath.Join(dir, name, f), []byte("export {};"), 0o644)
	}
}

func TestPacks(t *testing.T) {
	dir := t.TempDir()
	writePack(t, dir, "contour", `{"id":"contour","name":"Contour","version":"1.0.0","sdk":1,
		"widgets":{"LightBasic":{"tag":"contour-light","sizes":{"tile":[[1,1],[2,1]]}}}}`, "index.js")
	writePack(t, dir, "badtag", `{"id":"badtag","name":"x","sdk":1,"widgets":{"LightBasic":{"tag":"eria-light"}}}`, "index.js")
	writePack(t, dir, "oldsdk", `{"id":"oldsdk","name":"x","sdk":0,"widgets":{"LightBasic":{"tag":"oldsdk-l"}}}`, "index.js")
	writePack(t, dir, "noentry", `{"id":"noentry","name":"x","sdk":1,"widgets":{"LightBasic":{"tag":"noentry-l"}}}`)
	writePack(t, dir, "escape", `{"id":"escape","name":"x","sdk":1,"entry":"../contour/index.js","widgets":{"LightBasic":{"tag":"escape-l"}}}`)
	writePack(t, dir, "renamed", `{"id":"other","name":"x","sdk":1,"widgets":{"LightBasic":{"tag":"other-l"}}}`, "index.js")
	writePack(t, dir, "bigsize", `{"id":"bigsize","name":"x","sdk":1,"widgets":{"LightBasic":{"tag":"bigsize-l","sizes":{"tile":[[9,1]]}}}}`, "index.js")

	errs := map[string]string{}
	for _, p := range Packs(dir) {
		errs[p.ID] = p.Error
	}
	if errs["contour"] != "" {
		t.Errorf("valid pack refused: %s", errs["contour"])
	}
	for _, bad := range []string{"badtag", "oldsdk", "noentry", "escape", "bigsize"} {
		if errs[bad] == "" {
			t.Errorf("pack %s accepted, want an error", bad)
		}
	}
	if errs["other"] == "" { // id read from pack.json, not equal to its folder
		t.Errorf("pack whose id differs from its folder accepted")
	}
	if !packExists(dir, "contour") || !packExists(dir, "") || !packExists(dir, "standard") || packExists(dir, "badtag") || packExists(dir, "../x") {
		t.Error("packExists gives a wrong answer")
	}
	if len(Packs(filepath.Join(dir, "missing"))) != 0 {
		t.Error("a missing packs folder must list no pack")
	}

	mux := http.NewServeMux()
	PackRoutes(mux, dir)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, _ := http.Get(srv.URL + "/packs/contour/index.js")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "export {};" || resp.Header.Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(resp.Header.Get("Content-Type"), "javascript") {
		t.Errorf("pack file: %d %q %v", resp.StatusCode, body, resp.Header)
	}
	for _, path := range []string{"/packs/../packs_test.go", "/packs/%2e%2e/packs_test.go", "/packs/contour/../../packs_test.go"} {
		resp, err := http.Get(srv.URL + path)
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if strings.Contains(string(b), "func TestPacks") {
				t.Errorf("%s served a file outside the packs folder", path)
			}
		}
	}
}
