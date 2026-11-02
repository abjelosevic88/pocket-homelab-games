package server

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-games/internal/library"
	"github.com/abjelosevic88/pocket-homelab-games/internal/meta"
	"github.com/abjelosevic88/pocket-homelab-games/internal/platforms"
	"github.com/abjelosevic88/pocket-homelab-games/internal/store"
)

// ---------- shaping ----------

// decorate fills the computed fields of a game for the caller.
func (s *Server) decorate(u *store.User, g *store.Game) {
	p := platforms.Get(g.PlatformSlug)
	g.PlatformName = p.Name
	g.Browser = p.Browser() && g.MissingSince == "" && g.TrashedAt == ""
	g.CanEdit = u != nil && (u.Role == "admin" || (g.AddedBy != nil && *g.AddedBy == u.ID))
	if g.CoverPath != "" {
		g.Cover = fmt.Sprintf("/api/v1/games/%d/cover?v=%s", g.ID, version(g.CoverPath))
	}
	g.ShotURLs = []string{}
	for i, sp := range g.Shots {
		g.ShotURLs = append(g.ShotURLs, fmt.Sprintf("/api/v1/games/%d/shots/%d?v=%s", g.ID, i, version(sp)))
	}
}

// version is a short cache key for a stored file (its random name).
func version(rel string) string {
	b := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	if len(b) > 8 {
		b = b[:8]
	}
	return b
}

func (s *Server) decorateAll(u *store.User, gs []store.Game) {
	for i := range gs {
		s.decorate(u, &gs[i])
	}
}

// browserSlugs lists the platform folders whose games play in the browser.
func (s *Server) browserSlugs(ctx context.Context) []string {
	ps, _ := s.store.ListPlatforms(ctx)
	var out []string
	for _, p := range ps {
		if platforms.Get(p.Slug).Browser() {
			out = append(out, p.Slug)
		}
	}
	if len(out) == 0 {
		out = []string{"-"}
	}
	return out
}

// gameFor loads a game the caller may see (trashed games only on the trash page).
func (s *Server) gameFor(w http.ResponseWriter, r *http.Request) (*store.Game, bool) {
	u := userFrom(r.Context())
	g, err := s.store.GetGame(r.Context(), u.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "game")
		return nil, false
	}
	s.decorate(u, g)
	return g, true
}

// ---------- lists ----------

func (s *Server) filterFrom(r *http.Request) store.GameFilter {
	q := r.URL.Query()
	u := userFrom(r.Context())
	f := store.GameFilter{UserID: u.ID, Query: q.Get("q"), Status: q.Get("status"), Favorite: queryBool(r, "favorite"),
		Played: queryBool(r, "played"), CollectionID: int64(queryInt(r, "collection")), Missing: queryBool(r, "missing"),
		Hidden: queryBool(r, "hidden"), Sort: q.Get("sort"), Desc: queryBool(r, "desc"), Letter: q.Get("letter"),
		Limit: queryInt(r, "limit"), Offset: queryInt(r, "offset")}
	for _, p := range strings.Split(q.Get("platform"), ",") {
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		if pl, err := s.store.GetPlatform(r.Context(), p); err == nil {
			f.PlatformIDs = append(f.PlatformIDs, pl.ID)
		} else {
			f.PlatformIDs = append(f.PlatformIDs, -1)
		}
	}
	if queryBool(r, "browser") {
		f.Browser = s.browserSlugs(r.Context())
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 120
	}
	return f
}

func (s *Server) handleListGames(w http.ResponseWriter, r *http.Request) {
	f := s.filterFrom(r)
	gs, total, err := s.store.ListGames(r.Context(), f)
	if err != nil {
		s.fail(w, err, "list games")
		return
	}
	s.decorateAll(userFrom(r.Context()), gs)
	writeJSON(w, http.StatusOK, map[string]any{"items": gs, "total": total, "limit": f.Limit, "offset": f.Offset})
}

// handleHome is the first page: continue playing, new games, favourites,
// the backlog and the week in numbers.
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	list := func(f store.GameFilter) []store.Game {
		f.UserID = u.ID
		gs, _, _ := s.store.ListGames(ctx, f)
		s.decorateAll(u, gs)
		return gs
	}
	recent := list(store.GameFilter{Played: true, Sort: "played", Limit: 12})
	// the newest state of each recently played game: "continue" shows its picture
	states := map[int64]int64{}
	for _, g := range recent {
		if sv, _ := s.store.ListSaves(ctx, u.ID, g.ID, "state"); len(sv) > 0 && sv[0].HasShot {
			states[g.ID] = sv[0].ID
		}
	}
	today := time.Now()
	from := today.AddDate(0, 0, -6).Format("2006-01-02")
	sec, sessions, days, games, _ := s.store.TotalPlay(ctx, u.ID, from, today.Format("2006-01-02"))
	byDay, _ := s.store.PlayByDay(ctx, u.ID, from, today.Format("2006-01-02"))
	counts, _ := s.store.StatusCounts(ctx, u.ID)
	_, total, _ := s.store.ListGames(ctx, store.GameFilter{UserID: u.ID, Limit: 1})
	writeJSON(w, http.StatusOK, map[string]any{
		"continue":  recent,
		"states":    states,
		"added":     list(store.GameFilter{Sort: "added", Limit: 12}),
		"favorites": list(store.GameFilter{Favorite: true, Sort: "played", Limit: 12}),
		"backlog":   list(store.GameFilter{Status: "backlog", Sort: "added", Limit: 12}),
		"browser":   list(store.GameFilter{Browser: s.browserSlugs(ctx), Sort: "name", Limit: 12}),
		"week":      map[string]any{"seconds": sec, "sessions": sessions, "days": days, "games": games, "by_day": byDay},
		"counts":    counts,
		"total":     total,
		"scan":      s.scanState(),
	})
}

// handleDashboard is the summary for the sidebar counts and a Homepage widget.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	_, total, _ := s.store.ListGames(ctx, store.GameFilter{UserID: u.ID, Limit: 1})
	_, browser, _ := s.store.ListGames(ctx, store.GameFilter{UserID: u.ID, Browser: s.browserSlugs(ctx), Limit: 1})
	_, trash, _ := s.store.ListGames(ctx, store.GameFilter{UserID: u.ID, Trashed: true, Hidden: true, Limit: 1})
	counts, _ := s.store.StatusCounts(ctx, u.ID)
	cols, _ := s.store.ListCollections(ctx, u.ID)
	ps, _ := s.store.ListPlatforms(ctx)
	np := 0
	for _, p := range ps {
		if p.Games > 0 {
			np++
		}
	}
	today := time.Now()
	sec, _, _, _, _ := s.store.TotalPlay(ctx, u.ID, today.AddDate(0, 0, -6).Format("2006-01-02"), today.Format("2006-01-02"))
	writeJSON(w, http.StatusOK, map[string]any{
		"games": total, "platforms": np, "browser": browser, "trash": trash,
		"favorites": counts["favorites"], "playing": counts["playing"], "backlog": counts["backlog"], "finished": counts["finished"] + counts["completed"],
		"collections": len(cols), "minutes_week": sec / 60, "scanning": s.scanState().Running,
	})
}

// ---------- one game ----------

func (s *Server) handleGetGame(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	g, ok := s.gameFor(w, r)
	if !ok {
		return
	}
	files, _ := s.store.GameFiles(ctx, g.ID)
	cols, _ := s.store.GameCollections(ctx, u.ID, g.ID)
	saves, _ := s.store.ListSaves(ctx, u.ID, g.ID, "")
	sessions, _ := s.store.ListSessions(ctx, u.ID, store.SessionFilter{GameID: g.ID, Limit: 20})
	p := platforms.Get(g.PlatformSlug)
	var addedBy string
	if g.AddedBy != nil {
		if a, err := s.store.GetUser(ctx, *g.AddedBy); err == nil {
			addedBy = firstNonEmpty(a.Name, a.Email)
		}
	}
	// other copies of the same game (another region, the same file twice)
	var others []store.Game
	if g.MD5 != "" || g.Name != "" {
		all, _, _ := s.store.ListGames(ctx, store.GameFilter{UserID: u.ID, Query: g.Name, Limit: 10})
		for _, o := range all {
			if o.ID != g.ID && (library.Normalize(o.Name) == library.Normalize(g.Name) || (g.MD5 != "" && o.MD5 == g.MD5)) {
				others = append(others, o)
			}
		}
		s.decorateAll(u, others)
	}
	if others == nil {
		others = []store.Game{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"game": g, "files": files, "collections": cols, "saves": saves, "sessions": sessions,
		"platform": struct {
			platforms.Platform
			Browser   bool `json:"browser"`
			NeedsBios bool `json:"needs_bios"`
		}{p, p.Browser(), p.NeedsBios()}, "bios": s.biosCheck(ctx, g.PlatformSlug), "added_by": addedBy, "others": others,
		"play": s.playInfo(ctx, g, files),
	})
}

// playInfo says whether and how the game plays in the browser.
func (s *Server) playInfo(ctx context.Context, g *store.Game, files []store.GameFile) map[string]any {
	p := platforms.Get(g.PlatformSlug)
	out := map[string]any{"browser": g.Browser, "system": p.System, "cores": p.Cores, "threads": p.Threads}
	switch {
	case g.TrashedAt != "":
		out["why"] = "This game is in the trash."
	case g.MissingSince != "":
		out["why"] = "The game's files are no longer in the library."
	case !p.Browser():
		out["why"] = fmt.Sprintf("%s games cannot be played in a browser yet. Download the game and play it on a device or computer.", p.Short)
	}
	if b := s.biosCheck(ctx, g.PlatformSlug); b != nil {
		if ok, _ := b["ok"].(bool); !ok && p.Browser() {
			out["bios_missing"] = true
		}
	}
	return out
}

func (s *Server) canEdit(w http.ResponseWriter, g *store.Game) bool {
	if !g.CanEdit {
		writeErr(w, http.StatusForbidden, "only an admin or the person who added this game can change it")
		return false
	}
	return true
}

// handlePutMine changes the caller's own relation to a game.
func (s *Server) handlePutMine(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	g, ok := s.gameFor(w, r)
	if !ok {
		return
	}
	var p store.UserGamePatch
	if err := decode(r, &p); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p.LastPlayedAt = nil
	if p.Status != nil && !store.ValidStatus(*p.Status) {
		writeErr(w, http.StatusBadRequest, "unknown status")
		return
	}
	if p.Notes != nil && len(*p.Notes) > 20000 {
		writeErr(w, http.StatusBadRequest, "notes are too long")
		return
	}
	if p.Core != nil && *p.Core != "" {
		okCore := false
		for _, c := range platforms.Get(g.PlatformSlug).Cores {
			okCore = okCore || c == *p.Core
		}
		if !okCore {
			writeErr(w, http.StatusBadRequest, "unknown emulator for this system")
			return
		}
	}
	before := g.Mine.Status
	if err := s.store.PatchUserGame(ctx, u.ID, g.ID, p); err != nil {
		s.fail(w, err, "save")
		return
	}
	if p.Status != nil && *p.Status != before && *p.Status != "" {
		s.publish(ctx, u.ID, "games.status_changed", g.Name, map[string]any{"game_id": g.ID, "name": g.Name, "platform": g.PlatformSlug,
			"platform_name": g.PlatformName, "status": *p.Status, "previous": before, "url": s.cfg.BaseURL + fmt.Sprintf("/games/%d", g.ID)})
	}
	g2, _ := s.store.GetGame(ctx, u.ID, g.ID)
	s.decorate(u, g2)
	writeJSON(w, http.StatusOK, g2)
}

// handleUpdateGame edits what is known about a game (an admin or whoever
// added it). Edited facts are locked: lookups never overwrite them.
func (s *Server) handleUpdateGame(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, ok := s.gameFor(w, r)
	if !ok || !s.canEdit(w, g) {
		return
	}
	var m store.GameMeta
	if err := decode(r, &m); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	m.MetaStatus, m.SummaryURL, m.WikiTitle = nil, nil, nil
	if m.Name != nil && strings.TrimSpace(*m.Name) == "" {
		writeErr(w, http.StatusBadRequest, "the name cannot be empty")
		return
	}
	if m.ReleaseDate != nil && *m.ReleaseDate != "" && !validLooseDate(*m.ReleaseDate) {
		writeErr(w, http.StatusBadRequest, "release date: YYYY, YYYY-MM or YYYY-MM-DD")
		return
	}
	if m.Summary != nil {
		src := "manual"
		m.SummarySource = &src
		empty := ""
		m.SummaryURL = &empty
	}
	if m.MetaLocked == nil {
		t := true
		m.MetaLocked = &t
	}
	if err := s.store.UpdateGameMeta(ctx, g.ID, m); err != nil {
		s.fail(w, err, "save")
		return
	}
	g2, _ := s.store.GetGame(ctx, userFrom(ctx).ID, g.ID)
	s.decorate(userFrom(ctx), g2)
	writeJSON(w, http.StatusOK, g2)
}

func validLooseDate(d string) bool {
	for _, layout := range []string{"2006", "2006-01", "2006-01-02"} {
		if _, err := time.Parse(layout, d); err == nil && len(d) == len(layout) {
			return true
		}
	}
	return false
}

// handleLookup looks the game up again now (replacing the cover too when asked).
func (s *Server) handleLookup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, ok := s.gameFor(w, r)
	if !ok || !s.canEdit(w, g) {
		return
	}
	var in struct {
		Article string `json:"article"` // an exact Wikipedia title
		Cover   bool   `json:"cover"`
	}
	_ = decode(r, &in)
	if strings.TrimSpace(in.Article) != "" {
		f, err := s.meta.Article(ctx, strings.TrimSpace(strings.TrimPrefix(in.Article, "https://en.wikipedia.org/wiki/")))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "no Wikipedia article about a video game with that title")
			return
		}
		f2 := false
		_ = s.store.UpdateGameMeta(ctx, g.ID, store.GameMeta{MetaLocked: &f2})
		g.Summary, g.Genres, g.Developer, g.Publisher, g.ReleaseDate, g.Players = "", "", "", "", "", ""
		s.applyFacts(ctx, g, f, true)
		st := "matched"
		_ = s.store.UpdateGameMeta(ctx, g.ID, store.GameMeta{MetaStatus: &st})
	} else {
		if g.MetaLocked {
			f := false
			_ = s.store.UpdateGameMeta(ctx, g.ID, store.GameMeta{MetaLocked: &f})
		}
		s.lookup(ctx, g.ID, in.Cover)
	}
	g2, _ := s.store.GetGame(ctx, userFrom(ctx).ID, g.ID)
	s.decorate(userFrom(ctx), g2)
	writeJSON(w, http.StatusOK, g2)
}

// handleCoverCandidates lists box art that may fit (the best matches, or a search).
func (s *Server) handleCoverCandidates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, ok := s.gameFor(w, r)
	if !ok {
		return
	}
	p := platforms.Get(g.PlatformSlug)
	if p.Libretro == "" {
		writeJSON(w, http.StatusOK, []meta.Candidate{})
		return
	}
	var list []meta.Candidate
	var err error
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		list, err = s.meta.Search(ctx, p.Libretro, meta.Boxart, q, 24)
	} else {
		list, err = s.meta.Match(ctx, p.Libretro, meta.Boxart, g.FSName, g.Name, s.preferredRegions(ctx), 24)
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, "the cover collection could not be reached: "+err.Error())
		return
	}
	if list == nil {
		list = []meta.Candidate{}
	}
	writeJSON(w, http.StatusOK, list)
}

// handleSetCover takes a picture (the request body) or {"url": …} from the candidates.
func (s *Server) handleSetCover(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, ok := s.gameFor(w, r)
	if !ok || !s.canEdit(w, g) {
		return
	}
	var err error
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var in struct {
			URL string `json:"url"`
		}
		if err := decode(r, &in); err != nil || in.URL == "" {
			writeErr(w, http.StatusBadRequest, "a picture or its address is needed")
			return
		}
		pu, perr := url.Parse(in.URL)
		if perr != nil || pu.Scheme != "https" {
			writeErr(w, http.StatusBadRequest, "the picture's address must start with https://")
			return
		}
		src := "web"
		if pu.Host == "thumbnails.libretro.com" {
			src = "libretro"
		}
		err = s.setCoverFromURL(ctx, g, in.URL, src)
	} else {
		b, rerr := io.ReadAll(http.MaxBytesReader(w, r.Body, 20<<20))
		if rerr != nil {
			writeErr(w, http.StatusBadRequest, "the picture is too big (20 MB at most)")
			return
		}
		err = s.setCover(ctx, g, b, "upload")
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	g2, _ := s.store.GetGame(ctx, userFrom(ctx).ID, g.ID)
	s.decorate(userFrom(ctx), g2)
	writeJSON(w, http.StatusOK, g2)
}

func (s *Server) handleDeleteCover(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, ok := s.gameFor(w, r)
	if !ok || !s.canEdit(w, g) {
		return
	}
	s.removeFile(g.CoverPath)
	_ = s.store.SetCover(ctx, g.ID, "", "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCover(w http.ResponseWriter, r *http.Request) {
	g, ok := s.gameFor(w, r)
	if !ok {
		return
	}
	s.servePicture(w, r, g.CoverPath)
}

func (s *Server) handleShot(w http.ResponseWriter, r *http.Request) {
	g, ok := s.gameFor(w, r)
	if !ok {
		return
	}
	n, _ := strconv.Atoi(chi.URLParam(r, "n"))
	if n < 0 || n >= len(g.Shots) {
		writeErr(w, http.StatusNotFound, "no such picture")
		return
	}
	s.servePicture(w, r, g.Shots[n])
}

// servePicture sends a stored picture; the address carries a version, so it may be cached.
func (s *Server) servePicture(w http.ResponseWriter, r *http.Request, rel string) {
	if rel == "" {
		writeErr(w, http.StatusNotFound, "no picture")
		return
	}
	p := s.filePath(rel)
	if _, err := os.Stat(p); err != nil {
		writeErr(w, http.StatusNotFound, "picture missing")
		return
	}
	if r.URL.Query().Get("v") != "" {
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "private, max-age=3600")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	http.ServeFile(w, r, p)
}

// ---------- files ----------

// handleDownload sends the game: the file itself, or a zip of its files.
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	g, ok := s.gameFor(w, r)
	if !ok {
		return
	}
	files, _ := s.store.GameFiles(r.Context(), g.ID)
	s.sendGame(w, r, g, files, true)
}

// sendGame streams a game: one file as it is (with ranges, so big discs
// resume), several files as a zip without compression.
func (s *Server) sendGame(w http.ResponseWriter, r *http.Request, g *store.Game, files []store.GameFile, attachment bool) {
	if g.MissingSince != "" || g.TrashedAt != "" || len(files) == 0 {
		writeErr(w, http.StatusGone, "the game's files are not in the library")
		return
	}
	if len(files) == 1 {
		s.sendFile(w, r, g, files[0], attachment)
		return
	}
	name := library.SafeName(library.StripExt(g.FSName)) + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	if attachment {
		w.Header().Set("Content-Disposition", contentDisposition(name))
	}
	if r.Method == http.MethodHead {
		return
	}
	_ = writeZip(w, s.zipEntries(g, files, false, library.StripExt(g.FSName)))
}

// writeZip streams a zip without compression (game files are already
// compressed or don't shrink, and a stored zip starts sending at once).
func writeZip(w io.Writer, entries []zipEntry) error {
	zw := zip.NewWriter(w)
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.Name, Method: zip.Store, Modified: time.Now()}
		var fh *os.File
		if e.Data == nil {
			var err error
			if fh, err = os.Open(e.Path); err != nil {
				continue
			}
			if fi, err := fh.Stat(); err == nil {
				hdr.Modified = fi.ModTime()
			}
		}
		dst, err := zw.CreateHeader(hdr)
		if err == nil {
			if fh != nil {
				_, err = io.Copy(dst, fh)
			} else {
				_, err = dst.Write(e.Data)
			}
		}
		if fh != nil {
			fh.Close()
		}
		if err != nil {
			return err // the client went away
		}
	}
	return zw.Close()
}

func (s *Server) sendFile(w http.ResponseWriter, r *http.Request, g *store.Game, f store.GameFile, attachment bool) {
	p, err := s.gamePath(g, f.Rel)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad path")
		return
	}
	fh, err := os.Open(p)
	if err != nil {
		writeErr(w, http.StatusGone, "the file is not in the library")
		return
	}
	defer fh.Close()
	fi, err := fh.Stat()
	if err != nil {
		writeErr(w, http.StatusGone, "the file is not in the library")
		return
	}
	if attachment {
		w.Header().Set("Content-Disposition", contentDisposition(filepath.Base(f.Rel)))
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if f.MD5 != "" {
		w.Header().Set("ETag", `"`+f.MD5+`"`)
	}
	http.ServeContent(w, r, filepath.Base(f.Rel), fi.ModTime(), fh)
}

func contentDisposition(name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 32 || r > 126 || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii, url.PathEscape(name))
}

// handleFile sends one of a game's files.
func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	g, ok := s.gameFor(w, r)
	if !ok {
		return
	}
	files, _ := s.store.GameFiles(r.Context(), g.ID)
	fid := idParam(r, "fid")
	for _, f := range files {
		if f.ID == fid {
			s.sendFile(w, r, g, f, true)
			return
		}
	}
	writeErr(w, http.StatusNotFound, "no such file")
}

// ---------- trash ----------

func (s *Server) handleDeleteGame(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, ok := s.gameFor(w, r)
	if !ok || !s.canEdit(w, g) {
		return
	}
	if g.TrashedAt != "" {
		writeErr(w, http.StatusConflict, "this game is already in the trash")
		return
	}
	if g.MissingSince != "" {
		// nothing on disk to keep: forget it right away
		if err := s.purgeGame(ctx, g); err != nil {
			s.fail(w, err, "delete")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.trashGame(ctx, g); err != nil {
		s.fail(w, err, "move to the trash")
		return
	}
	s.log.Info("game moved to the trash", "game", g.Name, "by", userFrom(ctx).Email)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRestoreGame(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, ok := s.gameFor(w, r)
	if !ok || !s.canEdit(w, g) {
		return
	}
	if err := s.restoreGame(ctx, g); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	s.wakeScan()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePurgeGame(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, ok := s.gameFor(w, r)
	if !ok || !s.canEdit(w, g) {
		return
	}
	if g.TrashedAt == "" && g.MissingSince == "" {
		writeErr(w, http.StatusConflict, "move the game to the trash first")
		return
	}
	if err := s.purgeGame(ctx, g); err != nil {
		s.fail(w, err, "delete")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTrash(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	gs, _, err := s.store.ListGames(ctx, store.GameFilter{UserID: u.ID, Trashed: true, Hidden: true, Sort: "id", Desc: true, Limit: 500})
	if err != nil {
		s.fail(w, err, "trash")
		return
	}
	s.decorateAll(u, gs)
	missing, _, _ := s.store.ListGames(ctx, store.GameFilter{UserID: u.ID, Missing: true, Hidden: true, Limit: 500})
	var gone []store.Game
	for _, g := range missing {
		if g.MissingSince != "" {
			gone = append(gone, g)
		}
	}
	s.decorateAll(u, gone)
	if gone == nil {
		gone = []store.Game{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"trash": gs, "missing": gone, "days": s.cfg.TrashDays})
}
