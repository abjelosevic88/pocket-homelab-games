package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-games/internal/config"
)

// capLinks starts Games behind a fake Core whose pcx_<who>_<scope> tokens are
// links acting for alice@ or bob@: search, day and widget are the pocket:*
// scopes, read and write the app's own, export and import Pocket Move's. The
// second value answers with what the app last registered.
func capLinks(t *testing.T) (*app, func() map[string]any) {
	t.Helper()
	var mu sync.Mutex
	registered := map[string]any{}
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/apps/register" {
			mu.Lock()
			registered = map[string]any{}
			_ = json.NewDecoder(r.Body).Decode(&registered)
			mu.Unlock()
			io.WriteString(w, `{}`)
			return
		}
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		parts := strings.SplitN(strings.TrimPrefix(in.Token, "pcx_"), "_", 2)
		scope, known := map[string]string{"search": core.ScopeSearch, "day": core.ScopeDay, "widget": core.ScopeWidget, "capture": core.ScopeCapture,
			"read": "games:read", "write": "games:write", "export": core.ScopeMoveExport, "import": core.ScopeMoveImport}[parts[len(parts)-1]]
		if len(parts) != 2 || !known {
			io.WriteString(w, `{"active":false}`)
			return
		}
		email := parts[0] + "@example.com"
		fmt.Fprintf(w, `{"active":true,"kind":"delegated","scope":%q,"audience":"games","user":{"id":1,"sub":%q,"email":%q,"name":%q,"role":"member"},"app":{"id":7,"slug":"hub","name":"Pocket Hub"}}`, scope, email, email, parts[0])
	}))
	t.Cleanup(coreSrv.Close)
	a := newApp(t, func(c *config.Config) { c.CoreURL = coreSrv.URL })
	return a, func() map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return registered
	}
}

// played records a sitting for a person (as a device or the player reports it).
func (a *app) played(who string, game int64, start string, minutes int, device string) {
	a.t.Helper()
	ts, err := time.Parse(time.RFC3339, start)
	if err != nil {
		a.t.Fatal(err)
	}
	if err := a.st.AddPlayTime(context.Background(), a.ids[who], game, ts, int64(minutes)*60, device); err != nil {
		a.t.Fatal(err)
	}
}

// localNoon is noon of a day on the server's clock, so a test does not
// depend on the machine's time zone.
func localNoon(day string) string {
	t, _ := time.ParseInLocation("2006-01-02 15:04", day+" 12:00", time.Local)
	return t.Format(time.RFC3339)
}

func (a *app) search(tok, q string) []core.SearchItem {
	a.t.Helper()
	var res core.SearchResults
	code, raw, _ := a.raw(tok, "GET", "/api/v1/pocket/search?"+q, nil, "")
	if err := json.Unmarshal(raw, &res); code != 200 || err != nil || res.Items == nil {
		a.t.Fatalf("search %s = %d %s", q, code, raw)
	}
	return res.Items
}

func names(items []core.SearchItem) string {
	var t []string
	for _, it := range items {
		t = append(t, it.Title)
	}
	return strings.Join(t, " | ")
}

// Pocket Search finds the games of the library by name and by system, and
// says what the person keeps about each: their own status, stars and play
// time, never someone else's.
func TestPocketSearch(t *testing.T) {
	a := newApp(t)
	fusion, crash, sh := a.gameID("Metroid Fusion"), a.gameID("Crash Bandicoot"), a.gameID("Silent Hill")
	a.ok(a.alice, "PUT", fmt.Sprintf("/api/v1/games/%d/mine", fusion), map[string]any{"favorite": true, "status": "playing", "rating": 9})
	a.ok(a.alice, "PUT", fmt.Sprintf("/api/v1/games/%d/mine", crash), map[string]any{"status": "backlog", "rating": 2})
	a.played("Alice", fusion, localNoon("2025-03-10"), 135, "Retroid")
	lastPlayed, _ := time.Parse(time.RFC3339, localNoon("2025-03-10"))

	got := a.search(a.alice, "q=metroid")
	if len(got) != 1 {
		t.Fatalf("metroid = %s", names(got))
	}
	if it := got[0]; it.ID != fmt.Sprint(fusion) || it.Kind != "game" || it.URL != fmt.Sprintf("/games/%d", fusion) || it.Date != lastPlayed.Add(135*time.Minute).UTC().Format(time.RFC3339) ||
		it.Snippet != "Game Boy Advance · playing, 4.5 stars, favourite · 2 h 15 min played, last played 10 Mar 2025" {
		t.Errorf("Alice's Fusion = %+v", it)
	}
	if got := a.search(a.alice, "q=crash"); len(got) != 1 || got[0].Snippet != "PlayStation · in your backlog, 1 star · not played yet" {
		t.Errorf("Alice's Crash = %+v", got)
	}
	// a game finished before it came here does not say "not played yet"
	a.ok(a.alice, "PUT", fmt.Sprintf("/api/v1/games/%d/mine", sh), map[string]any{"status": "finished"})
	if got := a.search(a.alice, "q=silent"); len(got) != 1 || got[0].Snippet != "PlayStation · finished" {
		t.Errorf("Alice's Silent Hill = %+v", got)
	}
	// Bob sees the same game, with nothing of Alice's
	if got := a.search(a.bob, "q=metroid"); len(got) != 1 || got[0].Snippet != "Game Boy Advance · not played yet" || got[0].Date == "" || strings.HasPrefix(got[0].Date, "2025") {
		t.Errorf("Bob's Fusion = %+v", got)
	}
	// a name that is the words comes first
	if got := a.search(a.alice, "q=silent+hill"); len(got) != 1 || got[0].ID != fmt.Sprint(sh) {
		t.Errorf("silent hill = %+v", got)
	}
	// by system: its folder, its short name, its name
	for _, q := range []string{"q=psx", "q=PS1", "q=playstation", "q=station"} {
		if got := names(a.search(a.alice, q)); got != "Crash Bandicoot | Silent Hill" {
			t.Errorf("%s = %s", q, got)
		}
	}
	if got := a.search(a.alice, "q=game+boy"); len(got) != 1 || got[0].Title != "Metroid Fusion" {
		t.Errorf("game boy = %s", names(got))
	}
	// a name and a system together
	if got := a.search(a.alice, "q=crash+psx"); names(got) != "Crash Bandicoot" {
		t.Errorf("crash psx = %s", names(got))
	}
	if got := a.search(a.alice, "q=metroid+psx"); len(got) != 0 {
		t.Errorf("metroid psx = %s", names(got))
	}
	// the limit, and words with no letter or digit in them
	if got := a.search(a.alice, "q=psx&limit=1"); len(got) != 1 {
		t.Errorf("limit 1 = %s", names(got))
	}
	for _, q := range []string{"q=%3F%3F", "q=%25", "q=*", "q=_", "q=+"} {
		if got := a.search(a.alice, q); len(got) != 0 {
			t.Errorf("%s = %s", q, names(got))
		}
	}
	// a game Bob hid is gone from his answers only
	a.ok(a.bob, "PUT", fmt.Sprintf("/api/v1/games/%d/mine", crash), map[string]any{"hidden": true})
	if got := names(a.search(a.bob, "q=psx")); got != "Silent Hill" {
		t.Errorf("Bob's psx = %s", got)
	}
	if got := names(a.search(a.alice, "q=psx")); got != "Crash Bandicoot | Silent Hill" {
		t.Errorf("Alice's psx = %s", got)
	}
	// GET /games?q= is as it was: a system's name is not looked for there
	if m := a.ok(a.alice, "GET", "/api/v1/games?q=psx", nil); m["total"].(float64) != 0 {
		t.Errorf("/games?q=psx = %v", m["total"])
	}
}

func (a *app) dayOf(tok, date, tz string) core.Day {
	a.t.Helper()
	var d core.Day
	path := "/api/v1/pocket/day/" + date
	if tz != "" {
		path += "?tz=" + tz
	}
	code, raw, _ := a.raw(tok, "GET", path, nil, "")
	if err := json.Unmarshal(raw, &d); code != 200 || err != nil || d.Date != date || d.Items == nil {
		a.t.Fatalf("day %s = %d %s", date, code, raw)
	}
	return d
}

func dayItem(d core.Day, title string) core.DayItem {
	for _, it := range d.Items {
		if it.Title == title {
			return it
		}
	}
	return core.DayItem{}
}

// Pocket Day gets each game played that day once, with its minutes and when
// it was played, in the caller's time zone.
func TestPocketDay(t *testing.T) {
	a := newApp(t)
	fusion, crash, sh := a.gameID("Metroid Fusion"), a.gameID("Crash Bandicoot"), a.gameID("Silent Hill")
	const day = "2026-10-05"
	a.played("Alice", fusion, day+"T09:00:00Z", 45, "Retroid")
	a.played("Alice", fusion, day+"T18:00:00Z", 15, "browser")
	a.played("Alice", crash, localNoon(day), 30, "elsewhere") // added by hand: no clock, the day it was given
	a.played("Alice", sh, day+"T23:30:00Z", 40, "browser")    // past midnight in Sarajevo

	d := a.dayOf(a.alice, day, "UTC")
	if d.Summary != "Played 3 games for 2 h 10 min" || len(d.Items) != 3 || len(d.Metrics) != 1 || d.Metrics[0] != (core.Metric{Label: "Time", Value: "2 h 10 min"}) {
		t.Fatalf("day = %q %+v %+v", d.Summary, d.Items, d.Metrics)
	}
	if it := dayItem(d, "Metroid Fusion"); it.ID != fmt.Sprint(fusion) || it.Sub != "Game Boy Advance" || it.Kind != "played" || it.Minutes != 60 ||
		it.Time != "09:00" || it.End != "18:15" || it.URL != fmt.Sprintf("/games/%d", fusion) {
		t.Errorf("Fusion = %+v", it)
	}
	if it := dayItem(d, "Crash Bandicoot"); it.Minutes != 30 || it.Time != "" || it.End != "" || it.Sub != "PlayStation" {
		t.Errorf("Crash = %+v", it)
	}
	if it := dayItem(d, "Silent Hill"); it.Minutes != 40 || it.Time != "23:30" || it.End != "" {
		t.Errorf("Silent Hill = %+v", it)
	}
	// Sarajevo is two hours ahead in October: Silent Hill was the next morning
	d = a.dayOf(a.alice, day, "Europe/Sarajevo")
	if d.Summary != "Played Metroid Fusion and Crash Bandicoot for 1 h 30 min" || len(d.Items) != 2 {
		t.Errorf("Sarajevo day = %q %+v", d.Summary, d.Items)
	}
	if it := dayItem(d, "Metroid Fusion"); it.Time != "11:00" || it.End != "20:15" {
		t.Errorf("Sarajevo Fusion = %+v", it)
	}
	d = a.dayOf(a.alice, "2026-10-06", "Europe/Sarajevo")
	if it := dayItem(d, "Silent Hill"); d.Summary != "Played Silent Hill for 40 min" || len(d.Items) != 1 || it.Time != "01:30" || it.End != "02:10" {
		t.Errorf("next day = %q %+v", d.Summary, d.Items)
	}
	// without a zone the server's own day is used
	if d := a.dayOf(a.alice, day, ""); dayItem(d, "Crash Bandicoot").Minutes != 30 {
		t.Errorf("server's day = %+v", d)
	}
	// Bob played nothing; a day with nothing says nothing
	if d := a.dayOf(a.bob, day, "UTC"); d.Summary != "" || len(d.Items) != 0 || len(d.Metrics) != 0 {
		t.Errorf("Bob's day = %+v", d)
	}
	if code, _, _ := a.call(a.alice, "GET", "/api/v1/pocket/day/2026-13-01", nil); code != 400 {
		t.Errorf("bad date = %d", code)
	}
	// GET /day/{date} is as it was
	if m := a.ok(a.alice, "GET", "/api/v1/day/"+day, nil); m["seconds"] == nil || m["games"] == nil {
		t.Errorf("/day = %v", m)
	}
}

// Pocket Hub's widget is the person's own "Continue playing": the games they
// played last, the latest first, and this week's play time.
func TestPocketWidget(t *testing.T) {
	a := newApp(t)
	fusion, crash, sh := a.gameID("Metroid Fusion"), a.gameID("Crash Bandicoot"), a.gameID("Silent Hill")
	widget := func(tok string) core.WidgetData {
		t.Helper()
		var w core.WidgetData
		code, raw, _ := a.raw(tok, "GET", "/api/v1/pocket/widget", nil, "")
		if err := json.Unmarshal(raw, &w); code != 200 || err != nil || w.Items == nil {
			t.Fatalf("widget = %d %s", code, raw)
		}
		return w
	}
	if w := widget(a.alice); len(w.Items) != 0 || w.Line != "" || len(w.Metrics) != 0 {
		t.Fatalf("nothing played yet = %+v", w)
	}
	a.played("Alice", fusion, localNoon("2025-03-10"), 135, "Retroid")
	a.played("Alice", sh, time.Now().Add(-3*time.Hour).UTC().Format(time.RFC3339), 60, "browser")
	a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/games/%d/sessions", crash), map[string]any{"device": "browser"})

	w := widget(a.alice)
	if len(w.Items) != 3 || w.Line != "" || w.URL != "/" {
		t.Fatalf("widget = %+v", w)
	}
	if it := w.Items[0]; it.ID != fmt.Sprint(crash) || it.Title != "Crash Bandicoot" || it.Detail != "PlayStation · played today" || it.URL != fmt.Sprintf("/games/%d", crash) {
		t.Errorf("first = %+v", it)
	}
	if w.Items[1].Title != "Silent Hill" {
		t.Errorf("second = %+v", w.Items[1])
	}
	if it := w.Items[2]; it.Title != "Metroid Fusion" || it.Detail != "Game Boy Advance · played 10 Mar 2025" {
		t.Errorf("third = %+v", it)
	}
	if len(w.Metrics) != 1 || w.Metrics[0] != (core.Metric{Label: "This week", Value: "1 h"}) {
		t.Errorf("metrics = %+v", w.Metrics)
	}
	// Bob's widget has none of Alice's games
	if w := widget(a.bob); len(w.Items) != 0 {
		t.Errorf("Bob's widget = %+v", w)
	}
}

// A link token with a pocket:* scope reaches its one route (and /auth/me), as
// the person it acts for; the app's own scopes do not open them.
func TestPocketLinkScopes(t *testing.T) {
	a, _ := capLinks(t)
	fusion := a.gameID("Metroid Fusion")
	a.played("Bob", fusion, "2026-10-05T12:00:00Z", 30, "Retroid")
	a.played("Alice", a.gameID("Silent Hill"), "2026-10-05T12:00:00Z", 30, "Retroid")
	routes := map[string]string{"search": "/api/v1/pocket/search?q=metroid", "day": "/api/v1/pocket/day/2026-10-05?tz=UTC", "widget": "/api/v1/pocket/widget"}
	for scope := range routes {
		tok := "pcx_bob_" + scope
		for name, path := range routes {
			code, body, _ := a.raw(tok, "GET", path, nil, "")
			switch {
			case name == scope && (code != 200 || !strings.Contains(string(body), "Metroid Fusion") || strings.Contains(string(body), "Silent Hill")):
				t.Errorf("pocket:%s GET %s = %d %s", scope, path, code, body)
			case name != scope && (code != 403 || !strings.Contains(string(body), "link does not cover")):
				t.Errorf("pocket:%s on GET %s = %d %s, want 403", scope, path, code, body)
			}
		}
		if code, me, _ := a.call(tok, "GET", "/api/v1/auth/me", nil); code != 200 || !strings.Contains(fmt.Sprint(me), "bob@example.com") {
			t.Errorf("%s auth/me = %d %v", tok, code, me)
		}
		// nothing else in the app
		for _, c := range [][2]string{
			{"GET", "/api/v1/games"}, {"GET", fmt.Sprintf("/api/v1/games/%d", fusion)}, {"GET", "/api/v1/home"}, {"GET", "/api/v1/dashboard"},
			{"GET", "/api/v1/day/2026-10-05"}, {"GET", "/api/v1/stats"}, {"GET", "/api/v1/sessions"}, {"GET", "/api/v1/export.json"},
			{"PUT", fmt.Sprintf("/api/v1/games/%d/mine", fusion)}, {"DELETE", fmt.Sprintf("/api/v1/games/%d", fusion)},
		} {
			if code, _, _ := a.raw(tok, c[0], c[1], strings.NewReader("{}"), "application/json"); code != 403 {
				t.Errorf("%s %s %s = %d, want 403", tok, c[0], c[1], code)
			}
		}
		// and its own route in no other way, and no capture (the router has no such route to refuse)
		for _, c := range [][2]string{{"POST", "/api/v1/pocket/capture"}, {"POST", routes[scope]}, {"DELETE", "/api/v1/pocket/day/2026-10-05"}, {"GET", "/api/v1/pocket/day/2026-10-05/more"}} {
			if code, _, _ := a.raw(tok, c[0], c[1], strings.NewReader("{}"), "application/json"); code != 403 && code != 404 && code != 405 {
				t.Errorf("%s %s %s = %d", tok, c[0], c[1], code)
			}
		}
	}
	// the links the app had before do not open the new routes
	for _, tok := range []string{"pcx_bob_read", "pcx_bob_write", "pcx_bob_export", "pcx_bob_import", "pcx_bob_capture"} {
		for _, path := range routes {
			if code, _, _ := a.raw(tok, "GET", path, nil, ""); code != 403 {
				t.Errorf("%s GET %s = %d, want 403", tok, path, code)
			}
		}
	}
	// games:read still reads what it read
	if code, _, _ := a.raw("pcx_bob_read", "GET", "/api/v1/day/2026-10-05", nil, ""); code != 200 {
		t.Errorf("games:read /day = %d", code)
	}
	// a token Core does not vouch for
	if code, _, _ := a.raw("pcx_revoked", "GET", "/api/v1/pocket/widget", nil, ""); code != 401 {
		t.Errorf("unknown link token = %d, want 401", code)
	}
}

// Without Pocket Core the routes answer the person signed in to the app, and
// a link token means nothing.
func TestPocketRoutesWithoutCore(t *testing.T) {
	a := newApp(t)
	if got := a.search(a.bob, "q=crash"); len(got) != 1 {
		t.Errorf("search = %+v", got)
	}
	for _, path := range []string{"/api/v1/pocket/search?q=crash", "/api/v1/pocket/widget", "/api/v1/pocket/day/2026-10-05"} {
		if code, _, _ := a.raw("pcx_bob_search", "GET", path, nil, ""); code != 401 {
			t.Errorf("link token without Core on %s = %d, want 401", path, code)
		}
		if code, _, _ := a.raw("", "GET", path, nil, ""); code != 401 {
			t.Errorf("nobody on %s = %d, want 401", path, code)
		}
	}
}

// The manifest tells Pocket Core the shelf Hub puts the app on and what
// Search, Day and Hub can ask it; nothing can be captured.
func TestManifestCapabilities(t *testing.T) {
	a, manifest := capLinks(t)
	a.s.cfg.CoreRegistrationKey = "pcr_test"
	if err := a.s.registerWithCore(context.Background()); err != nil {
		t.Fatal(err)
	}
	m := manifest()
	if m["slug"] != "games" || m["category"] != "Media" {
		t.Fatalf("manifest: %v %v", m["slug"], m["category"])
	}
	search, _ := m["search"].(map[string]any)
	day, _ := m["day"].(map[string]any)
	widget, _ := m["widget"].(map[string]any)
	if search == nil || day == nil || widget == nil || m["capture"] != nil {
		t.Fatalf("capabilities: %v %v %v %v", search, day, widget, m["capture"])
	}
	if fmt.Sprint(search["bangs"]) != "[games game]" || fmt.Sprint(search["kinds"]) != "[game]" || search["what"] == "" {
		t.Errorf("search: %v", search)
	}
	if day["label"] != "Games" || day["one"] != "game" || day["many"] != "games" || day["unit"] != "min" || day["what"] == "" {
		t.Errorf("day: %v", day)
	}
	if widget["title"] != "Games" || widget["what"] == "" {
		t.Errorf("widget: %v", widget)
	}
	offered := map[string]bool{}
	for _, o := range m["offers"].([]any) {
		offered[o.(map[string]any)["scope"].(string)] = true
	}
	for _, sc := range []string{"games:read", "games:write", core.ScopeSearch, core.ScopeDay, core.ScopeWidget, core.ScopeMoveExport, core.ScopeMoveImport} {
		if !offered[sc] {
			t.Errorf("%s is not offered", sc)
		}
	}
	if offered[core.ScopeCapture] {
		t.Error("pocket:capture is offered")
	}
}
