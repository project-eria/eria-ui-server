package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/project-eria/go-wot/auth"
)

// fakeAuth plays auth for the relay: it signs tokens and the revocation
// list, keeps the configuration, and echoes the tunnels' bytes.
type fakeAuth struct {
	t      *testing.T
	priv   ed25519.PrivateKey
	pub    auth.PublicKey
	mu     sync.Mutex
	config Config
	revoke []auth.Revocation
	saved  []string // tokens that saved a page
	client *AuthClient
}

func newFakeAuth(t *testing.T) *fakeAuth {
	pubRaw, priv, _ := ed25519.GenerateKey(rand.Reader)
	pub, _ := auth.ParsePublicKey(auth.Ed25519, pubRaw)
	f := &fakeAuth{t: t, priv: priv, pub: pub, config: Config{Version: 1}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)

	echo, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Cleanup(func() { echo.Close() })
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go io.Copy(c, c)
		}
	}()

	f.client = &AuthClient{aud: "eria-maison", now: time.Now,
		keys: func(kid string) (auth.PublicKey, bool, bool) { return pub, false, kid == pub.KID() },
		http: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
			return http.DefaultTransport.RoundTrip(r)
		})},
		dial: func(context.Context) (net.Conn, error) { return net.Dial("tcp", echo.Addr().String()) },
	}
	f.sync()
	return f
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// sync makes the relay read the configuration and the revocations now.
func (f *fakeAuth) sync() {
	f.client.pollConfig(func(old, new Config) {})
	f.client.pollRevocations(func() {})
}

func (f *fakeAuth) token(term, sub string, lvl int, ttl time.Duration) string {
	now := time.Now()
	c := auth.Claims{Iss: "auth", Aud: "eria-maison", Kind: auth.KindUser, Term: term, Sub: sub, Lvl: &lvl,
		Iat: now.Unix(), Exp: now.Add(ttl).Unix(), Jti: randomHex()}
	tok, err := auth.Sign(f.priv, f.pub.KID(), c)
	if err != nil {
		f.t.Fatal(err)
	}
	return tok
}

func randomHex() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (f *fakeAuth) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/v1/config":
		json.NewEncoder(w).Encode(f.config)
	case "/v1/revocations":
		doc, _ := auth.SignDoc(f.priv, f.pub.KID(), auth.Doc{Iss: "auth", Aud: "eria-maison",
			Doc: auth.DocRevocation, Thing: "*", Version: time.Now().UnixNano(), Revoked: f.revoke})
		json.NewEncoder(w).Encode(map[string]string{"doc": doc})
	case "/v1/pages":
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		c, _, err := auth.Parse(token, f.client.keys)
		if err != nil || c.Sub == "" || *c.Lvl > 1 {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "denied"})
			return
		}
		var p Page
		json.NewDecoder(r.Body).Decode(&p)
		p.ID, p.Version = "p1", p.Version+1
		f.config.Pages = []Page{p}
		f.config.Version++
		f.saved = append(f.saved, token)
		json.NewEncoder(w).Encode(p)
	case "/v1/screen":
		w.Write([]byte(`{"ok":true}`))
	default:
		http.NotFound(w, r)
	}
}
