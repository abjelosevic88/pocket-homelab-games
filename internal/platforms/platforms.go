// Package platforms knows the game systems: their names, file types, which
// can be played in the browser (and with which EmulatorJS system), the BIOS
// files they need and how the libretro thumbnail sets call them.
//
// Slugs are the folder names RomM uses (IGDB style: ps2, psx, gba, genesis …),
// so an existing RomM library works unchanged.
package platforms

import (
	"sort"
	"strings"
)

// Bios is one firmware file a system wants.
type Bios struct {
	File     string `json:"file"`     // the name emulators look for
	MD5      string `json:"md5"`      // a known good dump ('' = any)
	Required bool   `json:"required"` // the game will not start without it
	Any      bool   `json:"any"`      // any file in the folder counts (dumps differ per console)
	Note     string `json:"note"`
}

// Platform describes one system.
type Platform struct {
	Slug     string   `json:"slug"`
	Name     string   `json:"name"`
	Short    string   `json:"short"`
	Maker    string   `json:"maker"`
	Year     int      `json:"year"`
	Kind     string   `json:"kind"` // console | handheld | computer | arcade | other
	Ext      []string `json:"extensions"`
	Disc     bool     `json:"disc"`     // games come on discs (folders of files, cue/bin, chd)
	System   string   `json:"system"`   // EmulatorJS system ('' = not in the browser)
	Cores    []string `json:"cores"`    // EmulatorJS cores to choose from (first = default)
	Threads  bool     `json:"threads"`  // the browser core needs SharedArrayBuffer
	Libretro string   `json:"libretro"` // libretro-thumbnails folder
	Wiki     string   `json:"wiki"`     // how Wikipedia calls the system (search hint)
	Bios     []Bios   `json:"bios"`
	Aliases  []string `json:"-"`       // other folder names for the same system
	IGDB     int      `json:"igdb_id"` // RomM apps look systems up by IGDB id
	Minor    bool     `json:"-"`       // a variant: a file that also fits the main system goes there
}

// Browser reports whether games of this system play in the browser.
func (p Platform) Browser() bool { return p.System != "" }

// NeedsBios reports whether at least one BIOS file is required.
func (p Platform) NeedsBios() bool {
	for _, b := range p.Bios {
		if b.Required {
			return true
		}
	}
	return false
}

var all = []Platform{
	// Nintendo
	{Slug: "nes", Name: "Nintendo Entertainment System", Short: "NES", Maker: "Nintendo", Year: 1983, Kind: "console", Ext: []string{"nes", "unf", "unif"}, System: "nes", Cores: []string{"fceumm", "nestopia"}, Libretro: "Nintendo - Nintendo Entertainment System", Wiki: "NES", IGDB: 18},
	{Slug: "famicom", Minor: true, Name: "Family Computer", Short: "Famicom", Maker: "Nintendo", Year: 1983, Kind: "console", Ext: []string{"nes"}, System: "nes", Cores: []string{"fceumm", "nestopia"}, Libretro: "Nintendo - Nintendo Entertainment System", Wiki: "Famicom", IGDB: 99},
	{Slug: "fds", Name: "Famicom Disk System", Short: "FDS", Maker: "Nintendo", Year: 1986, Kind: "console", Ext: []string{"fds"}, System: "nes", Cores: []string{"fceumm", "nestopia"}, Libretro: "Nintendo - Family Computer Disk System", Wiki: "Famicom Disk System", IGDB: 51,
		Bios: []Bios{{File: "disksys.rom", MD5: "ca30b50f880eb660a320674ed365ef7a", Required: true}}},
	{Slug: "snes", Name: "Super Nintendo", Short: "SNES", Maker: "Nintendo", Year: 1990, Kind: "console", Ext: []string{"sfc", "smc", "fig", "swc", "bs"}, System: "snes", Cores: []string{"snes9x"}, Libretro: "Nintendo - Super Nintendo Entertainment System", Wiki: "Super NES", Aliases: []string{"sfam"}, IGDB: 19},
	{Slug: "n64", Name: "Nintendo 64", Short: "N64", Maker: "Nintendo", Year: 1996, Kind: "console", Ext: []string{"n64", "z64", "v64"}, System: "n64", Cores: []string{"mupen64plus_next", "parallel_n64"}, Libretro: "Nintendo - Nintendo 64", Wiki: "Nintendo 64", IGDB: 4},
	{Slug: "64dd", Name: "Nintendo 64DD", Short: "64DD", Maker: "Nintendo", Year: 1999, Kind: "console", Ext: []string{"ndd"}, Libretro: "Nintendo - Nintendo 64DD", Wiki: "64DD", IGDB: 416},
	{Slug: "ngc", Name: "Nintendo GameCube", Short: "GameCube", Maker: "Nintendo", Year: 2001, Kind: "console", Ext: []string{"rvz", "gcz", "iso", "gcm", "ciso", "wia", "dol", "elf"}, Disc: true, Libretro: "Nintendo - GameCube", Wiki: "GameCube", IGDB: 21},
	{Slug: "wii", Name: "Wii", Short: "Wii", Maker: "Nintendo", Year: 2006, Kind: "console", Ext: []string{"rvz", "wbfs", "iso", "wia", "gcz", "ciso", "wad"}, Disc: true, Libretro: "Nintendo - Wii", Wiki: "Wii", IGDB: 5},
	{Slug: "wiiu", Name: "Wii U", Short: "Wii U", Maker: "Nintendo", Year: 2012, Kind: "console", Ext: []string{"wua", "wux", "rpx", "wud"}, Disc: true, Libretro: "Nintendo - Wii U", Wiki: "Wii U", IGDB: 41},
	{Slug: "switch", Name: "Nintendo Switch", Short: "Switch", Maker: "Nintendo", Year: 2017, Kind: "console", Ext: []string{"nsp", "xci", "nsz", "xcz"}, Libretro: "Nintendo - Switch", Wiki: "Nintendo Switch", IGDB: 130},
	{Slug: "gb", Name: "Game Boy", Short: "Game Boy", Maker: "Nintendo", Year: 1989, Kind: "handheld", Ext: []string{"gb"}, System: "gb", Cores: []string{"gambatte"}, Libretro: "Nintendo - Game Boy", Wiki: "Game Boy", IGDB: 33},
	{Slug: "gbc", Name: "Game Boy Color", Short: "GBC", Maker: "Nintendo", Year: 1998, Kind: "handheld", Ext: []string{"gbc"}, System: "gb", Cores: []string{"gambatte"}, Libretro: "Nintendo - Game Boy Color", Wiki: "Game Boy Color", IGDB: 22},
	{Slug: "gba", Name: "Game Boy Advance", Short: "GBA", Maker: "Nintendo", Year: 2001, Kind: "handheld", Ext: []string{"gba"}, System: "gba", Cores: []string{"mgba"}, Libretro: "Nintendo - Game Boy Advance", Wiki: "Game Boy Advance", IGDB: 24,
		Bios: []Bios{{File: "gba_bios.bin", MD5: "a860e8c0b6d573d191e4ec7db1b1e4f6", Note: "optional: shows the real start-up logo"}}},
	{Slug: "nds", Name: "Nintendo DS", Short: "DS", Maker: "Nintendo", Year: 2004, Kind: "handheld", Ext: []string{"nds", "dsi"}, System: "nds", Cores: []string{"melonds", "desmume"}, Libretro: "Nintendo - Nintendo DS", Wiki: "Nintendo DS", IGDB: 20,
		Bios: []Bios{{File: "bios7.bin", MD5: "df692a80a5b1bc90728bc3dfc76cd948"}, {File: "bios9.bin", MD5: "a392174eb3e572fed6447e956bde4b25"}, {File: "firmware.bin"}}},
	{Slug: "nintendo-dsi", Minor: true, Name: "Nintendo DSi", Short: "DSi", Maker: "Nintendo", Year: 2008, Kind: "handheld", Ext: []string{"nds", "dsi"}, Libretro: "Nintendo - Nintendo DSi", Wiki: "Nintendo DSi", IGDB: 159},
	{Slug: "3ds", Name: "Nintendo 3DS", Short: "3DS", Maker: "Nintendo", Year: 2011, Kind: "handheld", Ext: []string{"3ds", "cia", "cci", "cxi", "3dsx", "app"}, Libretro: "Nintendo - Nintendo 3DS", Wiki: "Nintendo 3DS", IGDB: 37},
	{Slug: "new-nintendo-3ds", Minor: true, Name: "New Nintendo 3DS", Short: "New 3DS", Maker: "Nintendo", Year: 2014, Kind: "handheld", Ext: []string{"3ds", "cia", "cci", "cxi"}, Libretro: "Nintendo - Nintendo 3DS", Wiki: "New Nintendo 3DS", IGDB: 137},
	{Slug: "virtualboy", Name: "Virtual Boy", Short: "Virtual Boy", Maker: "Nintendo", Year: 1995, Kind: "console", Ext: []string{"vb", "vboy"}, System: "vb", Cores: []string{"beetle_vb"}, Libretro: "Nintendo - Virtual Boy", Wiki: "Virtual Boy", IGDB: 87},
	{Slug: "pokemon-mini", Name: "Pokémon mini", Short: "Pokémon mini", Maker: "Nintendo", Year: 2001, Kind: "handheld", Ext: []string{"min"}, Libretro: "Nintendo - Pokemon Mini", Wiki: "Pokémon mini", IGDB: 166},
	{Slug: "g-and-w", Name: "Game & Watch", Short: "Game & Watch", Maker: "Nintendo", Year: 1980, Kind: "handheld", Ext: []string{"mgw"}, Libretro: "Handheld Electronic Game", Wiki: "Game & Watch", IGDB: 307},
	{Slug: "satellaview", Minor: true, Name: "Satellaview", Short: "Satellaview", Maker: "Nintendo", Year: 1995, Kind: "console", Ext: []string{"bs", "sfc"}, System: "snes", Cores: []string{"snes9x"}, Libretro: "Nintendo - Satellaview", Wiki: "Satellaview", IGDB: 306},
	{Slug: "sufami-turbo", Minor: true, Name: "Sufami Turbo", Short: "Sufami Turbo", Maker: "Bandai", Year: 1996, Kind: "console", Ext: []string{"st", "sfc"}, System: "snes", Cores: []string{"snes9x"}, Libretro: "Nintendo - Sufami Turbo", Wiki: "Sufami Turbo"},

	// Sony
	{Slug: "psx", Name: "PlayStation", Short: "PS1", Maker: "Sony", Year: 1994, Kind: "console", Ext: []string{"chd", "cue", "bin", "img", "iso", "pbp", "m3u", "ccd", "mdf", "toc", "ecm"}, Disc: true, System: "psx", Cores: []string{"pcsx_rearmed", "mednafen_psx_hw"}, Libretro: "Sony - PlayStation", Wiki: "PlayStation (console)", IGDB: 7,
		Bios: []Bios{
			{File: "scph5501.bin", MD5: "490f666e1afb15b7362b406ed1cea246", Note: "North America"},
			{File: "scph5500.bin", MD5: "8dd7d5296a650fac7319bce665a6a53c", Note: "Japan"},
			{File: "scph5502.bin", MD5: "32736f17079d0b2b7024407c39bd3050", Note: "Europe"},
			{File: "scph1001.bin", MD5: "924e392ed05558ffdb115408c263dccf", Note: "North America (early)"},
			{File: "scph7001.bin", MD5: "1e68c231d0896b7eadcad1d7d8e76129", Note: "North America"},
			{File: "scph101.bin", MD5: "6e3735ff4c7dc899ee98981385f6f3d0", Note: "PS one"},
		}},
	{Slug: "ps2", Name: "PlayStation 2", Short: "PS2", Maker: "Sony", Year: 2000, Kind: "console", Ext: []string{"iso", "chd", "cso", "zso", "bin", "cue", "gz", "elf", "mdf", "nrg"}, Disc: true, Libretro: "Sony - PlayStation 2", Wiki: "PlayStation 2", IGDB: 8,
		Bios: []Bios{{File: "PS2 BIOS", Required: true, Any: true, Note: "any PS2 BIOS dump; emulators on devices need one"}}},
	{Slug: "ps3", Name: "PlayStation 3", Short: "PS3", Maker: "Sony", Year: 2006, Kind: "console", Ext: []string{"iso", "pkg"}, Disc: true, Libretro: "Sony - PlayStation 3", Wiki: "PlayStation 3", IGDB: 9},
	{Slug: "psp", Name: "PlayStation Portable", Short: "PSP", Maker: "Sony", Year: 2004, Kind: "handheld", Ext: []string{"iso", "cso", "pbp", "chd", "elf", "prx"}, System: "psp", Cores: []string{"ppsspp"}, Threads: true, Libretro: "Sony - PlayStation Portable", Wiki: "PlayStation Portable", IGDB: 38},
	{Slug: "psvita", Name: "PlayStation Vita", Short: "Vita", Maker: "Sony", Year: 2011, Kind: "handheld", Ext: []string{"vpk", "zip"}, Libretro: "Sony - PlayStation Vita", Wiki: "PlayStation Vita", IGDB: 46},

	// Sega
	{Slug: "sg1000", Minor: true, Name: "SG-1000", Short: "SG-1000", Maker: "Sega", Year: 1983, Kind: "console", Ext: []string{"sg"}, System: "segaMS", Cores: []string{"genesis_plus_gx", "picodrive"}, Libretro: "Sega - SG-1000", Wiki: "SG-1000", IGDB: 84},
	{Slug: "sms", Name: "Master System", Short: "Master System", Maker: "Sega", Year: 1985, Kind: "console", Ext: []string{"sms"}, System: "segaMS", Cores: []string{"smsplus", "genesis_plus_gx", "picodrive"}, Libretro: "Sega - Master System - Mark III", Wiki: "Master System", IGDB: 64},
	{Slug: "genesis", Name: "Sega Genesis / Mega Drive", Short: "Genesis", Maker: "Sega", Year: 1988, Kind: "console", Ext: []string{"md", "gen", "smd", "bin", "68k", "sgd"}, System: "segaMD", Cores: []string{"genesis_plus_gx", "picodrive"}, Libretro: "Sega - Mega Drive - Genesis", Wiki: "Sega Genesis", Aliases: []string{"genesis-slash-megadrive", "megadrive", "md"}, IGDB: 29},
	{Slug: "gamegear", Name: "Game Gear", Short: "Game Gear", Maker: "Sega", Year: 1990, Kind: "handheld", Ext: []string{"gg"}, System: "segaGG", Cores: []string{"genesis_plus_gx"}, Libretro: "Sega - Game Gear", Wiki: "Game Gear", IGDB: 35},
	{Slug: "segacd", Name: "Sega CD / Mega-CD", Short: "Sega CD", Maker: "Sega", Year: 1991, Kind: "console", Ext: []string{"chd", "cue", "bin", "iso", "m3u"}, Disc: true, System: "segaCD", Cores: []string{"genesis_plus_gx", "picodrive"}, Libretro: "Sega - Mega-CD - Sega CD", Wiki: "Sega CD", IGDB: 78,
		Bios: []Bios{
			{File: "bios_CD_U.bin", MD5: "2efd74e3232ff260e371b99f84024f7f", Required: true, Note: "North America"},
			{File: "bios_CD_E.bin", MD5: "e66fa1dc5820d254611fdcdba0662372", Note: "Europe"},
			{File: "bios_CD_J.bin", MD5: "278a9397d192149e84e820ac621a8edd", Note: "Japan"},
		}},
	{Slug: "sega32", Name: "Sega 32X", Short: "32X", Maker: "Sega", Year: 1994, Kind: "console", Ext: []string{"32x"}, System: "sega32x", Cores: []string{"picodrive"}, Libretro: "Sega - 32X", Wiki: "32X", Aliases: []string{"sega32x"}, IGDB: 30},
	{Slug: "saturn", Name: "Sega Saturn", Short: "Saturn", Maker: "Sega", Year: 1994, Kind: "console", Ext: []string{"chd", "cue", "bin", "iso", "m3u", "ccd", "mds"}, Disc: true, System: "segaSaturn", Cores: []string{"yabause"}, Libretro: "Sega - Saturn", Wiki: "Sega Saturn", IGDB: 32,
		Bios: []Bios{{File: "saturn_bios.bin", MD5: "af5828fdff51384f99b3c4926be27762", Note: "makes more games work"}}},
	{Slug: "dc", Name: "Dreamcast", Short: "Dreamcast", Maker: "Sega", Year: 1998, Kind: "console", Ext: []string{"chd", "gdi", "cdi", "cue", "bin", "m3u"}, Disc: true, Libretro: "Sega - Dreamcast", Wiki: "Dreamcast", IGDB: 23,
		Bios: []Bios{{File: "dc_boot.bin", MD5: "e10c53c2f8b90bab96ead2d368858623"}, {File: "dc_flash.bin"}}},
	{Slug: "sega-pico", Minor: true, Name: "Sega Pico", Short: "Pico", Maker: "Sega", Year: 1993, Kind: "console", Ext: []string{"md", "bin"}, System: "segaMD", Cores: []string{"picodrive"}, Libretro: "Sega - PICO", Wiki: "Sega Pico", IGDB: 339},

	// NEC / SNK / Bandai / others
	{Slug: "tg16", Name: "TurboGrafx-16 / PC Engine", Short: "TG-16", Maker: "NEC", Year: 1987, Kind: "console", Ext: []string{"pce", "sgx"}, System: "pce", Cores: []string{"mednafen_pce"}, Libretro: "NEC - PC Engine - TurboGrafx 16", Wiki: "TurboGrafx-16", Aliases: []string{"pce", "pc-engine"}, IGDB: 86},
	{Slug: "turbografx-cd", Name: "TurboGrafx-CD / PC Engine CD", Short: "TG-CD", Maker: "NEC", Year: 1988, Kind: "console", Ext: []string{"chd", "cue", "bin", "iso", "ccd", "m3u"}, Disc: true, System: "pce", Cores: []string{"mednafen_pce"}, Libretro: "NEC - PC Engine CD - TurboGrafx-CD", Wiki: "TurboGrafx-CD", IGDB: 150,
		Bios: []Bios{{File: "syscard3.pce", MD5: "38179df8f4ac870017db21ebcbf53114", Required: true}}},
	{Slug: "supergrafx", Minor: true, Name: "SuperGrafx", Short: "SuperGrafx", Maker: "NEC", Year: 1989, Kind: "console", Ext: []string{"pce", "sgx"}, System: "pce", Cores: []string{"mednafen_pce"}, Libretro: "NEC - PC Engine SuperGrafx", Wiki: "SuperGrafx", IGDB: 128},
	{Slug: "pc-fx", Name: "PC-FX", Short: "PC-FX", Maker: "NEC", Year: 1994, Kind: "console", Ext: []string{"chd", "cue", "bin", "ccd", "m3u"}, Disc: true, System: "pcfx", Cores: []string{"mednafen_pcfx"}, Libretro: "NEC - PC-FX", Wiki: "PC-FX", IGDB: 274,
		Bios: []Bios{{File: "pcfx.rom", MD5: "08e36edbea28a017f79f8d4f7ff9b6d7", Required: true}}},
	{Slug: "neo-geo-pocket", Name: "Neo Geo Pocket", Short: "NGP", Maker: "SNK", Year: 1998, Kind: "handheld", Ext: []string{"ngp"}, System: "ngp", Cores: []string{"mednafen_ngp"}, Libretro: "SNK - Neo Geo Pocket", Wiki: "Neo Geo Pocket", IGDB: 119},
	{Slug: "neo-geo-pocket-color", Name: "Neo Geo Pocket Color", Short: "NGPC", Maker: "SNK", Year: 1999, Kind: "handheld", Ext: []string{"ngc", "ngpc", "npc"}, System: "ngp", Cores: []string{"mednafen_ngp"}, Libretro: "SNK - Neo Geo Pocket Color", Wiki: "Neo Geo Pocket Color", IGDB: 120},
	{Slug: "neogeoaes", Name: "Neo Geo AES", Short: "Neo Geo", Maker: "SNK", Year: 1990, Kind: "console", Ext: []string{"zip", "7z"}, System: "arcade", Cores: []string{"fbneo"}, Libretro: "SNK - Neo Geo", Wiki: "Neo Geo (system)", IGDB: 80,
		Bios: []Bios{{File: "neogeo.zip", Required: true, Note: "keep it next to the games too"}}},
	{Slug: "neogeomvs", Minor: true, Name: "Neo Geo MVS", Short: "MVS", Maker: "SNK", Year: 1990, Kind: "arcade", Ext: []string{"zip", "7z"}, System: "arcade", Cores: []string{"fbneo"}, Libretro: "SNK - Neo Geo", Wiki: "Neo Geo (system)", IGDB: 79,
		Bios: []Bios{{File: "neogeo.zip", Required: true, Note: "keep it next to the games too"}}},
	{Slug: "neo-geo-cd", Name: "Neo Geo CD", Short: "Neo Geo CD", Maker: "SNK", Year: 1994, Kind: "console", Ext: []string{"chd", "cue", "bin", "iso"}, Disc: true, Libretro: "SNK - Neo Geo CD", Wiki: "Neo Geo CD", IGDB: 136},
	{Slug: "wonderswan", Name: "WonderSwan", Short: "WonderSwan", Maker: "Bandai", Year: 1999, Kind: "handheld", Ext: []string{"ws"}, System: "ws", Cores: []string{"mednafen_wswan"}, Libretro: "Bandai - WonderSwan", Wiki: "WonderSwan", IGDB: 57},
	{Slug: "wonderswan-color", Name: "WonderSwan Color", Short: "WSC", Maker: "Bandai", Year: 2000, Kind: "handheld", Ext: []string{"wsc"}, System: "ws", Cores: []string{"mednafen_wswan"}, Libretro: "Bandai - WonderSwan Color", Wiki: "WonderSwan Color", IGDB: 123},
	{Slug: "3do", Name: "3DO", Short: "3DO", Maker: "Panasonic", Year: 1993, Kind: "console", Ext: []string{"chd", "cue", "bin", "iso"}, Disc: true, System: "3do", Cores: []string{"opera"}, Libretro: "The 3DO Company - 3DO", Wiki: "3DO Interactive Multiplayer", IGDB: 50,
		Bios: []Bios{{File: "panafz10.bin", MD5: "51f2f43ae2f3508a14d9f56597e2d3ce", Required: true}}},
	{Slug: "philips-cd-i", Name: "Philips CD-i", Short: "CD-i", Maker: "Philips", Year: 1991, Kind: "console", Ext: []string{"chd", "cue", "bin", "iso"}, Disc: true, Libretro: "Philips - CD-i", Wiki: "CD-i", IGDB: 117},
	{Slug: "colecovision", Name: "ColecoVision", Short: "ColecoVision", Maker: "Coleco", Year: 1982, Kind: "console", Ext: []string{"col", "cv"}, System: "coleco", Cores: []string{"gearcoleco"}, Libretro: "Coleco - ColecoVision", Wiki: "ColecoVision", IGDB: 68,
		Bios: []Bios{{File: "colecovision.rom", MD5: "2c66f5911e5b42b8ebe113403548eee7", Required: true}}},
	{Slug: "intellivision", Name: "Intellivision", Short: "Intellivision", Maker: "Mattel", Year: 1979, Kind: "console", Ext: []string{"int", "bin", "rom"}, Libretro: "Mattel - Intellivision", Wiki: "Intellivision", IGDB: 67},
	{Slug: "vectrex", Name: "Vectrex", Short: "Vectrex", Maker: "GCE", Year: 1982, Kind: "console", Ext: []string{"vec", "gam", "bin"}, Libretro: "GCE - Vectrex", Wiki: "Vectrex", IGDB: 70},
	{Slug: "odyssey-2", Name: "Magnavox Odyssey 2", Short: "Odyssey²", Maker: "Magnavox", Year: 1978, Kind: "console", Ext: []string{"bin"}, Libretro: "Magnavox - Odyssey2", Wiki: "Magnavox Odyssey 2", IGDB: 133},

	// Atari
	{Slug: "atari2600", Name: "Atari 2600", Short: "2600", Maker: "Atari", Year: 1977, Kind: "console", Ext: []string{"a26", "bin"}, System: "atari2600", Cores: []string{"stella2014"}, Libretro: "Atari - 2600", Wiki: "Atari 2600", IGDB: 59},
	{Slug: "atari5200", Name: "Atari 5200", Short: "5200", Maker: "Atari", Year: 1982, Kind: "console", Ext: []string{"a52", "bin"}, System: "atari5200", Cores: []string{"a5200"}, Libretro: "Atari - 5200", Wiki: "Atari 5200", IGDB: 66,
		Bios: []Bios{{File: "5200.rom", MD5: "281f20ea4320404ec820fb7ec0693b38", Required: true}}},
	{Slug: "atari7800", Name: "Atari 7800", Short: "7800", Maker: "Atari", Year: 1986, Kind: "console", Ext: []string{"a78", "bin"}, System: "atari7800", Cores: []string{"prosystem"}, Libretro: "Atari - 7800", Wiki: "Atari 7800", IGDB: 60,
		Bios: []Bios{{File: "7800 BIOS (U).rom", MD5: "0763f1ffb006ddbe32e52d497ee848ae", Note: "optional"}}},
	{Slug: "lynx", Name: "Atari Lynx", Short: "Lynx", Maker: "Atari", Year: 1989, Kind: "handheld", Ext: []string{"lnx", "o"}, System: "lynx", Cores: []string{"handy"}, Libretro: "Atari - Lynx", Wiki: "Atari Lynx", IGDB: 61,
		Bios: []Bios{{File: "lynxboot.img", MD5: "fcd403db69f54290b51035d82f835e7b", Required: true}}},
	{Slug: "jaguar", Name: "Atari Jaguar", Short: "Jaguar", Maker: "Atari", Year: 1993, Kind: "console", Ext: []string{"j64", "jag", "rom", "abs", "cof", "bin"}, System: "jaguar", Cores: []string{"virtualjaguar"}, Libretro: "Atari - Jaguar", Wiki: "Atari Jaguar", IGDB: 62},
	{Slug: "atari-st", Name: "Atari ST", Short: "ST", Maker: "Atari", Year: 1985, Kind: "computer", Ext: []string{"st", "msa", "stx", "dim", "ipf"}, Libretro: "Atari - ST", Wiki: "Atari ST", IGDB: 63},
	{Slug: "atari8bit", Name: "Atari 8-bit", Short: "Atari 8-bit", Maker: "Atari", Year: 1979, Kind: "computer", Ext: []string{"atr", "xex", "xfd", "car", "cas"}, Libretro: "Atari - 8-bit", Wiki: "Atari 8-bit family", Aliases: []string{"atari800"}, IGDB: 65},

	// Computers
	{Slug: "c64", Name: "Commodore 64", Short: "C64", Maker: "Commodore", Year: 1982, Kind: "computer", Ext: []string{"d64", "t64", "prg", "crt", "tap", "g64", "d81"}, System: "c64", Cores: []string{"vice_x64sc"}, Libretro: "Commodore - 64", Wiki: "Commodore 64", IGDB: 15},
	{Slug: "c128", Minor: true, Name: "Commodore 128", Short: "C128", Maker: "Commodore", Year: 1985, Kind: "computer", Ext: []string{"d64", "d71", "d81", "prg", "t64"}, System: "c128", Cores: []string{"vice_x128"}, Libretro: "Commodore - 128", Wiki: "Commodore 128", IGDB: 15},
	{Slug: "vic-20", Name: "Commodore VIC-20", Short: "VIC-20", Maker: "Commodore", Year: 1980, Kind: "computer", Ext: []string{"prg", "crt", "d64", "tap", "20", "a0"}, System: "vic20", Cores: []string{"vice_xvic"}, Libretro: "Commodore - VIC-20", Wiki: "VIC-20", IGDB: 71},
	{Slug: "amiga", Name: "Amiga", Short: "Amiga", Maker: "Commodore", Year: 1985, Kind: "computer", Ext: []string{"adf", "adz", "dms", "ipf", "hdf", "lha", "m3u"}, System: "amiga", Cores: []string{"puae"}, Libretro: "Commodore - Amiga", Wiki: "Amiga", IGDB: 16,
		Bios: []Bios{{File: "kick34005.A500", MD5: "82a21c1890cae844b3df741f2762d48d", Note: "Kickstart 1.3; without it a free replacement is used"}}},
	{Slug: "amiga-cd32", Name: "Amiga CD32", Short: "CD32", Maker: "Commodore", Year: 1993, Kind: "console", Ext: []string{"chd", "cue", "iso"}, Disc: true, Libretro: "Commodore - CD32", Wiki: "Amiga CD32", IGDB: 114},
	{Slug: "dos", Name: "MS-DOS", Short: "DOS", Maker: "Microsoft", Year: 1981, Kind: "computer", Ext: []string{"zip", "dosz", "exe", "com", "bat", "iso", "cue", "img", "conf"}, System: "dos", Cores: []string{"dosbox_pure"}, Threads: true, Libretro: "DOS", Wiki: "MS-DOS", IGDB: 13},
	{Slug: "win", Name: "Windows", Short: "PC", Maker: "Microsoft", Year: 1985, Kind: "computer", Ext: []string{"exe", "iso", "zip"}, Libretro: "", Wiki: "Microsoft Windows", IGDB: 6},
	{Slug: "msx", Name: "MSX", Short: "MSX", Maker: "Microsoft", Year: 1983, Kind: "computer", Ext: []string{"rom", "mx1", "mx2", "dsk", "cas"}, Libretro: "Microsoft - MSX", Wiki: "MSX", IGDB: 27},
	{Slug: "msx2", Name: "MSX2", Short: "MSX2", Maker: "Microsoft", Year: 1985, Kind: "computer", Ext: []string{"rom", "mx2", "dsk", "cas"}, Libretro: "Microsoft - MSX2", Wiki: "MSX2", IGDB: 53},
	{Slug: "zxs", Name: "ZX Spectrum", Short: "ZX Spectrum", Maker: "Sinclair", Year: 1982, Kind: "computer", Ext: []string{"tzx", "tap", "z80", "sna", "dsk", "trd", "scl"}, Libretro: "Sinclair - ZX Spectrum", Wiki: "ZX Spectrum", IGDB: 26},
	{Slug: "acpc", Name: "Amstrad CPC", Short: "CPC", Maker: "Amstrad", Year: 1984, Kind: "computer", Ext: []string{"dsk", "sna", "cdt", "kcr"}, Libretro: "Amstrad - CPC", Wiki: "Amstrad CPC", IGDB: 25},
	{Slug: "sharp-x68000", Name: "Sharp X68000", Short: "X68000", Maker: "Sharp", Year: 1987, Kind: "computer", Ext: []string{"dim", "xdf", "hdf", "2hd", "d88", "m3u"}, Libretro: "Sharp - X68000", Wiki: "X68000", IGDB: 121},
	{Slug: "pc-8800-series", Name: "PC-8800 series", Short: "PC-88", Maker: "NEC", Year: 1981, Kind: "computer", Ext: []string{"d88", "u88", "m3u"}, Libretro: "NEC - PC-8001 - PC-8801", Wiki: "PC-8800 series", IGDB: 125},
	{Slug: "pc-9800-series", Name: "PC-9800 series", Short: "PC-98", Maker: "NEC", Year: 1982, Kind: "computer", Ext: []string{"d98", "fdi", "hdi", "hdd", "m3u"}, Libretro: "NEC - PC-98", Wiki: "PC-9800 series", IGDB: 149},
	{Slug: "scummvm", Name: "ScummVM", Short: "ScummVM", Maker: "ScummVM", Year: 2001, Kind: "other", Ext: []string{"scummvm", "zip"}, Libretro: "ScummVM", Wiki: "ScummVM"},
	{Slug: "pico", Name: "PICO-8", Short: "PICO-8", Maker: "Lexaloffle", Year: 2015, Kind: "other", Ext: []string{"p8", "png"}, Libretro: "PICO-8", Wiki: "PICO-8", IGDB: 308},
	{Slug: "tic-80", Name: "TIC-80", Short: "TIC-80", Maker: "Nesbox", Year: 2017, Kind: "other", Ext: []string{"tic"}, Libretro: "TIC-80", Wiki: "TIC-80"},
	{Slug: "openbor", Name: "OpenBOR", Short: "OpenBOR", Maker: "OpenBOR", Year: 2004, Kind: "other", Ext: []string{"pak"}, Wiki: "OpenBOR"},

	// Arcade
	{Slug: "arcade", Name: "Arcade", Short: "Arcade", Maker: "Various", Year: 1971, Kind: "arcade", Ext: []string{"zip", "7z"}, System: "arcade", Cores: []string{"fbneo", "mame2003_plus"}, Libretro: "MAME", Wiki: "Arcade video game", Aliases: []string{"mame", "fbneo"}, IGDB: 52},
	{Slug: "cps1", Minor: true, Name: "Capcom CPS-1", Short: "CPS-1", Maker: "Capcom", Year: 1988, Kind: "arcade", Ext: []string{"zip"}, System: "arcade", Cores: []string{"fbneo", "fbalpha2012_cps1"}, Libretro: "FBNeo - Arcade Games", Wiki: "CP System"},
	{Slug: "cps2", Minor: true, Name: "Capcom CPS-2", Short: "CPS-2", Maker: "Capcom", Year: 1993, Kind: "arcade", Ext: []string{"zip"}, System: "arcade", Cores: []string{"fbneo", "fbalpha2012_cps2"}, Libretro: "FBNeo - Arcade Games", Wiki: "CP System II"},
	{Slug: "cps3", Minor: true, Name: "Capcom CPS-3", Short: "CPS-3", Maker: "Capcom", Year: 1996, Kind: "arcade", Ext: []string{"zip"}, System: "arcade", Cores: []string{"fbneo"}, Libretro: "FBNeo - Arcade Games", Wiki: "CP System III"},

	// Microsoft consoles
	{Slug: "xbox", Name: "Xbox", Short: "Xbox", Maker: "Microsoft", Year: 2001, Kind: "console", Ext: []string{"iso", "xiso"}, Disc: true, Libretro: "Microsoft - Xbox", Wiki: "Xbox (console)", IGDB: 11},
	{Slug: "xbox360", Name: "Xbox 360", Short: "Xbox 360", Maker: "Microsoft", Year: 2005, Kind: "console", Ext: []string{"iso", "xex"}, Disc: true, Libretro: "Microsoft - Xbox 360", Wiki: "Xbox 360", IGDB: 12},
	{Slug: "android", Name: "Android", Short: "Android", Maker: "Google", Year: 2008, Kind: "other", Ext: []string{"apk", "xapk"}, Wiki: "Android (operating system)", IGDB: 34},
}

var bySlug = map[string]*Platform{}

func init() {
	for i := range all {
		p := &all[i]
		bySlug[p.Slug] = p
		for _, a := range p.Aliases {
			if _, taken := bySlug[a]; !taken {
				bySlug[a] = p
			}
		}
	}
}

// Get returns the known platform for a folder name, or a plain one made
// from the name itself (an unknown folder is still a platform).
func Get(slug string) Platform {
	if p, ok := bySlug[strings.ToLower(slug)]; ok {
		q := *p
		q.Slug = slug // keep the folder name the library uses
		return q
	}
	return Platform{Slug: slug, Name: Title(slug), Short: Title(slug), Kind: "other"}
}

// Known reports whether the slug is a system this app knows.
func Known(slug string) bool { _, ok := bySlug[strings.ToLower(slug)]; return ok }

// All lists the known systems (not the aliases), by name.
func All() []Platform {
	out := make([]Platform, len(all))
	copy(out, all)
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// Title makes "neo-geo-pocket" read "Neo Geo Pocket".
func Title(slug string) string {
	parts := strings.FieldsFunc(slug, func(r rune) bool { return r == '-' || r == '_' || r == ' ' })
	for i, p := range parts {
		if p == "slash" {
			parts[i] = "/"
			continue
		}
		if len(p) <= 3 {
			parts[i] = strings.ToUpper(p)
		} else {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

// ForExtension lists the systems whose games use this file extension (lower
// case, no dot). One answer = the upload can be filed without asking.
// Variants (Famicom next to NES, DSi next to DS…) only count when no main
// system takes the extension.
func ForExtension(ext string) []string {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	var main, minor []string
	for _, p := range all {
		for _, e := range p.Ext {
			if e == ext {
				if p.Minor {
					minor = append(minor, p.Slug)
				} else {
					main = append(main, p.Slug)
				}
				break
			}
		}
	}
	if len(main) > 0 {
		return main
	}
	return minor
}

// GameExtension reports whether a file with this extension can be a game of
// the platform (or, for unknown platforms, anything that is not obviously
// a picture, a text or a leftover).
func GameExtension(p Platform, ext string) bool {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	if junkExt[ext] {
		return false
	}
	if len(p.Ext) == 0 {
		return true
	}
	if ext == "zip" || ext == "7z" {
		return true // zipped carts play directly; disc zips are opened on upload
	}
	for _, e := range p.Ext {
		if e == ext {
			return true
		}
	}
	return false
}

var junkExt = map[string]bool{
	"txt": true, "nfo": true, "jpg": true, "jpeg": true, "png": true, "gif": true, "webp": true, "pdf": true,
	"xml": true, "dat": true, "sfv": true, "md5": true, "sha1": true, "db": true, "ini": true,
	"part": true, "tmp": true, "crdownload": true, "srm": true, "sav": true, "state": true, "sbi": true,
	"log": true, "url": true, "html": true, "htm": true, "json": true, "ds_store": true,
}

// Companion reports files that belong next to a game but are not one
// (subchannel data, playlists, track bins and patches).
func Companion(ext string) bool {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "sbi", "sub", "m3u", "cue", "bin", "img", "ccd", "toc", "gdi", "raw", "ips", "bps", "ups", "xdelta":
		return true
	}
	return false
}
