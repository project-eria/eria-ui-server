package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/project-eria/go-wot/inventory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFollowInventory(t *testing.T) {
	gotToken := make(chan string, 1)
	thing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case gotToken <- r.Header.Get("Authorization"):
		default:
		}
		w.Write([]byte(`{"id": "urn:eria:salon:1", "title": "Salon"}`))
	}))
	defer thing.Close()
	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/inventory" || r.Header.Get("Authorization") != "Bearer svc" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		json.NewEncoder(w).Encode([]inventory.Entry{{ID: "eria:salon:1", URL: thing.URL + "/", Node: "rdc"}})
	}))
	defer master.Close()
	serviceToken = func() string { return "svc" }
	defer func() { serviceToken = func() string { return "" } }()

	r := NewRegistry(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Follow(ctx, master.URL+"/")
	require.Eventually(t, func() bool { return len(r.Things()) == 1 }, 2*time.Second, 20*time.Millisecond)
	assert.Equal(t, "rdc", r.Things()[0].Node)
	assert.Equal(t, "Bearer svc", <-gotToken, "a Thing of the inventory gets the service token")
	assert.Equal(t, "svc", tokenFor(thing.URL+"/properties/x"))
	assert.Empty(t, tokenFor("http://rogue:8080/"), "not in the inventory: no token")
}
