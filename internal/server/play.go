package server

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-games/internal/library"
	"github.com/abjelosevic88/pocket-homelab-games/internal/platforms"
	"github.com/abjelosevic88/pocket-homelab-games/internal/store"
)

// ---------- EmulatorJS files ----------
//
// The player's files come from EMULATORJS_URL once and are kept under
// DATA_DIR/emulatorjs/<version>/: the second start of a system needs no
// internet, and the page stays same-origin (needed for the cores that use
// threads, which require cross-origin isolation).

var reEJSPath = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)

func (s *Server) ejsDir() string {
	sum := md5.Sum([]byte(s.cfg.EmulatorJSURL))
	return filepath.Join(s.cfg.DataDir, "emulatorjs", hex.EncodeToString(sum[:4]))
}

func (s *Server) handleEmulatorJS(w http.ResponseWriter, r *http.Request) {
	if u, err := s.resolveUser(r); err != nil || u == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	rel := chi.URLParam(r, "*")
	if !reEJSPath.MatchString(rel) || strings.Contains(rel, "..") {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	local := filepath.Join(s.ejsDir(), filepath.FromSlash(rel))
	if _, err := os.Stat(local); err != nil {
		if err := s.fetchEJS(r.Context(), rel, local); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writeErr(w, http.StatusNotFound, "not found")
				return
			}
			s.log.Warn("emulatorjs: fetch failed", "file", rel, "err", err)
			writeErr(w, http.StatusBadGateway, "the player's files could not be downloaded: "+err.Error())
			return
		}
	}
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Cache-Control", "private, max-age=604800")
	http.ServeFile(w, r, local)
}

// fetchEJS downloads one file of the player (once, even when asked twice at the same time).
func (s *Server) fetchEJS(ctx context.Context, rel, local string) error {
	s.ejsMu.Lock()
	if wg, ok := s.ejsFetches[rel]; ok {
		s.ejsMu.Unlock()
		wg.Wait()
		if _, err := os.Stat(local); err != nil {
			return os.ErrNotExist
		}
		return nil
	}
	wg := &sync.WaitGroup{}
	wg.Add(1)
	s.ejsFetches[rel] = wg
	s.ejsMu.Unlock()
	defer func() {
		s.ejsMu.Lock()
		delete(s.ejsFetches, rel)
		s.ejsMu.Unlock()
		wg.Done()
	}()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.EmulatorJSURL+"/"+rel, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", s.cfg.UserAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return os.ErrNotExist
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return err
	}
	tmp := local + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, io.LimitReader(resp.Body, 512<<20))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, local)
}

// ---------- the player page ----------

// handlePlayPage serves the player for one game (a page of its own, so the
// emulator gets the whole window and cross-origin isolation).
func (s *Server) handlePlayPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, err := s.resolveUser(r)
	if err != nil || u == nil {
		http.Redirect(w, r, "/login?next="+r.URL.RequestURI(), http.StatusFound)
		return
	}
	g, err := s.store.GetGame(ctx, u.ID, idParam(r, "id"))
	if err != nil {
		http.Error(w, "No such game.", http.StatusNotFound)
		return
	}
	s.decorate(u, g)
	p := platforms.Get(g.PlatformSlug)
	if !g.Browser {
		http.Redirect(w, r, fmt.Sprintf("/games/%d", g.ID), http.StatusFound)
		return
	}
	files, _ := s.store.GameFiles(ctx, g.ID)
	st, _ := s.store.GetSettings(ctx, u.ID)
	core := p.System
	if g.Mine.Core != "" {
		core = g.Mine.Core
	}
	if c := r.URL.Query().Get("core"); c != "" {
		for _, k := range p.Cores {
			if k == c {
				core = c
			}
		}
	}
	romName := filepath.Base(g.MainFile)
	if len(files) > 1 {
		romName = library.SafeName(library.StripExt(g.FSName)) + ".zip"
	}
	cfg := map[string]any{
		"id": g.ID, "name": g.Name, "platform": p.Short, "system": p.System, "core": core, "threads": p.Threads,
		"rom":      fmt.Sprintf("/api/v1/games/%d/play/%s", g.ID, urlPathEscape(romName)),
		"gameKey":  fmt.Sprintf("pg-%d", g.ID),
		"back":     fmt.Sprintf("/games/%d", g.ID),
		"autoSave": st.AutoSave, "stateOnExit": st.StateOnExit, "volume": float64(st.PlayerVolume) / 100, "touch": st.TouchControls,
		"shader": st.PlayerShader, "cover": g.Cover,
	}
	if bios := s.biosFor(ctx, g, p); bios != "" {
		cfg["bios"] = bios
	}
	// continue from a state: the one asked for, else the newest (when the person wants that)
	states, _ := s.store.ListSaves(ctx, u.ID, g.ID, "state")
	if sid := queryInt(r, "state"); sid > 0 {
		for _, v := range states {
			if v.ID == int64(sid) {
				cfg["state"] = fmt.Sprintf("/api/v1/saves/%d/file", v.ID)
			}
		}
	} else if st.ResumeState && !queryBool(r, "fresh") && len(states) > 0 {
		cfg["state"] = fmt.Sprintf("/api/v1/saves/%d/file", states[0].ID)
		cfg["stateAt"] = states[0].UpdatedAt
	}
	if saves, _ := s.store.ListSaves(ctx, u.ID, g.ID, "save"); len(saves) > 0 {
		cfg["save"] = fmt.Sprintf("/api/v1/saves/%d/file", saves[0].ID)
		cfg["saveMD5"] = saves[0].MD5
	}
	b, _ := json.Marshal(cfg)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// cross-origin isolation: SharedArrayBuffer for the cores that use threads
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
	_ = playTmpl.Execute(w, map[string]any{"Title": g.Name, "Config": template.JS(b), "Accent": "#10b981"})
}

func urlPathEscape(s string) string {
	return strings.ReplaceAll(template.URLQueryEscaper(s), "+", "%20")
}

// biosFor picks the BIOS file for the browser player: a verified dump that
// fits the game's region when there is one.
func (s *Server) biosFor(ctx context.Context, g *store.Game, p platforms.Platform) string {
	check := s.biosCheck(ctx, g.PlatformSlug)
	if check == nil {
		return ""
	}
	items, _ := json.Marshal(check["items"])
	var list []struct {
		File     string `json:"file"`
		Note     string `json:"note"`
		Present  bool   `json:"present"`
		Verified bool   `json:"verified"`
		FileID   int64  `json:"file_id"`
	}
	_ = json.Unmarshal(items, &list)
	best, bestScore := -1, -1
	for i, b := range list {
		if !b.Present {
			continue
		}
		score := 1
		if b.Verified {
			score += 2
		}
		for _, reg := range strings.Split(g.Regions, ", ") {
			if reg != "" && (strings.Contains(b.Note, reg) || (reg == "USA" && strings.Contains(b.Note, "North America"))) {
				score += 4
			}
		}
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 {
		return ""
	}
	// the name the emulator looks for, whatever the file is called on disk
	return fmt.Sprintf("/api/v1/firmware/%d/file/%s", list[best].FileID, urlPathEscape(list[best].File))
}

// handlePlayROM sends the game to the player (several files as one zip,
// which the player unpacks).
func (s *Server) handlePlayROM(w http.ResponseWriter, r *http.Request) {
	g, ok := s.gameFor(w, r)
	if !ok {
		return
	}
	files, _ := s.store.GameFiles(r.Context(), g.ID)
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	if len(files) > 1 && g.MissingSince == "" && g.TrashedAt == "" {
		// several files: one zip built once on the server, so it has a size the
		// player's cache can compare (and the next start does not zip again)
		p, err := s.cachedZip(g, s.zipEntries(g, files, false, library.StripExt(g.FSName)), false)
		if err == nil {
			if fh, err := os.Open(p); err == nil {
				defer fh.Close()
				fi, _ := fh.Stat()
				w.Header().Set("Content-Type", "application/zip")
				http.ServeContent(w, r, filepath.Base(p), fi.ModTime(), fh)
				return
			}
		}
	}
	s.sendGame(w, r, g, files, false)
}

// ---------- play sessions ----------

func (s *Server) handleStartSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, ok := s.gameFor(w, r)
	if !ok {
		return
	}
	var in struct {
		Device string `json:"device"`
	}
	_ = decode(r, &in)
	if in.Device == "" {
		in.Device = "browser"
	}
	se, err := s.store.StartSession(ctx, userFrom(ctx).ID, g.ID, oneLine(in.Device, 60))
	if err != nil {
		s.fail(w, err, "session")
		return
	}
	writeJSON(w, http.StatusCreated, se)
}

func (s *Server) handleBeat(w http.ResponseWriter, r *http.Request) {
	se, err := s.store.TouchSession(r.Context(), userFrom(r.Context()).ID, idParam(r, "sid"), 3*time.Minute)
	if err != nil {
		s.fail(w, err, "session")
		return
	}
	writeJSON(w, http.StatusOK, se)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := s.store.ListSessions(r.Context(), userFrom(r.Context()).ID, store.SessionFilter{GameID: int64(queryInt(r, "game")), From: q.Get("from"), To: q.Get("to"), Limit: max(queryInt(r, "limit"), 0)})
	if err != nil {
		s.fail(w, err, "sessions")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeletePlaySession(r.Context(), userFrom(r.Context()).ID, idParam(r, "sid")); err != nil {
		s.fail(w, err, "session")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAddPlayTime records time played elsewhere (a console, a handheld without the app).
func (s *Server) handleAddPlayTime(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, ok := s.gameFor(w, r)
	if !ok {
		return
	}
	var in struct {
		Minutes int    `json:"minutes"`
		Day     string `json:"day"`
		Device  string `json:"device"`
	}
	if err := decode(r, &in); err != nil || in.Minutes <= 0 || in.Minutes > 24*60 {
		writeErr(w, http.StatusBadRequest, "minutes between 1 and 1440")
		return
	}
	start := time.Now().Add(-time.Duration(in.Minutes) * time.Minute)
	if in.Day != "" {
		d, err := time.ParseInLocation("2006-01-02", in.Day, time.Local)
		if err != nil || d.After(time.Now()) {
			writeErr(w, http.StatusBadRequest, "day: YYYY-MM-DD, not in the future")
			return
		}
		start = d.Add(20 * time.Hour)
	}
	if in.Device == "" {
		in.Device = "elsewhere"
	}
	if err := s.store.AddPlayTime(ctx, userFrom(ctx).ID, g.ID, start, int64(in.Minutes)*60, oneLine(in.Device, 60)); err != nil {
		s.fail(w, err, "play time")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- saves and states ----------

const maxSave = 128 << 20

// handleListSaves lists the caller's saves (of one game, or all with ?game=0).
func (s *Server) handleListSaves(w http.ResponseWriter, r *http.Request) {
	gid := idParam(r, "id")
	list, err := s.store.ListSaves(r.Context(), userFrom(r.Context()).ID, gid, r.URL.Query().Get("kind"))
	if err != nil {
		s.fail(w, err, "saves")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleUploadSave stores a save or a state (multipart: file, optional
// screenshot; or the raw body). slot=auto replaces the previous automatic
// one; manual states beyond the newest 30 are dropped.
func (s *Server) handleUploadSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	g, ok := s.gameFor(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	kind := q.Get("kind")
	if kind != "save" && kind != "state" {
		writeErr(w, http.StatusBadRequest, "kind is save or state")
		return
	}
	var data, shot []byte
	name := q.Get("name")
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if err := r.ParseMultipartForm(maxSave); err != nil {
			writeErr(w, http.StatusBadRequest, "the upload could not be read")
			return
		}
		f, fh, err := r.FormFile("file")
		if err != nil {
			writeErr(w, http.StatusBadRequest, "no file")
			return
		}
		data, _ = io.ReadAll(io.LimitReader(f, maxSave))
		f.Close()
		if name == "" {
			name = fh.Filename
		}
		if sf, _, err := r.FormFile("screenshot"); err == nil {
			shot, _ = io.ReadAll(io.LimitReader(sf, 8<<20))
			sf.Close()
		}
	} else {
		var err error
		data, err = io.ReadAll(http.MaxBytesReader(w, r.Body, maxSave))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "the save is too big")
			return
		}
	}
	if len(data) == 0 {
		writeErr(w, http.StatusBadRequest, "the save is empty")
		return
	}
	device := firstNonEmpty(q.Get("device"), "browser")
	v, err := s.storeSave(ctx, u.ID, g, kind, library.SafeName(name), q.Get("emulator"), q.Get("slot"), device, data, shot)
	if err != nil {
		s.fail(w, err, "save")
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

// storeSave writes a save's file and row (replacing the same slot, or the same file name for in-game saves).
func (s *Server) storeSave(ctx context.Context, uid int64, g *store.Game, kind, name, emulator, slot, device string, data, shot []byte) (*store.Save, error) {
	if name == "" {
		ext := ".srm"
		if kind == "state" {
			ext = ".state"
		}
		name = library.SafeName(library.StripExt(filepath.Base(g.MainFile))) + ext
	}
	sum := md5.Sum(data)
	digest := hex.EncodeToString(sum[:])
	var v *store.Save
	if slot != "" || kind == "save" {
		v, _ = s.store.FindSave(ctx, uid, g.ID, kind, name, slot)
	}
	if v != nil && v.MD5 == digest && shot == nil {
		return v, nil // nothing new
	}
	rel, err := s.saveFile(fmt.Sprintf("saves/%d/%d", uid, g.ID), filepath.Ext(name), data)
	if err != nil {
		return nil, err
	}
	shotRel := ""
	if len(shot) > 0 {
		shotRel, _ = s.savePicture(fmt.Sprintf("saves/%d/%d", uid, g.ID), shot, 640)
	}
	if v == nil {
		v = &store.Save{UserID: uid, GameID: g.ID, Kind: kind}
	} else {
		s.removeFile(v.Path)
		if shotRel != "" {
			s.removeFile(v.ShotPath)
		}
	}
	v.FileName, v.Emulator, v.Slot, v.Path, v.SizeBytes, v.MD5, v.Device = name, oneLine(emulator, 60), oneLine(slot, 40), rel, int64(len(data)), digest, oneLine(device, 60)
	if shotRel != "" {
		v.ShotPath = shotRel
	}
	if err := s.store.PutSave(ctx, v); err != nil {
		s.removeFile(rel)
		return nil, err
	}
	v.HasShot = v.ShotPath != ""
	if kind == "state" && slot == "" {
		s.pruneStates(ctx, uid, g.ID, 30)
	}
	return v, nil
}

func (s *Server) pruneStates(ctx context.Context, uid, gameID int64, keep int) {
	list, _ := s.store.ListSaves(ctx, uid, gameID, "state")
	n := 0
	for _, v := range list {
		if v.Slot != "" {
			continue
		}
		n++
		if n > keep {
			s.removeFile(v.Path)
			s.removeFile(v.ShotPath)
			_ = s.store.DeleteSave(ctx, uid, v.ID)
		}
	}
}

func (s *Server) saveFor(w http.ResponseWriter, r *http.Request) (*store.Save, bool) {
	v, err := s.store.GetSave(r.Context(), userFrom(r.Context()).ID, idParam(r, "sid"))
	if err != nil {
		s.fail(w, err, "save")
		return nil, false
	}
	return v, true
}

func (s *Server) handleSaveFile(w http.ResponseWriter, r *http.Request) {
	v, ok := s.saveFor(w, r)
	if !ok {
		return
	}
	p := s.filePath(v.Path)
	fh, err := os.Open(p)
	if err != nil {
		writeErr(w, http.StatusGone, "the file is missing")
		return
	}
	defer fh.Close()
	fi, _ := fh.Stat()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Content-Disposition", contentDisposition(v.FileName))
	w.Header().Set("Cache-Control", "private, no-cache")
	http.ServeContent(w, r, v.FileName, fi.ModTime(), fh)
}

func (s *Server) handleSaveShot(w http.ResponseWriter, r *http.Request) {
	v, ok := s.saveFor(w, r)
	if !ok {
		return
	}
	s.servePicture(w, r, v.ShotPath)
}

func (s *Server) handleDeleteSave(w http.ResponseWriter, r *http.Request) {
	v, ok := s.saveFor(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteSave(r.Context(), v.UserID, v.ID); err != nil {
		s.fail(w, err, "delete")
		return
	}
	s.removeFile(v.Path)
	s.removeFile(v.ShotPath)
	w.WriteHeader(http.StatusNoContent)
}

// handleRenameSave gives a state a name of its own ("before the boss").
func (s *Server) handleRenameSave(w http.ResponseWriter, r *http.Request) {
	v, ok := s.saveFor(w, r)
	if !ok {
		return
	}
	var in struct {
		Slot string `json:"slot"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	v.Slot = oneLine(in.Slot, 40)
	if err := s.store.PutSave(r.Context(), v); err != nil {
		s.fail(w, err, "save")
		return
	}
	v.HasShot = v.ShotPath != ""
	writeJSON(w, http.StatusOK, v)
}

var playTmpl = template.Must(template.New("play").Parse(playHTML))

const playHTML = `<!doctype html>
<html lang="en" translate="no">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover, user-scalable=no">
<meta name="color-scheme" content="dark">
<meta name="theme-color" content="#000000">
<meta name="apple-mobile-web-app-capable" content="yes">
<title>{{.Title}} · Pocket Games</title>
<link rel="icon" href="/favicon.svg" type="image/svg+xml">
<style>
  html, body { margin: 0; height: 100%; background: #000; color: #eee; font: 14px/1.4 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; overflow: hidden; }
  #bar { position: fixed; inset: 0 0 auto 0; height: 44px; padding: env(safe-area-inset-top) max(10px, env(safe-area-inset-right)) 0 max(10px, env(safe-area-inset-left));
    display: flex; align-items: center; gap: 8px; background: #0b0c0f; border-bottom: 1px solid #1d2026; z-index: 20; }
  #bar .t { flex: 1; min-width: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-weight: 600; }
  #bar .t small { font-weight: 400; color: #aaa; margin-left: 6px; }
  #bar button, #bar a { all: unset; cursor: pointer; display: inline-flex; align-items: center; gap: 6px; padding: 7px 10px; border-radius: 9px; color: #eee; background: #ffffff14; }
  #bar button:hover, #bar a:hover { background: #ffffff26; }
  #bar svg { width: 18px; height: 18px; }
  #status { color: #9ca3af; font-size: 12px; white-space: nowrap; }
  #wrap { position: fixed; inset: calc(44px + env(safe-area-inset-top)) 0 0 0; }
  #game { width: 100%; height: 100%; }
  #drawer { position: fixed; top: 0; right: 0; bottom: 0; width: min(360px, 92vw); background: #111317; border-left: 1px solid #262a31; z-index: 30;
    transform: translateX(100%); transition: transform .22s ease; display: flex; flex-direction: column; padding-top: env(safe-area-inset-top); }
  #drawer.open { transform: none; }
  #drawer header { display: flex; align-items: center; justify-content: space-between; padding: 14px 16px; border-bottom: 1px solid #262a31; font-weight: 600; }
  #drawer header button { all: unset; cursor: pointer; padding: 4px 8px; border-radius: 6px; background: #ffffff14; }
  #states { overflow: auto; padding: 12px; display: grid; gap: 10px; }
  .st { display: grid; grid-template-columns: 112px 1fr; gap: 10px; align-items: center; padding: 8px; border-radius: 10px; background: #1a1d23; cursor: pointer; border: 1px solid transparent; }
  .st:hover { border-color: {{.Accent}}; }
  .st img, .st .ph { width: 112px; height: 84px; object-fit: cover; border-radius: 6px; background: #0b0c0f; }
  .st b { display: block; font-weight: 600; }
  .st span { color: #9ca3af; font-size: 12px; }
  .empty { color: #9ca3af; padding: 20px 8px; text-align: center; }
  #toast { position: fixed; left: 50%; bottom: 24px; transform: translateX(-50%); background: #1f2937ee; color: #fff; padding: 9px 14px; border-radius: 10px; z-index: 40; opacity: 0; transition: opacity .2s; pointer-events: none; }
  #toast.show { opacity: 1; }
</style>
</head>
<body>
<div id="bar">
  <a id="back" title="Back to the game"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 18l-6-6 6-6"/></svg>Back</a>
  <div class="t" id="title"></div>
  <span id="status"></span>
  <button id="saveBtn" title="Save a state now"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/><path d="M17 21v-8H7v8M7 3v5h8"/></svg><span class="lbl">Save</span></button>
  <button id="statesBtn" title="Saved states"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg><span class="lbl">States</span></button>
  <button id="fsBtn" title="Full screen"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3H5a2 2 0 0 0-2 2v3m18 0V5a2 2 0 0 0-2-2h-3m0 18h3a2 2 0 0 0 2-2v-3M3 16v3a2 2 0 0 0 2 2h3"/></svg></button>
</div>
<div id="wrap"><div id="game"></div></div>
<aside id="drawer" aria-label="Saved states"><header>Saved states <button id="closeDrawer">Close</button></header><div id="states"></div></aside>
<div id="toast"></div>
<script>
(function () {
  const C = {{.Config}};
  const $ = (id) => document.getElementById(id);
  $('title').innerHTML = '';
  $('title').append(document.createTextNode(C.name));
  const sm = document.createElement('small'); sm.textContent = C.platform; $('title').append(sm);
  if (innerWidth < 520) document.querySelectorAll('.lbl').forEach(e => e.remove());
  let toastT;
  const toast = (t) => { const el = $('toast'); el.textContent = t; el.classList.add('show'); clearTimeout(toastT); toastT = setTimeout(() => el.classList.remove('show'), 2200); };
  const status = (t) => { $('status').textContent = t || ''; };

  // ---- the emulator ----
  window.EJS_player = '#game';
  window.EJS_core = C.core;
  window.EJS_gameUrl = C.rom;
  window.EJS_gameName = C.gameKey;
  window.EJS_gameID = C.id;
  window.EJS_pathtodata = '/emulatorjs/';
  window.EJS_startOnLoaded = true;
  window.EJS_color = '{{.Accent}}';
  window.EJS_volume = C.volume;
  window.EJS_threads = !!C.threads && typeof SharedArrayBuffer === 'function';
  window.EJS_backgroundImage = C.cover || undefined;
  window.EJS_backgroundBlur = true;
  // the browser keeps the game (and its BIOS) after the first start: later starts skip the download
  window.EJS_CacheLimit = 2 * 1024 * 1024 * 1024;
  if (C.bios) window.EJS_biosUrl = C.bios;
  if (C.state) window.EJS_loadStateURL = C.state;
  const opts = { 'save-state-location': 'browser', 'save-save-interval': C.autoSave ? '30' : '0' };
  if (C.touch === 'on') opts['virtual-gamepad'] = 'enabled';
  if (C.touch === 'off') opts['virtual-gamepad'] = 'disabled';
  if (C.shader) opts['shader'] = C.shader;
  window.EJS_defaultOptions = opts;

  const emu = () => window.EJS_emulator;
  const gm = () => emu() && emu().gameManager;

  async function upload(kind, data, shot, slot, name) {
    const fd = new FormData();
    fd.append('file', new Blob([data]), name || (kind === 'state' ? 'game.state' : 'game.srm'));
    if (shot) fd.append('screenshot', new Blob([shot], { type: 'image/png' }), 'shot.png');
    const q = new URLSearchParams({ kind, emulator: (emu() && emu().getCore && emu().getCore()) || C.core });
    if (slot) q.set('slot', slot);
    const r = await fetch('/api/v1/games/' + C.id + '/saves?' + q, { method: 'POST', body: fd, credentials: 'same-origin' });
    if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error || r.statusText);
    return r.json();
  }
  async function shot() { try { return await gm().screenshot(); } catch (e) { return null; } }

  // in-game saves (memory cards, battery saves) follow the person
  let lastSave = C.saveMD5 || '';
  async function digest(buf) {
    if (!crypto.subtle) return String(buf.length) + ':' + buf[0] + ':' + buf[buf.length - 1];
    const h = await crypto.subtle.digest('SHA-1', buf); return Array.from(new Uint8Array(h)).map(b => b.toString(16).padStart(2, '0')).join('');
  }
  let lastDigest = '';
  async function pushSave(data) {
    if (!data || !data.length || !C.autoSave) return;
    const d = await digest(data);
    if (d === lastDigest) return;
    lastDigest = d;
    try { await upload('save', data, null, 'auto', null); status('Saved ' + new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })); } catch (e) { status('Save not stored'); }
  }
  async function loadServerSave() {
    if (!C.save || !gm()) return;
    try {
      const r = await fetch(C.save, { credentials: 'same-origin' });
      if (!r.ok) return;
      const data = new Uint8Array(await r.arrayBuffer());
      const path = gm().getSaveFilePath();
      const parts = path.split('/'); let cp = '';
      for (let i = 0; i < parts.length - 1; i++) { if (!parts[i]) continue; cp += '/' + parts[i]; if (!gm().FS.analyzePath(cp).exists) gm().FS.mkdir(cp); }
      if (gm().FS.analyzePath(path).exists) gm().FS.unlink(path);
      gm().FS.writeFile(path, data);
      gm().loadSaveFiles();
      lastDigest = await digest(data);
    } catch (e) { console.warn('save load', e); }
  }

  // play time
  let session = null, beat = null;
  async function startSession() {
    try {
      const r = await fetch('/api/v1/games/' + C.id + '/sessions', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ device: 'browser' }) });
      if (r.ok) session = await r.json();
    } catch (e) {}
    beat = setInterval(() => { if (session && !document.hidden) fetch('/api/v1/sessions/' + session.id + '/beat', { method: 'POST', credentials: 'same-origin', keepalive: true }); }, 60000);
  }

  window.EJS_onGameStart = function () {
    loadServerSave();
    startSession();
    if (C.stateAt) toast('Continuing where you left off');
    try { emu().on('saveSaveFiles', (data) => pushSave(data)); } catch (e) {}
  };
  window.EJS_onSaveState = async function (e) {
    try { const v = await upload('state', e.state, await shot(), '', null); toast('State saved'); lastState = v; } catch (err) { toast('The state could not be saved: ' + err.message); }
  };
  window.EJS_onLoadState = function () { openDrawer(); };
  window.EJS_onSaveSave = async function (e) { await pushSave(e.save); toast('Game saved to the server'); };
  window.EJS_onLoadSave = function () { loadServerSave().then(() => toast('Loaded the save from the server')); };

  // ---- states drawer ----
  let lastState = null;
  async function openDrawer() {
    $('drawer').classList.add('open');
    const box = $('states'); box.textContent = 'Loading…';
    const r = await fetch('/api/v1/games/' + C.id + '/saves?kind=state', { credentials: 'same-origin' });
    const list = r.ok ? await r.json() : [];
    box.textContent = '';
    if (!list.length) { const d = document.createElement('div'); d.className = 'empty'; d.textContent = 'No saved states yet. Press Save to keep this moment.'; box.append(d); return; }
    for (const v of list) {
      const row = document.createElement('div'); row.className = 'st';
      const img = v.has_shot ? Object.assign(document.createElement('img'), { src: '/api/v1/saves/' + v.id + '/shot', alt: '' }) : Object.assign(document.createElement('div'), { className: 'ph' });
      const meta = document.createElement('div');
      const b = document.createElement('b'); b.textContent = v.slot === 'auto' ? 'When you left' : (v.slot || new Date(v.updated_at).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' }));
      const sp = document.createElement('span'); sp.textContent = (v.slot ? new Date(v.updated_at).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' }) + ' · ' : '') + (v.device || '');
      meta.append(b, sp); row.append(img, meta);
      row.onclick = async () => {
        const f = await fetch('/api/v1/saves/' + v.id + '/file', { credentials: 'same-origin' });
        if (!f.ok) { toast('The state could not be loaded'); return; }
        gm().loadState(new Uint8Array(await f.arrayBuffer()));
        closeDrawer(); toast('State loaded');
      };
      box.append(row);
    }
  }
  function closeDrawer() { $('drawer').classList.remove('open'); }
  $('closeDrawer').onclick = closeDrawer;
  $('statesBtn').onclick = () => ($('drawer').classList.contains('open') ? closeDrawer() : openDrawer());
  $('saveBtn').onclick = async () => {
    if (!gm()) return;
    try { const st = gm().getState(); await window.EJS_onSaveState({ state: st }); } catch (e) { toast('This game cannot be saved right now'); }
  };
  $('fsBtn').onclick = () => { const el = document.documentElement; if (document.fullscreenElement) document.exitFullscreen(); else if (el.requestFullscreen) el.requestFullscreen().catch(() => {}); };

  // leaving: keep the in-game save and the moment you left
  let leaving = false;
  async function keepProgress() {
    if (!gm() || !emu().started) return;
    try { gm().saveSaveFiles(); const d = gm().getSaveFile(false); await pushSave(d); } catch (e) {}
    if (C.stateOnExit) { try { await upload('state', gm().getState(), await shot(), 'auto', null); } catch (e) {} }
    if (session) { try { await fetch('/api/v1/sessions/' + session.id + '/beat', { method: 'POST', credentials: 'same-origin', keepalive: true }); } catch (e) {} }
  }
  $('back').onclick = async () => {
    if (leaving) return; leaving = true; status('Saving…');
    await keepProgress();
    location.href = C.back;
  };
  document.addEventListener('visibilitychange', () => { if (document.hidden) keepProgress(); });

})();
</script>
<script src="/emulatorjs/loader.js"></script>
</body>
</html>`
