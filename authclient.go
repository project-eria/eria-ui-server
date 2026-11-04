package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/project-eria/go-wot/auth"
	"github.com/project-eria/go-wot/auth/authz"
	zlog "github.com/rs/zerolog/log"
)

// AuthClient is the relay's side of auth (AUTH-DESIGN §3.6). The relay keeps
// no state of its own: it checks the tokens with the auth keyset signed by
// the master, applies auth's signed revocation list, and serves the
// configuration auth keeps (pages, start pages, contexts, hidden Things).
// It reaches auth in mTLS with its role certificate, like the apps' tunnels.
type AuthClient struct {
	aud  string
	dial func(ctx context.Context) (net.Conn, error) // mTLS connection to auth
	http *http.Client                                // the relay's own TLS session inside it
	now  func() time.Time

	mu      sync.RWMutex
	keys    auth.KeyLookup
	revoked auth.Doc
	config  Config
	changed func(old, new Config) // set by Run
	rights  rights
	journal *auth.Journal // lines for auth's journal (§4.5); nil in tests
}

// Config is what auth keeps for the interfaces (eria-auth config.go).
type Config struct {
	Version   int64            `json:"version"`
	Pages     []Page           `json:"pages"`
	HomePage  string           `json:"homePage"`
	Contexts  []Context        `json:"contexts"`
	Hidden    []string         `json:"hidden"`
	Manual    []ManualThing    `json:"manual"`
	Terminals []TerminalScreen `json:"terminals"`
}

type ManualThing struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

type TerminalScreen struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Screen         string `json:"screen"`
	DetectedScreen string `json:"detectedScreen"`
	HomePage       string `json:"homePage"`
}

// NewAuthClient reaches auth at addr. roots holds the deployment root
// certificates; their Ed25519 keys also verify the auth keyset.
func NewAuthClient(addr, aud string, cert tls.Certificate, rootsPEM []byte, keysetFile string) (*AuthClient, error) {
	roots := x509.NewCertPool()
	var deploy []auth.PublicKey
	for rest := rootsPEM; ; {
		var block *pem.Block
		if block, rest = pem.Decode(rest); block == nil {
			break
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		roots.AddCert(c)
		if k, ok := c.PublicKey.(ed25519.PublicKey); ok {
			pub, _ := auth.ParsePublicKey(auth.Ed25519, k)
			deploy = append(deploy, pub)
		}
	}
	if len(deploy) == 0 {
		return nil, errors.New("no deployment root")
	}
	a := &AuthClient{aud: aud, now: time.Now, rights: rights{tables: map[string]*heldTable{}}}
	a.rights.key, _ = cert.PrivateKey.(*ecdsa.PrivateKey)
	a.journal = auth.NewJournal(a.sendJournal)
	a.dial = func(ctx context.Context) (net.Conn, error) {
		d := tls.Dialer{Config: &tls.Config{RootCAs: roots, ServerName: "auth", Certificates: []tls.Certificate{cert},
			MinVersion: tls.VersionTLS13}}
		return d.DialContext(ctx, "tcp", addr)
	}
	a.http = &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{
		DialTLSContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			outer, err := a.dial(ctx)
			if err != nil {
				return nil, err
			}
			inner := tls.Client(outer, &tls.Config{RootCAs: roots, ServerName: "auth", MinVersion: tls.VersionTLS13})
			return inner, inner.HandshakeContext(ctx)
		}}}

	data, err := os.ReadFile(keysetFile)
	if err != nil {
		return nil, fmt.Errorf("auth keyset: %w", err)
	}
	var doc auth.Signed
	var ks auth.Keyset
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if err := doc.Open(auth.KeysetPrefix, deploy, &ks); err != nil {
		return nil, fmt.Errorf("auth keyset: %w", err)
	}
	if a.keys, err = ks.Lookup(); err != nil {
		return nil, err
	}
	return a, nil
}

// Authenticate checks a token presented by an app (steps 0 to 4).
func (a *AuthClient) Authenticate(token string) (auth.Claims, error) {
	a.mu.RLock()
	keys, revoked := a.keys, a.revoked
	a.mu.RUnlock()
	d := authz.Authenticate(authz.Request{Mode: authz.Normal, Verifier: authz.Relay, Token: token,
		Keys: keys, Aud: a.aud, Now: a.now()})
	if !d.Allow {
		return auth.Claims{}, fmt.Errorf("deny(%d): %s", d.Step, d.Reason)
	}
	if revoked.Revokes(d.Claims) {
		return auth.Claims{}, errors.New("deny(4): revoked")
	}
	return d.Claims, nil
}

// Revokes tells whether the current list makes these claims fall.
func (a *AuthClient) Revokes(c auth.Claims) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.revoked.Revokes(c)
}

// Run keeps the revocation list and the configuration fresh, calling
// changed after each new configuration and revoked after each new list.
func (a *AuthClient) Run(ctx context.Context, changed func(old, new Config), revoked func()) {
	a.mu.Lock()
	a.changed = changed
	a.mu.Unlock()
	if a.journal != nil {
		go a.journal.Run(ctx)
	}
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for n := 0; ; n++ {
		if err := a.pollConfig(changed); err != nil {
			zlog.Warn().Err(err).Msg("[auth] configuration not refreshed")
		}
		if n%5 == 0 {
			if err := a.pollRevocations(revoked); err != nil {
				zlog.Warn().Err(err).Msg("[auth] revocation list not refreshed")
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (a *AuthClient) pollConfig(changed func(old, new Config)) error {
	a.mu.RLock()
	since := a.config.Version
	a.mu.RUnlock()
	var c Config
	status, err := a.call(http.MethodGet, "/v1/config?since="+strconv.FormatInt(since, 10), "", nil, &c)
	if err != nil || status == http.StatusNotModified {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("auth answered %d", status)
	}
	a.mu.Lock()
	old := a.config
	a.config = c
	a.mu.Unlock()
	changed(old, c)
	return nil
}

func (a *AuthClient) pollRevocations(revoked func()) error {
	var out struct{ Doc string }
	if status, err := a.call(http.MethodGet, "/v1/revocations", "", nil, &out); err != nil || status != http.StatusOK {
		return errors.Join(err, fmt.Errorf("auth answered %d", status))
	}
	a.mu.RLock()
	keys, current := a.keys, a.revoked.Version
	a.mu.RUnlock()
	d, err := auth.ParseDoc(out.Doc, keys, a.aud, auth.DocRevocation, "*")
	if err != nil || d.Version <= current {
		return err
	}
	a.mu.Lock()
	a.revoked = d
	a.mu.Unlock()
	revoked()
	return nil
}

// Log queues a line for auth's journal (step 5e-3), never blocking.
func (a *AuthClient) Log(l auth.Line) {
	if a.journal != nil {
		a.journal.Add(l)
	}
}

// sendJournal hands auth a batch of lines, signed by the certificate key:
// auth tells the relay's own batches from the apps' requests it carries.
func (a *AuthClient) sendJournal(b auth.JournalBatch) error {
	if a.rights.key == nil {
		return errors.New("no P-256 certificate key")
	}
	doc, err := auth.SignDocumentP256(a.rights.key, auth.JournalPrefix, b)
	if err != nil {
		return err
	}
	_, err = a.call(http.MethodPost, "/v1/relay/journal", "", doc, nil)
	return err
}

// call sends one request to auth, with an app's token when given, and
// decodes the reply.
func (a *AuthClient) call(method, path, token string, in, out any) (int, error) {
	var body io.Reader
	if in != nil {
		data, _ := json.Marshal(in)
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, "https://auth"+path, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK && out != nil {
		err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
	} else if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotModified {
		var e struct{ Error string }
		json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&e)
		err = &authError{resp.StatusCode, e.Error}
	}
	return resp.StatusCode, err
}

// authError is auth's refusal of a change, with its reason.
type authError struct {
	Status int
	Reason string
}

func (e *authError) Error() string { return e.Reason }

// Tunnel opens a connection to auth for an app's own TLS session.
func (a *AuthClient) Tunnel(ctx context.Context) (net.Conn, error) { return a.dial(ctx) }

// --- Configuration, served from the last copy ---

func (a *AuthClient) snapshot() Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config
}

func (a *AuthClient) Pages() []Page {
	if p := a.snapshot().Pages; p != nil {
		return p
	}
	return []Page{}
}

func (a *AuthClient) Page(id string) (Page, error) {
	for _, p := range a.snapshot().Pages {
		if p.ID == id {
			return p, nil
		}
	}
	return Page{}, errors.New("page not found")
}

// Manifest lists every page with its version, and the themes they use.
func (a *AuthClient) Manifest() (pages []PageRef, themes []string) {
	pages, seen := []PageRef{}, map[string]bool{}
	for _, p := range a.snapshot().Pages {
		pages = append(pages, PageRef{p.ID, p.Version})
		if p.Theme != "" && !seen[p.Theme] {
			seen[p.Theme] = true
			themes = append(themes, p.Theme)
		}
	}
	return pages, themes
}

func (a *AuthClient) HomePage() string { return a.snapshot().HomePage }

func (a *AuthClient) Contexts() []Context {
	if c := a.snapshot().Contexts; c != nil {
		return c
	}
	return []Context{}
}

func (a *AuthClient) Hidden() []string { return a.snapshot().Hidden }

func (a *AuthClient) ManualURLs() []string {
	var urls []string
	for _, m := range a.snapshot().Manual {
		urls = append(urls, m.URL)
	}
	return urls
}

// Terminal returns what the interfaces show of a terminal; one not in the
// last copy (just enrolled) makes the relay read the configuration now.
func (a *AuthClient) Terminal(id string) TerminalScreen {
	for range 2 {
		for _, t := range a.snapshot().Terminals {
			if t.ID == id {
				return t
			}
		}
		a.mu.RLock()
		changed := a.changed
		a.mu.RUnlock()
		if changed == nil {
			changed = func(old, new Config) {}
		}
		if err := a.pollConfig(changed); err != nil {
			break
		}
	}
	return TerminalScreen{ID: id}
}

// --- Changes, decided by auth on the app's token ---

func (a *AuthClient) SavePage(token string, p Page) (Page, error) {
	var saved Page
	_, err := a.call(http.MethodPost, "/v1/pages", token, p, &saved)
	return saved, err
}

func (a *AuthClient) DeletePage(token, id string) error {
	_, err := a.call(http.MethodDelete, "/v1/pages/"+id, token, nil, nil)
	return err
}

func (a *AuthClient) ReportScreen(token, screen string) error {
	_, err := a.call(http.MethodPost, "/v1/screen", token, map[string]string{"screen": screen}, nil)
	return err
}

func (a *AuthClient) SetHomePage(token, id string) error {
	_, err := a.call(http.MethodPut, "/v1/config/home-page", token, map[string]string{"page": id}, nil)
	return err
}
