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
	m1p, _ := auth.CreateProfile("Marlène", RoleMember, "5678")
	m2p, _ := auth.CreateProfile("Lia", RoleMember, "2468")
	gp, _ := auth.CreateProfile("Invité", RoleGuest, "0000")
	m1, m2, guest := &m1p, &m2p, &gp
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

	// Shared pages: admin only; personal pages: their owner only.
	_, err = auth.SavePage(m1, Page{Title: "Salon", Shared: true})
	mustErr("member makes a shared page", err, errForbidden)
	_, err = auth.SavePage(guest, Page{Title: "Mine"})
	mustErr("guest makes a page", err, errForbidden)
	salon, err := auth.SavePage(admin, Page{Title: "Salon", Shared: true, Elements: []Element{light}})
	if err != nil || salon.Version != 1 || !salon.Shared || string(salon.Options) != "{}" {
		t.Fatalf("shared page = %+v, %v", salon, err)
	}
	mine, _ := auth.SavePage(m1, Page{Title: "Ma chambre"})
	if _, err := auth.Page(m2, mine.ID); !errors.Is(err, errPageNotFound) {
		t.Error("another member sees a personal page")
	}
	if _, err := auth.Page(nil, salon.ID); err != nil {
		t.Error("a shared page is hidden from a terminal nobody is signed in on")
	}
	salon.Title = "Salon !"
	_, err = auth.SavePage(m1, salon)
	mustErr("member edits a shared page", err, errForbidden)
	if list, _ := auth.Pages(m1); len(list) != 1 || list[0].ID != mine.ID {
		t.Errorf("member's pages = %v, want only their own", list)
	}

	// Versions: a stale save is refused.
	if salon, err = auth.SavePage(admin, salon); err != nil || salon.Version != 2 {
		t.Fatalf("update = %+v, %v", salon, err)
	}
	salon.Version = 1
	_, err = auth.SavePage(admin, salon)
	mustErr("stale save", err, errPageStale)

	// Terminals show shared pages only; the manifest adds the profile's.
	term, _ := auth.Identify("terminal-secret-000000000000")
	ids := []string{mine.ID}
	_, err = auth.ConfigureTerminal(TerminalChange{ID: term.ID, Pages: &ids})
	mustErr("personal page on a terminal", err, errInvalid)
	ids = []string{salon.ID}
	auth.ConfigureTerminal(TerminalChange{ID: term.ID, Pages: &ids})
	refs, _ := auth.Manifest(term.ID, m1)
	if len(refs) != 2 || refs[0] != (PageRef{salon.ID, 2}) || refs[1].ID != mine.ID {
		t.Errorf("manifest = %v, want the terminal's page then the profile's", refs)
	}
	if refs, _ := auth.Manifest(term.ID, nil); len(refs) != 1 {
		t.Errorf("manifest without profile = %v, want the terminal's page only", refs)
	}

	// Deleting a page removes it from the terminals.
	mustErr("member deletes a shared page", auth.DeletePage(m1, salon.ID), errForbidden)
	if err := auth.DeletePage(admin, salon.ID); err != nil {
		t.Fatal(err)
	}
	if term, _ = auth.Identify("terminal-secret-000000000000"); len(term.Pages) != 0 {
		t.Errorf("terminal pages after delete = %v", term.Pages)
	}
	if err := auth.DeletePage(m1, mine.ID); err != nil {
		t.Errorf("owner deletes their page: %v", err)
	}
}
