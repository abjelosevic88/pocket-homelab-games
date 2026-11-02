package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-games/internal/platforms"
	"github.com/abjelosevic88/pocket-homelab-games/internal/store"
)

// ---------- collections ----------

func (s *Server) handleListCollections(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListCollections(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		s.fail(w, err, "collections")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type collectionIn struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	GameIDs     *[]int64 `json:"game_ids"`
}

func (s *Server) handleCreateCollection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	var in collectionIn
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeErr(w, http.StatusBadRequest, "a collection needs a name")
		return
	}
	id, err := s.store.CreateCollection(ctx, u.ID, oneLine(in.Name, 120), strings.TrimSpace(in.Description))
	if err != nil {
		s.fail(w, err, "collection")
		return
	}
	if in.GameIDs != nil {
		_ = s.store.SetCollectionGames(ctx, u.ID, id, *in.GameIDs)
	}
	c, _ := s.store.GetCollection(ctx, u.ID, id)
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleGetCollection(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetCollection(r.Context(), userFrom(r.Context()).ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "collection")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleUpdateCollection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	id := idParam(r, "id")
	c, err := s.store.GetCollection(ctx, u.ID, id)
	if err != nil {
		s.fail(w, err, "collection")
		return
	}
	var in collectionIn
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	name, desc := c.Name, c.Description
	if keys["name"] {
		if strings.TrimSpace(in.Name) == "" {
			writeErr(w, http.StatusBadRequest, "a collection needs a name")
			return
		}
		name = oneLine(in.Name, 120)
	}
	if keys["description"] {
		desc = strings.TrimSpace(in.Description)
	}
	if err := s.store.UpdateCollection(ctx, u.ID, id, name, desc); err != nil {
		s.fail(w, err, "collection")
		return
	}
	if in.GameIDs != nil {
		if err := s.store.SetCollectionGames(ctx, u.ID, id, *in.GameIDs); err != nil {
			s.fail(w, err, "collection")
			return
		}
	}
	c, _ = s.store.GetCollection(ctx, u.ID, id)
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteCollection(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteCollection(r.Context(), userFrom(r.Context()).ID, idParam(r, "id")); err != nil {
		s.fail(w, err, "collection")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleCollectionGame puts a game into a collection or takes it out ({"in": true|false}).
func (s *Server) handleCollectionGame(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	if _, err := s.store.GetGame(ctx, u.ID, idParam(r, "gid")); err != nil {
		s.fail(w, err, "game")
		return
	}
	var in struct {
		In bool `json:"in"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.store.ToggleCollectionGame(ctx, u.ID, idParam(r, "id"), idParam(r, "gid"), in.In); err != nil {
		s.fail(w, err, "collection")
		return
	}
	c, _ := s.store.GetCollection(ctx, u.ID, idParam(r, "id"))
	writeJSON(w, http.StatusOK, c)
}

// ---------- stats ----------

// handleStats: the time played over a range (?days=30, or ?from=&to=).
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	q := r.URL.Query()
	to := time.Now()
	days := queryInt(r, "days")
	if days <= 0 || days > 3660 {
		days = 30
	}
	from := to.AddDate(0, 0, -(days - 1))
	fromS, toS := from.Format("2006-01-02"), to.Format("2006-01-02")
	if f, t := q.Get("from"), q.Get("to"); validDate(f) && validDate(t) && f != "" && t != "" && f <= t {
		fromS, toS = f, t
	}
	sec, sessions, activeDays, games, _ := s.store.TotalPlay(ctx, u.ID, fromS, toS)
	byDay, _ := s.store.PlayByDay(ctx, u.ID, fromS, toS)
	top, _ := s.store.TopGames(ctx, u.ID, fromS, toS, 10)
	byPlatform, _ := s.store.TopPlatforms(ctx, u.ID, fromS, toS)
	for i := range byPlatform {
		byPlatform[i].Name = platforms.Get(byPlatform[i].Platform).Name
	}
	allSec, allSessions, _, allGames, _ := s.store.TotalPlay(ctx, u.ID, "0000-00-00", "9999-99-99")
	counts, _ := s.store.StatusCounts(ctx, u.ID)
	recent, _ := s.store.ListSessions(ctx, u.ID, store.SessionFilter{Limit: 15})
	// the longest run of days in a row with play, ending today or yesterday
	streak := 0
	if all, err := s.store.PlayByDay(ctx, u.ID, to.AddDate(-1, 0, 0).Format("2006-01-02"), toS); err == nil {
		have := map[string]bool{}
		for _, d := range all {
			if d.Seconds >= 60 {
				have[d.Day] = true
			}
		}
		day := to
		if !have[day.Format("2006-01-02")] {
			day = day.AddDate(0, 0, -1)
		}
		for have[day.Format("2006-01-02")] {
			streak++
			day = day.AddDate(0, 0, -1)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from": fromS, "to": toS, "seconds": sec, "sessions": sessions, "days": activeDays, "games": games,
		"by_day": byDay, "top_games": top, "by_platform": byPlatform, "recent": recent, "streak": streak,
		"all_time": map[string]any{"seconds": allSec, "sessions": allSessions, "games": allGames}, "counts": counts,
	})
}

// handleDay is what the person played on one day (for Pocket Day's journal).
func (s *Server) handleDay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	day := chi.URLParam(r, "date")
	if !validDate(day) || day == "" {
		writeErr(w, http.StatusBadRequest, "date: YYYY-MM-DD")
		return
	}
	list, err := s.store.ListSessions(ctx, u.ID, store.SessionFilter{From: day, To: day})
	if err != nil {
		s.fail(w, err, "day")
		return
	}
	type gameDay struct {
		GameID   int64  `json:"game_id"`
		Name     string `json:"name"`
		Platform string `json:"platform"`
		Seconds  int64  `json:"seconds"`
		Sessions int    `json:"sessions"`
		URL      string `json:"url"`
	}
	by := map[int64]*gameDay{}
	var order []int64
	var total int64
	for _, se := range list {
		g, ok := by[se.GameID]
		if !ok {
			g = &gameDay{GameID: se.GameID, Name: se.GameName, Platform: platforms.Get(se.Platform).Name, URL: s.cfg.BaseURL + "/games/" + itoa64(se.GameID)}
			by[se.GameID] = g
			order = append(order, se.GameID)
		}
		g.Seconds += se.Seconds
		g.Sessions++
		total += se.Seconds
	}
	games := []gameDay{}
	for _, id := range order {
		games = append(games, *by[id])
	}
	writeJSON(w, http.StatusOK, map[string]any{"date": day, "seconds": total, "games": games, "sessions": list})
}

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }
