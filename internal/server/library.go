package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // covers people upload
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "golang.org/x/image/webp"

	"github.com/abjelosevic88/pocket-homelab-games/internal/library"
	"github.com/abjelosevic88/pocket-homelab-games/internal/meta"
	"github.com/abjelosevic88/pocket-homelab-games/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-games/internal/platforms"
	"github.com/abjelosevic88/pocket-homelab-games/internal/store"
)

// scanStatus is what the last library scan did (for Settings → Library).
type scanStatus struct {
	Running   bool   `json:"running"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at"`
	Ms        int64  `json:"ms"`
	Platforms int    `json:"platforms"`
	Games     int    `json:"games"`
	Added     int    `json:"added"`
	Missing   int    `json:"missing"`
	Error     string `json:"error"`
}

// ---------- paths ----------

// platformDir is the folder of a platform's games.
func (s *Server) platformDir(slug string) string { return filepath.Join(s.lib.Roms, slug) }

// gameBase is the folder a game's file paths are relative to.
func (s *Server) gameBase(g *store.Game) string {
	if g.IsFolder {
		return filepath.Join(s.platformDir(g.PlatformSlug), g.FSName)
	}
	return s.platformDir(g.PlatformSlug)
}

// gamePath is the absolute path of one of a game's files (rel checked to stay inside).
func (s *Server) gamePath(g *store.Game, rel string) (string, error) {
	base := s.gameBase(g)
	p := filepath.Join(base, filepath.FromSlash(rel))
	if !strings.HasPrefix(p, base+string(filepath.Separator)) {
		return "", errors.New("bad path")
	}
	return p, nil
}

func (s *Server) firmwarePath(slug, name string) string {
	return filepath.Join(s.lib.Bios, slug, filepath.Base(name))
}

// ---------- the scan ----------

// wakeScan asks the scanner to look at the library now.
func (s *Server) wakeScan() {
	select {
	case s.scanWake <- struct{}{}:
	default:
	}
}

func (s *Server) wakeWork() {
	select {
	case s.workWake <- struct{}{}:
	default:
	}
}

// scanner looks at the library on start, every SCAN_INTERVAL and when woken.
func (s *Server) scanner(ctx context.Context) {
	interval := s.cfg.ScanInterval
	for {
		if err := s.scanAll(ctx, nil); err != nil && ctx.Err() == nil {
			s.log.Warn("library scan failed", "err", err)
		}
		var tick <-chan time.Time
		if interval > 0 {
			tick = time.After(interval)
		}
		select {
		case <-ctx.Done():
			return
		case <-s.scanWake:
		case <-tick:
		}
	}
}

func (s *Server) scanState() scanStatus {
	s.infoMu.Lock()
	defer s.infoMu.Unlock()
	return s.scanInfo
}

// scanAll reads every platform folder and the firmware. addedBy says who
// uploaded which new games ("<slug>/<fs name>" → user id).
func (s *Server) scanAll(ctx context.Context, addedBy map[string]int64) error {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	start := time.Now()
	s.infoMu.Lock()
	s.scanInfo = scanStatus{Running: true, StartedAt: store.Now()}
	s.infoMu.Unlock()
	st := scanStatus{StartedAt: store.Now()}
	defer func() {
		st.EndedAt, st.Ms = store.Now(), time.Since(start).Milliseconds()
		s.infoMu.Lock()
		s.scanInfo = st
		s.infoMu.Unlock()
	}()
	s.lib = library.Detect(s.cfg.LibraryDir)
	slugs, err := s.lib.ListPlatforms()
	if err != nil {
		st.Error = err.Error()
		return err
	}
	var added []int64
	for _, slug := range slugs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		res, n, err := s.scanPlatform(ctx, slug, addedBy)
		if err != nil {
			s.log.Warn("scan platform", "platform", slug, "err", err)
			continue
		}
		st.Platforms++
		st.Games += n
		st.Added += len(res.Added)
		st.Missing += len(res.Missing)
		added = append(added, res.Added...)
	}
	// platform folders that disappeared: their games are missing
	if known, err := s.store.ListPlatforms(ctx); err == nil {
		on := map[string]bool{}
		for _, sl := range slugs {
			on[sl] = true
		}
		for _, p := range known {
			if !on[p.Slug] && p.Games > 0 {
				res, err := s.store.SyncPlatform(ctx, p.ID, nil, nil)
				if err == nil {
					st.Missing += len(res.Missing)
				}
			}
		}
	}
	s.scanFirmware(ctx)
	if len(added) > 0 {
		s.log.Info("library: new games", "count", len(added))
		s.announceNew(ctx, added, addedBy)
	}
	if st.Missing > 0 {
		s.log.Info("library: games whose files are gone", "count", st.Missing)
	}
	s.wakeWork()
	return nil
}

// scanPlatform syncs one platform folder; it returns what changed and how
// many games the folder holds.
func (s *Server) scanPlatform(ctx context.Context, slug string, addedBy map[string]int64) (store.SyncResult, int, error) {
	dir, err := s.lib.ScanPlatform(slug)
	if err != nil {
		return store.SyncResult{}, 0, err
	}
	pid, err := s.store.EnsurePlatform(ctx, slug)
	if err != nil {
		return store.SyncResult{}, 0, err
	}
	found := make([]store.DiskGame, 0, len(dir.Games))
	by := map[string]int64{}
	for _, e := range dir.Games {
		p := library.Parse(e.FSName)
		d := store.DiskGame{FSName: e.FSName, IsFolder: e.IsFolder, MainFile: e.Main, Name: p.Title, SortName: library.SortName(p.Title),
			Regions: strings.Join(p.Regions, ", "), Languages: strings.Join(p.Languages, ", "), Tags: strings.Join(p.Tags, ", "),
			SizeBytes: e.Size, MTime: e.MTime.UTC().Format(time.RFC3339)}
		for _, f := range e.Files {
			d.Files = append(d.Files, store.GameFile{Rel: f.Rel, SizeBytes: f.Size, MTime: f.MTime.UTC().Format(time.RFC3339)})
		}
		found = append(found, d)
		if uid := addedBy[slug+"/"+e.FSName]; uid > 0 {
			by[e.FSName] = uid
		}
	}
	res, err := s.store.SyncPlatform(ctx, pid, found, by)
	return res, len(found), err
}

func (s *Server) scanFirmware(ctx context.Context) {
	for slug, files := range s.lib.ScanFirmware() {
		pid, err := s.store.EnsurePlatform(ctx, slug)
		if err != nil {
			continue
		}
		m := map[string][2]any{}
		for _, f := range files {
			m[f.Name] = [2]any{f.Size, f.MTime.UTC().Format(time.RFC3339)}
		}
		if _, err := s.store.SyncFirmware(ctx, pid, m); err != nil {
			s.log.Warn("scan firmware", "platform", slug, "err", err)
		}
	}
	// hash firmware right away: it is small and the BIOS check needs it
	fw, _ := s.store.ListFirmware(ctx, 0)
	for _, f := range fw {
		if f.MD5 != "" || f.SizeBytes > 64<<20 {
			continue
		}
		if h, err := library.HashFile(s.firmwarePath(f.PlatformSlug, f.FileName)); err == nil {
			_ = s.store.SetFirmwareHashes(ctx, f.ID, h.CRC32, h.MD5, h.SHA1)
		}
	}
}

// announceNew publishes "added" events and tells the people who asked.
func (s *Server) announceNew(ctx context.Context, ids []int64, addedBy map[string]int64) {
	var names []string
	for _, id := range ids {
		g, err := s.store.GetGame(ctx, 0, id)
		if err != nil {
			continue
		}
		names = append(names, g.Name)
		by := int64(0)
		if g.AddedBy != nil {
			by = *g.AddedBy
		}
		payload := map[string]any{"game_id": g.ID, "name": g.Name, "platform": g.PlatformSlug, "platform_name": platforms.Get(g.PlatformSlug).Name, "url": s.cfg.BaseURL + fmt.Sprintf("/games/%d", g.ID)}
		if by > 0 {
			s.publish(ctx, by, "games.added", g.Name, payload)
		} else if owner := s.store.OwnerID(ctx); owner > 0 {
			s.publish(ctx, owner, "games.added", g.Name, payload)
		}
	}
	if len(names) == 0 {
		return
	}
	ids2, _ := s.store.SettingsUsers(ctx)
	for _, uid := range ids2 {
		st, _ := s.store.GetSettings(ctx, uid)
		if !st.NotifyNewGames || st.NotifyURL == "" {
			continue
		}
		body := strings.Join(names, ", ")
		if len(names) > 8 {
			body = strings.Join(names[:8], ", ") + fmt.Sprintf(" and %d more", len(names)-8)
		}
		title := "New game in your library"
		if len(names) > 1 {
			title = fmt.Sprintf("%d new games in your library", len(names))
		}
		_ = notify.New(st.NotifyURL, st.NotifyFormat).Send(ctx, notify.Message{Title: title, Body: body, Priority: "info", Tags: []string{"video_game"}, Link: s.cfg.BaseURL + "/library?sort=added"})
	}
}

// ---------- background work: hashes and lookups ----------

func (s *Server) worker(ctx context.Context) {
	for {
		did := s.workOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		wait := 10 * time.Minute
		if did {
			wait = 2 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-s.workWake:
		case <-time.After(wait):
		}
	}
}

// workOnce looks up a few games and hashes one; true = there may be more.
func (s *Server) workOnce(ctx context.Context) bool {
	did := false
	if s.cfg.Metadata {
		ids, _ := s.store.PendingMeta(ctx, 3)
		for _, id := range ids {
			s.lookup(ctx, id, false)
			did = true
		}
	}
	ids, _ := s.store.UnhashedGames(ctx, 1)
	for _, id := range ids {
		s.hashGame(ctx, id)
		did = true
	}
	return did
}

// hashGame checksums a game's files (those under HASH_MAX_BYTES).
func (s *Server) hashGame(ctx context.Context, id int64) {
	g, err := s.store.GetGame(ctx, 0, id)
	if err != nil {
		return
	}
	files, _ := s.store.GameFiles(ctx, id)
	var mainH library.Hashes
	for _, f := range files {
		if ctx.Err() != nil {
			return
		}
		if f.SizeBytes > s.cfg.HashMaxBytes {
			continue
		}
		if f.MD5 == "" {
			p, err := s.gamePath(g, f.Rel)
			if err != nil {
				continue
			}
			h, err := library.HashFile(p)
			if err != nil {
				continue
			}
			f.CRC32, f.MD5, f.SHA1 = h.CRC32, h.MD5, h.SHA1
			_ = s.store.SetFileHashes(ctx, f.ID, h.CRC32, h.MD5, h.SHA1)
		}
		if f.Rel == g.MainFile {
			mainH = library.Hashes{CRC32: f.CRC32, MD5: f.MD5, SHA1: f.SHA1}
		}
	}
	_ = s.store.SetGameHashes(ctx, id, mainH.CRC32, mainH.MD5, mainH.SHA1)
}

// lookup finds a cover, pictures and the facts about a game. force = also
// replace a cover that is already there (a person asked for a new lookup).
func (s *Server) lookup(ctx context.Context, id int64, force bool) {
	g, err := s.store.GetGame(ctx, 0, id)
	if err != nil {
		return
	}
	p := platforms.Get(g.PlatformSlug)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	status := "unmatched"
	regions := s.preferredRegions(ctx)
	if g.Regions != "" {
		regions = append(strings.Split(g.Regions, ", "), regions...)
	}
	// the cover and pictures from the libretro sets
	if p.Libretro != "" && (g.CoverPath == "" || force) {
		if c, err := s.meta.Match(ctx, p.Libretro, meta.Boxart, g.FSName, g.Name, regions, 1); err == nil && len(c) > 0 && c[0].Score >= 150 {
			if err := s.setCoverFromURL(ctx, g, c[0].URL, "libretro"); err == nil {
				status = "partial"
			}
			// a title screen and an in-game picture, by the same name
			var shots []string
			for _, kind := range []string{meta.Title, meta.Snap} {
				if b, err := s.meta.Fetch(ctx, meta.ThumbURL(p.Libretro, kind, c[0].Name)); err == nil {
					if rel, err := s.savePicture("shots", b, 1280); err == nil {
						shots = append(shots, rel)
					}
				}
			}
			if len(shots) > 0 {
				for _, old := range g.Shots {
					s.removeFile(old)
				}
				_ = s.store.SetShots(ctx, g.ID, shots)
			}
		} else if err != nil {
			s.log.Debug("cover lookup", "game", g.Name, "err", err)
		}
	} else if g.CoverPath != "" {
		status = "partial"
	}
	// what the game is
	if !g.MetaLocked {
		f, err := s.meta.Lookup(ctx, g.Name, p.Wiki)
		if err == nil {
			s.applyFacts(ctx, g, f, force)
			if status == "partial" || g.CoverPath != "" {
				status = "matched"
			} else {
				status = "partial"
			}
		} else if !errors.Is(err, meta.ErrNoMatch) {
			// a network problem: try again later
			s.log.Debug("wikipedia lookup", "game", g.Name, "err", err)
			if ctx.Err() == nil && !strings.Contains(err.Error(), "HTTP 4") {
				return
			}
		}
	}
	_ = s.store.UpdateGameMeta(ctx, g.ID, store.GameMeta{MetaStatus: &status})
}

// applyFacts stores what Wikipedia/Wikidata said (filling only empty fields
// unless overwrite).
func (s *Server) applyFacts(ctx context.Context, g *store.Game, f *meta.Facts, overwrite bool) {
	m := store.GameMeta{}
	set := func(dst **string, cur, v string) {
		if v != "" && (cur == "" || overwrite) {
			*dst = &v
		}
	}
	src := "wikipedia"
	set(&m.Summary, g.Summary, f.Summary)
	if m.Summary != nil {
		m.SummarySource, m.SummaryURL = &src, &f.URL
	}
	set(&m.WikiTitle, g.WikiTitle, f.WikiTitle)
	set(&m.Genres, g.Genres, strings.Join(f.Genres, ", "))
	set(&m.Developer, g.Developer, strings.Join(f.Developers, ", "))
	set(&m.Publisher, g.Publisher, strings.Join(f.Publishers, ", "))
	set(&m.ReleaseDate, g.ReleaseDate, f.ReleaseDate)
	set(&m.Players, g.Players, strings.Join(f.Modes, ", "))
	_ = s.store.UpdateGameMeta(ctx, g.ID, m)
	if g.CoverPath == "" && f.ImageURL != "" {
		// the article's picture is usually the box art
		_ = s.setCoverFromURL(ctx, g, f.ImageURL, "wikipedia")
	}
}

func (s *Server) preferredRegions(ctx context.Context) []string {
	st, _ := s.store.GetSettings(ctx, s.store.OwnerID(ctx))
	var out []string
	for _, r := range strings.Split(st.Regions, ",") {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, r)
		}
	}
	return out
}

// setCoverFromURL downloads a picture and makes it the game's cover.
func (s *Server) setCoverFromURL(ctx context.Context, g *store.Game, u, source string) error {
	b, err := s.meta.Fetch(ctx, u)
	if err != nil {
		return err
	}
	return s.setCover(ctx, g, b, source)
}

func (s *Server) setCover(ctx context.Context, g *store.Game, b []byte, source string) error {
	rel, err := s.savePicture("covers", b, 900)
	if err != nil {
		return err
	}
	old := g.CoverPath
	if err := s.store.SetCover(ctx, g.ID, rel, source); err != nil {
		s.removeFile(rel)
		return err
	}
	g.CoverPath, g.CoverSource = rel, source
	if old != "" && old != rel {
		s.removeFile(old)
	}
	return nil
}

// savePicture stores a picture as JPEG (scaled down to maxSide) and returns
// its FILES_DIR relative path. PNG covers with transparency get a dark
// background.
func (s *Server) savePicture(sub string, b []byte, maxSide int) (string, error) {
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("not a picture: %w", err)
	}
	img = fit(img, maxSide)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flatten(img), &jpeg.Options{Quality: 86}); err != nil {
		return "", err
	}
	return s.saveFile(sub, ".jpg", buf.Bytes())
}

// ---------- trash ----------

// trashGame moves a game's files into the trash folder and marks it deleted.
// It stays restorable for TRASH_DAYS.
func (s *Server) trashGame(ctx context.Context, g *store.Game) error {
	stamp := time.Now().Format("2006-01-02_150405")
	dstDir := filepath.Join(s.lib.Trash, stamp+fmt.Sprintf("_%d", g.ID), g.PlatformSlug)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	if g.IsFolder {
		if err := os.Rename(filepath.Join(s.platformDir(g.PlatformSlug), g.FSName), filepath.Join(dstDir, g.FSName)); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else {
		files, _ := s.store.GameFiles(ctx, g.ID)
		for _, f := range files {
			src := filepath.Join(s.platformDir(g.PlatformSlug), filepath.FromSlash(f.Rel))
			if err := os.Rename(src, filepath.Join(dstDir, filepath.Base(f.Rel))); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	rel, _ := filepath.Rel(s.lib.Root, dstDir)
	return s.store.TrashGame(ctx, g.ID, filepath.ToSlash(rel))
}

// restoreGame puts a trashed game's files back.
func (s *Server) restoreGame(ctx context.Context, g *store.Game) error {
	if g.TrashPath == "" {
		return errors.New("this game is not in the trash")
	}
	src := filepath.Join(s.lib.Root, filepath.FromSlash(g.TrashPath))
	dst := s.platformDir(g.PlatformSlug)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	des, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("the files are no longer in the trash: %w", err)
	}
	for _, d := range des {
		target := filepath.Join(dst, d.Name())
		if _, err := os.Stat(target); err == nil {
			return fmt.Errorf("%q is already in the library again", d.Name())
		}
	}
	for _, d := range des {
		if err := os.Rename(filepath.Join(src, d.Name()), filepath.Join(dst, d.Name())); err != nil {
			return err
		}
	}
	_ = os.Remove(src)
	_ = os.Remove(filepath.Dir(src))
	return s.store.UntrashGame(ctx, g.ID)
}

// purgeGame deletes a trashed game's files for good (and its row, with the
// saves and play time that belonged to it).
func (s *Server) purgeGame(ctx context.Context, g *store.Game) error {
	if g.TrashPath != "" {
		dir := filepath.Dir(filepath.Join(s.lib.Root, filepath.FromSlash(g.TrashPath)))
		if strings.HasPrefix(dir, s.lib.Trash+string(filepath.Separator)) {
			_ = os.RemoveAll(dir)
		}
	}
	saves, _ := s.allSavesOf(ctx, g.ID)
	for _, v := range saves {
		s.removeFile(v.Path)
		s.removeFile(v.ShotPath)
	}
	s.removeFile(g.CoverPath)
	for _, p := range g.Shots {
		s.removeFile(p)
	}
	return s.store.PurgeGame(ctx, g.ID)
}

func (s *Server) allSavesOf(ctx context.Context, gameID int64) ([]store.Save, error) {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT path, shot_path FROM saves WHERE game_id = ?`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Save
	for rows.Next() {
		var v store.Save
		if err := rows.Scan(&v.Path, &v.ShotPath); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Server) emptyTrash(ctx context.Context) {
	if s.cfg.TrashDays <= 0 {
		return
	}
	before := time.Now().Add(-time.Duration(s.cfg.TrashDays) * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	games, err := s.store.ExpiredTrash(ctx, before)
	if err != nil {
		return
	}
	for i := range games {
		if err := s.purgeGame(ctx, &games[i]); err == nil {
			s.log.Info("trash: removed for good", "game", games[i].Name)
		}
	}
}

// ---------- events ----------

// publishSessions announces sittings that ended (no heartbeat for 3 minutes).
func (s *Server) publishSessions(ctx context.Context) {
	before := time.Now().Add(-3 * time.Minute).UTC().Format(time.RFC3339Nano)
	list, err := s.store.FinishedUnpublished(ctx, before)
	if err != nil {
		return
	}
	for _, x := range list {
		if x.Seconds >= 60 {
			s.publish(ctx, x.UserID, "games.played", x.GameName, map[string]any{
				"game_id": x.GameID, "name": x.GameName, "platform": x.Platform, "platform_name": platforms.Get(x.Platform).Name,
				"minutes": (x.Seconds + 30) / 60, "seconds": x.Seconds, "started_at": x.StartedAt, "ended_at": x.EndedAt, "day": x.Day,
				"device": x.Device, "url": s.cfg.BaseURL + fmt.Sprintf("/games/%d", x.GameID),
			})
		}
		_ = s.store.MarkPublished(ctx, x.ID)
	}
}
