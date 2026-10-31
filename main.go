// eria-ui-server: the relay between the ERIA app (eria-ui-app) and the WoT
// Things of the LAN (AUTH-DESIGN §3.6). Discovers the Things, exposes them as
// services, relays commands and observations, and carries the apps' tunnels
// to auth. It keeps no state: tokens, pages and settings are auth's.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"

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
	Host string `yaml:"host,omitempty"`
	Port uint   `yaml:"port" default:"8090"` // the apps' WebSocket, in TLS
	// Plain HTTP health probe for the orchestrator's agent, local only.
	Health string `yaml:"health" default:"127.0.0.1:8099"`
	// TD id of service-notices, whose notifications are given to the apps.
	Notices string `yaml:"notices" default:"urn:eria:service:notices"`
	// Signed theme archives.
	Themes string `yaml:"themes" default:"themes"`
	// auth (AUTH-DESIGN §3.6): its tunnel listener, the house id, and the
	// relay's certificate (role relais-interieur or relais-exterieur) signed
	// by the master, with the deployment roots and the auth keyset.
	Auth       string `yaml:"auth" required:"true"`
	Deployment string `yaml:"deployment" required:"true"`
	Cert       string `yaml:"cert" default:"relay.crt"`
	CertKey    string `yaml:"certKey" default:"relay-tls.key"`
	Roots      string `yaml:"roots" default:"roots.pem"`
	Keyset     string `yaml:"keyset" default:"auth-keyset.json"`
}{}

// maxBody bounds the JSON values exchanged with a Thing.
const maxBody = 64 << 10

func main() {
	// The agent gives the app a data folder: the default file names live
	// there, for the local commands too.
	if dir := os.Getenv("ERIA_UI_SERVER_DATA"); dir != "" {
		if err := os.Chdir(dir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if len(os.Args) > 1 && os.Args[1] == "csr" {
		if err := csr(os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	eria.Init("ERIA UI server", &config)
	defer eria.Close()

	cert, err := tls.LoadX509KeyPair(config.Cert, config.CertKey)
	if err != nil {
		zlog.Fatal().Err(err).Msg("[main] No TLS certificate (eria-ui-server csr, then eria-manager tls-sign)")
	}
	roots, err := os.ReadFile(config.Roots)
	if err != nil {
		zlog.Fatal().Err(err).Msg("[main] No deployment roots (eria-manager tls-root)")
	}
	auth, err := NewAuthClient(config.Auth, config.Deployment, cert, roots, config.Keyset)
	if err != nil {
		zlog.Fatal().Err(err).Msg("[main] Can't reach auth")
	}

	serviceToken = auth.ServiceToken
	registry := NewRegistry(nil)
	ctx, stop := context.WithCancel(context.Background())
	go func() {
		if err := registry.Discover(ctx); err != nil {
			zlog.Fatal().Err(err).Msg("[main] mDNS discovery failed")
		}
	}()

	go registry.Poll(ctx, auth.ManualURLs)
	hub := NewHub(registry)
	go auth.Run(ctx, func(old, new Config) {
		if !slices.Equal(old.Hidden, new.Hidden) || !slices.Equal(old.Manual, new.Manual) {
			registry.SetHide(new.Hidden)
			hub.broadcast("", map[string]string{"push": "things-changed"})
		}
		if !slices.Equal(old.Contexts, new.Contexts) {
			hub.broadcast("", map[string]string{"push": "contexts-changed"})
		}
		hub.refresh("") // ponytail: every app re-reads its manifest; fine for a household
	}, hub.revalidate)
	go hub.WatchStatus(ctx)
	go hub.watchNotices(ctx, config.Notices)
	go announce(ctx, int(config.Port))

	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", config.Host, config.Port),
		Handler: newMux(&Channel{hub: hub, auth: auth, registry: registry, themes: config.Themes, noticesThing: config.Notices}),
	}
	server.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
	go func() {
		zlog.Info().Str("addr", server.Addr).Msg("[main] Listening (TLS)")
		if err := server.ListenAndServeTLS("", ""); err != http.ErrServerClosed {
			zlog.Fatal().Err(err).Msg("[main] HTTPS server failed")
		}
	}()
	// The agent's health gate probes in plain HTTP, on the node only.
	health := http.NewServeMux()
	health.HandleFunc("GET /api/health", healthHandler)
	go func() {
		if err := http.ListenAndServe(config.Health, health); err != nil {
			zlog.Fatal().Err(err).Msg("[main] Health listener failed")
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

	mux.HandleFunc("GET /api/health", healthHandler)
	return mux
}

// healthHandler answers the orchestrator's agent: its gate wants 200 and a
// JSON body (eria-agent probeWoT).
func healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		zlog.Error().Err(err).Msg("[main:writeJSON]")
	}
}

// csr makes the relay's TLS key in the current folder (if absent) and prints
// its certificate request, for eria-manager tls-sign --role relais-interieur
// (or relais-exterieur).
func csr(out io.Writer) error {
	const keyFile = "relay-tls.key"
	var priv *ecdsa.PrivateKey
	data, err := os.ReadFile(keyFile)
	switch {
	case errors.Is(err, os.ErrNotExist):
		priv, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		der, _ := x509.MarshalECPrivateKey(priv)
		if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		block, _ := pem.Decode(data)
		if block == nil {
			return errors.New(keyFile + ": not PEM")
		}
		if priv, err = x509.ParseECPrivateKey(block.Bytes); err != nil {
			return err
		}
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, priv)
	if err != nil {
		return err
	}
	return pem.Encode(out, &pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}
