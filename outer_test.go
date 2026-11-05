package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// roleCert is a self-signed certificate for a role, as eria-manager signs them.
func roleCert(t *testing.T, role string) tls.Certificate {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{role},
		NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour)}, &x509.Certificate{SerialNumber: big.NewInt(1)}, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}
}

// Step 5f: the side comes from the certificate's role; the outer server
// writes no page, the inner one takes the house's networks only and tells
// the apps where the outer one is.
func TestOuterAndInner(t *testing.T) {
	fa := newFakeAuth(t)
	registry := NewRegistry(nil)
	serve := func(ch *Channel) (string, func()) {
		ch.hub, ch.auth, ch.registry = NewHub(registry), fa.client, registry
		srv := httptest.NewServer(newMux(ch))
		return "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/ws", srv.Close
	}
	dial := func(url, token string) (*testApp, *http.Response) {
		conn, resp, err := websocket.DefaultDialer.Dial(url, http.Header{"Authorization": {"Bearer " + token}})
		if err != nil {
			return nil, resp
		}
		return &testApp{t: t, conn: conn}, resp
	}
	parent := fa.token("salon", "profil:1", 1, 5*time.Minute)

	outer := &Channel{}
	if err := outer.place(roleCert(t, roleOuter), []string{"10.9.0.0/16"}, "wss://x"); err != nil || !outer.outer || outer.inside != nil {
		t.Fatalf("outer place = %v, %+v", err, outer)
	}
	url, stop := serve(outer)
	defer stop()
	a, _ := dial(url, parent)
	if w := a.push("welcome"); w["outside"] != true || w["outerUrl"] != nil {
		t.Errorf("outer welcome = %v", w)
	}
	if r := a.call("savePage", map[string]any{"value": map[string]any{"title": "x"}}); errType(r) != failRefused {
		t.Errorf("savePage from outside = %v, want refused", r)
	}

	inner := &Channel{}
	if err := inner.place(roleCert(t, roleInner), []string{"10.9.0.0/16", "fd00:cafe::/48"}, "wss://10.20.0.5:8090"); err != nil {
		t.Fatal(err)
	}
	if !inner.fromInside("10.9.3.4:5000") || !inner.fromInside("[fd00:cafe::1]:5000") || inner.fromInside("192.168.1.2:5000") {
		t.Error("insideNets not applied")
	}
	url, stop = serve(inner)
	defer stop()
	if _, resp := dial(url, parent); resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("a connection from 127.0.0.1 outside insideNets was taken")
	}
	inner.inside = nil // every address
	a, _ = dial(url, parent)
	if w := a.push("welcome"); w["outside"] != false || w["outerUrl"] != "wss://10.20.0.5:8090" {
		t.Errorf("inner welcome = %v", w)
	}

	if err := (&Channel{}).place(roleCert(t, "auth"), nil, ""); err == nil {
		t.Error("a certificate of another role was taken")
	}
}
