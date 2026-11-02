package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/abjelosevic88/pocket-homelab-games/internal/store"
)

// A person's own data as JSON: what they think of each game, their play
// time and their collections. Games are named by platform + file name, so an
// export moves to another box with the same library. Save files are not in
// it (they can be big); the backup of DATA_DIR holds them.

type exportGame struct {
	Platform     string `json:"platform"`
	FSName       string `json:"fs_name"`
	Name         string `json:"name"`
	Favorite     bool   `json:"favorite,omitempty"`
	Status       string `json:"status,omitempty"`
	Rating       int    `json:"rating,omitempty"`
	Notes        string `json:"notes,omitempty"`
	Hidden       bool   `json:"hidden,omitempty"`
	LastPlayedAt string `json:"last_played_at,omitempty"`
	Core         string `json:"core,omitempty"`
}

type exportSession struct {
	Platform  string `json:"platform"`
	FSName    string `json:"fs_name"`
	StartedAt string `json:"started_at"`
	Seconds   int64  `json:"seconds"`
	Device    string `json:"device"`
}

type exportCollection struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Games       [][2]string `json:"games"` // [platform, fs_name]
}

type exportDoc struct {
	App         string             `json:"app"`
	Version     string             `json:"version"`
	ExportedAt  string             `json:"exported_at"`
	Games       []exportGame       `json:"games"`
	Sessions    []exportSession    `json:"sessions"`
	Collections []exportCollection `json:"collections"`
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	doc := exportDoc{App: "pocket-games", Version: Version, ExportedAt: store.Now(), Games: []exportGame{}, Sessions: []exportSession{}, Collections: []exportCollection{}}
	games, _, err := s.store.ListGames(ctx, store.GameFilter{UserID: u.ID, Missing: true, Hidden: true})
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	byID := map[int64]*store.Game{}
	for i := range games {
		g := &games[i]
		byID[g.ID] = g
		m := g.Mine
		if !m.Favorite && m.Status == "" && m.Rating == 0 && m.Notes == "" && !m.Hidden && m.LastPlayedAt == "" && m.Core == "" {
			continue
		}
		doc.Games = append(doc.Games, exportGame{Platform: g.PlatformSlug, FSName: g.FSName, Name: g.Name, Favorite: m.Favorite, Status: m.Status,
			Rating: m.Rating, Notes: m.Notes, Hidden: m.Hidden, LastPlayedAt: m.LastPlayedAt, Core: m.Core})
	}
	sessions, _ := s.store.ListSessions(ctx, u.ID, store.SessionFilter{})
	for _, se := range sessions {
		if g, ok := byID[se.GameID]; ok {
			doc.Sessions = append(doc.Sessions, exportSession{Platform: g.PlatformSlug, FSName: g.FSName, StartedAt: se.StartedAt, Seconds: se.Seconds, Device: se.Device})
		}
	}
	cols, _ := s.store.ListCollections(ctx, u.ID)
	for _, c := range cols {
		ec := exportCollection{Name: c.Name, Description: c.Description, Games: [][2]string{}}
		for _, id := range c.GameIDs {
			if g, ok := byID[id]; ok {
				ec.Games = append(ec.Games, [2]string{g.PlatformSlug, g.FSName})
			}
		}
		doc.Collections = append(doc.Collections, ec)
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="pocket-games-%s.json"`, time.Now().Format("2006-01-02")))
	writeJSON(w, http.StatusOK, doc)
}

// handleImport reads an export (of this box or another). Running it twice
// changes nothing the second time: sittings are matched by their start.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "the file is too big")
		return
	}
	var doc exportDoc
	if err := json.Unmarshal(b, &doc); err != nil || doc.App != "pocket-games" {
		writeErr(w, http.StatusBadRequest, "this is not a Pocket Games export")
		return
	}
	res, err := s.importDoc(ctx, u.ID, &doc)
	if err != nil {
		s.fail(w, err, "import")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) gameIDByPath(ctx context.Context, cache map[string]int64, platform, fsName string) int64 {
	key := platform + "/" + fsName
	if id, ok := cache[key]; ok {
		return id
	}
	var id int64
	_ = s.store.DB.QueryRowContext(ctx, `SELECT g.id FROM games g JOIN platforms p ON p.id = g.platform_id WHERE p.slug = ? AND g.fs_name = ?`, platform, fsName).Scan(&id)
	cache[key] = id
	return id
}

func (s *Server) importDoc(ctx context.Context, uid int64, doc *exportDoc) (map[string]int, error) {
	res := map[string]int{"games": 0, "sessions": 0, "collections": 0, "not_found": 0}
	cache := map[string]int64{}
	for _, eg := range doc.Games {
		id := s.gameIDByPath(ctx, cache, eg.Platform, eg.FSName)
		if id == 0 {
			res["not_found"]++
			continue
		}
		p := store.UserGamePatch{Favorite: &eg.Favorite, Rating: &eg.Rating, Notes: &eg.Notes, Hidden: &eg.Hidden, Core: &eg.Core}
		if store.ValidStatus(eg.Status) {
			p.Status = &eg.Status
		}
		if eg.LastPlayedAt != "" {
			p.LastPlayedAt = &eg.LastPlayedAt
		}
		if err := s.store.PatchUserGame(ctx, uid, id, p); err != nil {
			return res, err
		}
		res["games"]++
	}
	for _, es := range doc.Sessions {
		id := s.gameIDByPath(ctx, cache, es.Platform, es.FSName)
		if id == 0 || es.Seconds <= 0 {
			continue
		}
		var n int
		_ = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM play_sessions WHERE user_id = ? AND game_id = ? AND started_at = ?`, uid, id, es.StartedAt).Scan(&n)
		if n > 0 {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, es.StartedAt)
		if err != nil {
			continue
		}
		if err := s.store.AddPlayTime(ctx, uid, id, t, es.Seconds, firstNonEmpty(es.Device, "import")); err != nil {
			return res, err
		}
		// AddPlayTime stores the start in UTC with nanoseconds; keep the original text so a second import matches
		_, _ = s.store.DB.ExecContext(ctx, `UPDATE play_sessions SET started_at = ? WHERE id = (SELECT MAX(id) FROM play_sessions WHERE user_id = ?)`, es.StartedAt, uid)
		res["sessions"]++
	}
	cols, _ := s.store.ListCollections(ctx, uid)
	for _, ec := range doc.Collections {
		var cid int64
		for _, c := range cols {
			if c.Name == ec.Name {
				cid = c.ID
			}
		}
		if cid == 0 {
			var err error
			if cid, err = s.store.CreateCollection(ctx, uid, ec.Name, ec.Description); err != nil {
				return res, err
			}
			res["collections"]++
		}
		for _, pg := range ec.Games {
			if id := s.gameIDByPath(ctx, cache, pg[0], pg[1]); id > 0 {
				_ = s.store.ToggleCollectionGame(ctx, uid, cid, id, true)
			}
		}
	}
	return res, nil
}
