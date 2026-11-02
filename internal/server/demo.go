package server

import (
	"context"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-games/internal/store"
)

// demoGames are placeholder files with real game names (so covers and
// descriptions are found when the box is online). They do not play.
var demoGames = map[string][]string{
	"snes":    {"Super Mario World (USA).sfc", "Legend of Zelda, The - A Link to the Past (USA).sfc", "Super Metroid (Japan, USA) (En,Ja).sfc", "Chrono Trigger (USA).sfc", "Donkey Kong Country (USA) (Rev 2).sfc", "Secret of Mana (USA).sfc", "F-Zero (USA).sfc", "EarthBound (USA).sfc"},
	"gba":     {"Metroid Fusion (USA).gba", "Advance Wars (USA).gba", "Golden Sun (USA, Europe).gba", "Mario Kart - Super Circuit (USA).gba", "Castlevania - Aria of Sorrow (USA).gba", "Pokemon - Emerald Version (USA, Europe).gba"},
	"nes":     {"Super Mario Bros. 3 (USA).nes", "Mega Man 2 (USA).nes", "Metroid (USA).nes", "Punch-Out!! (USA).nes", "Castlevania (USA).nes"},
	"genesis": {"Sonic The Hedgehog 2 (World).md", "Streets of Rage 2 (USA).md", "Gunstar Heroes (USA).md", "Phantasy Star IV (USA).md"},
	"n64":     {"Super Mario 64 (USA).z64", "Legend of Zelda, The - Ocarina of Time (USA).z64", "GoldenEye 007 (USA).z64", "Mario Kart 64 (USA).z64"},
	"gb":      {"Tetris (World) (Rev 1).gb", "Legend of Zelda, The - Link's Awakening (USA, Europe) (Rev 2).gb"},
	"psx":     {"Final Fantasy VII (USA)/", "Castlevania - Symphony of the Night (USA)/", "Crash Bandicoot (USA)/", "Silent Hill (USA)/", "Metal Gear Solid (USA)/"},
	"ps2":     {"Shadow of the Colossus (USA)/", "Kingdom Hearts (USA)/", "Silent Hill 2 (USA)/", "Okami (USA)/"},
	"nds":     {"Mario Kart DS (USA, Australia) (En,Fr,De,Es,It).nds", "Professor Layton and the Curious Village (USA).nds"},
	"ngc":     {"Metroid Prime (USA).rvz", "Legend of Zelda, The - The Wind Waker (USA).rvz", "Resident Evil 4 (USA).rvz"},
}

// seedDemo fills an empty library with placeholder games and the owner's
// account with months of play, statuses, collections and saves.
func (s *Server) seedDemo(ctx context.Context, uid int64) error {
	if _, total, _ := s.store.ListGames(ctx, store.GameFilter{UserID: uid, Missing: true, Limit: 1}); total > 0 {
		return nil
	}
	rnd := rand.New(rand.NewSource(42))
	for slug, names := range demoGames {
		dir := s.platformDir(slug)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		for _, n := range names {
			junk := make([]byte, 2048+rnd.Intn(4096))
			rnd.Read(junk)
			if folder, ok := strings.CutSuffix(n, "/"); ok {
				gdir := filepath.Join(dir, folder)
				_ = os.MkdirAll(gdir, 0o755)
				_ = os.WriteFile(filepath.Join(gdir, folder+".chd"), junk, 0o644)
				continue
			}
			_ = os.WriteFile(filepath.Join(dir, n), junk, 0o644)
		}
	}
	if err := s.scanAll(ctx, nil); err != nil {
		return err
	}
	games, _, err := s.store.ListGames(ctx, store.GameFilter{UserID: uid})
	if err != nil {
		return err
	}
	byName := map[string]int64{}
	for _, g := range games {
		byName[g.Name] = g.ID
	}
	set := func(name, status string, fav bool, rating int) {
		if id := byName[name]; id > 0 {
			_ = s.store.PatchUserGame(ctx, uid, id, store.UserGamePatch{Status: &status, Favorite: &fav, Rating: &rating})
		}
	}
	set("Super Metroid", "completed", true, 10)
	set("Chrono Trigger", "finished", true, 9)
	set("The Legend of Zelda: A Link to the Past", "finished", true, 10)
	set("Castlevania: Symphony of the Night", "playing", true, 0)
	set("Metroid Fusion", "playing", false, 0)
	set("Golden Sun", "backlog", false, 0)
	set("EarthBound", "backlog", false, 0)
	set("Phantasy Star IV", "backlog", false, 0)
	set("Okami", "backlog", false, 0)
	set("Shadow of the Colossus", "finished", true, 9)
	set("Mega Man 2", "dropped", false, 6)
	set("Silent Hill 2", "playing", false, 0)

	// three months of evenings
	played := []string{"Castlevania: Symphony of the Night", "Metroid Fusion", "Super Metroid", "Chrono Trigger", "Silent Hill 2", "Tetris", "Sonic The Hedgehog 2", "Super Mario World", "Mario Kart 64", "Shadow of the Colossus"}
	now := time.Now()
	for d := 90; d >= 0; d-- {
		if rnd.Float64() < 0.45 {
			continue
		}
		day := now.AddDate(0, 0, -d)
		for i := 0; i < 1+rnd.Intn(2); i++ {
			name := played[rnd.Intn(len(played))]
			if d < 14 && rnd.Float64() < 0.6 {
				name = played[rnd.Intn(2)] // lately mostly the current ones
			}
			id := byName[name]
			if id == 0 {
				continue
			}
			start := time.Date(day.Year(), day.Month(), day.Day(), 18+rnd.Intn(4), rnd.Intn(60), 0, 0, time.Local)
			if start.After(now) {
				start = now.Add(-2 * time.Hour)
			}
			device := "browser"
			if rnd.Float64() < 0.35 {
				device = "Argosy (Odin 2)"
			}
			_ = s.store.AddPlayTime(ctx, uid, id, start, int64((12+rnd.Intn(80))*60), device)
		}
	}
	mk := func(name, desc string, titles ...string) {
		cid, err := s.store.CreateCollection(ctx, uid, name, desc)
		if err != nil {
			return
		}
		var ids []int64
		for _, t := range titles {
			if id := byName[t]; id > 0 {
				ids = append(ids, id)
			}
		}
		_ = s.store.SetCollectionGames(ctx, uid, cid, ids)
	}
	mk("Metroidvanias", "Explore, find the upgrade, go back.", "Super Metroid", "Metroid Fusion", "Castlevania: Symphony of the Night", "Castlevania: Aria of Sorrow", "Metroid")
	mk("Couch co-op", "For when friends are over.", "Mario Kart 64", "Streets of Rage 2", "Gunstar Heroes", "Secret of Mana", "GoldenEye 007")
	mk("Horror nights", "", "Silent Hill", "Silent Hill 2", "Resident Evil 4")

	// a few saves and states
	for _, name := range []string{"Castlevania: Symphony of the Night", "Metroid Fusion", "Super Metroid"} {
		g, err := s.store.GetGame(ctx, uid, byName[name])
		if err != nil {
			continue
		}
		data := make([]byte, 8192)
		rnd.Read(data)
		_, _ = s.storeSave(ctx, uid, g, "save", "", "", "auto", "browser", data, nil)
		_, _ = s.storeSave(ctx, uid, g, "state", "", "", "", "browser", data[:4096], nil)
	}
	s.log.Info("demo data seeded", "games", len(games))
	return nil
}
