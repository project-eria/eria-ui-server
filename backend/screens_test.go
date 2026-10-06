package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestScreens(t *testing.T) {
	auth, err := OpenAuth(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	auth.Bootstrap("1234")
	admin := Profile{ID: 1, Role: RoleAdmin}
	m1, _ := auth.CreateProfile("Marlène", RoleMember, "5678")
	m2, _ := auth.CreateProfile("Lia", RoleMember, "2468")
	guest, _ := auth.CreateProfile("Invité", RoleGuest, "0000")
	mustErr := func(what string, got, want error) {
		t.Helper()
		if !errors.Is(got, want) {
			t.Errorf("%s: got %v, want %v", what, got, want)
		}
	}
	layout := json.RawMessage(`[{"id":"a","kind":"title","text":"Salon"},{"id":"b","kind":"service","service":"urn:x#LightBasic","w":2,"h":1}]`)

	// Personal screen: invisible and untouchable by others.
	mine, _ := auth.CreateScreen(m1, "Ma chambre", false)
	if list, _ := auth.Screens(m2); len(list) != 0 {
		t.Errorf("another member sees %d screens, want 0", len(list))
	}
	mustErr("other member edits", auth.UpdateScreen(m2, mine, Screen{Title: "x", Version: 1, Layout: layout}), errNotFound)
	mustErr("other member deletes", auth.DeleteScreen(m2, mine), errNotFound)

	// Shared screen: only admins create it; editRole decides who edits.
	_, err = auth.CreateScreen(m1, "Salon", true)
	mustErr("member creates shared", err, errForbidden)
	shared, _ := auth.CreateScreen(admin, "Salon", true)
	mustErr("member edits admin-only screen", auth.UpdateScreen(m1, shared, Screen{Title: "Salon", Version: 1, Layout: layout}), errForbidden)
	mustErr("admin opens it to members", auth.UpdateScreen(admin, shared, Screen{Title: "Salon", Version: 1, EditRole: RoleMember, Layout: layout}), nil)
	mustErr("member edits now", auth.UpdateScreen(m1, shared, Screen{Title: "Salon", Version: 2, Layout: layout}), nil)
	mustErr("member can't change editRole", auth.UpdateScreen(m1, shared, Screen{Title: "Salon", Version: 3, EditRole: RoleAdmin, Layout: layout}), errForbidden)
	mustErr("guest never edits shared", auth.UpdateScreen(guest, shared, Screen{Title: "Salon", Version: 3, Layout: layout}), errForbidden)
	mustErr("member deletes shared", auth.DeleteScreen(m1, shared), errForbidden)

	// Concurrent edits: the second save from version 2 is refused.
	mustErr("stale draft", auth.UpdateScreen(m2, shared, Screen{Title: "Salon", Version: 2, Layout: layout}), errStale)

	// Theme and scheme of a screen are checked.
	mustErr("unknown theme", auth.UpdateScreen(m1, mine, Screen{Title: "x", Version: 1, Theme: "neon", Layout: layout}), errInvalid)
	mustErr("unknown scheme", auth.UpdateScreen(m1, mine, Screen{Title: "x", Version: 1, Scheme: "sepia", Layout: layout}), errInvalid)

	// Layout shape is checked.
	for _, bad := range []string{`{}`, `[{"id":"a","kind":"video"}]`, `[{"id":"a","kind":"service"}]`, `[{"kind":"title","text":"x"}]`, `[{"id":"a","kind":"service","service":"s","w":9}]`} {
		mustErr("bad layout "+bad, auth.UpdateScreen(m1, mine, Screen{Title: "x", Version: 1, Layout: json.RawMessage(bad)}), errInvalid)
	}

	// What m1 sees: shared first, then personal, with canEdit.
	list, _ := auth.Screens(m1)
	if len(list) != 2 || !list[0].Shared || list[1].Shared || !list[0].CanEdit || list[0].Version != 3 {
		t.Errorf("m1 screens = %+v", list)
	}
	if list, _ := auth.Screens(guest); len(list) != 1 || list[0].CanEdit {
		t.Errorf("guest screens = %+v", list)
	}

	// Deleting a profile removes its personal screens.
	auth.DeleteProfile(m1.ID)
	var n int
	auth.db.QueryRow(`SELECT count(*) FROM screens WHERE id = ?`, mine).Scan(&n)
	if n != 0 {
		t.Error("personal screen kept after its owner was deleted")
	}
}
