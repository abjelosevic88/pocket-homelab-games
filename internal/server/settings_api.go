package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-games/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-games/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-games/internal/store"
)

// ---- settings ----

func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "••••"
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

// publicSettings is what one person sees: their settings with keys masked.
func (s *Server) publicSettings(ctx context.Context, u *store.User, st store.Settings) map[string]any {
	return map[string]any{
		"core_configured": s.core.Configured(), "core_registered": s.coreStatus()["registered"],
		"title": st.Title, "date_format": st.DateFormat, "time_format": st.TimeFormat,
		"grid_size": st.GridSize, "show_missing": st.ShowMissing, "default_sort": st.DefaultSort, "regions": st.Regions,
		"auto_save": st.AutoSave, "resume_state": st.ResumeState, "state_on_exit": st.StateOnExit, "touch_controls": st.TouchControls,
		"player_volume": st.PlayerVolume, "player_shader": st.PlayerShader,
		"notify_url": st.NotifyURL, "notify_format": st.NotifyFormat, "notify_configured": st.NotifyURL != "",
		"notify_new_games": st.NotifyNewGames, "notify_uploads": st.NotifyUploads,
		"ai_picks": st.AIPicks, "ai_provider": st.AIProvider, "ai_pocket_model": st.AIPocketModel,
		"openai_api_key": mask(st.OpenAIKey), "openai_model": st.OpenAIModel, "openai_base_url": st.OpenAIBaseURL,
		"setup_complete": st.SetupComplete, "base_url": s.cfg.BaseURL, "romm_compat": s.cfg.RommCompat,
		"metadata": s.cfg.Metadata, "trash_days": s.cfg.TrashDays,
	}
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), userFrom(r.Context()), st))
}

// handlePutSettings saves the caller's personal settings. The box-wide title
// is only taken from an admin and silently ignored for everyone else.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	me := userFrom(r.Context())
	isAdmin := me.Role == "admin"
	cur, err := s.store.GetSettings(r.Context(), me.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	var in store.Settings
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if keys["title"] && isAdmin {
		cur.Title = strings.TrimSpace(in.Title)
		if cur.Title == "" {
			cur.Title = store.DefaultTitle
		}
	}
	text := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimSpace(v)
		}
	}
	intIn := func(k string, dst *int, v, lo, hi int) {
		if keys[k] && v >= lo && v <= hi {
			*dst = v
		}
	}
	boolIn := func(k string, dst *bool, v bool) {
		if keys[k] {
			*dst = v
		}
	}
	oneOf := func(k string, dst *string, v string, allowed ...string) {
		if !keys[k] {
			return
		}
		for _, a := range allowed {
			if v == a {
				*dst = v
			}
		}
	}
	secret := func(k string, dst *string, v string) {
		if keys[k] && !strings.Contains(v, "••") {
			*dst = strings.TrimSpace(v)
		}
	}
	text("date_format", &cur.DateFormat, in.DateFormat)
	text("time_format", &cur.TimeFormat, in.TimeFormat)
	oneOf("grid_size", &cur.GridSize, in.GridSize, "s", "m", "l")
	boolIn("show_missing", &cur.ShowMissing, in.ShowMissing)
	oneOf("default_sort", &cur.DefaultSort, in.DefaultSort, "name", "added", "played", "year", "size", "playtime", "rating", "platform")
	text("regions", &cur.Regions, in.Regions)
	boolIn("auto_save", &cur.AutoSave, in.AutoSave)
	boolIn("resume_state", &cur.ResumeState, in.ResumeState)
	boolIn("state_on_exit", &cur.StateOnExit, in.StateOnExit)
	oneOf("touch_controls", &cur.TouchControls, in.TouchControls, "auto", "on", "off")
	intIn("player_volume", &cur.PlayerVolume, in.PlayerVolume, 0, 100)
	oneOf("player_shader", &cur.PlayerShader, in.PlayerShader, "", "crt-easymode.glslp", "crt-aperture.glslp", "crt-geom.glslp", "crt-mattias.glslp", "2xScaleHQ.glslp", "4xScaleHQ.glslp")
	text("notify_url", &cur.NotifyURL, in.NotifyURL)
	if keys["notify_format"] && in.NotifyFormat != "" {
		cur.NotifyFormat = in.NotifyFormat
	}
	boolIn("notify_new_games", &cur.NotifyNewGames, in.NotifyNewGames)
	boolIn("notify_uploads", &cur.NotifyUploads, in.NotifyUploads)
	boolIn("ai_picks", &cur.AIPicks, in.AIPicks)
	if keys["ai_provider"] {
		if !validAIProvider(in.AIProvider) {
			writeErr(w, http.StatusBadRequest, "AI is automatic, Pocket AI or OpenAI")
			return
		}
		cur.AIProvider = in.AIProvider
	}
	if keys["ai_pocket_model"] {
		if !validPocketModel(in.AIPocketModel) {
			writeErr(w, http.StatusBadRequest, "unknown model")
			return
		}
		cur.AIPocketModel = in.AIPocketModel
	}
	secret("openai_api_key", &cur.OpenAIKey, in.OpenAIKey)
	text("openai_model", &cur.OpenAIModel, in.OpenAIModel)
	if keys["openai_base_url"] {
		cur.OpenAIBaseURL = strings.TrimRight(strings.TrimSpace(in.OpenAIBaseURL), "/")
	}
	if err := s.store.SaveUserSettings(r.Context(), me.ID, cur); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	if isAdmin && keys["title"] {
		if err := s.store.SaveBoxSettings(r.Context(), cur); err != nil {
			s.fail(w, err, "save settings")
			return
		}
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), me, cur))
}

// handleIntegrationTest checks a connection with the caller's own saved
// settings (or the URL/key typed into the form).
func (s *Server) handleIntegrationTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Which  string `json:"which"`
		URL    string `json:"url"`
		Key    string `json:"key"`
		Format string `json:"format"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, _ := s.store.GetSettings(r.Context(), actorID(r.Context()))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	keyOr := func(saved string) string {
		if strings.Contains(in.Key, "••") || in.Key == "" {
			return saved
		}
		return in.Key
	}
	urlOr := func(saved string) string {
		if strings.TrimSpace(in.URL) == "" {
			return saved
		}
		return in.URL
	}
	var msg string
	var err error
	switch in.Which {
	case "openai":
		c := ai.New(urlOr(st.OpenAIBaseURL), keyOr(st.OpenAIKey), st.OpenAIModel, 20*time.Second)
		var ids []string
		if ids, err = c.Models(ctx); err == nil {
			msg = fmt.Sprintf("The key works (%d models)", len(ids))
		}
	case "notify":
		f := in.Format
		if f == "" {
			f = st.NotifyFormat
		}
		n := notify.New(urlOr(st.NotifyURL), f)
		err = n.Send(ctx, notify.Message{Title: "Pocket Games", Body: "Notifications work. New games and finished uploads will arrive here.", Priority: "info", Tags: []string{"video_game"}})
		msg = "Test message sent via " + n.Format
	default:
		writeErr(w, http.StatusBadRequest, "unknown integration")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

// ---- system / backup ----

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var size, files int64
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		size = fi.Size()
	}
	_ = filepath.WalkDir(s.cfg.FilesDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				files += fi.Size()
			}
		}
		return nil
	})
	var ejs int64
	_ = filepath.WalkDir(s.ejsDir(), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				ejs += fi.Size()
			}
		}
		return nil
	})
	u := userFrom(r.Context())
	out := map[string]any{
		"version": Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"core":      s.coreStatus(),
		"auth_mode": s.cfg.AuthMode, "db_bytes": size, "files_bytes": files, "player_bytes": ejs, "free_bytes": diskFree(s.lib.Root),
		"uptime": time.Since(s.started).Round(time.Second).String(), "timezone": time.Now().Location().String(),
		"counts": s.store.CountsFor(r.Context(), actorID(r.Context())), "emulatorjs": s.cfg.EmulatorJSURL,
		"scan": s.scanState(), "romm_compat": s.cfg.RommCompat,
	}
	if u.Role == "admin" {
		out["data_dir"], out["db_path"], out["library_dir"] = s.cfg.DataDir, s.cfg.DBPath, s.cfg.LibraryDir
	}
	writeJSON(w, http.StatusOK, out)
}

// handleBackup downloads the whole database, so it is a box operation for an
// admin only (everyone can export their own data as JSON).
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "backups")
	name := fmt.Sprintf("pocket-games-%s.db", time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := s.store.Backup(r.Context(), dst); err != nil {
		s.fail(w, err, "backup")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, dst)
}
