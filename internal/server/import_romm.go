package server

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-games/internal/store"
)

// Bringing a person's things over from a RomM server: favourites,
// statuses, ratings, last played, collections, saves and states, and the
// covers RomM found for games that have none here yet. Games are matched by
// platform folder + file name, so both must look at the same library.
// Running it again changes nothing that is already here.

func md5sum(b []byte) [16]byte { return md5.Sum(b) }

func md5hex(b []byte) string {
	h := md5.Sum(b)
	return hex.EncodeToString(h[:])
}

type rommClient struct {
	base, user, pass string
	http             *http.Client
}

func (c *rommClient) get(ctx context.Context, path string, v any) error {
	b, err := c.raw(ctx, path, 32<<20)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func (c *rommClient) raw(ctx context.Context, path string, limit int64) ([]byte, error) {
	u := path
	if !strings.HasPrefix(path, "http") {
		u = c.base + path
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.user, c.pass)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("RomM did not accept the user name and password")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("RomM answered %s for %s", resp.Status, path)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

type rommRom struct {
	ID         int64  `json:"id"`
	PlatformFS string `json:"platform_fs_slug"`
	FSName     string `json:"fs_name"`
	CoverLarge string `json:"path_cover_large"`
	RomUser    struct {
		LastPlayed *string `json:"last_played"`
		Backlogged bool    `json:"backlogged"`
		NowPlaying bool    `json:"now_playing"`
		Hidden     bool    `json:"hidden"`
		Rating     int     `json:"rating"`
		Status     *string `json:"status"`
	} `json:"rom_user"`
}

type rommAssetIn struct {
	ID        int64   `json:"id"`
	RomID     int64   `json:"rom_id"`
	FileName  string  `json:"file_name"`
	Emulator  *string `json:"emulator"`
	Slot      *string `json:"slot"`
	UpdatedAt string  `json:"updated_at"`
}

func (s *Server) handleImportRomm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	var in struct {
		URL      string `json:"url"`
		Username string `json:"username"`
		Password string `json:"password"`
		Saves    bool   `json:"saves"`
		Covers   bool   `json:"covers"`
	}
	if err := decode(r, &in); err != nil || in.URL == "" || in.Username == "" || in.Password == "" {
		writeErr(w, http.StatusBadRequest, "RomM's address, user name and password are needed")
		return
	}
	pu, err := url.Parse(strings.TrimRight(strings.TrimSpace(in.URL), "/"))
	if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
		writeErr(w, http.StatusBadRequest, "RomM's address must start with http:// or https://")
		return
	}
	c := &rommClient{base: pu.String(), user: in.Username, pass: in.Password, http: &http.Client{Timeout: 2 * time.Minute}}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Minute)
	defer cancel()
	res, err := s.importRomm(ctx, u, c, in.Saves, in.Covers)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	s.log.Info("imported from RomM", "user", u.Email, "result", res)
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) importRomm(ctx context.Context, u *store.User, c *rommClient, withSaves, withCovers bool) (map[string]int, error) {
	res := map[string]int{"games": 0, "matched": 0, "not_found": 0, "favorites": 0, "collections": 0, "saves": 0, "states": 0, "covers": 0}
	var page struct {
		Items []rommRom `json:"items"`
	}
	if err := c.get(ctx, "/api/roms?limit=10000&with_char_index=false&with_filter_values=false&with_rom_id_index=false", &page); err != nil {
		return nil, err
	}
	res["games"] = len(page.Items)
	ours := map[int64]*store.Game{} // RomM id → our game
	cache := map[string]int64{}
	for _, it := range page.Items {
		id := s.gameIDByPath(ctx, cache, it.PlatformFS, it.FSName)
		if id == 0 {
			res["not_found"]++
			continue
		}
		g, err := s.store.GetGame(ctx, u.ID, id)
		if err != nil {
			continue
		}
		ours[it.ID] = g
		res["matched"]++
		ru := it.RomUser
		p := store.UserGamePatch{}
		if ru.Hidden {
			p.Hidden = &ru.Hidden
		}
		if ru.Rating > 0 && g.Mine.Rating == 0 {
			p.Rating = &ru.Rating
		}
		if g.Mine.Status == "" {
			st := ""
			switch {
			case ru.Status != nil && *ru.Status == "finished":
				st = "finished"
			case ru.Status != nil && *ru.Status == "completed_100":
				st = "completed"
			case ru.Status != nil && (*ru.Status == "retired" || *ru.Status == "never_playing"):
				st = "dropped"
			case ru.NowPlaying || (ru.Status != nil && *ru.Status == "incomplete"):
				st = "playing"
			case ru.Backlogged:
				st = "backlog"
			}
			if st != "" {
				p.Status = &st
			}
		}
		if ru.LastPlayed != nil && *ru.LastPlayed != "" {
			if t := utcParam(*ru.LastPlayed); t != "" {
				p.LastPlayedAt = &t
			}
		}
		if err := s.store.PatchUserGame(ctx, u.ID, g.ID, p); err != nil {
			return res, err
		}
		if withCovers && g.CoverPath == "" && it.CoverLarge != "" {
			if b, err := c.raw(ctx, it.CoverLarge, 20<<20); err == nil && s.setCover(ctx, g, b, "romm") == nil {
				res["covers"]++
			}
		}
	}
	// collections (the favourites one becomes the heart)
	var cols []struct {
		Name        string  `json:"name"`
		Description string  `json:"description"`
		RomIDs      []int64 `json:"rom_ids"`
		IsFavorite  bool    `json:"is_favorite"`
		IsVirtual   bool    `json:"is_virtual"`
	}
	if err := c.get(ctx, "/api/collections", &cols); err == nil {
		mine, _ := s.store.ListCollections(ctx, u.ID)
		for _, col := range cols {
			if col.IsVirtual {
				continue
			}
			if col.IsFavorite {
				t := true
				for _, rid := range col.RomIDs {
					if g, ok := ours[rid]; ok {
						_ = s.store.PatchUserGame(ctx, u.ID, g.ID, store.UserGamePatch{Favorite: &t})
						res["favorites"]++
					}
				}
				continue
			}
			var cid int64
			for _, m := range mine {
				if strings.EqualFold(m.Name, col.Name) {
					cid = m.ID
				}
			}
			if cid == 0 {
				var err error
				if cid, err = s.store.CreateCollection(ctx, u.ID, oneLine(col.Name, 120), col.Description); err != nil {
					return res, err
				}
				res["collections"]++
			}
			for _, rid := range col.RomIDs {
				if g, ok := ours[rid]; ok {
					_ = s.store.ToggleCollectionGame(ctx, u.ID, cid, g.ID, true)
				}
			}
		}
	}
	if !withSaves {
		return res, nil
	}
	for _, kind := range []string{"saves", "states"} {
		var list []rommAssetIn
		if err := c.get(ctx, "/api/"+kind, &list); err != nil {
			continue
		}
		for _, a := range list {
			g, ok := ours[a.RomID]
			if !ok {
				continue
			}
			data, err := c.raw(ctx, fmt.Sprintf("/api/%s/%d/content", kind, a.ID), maxSave)
			if err != nil || len(data) == 0 {
				continue
			}
			// the same file is not stored twice
			sum := md5hex(data)
			dup := false
			existing, _ := s.store.ListSaves(ctx, u.ID, g.ID, assetKind[kind])
			for _, e := range existing {
				dup = dup || e.MD5 == sum
			}
			if dup {
				continue
			}
			slot, emu := "", ""
			if a.Slot != nil {
				slot = *a.Slot
			}
			if a.Emulator != nil {
				emu = *a.Emulator
			}
			if _, err := s.storeSave(ctx, u.ID, g, assetKind[kind], a.FileName, emu, slot, "RomM", data, nil); err == nil {
				res[kind]++
			}
		}
	}
	return res, nil
}
