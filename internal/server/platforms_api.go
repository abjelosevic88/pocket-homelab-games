package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-games/internal/platforms"
	"github.com/abjelosevic88/pocket-homelab-games/internal/store"
)

// platformView is a platform folder with what the app knows about the system.
type platformView struct {
	store.Platform
	Name      string             `json:"name"`
	Short     string             `json:"short"`
	Maker     string             `json:"maker"`
	Year      int                `json:"year"`
	Kind      string             `json:"kind"`
	Known     bool               `json:"known"`
	Browser   bool               `json:"browser"`
	Disc      bool               `json:"disc"`
	Ext       []string           `json:"extensions"`
	Cores     []string           `json:"cores"`
	NeedsBios bool               `json:"needs_bios"`
	Bios      map[string]any     `json:"bios"`
	Covers    []int64            `json:"covers"` // a few games with covers, for the tile
	Firmware  []store.Firmware   `json:"firmware_files,omitempty"`
	Catalog   platforms.Platform `json:"-"`
}

func (s *Server) platformView(ctx context.Context, p store.Platform) platformView {
	k := platforms.Get(p.Slug)
	v := platformView{Platform: p, Name: k.Name, Short: k.Short, Maker: k.Maker, Year: k.Year, Kind: k.Kind, Known: platforms.Known(p.Slug),
		Browser: k.Browser(), Disc: k.Disc, Ext: k.Ext, Cores: k.Cores, NeedsBios: k.NeedsBios(), Catalog: k}
	if p.CustomName != "" {
		v.Name = p.CustomName
	}
	if v.Ext == nil {
		v.Ext = []string{}
	}
	if v.Cores == nil {
		v.Cores = []string{}
	}
	v.Bios = s.biosCheck(ctx, p.Slug)
	return v
}

// biosCheck says which of the system's BIOS files are there (matched by
// checksum when a good dump is known, else by name).
func (s *Server) biosCheck(ctx context.Context, slug string) map[string]any {
	k := platforms.Get(slug)
	if len(k.Bios) == 0 {
		return nil
	}
	var files []store.Firmware
	if p, err := s.store.GetPlatform(ctx, slug); err == nil {
		files, _ = s.store.ListFirmware(ctx, p.ID)
	}
	type item struct {
		platforms.Bios
		Present  bool   `json:"present"`
		Verified bool   `json:"verified"` // the checksum matches a known good dump
		FileID   int64  `json:"file_id"`
		Have     string `json:"have"` // the name of the file that counts
	}
	var items []item
	ok := true
	anyPresent := false
	for _, b := range k.Bios {
		it := item{Bios: b}
		for _, f := range files {
			switch {
			case b.MD5 != "" && strings.EqualFold(f.MD5, b.MD5):
				it.Present, it.Verified, it.FileID, it.Have = true, true, f.ID, f.FileName
			case strings.EqualFold(f.FileName, b.File) && !it.Present:
				it.Present, it.FileID, it.Have = true, f.ID, f.FileName
			case b.Any && !it.Present:
				it.Present, it.FileID, it.Have = true, f.ID, f.FileName
			}
		}
		if it.Present {
			anyPresent = true
		}
		items = append(items, it)
	}
	// "one of these" sets (several regions of the same BIOS): the system is
	// ready when at least one is there, unless one of them is required
	for _, it := range items {
		if it.Required && !it.Present {
			ok = false
		}
	}
	if !anyPresent && k.NeedsBios() {
		ok = false
	}
	return map[string]any{"ok": ok, "items": items, "files": len(files)}
}

func (s *Server) handleListPlatforms(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ps, err := s.store.ListPlatforms(ctx)
	if err != nil {
		s.fail(w, err, "platforms")
		return
	}
	all := queryBool(r, "all") // the empty folders too
	out := []platformView{}
	for _, p := range ps {
		if p.Games == 0 && p.Firmware == 0 && !all {
			continue
		}
		v := s.platformView(ctx, p)
		v.Covers, _ = s.coverSample(ctx, p.ID, 4)
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) coverSample(ctx context.Context, platformID int64, n int) ([]int64, error) {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id FROM games WHERE platform_id = ? AND cover_path != '' AND trashed_at = '' AND missing_since = '' ORDER BY RANDOM() LIMIT ?`, platformID, n)
	if err != nil {
		return []int64{}, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

func (s *Server) handleGetPlatform(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, err := s.store.GetPlatform(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, err, "platform")
		return
	}
	v := s.platformView(ctx, *p)
	v.Firmware, _ = s.store.ListFirmware(ctx, p.ID)
	writeJSON(w, http.StatusOK, v)
}

var reSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,48}$`)

// handleCreatePlatform makes a platform folder (admin).
func (s *Server) handleCreatePlatform(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var in struct {
		Slug string `json:"slug"`
	}
	if err := decode(r, &in); err != nil || !reSlug.MatchString(in.Slug) {
		writeErr(w, http.StatusBadRequest, "a folder name of lower-case letters, digits and dashes (e.g. snes)")
		return
	}
	if err := os.MkdirAll(s.platformDir(in.Slug), 0o755); err != nil {
		s.fail(w, err, "create folder")
		return
	}
	id, err := s.store.EnsurePlatform(r.Context(), in.Slug)
	if err != nil {
		s.fail(w, err, "platform")
		return
	}
	p, _ := s.store.GetPlatform(r.Context(), in.Slug)
	if p == nil {
		p = &store.Platform{ID: id, Slug: in.Slug}
	}
	writeJSON(w, http.StatusCreated, s.platformView(r.Context(), *p))
}

func (s *Server) handleUpdatePlatform(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	p, err := s.store.GetPlatform(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, err, "platform")
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == platforms.Get(p.Slug).Name {
		name = ""
	}
	if err := s.store.RenamePlatform(r.Context(), p.ID, name); err != nil {
		s.fail(w, err, "save")
		return
	}
	p, _ = s.store.GetPlatform(r.Context(), p.Slug)
	writeJSON(w, http.StatusOK, s.platformView(r.Context(), *p))
}

// handleCatalog lists every system the app knows (for uploads and new folders).
func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	type entry struct {
		platforms.Platform
		Browser   bool `json:"browser"`
		NeedsBios bool `json:"needs_bios"`
	}
	var out []entry
	for _, p := range platforms.All() {
		out = append(out, entry{Platform: p, Browser: p.Browser(), NeedsBios: p.NeedsBios()})
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------- firmware ----------

func (s *Server) handleFirmwareFile(w http.ResponseWriter, r *http.Request) {
	f, err := s.store.GetFirmware(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "firmware")
		return
	}
	p := s.firmwarePath(f.PlatformSlug, f.FileName)
	fh, err := os.Open(p)
	if err != nil {
		writeErr(w, http.StatusGone, "the file is no longer there")
		return
	}
	defer fh.Close()
	fi, _ := fh.Stat()
	w.Header().Set("Content-Type", "application/octet-stream")
	if chi.URLParam(r, "name") == "" {
		w.Header().Set("Content-Disposition", contentDisposition(f.FileName))
	}
	http.ServeContent(w, r, f.FileName, fi.ModTime(), fh)
}

func (s *Server) handleDeleteFirmware(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	f, err := s.store.GetFirmware(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "firmware")
		return
	}
	// into the trash folder too: a BIOS can be hard to find again
	dst := filepath.Join(s.lib.Trash, "bios", f.PlatformSlug)
	_ = os.MkdirAll(dst, 0o755)
	if err := os.Rename(s.firmwarePath(f.PlatformSlug, f.FileName), filepath.Join(dst, f.FileName)); err != nil && !os.IsNotExist(err) {
		s.fail(w, err, "delete")
		return
	}
	s.scanFirmware(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

// ---------- the library as a whole ----------

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	s.wakeScan()
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

// handleLibraryStatus: the last scan, where things are, and what needs a look.
func (s *Server) handleLibraryStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	var pending, unmatched, unhashed int
	_ = s.store.DB.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN meta_status = 'pending' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN meta_status = 'unmatched' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN hashed_at = '' THEN 1 ELSE 0 END),0)
		FROM games WHERE trashed_at = '' AND missing_since = ''`).Scan(&pending, &unmatched, &unhashed)
	unknown := []string{}
	ps, _ := s.store.ListPlatforms(ctx)
	for _, p := range ps {
		if !platforms.Known(p.Slug) && p.Games > 0 {
			unknown = append(unknown, p.Slug)
		}
	}
	dups, _ := s.store.Duplicates(ctx)
	var dupGames [][]store.Game
	for _, d := range dups {
		gs, _, _ := s.store.ListGames(ctx, store.GameFilter{UserID: u.ID, IDs: d, Hidden: true})
		s.decorateAll(u, gs)
		dupGames = append(dupGames, gs)
	}
	if dupGames == nil {
		dupGames = [][]store.Game{}
	}
	out := map[string]any{
		"scan": s.scanState(), "interval_minutes": int(s.cfg.ScanInterval.Minutes()), "metadata": s.cfg.Metadata,
		"pending": pending, "unmatched": unmatched, "unhashed": unhashed, "unknown_platforms": unknown, "duplicates": dupGames,
		"hash_max_bytes": s.cfg.HashMaxBytes, "trash_days": s.cfg.TrashDays, "romm_compat": s.cfg.RommCompat,
	}
	if u.Role == "admin" {
		out["paths"] = map[string]string{"library": s.lib.Root, "roms": s.lib.Roms, "bios": s.lib.Bios, "trash": s.lib.Trash}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleLookupAll looks up every game without a match again (admin).
func (s *Server) handleLookupAll(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var in struct {
		All bool `json:"all"` // every game, not only the unmatched ones
	}
	_ = decode(r, &in)
	q := `UPDATE games SET meta_status = 'pending' WHERE meta_locked = 0 AND trashed_at = ''`
	if !in.All {
		q += ` AND meta_status IN ('unmatched', 'partial')`
	}
	res, err := s.store.DB.ExecContext(r.Context(), q)
	if err != nil {
		s.fail(w, err, "lookup")
		return
	}
	n, _ := res.RowsAffected()
	s.wakeWork()
	writeJSON(w, http.StatusOK, map[string]any{"queued": n})
}
