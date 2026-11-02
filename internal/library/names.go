// Package library reads the game folder: names, files, hashes and archives.
package library

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// Parsed is what a No-Intro / Redump style file name says about a game.
type Parsed struct {
	Title     string   // "Silent Hill 2"
	Regions   []string // ["Europe"]
	Languages []string // ["En", "Ja", "Fr"]
	Tags      []string // ["Rev 1", "Disc 1"]
	Disc      int      // 1-based disc number, 0 = not a disc set
}

var regions = map[string]string{
	"usa": "USA", "us": "USA", "u": "USA", "america": "USA", "north america": "USA",
	"europe": "Europe", "eu": "Europe", "e": "Europe", "pal": "Europe",
	"japan": "Japan", "jp": "Japan", "j": "Japan", "jpn": "Japan",
	"world": "World", "w": "World",
	"australia": "Australia", "korea": "Korea", "k": "Korea", "china": "China", "brazil": "Brazil",
	"france": "France", "germany": "Germany", "spain": "Spain", "italy": "Italy", "asia": "Asia",
	"canada": "Canada", "netherlands": "Netherlands", "sweden": "Sweden", "uk": "UK", "russia": "Russia",
	"taiwan": "Taiwan", "hong kong": "Hong Kong", "scandinavia": "Scandinavia", "portugal": "Portugal",
	"poland": "Poland", "greece": "Greece", "denmark": "Denmark", "norway": "Norway", "finland": "Finland",
	"latin america": "Latin America", "mexico": "Mexico", "india": "India", "new zealand": "New Zealand",
}

var (
	reGroup    = regexp.MustCompile(`\s*[\(\[]([^\)\]]*)[\)\]]`)
	reLang     = regexp.MustCompile(`^[A-Z][a-z](-[A-Z][a-z])?$`)
	reDisc     = regexp.MustCompile(`(?i)^(disc|disk|cd)\s*([0-9]+)`)
	reTrack    = regexp.MustCompile(`(?i)\s*\(track\s*[0-9]+\)$`)
	reSpaces   = regexp.MustCompile(`\s+`)
	reArticle  = regexp.MustCompile(`^(.*), (The|A|An|Die|Der|Das|Le|La|Les|El|Il)(\b.*)$`)
	reSortSkip = regexp.MustCompile(`(?i)^(the|a|an)\s+`)
)

// StripExt removes a file extension unless the name is a folder (no dot
// followed by a short alphanumeric tail).
func StripExt(name string) string {
	ext := filepath.Ext(name)
	if ext == "" || len(ext) > 8 || strings.ContainsAny(ext, " ()[]") {
		return name
	}
	return strings.TrimSuffix(name, ext)
}

// Parse reads a game's file or folder name.
func Parse(name string) Parsed {
	base := strings.TrimSpace(reTrack.ReplaceAllString(StripExt(name), ""))
	var p Parsed
	for _, m := range reGroup.FindAllStringSubmatch(base, -1) {
		inner := strings.TrimSpace(m[1])
		if inner == "" || inner == "!" {
			continue
		}
		parts := splitList(inner)
		switch {
		case allRegions(parts):
			for _, r := range parts {
				p.Regions = appendUniq(p.Regions, regions[strings.ToLower(r)])
			}
		case allLangs(parts):
			for _, l := range parts {
				p.Languages = appendUniq(p.Languages, l)
			}
		default:
			if d := reDisc.FindStringSubmatch(inner); d != nil {
				p.Disc = atoi(d[2])
			}
			p.Tags = append(p.Tags, inner)
		}
	}
	title := reGroup.ReplaceAllString(base, "")
	title = strings.ReplaceAll(title, "_", " ")
	title = reSpaces.ReplaceAllString(strings.TrimSpace(title), " ")
	// "Legend of Zelda, The - A Link to the Past" → "The Legend of Zelda - A Link …"
	if m := reArticle.FindStringSubmatch(title); m != nil {
		title = m[2] + " " + m[1] + m[3]
	}
	// No-Intro writes a subtitle's colon as " - "
	title = strings.ReplaceAll(title, " - ", ": ")
	if title == "" {
		title = base
	}
	p.Title = title
	return p
}

// SortName is the title without a leading article, lower case.
func SortName(title string) string {
	s := strings.ToLower(reSortSkip.ReplaceAllString(title, ""))
	return strings.TrimSpace(s)
}

// Normalize makes titles comparable: lower case letters and digits only,
// roman numerals II–X turned into digits, "&" into "and".
func Normalize(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "&", " and ")
	s = strings.ReplaceAll(s, "'", "")
	var b strings.Builder
	words := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	for i, w := range words {
		if i == 0 && (w == "the" || w == "a" || w == "an") && len(words) > 1 {
			continue
		}
		if r, ok := roman[w]; ok && i > 0 {
			w = r
		}
		b.WriteString(w)
		b.WriteByte(' ')
	}
	return strings.TrimSpace(b.String())
}

var roman = map[string]string{"ii": "2", "iii": "3", "iv": "4", "v": "5", "vi": "6", "vii": "7", "viii": "8", "ix": "9", "x": "10"}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '+' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func allRegions(parts []string) bool {
	if len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		if _, ok := regions[strings.ToLower(p)]; !ok {
			return false
		}
		if len(p) == 1 && len(parts) == 1 && !strings.ContainsAny(p, "UEJWK") {
			return false
		}
	}
	return true
}

func allLangs(parts []string) bool {
	if len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		if !reLang.MatchString(p) {
			return false
		}
	}
	return true
}

func appendUniq(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}
