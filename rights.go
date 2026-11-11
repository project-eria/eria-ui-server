package main

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/project-eria/go-wot/auth"
	"github.com/project-eria/go-wot/auth/authz"
	zlog "github.com/rs/zerolog/log"
)

// Rights at the relay (AUTH-DESIGN §3.6). read, write and invoke go to the
// Thing with the app's token: the Thing decides. The relay reads the TDs and
// follows the observations with its own service token, then applies the
// same decision (authz) to each app's token before passing a value on. It
// also uses it to tell the app what it may do (Can), display only.

const (
	domainRelayToken = "eria-relay-token-v1"
	tableTTL         = 30 * time.Second
)

// serviceToken is the relay's own token for a Thing (its DNS-SD instance),
// "" for the master; "" when auth gives none, and in tests.
var serviceToken = func(thing string) string { return "" }

func setBearer(h http.Header, token string) {
	if token != "" {
		h.Set("Authorization", "Bearer "+token)
	}
}

type rights struct {
	key *ecdsa.PrivateKey // the relay's certificate key: proves its token requests

	mu      sync.Mutex
	tokens  map[string]heldToken // by Thing, "" for the master
	tables  map[string]*heldTable
	refresh sync.Mutex // one service token request at a time
}

type heldToken struct {
	token            string
	iat, exp, failed time.Time
}

type heldTable struct {
	doc      auth.Doc
	fetched  time.Time
	fetching bool
}

// ServiceToken returns the relay's token for thing (§4.7 step 4e: a Thing
// can't replay it at the others), "" for the master's, renewed every minute:
// its grants follow the Thing's rooms, which the admin changes at any time.
// A failure is retried after 30 s (a Thing auth doesn't know yet).
// ponytail: renewal by age, a push from auth when the grants change if the
// minute matters; one request at a time across the Things (a few ms on the
// LAN).
func (a *AuthClient) ServiceToken(thing string) string {
	if a.rights.key == nil {
		return ""
	}
	a.rights.refresh.Lock()
	defer a.rights.refresh.Unlock()
	a.rights.mu.Lock()
	h, now := a.rights.tokens[thing], a.now()
	a.rights.mu.Unlock()
	fresh := now.Before(h.iat.Add(time.Minute)) && now.Before(h.exp.Add(-5*time.Minute))
	if !fresh && now.After(h.failed.Add(30*time.Second)) {
		if t, err := a.newServiceToken(thing); err == nil {
			h = t
		} else {
			zlog.Warn().Err(err).Str("thing", thing).Msg("[rights] no service token")
			h.failed = now
		}
		a.rights.mu.Lock()
		a.rights.tokens[thing] = h
		a.rights.mu.Unlock()
	}
	if !now.Before(h.exp) {
		return ""
	}
	return h.token
}

func (a *AuthClient) newServiceToken(thing string) (heldToken, error) {
	var ch struct{ Challenge string }
	if _, err := a.call(http.MethodPost, "/v1/relay/challenge", "", nil, &ch); err != nil {
		return heldToken{}, err
	}
	hash := sha256.Sum256([]byte(ch.Challenge + domainRelayToken))
	r, s, err := ecdsa.Sign(rand.Reader, a.rights.key, hash[:])
	if err != nil {
		return heldToken{}, err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	var out struct{ Token string }
	body := map[string]any{"challenge": ch.Challenge, "sig": sig, "thing": thing}
	if _, err := a.call(http.MethodPost, "/v1/relay/token", "", body, &out); err != nil {
		return heldToken{}, err
	}
	c, err := a.Authenticate(out.Token)
	if err != nil {
		return heldToken{}, err
	}
	return heldToken{token: out.Token, iat: time.Unix(c.Iat, 0), exp: time.Unix(c.Exp, 0)}, nil
}

// Table returns the signed table held for a Thing. A stale one is fetched
// again in the background, unless wait: then now. No table: everything None.
func (a *AuthClient) Table(thing string, wait bool) auth.Doc {
	a.rights.mu.Lock()
	h, ok := a.rights.tables[thing]
	if !ok {
		h = &heldTable{}
		a.rights.tables[thing] = h
	}
	stale := a.now().Sub(h.fetched) > tableTTL && !h.fetching
	if stale {
		h.fetching = true
	}
	doc := h.doc
	a.rights.mu.Unlock()
	if !stale {
		return doc
	}
	fetch := func() {
		d, err := a.fetchTable(thing, doc.Version)
		a.rights.mu.Lock()
		defer a.rights.mu.Unlock()
		h.fetching, h.fetched = false, a.now()
		if err == nil && d.Version > h.doc.Version {
			h.doc = d
		} else if err != nil && !errors.Is(err, errNoTable) {
			zlog.Warn().Err(err).Str("thing", thing).Msg("[rights] table not refreshed")
		}
	}
	if !wait {
		go fetch()
		return doc
	}
	fetch()
	a.rights.mu.Lock()
	defer a.rights.mu.Unlock()
	return h.doc
}

var errNoTable = errors.New("no table")

func (a *AuthClient) fetchTable(thing string, held int64) (auth.Doc, error) {
	var out struct{ Doc string }
	status, err := a.call(http.MethodGet, "/v1/tables/"+url.PathEscape(thing), "", nil, &out)
	if status == http.StatusNotFound {
		return auth.Doc{}, errNoTable
	}
	if err != nil {
		return auth.Doc{}, err
	}
	a.mu.RLock()
	keys := a.keys
	a.mu.RUnlock()
	return auth.ParseDoc(out.Doc, keys, a.aud, auth.DocTable, thing)
}

// Decide is the decision of a Thing, run by the relay for an app's token.
func (a *AuthClient) Decide(token, thing string, f authz.Function, wait bool) authz.Decision {
	table := a.Table(thing, wait)
	a.mu.RLock()
	keys, revoked := a.keys, a.revoked
	a.mu.RUnlock()
	return authz.Decide(authz.Check{
		Request:  authz.Request{Mode: authz.Normal, Verifier: authz.Relay, Token: token, Keys: keys, Aud: a.aud, Now: a.now()},
		Thing:    thing,
		Zones:    table.Zones,
		Table:    table.Table,
		Revoked:  &revoked,
		Function: f,
	})
}

// --- Rate limit (G40) ---

// bucket is a token bucket: perSecond, up to burst.
type bucket struct {
	mu        sync.Mutex
	tokens    float64
	last      time.Time
	perSecond float64
	burst     float64
}

func newBucket(perSecond, burst float64) *bucket {
	return &bucket{tokens: burst, last: time.Now(), perSecond: perSecond, burst: burst}
}

func (b *bucket) take() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.tokens = min(b.burst, b.tokens+now.Sub(b.last).Seconds()*b.perSecond)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
