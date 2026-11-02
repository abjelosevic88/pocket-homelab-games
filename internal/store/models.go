package store

import "strings"

// User is an account on this box.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Platform is a platform folder with its counts.
type Platform struct {
	ID         int64  `json:"id"`
	Slug       string `json:"slug"`
	CustomName string `json:"custom_name"`
	Games      int    `json:"games"`
	SizeBytes  int64  `json:"size_bytes"`
	Firmware   int    `json:"firmware"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// Game is one game in the library, plus the caller's own relation to it.
type Game struct {
	ID            int64    `json:"id"`
	PlatformID    int64    `json:"platform_id"`
	PlatformSlug  string   `json:"platform_slug"`
	FSName        string   `json:"fs_name"`
	IsFolder      bool     `json:"is_folder"`
	MainFile      string   `json:"main_file"`
	Name          string   `json:"name"`
	SortName      string   `json:"sort_name"`
	Regions       string   `json:"regions"`
	Languages     string   `json:"languages"`
	Tags          string   `json:"tags"`
	SizeBytes     int64    `json:"size_bytes"`
	FileMTime     string   `json:"file_mtime"`
	CRC32         string   `json:"crc32"`
	MD5           string   `json:"md5"`
	SHA1          string   `json:"sha1"`
	HashedAt      string   `json:"hashed_at"`
	Summary       string   `json:"summary"`
	SummarySource string   `json:"summary_source"`
	SummaryURL    string   `json:"summary_url"`
	Genres        string   `json:"genres"`
	Developer     string   `json:"developer"`
	Publisher     string   `json:"publisher"`
	ReleaseDate   string   `json:"release_date"`
	Players       string   `json:"players"`
	CoverPath     string   `json:"-"`
	CoverSource   string   `json:"cover_source"`
	Shots         []string `json:"-"`
	WikiTitle     string   `json:"wiki_title"`
	MetaStatus    string   `json:"meta_status"`
	MetaLocked    bool     `json:"meta_locked"`
	MetaAt        string   `json:"meta_at"`
	MissingSince  string   `json:"missing_since"`
	TrashedAt     string   `json:"trashed_at"`
	TrashPath     string   `json:"-"`
	AddedBy       *int64   `json:"added_by"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`

	// the caller's own
	Mine UserGame `json:"mine"`

	// computed
	Cover     string   `json:"cover"`      // URL ('' = none)
	CoverAt   string   `json:"-"`          // cache buster
	ShotURLs  []string `json:"shots"`      // URLs
	FileCount int      `json:"file_count"` // files on disk

	PlatformName string `json:"platform_name"`
	Browser      bool   `json:"browser"`  // plays in the browser
	CanEdit      bool   `json:"can_edit"` // the caller may change or delete it
}

// UserGame is one person's relation to a game.
type UserGame struct {
	Favorite     bool   `json:"favorite"`
	Status       string `json:"status"`
	Rating       int    `json:"rating"`
	Notes        string `json:"notes"`
	Hidden       bool   `json:"hidden"`
	PlaySeconds  int64  `json:"play_seconds"`
	PlayCount    int    `json:"play_count"`
	LastPlayedAt string `json:"last_played_at"`
	Core         string `json:"core"`
}

// Statuses a person can give a game.
var Statuses = []string{"", "backlog", "playing", "finished", "completed", "dropped"}

// ValidStatus reports a known status.
func ValidStatus(s string) bool {
	for _, x := range Statuses {
		if x == s {
			return true
		}
	}
	return false
}

// GameFile is one file of a game.
type GameFile struct {
	ID        int64  `json:"id"`
	GameID    int64  `json:"game_id"`
	Rel       string `json:"rel"`
	SizeBytes int64  `json:"size_bytes"`
	MTime     string `json:"mtime"`
	CRC32     string `json:"crc32"`
	MD5       string `json:"md5"`
	SHA1      string `json:"sha1"`
}

// Firmware is a BIOS file.
type Firmware struct {
	ID           int64  `json:"id"`
	PlatformID   int64  `json:"platform_id"`
	PlatformSlug string `json:"platform_slug"`
	FileName     string `json:"file_name"`
	SizeBytes    int64  `json:"size_bytes"`
	MTime        string `json:"mtime"`
	CRC32        string `json:"crc32"`
	MD5          string `json:"md5"`
	SHA1         string `json:"sha1"`
	CreatedAt    string `json:"created_at"`
}

// Session is one sitting with a game.
type Session struct {
	ID        int64  `json:"id"`
	GameID    int64  `json:"game_id"`
	GameName  string `json:"game_name"`
	Platform  string `json:"platform"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at"`
	Seconds   int64  `json:"seconds"`
	Day       string `json:"day"`
	Device    string `json:"device"`
	Published bool   `json:"-"`
}

// Save is an in-game save or a save state.
type Save struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	GameID    int64  `json:"game_id"`
	Kind      string `json:"kind"` // save | state
	FileName  string `json:"file_name"`
	Emulator  string `json:"emulator"`
	Slot      string `json:"slot"`
	Path      string `json:"-"`
	SizeBytes int64  `json:"size_bytes"`
	MD5       string `json:"md5"`
	ShotPath  string `json:"-"`
	HasShot   bool   `json:"has_shot"`
	Device    string `json:"device"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// Collection is a person's own list of games.
type Collection struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Games       int     `json:"games"`
	GameIDs     []int64 `json:"game_ids"`
	Covers      []int64 `json:"covers"` // a few game ids with covers, for the mosaic
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// Upload is a file on its way into the library.
type Upload struct {
	ID        string `json:"id"`
	UserID    int64  `json:"-"`
	Kind      string `json:"kind"`
	Platform  string `json:"platform"`
	Rel       string `json:"rel"`
	SizeBytes int64  `json:"size_bytes"`
	Received  int64  `json:"received"`
	Status    string `json:"status"`
	Error     string `json:"error"`
	GameID    *int64 `json:"game_id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// Device is an app that talked to the RomM-compatible API.
type Device struct {
	ID            int64  `json:"id"`
	UUID          string `json:"uuid"`
	Identifier    string `json:"identifier"`
	Name          string `json:"name"`
	Client        string `json:"client"`
	ClientVersion string `json:"client_version"`
	Platform      string `json:"platform"`
	Hostname      string `json:"hostname"`
	IP            string `json:"ip"`
	FirstSeen     string `json:"first_seen"`
	LastSeen      string `json:"last_seen"`
	Requests      int64  `json:"requests"`
	Downloads     int64  `json:"downloads"`
}

// Settings are the box-wide values plus one person's preferences.
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`

	DateFormat string `json:"date_format"`
	TimeFormat string `json:"time_format"`

	// The library view.
	GridSize    string `json:"grid_size"`    // s | m | l
	ShowMissing bool   `json:"show_missing"` // games whose files are gone
	DefaultSort string `json:"default_sort"` // name | added | played | year | size
	Regions     string `json:"regions"`      // preferred regions for covers, "Europe, USA"

	// The player.
	AutoSave      bool   `json:"auto_save"`      // keep in-game saves on the server while playing
	ResumeState   bool   `json:"resume_state"`   // continue from the last state when you press Play
	StateOnExit   bool   `json:"state_on_exit"`  // write a state when you leave the player
	TouchControls string `json:"touch_controls"` // auto | on | off
	PlayerVolume  int    `json:"player_volume"`  // 0..100
	PlayerShader  string `json:"player_shader"`  // '' | crt-easymode.glslp …

	// Telling.
	NotifyURL      string `json:"notify_url"`
	NotifyFormat   string `json:"notify_format"` // apprise | ntfy | json
	NotifyNewGames bool   `json:"notify_new_games"`
	NotifyUploads  bool   `json:"notify_uploads"` // a big upload finished or failed

	// AI: "what should I play?" and descriptions.
	AIPicks       bool   `json:"ai_picks"`
	AIProvider    string `json:"ai_provider"` // auto | pocket | openai
	AIPocketModel string `json:"ai_pocket_model"`
	OpenAIKey     string `json:"openai_api_key"`
	OpenAIModel   string `json:"openai_model"`
	OpenAIBaseURL string `json:"openai_base_url"`
}

// SplitTags turns "a, b ,c" into clean lowercase tags.
func SplitTags(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range strings.Split(s, ",") {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// JoinTags is the stored form of tags.
func JoinTags(s string) string { return strings.Join(SplitTags(s), ", ") }

// Lines splits a multi-line setting into its trimmed, non-empty lines.
func Lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}
