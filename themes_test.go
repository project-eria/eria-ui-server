package main

import (
	"archive/tar"
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeTheme(t *testing.T, dir, name string, files map[string]string, signed bool) {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for path, content := range files {
		w.WriteHeader(&tar.Header{Name: path, Mode: 0o644, Size: int64(len(content))})
		w.Write([]byte(content))
	}
	w.Close()
	os.WriteFile(filepath.Join(dir, name+".tar"), buf.Bytes(), 0o644)
	if signed {
		os.WriteFile(filepath.Join(dir, name+".tar.sig"), []byte("c2lnbmF0dXJl\n"), 0o644)
	}
}

func TestThemes(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "minimal", map[string]string{
		"theme.json": `{"id":"minimal","name":"Minimal","version":"0.1.0","aspects":[{"id":"clair","name":"Clair"}],
			"pages":[{"tag":"minimal-page"},{"tag":"minimal-wall","layout":"main"},{"tag":"minimal-phone","layout":"main"}]}`,
		"index.js": "export {};",
	}, true)
	writeTheme(t, dir, "nosig", map[string]string{"theme.json": `{"id":"nosig"}`}, false)
	writeTheme(t, dir, "renamed", map[string]string{"theme.json": `{"id":"other"}`}, true)

	byID := map[string]Theme{}
	for _, th := range Themes(dir) {
		byID[th.ID] = th
	}
	if m := byID["minimal"]; m.Error != "" || m.Name != "Minimal" || len(m.Aspects) != 1 || m.Hash == "" || m.Report ||
		!slices.Equal(m.Layouts, []string{"minimal-page", "main"}) {
		t.Errorf("minimal = %+v", m)
	}
	if byID["nosig"].Error == "" || byID["renamed"].Error == "" {
		t.Errorf("unsigned or renamed theme accepted: %+v %+v", byID["nosig"], byID["renamed"])
	}
	if themeHash(dir, "minimal") != byID["minimal"].Hash || themeHash(dir, "nosig") != "" || themeHash(dir, "") != "" {
		t.Error("themeHash gives a wrong answer")
	}
	a, err := themeArchive(dir, "minimal")
	if err != nil || a["signature"] != "c2lnbmF0dXJl" {
		t.Fatalf("archive = %v, %v", a, err)
	}
	if raw, _ := base64.StdEncoding.DecodeString(a["archive"]); len(raw) == 0 {
		t.Error("empty archive")
	}
	if _, err := themeArchive(dir, "../minimal"); err == nil {
		t.Error("path outside the themes folder accepted")
	}
}
