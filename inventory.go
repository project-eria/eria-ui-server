package main

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/project-eria/go-wot/inventory"
	zlog "github.com/rs/zerolog/log"
)

// The master's inventory (AUTH-DESIGN §2.4, F037): eria-ui-server no longer
// browses DNS-SD; it reads the Things from the master, and sends its service
// tokens only to their URLs, each Thing its own. Without `master` (a bench without one) it browses
// mDNS and sends no token.
//
// ponytail: the Things added by address by the admin get no token either;
// they are for networks where mDNS is filtered, which the master now covers.

var (
	_inventoryMu sync.RWMutex
	_inventory   []inventory.Entry
)

// tokenFor is the service token for the Thing at url, "" when url is not a
// Thing of the inventory.
func tokenFor(url string) string {
	if thing := thingAt(url); thing != "" {
		return serviceToken(thing)
	}
	return ""
}

// thingAt is the inventory's Thing u belongs to: same origin as its TD, the
// longest TD path u starts with (an app serves its Things under /<ref>, an
// ESP32 under /); "" when none.
func thingAt(u string) string {
	origin, path := inventory.Origin(u), pathOf(u)
	_inventoryMu.RLock()
	defer _inventoryMu.RUnlock()
	best, length := "", -1
	for _, e := range _inventory {
		p := pathOf(e.URL)
		if inventory.Origin(e.URL) == origin && (path == p || strings.HasPrefix(path, p+"/")) && len(p) > length {
			best, length = e.ID, len(p)
		}
	}
	return best
}

func pathOf(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(p.Path, "/")
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
		if token := serviceToken(""); token != "" {
			list, err := inventory.Read(ctx, client, master, token)
			if err != nil {
				zlog.Warn().Err(err).Msg("[inventory] Can't read the master's inventory, the last one kept")
			} else {
				_inventoryMu.Lock()
				_inventory = list
				_inventoryMu.Unlock()
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
