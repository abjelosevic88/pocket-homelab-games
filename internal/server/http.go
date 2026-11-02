package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-games/internal/store"
)

// Router builds the chi router.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	if s.cfg.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(middleware.RequestID)
	r.Use(s.logging)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))

	r.Get("/pocket-ask.js", s.core.AskWidget("games"))
	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)
	r.Get("/api/health", s.handleHealth)
	if s.cfg.MetricsEnabled {
		r.Get("/metrics", s.handleMetrics)
	}
	// the player: a page of its own, and the emulator's files (fetched once, then local)
	r.Get("/play/{id}", s.handlePlayPage)
	r.Get("/emulatorjs/*", s.handleEmulatorJS)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/setup", s.handleSetup)
		r.Get("/auth/status", s.handleAuthStatus)
		r.Get("/auth/oidc/start", s.handleOIDCStart)
		r.Get("/auth/oidc/callback", s.handleOIDCCallback)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			r.Put("/auth/me", s.handleUpdateMe)

			r.Get("/home", s.handleHome)
			r.Get("/dashboard", s.handleDashboard)
			r.Get("/day/{date}", s.handleDay)
			r.Get("/stats", s.handleStats)
			r.Post("/picks", s.handlePicks)

			r.Get("/games", s.handleListGames)
			r.Get("/games/{id}", s.handleGetGame)
			r.Put("/games/{id}", s.handleUpdateGame)
			r.Delete("/games/{id}", s.handleDeleteGame)
			r.Put("/games/{id}/mine", s.handlePutMine)
			r.Post("/games/{id}/lookup", s.handleLookup)
			r.Post("/games/{id}/restore", s.handleRestoreGame)
			r.Delete("/games/{id}/purge", s.handlePurgeGame)
			r.Get("/games/{id}/covers", s.handleCoverCandidates)
			r.Get("/games/{id}/cover", s.handleCover)
			r.Post("/games/{id}/cover", s.handleSetCover)
			r.Delete("/games/{id}/cover", s.handleDeleteCover)
			r.Get("/games/{id}/shots/{n}", s.handleShot)
			r.Get("/games/{id}/download", s.handleDownload)
			r.Head("/games/{id}/download", s.handleDownload)
			r.Get("/games/{id}/files/{fid}", s.handleFile)
			r.Get("/games/{id}/play/{name}", s.handlePlayROM)
			r.Head("/games/{id}/play/{name}", s.handlePlayROM)
			r.Post("/games/{id}/sessions", s.handleStartSession)
			r.Post("/games/{id}/playtime", s.handleAddPlayTime)
			r.Get("/games/{id}/saves", s.handleListSaves)
			r.Post("/games/{id}/saves", s.handleUploadSave)
			r.Get("/trash", s.handleTrash)

			r.Get("/sessions", s.handleListSessions)
			r.Post("/sessions/{sid}/beat", s.handleBeat)
			r.Delete("/sessions/{sid}", s.handleDeleteSession)
			r.Get("/saves", s.handleListSaves)
			r.Get("/saves/{sid}/file", s.handleSaveFile)
			r.Get("/saves/{sid}/shot", s.handleSaveShot)
			r.Put("/saves/{sid}", s.handleRenameSave)
			r.Delete("/saves/{sid}", s.handleDeleteSave)

			r.Get("/collections", s.handleListCollections)
			r.Post("/collections", s.handleCreateCollection)
			r.Get("/collections/{id}", s.handleGetCollection)
			r.Put("/collections/{id}", s.handleUpdateCollection)
			r.Delete("/collections/{id}", s.handleDeleteCollection)
			r.Put("/collections/{id}/games/{gid}", s.handleCollectionGame)

			r.Get("/platforms", s.handleListPlatforms)
			r.Post("/platforms", s.handleCreatePlatform)
			r.Get("/platforms/{id}", s.handleGetPlatform)
			r.Put("/platforms/{id}", s.handleUpdatePlatform)
			r.Get("/catalog", s.handleCatalog)
			r.Get("/firmware/{id}/file", s.handleFirmwareFile)
			r.Get("/firmware/{id}/file/{name}", s.handleFirmwareFile)
			r.Head("/firmware/{id}/file/{name}", s.handleFirmwareFile)
			r.Delete("/firmware/{id}", s.handleDeleteFirmware)

			r.Get("/library", s.handleLibraryStatus)
			r.Post("/library/scan", s.handleScan)
			r.Post("/library/lookup", s.handleLookupAll)
			r.Get("/uploads/guess", s.handleGuessPlatform)
			r.Post("/uploads", s.handleCreateUpload)
			r.Get("/uploads/{id}", s.handleGetUpload)
			r.Put("/uploads/{id}", s.handleUploadChunk)
			r.Post("/uploads/{id}/finish", s.handleFinishUpload)
			r.Delete("/uploads/{id}", s.handleCancelUpload)

			r.Get("/devices", s.handleListDevices)
			r.Delete("/devices/{id}", s.handleDeleteDevice)
			r.Post("/devices/connect", s.handleConnectDevice)
			r.Post("/devices/tokens/{id}/reveal", s.handleRevealToken)
			r.Delete("/devices/tokens/{id}", s.handleDeleteClientToken)
			r.Get("/pair/{code}", s.handlePendingDevice)
			r.Post("/pair", s.handleDecideDevice)
			r.Post("/import/romm", s.handleImportRomm)

			r.Post("/integrations/test", s.handleIntegrationTest)
			r.Get("/ai/status", s.handleAIStatus)
			r.Get("/ai/models", s.handleAIModels)

			r.Get("/users", s.handleListUsers)
			r.Post("/users", s.handleCreateUser)
			r.Put("/users/{id}", s.handleUpdateUser)
			r.Delete("/users/{id}", s.handleDeleteUser)

			r.Get("/settings", s.handleGetSettings)
			r.Put("/settings", s.handlePutSettings)
			r.Get("/settings/system", s.handleSystemInfo)

			r.Get("/tokens", s.handleListTokens)
			r.Post("/tokens", s.handleCreateToken)
			r.Delete("/tokens/{id}", s.handleDeleteToken)

			r.Get("/backup", s.handleBackup)
			r.Get("/export.json", s.handleExportJSON)
			r.Post("/import", s.handleImport)
		})
	})

	// RomM apps on handhelds talk to /api/… as if this were RomM
	if s.cfg.RommCompat {
		s.mountRomm(r)
	}

	r.NotFound(s.handleSPA)
	return r
}

// ---------- helpers ----------

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.requests.Add(1)
		if ww.Status() >= 500 {
			s.errors.Add(1)
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") || strings.HasPrefix(r.URL.Path, "/emulatorjs/") || strings.HasSuffix(r.URL.Path, "/beat") {
			return
		}
		s.log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "dur", time.Since(start).String(), "ip", r.RemoteAddr)
	})
}

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

func (s *Server) fail(w http.ResponseWriter, err error, context string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, store.ErrConflict), errors.Is(err, core.ErrOperationConflict):
		writeErr(w, http.StatusConflict, "already exists")
		return
	}
	s.log.Error(context, "err", err)
	writeErr(w, http.StatusInternalServerError, context+": "+err.Error())
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 32<<20))
	return dec.Decode(v)
}

// decodeKeys decodes JSON into v and also reports which top-level keys were
// present, so PUT handlers can tell "not sent" from "sent empty".
func decodeKeys(r *http.Request, v any) (map[string]bool, error) {
	b, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	keys := make(map[string]bool, len(raw))
	for k := range raw {
		keys[k] = true
	}
	return keys, json.Unmarshal(b, v)
}

func idParam(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return n
}

func queryInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(name))
	return n
}

func queryBool(r *http.Request, name string) bool {
	v := strings.ToLower(r.URL.Query().Get(name))
	return v == "1" || v == "true" || v == "yes"
}

func validDate(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// ---------- health / metrics ----------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"core_outbox": s.coreOutboxHealth(r.Context()), "status": "ok", "version": Version, "uptime": time.Since(s.started).Round(time.Second).String()})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DB.PingContext(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts := s.store.Counts(ctx)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP pocket_games_info Build info.\n# TYPE pocket_games_info gauge\npocket_games_info{version=%q} 1\n", Version)
	fmt.Fprintf(w, "# HELP pocket_games_uptime_seconds Process uptime.\n# TYPE pocket_games_uptime_seconds gauge\npocket_games_uptime_seconds %d\n", int(time.Since(s.started).Seconds()))
	fmt.Fprintf(w, "# HELP pocket_games_http_requests_total HTTP requests served.\n# TYPE pocket_games_http_requests_total counter\npocket_games_http_requests_total %d\n", s.requests.Load())
	fmt.Fprintf(w, "# HELP pocket_games_http_errors_total HTTP 5xx responses.\n# TYPE pocket_games_http_errors_total counter\npocket_games_http_errors_total %d\n", s.errors.Load())
	fmt.Fprintf(w, "# HELP pocket_games_rows Rows per table.\n# TYPE pocket_games_rows gauge\n")
	for k, v := range counts {
		fmt.Fprintf(w, "pocket_games_rows{table=%q} %d\n", k, v)
	}
	st := s.scanState()
	fmt.Fprintf(w, "# HELP pocket_games_last_scan_ms Duration of the last library scan.\n# TYPE pocket_games_last_scan_ms gauge\npocket_games_last_scan_ms %d\n", st.Ms)
}

// ---------- static / SPA ----------

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if s.webFS == nil {
		http.Error(w, "web UI not built (run `make web`)", http.StatusNotFound)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if f, err := s.webFS.Open(path); err == nil {
		f.Close()
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.FileServerFS(s.webFS).ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	r.URL.Path = "/"
	http.FileServerFS(s.webFS).ServeHTTP(w, r)
}

// ---------- files ----------

// saveFile writes bytes under FILES_DIR/<sub>/ with a random name and returns the relative path.
func (s *Server) saveFile(sub, ext string, data []byte) (string, error) {
	dir := filepath.Join(s.cfg.FilesDir, sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ext = strings.ToLower(filepath.Ext("x" + ext))
	if len(ext) > 8 {
		ext = ""
	}
	name := store.RandomToken(12) + ext
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return "", err
	}
	return sub + "/" + name, nil
}

func (s *Server) filePath(rel string) string {
	rel = filepath.Clean("/" + rel)
	return filepath.Join(s.cfg.FilesDir, rel)
}

func (s *Server) removeFile(rel string) {
	if rel == "" {
		return
	}
	_ = os.Remove(s.filePath(rel))
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, rel string) {
	if rel == "" {
		writeErr(w, http.StatusNotFound, "no file")
		return
	}
	p := s.filePath(rel)
	if _, err := os.Stat(p); err != nil {
		writeErr(w, http.StatusNotFound, "file missing")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	// a mislabelled file opened directly must not run as a page of this origin
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, p)
}

// oneLine squeezes whitespace and cuts the text to n characters.
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n]) + "…"
	}
	return s
}
