package main

import (
	"errors"
	"path/filepath"
	"testing"
)

// Start pages: one per terminal, the house's as fallback (2026-10-26).
func TestHomePages(t *testing.T) {
	auth, err := OpenAuth(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	admin := &Profile{ID: 1, Level: 0}
	salon, _ := auth.SavePage(admin, Page{Title: "Salon"})

	if auth.HomePage() != "" {
		t.Error("a new house has a start page")
	}
	if err := auth.SetHomePage("nope"); !errors.Is(err, errInvalid) {
		t.Errorf("unknown start page accepted: %v", err)
	}
	if err := auth.SetHomePage(salon.ID); err != nil || auth.HomePage() != salon.ID {
		t.Errorf("house start page = %q, %v", auth.HomePage(), err)
	}

	term, err := auth.Identify("abcdefghijklmnopqrstuvwxyz")
	if err != nil || term.HomePage != "" {
		t.Fatalf("new terminal = %+v, %v", term, err)
	}
	bad := "nope"
	if _, err := auth.ConfigureTerminal(TerminalChange{ID: term.ID, HomePage: &bad}); !errors.Is(err, errInvalid) {
		t.Errorf("unknown terminal start page accepted: %v", err)
	}
	if term, err = auth.ConfigureTerminal(TerminalChange{ID: term.ID, HomePage: &salon.ID}); err != nil || term.HomePage != salon.ID {
		t.Errorf("terminal = %+v, %v", term, err)
	}
	none := ""
	if term, _ = auth.ConfigureTerminal(TerminalChange{ID: term.ID, HomePage: &none}); term.HomePage != "" {
		t.Error("terminal start page not cleared")
	}
}
