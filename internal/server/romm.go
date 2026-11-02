package server

// A RomM 5.4.1-compatible API under /api, so the RomM apps on handhelds,
// phones and launchers (Argosy, Grout, romm-mobile, the muOS app, Playnite …)
// keep working against Pocket Games: browse platforms and games, download
// them (with resume), sync saves and states, report play time.
//
// Ids are Pocket Games' own: rom id = game id, platform id = platform id,
// file id = game file id. People sign in with their email (or the part
// before the @) and password, a Pocket Games API token, an rmm_ client token
// (pairing code / device flow, made in Settings → Devices) or the tokens of
// POST /api/token.

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/abjelosevic88/pocket-homelab-games/internal/library"
	"github.com/abjelosevic88/pocket-homelab-games/internal/platforms"
	"github.com/abjelosevic88/pocket-homelab-games/internal/store"
)

const rommVersion = "5.4.1"

var (
	rommRead  = []string{"me.read", "roms.read", "platforms.read", "assets.read", "devices.read", "firmware.read", "roms.user.read", "collections.read", "playlists.read"}
	rommWrite = []string{"me.write", "assets.write", "devices.write", "roms.user.write", "collections.write", "playlists.write"}
	rommEdit  = []string{"roms.write", "platforms.write", "firmware.write"}
	rommFull  = []string{"users.read", "users.write", "tasks.run", "logs.read"}
)

func rommScopes(u *store.User) []string {
	out := append(append(append([]string{}, rommRead...), rommWrite...), rommEdit...)
	if u.Role == "admin" {
		out = append(out, rommFull...)
	}
	return out
}

const rommCookie = "romm_session"

type rommCtx struct {
	user   *store.User
	device string // device uuid of a device-bound token
}

const rommKey ctxKey = 3

func rommFrom(ctx context.Context) *rommCtx {
	c, _ := ctx.Value(rommKey).(*rommCtx)
	return c
}

// rommDetail writes RomM's error shape.
func rommDetail(w http.ResponseWriter, status int, detail any) {
	writeJSON(w, status, map[string]any{"detail": detail})
}

// rommUser resolves a request the way RomM does: Basic, Bearer (rmm_ client
// tokens, /api/token access tokens, Pocket Games tokens, Pocket Core tokens),
// X-Api-Key, then the session cookies. A bad credential is "not signed in".
func (s *Server) rommUser(r *http.Request) *rommCtx {
	ctx := r.Context()
	parts := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "basic") {
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil
		}
		name, pass, ok := strings.Cut(string(b), ":")
		if !ok {
			return nil
		}
		if u := s.passwordUser(ctx, name, pass); u != nil {
			return &rommCtx{user: u}
		}
		return nil
	}
	tok := ""
	if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
		tok = strings.TrimSpace(parts[1])
	} else if k := r.Header.Get("X-Api-Key"); k != "" {
		tok = k
	}
	if tok != "" {
		if strings.HasPrefix(tok, "rmm_") || strings.HasPrefix(tok, "pgm_a") {
			kind := "client"
			if strings.HasPrefix(tok, "pgm_a") {
				kind = "access"
			}
			if u, t, err := s.store.CompatTokenUser(ctx, tok, kind); err == nil {
				return &rommCtx{user: u, device: t.DeviceUUID}
			}
			return nil
		}
		if u, _, err := s.resolveAuth(r); err == nil && u != nil {
			return &rommCtx{user: u}
		}
		return nil
	}
	if c, err := r.Cookie(rommCookie); err == nil {
		if u, err := s.store.GetSessionUser(ctx, c.Value); err == nil {
			return &rommCtx{user: u}
		}
	}
	if u, _, err := s.resolveAuth(r); err == nil && u != nil {
		return &rommCtx{user: u}
	}
	return nil
}

// passwordUser checks a sign-in: the email, or the part before the @, or the name.
func (s *Server) passwordUser(ctx context.Context, name, pass string) *store.User {
	name = strings.TrimSpace(strings.ToLower(name))
	var u *store.User
	if x, err := s.store.GetUserByEmail(ctx, name); err == nil {
		u = x
	} else if users, err := s.store.ListUsers(ctx); err == nil {
		for i := range users {
			local, _, _ := strings.Cut(users[i].Email, "@")
			if local == name || strings.EqualFold(users[i].Name, name) {
				u = &users[i]
				break
			}
		}
	}
	if u == nil || u.PasswordHash == "" {
		time.Sleep(300 * time.Millisecond)
		return nil
	}
	if len(pass) > 72 {
		pass = pass[:72]
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(pass)) != nil {
		time.Sleep(300 * time.Millisecond)
		return nil
	}
	return u
}

func rommUsername(u *store.User) string {
	local, _, _ := strings.Cut(u.Email, "@")
	return local
}

// rommAuth guards the RomM routes and counts the app as a device.
func (s *Server) rommAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := s.rommUser(r)
		if rc == nil {
			w.Header().Set("WWW-Authenticate", "Bearer")
			rommDetail(w, http.StatusUnauthorized, "Not authenticated")
			return
		}
		if ua := r.UserAgent(); ua != "" && !strings.Contains(ua, "Mozilla/") {
			download := strings.Contains(r.URL.Path, "/content")
			if rc.device != "" {
				_, _ = s.store.DB.ExecContext(r.Context(), `UPDATE devices SET last_seen = ?, requests = requests + 1, downloads = downloads + ?, ip = ? WHERE uuid = ?`, store.Now(), dlInt(download), r.RemoteAddr, rc.device)
			} else {
				s.store.SeeDevice(r.Context(), rc.user.ID, "", ua, r.RemoteAddr, download)
			}
		}
		ctx := context.WithValue(r.Context(), rommKey, rc)
		ctx = context.WithValue(ctx, userKey, rc.user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func dlInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// mountRomm adds the RomM routes next to /api/v1.
func (s *Server) mountRomm(r chi.Router) {
	r.Get("/api/heartbeat", s.rommHeartbeat)
	r.Get("/api/stats", s.rommStats)
	r.Post("/api/token", s.rommToken)
	r.Post("/api/login", s.rommLogin)
	r.Post("/api/logout", s.rommLogout)
	r.Post("/api/forgot-password", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, nil) })
	r.Post("/api/client-tokens/exchange", s.rommExchange)
	r.Get("/api/client-tokens/pair/{code}/status", s.rommPairStatus)
	r.Post("/api/auth/device/init", s.rommDeviceInit)
	r.Post("/api/auth/device/token", s.rommDeviceToken)
	r.Get("/assets/romm/resources/roms/{pid}/{rid}/cover/{size}", s.rommCover)
	r.Get("/assets/romm/resources/roms/{pid}/{rid}/screenshots/{n}", s.rommShot)

	r.Group(func(r chi.Router) {
		r.Use(s.rommAuth)
		r.Get("/api/config", s.rommConfig)
		r.Get("/api/users/me", s.rommMe)
		r.Get("/api/users/{id}/avatar", func(w http.ResponseWriter, r *http.Request) { rommDetail(w, http.StatusNotFound, "Avatar not found") })
		r.Get("/api/permissions/me", s.rommPermissions)

		r.Get("/api/client-tokens", s.rommListClientTokens)
		r.Post("/api/client-tokens", s.rommCreateClientToken)
		r.Delete("/api/client-tokens/{id}", s.rommDeleteClientToken)
		r.Put("/api/client-tokens/{id}/regenerate", s.rommRegenerateClientToken)
		r.Post("/api/client-tokens/{id}/pair", s.rommPairCode)
		r.Get("/api/auth/device/pending/{code}", s.rommDevicePending)
		r.Post("/api/auth/device/approve", s.rommDeviceApprove)
		r.Post("/api/auth/device/deny", s.rommDeviceDeny)

		r.Get("/api/platforms", s.rommPlatforms)
		r.Get("/api/platforms/identifiers", s.rommPlatformIDs)
		r.Get("/api/platforms/{id}", s.rommPlatform)

		r.Get("/api/roms", s.rommRoms)
		r.Get("/api/roms/identifiers", s.rommRomIDs)
		r.Get("/api/roms/by-hash", s.rommByHash)
		r.Get("/api/roms/download", s.rommBulkDownload)
		r.Get("/api/roms/{id}", s.rommRom(true))
		r.Get("/api/roms/{id}/simple", s.rommRom(false))
		r.Get("/api/roms/{id}/files", s.rommRomFile)
		r.Get("/api/roms/{id}/content/{name}", s.rommContent)
		r.Head("/api/roms/{id}/content/{name}", s.rommContent)
		r.Get("/api/roms/{id}/files/content/{name}", s.rommFileContent)
		r.Head("/api/roms/{id}/files/content/{name}", s.rommFileContent)
		r.Put("/api/roms/{id}/props", s.rommProps)
		r.Get("/api/roms/{id}/notes", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, []any{}) })
		r.Get("/api/search/cover", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, []any{}) })

		r.Get("/api/firmware", s.rommFirmwareList)
		r.Get("/api/firmware/identifiers", s.rommFirmwareIDs)
		r.Get("/api/firmware/{id}", s.rommFirmware)
		r.Get("/api/firmware/{id}/content/{name}", s.handleFirmwareFile)
		r.Head("/api/firmware/{id}/content/{name}", s.handleFirmwareFile)

		for _, kind := range []string{"saves", "states", "screenshots"} {
			r.Get("/api/"+kind, s.rommAssets(kind))
			r.Get("/api/"+kind+"/identifiers", s.rommAssetIDs(kind))
			r.Get("/api/"+kind+"/{id}", s.rommAsset(kind))
			r.Get("/api/"+kind+"/{id}/content", s.rommAssetContent(kind))
			r.Get("/api/"+kind+"/{id}/content/{name}", s.rommAssetContent(kind))
			r.Post("/api/"+kind, s.rommUploadAsset(kind))
			r.Put("/api/"+kind+"/{id}", s.rommUpdateAsset(kind))
			r.Delete("/api/"+kind+"/{id}", s.rommDeleteOne(kind))
			r.Post("/api/"+kind+"/delete", s.rommDeleteAssets(kind))
		}
		r.Get("/api/saves/summary", s.rommSaveSummary)
		r.Post("/api/saves/{id}/downloaded", s.rommAsset("saves"))
		r.Post("/api/saves/{id}/track", s.rommAsset("saves"))
		r.Post("/api/saves/{id}/untrack", s.rommAsset("saves"))

		r.Get("/api/collections", s.rommCollections)
		r.Get("/api/collections/identifiers", s.rommCollectionIDs)
		r.Get("/api/collections/virtual", s.rommVirtualCollections)
		r.Get("/api/collections/virtual/identifiers", s.rommVirtualIDs)
		r.Get("/api/collections/virtual/{id}", s.rommVirtualCollection)
		r.Get("/api/collections/smart", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, []any{}) })
		r.Get("/api/collections/{id}", s.rommCollection)
		r.Post("/api/collections", s.rommCreateCollection)
		r.Put("/api/collections/{id}", s.rommUpdateCollection)
		r.Delete("/api/collections/{id}", s.rommDeleteCollection)
		r.Post("/api/collections/{id}/roms", s.rommCollectionRoms(true))
		r.Delete("/api/collections/{id}/roms", s.rommCollectionRoms(false))

		r.Get("/api/devices", s.rommDevices)
		r.Post("/api/devices", s.rommRegisterDevice)
		r.Get("/api/devices/{id}", s.rommDevice)
		r.Put("/api/devices/{id}", s.rommDevice)
		r.Post("/api/play-sessions", s.rommPlaySessions)
		r.Get("/api/play-sessions", s.rommListPlaySessions)
		r.Post("/api/activity/heartbeat", s.rommActivity)
		r.Delete("/api/activity/heartbeat", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
		r.Get("/api/raw/assets/*", s.rommRawAsset)
	})
}

// ---------- system ----------

func (s *Server) rommHeartbeat(w http.ResponseWriter, r *http.Request) {
	ps, _ := s.store.ListPlatforms(r.Context())
	fs := []string{}
	for _, p := range ps {
		fs = append(fs, p.Slug)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"SYSTEM": map[string]any{"VERSION": rommVersion, "GIT_BRANCH": nil, "SHOW_SETUP_WIZARD": false},
		"METADATA_SOURCES": map[string]any{"ANY_SOURCE_ENABLED": true, "IGDB_API_ENABLED": false, "SS_API_ENABLED": false, "SS_DEV_CREDENTIALS_SET": false,
			"MOBY_API_ENABLED": false, "STEAMGRIDDB_API_ENABLED": false, "RA_API_ENABLED": false, "LAUNCHBOX_API_ENABLED": false, "HASHEOUS_API_ENABLED": false,
			"PLAYMATCH_API_ENABLED": false, "TGDB_API_ENABLED": false, "FLASHPOINT_API_ENABLED": false, "HLTB_API_ENABLED": false, "DEMOZOO_API_ENABLED": false,
			"POUET_API_ENABLED": false, "CSDB_API_ENABLED": false, "STEAM_API_ENABLED": false, "LIBRETRO_API_ENABLED": true},
		"FILESYSTEM":     map[string]any{"FS_PLATFORMS": fs, "TITLE_ID_EXTRACTION_ENABLED": false},
		"EMULATION":      map[string]any{"DISABLE_EMULATOR_JS": false, "DISABLE_RUFFLE_RS": true, "DISABLE_JSDOS": true, "DISABLE_PICO8": true, "DISABLE_EASYRPG": true},
		"FRONTEND":       map[string]any{"DISABLE_USERPASS_LOGIN": false, "DISABLE_LOGS_VIEWER": true, "YOUTUBE_BASE_URL": "https://www.youtube.com"},
		"OIDC":           map[string]any{"ENABLED": false, "AUTOLOGIN": false, "PROVIDER": "", "RP_INITIATED_LOGOUT": false},
		"NOTIFICATIONS":  map[string]any{"EMAIL_ENABLED": false, "EMAILS_RESET_LINKS": false},
		"DEVICE_INSTALL": map[string]any{"ENABLED": false, "EXCLUDED_PLATFORM_SLUGS": []string{}},
		"CONVERTO":       map[string]any{"ENABLED": false},
		"TASKS": map[string]any{"ENABLE_SCHEDULED_RESCAN": s.cfg.ScanInterval > 0, "SCHEDULED_RESCAN_CRON": "*/15 * * * *",
			"ENABLE_SCHEDULED_UPDATE_SWITCH_TITLEDB": false, "SCHEDULED_UPDATE_SWITCH_TITLEDB_CRON": "0 4 * * *",
			"ENABLE_SCHEDULED_UPDATE_LAUNCHBOX_METADATA": false, "SCHEDULED_UPDATE_LAUNCHBOX_METADATA_CRON": "0 4 * * *",
			"ENABLE_SCHEDULED_CONVERT_IMAGES_TO_WEBP": false, "SCHEDULED_CONVERT_IMAGES_TO_WEBP_CRON": "0 4 * * *"},
	})
}

func (s *Server) rommStats(w http.ResponseWriter, r *http.Request) {
	var games, size, saves, states, shots int64
	_ = s.store.DB.QueryRowContext(r.Context(), `SELECT COUNT(1), COALESCE(SUM(size_bytes),0) FROM games WHERE trashed_at = '' AND missing_since = ''`).Scan(&games, &size)
	_ = s.store.DB.QueryRowContext(r.Context(), `SELECT COALESCE(SUM(kind='save'),0), COALESCE(SUM(kind='state'),0), COALESCE(SUM(kind='screenshot'),0) FROM saves`).Scan(&saves, &states, &shots)
	ps, _ := s.store.ListPlatforms(r.Context())
	n := 0
	for _, p := range ps {
		if p.Games > 0 {
			n++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"PLATFORMS": n, "ROMS": games, "SAVES": saves, "STATES": states, "SCREENSHOTS": shots, "TOTAL_FILESIZE_BYTES": size})
}

func (s *Server) rommConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"CONFIG_FILE_MOUNTED": false, "CONFIG_FILE_WRITABLE": false,
		"EXCLUDED_PLATFORMS": []string{}, "EXCLUDED_SINGLE_EXT": []string{}, "EXCLUDED_SINGLE_FILES": []string{},
		"EXCLUDED_MULTI_FILES": []string{}, "EXCLUDED_MULTI_PARTS_EXT": []string{}, "EXCLUDED_MULTI_PARTS_FILES": []string{},
		"PLATFORMS_BINDING": map[string]string{}, "PLATFORMS_VERSIONS": map[string]string{}, "SKIP_HASH_CALCULATION": false,
		"EJS_DEBUG": false, "EJS_CACHE_LIMIT": nil, "EJS_SETTINGS": map[string]any{}, "EJS_CONTROLS": map[string]any{},
		"SCAN_METADATA_PRIORITY": []string{}, "SCAN_ARTWORK_PRIORITY": []string{}, "SCAN_REGION_PRIORITY": []string{}, "SCAN_LANGUAGE_PRIORITY": []string{},
	})
}

// ---------- sign-in ----------

func (s *Server) rommToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		rommDetail(w, http.StatusBadRequest, "Invalid form")
		return
	}
	var u *store.User
	switch firstNonEmpty(r.PostForm.Get("grant_type"), "password") {
	case "password":
		name, pass := r.PostForm.Get("username"), r.PostForm.Get("password")
		if name == "" || pass == "" {
			rommDetail(w, http.StatusBadRequest, "Missing username or password")
			return
		}
		if u = s.passwordUser(ctx, name, pass); u == nil {
			rommDetail(w, http.StatusUnauthorized, "Invalid username or password")
			return
		}
	case "refresh_token":
		rt := r.PostForm.Get("refresh_token")
		if rt == "" {
			rommDetail(w, http.StatusBadRequest, "Missing refresh token")
			return
		}
		x, _, err := s.store.CompatTokenUser(ctx, rt, "refresh")
		if err != nil {
			rommDetail(w, http.StatusUnauthorized, "Invalid refresh token")
			return
		}
		s.store.DeleteCompatTokenByHash(ctx, rt) // one use only
		u = x
	case "client_credentials":
		rommDetail(w, http.StatusBadRequest, "Client credentials are not yet supported")
		return
	default:
		rommDetail(w, http.StatusBadRequest, "Invalid or unsupported grant type")
		return
	}
	// an empty or old-style scope list gets the person's scopes (RomM would refuse; this only helps)
	scopes := strings.Join(rommScopes(u), " ")
	access, _, err := s.store.CreateCompatToken(ctx, store.CompatToken{UserID: u.ID, Kind: "access", Scopes: scopes}, 30*time.Minute)
	if err != nil {
		s.fail(w, err, "token")
		return
	}
	refresh, _, err := s.store.CreateCompatToken(ctx, store.CompatToken{UserID: u.ID, Kind: "refresh", Scopes: scopes}, 7*24*time.Hour)
	if err != nil {
		s.fail(w, err, "token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"access_token": access, "refresh_token": refresh, "token_type": "bearer", "expires": 1800, "expires_in": 1800, "refresh_expires": 604800})
}

func (s *Server) rommLogin(w http.ResponseWriter, r *http.Request) {
	rc := s.rommUser(r)
	if rc == nil {
		rommDetail(w, http.StatusUnauthorized, "Incorrect username or password")
		return
	}
	id, err := s.store.CreateSession(r.Context(), rc.user.ID, 14*24*time.Hour)
	if err != nil {
		s.fail(w, err, "session")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: rommCookie, Value: id, Path: "/", HttpOnly: true, MaxAge: 1209600, SameSite: http.SameSiteStrictMode, Secure: s.cfg.SecureCookies})
	writeJSON(w, http.StatusOK, nil)
}

func (s *Server) rommLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(rommCookie); err == nil {
		_ = s.store.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: rommCookie, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, nil)
}

func (s *Server) rommUserJSON(u *store.User) map[string]any {
	role := "editor"
	if u.Role == "admin" {
		role = "admin"
	}
	return map[string]any{"id": u.ID, "username": rommUsername(u), "email": u.Email, "enabled": true, "role": role, "permission_group_id": nil,
		"oauth_scopes": rommScopes(u), "avatar_path": "", "last_login": nil, "last_active": nil, "ra_username": nil, "ra_progression": nil,
		"ui_settings": nil, "current_device_id": nil, "created_at": isoT(u.CreatedAt), "updated_at": isoT(u.CreatedAt)}
}

func (s *Server) rommMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.rommUserJSON(rommFrom(r.Context()).user))
}

func (s *Server) rommPermissions(w http.ResponseWriter, r *http.Request) {
	u := rommFrom(r.Context()).user
	var grants []map[string]string
	for _, sc := range rommScopes(u) {
		a, b, _ := strings.Cut(sc, ".")
		grants = append(grants, map[string]string{"scope": a, "action": b})
	}
	hidden := []int64{}
	if rows, err := s.store.DB.QueryContext(r.Context(), `SELECT game_id FROM user_games WHERE user_id = ? AND hidden = 1`, u.ID); err == nil {
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				hidden = append(hidden, id)
			}
		}
		rows.Close()
	}
	writeJSON(w, http.StatusOK, map[string]any{"is_admin": u.Role == "admin", "grants": grants, "hidden": map[string]any{"platforms": []int64{}, "roms": hidden}})
}

// ---------- client tokens, pairing codes, the device flow ----------

type pairCode struct {
	userID, tokenID int64
	expires         time.Time
}

type deviceRequest struct {
	DeviceCode     string   `json:"-"`
	UserCode       string   `json:"user_code"`
	Identifier     string   `json:"client_device_identifier"`
	Name           string   `json:"name"`
	Client         string   `json:"client"`
	Platform       string   `json:"platform"`
	ClientVersion  string   `json:"client_version"`
	Scopes         []string `json:"requested_scopes"`
	Expires        time.Time
	LastPoll       time.Time
	Status         string // pending | approved | denied | done
	UserID         int64
	DeviceName     string
	ApprovedScopes []string
}

var (
	pairMu   sync.Mutex
	pairs    = map[string]*pairCode{}
	devReqs  = map[string]*deviceRequest{} // by user code
	rateMu   sync.Mutex
	rateSeen = map[string][]time.Time{}
)

const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func newCode() string {
	b, _ := hex.DecodeString(store.RandomHex(8))
	out := make([]byte, 8)
	for i := range out {
		out[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
	}
	return string(out)
}

// limited reports a client over n requests per window (by IP and bucket).
func limited(r *http.Request, bucket string, n int, window time.Duration) bool {
	rateMu.Lock()
	defer rateMu.Unlock()
	key := bucket + "|" + r.RemoteAddr
	now := time.Now()
	var keep []time.Time
	for _, t := range rateSeen[key] {
		if now.Sub(t) < window {
			keep = append(keep, t)
		}
	}
	if len(keep) >= n {
		rateSeen[key] = keep
		return true
	}
	rateSeen[key] = append(keep, now)
	return false
}

func clientTokenJSON(t *store.CompatToken, raw string) map[string]any {
	scopes := strings.Fields(t.Scopes)
	if scopes == nil {
		scopes = []string{}
	}
	m := map[string]any{"id": t.ID, "name": t.Name, "scopes": scopes, "expires_at": isoT(t.ExpiresAt), "last_used_at": isoT(t.LastUsedAt),
		"created_at": isoT(t.CreatedAt), "user_id": t.UserID, "device_id": nilIfEmpty(t.DeviceUUID)}
	if raw != "" {
		m["raw_token"] = raw
	}
	return m
}

func (s *Server) rommListClientTokens(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListCompatTokens(r.Context(), rommFrom(r.Context()).user.ID)
	if err != nil {
		s.fail(w, err, "tokens")
		return
	}
	out := []map[string]any{}
	for i := range list {
		out = append(out, clientTokenJSON(&list[i], ""))
	}
	writeJSON(w, http.StatusOK, out)
}

func expiresIn(v string) time.Duration {
	switch v {
	case "30d":
		return 30 * 24 * time.Hour
	case "90d":
		return 90 * 24 * time.Hour
	case "1y":
		return 365 * 24 * time.Hour
	}
	return 0
}

func (s *Server) rommCreateClientToken(w http.ResponseWriter, r *http.Request) {
	u := rommFrom(r.Context()).user
	var in struct {
		Name      string   `json:"name"`
		Scopes    []string `json:"scopes"`
		ExpiresIn string   `json:"expires_in"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		rommDetail(w, http.StatusUnprocessableEntity, "name is required")
		return
	}
	plain, t, err := s.makeClientToken(r.Context(), u, oneLine(in.Name, 80), in.Scopes, expiresIn(in.ExpiresIn), "")
	if err != nil {
		s.fail(w, err, "token")
		return
	}
	writeJSON(w, http.StatusCreated, clientTokenJSON(t, plain))
}

func (s *Server) makeClientToken(ctx context.Context, u *store.User, name string, scopes []string, ttl time.Duration, device string) (string, *store.CompatToken, error) {
	allowed := map[string]bool{}
	for _, sc := range rommScopes(u) {
		allowed[sc] = true
	}
	var keep []string
	for _, sc := range scopes {
		if allowed[sc] {
			keep = append(keep, sc)
		}
	}
	if len(keep) == 0 {
		keep = rommScopes(u)
	}
	return s.store.CreateCompatToken(ctx, store.CompatToken{UserID: u.ID, Kind: "client", Name: name, Scopes: strings.Join(keep, " "), DeviceUUID: device}, ttl)
}

func (s *Server) rommDeleteClientToken(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteCompatToken(r.Context(), rommFrom(r.Context()).user.ID, idParam(r, "id")); err != nil {
		rommDetail(w, http.StatusNotFound, "Token not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) rommRegenerateClientToken(w http.ResponseWriter, r *http.Request) {
	plain, t, err := s.store.RegenerateCompatToken(r.Context(), rommFrom(r.Context()).user.ID, idParam(r, "id"))
	if err != nil {
		rommDetail(w, http.StatusNotFound, "Token not found")
		return
	}
	writeJSON(w, http.StatusOK, clientTokenJSON(t, plain))
}

// rommPairCode makes an 8-letter code (60 s) an app can exchange for the token.
func (s *Server) rommPairCode(w http.ResponseWriter, r *http.Request) {
	u := rommFrom(r.Context()).user
	t, err := s.store.GetCompatToken(r.Context(), u.ID, idParam(r, "id"))
	if err != nil {
		rommDetail(w, http.StatusNotFound, "Token not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": s.newPairCode(u.ID, t.ID, 60*time.Second), "expires_in": 60})
}

func (s *Server) newPairCode(uid, tokenID int64, ttl time.Duration) string {
	code := newCode()
	pairMu.Lock()
	for k, v := range pairs {
		if time.Now().After(v.expires) {
			delete(pairs, k)
		}
	}
	pairs[code] = &pairCode{userID: uid, tokenID: tokenID, expires: time.Now().Add(ttl)}
	pairMu.Unlock()
	return code
}

func (s *Server) rommPairStatus(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.ReplaceAll(chi.URLParam(r, "code"), "-", ""))
	pairMu.Lock()
	p, ok := pairs[code]
	pairMu.Unlock()
	if !ok || time.Now().After(p.expires) {
		rommDetail(w, http.StatusNotFound, "Invalid or expired pairing code")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "pending"})
}

func (s *Server) rommExchange(w http.ResponseWriter, r *http.Request) {
	if limited(r, "exchange", 5, time.Minute) {
		rommDetail(w, http.StatusTooManyRequests, "Too many attempts")
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	_ = decode(r, &in)
	code := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(in.Code), "-", ""))
	pairMu.Lock()
	p, ok := pairs[code]
	delete(pairs, code)
	pairMu.Unlock()
	if !ok || time.Now().After(p.expires) {
		rommDetail(w, http.StatusNotFound, "Invalid or expired pairing code")
		return
	}
	plain, t, err := s.store.RegenerateCompatToken(r.Context(), p.userID, p.tokenID)
	if err != nil {
		rommDetail(w, http.StatusNotFound, "Invalid or expired pairing code")
		return
	}
	writeJSON(w, http.StatusOK, clientTokenJSON(t, plain))
}

func (s *Server) rommDeviceInit(w http.ResponseWriter, r *http.Request) {
	if limited(r, "device-init", 10, time.Minute) {
		rommDetail(w, http.StatusTooManyRequests, "Too many requests")
		return
	}
	var in deviceRequest
	if err := decode(r, &in); err != nil || in.Identifier == "" || in.Name == "" || in.Client == "" || len(in.Scopes) == 0 {
		rommDetail(w, http.StatusUnprocessableEntity, []map[string]any{{"loc": []string{"body"}, "msg": "client_device_identifier, name, client and requested_scopes are required", "type": "value_error"}})
		return
	}
	in.DeviceCode = store.RandomHex(32)
	in.UserCode = newCode()
	in.Expires = time.Now().Add(10 * time.Minute)
	in.Status = "pending"
	pairMu.Lock()
	for k, v := range devReqs {
		if time.Now().After(v.Expires) {
			delete(devReqs, k)
		}
	}
	devReqs[in.UserCode] = &in
	pairMu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]any{"device_code": in.DeviceCode, "user_code": in.UserCode, "verification_path": "/pair/device",
		"verification_path_complete": "/pair/device?user_code=" + in.UserCode, "expires_in": 600, "interval": 5})
}

func (s *Server) rommDeviceToken(w http.ResponseWriter, r *http.Request) {
	if limited(r, "device-token", 60, time.Minute) {
		rommDetail(w, http.StatusBadRequest, "slow_down")
		return
	}
	var in struct {
		DeviceCode string `json:"device_code"`
	}
	_ = decode(r, &in)
	pairMu.Lock()
	var req *deviceRequest
	for _, v := range devReqs {
		if in.DeviceCode != "" && v.DeviceCode == in.DeviceCode {
			req = v
		}
	}
	if req == nil || time.Now().After(req.Expires) {
		pairMu.Unlock()
		rommDetail(w, http.StatusBadRequest, "expired_token")
		return
	}
	if !req.LastPoll.IsZero() && time.Since(req.LastPoll) < 4*time.Second {
		req.LastPoll = time.Now()
		pairMu.Unlock()
		rommDetail(w, http.StatusBadRequest, "slow_down")
		return
	}
	req.LastPoll = time.Now()
	status := req.Status
	if status == "approved" {
		req.Status = "done"
	}
	pairMu.Unlock()
	switch status {
	case "pending":
		rommDetail(w, http.StatusBadRequest, "authorization_pending")
		return
	case "denied":
		rommDetail(w, http.StatusBadRequest, "access_denied")
		return
	case "done":
		rommDetail(w, http.StatusBadRequest, "expired_token")
		return
	}
	ctx := r.Context()
	u, err := s.store.GetUser(ctx, req.UserID)
	if err != nil {
		rommDetail(w, http.StatusBadRequest, "access_denied")
		return
	}
	dev, _, err := s.store.RegisterDevice(ctx, u.ID, store.Device{Identifier: req.Identifier, Name: firstNonEmpty(req.DeviceName, req.Name), Client: req.Client,
		ClientVersion: req.ClientVersion, Platform: req.Platform, IP: r.RemoteAddr})
	if err != nil {
		s.fail(w, err, "device")
		return
	}
	plain, t, err := s.makeClientToken(ctx, u, firstNonEmpty(req.DeviceName, req.Name), req.ApprovedScopes, 0, dev.UUID)
	if err != nil {
		s.fail(w, err, "token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"access_token": plain, "device_id": dev.UUID, "scopes": strings.Fields(t.Scopes), "expires_at": nil})
}

func (s *Server) pendingDevice(code string) *deviceRequest {
	pairMu.Lock()
	defer pairMu.Unlock()
	req, ok := devReqs[strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))]
	if !ok || time.Now().After(req.Expires) {
		return nil
	}
	return req
}

func (s *Server) rommDevicePending(w http.ResponseWriter, r *http.Request) {
	req := s.pendingDevice(chi.URLParam(r, "code"))
	if req == nil {
		rommDetail(w, http.StatusNotFound, "Unknown or expired code")
		return
	}
	if req.Status != "pending" {
		rommDetail(w, http.StatusGone, "Already decided")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"client_device_identifier": req.Identifier, "name": req.Name, "client": req.Client, "platform": req.Platform,
		"client_version": req.ClientVersion, "requested_scopes": req.Scopes, "allowed_scopes": rommScopes(rommFrom(r.Context()).user), "expires_at": req.Expires.UTC().Format(time.RFC3339)})
}

func (s *Server) rommDeviceApprove(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserCode   string   `json:"user_code"`
		Scopes     []string `json:"approved_scopes"`
		DeviceName string   `json:"device_name"`
	}
	_ = decode(r, &in)
	req := s.pendingDevice(in.UserCode)
	if req == nil || req.Status != "pending" {
		rommDetail(w, http.StatusNotFound, "Unknown or expired code")
		return
	}
	pairMu.Lock()
	req.Status, req.UserID, req.DeviceName, req.ApprovedScopes = "approved", rommFrom(r.Context()).user.ID, oneLine(in.DeviceName, 80), in.Scopes
	if len(req.ApprovedScopes) == 0 {
		req.ApprovedScopes = req.Scopes
	}
	pairMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"device_id": nil, "device_name": firstNonEmpty(req.DeviceName, req.Name)})
}

func (s *Server) rommDeviceDeny(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserCode string `json:"user_code"`
	}
	_ = decode(r, &in)
	if req := s.pendingDevice(in.UserCode); req != nil {
		pairMu.Lock()
		req.Status = "denied"
		pairMu.Unlock()
	}
	writeJSON(w, http.StatusOK, nil)
}

// ---------- platforms ----------

func (s *Server) rommPlatformJSON(p store.Platform, fw []store.Firmware) map[string]any {
	k := platforms.Get(p.Slug)
	display := k.Name
	var custom any
	if p.CustomName != "" {
		display, custom = p.CustomName, p.CustomName
	}
	cat := map[string]string{"console": "Console", "handheld": "Portable console", "computer": "Computer", "arcade": "Arcade", "other": "Platform"}[k.Kind]
	fws := []map[string]any{}
	for _, f := range fw {
		if f.PlatformID == p.ID {
			fws = append(fws, s.rommFirmwareJSON(f))
		}
	}
	var igdb any
	if k.IGDB > 0 {
		igdb = k.IGDB
	}
	return map[string]any{
		"id": p.ID, "slug": p.Slug, "fs_slug": p.Slug, "rom_count": p.Games, "name": k.Name, "igdb_slug": p.Slug, "moby_slug": nil, "hltb_slug": nil,
		"libretro_slug": nil, "custom_name": custom, "description": nil, "igdb_id": igdb, "sgdb_id": nil, "moby_id": nil, "launchbox_id": nil, "ss_id": nil,
		"ra_id": nil, "hasheous_id": nil, "tgdb_id": nil, "flashpoint_id": nil, "category": cat, "generation": nil, "family_name": nilIfEmpty(k.Maker),
		"family_slug": nil, "url": nil, "url_logo": nil, "firmware": fws, "created_at": isoT(p.CreatedAt), "updated_at": isoT(p.UpdatedAt),
		"fs_size_bytes": p.SizeBytes, "is_unidentified": !platforms.Known(p.Slug), "is_identified": platforms.Known(p.Slug), "missing_from_fs": false,
		"display_name": display, "abbreviation": k.Short, "alternative_names": []string{}, "firmware_count": len(fws),
	}
}

// rommPlatformList: the platforms that hold games or firmware (not the hundreds of empty folders).
func (s *Server) rommPlatformList(ctx context.Context, after string) []map[string]any {
	ps, _ := s.store.ListPlatforms(ctx)
	fw, _ := s.store.ListFirmware(ctx, 0)
	out := []map[string]any{}
	for _, p := range ps {
		if (p.Games == 0 && p.Firmware == 0) || (after != "" && p.UpdatedAt <= after) {
			continue
		}
		out = append(out, s.rommPlatformJSON(p, fw))
	}
	return out
}

func (s *Server) rommPlatforms(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.rommPlatformList(r.Context(), utcParam(r.URL.Query().Get("updated_after"))))
}

func (s *Server) rommPlatformIDs(w http.ResponseWriter, r *http.Request) {
	ids := []int64{}
	for _, p := range s.rommPlatformList(r.Context(), "") {
		ids = append(ids, p["id"].(int64))
	}
	writeJSON(w, http.StatusOK, ids)
}

func (s *Server) rommPlatform(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPlatform(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		rommDetail(w, http.StatusNotFound, "Platform not found")
		return
	}
	fw, _ := s.store.ListFirmware(r.Context(), p.ID)
	writeJSON(w, http.StatusOK, s.rommPlatformJSON(*p, fw))
}

// ---------- roms ----------

// romFS is where a game lives, relative to the library folder.
func (s *Server) romFS(g *store.Game) (fsPath, fullPath string) {
	rel, err := filepath.Rel(s.lib.Root, s.platformDir(g.PlatformSlug))
	if err != nil {
		rel = g.PlatformSlug
	}
	fsPath = filepath.ToSlash(rel)
	return fsPath, fsPath + "/" + g.FSName
}

var reTags = regexp.MustCompile(`\s*[\(\[][^\)\]]*[\)\]]`)

func splitList(v string) []string {
	out := []string{}
	for _, x := range strings.Split(v, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Server) coverURL(g *store.Game, size string) string {
	if g.CoverPath == "" {
		return ""
	}
	return fmt.Sprintf("/assets/romm/resources/roms/%d/%d/cover/%s.png?ts=%s", g.PlatformID, g.ID, size, version(g.CoverPath))
}

// rommRomJSON is RomM's RomSchema for a game (detailed adds the person's saves and collections).
func (s *Server) rommRomJSON(ctx context.Context, u *store.User, g *store.Game, files []store.GameFile, withFiles, detailed bool) map[string]any {
	fsPath, fullPath := s.romFS(g)
	ext, noExt := "", g.FSName
	if !g.IsFolder {
		ext = strings.TrimPrefix(filepath.Ext(g.FSName), ".")
		noExt = strings.TrimSuffix(g.FSName, filepath.Ext(g.FSName))
	}
	shots := []string{}
	for i := range g.Shots {
		shots = append(shots, fmt.Sprintf("/assets/romm/resources/roms/%d/%d/screenshots/%d.jpg", g.PlatformID, g.ID, i))
	}
	var release any
	if layout, ok := map[int]string{4: "2006", 7: "2006-01", 10: "2006-01-02"}[len(g.ReleaseDate)]; ok {
		if t, err := time.Parse(layout, g.ReleaseDate); err == nil {
			release = t.UnixMilli()
		}
	}
	pd := platforms.Get(g.PlatformSlug)
	display := pd.Name
	var customName any
	if p, err := s.store.GetPlatform(ctx, g.PlatformSlug); err == nil && p.CustomName != "" {
		display, customName = p.CustomName, p.CustomName
	}
	fileList := []map[string]any{}
	if withFiles {
		for _, f := range files {
			dir, base := path2(f.Rel)
			fp := fsPath
			if g.IsFolder {
				fp = fsPath + "/" + g.FSName
			}
			if dir != "" {
				fp += "/" + dir
			}
			fileList = append(fileList, map[string]any{"id": f.ID, "rom_id": g.ID, "file_name": base, "file_path": fp, "file_size_bytes": f.SizeBytes,
				"full_path": fp + "/" + base, "is_top_level": dir == "", "created_at": isoT(g.CreatedAt), "updated_at": isoT(g.UpdatedAt),
				"last_modified": isoT(f.MTime), "crc_hash": nilIfEmpty(f.CRC32), "md5_hash": nilIfEmpty(f.MD5), "sha1_hash": nilIfEmpty(f.SHA1),
				"ra_hash": nil, "chd_sha1_hash": nil, "archive_members": nil, "category": "game", "track_meta": nil, "doc_meta": nil,
				"file_extension": strings.TrimPrefix(filepath.Ext(base), ".")})
		}
	}
	count := g.FileCount
	m := g.Mine
	completion := 0
	if m.Status == "completed" {
		completion = 100
	}
	romUser := map[string]any{"id": -1, "user_id": u.ID, "rom_id": g.ID, "created_at": isoT(g.CreatedAt), "updated_at": isoT(g.UpdatedAt),
		"last_played": isoT(m.LastPlayedAt), "is_main_sibling": false, "backlogged": m.Status == "backlog", "now_playing": m.Status == "playing",
		"hidden": m.Hidden, "rating": m.Rating, "difficulty": 0, "completion": completion, "status": rommStatus(m.Status), "pinned_media": nil}
	tags := splitList(g.Tags)
	var revision any
	for _, t := range tags {
		if strings.HasPrefix(strings.ToLower(t), "rev ") {
			revision = strings.TrimSpace(t[4:])
		}
	}
	cover, coverBig := s.coverURL(g, "small"), s.coverURL(g, "big")
	var urlCover any
	if coverBig != "" && s.cfg.BaseURL != "" {
		urlCover = s.cfg.BaseURL + coverBig
	}
	out := map[string]any{
		"id": g.ID, "igdb_id": nil, "sgdb_id": nil, "moby_id": nil, "ss_id": nil, "ra_id": nil, "launchbox_id": nil, "hasheous_id": nil, "tgdb_id": nil,
		"hltb_id": nil, "demozoo_id": nil, "pouet_id": nil, "csdb_id": nil, "steam_id": nil, "flashpoint_id": nil, "gamelist_id": nil, "libretro_id": nil,
		"platform_id": g.PlatformID, "platform_slug": g.PlatformSlug, "platform_fs_slug": g.PlatformSlug, "platform_custom_name": customName,
		"platform_display_name": display, "platform_name": display,
		"fs_name": g.FSName, "fs_name_no_tags": strings.TrimSpace(reTags.ReplaceAllString(noExt, "")), "fs_name_no_ext": noExt, "fs_extension": ext,
		"fs_path": fsPath, "fs_size_bytes": g.SizeBytes, "name": g.Name, "name_sort_key": g.SortName, "slug": nil, "summary": nilIfEmpty(g.Summary),
		"alternative_names": []string{}, "youtube_video_id": nil,
		"metadatum": map[string]any{"rom_id": g.ID, "genres": splitList(g.Genres), "franchises": []string{}, "collections": []string{},
			"companies": splitList(g.Developer + ", " + g.Publisher), "publishers": splitList(g.Publisher), "developers": splitList(g.Developer),
			"game_modes": splitList(g.Players), "age_ratings": []string{}, "player_count": g.Players, "first_release_date": release, "average_rating": nil},
		"igdb_metadata": nil, "moby_metadata": nil, "ss_metadata": nil, "launchbox_metadata": nil, "hasheous_metadata": nil, "flashpoint_metadata": nil,
		"hltb_metadata": nil, "demozoo_metadata": nil, "pouet_metadata": nil, "csdb_metadata": nil, "steam_metadata": nil, "gamelist_metadata": nil, "manual_metadata": nil,
		"path_cover_small": cover, "path_cover_large": coverBig, "url_cover": urlCover,
		"has_manual": false, "has_soundtrack": false, "path_manual": nil, "url_manual": nil, "path_video": nil,
		"is_identifying": false, "is_unidentified": g.MetaStatus == "unmatched", "is_identified": g.MetaStatus == "matched" || g.MetaStatus == "partial",
		"revision": revision, "regions": splitList(g.Regions), "languages": splitList(g.Languages), "tags": tags,
		"crc_hash": nilIfEmpty(g.CRC32), "md5_hash": nilIfEmpty(g.MD5), "sha1_hash": nilIfEmpty(g.SHA1), "ra_hash": nil,
		"title_id": nil, "save_target": nil, "save_target_layout": nil,
		"has_simple_single_file": !g.IsFolder && count == 1, "has_nested_single_file": g.IsFolder && count == 1, "has_multiple_files": count > 1,
		"multi": count > 1, "is_easyrpg_game": false,
		"full_path": fullPath, "created_at": isoT(g.CreatedAt), "updated_at": isoT(g.UpdatedAt), "missing_from_fs": g.MissingSince != "",
		"is_physical": false, "has_file_on_disk": g.MissingSince == "" && g.TrashedAt == "", "upc": nil, "has_notes": m.Notes != "",
		"rom_user": romUser, "merged_screenshots": shots, "merged_ra_metadata": nil, "files": fileList,
		"sibling_roms": []any{}, "siblings": []any{},
	}
	if !detailed {
		out["screenshot_path"] = nil
		return out
	}
	saves, _ := s.store.ListSaves(ctx, u.ID, g.ID, "")
	us, st, sh := []map[string]any{}, []map[string]any{}, []map[string]any{}
	for i := range saves {
		j := s.rommAssetJSON(&saves[i], g)
		switch saves[i].Kind {
		case "save":
			us = append(us, j)
		case "state":
			st = append(st, j)
		case "screenshot":
			sh = append(sh, j)
		}
	}
	cols := []map[string]any{}
	ids, _ := s.store.GameCollections(ctx, u.ID, g.ID)
	in := map[int64]bool{}
	for _, id := range ids {
		in[id] = true
	}
	all, _ := s.store.ListCollections(ctx, u.ID)
	for _, c := range all {
		if in[c.ID] {
			cols = append(cols, map[string]any{"id": c.ID, "name": c.Name, "is_smart": false})
		}
	}
	if m.Favorite {
		cols = append(cols, map[string]any{"id": favID(u.ID), "name": "Favorites", "is_smart": false})
	}
	out["user_saves"], out["user_states"], out["user_screenshots"] = us, st, sh
	out["all_user_saves"], out["all_user_states"], out["all_user_screenshots"] = []any{}, []any{}, []any{}
	out["user_collections"], out["all_user_notes"], out["download_formats"] = cols, []any{}, []any{}
	return out
}

func path2(rel string) (dir, base string) {
	i := strings.LastIndex(rel, "/")
	if i < 0 {
		return "", rel
	}
	return rel[:i], rel[i+1:]
}

// rommStatus maps a Pocket Games status to RomM's.
func rommStatus(st string) any {
	switch st {
	case "playing":
		return "incomplete"
	case "finished":
		return "finished"
	case "completed":
		return "completed_100"
	case "dropped":
		return "retired"
	}
	return nil
}

// favID is the id of a person's favourites as a RomM collection.
func favID(uid int64) int64 { return 900000000 + uid }

// romFilter reads RomM's /api/roms query (with the spellings clients use that RomM itself ignores).
func (s *Server) romFilter(r *http.Request) (store.GameFilter, bool, error) {
	q := r.URL.Query()
	u := rommFrom(r.Context()).user
	f := store.GameFilter{UserID: u.ID}
	for _, key := range []string{"platform_ids", "platform_id"} {
		for _, v := range q[key] {
			for _, p := range strings.Split(v, ",") {
				if id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil {
					f.PlatformIDs = append(f.PlatformIDs, id)
				}
			}
		}
	}
	term, _, _ := strings.Cut(firstNonEmpty(q.Get("search_term"), q.Get("search")), "|")
	if t := strings.TrimSpace(term); isHex(t) && (len(t) == 8 || len(t) == 32 || len(t) == 40) {
		f.Hash = t
	} else {
		f.Query = t
	}
	if cid := q.Get("collection_id"); cid != "" {
		id, _ := strconv.ParseInt(cid, 10, 64)
		if id == favID(u.ID) {
			f.Favorite = true
		} else {
			f.CollectionID = id
		}
	}
	if v := q.Get("virtual_collection_id"); v != "" {
		if typ, name := decodeVirtual(v); typ == "genre" {
			f.Genre = name
		} else {
			f.IDs = []int64{-1}
		}
	}
	if q.Get("smart_collection_id") != "" {
		f.IDs = []int64{-1}
	}
	if b := firstNonEmpty(q.Get("favorite"), q.Get("favourite")); b == "true" || b == "1" {
		f.Favorite = true
	}
	if b := q.Get("last_played"); b == "true" || b == "1" {
		f.Played = true
	}
	f.Missing = q.Get("missing") == "true"
	f.UpdatedAfter = utcParam(q.Get("updated_after"))
	switch strings.ToLower(q.Get("order_by")) {
	case "id":
		f.Sort = "id"
	case "created_at":
		f.Sort = "added"
	case "fs_size_bytes":
		f.Sort = "size"
	case "last_played":
		f.Sort = "played"
	case "rating":
		f.Sort = "rating"
	case "first_release_date":
		f.Sort = "year"
	default:
		f.Sort = "name"
	}
	desc := strings.EqualFold(q.Get("order_dir"), "desc")
	switch f.Sort {
	case "added", "size", "played", "rating":
		f.Desc = !desc // the store sorts these newest / biggest first unless Desc
	default:
		f.Desc = desc
	}
	f.Limit = 50
	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 || n > 10000 {
			return f, false, errors.New("limit must be between 1 and 10000")
		}
		f.Limit = n
	}
	f.Offset, _ = strconv.Atoi(q.Get("offset"))
	return f, q.Get("with_files") == "true" || q.Get("with_files") == "1", nil
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && len(s)%2 == 0
}

func (s *Server) rommRoms(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := rommFrom(ctx).user
	f, withFiles, err := s.romFilter(r)
	if err != nil {
		rommDetail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	games, total, err := s.store.ListGames(ctx, f)
	if err != nil {
		s.fail(w, err, "roms")
		return
	}
	items := make([]map[string]any, 0, len(games))
	for i := range games {
		var files []store.GameFile
		if withFiles {
			files, _ = s.store.GameFiles(ctx, games[i].ID)
		}
		items = append(items, s.rommRomJSON(ctx, u, &games[i], files, withFiles, false))
	}
	q := r.URL.Query()
	charIndex := map[string]int{}
	idIndex := []int64{}
	wantChars, wantIDs := q.Get("with_char_index") != "false", q.Get("with_rom_id_index") != "false"
	if wantChars || wantIDs {
		all := f
		all.Limit, all.Offset = 0, 0
		if list, _, err := s.store.ListGames(ctx, all); err == nil {
			for i, g := range list {
				if wantIDs {
					idIndex = append(idIndex, g.ID)
				}
				if wantChars && f.Sort == "name" && g.SortName != "" {
					c := strings.ToUpper(g.SortName[:1])
					if c < "A" || c > "Z" {
						c = "#"
					}
					if _, ok := charIndex[c]; !ok {
						charIndex[c] = i
					}
				}
			}
		}
	}
	fv := map[string]any{}
	for _, k := range []string{"genres", "franchises", "collections", "companies", "publishers", "developers", "game_modes", "age_ratings", "player_counts", "regions", "languages", "tags", "platforms"} {
		fv[k] = []any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": f.Limit, "offset": f.Offset, "char_index": charIndex, "rom_id_index": idIndex, "filter_values": fv})
}

func (s *Server) rommRomIDs(w http.ResponseWriter, r *http.Request) {
	f, _, _ := s.romFilter(r)
	f.Limit, f.Offset = 0, 0
	list, _, err := s.store.ListGames(r.Context(), f)
	if err != nil {
		s.fail(w, err, "roms")
		return
	}
	ids := []int64{}
	for _, g := range list {
		ids = append(ids, g.ID)
	}
	writeJSON(w, http.StatusOK, ids)
}

func (s *Server) rommGame(w http.ResponseWriter, r *http.Request) (*store.Game, []store.GameFile, bool) {
	u := rommFrom(r.Context()).user
	g, err := s.store.GetGame(r.Context(), u.ID, idParam(r, "id"))
	if err != nil || g.TrashedAt != "" || g.Mine.Hidden {
		rommDetail(w, http.StatusNotFound, fmt.Sprintf("ROM with ID %s not found", chi.URLParam(r, "id")))
		return nil, nil, false
	}
	s.decorate(u, g)
	files, _ := s.store.GameFiles(r.Context(), g.ID)
	return g, files, true
}

func (s *Server) rommRom(detailed bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if g, files, ok := s.rommGame(w, r); ok {
			writeJSON(w, http.StatusOK, s.rommRomJSON(r.Context(), rommFrom(r.Context()).user, g, files, true, detailed))
		}
	}
}

func (s *Server) rommByHash(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	h := firstNonEmpty(q.Get("md5_hash"), q.Get("sha1_hash"), q.Get("crc_hash"), q.Get("ra_hash"))
	if h == "" {
		rommDetail(w, http.StatusBadRequest, "At least one hash must be provided")
		return
	}
	u := rommFrom(r.Context()).user
	list, _, _ := s.store.ListGames(r.Context(), store.GameFilter{UserID: u.ID, Hash: h, Limit: 1})
	if len(list) == 0 {
		rommDetail(w, http.StatusNotFound, "No ROM or file found with given hash values")
		return
	}
	files, _ := s.store.GameFiles(r.Context(), list[0].ID)
	writeJSON(w, http.StatusOK, s.rommRomJSON(r.Context(), u, &list[0], files, true, true))
}

// fileByID finds a game file by its id (the id in /roms/{id}/files… is a file id).
func (s *Server) fileByID(ctx context.Context, uid, fid int64) (*store.Game, *store.GameFile, error) {
	var gid int64
	if err := s.store.DB.QueryRowContext(ctx, `SELECT game_id FROM game_files WHERE id = ?`, fid).Scan(&gid); err != nil {
		return nil, nil, store.ErrNotFound
	}
	g, err := s.store.GetGame(ctx, uid, gid)
	if err != nil || g.TrashedAt != "" || g.Mine.Hidden {
		return nil, nil, store.ErrNotFound
	}
	files, _ := s.store.GameFiles(ctx, gid)
	for i := range files {
		if files[i].ID == fid {
			return g, &files[i], nil
		}
	}
	return nil, nil, store.ErrNotFound
}

func (s *Server) rommRomFile(w http.ResponseWriter, r *http.Request) {
	u := rommFrom(r.Context()).user
	g, f, err := s.fileByID(r.Context(), u.ID, idParam(r, "id"))
	if err != nil {
		rommDetail(w, http.StatusNotFound, "File not found")
		return
	}
	j := s.rommRomJSON(r.Context(), u, g, []store.GameFile{*f}, true, false)
	writeJSON(w, http.StatusOK, j["files"].([]map[string]any)[0])
}

func (s *Server) rommFileContent(w http.ResponseWriter, r *http.Request) {
	u := rommFrom(r.Context()).user
	g, f, err := s.fileByID(r.Context(), u.ID, idParam(r, "id"))
	if err != nil {
		rommDetail(w, http.StatusNotFound, "File not found")
		return
	}
	s.sendFile(w, r, g, *f, true)
}

// rommContent: one file as it is (with ranges), several as a stored zip with
// an .m3u playlist, the way RomM sends them.
func (s *Server) rommContent(w http.ResponseWriter, r *http.Request) {
	g, files, ok := s.rommGame(w, r)
	if !ok {
		return
	}
	if g.MissingSince != "" || len(files) == 0 {
		rommDetail(w, http.StatusNotFound, fmt.Sprintf("ROM %d has no file to download", g.ID))
		return
	}
	if ids := r.URL.Query().Get("file_ids"); ids != "" {
		want := map[int64]bool{}
		for _, x := range strings.Split(ids, ",") {
			if n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64); err == nil {
				want[n] = true
			}
		}
		var keep []store.GameFile
		for _, f := range files {
			if want[f.ID] {
				keep = append(keep, f)
			}
		}
		files = keep
	}
	if len(files) == 0 {
		rommDetail(w, http.StatusNotFound, fmt.Sprintf("No files found for ROM %d", g.ID))
		return
	}
	if len(files) == 1 {
		s.sendFile(w, r, g, files[0], true)
		return
	}
	name := chi.URLParam(r, "name")
	if un, err := url.PathUnescape(name); err == nil {
		name = un
	}
	name = firstNonEmpty(name, g.FSName)
	entries := s.zipEntries(g, files, r.URL.Query().Get("hidden_folder") == "true", name)
	zipName := name + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDisposition(zipName))
	if r.Header.Get("Range") != "" || r.Method == http.MethodHead {
		// resumable: build the zip once on disk, then serve it with ranges
		if p, err := s.cachedZip(g, entries, r.Method == http.MethodHead); err == nil && p != "" {
			if fh, err := os.Open(p); err == nil {
				defer fh.Close()
				fi, _ := fh.Stat()
				http.ServeContent(w, r, zipName, fi.ModTime(), fh)
				return
			}
		}
		if r.Method == http.MethodHead {
			return
		}
	}
	if err := writeZip(w, entries); err != nil {
		s.log.Debug("zip stream", "err", err)
	}
}

// zipEntry is one file in a zip we send (path on disk, or bytes for the playlist).
type zipEntry struct {
	Name string
	Path string
	Data []byte
	Size int64
}

var reDiscN = regexp.MustCompile(`(?i)(disc|disk|cd)\s*([0-9]+)`)

// zipEntries lists a game's files for a zip, with an .m3u (RomM's rules)
// when the game has none.
func (s *Server) zipEntries(g *store.Game, files []store.GameFile, hidden bool, name string) []zipEntry {
	prefix := ""
	if hidden {
		prefix = ".hidden/"
	}
	var out []zipEntry
	hasM3U, hasSheet := false, false
	for _, f := range files {
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(f.Rel), "."))
		hasM3U = hasM3U || ext == "m3u"
		hasSheet = hasSheet || ext == "cue" || ext == "gdi" || ext == "ccd" || ext == "mds"
		if p, err := s.gamePath(g, f.Rel); err == nil {
			out = append(out, zipEntry{Name: prefix + f.Rel, Path: p, Size: f.SizeBytes})
		}
	}
	if hasM3U {
		return out
	}
	skip := map[string]bool{"m3u": true, "sbi": true, "sub": true}
	if hasSheet {
		for _, e := range []string{"bin", "raw", "img", "mdf", "wav", "ogg", "flac", "mp3"} {
			skip[e] = true
		}
	}
	var discs []string
	for _, e := range out {
		if !skip[strings.ToLower(strings.TrimPrefix(filepath.Ext(e.Name), "."))] {
			discs = append(discs, e.Name)
		}
	}
	discNo := func(n string) int {
		if m := reDiscN.FindStringSubmatch(n); m != nil {
			d, _ := strconv.Atoi(m[2])
			return d
		}
		return 1 << 20
	}
	sort.SliceStable(discs, func(i, j int) bool {
		if a, b := discNo(discs[i]), discNo(discs[j]); a != b {
			return a < b
		}
		return discs[i] < discs[j]
	})
	if len(discs) > 0 {
		data := []byte(strings.Join(discs, "\n"))
		out = append(out, zipEntry{Name: library.SafeName(name) + ".m3u", Data: data, Size: int64(len(data))})
	}
	return out
}

// cachedZip builds (once) a zip of these entries under DATA_DIR/cache/zips.
// onlyIfThere = do not build, just say whether it exists (HEAD).
func (s *Server) cachedZip(g *store.Game, entries []zipEntry, onlyIfThere bool) (string, error) {
	var sig strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&sig, "%s|%s|%d|%s\n", e.Name, e.Path, e.Size, e.Data)
	}
	dir := filepath.Join(s.cfg.DataDir, "cache", "zips")
	p := filepath.Join(dir, fmt.Sprintf("%d-%s.zip", g.ID, md5hex([]byte(sig.String()))[:16]))
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	if onlyIfThere {
		return "", nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// one cached zip per game: drop older versions
	old, _ := filepath.Glob(filepath.Join(dir, fmt.Sprintf("%d-*.zip", g.ID)))
	for _, o := range old {
		_ = os.Remove(o)
	}
	tmp := p + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	err = writeZip(f, entries)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return p, os.Rename(tmp, p)
}

func (s *Server) rommBulkDownload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := rommFrom(ctx).user
	var entries []zipEntry
	n := 0
	for _, x := range strings.Split(r.URL.Query().Get("rom_ids"), ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			continue
		}
		g, err := s.store.GetGame(ctx, u.ID, id)
		if err != nil || g.TrashedAt != "" || g.MissingSince != "" {
			continue
		}
		n++
		fsPath, full := s.romFS(g)
		files, _ := s.store.GameFiles(ctx, g.ID)
		for _, f := range files {
			if p, err := s.gamePath(g, f.Rel); err == nil {
				name := fsPath + "/" + f.Rel
				if g.IsFolder {
					name = full + "/" + f.Rel
				}
				entries = append(entries, zipEntry{Name: name, Path: p, Size: f.SizeBytes})
			}
		}
	}
	if len(entries) == 0 {
		rommDetail(w, http.StatusNotFound, "No ROMs found")
		return
	}
	name := firstNonEmpty(r.URL.Query().Get("filename"), fmt.Sprintf("%d ROMs.zip", n))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDisposition(name))
	_ = writeZip(w, entries)
}

// rommProps changes the person's relation to a game in RomM's terms.
func (s *Server) rommProps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := rommFrom(ctx).user
	g, _, ok := s.rommGame(w, r)
	if !ok {
		return
	}
	b, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(b, &raw)
	if d, ok := raw["data"]; ok && len(raw) == 1 {
		_ = json.Unmarshal(d, &raw) // Freegosy wraps the fields
	}
	var in struct {
		Backlogged *bool   `json:"backlogged"`
		NowPlaying *bool   `json:"now_playing"`
		Hidden     *bool   `json:"hidden"`
		Rating     *int    `json:"rating"`
		Completion *int    `json:"completion"`
		Status     *string `json:"status"`
	}
	b2, _ := json.Marshal(raw)
	_ = json.Unmarshal(b2, &in)
	q := r.URL.Query()
	if q.Get("update_last_played") == "true" && q.Get("remove_last_played") == "true" {
		rommDetail(w, http.StatusBadRequest, "Cannot update and remove last played at the same time")
		return
	}
	p := store.UserGamePatch{Hidden: in.Hidden, Rating: in.Rating}
	status := g.Mine.Status
	if _, ok := raw["status"]; ok {
		switch {
		case in.Status == nil:
			if status == "finished" || status == "completed" || status == "dropped" {
				status = ""
			}
		case *in.Status == "finished":
			status = "finished"
		case *in.Status == "completed_100":
			status = "completed"
		case *in.Status == "retired" || *in.Status == "never_playing":
			status = "dropped"
		case *in.Status == "incomplete":
			status = "playing"
		}
	}
	if in.NowPlaying != nil {
		if *in.NowPlaying {
			status = "playing"
		} else if status == "playing" {
			status = ""
		}
	}
	if in.Backlogged != nil {
		if *in.Backlogged && status == "" {
			status = "backlog"
		} else if !*in.Backlogged && status == "backlog" {
			status = ""
		}
	}
	if in.Completion != nil && *in.Completion >= 100 {
		status = "completed"
	}
	if status != g.Mine.Status {
		p.Status = &status
	}
	if err := s.store.PatchUserGame(ctx, u.ID, g.ID, p); err != nil {
		s.fail(w, err, "props")
		return
	}
	if q.Get("update_last_played") == "true" {
		_, _ = s.store.DB.ExecContext(ctx, `UPDATE user_games SET last_played_at = ?, updated_at = ? WHERE user_id = ? AND game_id = ?`, store.Now(), store.Now(), u.ID, g.ID)
	}
	if q.Get("remove_last_played") == "true" {
		_, _ = s.store.DB.ExecContext(ctx, `UPDATE user_games SET last_played_at = '', updated_at = ? WHERE user_id = ? AND game_id = ?`, store.Now(), u.ID, g.ID)
	}
	g2, _ := s.store.GetGame(ctx, u.ID, g.ID)
	writeJSON(w, http.StatusOK, s.rommRomJSON(ctx, u, g2, nil, false, false)["rom_user"])
}

// ---------- firmware ----------

func (s *Server) rommFirmwareJSON(f store.Firmware) map[string]any {
	verified := false
	for _, b := range platforms.Get(f.PlatformSlug).Bios {
		verified = verified || (b.MD5 != "" && strings.EqualFold(b.MD5, f.MD5))
	}
	rel, _ := filepath.Rel(s.lib.Root, filepath.Join(s.lib.Bios, f.PlatformSlug))
	noExt := strings.TrimSuffix(f.FileName, filepath.Ext(f.FileName))
	return map[string]any{"id": f.ID, "platform_id": f.PlatformID, "file_name": f.FileName, "file_name_no_tags": strings.TrimSpace(reTags.ReplaceAllString(noExt, "")),
		"file_name_no_ext": noExt, "file_extension": strings.TrimPrefix(filepath.Ext(f.FileName), "."), "file_path": filepath.ToSlash(rel), "file_size_bytes": f.SizeBytes,
		"full_path": filepath.ToSlash(rel) + "/" + f.FileName, "is_verified": verified, "crc_hash": f.CRC32, "md5_hash": f.MD5, "sha1_hash": f.SHA1,
		"missing_from_fs": false, "created_at": isoT(f.CreatedAt), "updated_at": isoT(f.MTime)}
}

func (s *Server) rommFirmwareList(w http.ResponseWriter, r *http.Request) {
	pid, _ := strconv.ParseInt(r.URL.Query().Get("platform_id"), 10, 64)
	list, err := s.store.ListFirmware(r.Context(), pid)
	if err != nil {
		s.fail(w, err, "firmware")
		return
	}
	out := []map[string]any{}
	for _, f := range list {
		out = append(out, s.rommFirmwareJSON(f))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) rommFirmwareIDs(w http.ResponseWriter, r *http.Request) {
	list, _ := s.store.ListFirmware(r.Context(), 0)
	ids := []int64{}
	for _, f := range list {
		ids = append(ids, f.ID)
	}
	writeJSON(w, http.StatusOK, ids)
}

func (s *Server) rommFirmware(w http.ResponseWriter, r *http.Request) {
	f, err := s.store.GetFirmware(r.Context(), idParam(r, "id"))
	if err != nil {
		rommDetail(w, http.StatusNotFound, "Firmware not found")
		return
	}
	writeJSON(w, http.StatusOK, s.rommFirmwareJSON(*f))
}

// ---------- saves, states, screenshots ----------

var (
	assetKind = map[string]string{"saves": "save", "states": "state", "screenshots": "screenshot"}
	assetPart = map[string]string{"saves": "saveFile", "states": "stateFile", "screenshots": "screenshotFile"}
)

func (s *Server) rommAssetJSON(v *store.Save, g *store.Game) map[string]any {
	kind := map[string]string{"save": "saves", "state": "states", "screenshot": "screenshots"}[v.Kind]
	noExt := strings.TrimSuffix(v.FileName, filepath.Ext(v.FileName))
	plat := ""
	if g != nil {
		plat = g.PlatformSlug
	}
	fp := fmt.Sprintf("users/%d/%s/%s/%d", v.UserID, kind, plat, v.GameID)
	if v.Emulator != "" {
		fp += "/" + v.Emulator
	}
	m := map[string]any{"id": v.ID, "rom_id": v.GameID, "user_id": v.UserID, "file_name": v.FileName,
		"file_name_no_tags": strings.TrimSpace(reTags.ReplaceAllString(noExt, "")), "file_name_no_ext": noExt, "file_extension": strings.TrimPrefix(filepath.Ext(v.FileName), "."),
		"file_path": fp, "file_size_bytes": v.SizeBytes, "full_path": fp + "/" + v.FileName,
		"download_path":   fmt.Sprintf("/api/%s/%d/content?timestamp=%s&name=%s", kind, v.ID, urlQ(v.UpdatedAt), urlQ(v.FileName)),
		"missing_from_fs": false, "created_at": isoT(v.CreatedAt), "updated_at": isoT(v.UpdatedAt), "is_favorite": false, "labels": []string{},
		"emulator": nilIfEmpty(v.Emulator), "is_public": false}
	switch v.Kind {
	case "save":
		m["slot"], m["content_hash"], m["origin_device_id"], m["device_syncs"], m["screenshot"] = nilIfEmpty(v.Slot), nilIfEmpty(v.MD5), nil, []any{}, nil
	case "state":
		m["core"], m["screenshot"] = nilIfEmpty(v.Emulator), nil
	case "screenshot":
		m["is_gallery"] = false
	}
	return m
}

func urlQ(s string) string { return strings.ReplaceAll(urlPathEscape(s), "/", "%2F") }

func (s *Server) rommAssets(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		u := rommFrom(ctx).user
		q := r.URL.Query()
		want := map[int64]bool{}
		for _, key := range []string{"rom_id", "rom_ids"} {
			for _, v := range q[key] {
				for _, x := range strings.Split(v, ",") {
					if n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64); err == nil {
						want[n] = true
					}
				}
			}
		}
		pid, _ := strconv.ParseInt(q.Get("platform_id"), 10, 64)
		list, err := s.store.ListSaves(ctx, u.ID, 0, assetKind[kind])
		if err != nil {
			s.fail(w, err, kind)
			return
		}
		out := []map[string]any{}
		games := map[int64]*store.Game{}
		for i := range list {
			v := &list[i]
			if (len(want) > 0 && !want[v.GameID]) || (q.Get("slot") != "" && v.Slot != q.Get("slot")) {
				continue
			}
			g, ok := games[v.GameID]
			if !ok {
				g, _ = s.store.GetGame(ctx, u.ID, v.GameID)
				games[v.GameID] = g
			}
			if pid > 0 && (g == nil || g.PlatformID != pid) {
				continue
			}
			out = append(out, s.rommAssetJSON(v, g))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func (s *Server) rommAssetIDs(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, _ := s.store.ListSaves(r.Context(), rommFrom(r.Context()).user.ID, 0, assetKind[kind])
		ids := []int64{}
		for _, v := range list {
			ids = append(ids, v.ID)
		}
		writeJSON(w, http.StatusOK, ids)
	}
}

func (s *Server) rommAssetFor(w http.ResponseWriter, r *http.Request, kind string) (*store.Save, *store.Game, bool) {
	u := rommFrom(r.Context()).user
	v, err := s.store.GetSave(r.Context(), u.ID, idParam(r, "id"))
	if err != nil || v.Kind != assetKind[kind] {
		rommDetail(w, http.StatusNotFound, "Not found")
		return nil, nil, false
	}
	g, _ := s.store.GetGame(r.Context(), u.ID, v.GameID)
	return v, g, true
}

func (s *Server) rommAsset(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if v, g, ok := s.rommAssetFor(w, r, kind); ok {
			writeJSON(w, http.StatusOK, s.rommAssetJSON(v, g))
		}
	}
}

func (s *Server) rommAssetContent(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, _, ok := s.rommAssetFor(w, r, kind)
		if !ok {
			return
		}
		fh, err := os.Open(s.filePath(v.Path))
		if err != nil {
			rommDetail(w, http.StatusNotFound, "File not found")
			return
		}
		defer fh.Close()
		fi, _ := fh.Stat()
		if kind == "screenshots" {
			w.Header().Set("Content-Type", "image/png")
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", contentDisposition(v.FileName))
		}
		http.ServeContent(w, r, v.FileName, fi.ModTime(), fh)
	}
}

// readAssetUpload reads RomM's multipart upload (saveFile / stateFile / screenshotFile).
func readAssetUpload(r *http.Request, kind string) (data []byte, name string, shot []byte, err error) {
	if err := r.ParseMultipartForm(maxSave); err != nil {
		return nil, "", nil, errors.New("a multipart upload is needed")
	}
	if f, fh, err := r.FormFile(assetPart[kind]); err == nil {
		data, _ = io.ReadAll(io.LimitReader(f, maxSave))
		name = fh.Filename
		f.Close()
	}
	if kind != "screenshots" {
		if f, _, err := r.FormFile("screenshotFile"); err == nil {
			shot, _ = io.ReadAll(io.LimitReader(f, 8<<20))
			f.Close()
		}
	}
	return data, name, shot, nil
}

func (s *Server) rommUploadAsset(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		rc := rommFrom(ctx)
		q := r.URL.Query()
		g, err := s.store.GetGame(ctx, rc.user.ID, int64(atoiQ(q.Get("rom_id"))))
		if err != nil {
			rommDetail(w, http.StatusNotFound, "ROM not found")
			return
		}
		data, name, shot, err := readAssetUpload(r, kind)
		if err != nil || len(data) == 0 {
			rommDetail(w, http.StatusBadRequest, "No file was uploaded")
			return
		}
		v, err := s.storeSave(ctx, rc.user.ID, g, assetKind[kind], library.SafeName(name), q.Get("emulator"), q.Get("slot"), s.deviceName(ctx, rc, r), data, shot)
		if err != nil {
			s.fail(w, err, kind)
			return
		}
		if kind == "saves" {
			_, _ = s.store.DB.ExecContext(ctx, `INSERT INTO user_games (user_id, game_id, last_played_at) VALUES (?,?,?) ON CONFLICT(user_id, game_id) DO UPDATE SET last_played_at = excluded.last_played_at, updated_at = excluded.last_played_at`, rc.user.ID, g.ID, store.Now())
		}
		writeJSON(w, http.StatusOK, s.rommAssetJSON(v, g))
	}
}

func (s *Server) rommUpdateAsset(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		rc := rommFrom(ctx)
		v, g, ok := s.rommAssetFor(w, r, kind)
		if !ok {
			return
		}
		data, _, shot, err := readAssetUpload(r, kind)
		if err != nil {
			// RomM needs multipart; a raw body is taken too
			data, _ = io.ReadAll(io.LimitReader(r.Body, maxSave))
		}
		if len(data) > 0 {
			rel, err := s.saveFile(fmt.Sprintf("saves/%d/%d", rc.user.ID, v.GameID), filepath.Ext(v.FileName), data)
			if err != nil {
				s.fail(w, err, kind)
				return
			}
			s.removeFile(v.Path)
			v.Path, v.SizeBytes, v.MD5 = rel, int64(len(data)), md5hex(data)
		}
		if len(shot) > 0 {
			if rel, err := s.savePicture(fmt.Sprintf("saves/%d/%d", rc.user.ID, v.GameID), shot, 640); err == nil {
				s.removeFile(v.ShotPath)
				v.ShotPath = rel
			}
		}
		v.Device = s.deviceName(ctx, rc, r)
		if err := s.store.PutSave(ctx, v); err != nil {
			s.fail(w, err, kind)
			return
		}
		writeJSON(w, http.StatusOK, s.rommAssetJSON(v, g))
	}
}

func (s *Server) rommDeleteOne(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, _, ok := s.rommAssetFor(w, r, kind)
		if !ok {
			return
		}
		_ = s.store.DeleteSave(r.Context(), v.UserID, v.ID)
		s.removeFile(v.Path)
		s.removeFile(v.ShotPath)
		writeJSON(w, http.StatusOK, nil)
	}
}

func (s *Server) rommDeleteAssets(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		u := rommFrom(ctx).user
		var in map[string][]int64
		_ = decode(r, &in)
		ids := in[kind]
		if len(ids) == 0 {
			rommDetail(w, http.StatusBadRequest, "No "+kind+" were provided")
			return
		}
		for _, id := range ids {
			v, err := s.store.GetSave(ctx, u.ID, id)
			if err != nil || v.Kind != assetKind[kind] {
				rommDetail(w, http.StatusNotFound, fmt.Sprintf("%s with ID %d not found", strings.TrimSuffix(kind, "s"), id))
				return
			}
			_ = s.store.DeleteSave(ctx, u.ID, id)
			s.removeFile(v.Path)
			s.removeFile(v.ShotPath)
		}
		writeJSON(w, http.StatusOK, ids)
	}
}

func (s *Server) rommSaveSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := rommFrom(ctx).user
	gid := int64(atoiQ(r.URL.Query().Get("rom_id")))
	list, _ := s.store.ListSaves(ctx, u.ID, gid, "save")
	g, _ := s.store.GetGame(ctx, u.ID, gid)
	type slot struct {
		Slot   any            `json:"slot"`
		Count  int            `json:"count"`
		Latest map[string]any `json:"latest"`
	}
	by := map[string]*slot{}
	var order []string
	for i := range list {
		key := list[i].Slot
		if _, ok := by[key]; !ok {
			by[key] = &slot{Slot: nilIfEmpty(key), Latest: s.rommAssetJSON(&list[i], g)}
			order = append(order, key)
		}
		by[key].Count++
	}
	slots := []slot{}
	for _, k := range order {
		slots = append(slots, *by[k])
	}
	writeJSON(w, http.StatusOK, map[string]any{"total_count": len(list), "slots": slots})
}

// deviceName names where a save came from: the registered device, else the app.
func (s *Server) deviceName(ctx context.Context, rc *rommCtx, r *http.Request) string {
	if id := firstNonEmpty(r.URL.Query().Get("device_id"), rc.device); id != "" {
		if d, err := s.store.GetDevice(ctx, rc.user.ID, id); err == nil {
			return firstNonEmpty(d.Name, d.Client)
		}
	}
	ua := r.UserAgent()
	if i := strings.IndexAny(ua, "/ ("); i > 0 {
		ua = ua[:i]
	}
	return firstNonEmpty(ua, "device")
}

// ---------- collections ----------

func (s *Server) rommCollectionJSON(ctx context.Context, c store.Collection, u *store.User, fav bool) map[string]any {
	covers := []string{}
	for _, id := range c.Covers {
		if g, err := s.store.GetGame(ctx, u.ID, id); err == nil && g.CoverPath != "" {
			covers = append(covers, s.coverURL(g, "small"))
		}
	}
	var first any
	if len(covers) > 0 {
		first = covers[0]
	}
	ids := c.GameIDs
	if ids == nil {
		ids = []int64{}
	}
	return map[string]any{"id": c.ID, "name": c.Name, "description": c.Description, "rom_ids": ids, "rom_count": len(ids),
		"path_cover_small": first, "path_cover_large": first, "path_covers_small": covers, "path_covers_large": covers,
		"is_public": false, "is_favorite": fav, "is_virtual": false, "is_smart": false, "created_at": isoT(c.CreatedAt), "updated_at": isoT(c.UpdatedAt),
		"url_cover": nil, "user_id": u.ID, "owner_username": rommUsername(u)}
}

// favorites is the person's favourites as a RomM collection.
func (s *Server) favorites(ctx context.Context, u *store.User) store.Collection {
	games, _, _ := s.store.ListGames(ctx, store.GameFilter{UserID: u.ID, Favorite: true, Sort: "name"})
	c := store.Collection{ID: favID(u.ID), Name: "Favorites", GameIDs: []int64{}, CreatedAt: u.CreatedAt, UpdatedAt: u.CreatedAt}
	for _, g := range games {
		c.GameIDs = append(c.GameIDs, g.ID)
		if g.CoverPath != "" && len(c.Covers) < 4 {
			c.Covers = append(c.Covers, g.ID)
		}
	}
	return c
}

func (s *Server) rommCollectionList(ctx context.Context, u *store.User) []map[string]any {
	out := []map[string]any{s.rommCollectionJSON(ctx, s.favorites(ctx, u), u, true)}
	cols, _ := s.store.ListCollections(ctx, u.ID)
	for _, c := range cols {
		out = append(out, s.rommCollectionJSON(ctx, c, u, false))
	}
	return out
}

func (s *Server) rommCollections(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.rommCollectionList(r.Context(), rommFrom(r.Context()).user))
}

func (s *Server) rommCollectionIDs(w http.ResponseWriter, r *http.Request) {
	ids := []int64{}
	for _, c := range s.rommCollectionList(r.Context(), rommFrom(r.Context()).user) {
		ids = append(ids, c["id"].(int64))
	}
	writeJSON(w, http.StatusOK, ids)
}

func (s *Server) rommCollection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := rommFrom(ctx).user
	id := idParam(r, "id")
	if id == favID(u.ID) {
		writeJSON(w, http.StatusOK, s.rommCollectionJSON(ctx, s.favorites(ctx, u), u, true))
		return
	}
	c, err := s.store.GetCollection(ctx, u.ID, id)
	if err != nil {
		rommDetail(w, http.StatusNotFound, "Collection not found")
		return
	}
	writeJSON(w, http.StatusOK, s.rommCollectionJSON(ctx, *c, u, false))
}

func (s *Server) rommCreateCollection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := rommFrom(ctx).user
	_ = r.ParseMultipartForm(8 << 20)
	if r.URL.Query().Get("is_favorite") == "true" {
		writeJSON(w, http.StatusOK, s.rommCollectionJSON(ctx, s.favorites(ctx, u), u, true))
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		rommDetail(w, http.StatusUnprocessableEntity, "name is required")
		return
	}
	cols, _ := s.store.ListCollections(ctx, u.ID)
	for _, c := range cols {
		if strings.EqualFold(c.Name, name) {
			rommDetail(w, http.StatusConflict, "Collection already exists")
			return
		}
	}
	id, err := s.store.CreateCollection(ctx, u.ID, oneLine(name, 120), strings.TrimSpace(r.FormValue("description")))
	if err != nil {
		s.fail(w, err, "collection")
		return
	}
	c, _ := s.store.GetCollection(ctx, u.ID, id)
	writeJSON(w, http.StatusOK, s.rommCollectionJSON(ctx, *c, u, false))
}

// setFavorites makes exactly these games the person's favourites.
func (s *Server) setFavorites(ctx context.Context, u *store.User, ids []int64) {
	t, f := true, false
	want := map[int64]bool{}
	for _, id := range ids {
		want[id] = true
		_ = s.store.PatchUserGame(ctx, u.ID, id, store.UserGamePatch{Favorite: &t})
	}
	for _, id := range s.favorites(ctx, u).GameIDs {
		if !want[id] {
			_ = s.store.PatchUserGame(ctx, u.ID, id, store.UserGamePatch{Favorite: &f})
		}
	}
}

func (s *Server) rommUpdateCollection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := rommFrom(ctx).user
	id := idParam(r, "id")
	_ = r.ParseMultipartForm(8 << 20)
	ids := []int64{}
	if raw := r.FormValue("rom_ids"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &ids); err != nil {
			rommDetail(w, http.StatusUnprocessableEntity, "rom_ids must be a JSON list")
			return
		}
	}
	if id == favID(u.ID) {
		s.setFavorites(ctx, u, ids)
		writeJSON(w, http.StatusOK, s.rommCollectionJSON(ctx, s.favorites(ctx, u), u, true))
		return
	}
	c, err := s.store.GetCollection(ctx, u.ID, id)
	if err != nil {
		rommDetail(w, http.StatusNotFound, "Collection not found")
		return
	}
	desc := c.Description
	if r.MultipartForm != nil {
		if v, ok := r.MultipartForm.Value["description"]; ok && len(v) > 0 {
			desc = v[0]
		}
	}
	_ = s.store.UpdateCollection(ctx, u.ID, id, firstNonEmpty(oneLine(r.FormValue("name"), 120), c.Name), desc)
	_ = s.store.SetCollectionGames(ctx, u.ID, id, ids)
	c, _ = s.store.GetCollection(ctx, u.ID, id)
	writeJSON(w, http.StatusOK, s.rommCollectionJSON(ctx, *c, u, false))
}

func (s *Server) rommDeleteCollection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := rommFrom(ctx).user
	id := idParam(r, "id")
	if id == favID(u.ID) {
		s.setFavorites(ctx, u, nil)
		writeJSON(w, http.StatusOK, nil)
		return
	}
	if err := s.store.DeleteCollection(ctx, u.ID, id); err != nil {
		rommDetail(w, http.StatusNotFound, "Collection not found")
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

func (s *Server) rommCollectionRoms(add bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		u := rommFrom(ctx).user
		id := idParam(r, "id")
		var in struct {
			RomIDs []int64 `json:"rom_ids"`
		}
		_ = decode(r, &in)
		if id == favID(u.ID) {
			for _, gid := range in.RomIDs {
				v := add
				_ = s.store.PatchUserGame(ctx, u.ID, gid, store.UserGamePatch{Favorite: &v})
			}
			writeJSON(w, http.StatusOK, s.rommCollectionJSON(ctx, s.favorites(ctx, u), u, true))
			return
		}
		for _, gid := range in.RomIDs {
			if err := s.store.ToggleCollectionGame(ctx, u.ID, id, gid, add); err != nil {
				rommDetail(w, http.StatusNotFound, "Collection not found")
				return
			}
		}
		c, err := s.store.GetCollection(ctx, u.ID, id)
		if err != nil {
			rommDetail(w, http.StatusNotFound, "Collection not found")
			return
		}
		writeJSON(w, http.StatusOK, s.rommCollectionJSON(ctx, *c, u, false))
	}
}

// virtual collections: one per genre (the only kind this library knows)
func encodeVirtual(typ, name string) string {
	b, _ := json.Marshal(map[string]string{"name": name, "type": typ})
	return base64.URLEncoding.EncodeToString(b)
}

func decodeVirtual(id string) (typ, name string) {
	b, err := base64.URLEncoding.DecodeString(id)
	if err != nil {
		b, _ = base64.RawURLEncoding.DecodeString(id)
	}
	var v map[string]string
	_ = json.Unmarshal(b, &v)
	return v["type"], v["name"]
}

func (s *Server) virtualList(ctx context.Context, u *store.User, typ string) []map[string]any {
	out := []map[string]any{}
	if typ != "genre" && typ != "all" {
		return out
	}
	games, _, _ := s.store.ListGames(ctx, store.GameFilter{UserID: u.ID, Sort: "name"})
	by := map[string][]store.Game{}
	for _, g := range games {
		for _, x := range strings.Split(g.Genres, ",") {
			if x = strings.TrimSpace(x); x != "" {
				by[x] = append(by[x], g)
			}
		}
	}
	names := make([]string, 0, len(by))
	for n := range by {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		ids, covers := []int64{}, []string{}
		for i := range by[n] {
			g := &by[n][i]
			ids = append(ids, g.ID)
			if g.CoverPath != "" && len(covers) < 4 {
				covers = append(covers, s.coverURL(g, "small"))
			}
		}
		out = append(out, map[string]any{"id": encodeVirtual("genre", n), "name": n, "type": "genre", "description": "", "rom_ids": ids, "rom_count": len(ids),
			"path_cover_small": nil, "path_cover_large": nil, "path_covers_small": covers, "path_covers_large": covers, "is_public": false,
			"is_favorite": false, "is_virtual": true, "is_smart": false, "created_at": isoT(u.CreatedAt), "updated_at": isoT(u.CreatedAt)})
	}
	return out
}

func (s *Server) rommVirtualCollections(w http.ResponseWriter, r *http.Request) {
	typ := r.URL.Query().Get("type")
	if typ == "" {
		rommDetail(w, http.StatusUnprocessableEntity, []map[string]any{{"loc": []string{"query", "type"}, "msg": "Field required", "type": "missing"}})
		return
	}
	writeJSON(w, http.StatusOK, s.virtualList(r.Context(), rommFrom(r.Context()).user, typ))
}

func (s *Server) rommVirtualIDs(w http.ResponseWriter, r *http.Request) {
	ids := []string{}
	for _, c := range s.virtualList(r.Context(), rommFrom(r.Context()).user, "all") {
		ids = append(ids, c["id"].(string))
	}
	writeJSON(w, http.StatusOK, ids)
}

func (s *Server) rommVirtualCollection(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	for _, c := range s.virtualList(r.Context(), rommFrom(r.Context()).user, "all") {
		if c["id"] == id {
			writeJSON(w, http.StatusOK, c)
			return
		}
	}
	rommDetail(w, http.StatusNotFound, "Collection not found")
}

// ---------- devices and play sessions ----------

func (s *Server) rommDeviceJSON(d *store.Device, uid int64) map[string]any {
	return map[string]any{"id": d.UUID, "user_id": uid, "name": firstNonEmpty(d.Name, d.Client), "platform": nilIfEmpty(d.Platform), "client": nilIfEmpty(d.Client),
		"client_version": nilIfEmpty(d.ClientVersion), "ip_address": nilIfEmpty(d.IP), "mac_address": nil, "hostname": nilIfEmpty(d.Hostname),
		"client_device_identifier": d.Identifier, "sync_mode": "api", "sync_enabled": true, "sync_config": nil, "capabilities": nil,
		"last_seen": isoT(d.LastSeen), "created_at": isoT(d.FirstSeen), "updated_at": isoT(d.LastSeen)}
}

func (s *Server) rommDevices(w http.ResponseWriter, r *http.Request) {
	u := rommFrom(r.Context()).user
	list, _ := s.store.ListDevices(r.Context(), u.ID)
	out := []map[string]any{}
	for i := range list {
		if !strings.HasPrefix(list[i].Identifier, "ua:") {
			out = append(out, s.rommDeviceJSON(&list[i], u.ID))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) rommRegisterDevice(w http.ResponseWriter, r *http.Request) {
	u := rommFrom(r.Context()).user
	var in struct {
		Name          string `json:"name"`
		Platform      string `json:"platform"`
		Client        string `json:"client"`
		ClientVersion string `json:"client_version"`
		Hostname      string `json:"hostname"`
		MAC           string `json:"mac_address"`
		AllowExisting *bool  `json:"allow_existing"`
	}
	_ = decode(r, &in)
	ident := "dev:" + firstNonEmpty(in.MAC, in.Hostname, in.Name) + ":" + in.Platform + ":" + in.Client
	d, created, err := s.store.RegisterDevice(r.Context(), u.ID, store.Device{Identifier: ident, Name: oneLine(in.Name, 80), Platform: in.Platform,
		Client: in.Client, ClientVersion: in.ClientVersion, Hostname: in.Hostname, IP: r.RemoteAddr})
	if err != nil {
		s.fail(w, err, "device")
		return
	}
	if !created && in.AllowExisting != nil && !*in.AllowExisting {
		rommDetail(w, http.StatusConflict, map[string]any{"error": "device_exists", "message": "A device with these details already exists", "device_id": d.UUID})
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"device_id": d.UUID, "name": d.Name, "created_at": isoT(d.FirstSeen)})
}

func (s *Server) rommDevice(w http.ResponseWriter, r *http.Request) {
	u := rommFrom(r.Context()).user
	d, err := s.store.GetDevice(r.Context(), u.ID, chi.URLParam(r, "id"))
	if err != nil {
		rommDetail(w, http.StatusNotFound, "Device not found")
		return
	}
	if r.Method == http.MethodPut {
		var in struct {
			Name string `json:"name"`
		}
		_ = decode(r, &in)
		if in.Name != "" {
			_, _ = s.store.DB.ExecContext(r.Context(), `UPDATE devices SET name = ? WHERE id = ?`, oneLine(in.Name, 80), d.ID)
			d.Name = in.Name
		}
	}
	writeJSON(w, http.StatusOK, s.rommDeviceJSON(d, u.ID))
}

// rommPlaySessions records time played on a device (each sitting once).
func (s *Server) rommPlaySessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rc := rommFrom(ctx)
	var in struct {
		DeviceID string `json:"device_id"`
		Sessions []struct {
			RomID      int64  `json:"rom_id"`
			StartTime  string `json:"start_time"`
			EndTime    string `json:"end_time"`
			DurationMS int64  `json:"duration_ms"`
		} `json:"sessions"`
	}
	if err := decode(r, &in); err != nil {
		rommDetail(w, http.StatusUnprocessableEntity, "invalid body")
		return
	}
	device := s.deviceName(ctx, &rommCtx{user: rc.user, device: firstNonEmpty(in.DeviceID, rc.device)}, r)
	results := []map[string]any{}
	created, skipped := 0, 0
	for _, se := range in.Sessions {
		start, err := time.Parse(time.RFC3339Nano, se.StartTime)
		if err != nil {
			start, err = time.Parse("2006-01-02T15:04:05", se.StartTime)
		}
		sec := se.DurationMS / 1000
		if sec <= 0 && err == nil {
			if end, e2 := time.Parse(time.RFC3339Nano, se.EndTime); e2 == nil {
				sec = int64(end.Sub(start).Seconds())
			}
		}
		g, gerr := s.store.GetGame(ctx, rc.user.ID, se.RomID)
		n := 0
		if err == nil {
			_ = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM play_sessions WHERE user_id = ? AND game_id = ? AND started_at = ?`, rc.user.ID, se.RomID, start.UTC().Format(time.RFC3339Nano)).Scan(&n)
		}
		if err != nil || gerr != nil || sec <= 0 || n > 0 || s.store.AddPlayTime(ctx, rc.user.ID, g.ID, start, sec, device) != nil {
			skipped++
			results = append(results, map[string]any{"rom_id": se.RomID, "status": "skipped"})
			continue
		}
		created++
		results = append(results, map[string]any{"rom_id": se.RomID, "status": "created"})
	}
	writeJSON(w, http.StatusCreated, map[string]any{"results": results, "created_count": created, "skipped_count": skipped})
}

func (s *Server) rommListPlaySessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := rommFrom(ctx).user
	q := r.URL.Query()
	list, _ := s.store.ListSessions(ctx, u.ID, store.SessionFilter{GameID: int64(atoiQ(q.Get("rom_id"))), Limit: max(atoiQ(q.Get("limit")), 50)})
	out := []map[string]any{}
	for _, se := range list {
		out = append(out, map[string]any{"id": se.ID, "rom_id": se.GameID, "device_id": nil, "start_time": isoT(se.StartedAt), "end_time": isoT(se.EndedAt),
			"duration_ms": se.Seconds * 1000, "save_slot": nil, "created_at": isoT(se.StartedAt)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) rommActivity(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RomID int64 `json:"rom_id"`
	}
	_ = decode(r, &in)
	writeJSON(w, http.StatusOK, map[string]any{"rom_id": in.RomID, "user_id": rommFrom(r.Context()).user.ID, "last_seen": time.Now().UTC().Format(time.RFC3339)})
}

// ---------- pictures ----------

// rommCover serves covers the way RomM's nginx does: no sign-in, cacheable.
func (s *Server) rommCover(w http.ResponseWriter, r *http.Request) {
	g, err := s.store.GetGame(r.Context(), 0, idParam(r, "rid"))
	if err != nil || g.PlatformID != idParam(r, "pid") || g.CoverPath == "" {
		http.NotFound(w, r)
		return
	}
	s.servePicture(w, r, g.CoverPath)
}

func (s *Server) rommShot(w http.ResponseWriter, r *http.Request) {
	g, err := s.store.GetGame(r.Context(), 0, idParam(r, "rid"))
	n, _ := strconv.Atoi(strings.TrimSuffix(chi.URLParam(r, "n"), filepath.Ext(chi.URLParam(r, "n"))))
	if err != nil || g.PlatformID != idParam(r, "pid") || n < 0 || n >= len(g.Shots) {
		http.NotFound(w, r)
		return
	}
	s.servePicture(w, r, g.Shots[n])
}

// rommRawAsset: older apps read saves through /api/raw/assets/users/<uid>/<kind>/<platform>/<rom>[/<emulator>]/<file>.
func (s *Server) rommRawAsset(w http.ResponseWriter, r *http.Request) {
	u := rommFrom(r.Context()).user
	parts := strings.Split(chi.URLParam(r, "*"), "/")
	if len(parts) < 6 || parts[0] != "users" || parts[1] != strconv.FormatInt(u.ID, 10) {
		rommDetail(w, http.StatusNotFound, "Not found")
		return
	}
	gid, _ := strconv.ParseInt(parts[4], 10, 64)
	name := parts[len(parts)-1]
	list, _ := s.store.ListSaves(r.Context(), u.ID, gid, assetKind[parts[2]])
	for _, v := range list {
		if v.FileName != name {
			continue
		}
		fh, err := os.Open(s.filePath(v.Path))
		if err != nil {
			break
		}
		defer fh.Close()
		fi, _ := fh.Stat()
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeContent(w, r, v.FileName, fi.ModTime(), fh)
		return
	}
	rommDetail(w, http.StatusNotFound, "Not found")
}

// ---------- Pocket Games' own page for devices (Settings → Devices) ----------

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	list, err := s.store.ListDevices(ctx, u.ID)
	if err != nil {
		s.fail(w, err, "devices")
		return
	}
	tokens, _ := s.store.ListCompatTokens(ctx, u.ID)
	writeJSON(w, http.StatusOK, map[string]any{"devices": list, "tokens": tokens, "username": rommUsername(u), "enabled": s.cfg.RommCompat, "url": s.cfg.BaseURL})
}

func (s *Server) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteDevice(r.Context(), userFrom(r.Context()).ID, idParam(r, "id")); err != nil {
		s.fail(w, err, "device")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleConnectDevice makes a token for an app and a pairing code for it
// (good for 10 minutes): type the code into the app instead of a password.
func (s *Server) handleConnectDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	var in struct {
		Name string `json:"name"`
	}
	_ = decode(r, &in)
	_, t, err := s.makeClientToken(ctx, u, firstNonEmpty(oneLine(in.Name, 80), "Handheld"), nil, 0, "")
	if err != nil {
		s.fail(w, err, "token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": s.newPairCode(u.ID, t.ID, 10*time.Minute), "expires_in": 600, "token": clientTokenJSON(t, "")})
}

// handleRevealToken gives a token a new secret and shows it once (for apps that take a token).
func (s *Server) handleRevealToken(w http.ResponseWriter, r *http.Request) {
	plain, t, err := s.store.RegenerateCompatToken(r.Context(), userFrom(r.Context()).ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "token")
		return
	}
	writeJSON(w, http.StatusOK, clientTokenJSON(t, plain))
}

func (s *Server) handleDeleteClientToken(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteCompatToken(r.Context(), userFrom(r.Context()).ID, idParam(r, "id")); err != nil {
		s.fail(w, err, "token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pair page (the device flow): what the app asked for, and the answer.
func (s *Server) handlePendingDevice(w http.ResponseWriter, r *http.Request) {
	req := s.pendingDevice(chi.URLParam(r, "code"))
	if req == nil || req.Status != "pending" {
		writeErr(w, http.StatusNotFound, "this code is unknown or expired — start the sign-in on the device again")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": req.Name, "client": req.Client, "platform": req.Platform, "client_version": req.ClientVersion, "expires_at": req.Expires.UTC().Format(time.RFC3339)})
}

func (s *Server) handleDecideDevice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code    string `json:"code"`
		Approve bool   `json:"approve"`
		Name    string `json:"name"`
	}
	_ = decode(r, &in)
	req := s.pendingDevice(in.Code)
	if req == nil || req.Status != "pending" {
		writeErr(w, http.StatusNotFound, "this code is unknown or expired — start the sign-in on the device again")
		return
	}
	pairMu.Lock()
	if in.Approve {
		req.Status, req.UserID, req.DeviceName, req.ApprovedScopes = "approved", userFrom(r.Context()).ID, oneLine(in.Name, 80), req.Scopes
	} else {
		req.Status = "denied"
	}
	pairMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- helpers ----------

// isoT turns a stored time into RomM's isoformat ("…+00:00"); ” → null.
func isoT(v string) any {
	if v == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC().Format("2006-01-02T15:04:05.999999") + "+00:00"
		}
	}
	return v
}

// utcParam turns an ISO time from a query into the stored format (” if unreadable).
func utcParam(v string) string {
	if v == "" {
		return ""
	}
	v = strings.ReplaceAll(v, " ", "+")
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999", "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC().Format(time.RFC3339Nano)
		}
	}
	return ""
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func atoiQ(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
