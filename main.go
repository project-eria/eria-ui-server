// eria-ui-server: relay between the ERIA app (eria-ui-app) and the WoT Things of
// the LAN. Discovers the Things, exposes them as services, relays commands and
// observations, and keeps the accounts, terminals and pages.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	eria "github.com/project-eria/eria-core"
	zlog "github.com/rs/zerolog/log"
)

// Context is a context of manager-contexts (a state of the house: away,
// guests…) that people switch on and off.
type Context struct {
	ID    string `yaml:"id" json:"id"`
	Label string `yaml:"label" json:"label"`
}

var config = struct {
	Host     string `yaml:"host,omitempty"`
	Port     uint   `yaml:"port" default:"8090"`
	Database string `yaml:"database" default:"eria-ui-server.db"` // SQLite file: profiles, sessions, terminals, pages
	// First admin's PIN, used only while there is no profile. The orchestrator's
	// deployment config is the place for secrets (never eria-app.yml).
	AdminPin string `yaml:"adminPin,omitempty"`
	// Signed theme archives (env ERIA_UI_SERVER_THEMES overrides it).
	Themes string `yaml:"themes" default:"themes"`
	// First values only, copied to the database at the first start; then
	// edited by the admin (PLAN §10).
	Hide     []string  `yaml:"hide,omitempty"`     // glob patterns on TD ids, see config.sample.yml
	Contexts []Context `yaml:"contexts,omitempty"` // contexts offered to people, in display order
}{}

// maxBody bounds the JSON values exchanged with a Thing.
const maxBody = 64 << 10

func main() {
	eria.Init("ERIA UI server", &config)
	defer eria.Close()

	// The orchestrator's agent gives the app a data folder: ERIA_UI_SERVER_DATABASE={data}/eria-ui-server.db
	if path := os.Getenv("ERIA_UI_SERVER_DATABASE"); path != "" {
		config.Database = path
	}
	auth, err := OpenAuth(config.Database)
	if err != nil {
		zlog.Fatal().Err(err).Str("file", config.Database).Msg("[main] Can't open the database")
	}
	// First admin's PIN: environment (manual install) or deployment config (orchestrator).
	pin := os.Getenv("ERIA_UI_SERVER_ADMIN_PIN")
	if pin == "" {
		pin = config.AdminPin
	}
	if err := auth.Bootstrap(pin); err != nil {
		zlog.Fatal().Err(err).Msg("[main] Can't create the first profile")
	}
	if err := auth.SeedSettings(config.Contexts, config.Hide); err != nil {
		zlog.Fatal().Err(err).Msg("[main] Can't store the first settings")
	}
	hidden, err := auth.Hidden()
	if err != nil {
		zlog.Fatal().Err(err).Msg("[main] Can't read the hidden Things")
	}

	if dir := os.Getenv("ERIA_UI_SERVER_THEMES"); dir != "" {
		config.Themes = dir
	}

	registry := NewRegistry(hidden)
	ctx, stop := context.WithCancel(context.Background())
	go func() {
		if err := registry.Discover(ctx); err != nil {
			zlog.Fatal().Err(err).Msg("[main] mDNS discovery failed")
		}
	}()

	go registry.Poll(ctx, auth.ManualURLs)
	hub := NewHub(registry)
	go hub.WatchStatus(ctx)
	go announce(ctx, int(config.Port))

	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", config.Host, config.Port),
		Handler: newMux(&Channel{hub: hub, auth: auth, registry: registry, themes: config.Themes}),
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

func newMux(ch *Channel) *http.ServeMux {
	mux := http.NewServeMux()

	// Everything the app does with the Things goes through this WebSocket.
	mux.HandleFunc("GET /api/ws", ch.ServeWS)

	// Health probe of the orchestrator's agent (public, see eria-app.yml).
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		zlog.Error().Err(err).Msg("[main:writeJSON]")
	}
}
