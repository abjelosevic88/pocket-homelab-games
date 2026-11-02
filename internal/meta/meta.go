// Package meta looks games up without any API key: box art, title screens
// and in-game pictures from the libretro thumbnail sets, descriptions from
// Wikipedia and the facts (genre, developer, publisher, release date, modes)
// from Wikidata.
package meta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/abjelosevic88/pocket-homelab-games/internal/library"
)

// Client does the lookups. Indexes of the thumbnail sets are kept in memory
// for a day (one request per system).
type Client struct {
	HTTP      *http.Client
	UserAgent string

	mu    sync.Mutex
	index map[string]indexEntry
}

type indexEntry struct {
	at    time.Time
	names []string
}

// New makes a client.
func New(ua string) *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Second}, UserAgent: ua, index: map[string]indexEntry{}}
}

func (c *Client) get(ctx context.Context, u string, limit int64) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	return b, resp.StatusCode, err
}

// ---------- libretro thumbnails ----------

const thumbsBase = "https://thumbnails.libretro.com/"

// Picture kinds in the thumbnail sets.
const (
	Boxart = "Named_Boxarts"
	Title  = "Named_Titles"
	Snap   = "Named_Snaps"
)

var reHref = regexp.MustCompile(`href="([^"?/][^"]*\.png)"`)

// names lists the pictures of one kind for a system (file names without .png).
func (c *Client) names(ctx context.Context, system, kind string) ([]string, error) {
	key := system + "/" + kind
	c.mu.Lock()
	if e, ok := c.index[key]; ok && time.Since(e.at) < 24*time.Hour {
		c.mu.Unlock()
		return e.names, nil
	}
	c.mu.Unlock()
	b, _, err := c.get(ctx, thumbsBase+url.PathEscape(system)+"/"+kind+"/", 64<<20)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, m := range reHref.FindAllStringSubmatch(string(b), -1) {
		n, err := url.PathUnescape(m[1])
		if err != nil {
			continue
		}
		names = append(names, html.UnescapeString(strings.TrimSuffix(n, ".png")))
	}
	c.mu.Lock()
	c.index[key] = indexEntry{at: time.Now(), names: names}
	c.mu.Unlock()
	return names, nil
}

// Candidate is a picture that may show the game.
type Candidate struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Score int    `json:"score"`
}

// ThumbURL is the address of one picture.
func ThumbURL(system, kind, name string) string {
	// the sets replace characters that cannot be in file names
	safe := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`&*/:`+"`"+`<>?\|"`, r) {
			return '_'
		}
		return r
	}, name)
	return thumbsBase + url.PathEscape(system) + "/" + kind + "/" + url.PathEscape(safe) + ".png"
}

// Match finds the pictures whose name fits the game best. fsName is the
// file or folder name (often exactly the set's name); title the clean one.
func (c *Client) Match(ctx context.Context, system, kind, fsName, title string, regions []string, n int) ([]Candidate, error) {
	if system == "" {
		return nil, errors.New("no picture set for this system")
	}
	names, err := c.names(ctx, system, kind)
	if err != nil {
		return nil, err
	}
	exact := library.StripExt(fsName)
	want := library.Normalize(title)
	wantWords := strings.Fields(want)
	var out []Candidate
	for _, name := range names {
		score := 0
		if name == exact {
			score = 1000
		} else {
			p := library.Parse(name)
			got := library.Normalize(p.Title)
			switch {
			case got == want:
				score = 500
			case strings.HasPrefix(got, want+" ") || strings.HasPrefix(want, got+" "):
				score = 200 - abs(len(got)-len(want))
			default:
				common := overlap(wantWords, strings.Fields(got))
				if common == 0 || common*3 < len(wantWords)*2 {
					continue
				}
				score = 100*common/max(len(wantWords), len(strings.Fields(got))) - 10
			}
			for _, r := range p.Regions {
				for _, w := range regions {
					if r == w {
						score += 20
					}
				}
			}
			if len(p.Tags) > 0 {
				score -= 5 * len(p.Tags) // prefer the plain release over betas and demos
			}
		}
		out = append(out, Candidate{Name: name, URL: ThumbURL(system, kind, name), Score: score})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > n {
		out = out[:n]
	}
	return out, nil
}

// Search lists the pictures of a system whose names contain every word of q.
func (c *Client) Search(ctx context.Context, system, kind, q string, n int) ([]Candidate, error) {
	names, err := c.names(ctx, system, kind)
	if err != nil {
		return nil, err
	}
	words := strings.Fields(library.Normalize(q))
	var out []Candidate
	for _, name := range names {
		got := library.Normalize(name)
		ok := true
		for _, w := range words {
			if !strings.Contains(got, w) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, Candidate{Name: name, URL: ThumbURL(system, kind, name)})
			if len(out) >= n {
				break
			}
		}
	}
	return out, nil
}

// Fetch downloads a picture (max 15 MB).
func (c *Client) Fetch(ctx context.Context, u string) ([]byte, error) {
	b, _, err := c.get(ctx, u, 15<<20)
	return b, err
}

func overlap(a, b []string) int {
	set := map[string]bool{}
	for _, w := range b {
		set[w] = true
	}
	n := 0
	for _, w := range a {
		if set[w] {
			n++
		}
	}
	return n
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// ---------- Wikipedia + Wikidata ----------

// Facts is what Wikipedia and Wikidata know about a game.
type Facts struct {
	WikiTitle   string
	URL         string
	Summary     string
	Genres      []string
	Developers  []string
	Publishers  []string
	ReleaseDate string // YYYY-MM-DD / YYYY-MM / YYYY
	Modes       []string
	ImageURL    string
}

// ErrNoMatch means nothing on Wikipedia fits.
var ErrNoMatch = errors.New("no matching article")

// Lookup finds the game's article. Candidates from two searches are
// scored: the article's title must be the game's title (or contain all its
// words, or the summary must name it), and an article that mentions the
// system wins (the 2001 "Silent Hill 2" for PS2, not the 2024 remake).
func (c *Client) Lookup(ctx context.Context, title, system string) (*Facts, error) {
	hint := system
	if i := strings.Index(hint, " ("); i > 0 {
		hint = hint[:i]
	}
	queries := []string{title + " " + hint + " video game", title + " video game"}
	want := library.Normalize(title)
	wantWords := strings.Fields(want)
	seen := map[string]bool{}
	var best *Facts
	bestScore := 0
	for _, q := range queries {
		titles, err := c.wikiSearch(ctx, q)
		if err != nil {
			return nil, err
		}
		for _, t := range titles {
			if seen[t] {
				continue
			}
			seen[t] = true
			base := t
			if i := strings.Index(base, " ("); i > 0 {
				base = base[:i]
			}
			got := library.Normalize(base)
			gotWords := strings.Fields(got)
			score := 0
			switch {
			case got == want:
				score = 100
			case overlap(wantWords, gotWords) == len(wantWords) && len(gotWords) <= len(wantWords)+2:
				score = 60
			}
			f, err := c.summary(ctx, t)
			if err != nil || f == nil {
				continue
			}
			lead := library.Normalize(firstChars(f.Summary, 400))
			if score == 0 && strings.Contains(" "+lead+" ", " "+want+" ") {
				score = 50
			}
			if score == 0 {
				continue
			}
			if hint != "" && mentions(f.Summary, hint) {
				score += 30
			}
			if score > bestScore {
				best, bestScore = f, score
			}
			if score >= 130 {
				return best, nil
			}
		}
	}
	if best == nil {
		return nil, ErrNoMatch
	}
	return best, nil
}

func firstChars(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// mentions reports whether text names the system: "PlayStation" must not be
// followed by a number ("PlayStation 5" is another system), "PlayStation 2"
// matches only itself.
func mentions(text, system string) bool {
	low, sys := strings.ToLower(text), strings.ToLower(system)
	for i := 0; ; {
		j := strings.Index(low[i:], sys)
		if j < 0 {
			return false
		}
		k := i + j + len(sys)
		rest := strings.TrimLeft(low[k:], " ")
		if rest == "" || rest[0] < '0' || rest[0] > '9' || strings.HasSuffix(sys, " ") {
			return true
		}
		i = k
	}
}

// Article reads one article by its exact title (a correction typed by hand).
func (c *Client) Article(ctx context.Context, title string) (*Facts, error) {
	f, err := c.summary(ctx, title)
	if err == nil && f == nil {
		err = ErrNoMatch
	}
	return f, err
}

func (c *Client) wikiSearch(ctx context.Context, q string) ([]string, error) {
	u := "https://en.wikipedia.org/w/api.php?action=query&list=search&format=json&srlimit=5&srsearch=" + url.QueryEscape(q)
	b, _, err := c.get(ctx, u, 1<<20)
	if err != nil {
		return nil, err
	}
	var r struct {
		Query struct {
			Search []struct {
				Title string `json:"title"`
			} `json:"search"`
		} `json:"query"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	var out []string
	for _, s := range r.Query.Search {
		out = append(out, s.Title)
	}
	return out, nil
}

func (c *Client) summary(ctx context.Context, title string) (*Facts, error) {
	u := "https://en.wikipedia.org/api/rest_v1/page/summary/" + url.PathEscape(strings.ReplaceAll(title, " ", "_"))
	b, status, err := c.get(ctx, u, 1<<20)
	if status == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r struct {
		Type        string `json:"type"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Extract     string `json:"extract"`
		Wikibase    string `json:"wikibase_item"`
		ContentURLs struct {
			Desktop struct {
				Page string `json:"page"`
			} `json:"desktop"`
		} `json:"content_urls"`
		Original struct {
			Source string `json:"source"`
		} `json:"originalimage"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	d := strings.ToLower(r.Description + " " + firstSentence(r.Extract))
	if r.Type != "standard" || !(strings.Contains(d, "video game") || strings.Contains(d, "game for") || strings.Contains(d, "game developed") || strings.Contains(d, "game published")) {
		return nil, nil
	}
	f := &Facts{WikiTitle: r.Title, URL: r.ContentURLs.Desktop.Page, Summary: r.Extract, ImageURL: r.Original.Source}
	if r.Wikibase != "" {
		c.wikidata(ctx, r.Wikibase, f)
	}
	return f, nil
}

func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i > 0 {
		return s[:i]
	}
	return s
}

// wikidata adds genre, developer, publisher, release date and modes.
func (c *Client) wikidata(ctx context.Context, id string, f *Facts) {
	b, _, err := c.get(ctx, "https://www.wikidata.org/wiki/Special:EntityData/"+url.PathEscape(id)+".json", 8<<20)
	if err != nil {
		return
	}
	var r struct {
		Entities map[string]struct {
			Claims map[string][]struct {
				Mainsnak struct {
					Datavalue struct {
						Value json.RawMessage `json:"value"`
					} `json:"datavalue"`
				} `json:"mainsnak"`
			} `json:"claims"`
		} `json:"entities"`
	}
	if json.Unmarshal(b, &r) != nil {
		return
	}
	ent, ok := r.Entities[id]
	if !ok {
		return
	}
	ids := func(prop string) []string {
		var out []string
		for _, cl := range ent.Claims[prop] {
			var v struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(cl.Mainsnak.Datavalue.Value, &v) == nil && v.ID != "" {
				out = append(out, v.ID)
			}
		}
		return out
	}
	genres, devs, pubs, modes := ids("P136"), ids("P178"), ids("P123"), ids("P404")
	// the earliest publication date
	for _, cl := range ent.Claims["P577"] {
		var v struct {
			Time      string `json:"time"`
			Precision int    `json:"precision"`
		}
		if json.Unmarshal(cl.Mainsnak.Datavalue.Value, &v) != nil || len(v.Time) < 11 {
			continue
		}
		d := strings.TrimPrefix(v.Time, "+")[:10] // YYYY-MM-DD
		switch {
		case v.Precision <= 9:
			d = d[:4]
		case v.Precision == 10:
			d = d[:7]
		}
		if f.ReleaseDate == "" || d < f.ReleaseDate {
			f.ReleaseDate = d
		}
	}
	all := append(append(append(append([]string{}, genres...), devs...), pubs...), modes...)
	if len(all) == 0 {
		return
	}
	labels := c.labels(ctx, all)
	pick := func(list []string, n int) []string {
		var out []string
		for _, id := range list {
			if l := labels[id]; l != "" {
				out = append(out, l)
			}
			if len(out) >= n {
				break
			}
		}
		return out
	}
	f.Genres, f.Developers, f.Publishers, f.Modes = pick(genres, 4), pick(devs, 3), pick(pubs, 3), pick(modes, 4)
	for i, g := range f.Genres {
		f.Genres[i] = strings.ToUpper(g[:1]) + g[1:]
	}
	for i, m := range f.Modes {
		f.Modes[i] = strings.ToUpper(m[:1]) + m[1:]
	}
}

func (c *Client) labels(ctx context.Context, ids []string) map[string]string {
	out := map[string]string{}
	if len(ids) > 50 {
		ids = ids[:50]
	}
	u := "https://www.wikidata.org/w/api.php?action=wbgetentities&props=labels&languages=en&format=json&ids=" + url.QueryEscape(strings.Join(ids, "|"))
	b, _, err := c.get(ctx, u, 4<<20)
	if err != nil {
		return out
	}
	var r struct {
		Entities map[string]struct {
			Labels map[string]struct {
				Value string `json:"value"`
			} `json:"labels"`
		} `json:"entities"`
	}
	if json.Unmarshal(b, &r) != nil {
		return out
	}
	for id, e := range r.Entities {
		if l, ok := e.Labels["en"]; ok {
			out[id] = l.Value
		}
	}
	return out
}
