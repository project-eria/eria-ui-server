package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Widget packs (PLAN §5.2): folders dropped in the packs directory, each with
// a pack.json and an ES module defining its custom elements. They are read at
// every request, so adding or updating a pack needs no restart.

const sdkVersion = 1 // contract version of /sdk/eria-widgets.js, see PACKS.md

var packID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

type PackWidget struct {
	Tag   string              `json:"tag"`
	Sizes map[string][][2]int `json:"sizes,omitempty"` // variant → allowed sizes [w, h]
}

type Pack struct {
	ID      string                `json:"id"`
	Name    string                `json:"name"`
	Version string                `json:"version"`
	SDK     int                   `json:"sdk"`
	Entry   string                `json:"entry"`            // module file, default index.js
	Report  string                `json:"report,omitempty"` // accessibility report file, if any
	Widgets map[string]PackWidget `json:"widgets"`          // capability → widget
	Error   string                `json:"error,omitempty"`  // why the pack can't be used
}

// Packs lists the packs of dir; an invalid one is listed with its error.
func Packs(dir string) []Pack {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []Pack{} // no packs directory: only the built-in Standard pack
	}
	list := []Pack{}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			list = append(list, readPack(dir, e.Name()))
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list
}

func readPack(dir, name string) Pack {
	p := Pack{ID: name}
	b, err := os.ReadFile(filepath.Join(dir, name, "pack.json"))
	if err == nil {
		err = json.Unmarshal(b, &p)
	}
	if err != nil {
		p.Error = "pack.json missing or invalid: " + err.Error()
		return p
	}
	if p.Entry == "" {
		p.Entry = "index.js"
	}
	p.Error = validatePack(dir, name, p)
	return p
}

func validatePack(dir, name string, p Pack) string {
	switch {
	case p.ID != name || !packID.MatchString(p.ID) || p.ID == "standard":
		return fmt.Sprintf("id %q must equal the folder name, in lowercase letters, digits and dashes, and not be \"standard\"", p.ID)
	case p.SDK != sdkVersion:
		return fmt.Sprintf("needs SDK version %d, this ui-user provides %d", p.SDK, sdkVersion)
	case strings.TrimSpace(p.Name) == "" || len(p.Widgets) == 0:
		return "name and at least one widget are required"
	case !fileIn(dir, name, p.Entry):
		return fmt.Sprintf("entry %q not found in the pack", p.Entry)
	case p.Report != "" && !fileIn(dir, name, p.Report):
		return fmt.Sprintf("report %q not found in the pack", p.Report)
	}
	for capa, w := range p.Widgets {
		// Prefixed tags: two packs can never define the same element.
		if !strings.HasPrefix(w.Tag, p.ID+"-") || strings.ContainsAny(w.Tag, " \"'<>") {
			return fmt.Sprintf("widget %s: tag %q must start with %q", capa, w.Tag, p.ID+"-")
		}
		for variant, sizes := range w.Sizes {
			if variant != "tile" && variant != "full" && variant != "value" {
				return fmt.Sprintf("widget %s: unknown variant %q", capa, variant)
			}
			for _, s := range sizes {
				if s[0] < 1 || s[0] > 4 || s[1] < 1 || s[1] > 8 {
					return fmt.Sprintf("widget %s: size %v out of 1-4 × 1-8", capa, s)
				}
			}
		}
	}
	return ""
}

// fileIn tells whether rel is a regular file inside dir/name (no escaping).
func fileIn(dir, name, rel string) bool {
	if !fs.ValidPath(rel) {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, name, filepath.FromSlash(rel)))
	return err == nil && info.Mode().IsRegular()
}

// packExists tells whether id can be chosen: built-in Standard, or a valid pack.
func packExists(dir, id string) bool {
	if id == "" || id == "standard" {
		return true
	}
	return packID.MatchString(id) && readPack(dir, id).Error == ""
}

func PackRoutes(mux *http.ServeMux, dir string) {
	mux.HandleFunc("GET /api/packs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, Packs(dir))
	})
	// Pack files are code run by the app: same origin, never sniffed. os.DirFS
	// refuses paths escaping the directory.
	files := http.StripPrefix("/packs/", http.FileServerFS(os.DirFS(dir)))
	mux.Handle("GET /packs/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-cache") // packs change without a release
		files.ServeHTTP(w, r)
	}))
}
