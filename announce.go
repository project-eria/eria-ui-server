package main

import (
	"context"
	"os"

	"github.com/brutella/dnssd"
	eria "github.com/project-eria/eria-core"
	zlog "github.com/rs/zerolog/log"
)

// serviceType is how the apps find the server on the LAN (PLAN §2.1). It is
// the same for every ERIA network: the app lists the servers of all of them.
const serviceType = "_eria-ui._tcp"

// announce advertises the server over mDNS until ctx is done; the apps
// browse for it. A failure only costs the discovery: the address can be typed.
// The TXT key `net` carries the server's network (absent for the default
// one): the app proposes it as the server's name when adding it.
func announce(ctx context.Context, port int) {
	name, _ := os.Hostname()
	var txt map[string]string
	if network := eria.Network(); network != "" {
		txt = map[string]string{"net": network}
	}
	srv, err := dnssd.NewService(dnssd.Config{Name: "ERIA " + name, Type: serviceType, Port: port, Text: txt})
	if err != nil {
		zlog.Error().Err(err).Msg("[announce] mDNS service")
		return
	}
	rp, err := dnssd.NewResponder()
	if err != nil {
		zlog.Error().Err(err).Msg("[announce] mDNS responder")
		return
	}
	if _, err := rp.Add(srv); err != nil {
		zlog.Error().Err(err).Msg("[announce] mDNS registration")
		return
	}
	zlog.Info().Str("type", serviceType).Str("network", eria.Network()).Int("port", port).Msg("[announce] advertising")
	if err := rp.Respond(ctx); err != nil && ctx.Err() == nil {
		zlog.Error().Err(err).Msg("[announce] mDNS responder stopped")
	}
}
