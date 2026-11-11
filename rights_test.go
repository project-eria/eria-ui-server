package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/project-eria/go-wot/guard"
)

// The service token is renewed at 2/3 of its 5 min life, and a fresh token
// never waits for a renewal in progress.
func TestServiceTokenRenewal(t *testing.T) {
	fa := newFakeAuth(t)
	c := fa.client
	c.rights.key, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	start := time.Now()
	at := func(d time.Duration) { c.now = func() time.Time { return start.Add(d) } }
	issued := func() int { fa.mu.Lock(); defer fa.mu.Unlock(); return fa.issued }

	at(0)
	first := c.ServiceToken("lampe")
	if first == "" || issued() != 1 {
		t.Fatalf("token %q, issued %d", first, issued())
	}
	at(3 * time.Minute)
	if c.ServiceToken("lampe") != first || issued() != 1 {
		t.Fatalf("renewed before 2/3 of its life: issued %d", issued())
	}

	c.rights.refresh.Lock() // a renewal in progress
	done := make(chan string)
	go func() { done <- c.ServiceToken("lampe") }()
	select {
	case tok := <-done:
		if tok != first {
			t.Fatal("fresh token changed")
		}
	case <-time.After(time.Second):
		t.Fatal("fresh token waited for the renewal")
	}
	c.rights.refresh.Unlock()

	at(3*time.Minute + 30*time.Second) // past 2/3, still valid: served during a renewal
	c.rights.refresh.Lock()
	go func() { done <- c.ServiceToken("lampe") }()
	select {
	case tok := <-done:
		if tok != first || issued() != 1 {
			t.Fatal("a valid token waited or changed during a renewal")
		}
	case <-time.After(time.Second):
		t.Fatal("a valid token waited for the renewal")
	}
	c.rights.refresh.Unlock()

	if c.ServiceToken("lampe") == first || issued() != 2 {
		t.Fatalf("not renewed after 2/3 of its life: issued %d", issued())
	}
}

// The keyset the agent writes beside the config's file wins over it.
func TestReadKeysetPrefersTheAgents(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "auth-keyset.json")
	os.WriteFile(file, []byte("by hand"), 0o600)
	if got, _ := readKeyset(file); string(got) != "by hand" {
		t.Fatalf("got %q", got)
	}
	os.WriteFile(filepath.Join(dir, guard.SettingsFile), []byte(`{"keyset":"from the agent"}`), 0o600)
	if got, _ := readKeyset(file); string(got) != "from the agent" {
		t.Fatalf("got %q", got)
	}
}
