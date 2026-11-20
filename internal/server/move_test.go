package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-games/internal/config"
)

// moveLinks starts Games behind a fake Core whose pcx_<who>_<export|import|read>
// tokens are Pocket Move links acting for alice@ or bob@.
func moveLinks(t *testing.T) *app {
	t.Helper()
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		parts := strings.SplitN(strings.TrimPrefix(in.Token, "pcx_"), "_", 2)
		if len(parts) != 2 {
			io.WriteString(w, `{"active":false}`)
			return
		}
		scope := map[string]string{"export": core.ScopeMoveExport, "import": core.ScopeMoveImport, "read": "games:read"}[parts[1]]
		email := parts[0] + "@example.com"
		fmt.Fprintf(w, `{"active":true,"kind":"delegated","scope":%q,"audience":"games","user":{"id":1,"sub":%q,"email":%q,"name":%q,"role":"member"},"app":{"id":7,"slug":"move","name":"Pocket Move"}}`, scope, email, email, parts[0])
	}))
	t.Cleanup(coreSrv.Close)
	return newApp(t, func(c *config.Config) { c.CoreURL = coreSrv.URL })
}

func TestMoveLinkScopes(t *testing.T) {
	a := moveLinks(t)
	exp, imp, read := "pcx_bob_export", "pcx_bob_import", "pcx_bob_read"
	for _, c := range []struct {
		tok, method, path string
		want              int
	}{
		{exp, "GET", core.PortablePath, 200},
		{exp, "GET", "/api/v1/export.json", 200},
		{exp, "POST", "/api/v1/import", 403},
		{exp, "GET", "/api/v1/settings", 403},
		{exp, "GET", "/api/v1/saves", 403},
		{exp, "GET", "/api/v1/backup", 403},
		{imp, "GET", core.PortablePath, 200},
		{imp, "GET", "/api/v1/export.json", 403},
		{imp, "PUT", "/api/v1/settings", 403},
		{imp, "POST", "/api/v1/collections", 403},
		{imp, "POST", "/api/v1/import/romm", 403},
		{read, "GET", core.PortablePath, 403},
		{read, "GET", "/api/v1/export.json", 403},
	} {
		if code, body, _ := a.raw(c.tok, c.method, c.path, strings.NewReader("{}"), "application/json"); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%s)", c.tok, c.method, c.path, code, c.want, body)
		}
	}
	_, body, _ := a.raw(exp, "GET", core.PortablePath, nil, "")
	var p core.Portable
	if err := json.Unmarshal(body, &p); err != nil || len(p.Exports) != 1 || len(p.Imports) != 1 || p.Exports[0].Kind != core.KindPocket {
		t.Fatalf("portable: %s", body)
	}
}

// Move brings Alice's opinions, play time and collections to Bob: the import
// adds once, never takes away what Bob has, and keeps the two apart.
func TestMoveImportsAddOnce(t *testing.T) {
	a := moveLinks(t)
	fusion, crash := a.gameID("Metroid Fusion"), a.gameID("Crash Bandicoot")
	a.ok(a.alice, "PUT", fmt.Sprintf("/api/v1/games/%d/mine", fusion), map[string]any{"favorite": true, "status": "finished", "rating": 9, "notes": "Alice's notes"})
	a.ok(a.alice, "PUT", fmt.Sprintf("/api/v1/games/%d/mine", crash), map[string]any{"rating": 3, "notes": "too hard"})
	a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/games/%d/playtime", fusion), map[string]any{"minutes": 30})
	a.ok(a.alice, "POST", "/api/v1/collections", map[string]any{"name": "GBA", "game_ids": []int64{fusion}})
	// Bob already has his own word on Crash
	a.ok(a.bob, "PUT", fmt.Sprintf("/api/v1/games/%d/mine", crash), map[string]any{"rating": 8, "notes": "Bob loves it"})

	code, export, _ := a.raw("pcx_alice_export", "GET", "/api/v1/export.json", nil, "")
	if code != 200 || !strings.Contains(string(export), "Alice's notes") {
		t.Fatalf("alice's export: %d %s", code, export)
	}
	if _, bob, _ := a.raw("pcx_bob_export", "GET", "/api/v1/export.json", nil, ""); strings.Contains(string(bob), "Alice's notes") || !strings.Contains(string(bob), "Bob loves it") {
		t.Fatalf("bob's export: %s", bob)
	}
	imp := func() map[string]any {
		code, out, _ := a.raw("pcx_bob_import", "POST", "/api/v1/import", bytes.NewReader(export), "application/json")
		if code != 200 {
			t.Fatalf("import → %d %s", code, out)
		}
		var m map[string]any
		_ = json.Unmarshal(out, &m)
		return m
	}
	first := imp()
	if first["games"] != 1.0 || first["sessions"] != 1.0 || first["collections"] != 1.0 {
		t.Fatalf("first import: %v", first)
	}
	second := imp()
	if second["games"] != 0.0 || second["sessions"] != 0.0 || second["collections"] != 0.0 {
		t.Fatalf("second import added: %v", second)
	}
	mine := func(tok string, id int64) map[string]any {
		return a.ok(tok, "GET", fmt.Sprintf("/api/v1/games/%d", id), nil)["game"].(map[string]any)["mine"].(map[string]any)
	}
	if m := mine(a.bob, crash); m["notes"] != "Bob loves it" || m["rating"] != 8.0 {
		t.Fatalf("bob's own word on Crash changed: %v", m)
	}
	if m := mine(a.bob, fusion); m["notes"] != "Alice's notes" || m["favorite"] != true {
		t.Fatalf("bob's Fusion: %v", m)
	}
	if m := mine(a.alice, crash); m["notes"] != "too hard" {
		t.Fatalf("alice's Crash changed: %v", m)
	}
}
