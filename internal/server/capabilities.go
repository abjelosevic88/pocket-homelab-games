package server

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-games/internal/platforms"
	"github.com/abjelosevic88/pocket-homelab-games/internal/store"
)

// The three routes behind `capabilities` (links.go): /api/v1/pocket/search,
// /day/{date} and /widget, for Pocket Search, Pocket Day and Pocket Hub. The
// games are the household's, as in the library; what comes along with them
// (status, stars, play time, when they were last played) is the request's
// person's own. The SDK's handlers check and cap what goes in and out. They
// read what GET /games, GET /day/{date} and GET /home read; nothing is
// played, marked or recorded. There is no capture: a game comes from a file,
// not from a line of text.

const widgetRows = 5 // rows in the widget

// gameURL opens a game in the app.
func gameURL(id int64) string { return "/games/" + strconv.FormatInt(id, 10) }

// dots joins the parts of a line that are there: "Game Boy Advance · playing".
func dots(parts ...string) string {
	out := parts[:0:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

// count is a number with its word: "1 game", "3 games".
func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// hasWord reports whether there is a letter or a digit to look for.
func hasWord(q string) bool {
	return strings.IndexFunc(q, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0
}

// playWords is a length of play as the pages say it: "2 h 15 min", "45 min",
// "under a minute".
func playWords(sec int64) string {
	if sec < 60 {
		return "under a minute"
	}
	m := int(math.Round(float64(sec) / 60))
	switch h, r := m/60, m%60; {
	case h == 0:
		return fmt.Sprintf("%d min", m)
	case r == 0:
		return fmt.Sprintf("%d h", h)
	default:
		return fmt.Sprintf("%d h %d min", h, r)
	}
}

// whenWords says a day in plain words, seen from now: "today", "yesterday",
// "12 Nov", "12 Nov 2025".
func whenWords(ts string, now time.Time) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return ""
	}
	t = t.In(now.Location())
	switch day := t.Format("2006-01-02"); {
	case day == now.Format("2006-01-02"):
		return "today"
	case day == now.AddDate(0, 0, -1).Format("2006-01-02"):
		return "yesterday"
	case t.Year() == now.Year():
		return t.Format("2 Jan")
	}
	return t.Format("2 Jan 2006")
}

// statusWords are the person's statuses as a line says them.
var statusWords = map[string]string{
	"backlog": "in your backlog", "playing": "playing", "finished": "finished", "completed": "completed 100%", "dropped": "dropped",
}

// opinionWords is what the person keeps about a game: "playing, 4.5 stars,
// favourite"; nothing when they keep nothing.
func opinionWords(m store.UserGame) string {
	var parts []string
	if w := statusWords[m.Status]; w != "" {
		parts = append(parts, w)
	}
	if m.Rating > 0 {
		stars := "stars"
		if m.Rating == 2 {
			stars = "star"
		}
		parts = append(parts, strconv.FormatFloat(float64(m.Rating)/2, 'f', -1, 64)+" "+stars)
	}
	if m.Favorite {
		parts = append(parts, "favourite")
	}
	return strings.Join(parts, ", ")
}

// playedWords is how much and when the person played a game here: "2 h 15
// min played, last played 12 Nov", "not played yet".
func playedWords(m store.UserGame, now time.Time) string {
	last := ""
	if w := whenWords(m.LastPlayedAt, now); w != "" {
		last = "last played " + w
	}
	switch {
	case m.PlaySeconds >= 60 && last != "":
		return playWords(m.PlaySeconds) + " played, " + last
	case m.PlaySeconds >= 60:
		return playWords(m.PlaySeconds) + " played"
	case last != "":
		return last
	case m.Status != "" && m.Status != "backlog":
		return "" // finished or dropped elsewhere: "not played yet" would say otherwise
	}
	return "not played yet"
}

// rfc3339 is a stored time as RFC 3339 in seconds, or nothing.
func rfc3339(ts string) string {
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	return ""
}

// ---------- search ----------

// titleScore says how well a game's name answers the words: it is the name
// (4), starts with them (3), has them (2), or the game was found by its
// genre, developer or file name (1).
func titleScore(name, q string) float64 {
	n, q := strings.ToLower(strings.TrimSpace(name)), strings.ToLower(strings.TrimSpace(q))
	switch {
	case n == q:
		return 4
	case strings.HasPrefix(n, q):
		return 3
	case strings.Contains(n, q):
		return 2
	}
	return 1
}

// platformMatch finds the library's systems the words name: the whole query
// as a system's folder or short name, or (three letters or more) as part of
// its name or the name the household gave it ("snes", "game boy"), and single
// words that are a system's folder or short name ("mario gba" → gba, leaving
// "mario").
func platformMatch(ps []store.Platform, q string) (whole []int64, inWords []int64, rest string) {
	lq := strings.ToLower(q)
	short := func(p store.Platform) (string, string) {
		k := platforms.Get(p.Slug)
		return strings.ToLower(p.Slug), strings.ToLower(k.Short)
	}
	for _, p := range ps {
		if p.Games == 0 {
			continue
		}
		slug, sh := short(p)
		name, custom := strings.ToLower(platforms.Get(p.Slug).Name), strings.ToLower(p.CustomName)
		named := len([]rune(lq)) >= 3 && (strings.Contains(name, lq) || (custom != "" && strings.Contains(custom, lq)))
		if lq == slug || lq == sh || named {
			whole = append(whole, p.ID)
		}
	}
	words := strings.Fields(lq)
	if len(words) < 2 {
		return whole, nil, ""
	}
	var left []string
	for _, w := range words {
		found := false
		for _, p := range ps {
			if slug, sh := short(p); p.Games > 0 && (w == slug || w == sh) {
				inWords, found = append(inWords, p.ID), true
			}
		}
		if !found {
			left = append(left, w)
		}
	}
	if len(inWords) == 0 || len(left) == 0 {
		return whole, nil, ""
	}
	return whole, inWords, strings.Join(left, " ")
}

// pocketSearch answers Pocket Search with the games GET /games?q= finds (by
// name, file name, genre and developer), then the games of a system the words
// name, each game once: the name that answers best first, and among equals
// the game played last. Games the person hid and games whose files are gone
// are left out, as in the library. The line says the system, the person's
// status and stars, and how much and when they played it.
func (s *Server) pocketSearch(r *http.Request, q string, limit int) ([]core.SearchItem, error) {
	ctx, u := r.Context(), userFrom(r.Context())
	if u == nil || !hasWord(q) {
		return nil, nil
	}
	ps, err := s.store.ListPlatforms(ctx)
	if err != nil {
		return nil, err
	}
	whole, inWords, rest := platformMatch(ps, q)
	now := time.Now()
	out := []core.SearchItem{}
	seen := map[int64]bool{}
	add := func(f store.GameFilter, score func(g *store.Game) float64) error {
		f.UserID, f.Sort, f.Limit = u.ID, "played", limit
		gs, _, err := s.store.ListGames(ctx, f)
		if err != nil {
			return err
		}
		s.decorateAll(u, gs)
		for i := range gs {
			g := &gs[i]
			if seen[g.ID] {
				continue
			}
			seen[g.ID] = true
			out = append(out, core.SearchItem{ID: strconv.FormatInt(g.ID, 10), Title: g.Name, Kind: "game", URL: gameURL(g.ID),
				Snippet: dots(g.PlatformName, opinionWords(g.Mine), playedWords(g.Mine, now)),
				Date:    firstNonEmpty(rfc3339(g.Mine.LastPlayedAt), rfc3339(g.CreatedAt)), Score: score(g)})
		}
		return nil
	}
	if err := add(store.GameFilter{Query: q}, func(g *store.Game) float64 { return titleScore(g.Name, q) }); err != nil {
		return nil, err
	}
	if len(inWords) > 0 {
		if err := add(store.GameFilter{Query: rest, PlatformIDs: inWords}, func(g *store.Game) float64 { return titleScore(g.Name, rest) }); err != nil {
			return nil, err
		}
	}
	if len(whole) > 0 {
		if err := add(store.GameFilter{PlatformIDs: whole}, func(*store.Game) float64 { return 0.5 }); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ---------- day ----------

// dayGame is one game on a day: every sitting with it together.
type dayGame struct {
	id             int64
	name, platform string
	seconds        int64
	first, last    time.Time // of the sittings the player or a device timed
}

// typedIn reports a sitting added by hand ("I played 40 minutes on the
// console"): its day is the one the person gave, its clock is made up.
func typedIn(se store.Session) bool { return se.Device == "elsewhere" }

// pocketDay answers Pocket Day from the play log (what GET /day/{date}
// reads): each game played that day once, with the minutes it got and when
// the first sitting started and the last one stopped. The day runs in the
// caller's time zone when it gives one, else in the server's, as the log
// keeps it; time added by hand stays on the day it was given and has no clock.
func (s *Server) pocketDay(r *http.Request, date string, tz *time.Location) (*core.Day, error) {
	ctx, uid := r.Context(), actorID(r.Context())
	loc, from, to := time.Local, date, date
	if tz != nil {
		// the log keeps the server's day: read the days around it and keep the caller's
		loc, from, to = tz, store.AddDays(date, -1), store.AddDays(date, 1)
	}
	list, err := s.store.ListSessions(ctx, uid, store.SessionFilter{From: from, To: to})
	if err != nil {
		return nil, err
	}
	var order []*dayGame
	by := map[int64]*dayGame{}
	var total int64
	for i := len(list) - 1; i >= 0; i-- { // oldest first
		se := list[i]
		start, serr := time.Parse(time.RFC3339Nano, se.StartedAt)
		end, eerr := time.Parse(time.RFC3339Nano, se.EndedAt)
		timed := !typedIn(se) && serr == nil && eerr == nil
		switch {
		case tz == nil || !timed:
			if se.Day != date {
				continue
			}
		case start.In(loc).Format("2006-01-02") != date:
			continue
		}
		g := by[se.GameID]
		if g == nil {
			g = &dayGame{id: se.GameID, name: se.GameName, platform: platforms.Get(se.Platform).Name}
			by[se.GameID] = g
			order = append(order, g)
		}
		g.seconds += se.Seconds
		total += se.Seconds
		if timed {
			if g.first.IsZero() || start.Before(g.first) {
				g.first = start
			}
			if end.After(g.last) {
				g.last = end
			}
		}
	}
	if len(order) == 0 {
		return nil, nil
	}
	// the covers of the games of the day, as the library shows them
	ids := make([]int64, 0, len(order))
	for _, g := range order {
		ids = append(ids, g.id)
	}
	covers := map[int64]string{}
	if gs, _, err := s.store.ListGames(ctx, store.GameFilter{UserID: uid, IDs: ids, Missing: true, Hidden: true}); err == nil {
		s.decorateAll(userFrom(ctx), gs)
		for _, g := range gs {
			covers[g.ID] = g.Cover
		}
	}

	day := &core.Day{Date: date}
	for _, g := range order {
		it := core.DayItem{ID: strconv.FormatInt(g.id, 10), Title: g.name, Sub: g.platform, Kind: "played",
			Minutes: math.Round(float64(g.seconds)/60*100) / 100, URL: gameURL(g.id), Image: covers[g.id]}
		if !g.first.IsZero() && g.first.In(loc).Format("2006-01-02") == date {
			it.Time = g.first.In(loc).Format("15:04")
			if end := g.last.In(loc); end.Format("2006-01-02") == date && end.Format("15:04") != it.Time {
				it.End = end.Format("15:04")
			}
		}
		day.Items = append(day.Items, it)
	}
	// "Played Metroid Fusion for 45 min", "Played Metroid Fusion and Crash
	// Bandicoot for 1 h 10 min", "Played 3 games for 2 h 5 min"
	byTime := append([]*dayGame(nil), order...)
	sort.SliceStable(byTime, func(i, j int) bool { return byTime[i].seconds > byTime[j].seconds })
	switch len(byTime) {
	case 1:
		day.Summary = "Played " + byTime[0].name
	case 2:
		day.Summary = "Played " + byTime[0].name + " and " + byTime[1].name
	default:
		day.Summary = "Played " + count(len(byTime), "game", "games")
	}
	day.Summary += " for " + playWords(total)
	day.Metrics = []core.Metric{{Label: "Time", Value: playWords(total)}}
	return day, nil
}

// ---------- widget ----------

// pocketWidget answers Pocket Hub with the home page's "Continue playing":
// the games the person played last, the latest first, each with its system
// and when it was played, and the week's play time as on the home page. It
// reads the newest few rows only. Someone who has not played anything yet
// gets nothing to show.
func (s *Server) pocketWidget(r *http.Request) (*core.WidgetData, error) {
	ctx, u := r.Context(), userFrom(r.Context())
	if u == nil {
		return nil, nil
	}
	gs, _, err := s.store.ListGames(ctx, store.GameFilter{UserID: u.ID, Played: true, Sort: "played", Limit: widgetRows})
	if err != nil {
		return nil, err
	}
	if len(gs) == 0 {
		return nil, nil
	}
	s.decorateAll(u, gs)
	now := time.Now()
	w := &core.WidgetData{URL: "/"}
	for _, g := range gs {
		played := ""
		if when := whenWords(g.Mine.LastPlayedAt, now); when != "" {
			played = "played " + when
		}
		w.Items = append(w.Items, core.WidgetItem{ID: strconv.FormatInt(g.ID, 10), Title: g.Name, Detail: dots(g.PlatformName, played), URL: gameURL(g.ID)})
	}
	sec, _, _, _, err := s.store.TotalPlay(ctx, u.ID, now.AddDate(0, 0, -6).Format("2006-01-02"), now.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	if sec >= 60 {
		w.Metrics = []core.Metric{{Label: "This week", Value: playWords(sec)}}
	}
	return w, nil
}
