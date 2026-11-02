package library

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-games/internal/platforms"
)

// Layout says where games and firmware live inside the library folder.
type Layout struct {
	Root  string // the library folder
	Roms  string // one folder per platform
	Bios  string // bios/<platform>/
	Trash string // deleted games wait here
	Temp  string // uploads in progress (same disk, so finishing is a rename)
}

// skipRoot are folders at the top of the library that are not platforms.
var skipRoot = map[string]bool{"bios": true, "roms": true, "inbox": true, "_failed": true, "trash": true, "lost+found": true}

// Detect finds the layout: RomM's "roms/<platform>" + "bios/<platform>" when a
// roms folder exists, else platforms directly in the library folder.
func Detect(root string) Layout {
	l := Layout{Root: root, Roms: root, Bios: filepath.Join(root, "bios"), Trash: filepath.Join(root, ".trash"), Temp: filepath.Join(root, ".uploads")}
	if fi, err := os.Stat(filepath.Join(root, "roms")); err == nil && fi.IsDir() {
		l.Roms = filepath.Join(root, "roms")
	}
	return l
}

// PlatformDir is a platform folder found on disk.
type PlatformDir struct {
	Slug  string
	Games []Entry
}

// File is one file of a game.
type File struct {
	Rel   string // relative to the game's base folder
	Size  int64
	MTime time.Time
}

// Entry is one game found on disk.
type Entry struct {
	FSName   string // file or folder name inside the platform folder
	IsFolder bool
	Main     string // the file to open, relative to the base folder
	Files    []File
	Size     int64
	MTime    time.Time
}

// Base is the folder the entry's file paths are relative to.
func (e Entry) Base(platformDir string) string {
	if e.IsFolder {
		return filepath.Join(platformDir, e.FSName)
	}
	return platformDir
}

func hidden(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || strings.EqualFold(name, "Thumbs.db") || strings.EqualFold(name, "desktop.ini")
}

// ListPlatforms returns the platform folders (empty ones too).
func (l Layout) ListPlatforms() ([]string, error) {
	des, err := os.ReadDir(l.Roms)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, d := range des {
		n := d.Name()
		if !d.IsDir() || hidden(n) || (l.Roms == l.Root && skipRoot[strings.ToLower(n)]) {
			continue
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

// ScanPlatform reads one platform folder.
func (l Layout) ScanPlatform(slug string) (PlatformDir, error) {
	p := platforms.Get(slug)
	dir := filepath.Join(l.Roms, slug)
	des, err := os.ReadDir(dir)
	if err != nil {
		return PlatformDir{Slug: slug}, err
	}
	out := PlatformDir{Slug: slug}
	var loose []File
	for _, d := range des {
		n := d.Name()
		if hidden(n) {
			continue
		}
		if d.IsDir() {
			if e, ok := scanFolder(p, dir, n); ok {
				out.Games = append(out.Games, e)
			}
			continue
		}
		fi, err := d.Info()
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		loose = append(loose, File{Rel: n, Size: fi.Size(), MTime: fi.ModTime()})
	}
	out.Games = append(out.Games, groupLoose(p, loose)...)
	sort.Slice(out.Games, func(i, j int) bool {
		return strings.ToLower(out.Games[i].FSName) < strings.ToLower(out.Games[j].FSName)
	})
	return out, nil
}

// scanFolder makes one game out of a folder (multi-disc sets, cue/bin, a
// CHD with its .sbi). A folder without a single game file is skipped.
func scanFolder(p platforms.Platform, platformDir, name string) (Entry, bool) {
	base := filepath.Join(platformDir, name)
	e := Entry{FSName: name, IsFolder: true}
	_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path != base && hidden(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil || !fi.Mode().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(base, path)
		e.Files = append(e.Files, File{Rel: filepath.ToSlash(rel), Size: fi.Size(), MTime: fi.ModTime()})
		e.Size += fi.Size()
		if fi.ModTime().After(e.MTime) {
			e.MTime = fi.ModTime()
		}
		return nil
	})
	sort.Slice(e.Files, func(i, j int) bool { return e.Files[i].Rel < e.Files[j].Rel })
	e.Main = PickMain(p, e.Files)
	return e, e.Main != ""
}

// mainRank: lower wins. Playlists first (multi-disc), then single-file
// images, then sheets that point at track files.
var mainRank = map[string]int{"m3u": 0, "chd": 1, "rvz": 1, "pbp": 1, "cso": 1, "iso": 2, "gdi": 3, "cue": 3, "ccd": 4, "cdi": 4, "mds": 4, "toc": 5}

// PickMain chooses the file an emulator should open.
func PickMain(p platforms.Platform, files []File) string {
	best, bestRank := "", 99
	var bestSize int64
	for _, f := range files {
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(f.Rel), "."))
		rank, sheet := mainRank[ext]
		if !platforms.GameExtension(p, ext) && !(sheet && p.Disc) {
			continue
		}
		if !sheet {
			rank = 6
		}
		// a .bin next to a .cue is a track, not the game
		if ext == "bin" || ext == "img" {
			rank = 7
		}
		if rank < bestRank || (rank == bestRank && f.Size > bestSize) {
			best, bestRank, bestSize = f.Rel, rank, f.Size
		}
	}
	return best
}

// groupLoose turns the files lying directly in a platform folder into games:
// "Game (Track 1).bin" … and "Game.cue" become one, a ".sbi" joins its
// image, every other game file is a game of its own.
func groupLoose(p platforms.Platform, files []File) []Entry {
	groups := map[string][]File{}
	var order []string
	for _, f := range files {
		key := strings.ToLower(reTrack.ReplaceAllString(StripExt(f.Rel), ""))
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], f)
	}
	var out []Entry
	for _, key := range order {
		g := groups[key]
		main := PickMain(p, g)
		if main == "" {
			continue
		}
		mainExt := strings.ToLower(strings.TrimPrefix(filepath.Ext(main), "."))
		sheet := mainExt == "cue" || mainExt == "m3u" || mainExt == "gdi" || mainExt == "ccd" || mainExt == "toc"
		if sheet || len(g) == 1 || onlyCompanions(g, main) {
			e := Entry{FSName: main, Main: main, Files: g}
			for _, f := range g {
				e.Size += f.Size
				if f.MTime.After(e.MTime) {
					e.MTime = f.MTime
				}
			}
			out = append(out, e)
			continue
		}
		// same name, different game files (e.g. "Game.gba" and "Game.zip"): separate games
		for _, f := range g {
			ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(f.Rel), "."))
			if !platforms.GameExtension(p, ext) {
				continue
			}
			out = append(out, Entry{FSName: f.Rel, Main: f.Rel, Files: []File{f}, Size: f.Size, MTime: f.MTime})
		}
	}
	return out
}

func onlyCompanions(g []File, main string) bool {
	for _, f := range g {
		if f.Rel == main {
			continue
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(f.Rel), "."))
		if ext != "sbi" && ext != "sub" && ext != "ips" && ext != "bps" && ext != "ups" && ext != "xdelta" {
			return false
		}
	}
	return true
}

// FirmwareFile is a file in bios/<platform>/.
type FirmwareFile struct {
	Name  string
	Size  int64
	MTime time.Time
}

// ScanFirmware lists bios/<platform>/ (empty when there is none).
func (l Layout) ScanFirmware() map[string][]FirmwareFile {
	out := map[string][]FirmwareFile{}
	des, err := os.ReadDir(l.Bios)
	if err != nil {
		return out
	}
	for _, d := range des {
		if !d.IsDir() || hidden(d.Name()) {
			continue
		}
		files, _ := os.ReadDir(filepath.Join(l.Bios, d.Name()))
		for _, f := range files {
			if f.IsDir() || hidden(f.Name()) {
				continue
			}
			fi, err := f.Info()
			if err != nil || !fi.Mode().IsRegular() {
				continue
			}
			out[d.Name()] = append(out[d.Name()], FirmwareFile{Name: f.Name(), Size: fi.Size(), MTime: fi.ModTime()})
		}
	}
	return out
}

// Hashes are the checksums of one file.
type Hashes struct{ CRC32, MD5, SHA1 string }

// HashFile reads a file once for CRC32, MD5 and SHA-1.
func HashFile(path string) (Hashes, error) {
	f, err := os.Open(path)
	if err != nil {
		return Hashes{}, err
	}
	defer f.Close()
	c, m, s := crc32.NewIEEE(), md5.New(), sha1.New()
	if _, err := io.Copy(io.MultiWriter(c, m, s), f); err != nil {
		return Hashes{}, err
	}
	return Hashes{CRC32: hex.EncodeToString(c.Sum(nil)), MD5: hex.EncodeToString(m.Sum(nil)), SHA1: hex.EncodeToString(s.Sum(nil))}, nil
}

// SafeName makes a file or folder name safe to create: no path separators,
// no control characters, no leading dots.
func SafeName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\' || r == 0 || r < 32:
			return '_'
		case strings.ContainsRune(`<>:"|?*`, r):
			return '_'
		}
		return r
	}, strings.TrimSpace(s))
	s = strings.TrimLeft(s, ". ")
	if len(s) > 200 {
		ext := filepath.Ext(s)
		s = s[:200-len(ext)] + ext
	}
	return s
}

// SafeRel cleans a relative path ("Folder/file.bin") so it stays inside its base.
func SafeRel(rel string) string {
	var parts []string
	for _, p := range strings.Split(filepath.ToSlash(rel), "/") {
		if p = SafeName(p); p != "" && p != ".." {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "/")
}
