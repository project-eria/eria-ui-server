package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"
	"time"
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

	at(3*time.Minute + 30*time.Second)
	if c.ServiceToken("lampe") == first || issued() != 2 {
		t.Fatalf("not renewed after 2/3 of its life: issued %d", issued())
	}
}
