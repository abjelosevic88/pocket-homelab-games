package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-games/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-games/internal/store"
)

// ---- AI: "what should I play?" ----
//
// Pocket AI through Core (the person's own AI settings there) or an
// OpenAI-compatible API with the person's own key. Without either, the picks
// come from the person's own lists (backlog, unfinished, favourites).

func (s *Server) aiFor(ctx context.Context, uid int64, st store.Settings) (*ai.Client, string, string) {
	email := ""
	if u, err := s.store.GetUser(ctx, uid); err == nil {
		email = u.Email
	}
	pocket, status := s.core.SelectAI(ctx, email, st.AIProvider, st.OpenAIKey != "")
	if pocket != nil {
		if !status.Ready {
			return nil, "", "Pocket AI: " + status.Detail
		}
		c := ai.New("", "", "", 120*time.Second)
		c.Pocket = pocket.WithModel(st.AIPocketModel)
		return c, "pocket", ""
	}
	if st.OpenAIKey == "" {
		why := status.Detail
		if why == "" {
			why = "no AI is set up"
		}
		return nil, "", why
	}
	return ai.New(st.OpenAIBaseURL, st.OpenAIKey, st.OpenAIModel, 120*time.Second), "openai", ""
}

func aiJSON(ctx context.Context, c *ai.Client, system, prompt string, v any) error {
	txt, err := c.Complete(ctx, system, prompt, true)
	if err != nil {
		return err
	}
	if i, j := strings.Index(txt, "{"), strings.LastIndex(txt, "}"); i >= 0 && j > i {
		txt = txt[i : j+1]
	}
	if err := json.Unmarshal([]byte(txt), v); err != nil {
		return fmt.Errorf("the AI's answer was not valid JSON: %w", err)
	}
	return nil
}

const picksSystem = `You help a person pick a game from their OWN library for tonight.
You get their mood or time (may be empty), what they played lately, and a numbered list of games they own
(name, system, year, genres, their status and play time). Pick exactly 3 different games from that list only.
Answer with JSON only: {"picks": [{"id": <the game's number>, "why": "..."}]}.
- why: ONE short friendly sentence (at most 120 characters) in plain words: why this game fits now.
- Prefer games they started and did not finish, their backlog, and favourites they have not played in a while;
  vary the systems and genres; respect the mood or time if given (e.g. "30 minutes" → something quick to pick up).`

type pick struct {
	ID  int64  `json:"id"`
	Why string `json:"why"`
}

// handlePicks suggests three games from the person's library ({"mood": "…"}).
func (s *Server) handlePicks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	var in struct {
		Mood string `json:"mood"`
	}
	_ = decode(r, &in)
	games, _, err := s.store.ListGames(ctx, store.GameFilter{UserID: u.ID, Sort: "played", Limit: 400})
	if err != nil {
		s.fail(w, err, "games")
		return
	}
	if len(games) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"picks": []any{}, "by": "none"})
		return
	}
	byID := map[int64]*store.Game{}
	for i := range games {
		byID[games[i].ID] = &games[i]
	}
	answer := func(list []pick, by string) {
		out := []map[string]any{}
		for _, p := range list {
			g, ok := byID[p.ID]
			if !ok {
				continue
			}
			s.decorate(u, g)
			out = append(out, map[string]any{"game": g, "why": p.Why})
		}
		writeJSON(w, http.StatusOK, map[string]any{"picks": out, "by": by})
	}
	st, _ := s.store.GetSettings(ctx, u.ID)
	if st.AIPicks {
		if cl, used, _ := s.aiFor(ctx, u.ID, st); cl != nil {
			var p strings.Builder
			fmt.Fprintf(&p, "Mood or time: %s\n\nPlayed lately:\n", firstNonEmpty(strings.TrimSpace(in.Mood), "(not given)"))
			recent, _ := s.store.ListSessions(ctx, u.ID, store.SessionFilter{Limit: 8})
			for _, se := range recent {
				fmt.Fprintf(&p, "- %s (%s), %d min on %s\n", se.GameName, se.Platform, se.Seconds/60, se.Day)
			}
			p.WriteString("\nTheir games:\n")
			for i, g := range games {
				if i >= 250 {
					break
				}
				year := ""
				if len(g.ReleaseDate) >= 4 {
					year = g.ReleaseDate[:4]
				}
				fmt.Fprintf(&p, "%d. %s | %s | %s | %s | status: %s | played %d min | fav: %v\n", g.ID, g.Name, g.PlatformSlug, year, oneLine(g.Genres, 60),
					firstNonEmpty(g.Mine.Status, "none"), g.Mine.PlaySeconds/60, g.Mine.Favorite)
			}
			actx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()
			var res struct {
				Picks []pick `json:"picks"`
			}
			if err := aiJSON(actx, cl, picksSystem, p.String(), &res); err == nil && len(res.Picks) > 0 {
				answer(res.Picks, used)
				return
			} else if err != nil {
				s.log.Debug("ai picks", "err", err)
			}
		}
	}
	answer(plainPicks(games), "lists")
}

// plainPicks: something started and unfinished, something from the backlog,
// a favourite not played for a while.
func plainPicks(games []store.Game) []pick {
	var out []pick
	used := map[int64]bool{}
	add := func(g *store.Game, why string) {
		if g != nil && !used[g.ID] && len(out) < 3 {
			used[g.ID] = true
			out = append(out, pick{ID: g.ID, Why: why})
		}
	}
	find := func(ok func(g *store.Game) bool) *store.Game {
		for i := range games {
			if !used[games[i].ID] && ok(&games[i]) {
				return &games[i]
			}
		}
		return nil
	}
	add(find(func(g *store.Game) bool { return g.Mine.Status == "playing" }), "You are in the middle of it — pick up where you left off.")
	add(find(func(g *store.Game) bool { return g.Mine.Status == "backlog" }), "It has been waiting in your backlog.")
	add(find(func(g *store.Game) bool {
		return g.Mine.Favorite && g.Mine.LastPlayedAt < time.Now().AddDate(0, -1, 0).UTC().Format(time.RFC3339)
	}), "A favourite you have not played in a while.")
	for len(out) < 3 {
		g := find(func(g *store.Game) bool { return g.Mine.PlayCount == 0 })
		if g == nil {
			g = find(func(*store.Game) bool { return true })
		}
		if g == nil {
			break
		}
		add(g, "Something you have not tried yet.")
	}
	return out
}

func (s *Server) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	cl, used, why := s.aiFor(ctx, actorID(ctx), st)
	writeJSON(w, http.StatusOK, map[string]any{"ready": cl != nil, "provider": used, "detail": why})
}

func (s *Server) handleAIModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	email := userFrom(ctx).Email
	c, status := s.core.SelectAI(ctx, email, "pocket", false)
	if c == nil || !status.Ready {
		writeErr(w, http.StatusServiceUnavailable, status.Detail)
		return
	}
	models, err := c.Models(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models)
}

func validAIProvider(v string) bool  { return core.ValidAIProvider(v) }
func validPocketModel(v string) bool { return core.ValidInferenceModel(v) }
