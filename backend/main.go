// ui-user backend: discovers the WoT Things of the LAN and exposes them to
// the user dashboard as services, through a single HTTP entry point.
package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"

	eria "github.com/project-eria/eria-core"
	zlog "github.com/rs/zerolog/log"
)

// Mode is a context of manager-contexts that users switch on and off.
type Mode struct {
	ID    string `yaml:"id" json:"id"`
	Label string `yaml:"label" json:"label"`
}

var config = struct {
	Host     string `yaml:"host,omitempty"`
	Port     uint   `yaml:"port" default:"8090"`
	Database string `yaml:"database" default:"ui-user.db"` // SQLite file: profiles, sessions, screens
	// First admin's PIN, used only while there is no profile. The orchestrator's
	// deployment config is the place for secrets (never eria-app.yml).
	AdminPin string `yaml:"adminPin,omitempty"`
	// Widget packs folder (env UI_USER_PACKS overrides it), see PACKS.md.
	Packs string   `yaml:"packs" default:"packs"`
	Hide  []string `yaml:"hide,omitempty"`  // glob patterns on TD ids, see config.sample.yml
	Modes []Mode   `yaml:"modes,omitempty"` // contexts offered as modes, in display order
}{}

// maxBody bounds the JSON values a browser may send to a Thing.
const maxBody = 64 << 10

// web is the SvelteKit build (`pnpm build` in ../frontend writes it here).
//
//go:embed all:web
var web embed.FS

func main() {
	eria.Init("ERIA UI user", &config)
	defer eria.Close()

	// The orchestrator's agent gives the app a data folder: UI_USER_DATABASE={data}/ui-user.db
	if path := os.Getenv("UI_USER_DATABASE"); path != "" {
		config.Database = path
	}
	auth, err := OpenAuth(config.Database)
	if err != nil {
		zlog.Fatal().Err(err).Str("file", config.Database).Msg("[main] Can't open the database")
	}
	// First admin's PIN: environment (manual install) or deployment config (orchestrator).
	pin := os.Getenv("UI_USER_ADMIN_PIN")
	if pin == "" {
		pin = config.AdminPin
	}
	auth.PacksDir = config.Packs
	if dir := os.Getenv("UI_USER_PACKS"); dir != "" {
		auth.PacksDir = dir
	}
	if err := auth.Bootstrap(pin); err != nil {
		zlog.Fatal().Err(err).Msg("[main] Can't create the first profile")
	}

	registry := NewRegistry(config.Hide)
	ctx, stop := context.WithCancel(context.Background())
	go func() {
		if err := registry.Discover(ctx); err != nil {
			zlog.Fatal().Err(err).Msg("[main] mDNS discovery failed")
		}
	}()

	hub := NewHub(registry)
	go hub.WatchStatus(ctx)

	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", config.Host, config.Port),
		Handler: withAuth(newMux(registry, hub), auth),
	}
	go func() {
		zlog.Info().Str("addr", server.Addr).Msg("[main] Listening")
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			zlog.Fatal().Err(err).Msg("[main] HTTP server failed")
		}
	}()

	eria.WaitForSignal()
	stop()
	server.Shutdown(context.Background())
}

func withAuth(mux *http.ServeMux, auth *Auth) http.Handler {
	auth.Routes(mux)
	auth.ScreenRoutes(mux)
	PackRoutes(mux, auth.PacksDir)
	return auth.Protect(mux)
}

func newMux(registry *Registry, hub *Hub) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/ws", hub.ServeWS)

	// Health probe of the orchestrator's agent (public, see eria-app.yml).
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /api/services", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, registry.Services())
	})

	mux.HandleFunc("GET /api/modes", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, append([]Mode{}, config.Modes...)) // never null
	})

	// Property and action names may contain "/" (e.g. astral "today/sunrise").
	mux.HandleFunc("GET /api/things/{id}/properties/{name...}", func(w http.ResponseWriter, r *http.Request) {
		_, p, ok := lookupProperty(w, r, registry)
		if ok {
			forward(w, http.MethodGet, href(p.Forms, "readproperty"), nil)
		}
	})

	mux.HandleFunc("PUT /api/things/{id}/properties/{name...}", func(w http.ResponseWriter, r *http.Request) {
		td, p, ok := lookupProperty(w, r, registry)
		if !ok {
			return
		}
		if !isWritable(td, r.PathValue("name")) {
			http.Error(w, "read-only property", http.StatusForbidden)
			return
		}
		body, ok := readJSON(w, r)
		if ok {
			forward(w, http.MethodPut, href(p.Forms, "writeproperty"), body)
		}
	})

	mux.HandleFunc("POST /api/things/{id}/actions/{name...}", func(w http.ResponseWriter, r *http.Request) {
		td, found := registry.Thing(r.PathValue("id"))
		a, exists := td.Actions[r.PathValue("name")]
		if !found || !exists {
			http.NotFound(w, r)
			return
		}
		body, ok := readJSON(w, r)
		if ok {
			forward(w, http.MethodPost, href(a.Forms, "invokeaction"), body)
		}
	})

	static, _ := fs.Sub(web, "web")
	mux.Handle("/", spa(static))

	return mux
}

// spa serves the built files, and index.html for client-side routes.
func spa(files fs.FS) http.Handler {
	fileServer := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if _, err := fs.Stat(files, name); name != "" && err != nil {
			http.ServeFileFS(w, r, files, "index.html")
			return
		}
		if strings.HasPrefix(name, "_app/immutable/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, r)
	})
}

func lookupProperty(w http.ResponseWriter, r *http.Request, registry *Registry) (TD, Property, bool) {
	td, found := registry.Thing(r.PathValue("id"))
	p, exists := td.Properties[r.PathValue("name")]
	if !found || !exists {
		http.NotFound(w, r)
		return td, p, false
	}
	// ponytail: properties with uriVariables (contexts isActive/{context})
	// aren't proxied; the "actives" array covers them for now.
	if len(p.UriVariables) > 0 {
		http.Error(w, "uriVariables not supported", http.StatusNotImplemented)
		return td, p, false
	}
	return td, p, true
}

// readJSON reads a bounded, valid JSON body (empty allowed for actions).
func readJSON(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return nil, false
	}
	if len(bytes.TrimSpace(body)) > 0 && !json.Valid(body) {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return nil, false
	}
	return body, true
}

// forward is the single path from the UI to the Things: the place where the
// user's token will be added once an authorization service exists.
func forward(w http.ResponseWriter, method, url string, body []byte) {
	if url == "" {
		http.Error(w, "no HTTP form for this operation", http.StatusNotImplemented)
		return
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json") // required by go-wot
	}
	resp, err := upstream.Do(req)
	if err != nil {
		zlog.Warn().Err(err).Str("url", url).Msg("[main:forward] Thing unreachable")
		http.Error(w, "Thing unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		zlog.Error().Err(err).Msg("[main:writeJSON]")
	}
}
