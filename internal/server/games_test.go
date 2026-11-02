package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-games/internal/library"
	"github.com/abjelosevic88/pocket-homelab-games/internal/store"
)

type app struct {
	t     *testing.T
	url   string
	s     *Server
	st    *store.Store
	lib   string
	alice string // admin, owner of the box
	bob   string // member
	ids   map[string]int64
}

func newApp(t *testing.T) *app {
	t.Helper()
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.BaseURL = "http://games.test"
	// a small library: a cartridge, a disc folder with its .sbi, a cue/bin set, firmware
	put := func(rel string, n int) {
		p := filepath.Join(cfg.LibraryDir, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, bytes.Repeat([]byte(rel[:1]), n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("roms/gba/Metroid Fusion (USA).gba", 4096)
	put("roms/psx/Silent Hill (Europe) (En,Fr,De,Es,It)/Silent Hill (Europe) (En,Fr,De,Es,It).chd", 8192)
	put("roms/psx/Silent Hill (Europe) (En,Fr,De,Es,It)/Silent Hill (Europe) (En,Fr,De,Es,It).sbi", 64)
	put("roms/psx/Crash Bandicoot (USA).cue", 120)
	put("roms/psx/Crash Bandicoot (USA) (Track 1).bin", 9000)
	put("roms/psx/Crash Bandicoot (USA) (Track 2).bin", 3000)
	put("bios/psx/scph5501.bin", 512)
	st := openTestStore(t, cfg)
	ctx := context.Background()
	a := &app{t: t, st: st, lib: cfg.LibraryDir, ids: map[string]int64{}}
	for _, u := range []*store.User{{Email: "alice@example.com", Name: "Alice", Role: "admin"}, {Email: "bob@example.com", Name: "Bob", Role: "member"}} {
		if u.Name == "Bob" {
			u.PasswordHash = "$2a$10$Q4DJ0wQ1ZpZ8y2t7c7xD9e3H2u0sJ0bN0nJ0f2m8x5x2F0x0n1n6S" // not used: Bob signs in with a token
		}
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		tok, _, _ := st.CreateAPIToken(ctx, u.ID, "t")
		a.ids[u.Name] = u.ID
		if u.Name == "Alice" {
			a.alice = tok
		} else {
			a.bob = tok
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	a.s = New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err := a.s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.s.scanAll(ctx, nil); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(a.s.Router())
	t.Cleanup(ts.Close)
	a.url = ts.URL
	return a
}

func (a *app) raw(auth, method, path string, body io.Reader, ct string) (int, []byte, http.Header) {
	a.t.Helper()
	req, _ := http.NewRequest(method, a.url+path, body)
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if strings.HasPrefix(auth, "Basic ") || strings.HasPrefix(auth, "Bearer ") {
		req.Header.Set("Authorization", auth)
	} else if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b, res.Header
}

func (a *app) call(auth, method, path string, body any) (int, map[string]any, []any) {
	a.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	code, raw, _ := a.raw(auth, method, path, rd, "application/json")
	var m map[string]any
	var l []any
	if json.Unmarshal(raw, &m) != nil {
		_ = json.Unmarshal(raw, &l)
	}
	return code, m, l
}

func (a *app) ok(auth, method, path string, body any) map[string]any {
	a.t.Helper()
	code, m, _ := a.call(auth, method, path, body)
	if code >= 300 {
		a.t.Fatalf("%s %s → %d %v", method, path, code, m)
	}
	return m
}

// gameID finds a game by its clean name.
func (a *app) gameID(name string) int64 {
	a.t.Helper()
	m := a.ok(a.alice, "GET", "/api/v1/games?limit=100", nil)
	for _, it := range m["items"].([]any) {
		g := it.(map[string]any)
		if g["name"] == name {
			return int64(g["id"].(float64))
		}
	}
	a.t.Fatalf("no game %q", name)
	return 0
}

func TestScanFindsGames(t *testing.T) {
	a := newApp(t)
	m := a.ok(a.alice, "GET", "/api/v1/games?limit=100", nil)
	if m["total"].(float64) != 3 {
		t.Fatalf("total = %v, want 3 (a cartridge, a disc folder, a cue/bin set)", m["total"])
	}
	crash := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/games/%d", a.gameID("Crash Bandicoot")), nil)
	g := crash["game"].(map[string]any)
	if g["main_file"] != "Crash Bandicoot (USA).cue" || g["file_count"].(float64) != 3 {
		t.Fatalf("cue/bin set = %v / %v files", g["main_file"], g["file_count"])
	}
	sh := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/games/%d", a.gameID("Silent Hill")), nil)
	if g := sh["game"].(map[string]any); g["regions"] != "Europe" || g["languages"] != "En, Fr, De, Es, It" || !g["is_folder"].(bool) {
		t.Fatalf("parsed = %v", g)
	}
	bios := sh["bios"].(map[string]any)
	if bios["ok"] != true {
		t.Fatalf("psx bios = %v", bios)
	}
	if sh["play"].(map[string]any)["browser"] != true {
		t.Fatal("psx should play in the browser")
	}
}

func TestNamesParse(t *testing.T) {
	cases := map[string][3]string{
		"Legend of Zelda, The - A Link to the Past (USA).sfc":              {"The Legend of Zelda: A Link to the Past", "USA", ""},
		"Silent Hill 2 (Europe) (En,Ja,Fr,De,Es,It)":                       {"Silent Hill 2", "Europe", "En,Ja,Fr,De,Es,It"},
		"Donkey Kong Country (USA) (Rev 2).sfc":                            {"Donkey Kong Country", "USA", ""},
		"Guitar Hero - Greatest Hits (Europe, Australia) (En,Fr,De,Es,It)": {"Guitar Hero: Greatest Hits", "Europe,Australia", "En,Fr,De,Es,It"},
	}
	for in, want := range cases {
		p := library.Parse(in)
		if p.Title != want[0] || strings.Join(p.Regions, ",") != want[1] || strings.Join(p.Languages, ",") != want[2] {
			t.Errorf("%q → %q %v %v", in, p.Title, p.Regions, p.Languages)
		}
	}
	if library.Normalize("Final Fantasy VII") != "final fantasy 7" {
		t.Error(library.Normalize("Final Fantasy VII"))
	}
}

// Everyone shares the games; favourites, saves, play time and collections are each person's own.
func TestPerPersonIsolation(t *testing.T) {
	a := newApp(t)
	id := a.gameID("Metroid Fusion")
	a.ok(a.alice, "PUT", fmt.Sprintf("/api/v1/games/%d/mine", id), map[string]any{"favorite": true, "status": "playing", "rating": 9, "notes": "secret"})
	col := a.ok(a.alice, "POST", "/api/v1/collections", map[string]any{"name": "Alice only", "game_ids": []int64{id}})
	se := a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/games/%d/sessions", id), map[string]any{})
	save := a.uploadSave(a.alice, id, "state", []byte("alice's state"))

	// Bob sees the game, not Alice's relation to it
	g := a.ok(a.bob, "GET", fmt.Sprintf("/api/v1/games/%d", id), nil)["game"].(map[string]any)
	if mine := g["mine"].(map[string]any); mine["favorite"] != false || mine["status"] != "" || mine["notes"] != "" {
		t.Fatalf("Bob sees Alice's marks: %v", mine)
	}
	for _, p := range []string{
		fmt.Sprintf("/api/v1/collections/%v", col["id"]),
		fmt.Sprintf("/api/v1/saves/%v/file", save["id"]),
	} {
		if code, _, _ := a.call(a.bob, "GET", p, nil); code != http.StatusNotFound {
			t.Errorf("Bob GET %s → %d, want 404", p, code)
		}
	}
	if code, _, _ := a.call(a.bob, "DELETE", fmt.Sprintf("/api/v1/saves/%v", save["id"]), nil); code != http.StatusNotFound {
		t.Errorf("Bob deleting Alice's save → %d", code)
	}
	if code, _, _ := a.call(a.bob, "POST", fmt.Sprintf("/api/v1/sessions/%v/beat", se["id"]), nil); code != http.StatusNotFound {
		t.Errorf("Bob touching Alice's sitting → %d", code)
	}
	if _, _, l := a.call(a.bob, "GET", "/api/v1/collections", nil); len(l) != 0 {
		t.Errorf("Bob lists %d collections", len(l))
	}
	if m := a.ok(a.bob, "GET", "/api/v1/games?favorite=1", nil); m["total"].(float64) != 0 {
		t.Errorf("Bob's favourites = %v", m["total"])
	}
	// a member cannot change or delete a game someone else added (scanned games belong to the box)
	if code, _, _ := a.call(a.bob, "PUT", fmt.Sprintf("/api/v1/games/%d", id), map[string]any{"name": "x"}); code != http.StatusForbidden {
		t.Errorf("Bob editing → %d", code)
	}
	if code, _, _ := a.call(a.bob, "DELETE", fmt.Sprintf("/api/v1/games/%d", id), nil); code != http.StatusForbidden {
		t.Errorf("Bob deleting → %d", code)
	}
	// the admin sees no one else's things either
	if _, _, l := a.call(a.alice, "GET", "/api/v1/sessions", nil); len(l) != 0 {
		t.Errorf("sittings under 10 s are not listed; got %d", len(l))
	}
}

func (a *app) uploadSave(auth string, id int64, kind string, data []byte) map[string]any {
	a.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "x."+kind)
	_, _ = fw.Write(data)
	_ = mw.Close()
	code, raw, _ := a.raw(auth, "POST", fmt.Sprintf("/api/v1/games/%d/saves?kind=%s", id, kind), &buf, mw.FormDataContentType())
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if code != http.StatusCreated {
		a.t.Fatalf("upload save → %d %s", code, raw)
	}
	return m
}

// Uploads come in chunks at an offset; a zipped disc becomes a folder.
func TestChunkedUploadAndZipUnpack(t *testing.T) {
	a := newApp(t)
	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	for _, n := range []string{"Spyro (USA).cue", "Spyro (USA).bin"} {
		w, _ := zw.Create(n)
		_, _ = w.Write(bytes.Repeat([]byte("s"), 3000))
	}
	_ = zw.Close()
	data := zb.Bytes()
	up := a.ok(a.alice, "POST", "/api/v1/uploads", map[string]any{"platform": "psx", "name": "Spyro (USA).zip", "size": len(data)})
	id := up["id"].(string)
	half := len(data) / 2
	for _, part := range []struct {
		off  int
		body []byte
	}{{0, data[:half]}, {half, data[half:]}} {
		code, raw, _ := a.raw(a.alice, "PUT", fmt.Sprintf("/api/v1/uploads/%s?offset=%d", id, part.off), bytes.NewReader(part.body), "application/octet-stream")
		if code != http.StatusOK {
			t.Fatalf("chunk → %d %s", code, raw)
		}
	}
	// a gap is refused
	if code, _, _ := a.raw(a.alice, "PUT", fmt.Sprintf("/api/v1/uploads/%s?offset=%d", id, len(data)+10), bytes.NewReader([]byte("x")), ""); code != http.StatusConflict {
		t.Fatalf("gap → %d", code)
	}
	done := a.ok(a.alice, "POST", "/api/v1/uploads/"+id+"/finish", nil)
	if done["status"] != "done" || done["game_id"] == nil {
		t.Fatalf("finish = %v", done)
	}
	if fi, err := os.Stat(filepath.Join(a.lib, "roms/psx/Spyro (USA)/Spyro (USA).cue")); err != nil || fi.IsDir() {
		t.Fatalf("the zip was not unpacked into a folder: %v", err)
	}
	g := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/games/%v", done["game_id"]), nil)["game"].(map[string]any)
	if g["name"] != "Spyro" || g["can_edit"] != true {
		t.Fatalf("uploaded game = %v", g)
	}
	// the uploader may delete it; it goes to the trash and comes back
	a.ok(a.alice, "DELETE", fmt.Sprintf("/api/v1/games/%v", done["game_id"]), nil)
	if _, err := os.Stat(filepath.Join(a.lib, "roms/psx/Spyro (USA)")); !os.IsNotExist(err) {
		t.Fatal("the files should be in the trash")
	}
	a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/games/%v/restore", done["game_id"]), nil)
	if _, err := os.Stat(filepath.Join(a.lib, "roms/psx/Spyro (USA)/Spyro (USA).bin")); err != nil {
		t.Fatal("restore did not bring the files back")
	}
}

func basic(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// RomM apps: heartbeat, sign-in, lists, downloads with ranges and zips, props, favourites, saves.
func TestRommCompat(t *testing.T) {
	a := newApp(t)
	code, raw, _ := a.raw("", "GET", "/api/heartbeat", nil, "")
	if code != 200 || !strings.Contains(string(raw), `"VERSION":"5.4.1"`) {
		t.Fatalf("heartbeat → %d %s", code, raw)
	}
	if code, _, _ := a.call("", "GET", "/api/roms", nil); code != http.StatusUnauthorized {
		t.Fatalf("no sign-in → %d", code)
	}
	if code, _, _ := a.call(basic("bob", "wrong"), "GET", "/api/users/me", nil); code != http.StatusUnauthorized {
		t.Fatalf("bad password → %d", code)
	}
	// a client token from a pairing code
	pair := a.ok(a.alice, "POST", "/api/v1/devices/connect", map[string]any{"name": "Odin 2"})
	ex := a.ok("", "POST", "/api/client-tokens/exchange", map[string]any{"code": pair["code"]})
	rmm := ex["raw_token"].(string)
	if !strings.HasPrefix(rmm, "rmm_") {
		t.Fatalf("token = %q", rmm)
	}
	me := a.ok(rmm, "GET", "/api/users/me", nil)
	if me["username"] != "alice" {
		t.Fatalf("me = %v", me)
	}
	_, _, plats := a.call(rmm, "GET", "/api/platforms", nil)
	if len(plats) != 2 {
		t.Fatalf("platforms = %d, want gba and psx", len(plats))
	}
	var psx int64
	for _, p := range plats {
		if p.(map[string]any)["slug"] == "psx" {
			psx = int64(p.(map[string]any)["id"].(float64))
		}
	}
	// the singular platform_id spelling some apps send
	roms := a.ok(rmm, "GET", fmt.Sprintf("/api/roms?platform_id=%d&with_files=true", psx), nil)
	if roms["total"].(float64) != 2 {
		t.Fatalf("psx roms = %v", roms["total"])
	}
	sh := a.gameID("Silent Hill")
	rom := a.ok(rmm, "GET", fmt.Sprintf("/api/roms/%d", sh), nil)
	if rom["has_multiple_files"] != true || len(rom["files"].([]any)) != 2 || rom["fs_path"] != "roms/psx" {
		t.Fatalf("rom = %v %v %v", rom["has_multiple_files"], rom["files"], rom["fs_path"])
	}
	// several files → a stored zip with an .m3u
	code, raw, hdr := a.raw(rmm, "GET", fmt.Sprintf("/api/roms/%d/content/%s", sh, url.PathEscape("Silent Hill (Europe) (En,Fr,De,Es,It)")), nil, "")
	if code != 200 || hdr.Get("Content-Type") != "application/zip" {
		t.Fatalf("zip → %d %s", code, hdr)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if len(names) != 3 || !strings.HasSuffix(names[2], ".m3u") || strings.Contains(names[2], "%20") {
		t.Fatalf("zip entries = %v", names)
	}
	// one file → itself, with ranges
	fusion := a.gameID("Metroid Fusion")
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/roms/%d/content/x.gba", a.url, fusion), nil)
	req.Header.Set("Authorization", "Bearer "+rmm)
	req.Header.Set("Range", "bytes=100-")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusPartialContent || len(b) != 4096-100 {
		t.Fatalf("range → %d, %d bytes", res.StatusCode, len(b))
	}
	// props and the favourites collection
	a.ok(rmm, "PUT", fmt.Sprintf("/api/roms/%d/props", fusion), map[string]any{"now_playing": true, "rating": 7})
	_, _, cols := a.call(rmm, "GET", "/api/collections", nil)
	fav := cols[0].(map[string]any)
	if fav["is_favorite"] != true {
		t.Fatalf("first collection = %v", fav)
	}
	a.ok(rmm, "POST", fmt.Sprintf("/api/collections/%d/roms", int64(fav["id"].(float64))), map[string]any{"rom_ids": []int64{fusion}})
	g := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/games/%d", fusion), nil)["game"].(map[string]any)["mine"].(map[string]any)
	if g["status"] != "playing" || g["rating"].(float64) != 7 || g["favorite"] != true {
		t.Fatalf("mine after RomM props = %v", g)
	}
	// a save from the device, seen in Pocket Games
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("saveFile", "Metroid Fusion (USA).srm")
	_, _ = fw.Write([]byte("sram"))
	_ = mw.Close()
	code, raw, _ = a.raw(rmm, "POST", fmt.Sprintf("/api/saves?rom_id=%d&emulator=mgba", fusion), &buf, mw.FormDataContentType())
	if code != 200 {
		t.Fatalf("save upload → %d %s", code, raw)
	}
	saves := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/games/%d", fusion), nil)["saves"].([]any)
	if len(saves) != 1 || saves[0].(map[string]any)["emulator"] != "mgba" {
		t.Fatalf("saves = %v", saves)
	}
	// deleting from an app only moves the game to the trash
	if code, _, _ := a.call(a.bob, "GET", "/api/collections", nil); code != 200 {
		t.Fatalf("Bob via Pocket token → %d", code)
	}
}

func TestExportImportIdempotent(t *testing.T) {
	a := newApp(t)
	id := a.gameID("Metroid Fusion")
	a.ok(a.alice, "PUT", fmt.Sprintf("/api/v1/games/%d/mine", id), map[string]any{"favorite": true, "status": "finished"})
	a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/games/%d/playtime", id), map[string]any{"minutes": 30})
	a.ok(a.alice, "POST", "/api/v1/collections", map[string]any{"name": "GBA", "game_ids": []int64{id}})
	_, raw, _ := a.raw(a.alice, "GET", "/api/v1/export.json", nil, "")
	for i := 0; i < 2; i++ {
		code, out, _ := a.raw(a.bob, "POST", "/api/v1/import", bytes.NewReader(raw), "application/json")
		if code != 200 {
			t.Fatalf("import → %d %s", code, out)
		}
	}
	_, _, sess := a.call(a.bob, "GET", "/api/v1/sessions", nil)
	_, _, cols := a.call(a.bob, "GET", "/api/v1/collections", nil)
	if len(sess) != 1 || len(cols) != 1 {
		t.Fatalf("after two imports: %d sittings, %d collections", len(sess), len(cols))
	}
}

func TestLinkScopes(t *testing.T) {
	req := func(method, path string) *http.Request { r, _ := http.NewRequest(method, path, nil); return r }
	cases := []struct {
		scopes []string
		r      *http.Request
		want   bool
	}{
		{nil, req("GET", "/api/v1/auth/me"), true},
		{[]string{"games:read"}, req("GET", "/api/v1/games"), true},
		{[]string{"games:read"}, req("GET", "/api/v1/day/2026-11-02"), true},
		{[]string{"games:read"}, req("PUT", "/api/v1/games/4/mine"), false},
		{[]string{"games:write"}, req("PUT", "/api/v1/games/4/mine"), true},
		{[]string{"games:read", "games:write"}, req("DELETE", "/api/v1/games/4"), false},
		{[]string{"games:read", "games:write"}, req("POST", "/api/v1/uploads"), false},
		{[]string{"games:read"}, req("GET", "/api/v1/games/4/download"), false},
	}
	for _, c := range cases {
		if got := linkAllows(c.scopes, c.r); got != c.want {
			t.Errorf("%v %s %s = %v, want %v", c.scopes, c.r.Method, c.r.URL.Path, got, c.want)
		}
	}
}

func TestSettingsPerPerson(t *testing.T) {
	a := newApp(t)
	a.ok(a.bob, "PUT", "/api/v1/settings", map[string]any{"grid_size": "l", "title": "Bob's box"})
	if m := a.ok(a.alice, "GET", "/api/v1/settings", nil); m["grid_size"] != "m" || m["title"] != "Pocket Games" {
		t.Fatalf("Alice's settings changed: %v %v", m["grid_size"], m["title"])
	}
}

func (a *app) waitDownload(auth, id string, want ...string) map[string]any {
	a.t.Helper()
	for i := 0; i < 200; i++ {
		_, _, list := a.call(auth, "GET", "/api/v1/downloads", nil)
		for _, it := range list {
			d := it.(map[string]any)
			if d["id"] == id {
				for _, w := range want {
					if d["status"] == w {
						return d
					}
				}
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	a.t.Fatalf("download %s never reached %v", id, want)
	return nil
}

// A pasted link is fetched by the server and filed like an upload.
func TestDownloadFromLink(t *testing.T) {
	a := newApp(t)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Tetris (World).gb":
			http.ServeContent(w, r, "Tetris (World).gb", time.Now(), bytes.NewReader(bytes.Repeat([]byte("t"), 32768)))
		case "/get":
			w.Header().Set("Content-Disposition", `attachment; filename="Wipeout (Europe).iso"`)
			_, _ = w.Write(bytes.Repeat([]byte("w"), 5000))
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<html>not a file</html>")
		}
	}))
	t.Cleanup(site.Close)

	// an admin may fetch from the local network
	d := a.ok(a.alice, "POST", "/api/v1/downloads", map[string]any{"url": site.URL + "/Tetris%20(World).gb"})
	done := a.waitDownload(a.alice, d["id"].(string), "done", "failed")
	if done["status"] != "done" || done["platform"] != "gb" || done["game_id"] == nil {
		t.Fatalf("download = %v", done)
	}
	if _, err := os.Stat(filepath.Join(a.lib, "roms/gb/Tetris (World).gb")); err != nil {
		t.Fatal("the file is not in the library")
	}
	// an .iso fits several systems: it waits for the person to pick one
	d = a.ok(a.alice, "POST", "/api/v1/downloads", map[string]any{"url": site.URL + "/get"})
	w := a.waitDownload(a.alice, d["id"].(string), "waiting", "failed")
	if w["status"] != "waiting" || len(w["choices"].([]any)) < 2 || w["file_name"] != "Wipeout (Europe).iso" {
		t.Fatalf("ambiguous download = %v", w)
	}
	a.ok(a.alice, "PUT", "/api/v1/downloads/"+d["id"].(string), map[string]any{"platform": "psx"})
	a.waitDownload(a.alice, d["id"].(string), "done")
	// a web page is not a game
	d = a.ok(a.alice, "POST", "/api/v1/downloads", map[string]any{"url": site.URL + "/page"})
	if f := a.waitDownload(a.alice, d["id"].(string), "failed", "done"); !strings.Contains(f["error"].(string), "web page") {
		t.Fatalf("page = %v", f)
	}
	// a member may not reach into the local network
	d = a.ok(a.bob, "POST", "/api/v1/downloads", map[string]any{"url": site.URL + "/Tetris%20(World).gb"})
	if f := a.waitDownload(a.bob, d["id"].(string), "failed", "done"); !strings.Contains(f["error"].(string), "local network") {
		t.Fatalf("member local fetch = %v", f)
	}
	// and nobody sees anyone else's downloads
	if code, _, _ := a.call(a.alice, "DELETE", "/api/v1/downloads/"+d["id"].(string), nil); code != http.StatusNotFound {
		t.Fatalf("Alice deleting Bob's download → %d", code)
	}
}

func TestResolveLinks(t *testing.T) {
	s := &Server{}
	cases := map[string]string{
		"https://drive.google.com/file/d/1AbCdEfGhIjKlMn/view?usp=sharing": "https://drive.usercontent.google.com/download?id=1AbCdEfGhIjKlMn&export=download&confirm=t",
		"https://www.dropbox.com/s/abc/Game.gba?dl=0":                      "https://www.dropbox.com/s/abc/Game.gba?dl=1",
		"https://example.com/roms/Game.nes":                                "https://example.com/roms/Game.nes",
	}
	for in, want := range cases {
		got, err := s.resolveLink(context.Background(), nil, in)
		if err != nil || got != want {
			t.Errorf("%s → %s (%v), want %s", in, got, err, want)
		}
	}
	if _, err := s.resolveLink(context.Background(), nil, "ftp://x/y"); err == nil {
		t.Error("ftp should be refused")
	}
}
