package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/project-eria/go-wot/inventory"
	zlog "github.com/rs/zerolog/log"
)

// The app's versions (AUTH-DESIGN §3.4, step 5g-2): the inner
// eria-ui-server reads the highest published one from the coordinator, tells the
// apps (push "resources"), and serves the Tauri updater its manifest and the
// package, fetched from the coordinator's artifact server. It checks nothing: the
// app does, with the descriptor signed by the deployment key, the package's
// sha256, then the updater with the update key's minisign signature.

// Release is the coordinator's property appRelease.
type Release struct {
	Version string                   `json:"version"`
	Targets map[string]releaseTarget `json:"targets"`
}

type releaseTarget struct {
	URL        string          `json:"url"` // on the coordinator's artifact server
	Sha256     string          `json:"sha256"`
	Signature  string          `json:"signature"`  // minisign, for the updater
	Descriptor json.RawMessage `json:"descriptor"` // AppRelease signed by the deployment key
}

var _release atomic.Pointer[Release]

// releaseEvery is how often the coordinator is asked: a new version reaches the
// apps within it.
const releaseEvery = 10 * time.Minute

// releaseNow asks followRelease to read the coordinator at once (an app's
// « Rechercher une mise à jour »); the channel it sends is closed once read.
var releaseNow = make(chan chan struct{})

// checkRelease reads the coordinator now when followRelease runs (not on a
// bench without coordinator), then gives what the apps get.
func checkRelease() map[string]any {
	done := make(chan struct{})
	select {
	case releaseNow <- done:
		select {
		case <-done:
		case <-time.After(15 * time.Second):
		}
	case <-time.After(time.Second):
	}
	return releasePush()
}

// followRelease reads the coordinator's published version until ctx is done;
// changed runs when it changes.
func followRelease(ctx context.Context, coordinator string, roots []byte, changed func()) {
	client, err := inventory.CoordinatorClient(roots, 10*time.Second)
	if err != nil {
		zlog.Fatal().Err(err).Msg("[release] Bad deployment roots")
	}
	t := time.NewTicker(releaseEvery)
	defer t.Stop()
	read := func() {
		if token := serviceToken(""); token != "" {
			if r, err := readRelease(ctx, client, coordinator, token); err != nil {
				zlog.Warn().Err(err).Msg("[release] Can't read the app's release from the coordinator")
			} else if old := _release.Swap(r); version(old) != version(r) {
				zlog.Info().Str("version", version(r)).Msg("[release] App release")
				changed()
			}
		}
	}
	read()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			read()
		case done := <-releaseNow:
			read()
			close(done)
		}
	}
}

func version(r *Release) string {
	if r == nil {
		return ""
	}
	return r.Version
}

func readRelease(ctx context.Context, client *http.Client, coordinator, token string) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(coordinator, "/")+"/appRelease", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("coordinator answered %d", resp.StatusCode)
	}
	var r *Release // null: none published
	return r, json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r)
}

// releasePush is what the apps get: the version and each target's descriptor.
func releasePush() map[string]any {
	r := _release.Load()
	if r == nil {
		return nil
	}
	descriptors := map[string]json.RawMessage{}
	for t, x := range r.Targets {
		descriptors[t] = x.Descriptor
	}
	return map[string]any{"version": r.Version, "descriptors": descriptors}
}

// packageClient fetches a package from the coordinator's artifact server.
var packageClient = &http.Client{Timeout: 10 * time.Minute}

// serveRelease answers the Tauri updater: GET /api/release/{target} is its
// manifest (204: nothing), GET /api/release/{target}/package the package.
func (ch *Channel) serveRelease(w http.ResponseWriter, r *http.Request) {
	rel := _release.Load()
	if ch.outer || rel == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	target, ok := rel.Targets[r.PathValue("target")]
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if what := r.PathValue("what"); what != "" && what != "package" {
		http.NotFound(w, r)
		return
	}
	if r.PathValue("what") == "" {
		writeJSON(w, map[string]string{"version": rel.Version, "signature": target.Signature,
			"url": "https://" + r.Host + "/api/release/" + r.PathValue("target") + "/package"})
		return
	}
	resp, err := packageClient.Get(target.URL) // the coordinator's, never the request's
	if err != nil || resp.StatusCode != http.StatusOK {
		http.Error(w, "package unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	if resp.ContentLength > 0 {
		w.Header().Set("Content-Length", fmt.Sprint(resp.ContentLength))
	}
	io.Copy(w, resp.Body)
}
