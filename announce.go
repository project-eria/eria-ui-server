package main

import (
	"context"
	"os"

	"github.com/brutella/dnssd"
	zlog "github.com/rs/zerolog/log"
)

// serviceType is how the apps find the server on the LAN (PLAN §2.1).
const serviceType = "_eria-ui._tcp"

// announce advertises the server over mDNS until ctx is done; the apps
// browse for it. A failure only costs the discovery: the address can be typed.
func announce(ctx context.Context, port int) {
	name, _ := os.Hostname()
	srv, err := dnssd.NewService(dnssd.Config{Name: "ERIA " + name, Type: serviceType, Port: port})
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
	zlog.Info().Str("type", serviceType).Int("port", port).Msg("[announce] advertising")
	if err := rp.Respond(ctx); err != nil && ctx.Err() == nil {
		zlog.Error().Err(err).Msg("[announce] mDNS responder stopped")
	}
}
