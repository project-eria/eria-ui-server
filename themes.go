package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Themes (eria-ui-app PLAN §5.00): one signed archive per theme, dropped in
// the themes folder as <id>.tar and <id>.tar.sig. The server only serves
// them; the app checks the signature before loading anything.

type Theme struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Version string        `json:"version"` // from theme.json
	Hash    string        `json:"hash"`    // of the archive: the app re-downloads when it changes
	Aspects []ThemeAspect `json:"aspects"`
	Layouts []string      `json:"layouts"` // offered when a page is created (theme.json pages: layout, else tag)
	Report  bool          `json:"report"`  // ships a REPORT.md (else "unverified")
	Error   string        `json:"error,omitempty"`
}

type ThemeAspect struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// maxTheme bounds an archive (fonts and icons included).
const maxTheme = 16 << 20

var themeID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// Themes lists the archives of dir; an unreadable one is listed with its error.
func Themes(dir string) []Theme {
	list := []Theme{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".tar")
		if !ok || !themeID.MatchString(id) {
			continue
		}
		t, err := readTheme(dir, id)
		if err != nil {
			t = Theme{ID: id, Error: err.Error()}
		}
		list = append(list, t)
	}
	return list
}

func readTheme(dir, id string) (Theme, error) {
	archive, err := os.ReadFile(filepath.Join(dir, id+".tar"))
	if err != nil {
		return Theme{}, err
	}
	if len(archive) > maxTheme {
		return Theme{}, errors.New("archive too large")
	}
	if _, err := os.Stat(filepath.Join(dir, id+".tar.sig")); err != nil {
		return Theme{}, errors.New("no signature (" + id + ".tar.sig)")
	}
	var manifest struct {
		ID, Name, Version string
		Aspects           []ThemeAspect
		Pages             []struct{ Tag, Layout string }
	}
	report := false
	r := tar.NewReader(bytes.NewReader(archive))
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Theme{}, err
		}
		switch strings.TrimPrefix(h.Name, "./") {
		case "theme.json":
			if err := json.NewDecoder(r).Decode(&manifest); err != nil {
				return Theme{}, errors.New("theme.json: " + err.Error())
			}
		case "REPORT.md":
			report = true
		}
	}
	if manifest.ID != id {
		return Theme{}, errors.New("theme.json id differs from the archive name")
	}
	layouts := []string{}
	for _, p := range manifest.Pages {
		l := p.Layout
		if l == "" {
			l = p.Tag
		}
		if l != "" && !slices.Contains(layouts, l) {
			layouts = append(layouts, l)
		}
	}
	sum := sha256.Sum256(archive)
	return Theme{ID: id, Name: manifest.Name, Version: manifest.Version, Hash: hex.EncodeToString(sum[:8]),
		Aspects: manifest.Aspects, Layouts: layouts, Report: report}, nil
}

// themeArchive returns a theme's archive and signature, for the app to check.
func themeArchive(dir, id string) (map[string]string, error) {
	if !themeID.MatchString(id) {
		return nil, errUnknown
	}
	archive, err := os.ReadFile(filepath.Join(dir, id+".tar"))
	if err != nil {
		return nil, errUnknown
	}
	sig, err := os.ReadFile(filepath.Join(dir, id+".tar.sig"))
	if err != nil {
		return nil, errUnknown
	}
	return map[string]string{"archive": base64.StdEncoding.EncodeToString(archive), "signature": strings.TrimSpace(string(sig))}, nil
}

// themeHash is the version the resource manifest gives for a theme; "" when
// the theme is Base or missing (the app then shows Base).
func themeHash(dir, id string) string {
	if id == "" {
		return ""
	}
	t, err := readTheme(dir, id)
	if err != nil {
		return ""
	}
	return t.Hash
}
