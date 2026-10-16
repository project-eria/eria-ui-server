package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestPages(t *testing.T) {
	auth, err := OpenAuth(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	auth.Bootstrap("1234")
	admin := &Profile{ID: 1, Level: 0}
	mp, _ := auth.CreateProfile("Marlène", 5, "5678")
	member := &mp
	mustErr := func(what string, got, want error) {
		t.Helper()
		if !errors.Is(got, want) {
			t.Errorf("%s: got %v, want %v", what, got, want)
		}
	}
	light := Element{ID: "e1", Type: "service", Thing: "urn:x", Capability: "LightDimmer", Options: json.RawMessage(`{"base":{"size":[2,1]}}`)}

	// Validation: title, typed elements, unique ids, object options.
	for what, p := range map[string]Page{
		"no title":         {Title: " "},
		"unknown type":     {Title: "x", Elements: []Element{{ID: "a", Type: "title"}}},
		"service, no capa": {Title: "x", Elements: []Element{{ID: "a", Type: "service", Thing: "urn:x"}}},
		"duplicate ids":    {Title: "x", Elements: []Element{light, light}},
		"options array":    {Title: "x", Options: json.RawMessage(`[]`)},
	} {
		_, err := auth.SavePage(admin, p)
		mustErr(what, err, errPageInvalid)
	}

	// Pages are made by level 0 only, with their theme, aspect and layout.
	_, err = auth.SavePage(member, Page{Title: "Salon"})
	mustErr("member makes a page", err, errForbidden)
	salon, err := auth.SavePage(admin, Page{Title: "Salon", Aspect: "contraste-dark", Layout: "grid", Elements: []Element{light}})
	if err != nil || salon.Version != 1 || salon.Layout != "grid" || salon.Aspect != "contraste-dark" || string(salon.Options) != "{}" {
		t.Fatalf("page = %+v, %v", salon, err)
	}
	if list, _ := auth.Pages(); len(list) != 1 {
		t.Errorf("pages = %v", list)
	}

	// Versions and fixed look.
	salon.Title = "Salon !"
	if salon, err = auth.SavePage(admin, salon); err != nil || salon.Version != 2 {
		t.Fatalf("update = %+v, %v", salon, err)
	}
	stale := salon
	stale.Version = 1
	_, err = auth.SavePage(admin, stale)
	mustErr("stale save", err, errPageStale)
	other := salon
	other.Layout = "columns"
	_, err = auth.SavePage(admin, other)
	mustErr("layout changed after creation", err, errPageInvalid)

	// The manifest lists every page and the themes they use.
	auth.SavePage(admin, Page{Title: "Cuisine", Theme: "hex", Layout: "hex-main"})
	refs, themes, _ := auth.Manifest()
	if len(refs) != 2 || len(themes) != 1 || themes[0] != "hex" {
		t.Errorf("manifest = %v %v", refs, themes)
	}

	mustErr("member deletes a page", auth.DeletePage(member, salon.ID), errForbidden)
	if err := auth.DeletePage(admin, salon.ID); err != nil {
		t.Fatal(err)
	}
	mustErr("deleted page", func() error { _, err := auth.Page(salon.ID); return err }(), errPageNotFound)
}
