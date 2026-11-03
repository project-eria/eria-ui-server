package main

import (
	"context"
	"sync"
	"time"

	"github.com/project-eria/go-wot/inventory"
	zlog "github.com/rs/zerolog/log"
)

// The master's inventory (AUTH-DESIGN §2.4, F037): eria-ui-server no longer
// browses DNS-SD; it reads the Things from the master, and sends its service
// token only to their URLs. Without `master` (a bench without one) it browses
// mDNS and sends no token.
//
// ponytail: the Things added by address by the admin get no token either;
// they are for networks where mDNS is filtered, which the master now covers.

var (
	_originsMu sync.RWMutex
	_origins   = map[string]bool{}
)

// tokenFor is the service token for url, "" when url is not a Thing of the
// inventory.
func tokenFor(url string) string {
	_originsMu.RLock()
	ok := _origins[inventory.Origin(url)]
	_originsMu.RUnlock()
	if !ok {
		return ""
	}
	return serviceToken()
}

// Follow reads the master's inventory every browse round (10 s) until ctx is
// done, as Discover would announce the Things.
// The master is reached with its certificate checked against the deployment
// roots (role maitre) once it serves HTTPS (AUTH-DESIGN §4.7 step 5d).
func (r *Registry) Follow(ctx context.Context, master string, roots []byte) {
	client, err := inventory.MasterClient(roots, 5*time.Second)
	if err != nil {
		zlog.Fatal().Err(err).Msg("[inventory] Bad deployment roots")
	}
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		if token := serviceToken(); token != "" {
			list, err := inventory.Read(ctx, client, master, token)
			if err != nil {
				zlog.Warn().Err(err).Msg("[inventory] Can't read the master's inventory, the last one kept")
			} else {
				origins := map[string]bool{}
				for _, e := range list {
					origins[inventory.Origin(e.URL)] = true
				}
				_originsMu.Lock()
				_origins = origins
				_originsMu.Unlock()
				for _, e := range list {
					r.seen(e.URL, e.Node, false)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
