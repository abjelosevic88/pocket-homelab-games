package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// ---------- platforms ----------

// EnsurePlatform returns the id of the platform folder, creating the row.
func (s *Store) EnsurePlatform(ctx context.Context, slug string) (int64, error) {
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO platforms (slug) VALUES (?) ON CONFLICT(slug) DO NOTHING`, slug); err != nil {
		return 0, err
	}
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM platforms WHERE slug = ?`, slug).Scan(&id)
	return id, err
}

// ListPlatforms lists every platform with its number of games (in the library, not trashed).
func (s *Store) ListPlatforms(ctx context.Context) ([]Platform, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.id, p.slug, p.custom_name, p.created_at, p.updated_at,
		(SELECT COUNT(1) FROM games g WHERE g.platform_id = p.id AND g.trashed_at = '' AND g.missing_since = ''),
		(SELECT COALESCE(SUM(size_bytes),0) FROM games g WHERE g.platform_id = p.id AND g.trashed_at = '' AND g.missing_since = ''),
		(SELECT COUNT(1) FROM firmware f WHERE f.platform_id = p.id)
		FROM platforms p ORDER BY p.slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Platform{}
	for rows.Next() {
		var p Platform
		if err := rows.Scan(&p.ID, &p.Slug, &p.CustomName, &p.CreatedAt, &p.UpdatedAt, &p.Games, &p.SizeBytes, &p.Firmware); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetPlatform finds a platform by id or slug.
func (s *Store) GetPlatform(ctx context.Context, idOrSlug string) (*Platform, error) {
	all, err := s.ListPlatforms(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Slug == idOrSlug || itoa(all[i].ID) == idOrSlug {
			return &all[i], nil
		}
	}
	return nil, ErrNotFound
}

// RenamePlatform sets the shown name (” = the built-in one).
func (s *Store) RenamePlatform(ctx context.Context, id int64, name string) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE platforms SET custom_name = ?, updated_at = ? WHERE id = ?`, name, Now(), id)
	return affected(res, err)
}

// ---------- games ----------

const gameCols = `g.id, g.platform_id, p.slug, g.fs_name, g.is_folder, g.main_file, g.name, g.sort_name, g.regions, g.languages, g.tags,
	g.size_bytes, g.file_mtime, g.crc32, g.md5, g.sha1, g.hashed_at, g.summary, g.summary_source, g.summary_url, g.genres,
	g.developer, g.publisher, g.release_date, g.players, g.cover_path, g.cover_source, g.shots, g.wiki_title, g.meta_status,
	g.meta_locked, g.meta_at, g.missing_since, g.trashed_at, g.trash_path, g.added_by, g.created_at, g.updated_at,
	COALESCE(u.favorite,0), COALESCE(u.status,''), COALESCE(u.rating,0), COALESCE(u.notes,''), COALESCE(u.hidden,0),
	COALESCE(u.play_seconds,0), COALESCE(u.play_count,0), COALESCE(u.last_played_at,''), COALESCE(u.core,''),
	(SELECT COUNT(1) FROM game_files f WHERE f.game_id = g.id)`

func scanGame(row interface{ Scan(...any) error }) (*Game, error) {
	var g Game
	var shots string
	var addedBy sql.NullInt64
	err := row.Scan(&g.ID, &g.PlatformID, &g.PlatformSlug, &g.FSName, &g.IsFolder, &g.MainFile, &g.Name, &g.SortName, &g.Regions, &g.Languages, &g.Tags,
		&g.SizeBytes, &g.FileMTime, &g.CRC32, &g.MD5, &g.SHA1, &g.HashedAt, &g.Summary, &g.SummarySource, &g.SummaryURL, &g.Genres,
		&g.Developer, &g.Publisher, &g.ReleaseDate, &g.Players, &g.CoverPath, &g.CoverSource, &shots, &g.WikiTitle, &g.MetaStatus,
		&g.MetaLocked, &g.MetaAt, &g.MissingSince, &g.TrashedAt, &g.TrashPath, &addedBy, &g.CreatedAt, &g.UpdatedAt,
		&g.Mine.Favorite, &g.Mine.Status, &g.Mine.Rating, &g.Mine.Notes, &g.Mine.Hidden,
		&g.Mine.PlaySeconds, &g.Mine.PlayCount, &g.Mine.LastPlayedAt, &g.Mine.Core, &g.FileCount)
	if err != nil {
		return nil, err
	}
	if addedBy.Valid {
		g.AddedBy = &addedBy.Int64
	}
	_ = json.Unmarshal([]byte(shots), &g.Shots)
	return &g, nil
}

// GameFilter narrows a game list. UserID is the caller: their favourites,
// statuses and play times come along and filter.
type GameFilter struct {
	UserID       int64
	PlatformIDs  []int64
	Query        string
	Status       string // a status, or "any" = has one
	Favorite     bool
	Played       bool // played at least once
	CollectionID int64
	IDs          []int64
	Browser      []string // platform slugs that play in the browser (filter "plays here")
	Missing      bool     // include games whose files are gone
	Trashed      bool     // only the trash
	Hidden       bool     // include games the person hid
	UpdatedAfter string
	Hash         string // a CRC32, MD5 or SHA-1 of the game or one of its files
	Genre        string
	Letter       string // first letter of the sort name ("#" = not a letter)
	Sort         string // name | added | played | year | size | playtime | rating
	Desc         bool
	Limit        int
	Offset       int
}

func (f GameFilter) where() (string, []any) {
	var w []string
	args := []any{}
	if f.Trashed {
		w = append(w, "g.trashed_at != ''")
	} else {
		w = append(w, "g.trashed_at = ''")
		if !f.Missing {
			w = append(w, "g.missing_since = ''")
		}
	}
	if !f.Hidden {
		w = append(w, "COALESCE(u.hidden,0) = 0")
	}
	if len(f.PlatformIDs) > 0 {
		w = append(w, "g.platform_id IN ("+placeholders(len(f.PlatformIDs))+")")
		for _, id := range f.PlatformIDs {
			args = append(args, id)
		}
	}
	if len(f.IDs) > 0 {
		w = append(w, "g.id IN ("+placeholders(len(f.IDs))+")")
		for _, id := range f.IDs {
			args = append(args, id)
		}
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		for _, word := range strings.Fields(strings.ToLower(q)) {
			w = append(w, "(LOWER(g.name) LIKE ? OR LOWER(g.fs_name) LIKE ? OR LOWER(g.genres) LIKE ? OR LOWER(g.developer) LIKE ?)")
			like := "%" + word + "%"
			args = append(args, like, like, like, like)
		}
	}
	switch f.Status {
	case "":
	case "any":
		w = append(w, "COALESCE(u.status,'') != ''")
	default:
		w = append(w, "COALESCE(u.status,'') = ?")
		args = append(args, f.Status)
	}
	if f.Favorite {
		w = append(w, "COALESCE(u.favorite,0) = 1")
	}
	if f.Played {
		w = append(w, "COALESCE(u.last_played_at,'') != ''")
	}
	if f.CollectionID > 0 {
		w = append(w, "g.id IN (SELECT game_id FROM collection_games WHERE collection_id = ? AND user_id = ?)")
		args = append(args, f.CollectionID, f.UserID)
	}
	if len(f.Browser) > 0 {
		w = append(w, "p.slug IN ("+placeholders(len(f.Browser))+")")
		for _, s := range f.Browser {
			args = append(args, s)
		}
	}
	if f.UpdatedAfter != "" {
		w = append(w, "(g.updated_at > ? OR COALESCE(u.updated_at,'') > ?)")
		args = append(args, f.UpdatedAfter, f.UpdatedAfter)
	}
	if h := strings.ToLower(f.Hash); h != "" {
		w = append(w, "(g.crc32 = ? OR g.md5 = ? OR g.sha1 = ? OR g.id IN (SELECT game_id FROM game_files WHERE crc32 = ? OR md5 = ? OR sha1 = ?))")
		args = append(args, h, h, h, h, h, h)
	}
	if f.Genre != "" {
		w = append(w, "(', ' || LOWER(g.genres) || ',') LIKE ?")
		args = append(args, "%, "+strings.ToLower(f.Genre)+",%")
	}
	switch l := strings.ToLower(f.Letter); {
	case l == "#":
		w = append(w, "SUBSTR(g.sort_name,1,1) NOT BETWEEN 'a' AND 'z'")
	case len(l) == 1:
		w = append(w, "SUBSTR(g.sort_name,1,1) = ?")
		args = append(args, l)
	}
	return strings.Join(w, " AND "), args
}

func (f GameFilter) order() string {
	dir := "ASC"
	if f.Desc {
		dir = "DESC"
	}
	rev := "DESC"
	if f.Desc {
		rev = "ASC"
	}
	switch f.Sort {
	case "added":
		return "g.created_at " + rev + ", g.id " + rev
	case "played":
		return "COALESCE(u.last_played_at,'') " + rev + ", g.sort_name"
	case "playtime":
		return "COALESCE(u.play_seconds,0) " + rev + ", g.sort_name"
	case "rating":
		return "COALESCE(u.rating,0) " + rev + ", g.sort_name"
	case "year":
		return "CASE WHEN g.release_date = '' THEN 1 ELSE 0 END, g.release_date " + dir + ", g.sort_name"
	case "size":
		return "g.size_bytes " + rev + ", g.sort_name"
	case "platform":
		return "p.slug " + dir + ", g.sort_name"
	case "id":
		return "g.id " + dir
	}
	return "g.sort_name " + dir + ", g.id"
}

// ListGames returns one page of games and how many match in total.
func (s *Store) ListGames(ctx context.Context, f GameFilter) ([]Game, int, error) {
	where, args := f.where()
	from := ` FROM games g JOIN platforms p ON p.id = g.platform_id LEFT JOIN user_games u ON u.game_id = g.id AND u.user_id = ? WHERE ` + where
	args = append([]any{f.UserID}, args...)
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1)`+from, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT ` + gameCols + from + ` ORDER BY ` + f.order()
	if f.Limit > 0 {
		q += ` LIMIT ? OFFSET ?`
		args = append(args, f.Limit, f.Offset)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Game{}
	for rows.Next() {
		g, err := scanGame(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *g)
	}
	return out, total, rows.Err()
}

// GetGame returns one game with the person's own relation (trashed and
// missing games too: the caller decides what to show).
func (s *Store) GetGame(ctx context.Context, uid, id int64) (*Game, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT `+gameCols+` FROM games g JOIN platforms p ON p.id = g.platform_id
		LEFT JOIN user_games u ON u.game_id = g.id AND u.user_id = ? WHERE g.id = ?`, uid, id)
	g, err := scanGame(row)
	return g, notFound(err)
}

// GameByPath finds a game by platform and file/folder name.
func (s *Store) GameByPath(ctx context.Context, platformID int64, fsName string) (*Game, error) {
	var id int64
	if err := s.DB.QueryRowContext(ctx, `SELECT id FROM games WHERE platform_id = ? AND fs_name = ?`, platformID, fsName).Scan(&id); err != nil {
		return nil, notFound(err)
	}
	return s.GetGame(ctx, 0, id)
}

// DiskGame is what the scanner found for one game.
type DiskGame struct {
	FSName    string
	IsFolder  bool
	MainFile  string
	Name      string
	SortName  string
	Regions   string
	Languages string
	Tags      string
	SizeBytes int64
	MTime     string
	Files     []GameFile
}

// SyncResult says what a scan changed in one platform.
type SyncResult struct {
	Added    []int64
	Changed  []int64
	Missing  []int64
	Restored []int64
}

// SyncPlatform makes the games of one platform match the disk: new ones are
// added (addedBy = whoever uploaded them, 0 = found by a scan), changed ones
// get their files again, gone ones are marked missing (never deleted — the
// person's play time, saves and notes stay; a returning file finds them).
func (s *Store) SyncPlatform(ctx context.Context, platformID int64, found []DiskGame, addedBy map[string]int64) (SyncResult, error) {
	var res SyncResult
	type cur struct {
		id      int64
		mtime   string
		size    int64
		missing string
		main    string
		trashed string
	}
	have := map[string]cur{}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, fs_name, file_mtime, size_bytes, missing_since, main_file, trashed_at FROM games WHERE platform_id = ?`, platformID)
	if err != nil {
		return res, err
	}
	for rows.Next() {
		var c cur
		var name string
		if err := rows.Scan(&c.id, &name, &c.mtime, &c.size, &c.missing, &c.main, &c.trashed); err != nil {
			rows.Close()
			return res, err
		}
		have[name] = c
	}
	rows.Close()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	now := Now()
	seen := map[string]bool{}
	for _, d := range found {
		seen[d.FSName] = true
		c, ok := have[d.FSName]
		switch {
		case !ok:
			var by any
			if uid := addedBy[d.FSName]; uid > 0 {
				by = uid
			}
			r, err := tx.ExecContext(ctx, `INSERT INTO games (platform_id, fs_name, is_folder, main_file, name, sort_name, regions, languages, tags, size_bytes, file_mtime, added_by, created_at, updated_at)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, platformID, d.FSName, d.IsFolder, d.MainFile, d.Name, d.SortName, d.Regions, d.Languages, d.Tags, d.SizeBytes, d.MTime, by, now, now)
			if err != nil {
				return res, err
			}
			id, _ := r.LastInsertId()
			if err := replaceFiles(ctx, tx, id, d.Files); err != nil {
				return res, err
			}
			res.Added = append(res.Added, id)
		case c.trashed != "":
			// a trashed game's name came back (re-uploaded): it is a new copy of the same game
			if _, err := tx.ExecContext(ctx, `UPDATE games SET trashed_at = '', trash_path = '', missing_since = '', is_folder = ?, main_file = ?, size_bytes = ?, file_mtime = ?, hashed_at = '', updated_at = ? WHERE id = ?`,
				d.IsFolder, d.MainFile, d.SizeBytes, d.MTime, now, c.id); err != nil {
				return res, err
			}
			if err := replaceFiles(ctx, tx, c.id, d.Files); err != nil {
				return res, err
			}
			res.Restored = append(res.Restored, c.id)
		case c.mtime != d.MTime || c.size != d.SizeBytes || c.missing != "" || c.main != d.MainFile:
			if _, err := tx.ExecContext(ctx, `UPDATE games SET is_folder = ?, main_file = ?, size_bytes = ?, file_mtime = ?, missing_since = '', hashed_at = CASE WHEN size_bytes = ? AND file_mtime = ? THEN hashed_at ELSE '' END, updated_at = ? WHERE id = ?`,
				d.IsFolder, d.MainFile, d.SizeBytes, d.MTime, d.SizeBytes, d.MTime, now, c.id); err != nil {
				return res, err
			}
			if err := replaceFiles(ctx, tx, c.id, d.Files); err != nil {
				return res, err
			}
			if c.missing != "" {
				res.Restored = append(res.Restored, c.id)
			} else {
				res.Changed = append(res.Changed, c.id)
			}
		}
	}
	for name, c := range have {
		if !seen[name] && c.missing == "" && c.trashed == "" {
			if _, err := tx.ExecContext(ctx, `UPDATE games SET missing_since = ?, updated_at = ? WHERE id = ?`, now, now, c.id); err != nil {
				return res, err
			}
			res.Missing = append(res.Missing, c.id)
		}
	}
	return res, tx.Commit()
}

func replaceFiles(ctx context.Context, tx *sql.Tx, gameID int64, files []GameFile) error {
	// keep the hashes of files that did not change
	type h struct {
		size                  int64
		mtime, crc, md5, sha1 string
	}
	old := map[string]h{}
	rows, err := tx.QueryContext(ctx, `SELECT rel, size_bytes, mtime, crc32, md5, sha1 FROM game_files WHERE game_id = ?`, gameID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var rel string
		var x h
		if err := rows.Scan(&rel, &x.size, &x.mtime, &x.crc, &x.md5, &x.sha1); err != nil {
			rows.Close()
			return err
		}
		old[rel] = x
	}
	rows.Close()
	if _, err := tx.ExecContext(ctx, `DELETE FROM game_files WHERE game_id = ?`, gameID); err != nil {
		return err
	}
	for _, f := range files {
		if o, ok := old[f.Rel]; ok && o.size == f.SizeBytes && o.mtime == f.MTime {
			f.CRC32, f.MD5, f.SHA1 = o.crc, o.md5, o.sha1
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO game_files (game_id, rel, size_bytes, mtime, crc32, md5, sha1) VALUES (?,?,?,?,?,?,?)`,
			gameID, f.Rel, f.SizeBytes, f.MTime, f.CRC32, f.MD5, f.SHA1); err != nil {
			return err
		}
	}
	return nil
}

// GameFiles lists a game's files.
func (s *Store) GameFiles(ctx context.Context, gameID int64) ([]GameFile, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, game_id, rel, size_bytes, mtime, crc32, md5, sha1 FROM game_files WHERE game_id = ? ORDER BY rel`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GameFile{}
	for rows.Next() {
		var f GameFile
		if err := rows.Scan(&f.ID, &f.GameID, &f.Rel, &f.SizeBytes, &f.MTime, &f.CRC32, &f.MD5, &f.SHA1); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SetFileHashes stores the checksums of one file (and of the game when it is the main file).
func (s *Store) SetFileHashes(ctx context.Context, fileID int64, crc, md5, sha1 string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE game_files SET crc32 = ?, md5 = ?, sha1 = ? WHERE id = ?`, crc, md5, sha1, fileID)
	return err
}

// SetGameHashes stores the main file's checksums and marks the game hashed.
func (s *Store) SetGameHashes(ctx context.Context, id int64, crc, md5, sha1 string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE games SET crc32 = ?, md5 = ?, sha1 = ?, hashed_at = ? WHERE id = ?`, crc, md5, sha1, Now(), id)
	return err
}

// UnhashedGames lists games whose files were not hashed yet.
func (s *Store) UnhashedGames(ctx context.Context, limit int) ([]int64, error) {
	return s.ids(ctx, `SELECT id FROM games WHERE hashed_at = '' AND trashed_at = '' AND missing_since = '' ORDER BY id LIMIT ?`, limit)
}

// PendingMeta lists games waiting for a lookup.
func (s *Store) PendingMeta(ctx context.Context, limit int) ([]int64, error) {
	return s.ids(ctx, `SELECT id FROM games WHERE meta_status = 'pending' AND trashed_at = '' AND missing_since = '' ORDER BY id LIMIT ?`, limit)
}

func (s *Store) ids(ctx context.Context, q string, args ...any) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// GameMeta is what can be said about a game (looked up or typed in).
type GameMeta struct {
	Name          *string `json:"name"`
	Summary       *string `json:"summary"`
	SummarySource *string `json:"summary_source"`
	SummaryURL    *string `json:"summary_url"`
	Genres        *string `json:"genres"`
	Developer     *string `json:"developer"`
	Publisher     *string `json:"publisher"`
	ReleaseDate   *string `json:"release_date"`
	Players       *string `json:"players"`
	WikiTitle     *string `json:"wiki_title"`
	MetaStatus    *string `json:"meta_status"`
	MetaLocked    *bool   `json:"meta_locked"`
}

// UpdateGameMeta changes the given fields.
func (s *Store) UpdateGameMeta(ctx context.Context, id int64, m GameMeta) error {
	sets := []string{"updated_at = ?"}
	args := []any{Now()}
	add := func(col string, v *string) {
		if v != nil {
			sets = append(sets, col+" = ?")
			args = append(args, strings.TrimSpace(*v))
		}
	}
	add("name", m.Name)
	if m.Name != nil {
		sets = append(sets, "sort_name = ?")
		args = append(args, sortName(*m.Name))
	}
	add("summary", m.Summary)
	add("summary_source", m.SummarySource)
	add("summary_url", m.SummaryURL)
	add("genres", m.Genres)
	add("developer", m.Developer)
	add("publisher", m.Publisher)
	add("release_date", m.ReleaseDate)
	add("players", m.Players)
	add("wiki_title", m.WikiTitle)
	if m.MetaStatus != nil {
		sets = append(sets, "meta_status = ?", "meta_at = ?")
		args = append(args, *m.MetaStatus, Now())
	}
	if m.MetaLocked != nil {
		sets = append(sets, "meta_locked = ?")
		args = append(args, boolInt(*m.MetaLocked))
	}
	args = append(args, id)
	res, err := s.DB.ExecContext(ctx, `UPDATE games SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return affected(res, err)
}

func sortName(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	for _, a := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(t, a) && len(t) > len(a) {
			return t[len(a):]
		}
	}
	return t
}

// SetCover stores the cover picture's path (” = none).
func (s *Store) SetCover(ctx context.Context, id int64, path, source string) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE games SET cover_path = ?, cover_source = ?, updated_at = ? WHERE id = ?`, path, source, Now(), id)
	return affected(res, err)
}

// SetShots stores the in-game pictures.
func (s *Store) SetShots(ctx context.Context, id int64, paths []string) error {
	if paths == nil {
		paths = []string{}
	}
	b, _ := json.Marshal(paths)
	_, err := s.DB.ExecContext(ctx, `UPDATE games SET shots = ?, updated_at = ? WHERE id = ?`, string(b), Now(), id)
	return err
}

// ResetMeta queues games for a new lookup (locked ones stay as typed).
func (s *Store) ResetMeta(ctx context.Context, ids []int64) error {
	q := `UPDATE games SET meta_status = 'pending' WHERE meta_locked = 0`
	args := []any{}
	if len(ids) > 0 {
		q += ` AND id IN (` + placeholders(len(ids)) + `)`
		for _, id := range ids {
			args = append(args, id)
		}
	}
	_, err := s.DB.ExecContext(ctx, q, args...)
	return err
}

// TrashGame marks a game deleted; its files were moved to trashPath.
func (s *Store) TrashGame(ctx context.Context, id int64, trashPath string) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE games SET trashed_at = ?, trash_path = ?, updated_at = ? WHERE id = ? AND trashed_at = ''`, Now(), trashPath, Now(), id)
	return affected(res, err)
}

// UntrashGame brings a game back (its files are back in the library).
func (s *Store) UntrashGame(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE games SET trashed_at = '', trash_path = '', missing_since = '', updated_at = ? WHERE id = ?`, Now(), id)
	return affected(res, err)
}

// PurgeGame removes a game row for good (its files are already gone).
func (s *Store) PurgeGame(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM games WHERE id = ?`, id)
	return affected(res, err)
}

// ExpiredTrash lists games trashed before the cutoff.
func (s *Store) ExpiredTrash(ctx context.Context, before string) ([]Game, error) {
	ids, err := s.ids(ctx, `SELECT id FROM games WHERE trashed_at != '' AND trashed_at < ?`, before)
	if err != nil {
		return nil, err
	}
	var out []Game
	for _, id := range ids {
		if g, err := s.GetGame(ctx, 0, id); err == nil {
			out = append(out, *g)
		}
	}
	return out, nil
}

// Duplicates lists groups of games whose main files are identical.
func (s *Store) Duplicates(ctx context.Context) ([][]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT GROUP_CONCAT(id) FROM games WHERE md5 != '' AND trashed_at = '' AND missing_since = '' GROUP BY md5 HAVING COUNT(1) > 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]int64
	for rows.Next() {
		var list string
		if err := rows.Scan(&list); err != nil {
			return nil, err
		}
		var g []int64
		for _, p := range strings.Split(list, ",") {
			g = append(g, atoi64(p))
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ---------- firmware ----------

// SyncFirmware makes the firmware rows of a platform match the files.
func (s *Store) SyncFirmware(ctx context.Context, platformID int64, files map[string][2]any) (added []int64, err error) {
	have := map[string]int64{}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, file_name FROM firmware WHERE platform_id = ?`, platformID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return nil, err
		}
		have[n] = id
	}
	rows.Close()
	for name, v := range files {
		size, mtime := v[0].(int64), v[1].(string)
		if id, ok := have[name]; ok {
			if _, err := s.DB.ExecContext(ctx, `UPDATE firmware SET crc32 = CASE WHEN size_bytes = ? AND mtime = ? THEN crc32 ELSE '' END,
				md5 = CASE WHEN size_bytes = ? AND mtime = ? THEN md5 ELSE '' END, sha1 = CASE WHEN size_bytes = ? AND mtime = ? THEN sha1 ELSE '' END,
				size_bytes = ?, mtime = ? WHERE id = ?`, size, mtime, size, mtime, size, mtime, size, mtime, id); err != nil {
				return nil, err
			}
			delete(have, name)
			continue
		}
		r, err := s.DB.ExecContext(ctx, `INSERT INTO firmware (platform_id, file_name, size_bytes, mtime) VALUES (?,?,?,?)`, platformID, name, size, mtime)
		if err != nil {
			return nil, err
		}
		id, _ := r.LastInsertId()
		added = append(added, id)
	}
	for _, id := range have {
		if _, err := s.DB.ExecContext(ctx, `DELETE FROM firmware WHERE id = ?`, id); err != nil {
			return nil, err
		}
	}
	return added, nil
}

// ListFirmware lists firmware files (platformID 0 = all).
func (s *Store) ListFirmware(ctx context.Context, platformID int64) ([]Firmware, error) {
	q := `SELECT f.id, f.platform_id, p.slug, f.file_name, f.size_bytes, f.mtime, f.crc32, f.md5, f.sha1, f.created_at FROM firmware f JOIN platforms p ON p.id = f.platform_id`
	args := []any{}
	if platformID > 0 {
		q += ` WHERE f.platform_id = ?`
		args = append(args, platformID)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY p.slug, f.file_name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Firmware{}
	for rows.Next() {
		var f Firmware
		if err := rows.Scan(&f.ID, &f.PlatformID, &f.PlatformSlug, &f.FileName, &f.SizeBytes, &f.MTime, &f.CRC32, &f.MD5, &f.SHA1, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetFirmware returns one firmware file.
func (s *Store) GetFirmware(ctx context.Context, id int64) (*Firmware, error) {
	all, err := s.ListFirmware(ctx, 0)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, ErrNotFound
}

// SetFirmwareHashes stores a firmware file's checksums.
func (s *Store) SetFirmwareHashes(ctx context.Context, id int64, crc, md5, sha1 string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE firmware SET crc32 = ?, md5 = ?, sha1 = ? WHERE id = ?`, crc, md5, sha1, id)
	return err
}

// ---------- helpers ----------

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func atoi64(s string) int64 {
	var n int64
	for _, r := range strings.TrimSpace(s) {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int64(r-'0')
	}
	return n
}
