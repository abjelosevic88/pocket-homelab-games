package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// ---------- one person's relation to a game ----------

// UserGamePatch changes the given fields of a person's relation to a game.
type UserGamePatch struct {
	Favorite *bool   `json:"favorite"`
	Status   *string `json:"status"`
	Rating   *int    `json:"rating"`
	Notes    *string `json:"notes"`
	Hidden   *bool   `json:"hidden"`
	Core     *string `json:"core"`
	// set by imports: the time already played elsewhere
	LastPlayedAt *string `json:"last_played_at"`
}

// PatchUserGame applies the patch (creating the row).
func (s *Store) PatchUserGame(ctx context.Context, uid, gameID int64, p UserGamePatch) error {
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO user_games (user_id, game_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, uid, gameID); err != nil {
		return err
	}
	sets := []string{"updated_at = ?"}
	args := []any{Now()}
	if p.Favorite != nil {
		sets = append(sets, "favorite = ?")
		args = append(args, boolInt(*p.Favorite))
	}
	if p.Status != nil {
		sets = append(sets, "status = ?")
		args = append(args, *p.Status)
	}
	if p.Rating != nil {
		sets = append(sets, "rating = ?")
		args = append(args, min(max(*p.Rating, 0), 10))
	}
	if p.Notes != nil {
		sets = append(sets, "notes = ?")
		args = append(args, *p.Notes)
	}
	if p.Hidden != nil {
		sets = append(sets, "hidden = ?")
		args = append(args, boolInt(*p.Hidden))
	}
	if p.Core != nil {
		sets = append(sets, "core = ?")
		args = append(args, *p.Core)
	}
	if p.LastPlayedAt != nil {
		sets = append(sets, "last_played_at = CASE WHEN last_played_at < ? THEN ? ELSE last_played_at END")
		args = append(args, *p.LastPlayedAt, *p.LastPlayedAt)
	}
	args = append(args, uid, gameID)
	_, err := s.DB.ExecContext(ctx, `UPDATE user_games SET `+strings.Join(sets, ", ")+` WHERE user_id = ? AND game_id = ?`, args...)
	return err
}

// ---------- play sessions ----------

// StartSession opens a sitting (and returns it).
func (s *Store) StartSession(ctx context.Context, uid, gameID int64, device string) (*Session, error) {
	now := time.Now()
	r, err := s.DB.ExecContext(ctx, `INSERT INTO play_sessions (user_id, game_id, started_at, ended_at, day, device) VALUES (?,?,?,?,?,?)`,
		uid, gameID, now.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano), now.Format("2006-01-02"), device)
	if err != nil {
		return nil, err
	}
	id, _ := r.LastInsertId()
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO user_games (user_id, game_id, play_count, last_played_at) VALUES (?, ?, 1, ?)
		ON CONFLICT(user_id, game_id) DO UPDATE SET play_count = play_count + 1, last_played_at = excluded.last_played_at,
		status = CASE WHEN status IN ('', 'backlog') THEN 'playing' ELSE status END, updated_at = excluded.last_played_at`, uid, gameID, Now()); err != nil {
		return nil, err
	}
	return s.GetSession(ctx, uid, id)
}

// TouchSession extends an open sitting to now (a heartbeat from the player).
// Gaps longer than idle (the tab slept) are not counted.
func (s *Store) TouchSession(ctx context.Context, uid, id int64, idle time.Duration) (*Session, error) {
	se, err := s.GetSession(ctx, uid, id)
	if err != nil {
		return nil, err
	}
	end, _ := time.Parse(time.RFC3339Nano, se.EndedAt)
	now := time.Now().UTC()
	add := now.Sub(end)
	if add < 0 || add > idle {
		add = 0
	}
	sec := int64(add.Seconds())
	if _, err := s.DB.ExecContext(ctx, `UPDATE play_sessions SET ended_at = ?, seconds = seconds + ? WHERE id = ? AND user_id = ?`, now.Format(time.RFC3339Nano), sec, id, uid); err != nil {
		return nil, err
	}
	if sec > 0 {
		if _, err := s.DB.ExecContext(ctx, `UPDATE user_games SET play_seconds = play_seconds + ?, last_played_at = ? WHERE user_id = ? AND game_id = ?`, sec, Now(), uid, se.GameID); err != nil {
			return nil, err
		}
	}
	return s.GetSession(ctx, uid, id)
}

// AddPlayTime records a sitting reported afterwards (a device, an import).
func (s *Store) AddPlayTime(ctx context.Context, uid, gameID int64, startedAt time.Time, seconds int64, device string) error {
	end := startedAt.Add(time.Duration(seconds) * time.Second)
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO play_sessions (user_id, game_id, started_at, ended_at, seconds, day, device, published) VALUES (?,?,?,?,?,?,?,1)`,
		uid, gameID, startedAt.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano), seconds, startedAt.Local().Format("2006-01-02"), device); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO user_games (user_id, game_id, play_seconds, play_count, last_played_at) VALUES (?, ?, ?, 1, ?)
		ON CONFLICT(user_id, game_id) DO UPDATE SET play_seconds = play_seconds + excluded.play_seconds, play_count = play_count + 1,
		last_played_at = MAX(last_played_at, excluded.last_played_at)`, uid, gameID, seconds, end.UTC().Format(time.RFC3339Nano))
	return err
}

const sessionCols = `s.id, s.game_id, g.name, p.slug, s.started_at, s.ended_at, s.seconds, s.day, s.device, s.published`

func scanSession(row interface{ Scan(...any) error }) (*Session, error) {
	var se Session
	err := row.Scan(&se.ID, &se.GameID, &se.GameName, &se.Platform, &se.StartedAt, &se.EndedAt, &se.Seconds, &se.Day, &se.Device, &se.Published)
	return &se, err
}

// GetSession returns one of the person's sittings.
func (s *Store) GetSession(ctx context.Context, uid, id int64) (*Session, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM play_sessions s JOIN games g ON g.id = s.game_id JOIN platforms p ON p.id = g.platform_id WHERE s.id = ? AND s.user_id = ?`, id, uid)
	se, err := scanSession(row)
	return se, notFound(err)
}

// SessionFilter narrows the sittings list.
type SessionFilter struct {
	GameID   int64
	From, To string // days, inclusive
	Limit    int
}

// ListSessions lists a person's sittings, newest first (sittings under 10 s are left out).
func (s *Store) ListSessions(ctx context.Context, uid int64, f SessionFilter) ([]Session, error) {
	q := `SELECT ` + sessionCols + ` FROM play_sessions s JOIN games g ON g.id = s.game_id JOIN platforms p ON p.id = g.platform_id WHERE s.user_id = ? AND s.seconds >= 10`
	args := []any{uid}
	if f.GameID > 0 {
		q += ` AND s.game_id = ?`
		args = append(args, f.GameID)
	}
	if f.From != "" {
		q += ` AND s.day >= ?`
		args = append(args, f.From)
	}
	if f.To != "" {
		q += ` AND s.day <= ?`
		args = append(args, f.To)
	}
	q += ` ORDER BY s.started_at DESC`
	if f.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, f.Limit)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		se, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *se)
	}
	return out, rows.Err()
}

// FinishedUnpublished lists sittings that ended a while ago and were not announced yet.
func (s *Store) FinishedUnpublished(ctx context.Context, before string) ([]struct {
	UserID int64
	Session
}, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT s.user_id, `+sessionCols+` FROM play_sessions s JOIN games g ON g.id = s.game_id JOIN platforms p ON p.id = g.platform_id
		WHERE s.published = 0 AND s.ended_at < ? ORDER BY s.id LIMIT 100`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		UserID int64
		Session
	}
	for rows.Next() {
		var x struct {
			UserID int64
			Session
		}
		se := &x.Session
		if err := rows.Scan(&x.UserID, &se.ID, &se.GameID, &se.GameName, &se.Platform, &se.StartedAt, &se.EndedAt, &se.Seconds, &se.Day, &se.Device, &se.Published); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// MarkPublished notes that a sitting was announced.
func (s *Store) MarkPublished(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE play_sessions SET published = 1 WHERE id = ?`, id)
	return err
}

// DeletePlaySession removes one sitting and its time.
func (s *Store) DeletePlaySession(ctx context.Context, uid, id int64) error {
	se, err := s.GetSession(ctx, uid, id)
	if err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM play_sessions WHERE id = ? AND user_id = ?`, id, uid); err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE user_games SET play_seconds = MAX(0, play_seconds - ?), play_count = MAX(0, play_count - 1) WHERE user_id = ? AND game_id = ?`, se.Seconds, uid, se.GameID)
	return err
}

// ---------- stats ----------

// DayTotal is the time played on one day.
type DayTotal struct {
	Day     string `json:"day"`
	Seconds int64  `json:"seconds"`
}

// NameTotal is the time played per game or platform.
type NameTotal struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Seconds  int64  `json:"seconds"`
	Sessions int    `json:"sessions"`
}

// PlayByDay sums the play time per day in [from, to].
func (s *Store) PlayByDay(ctx context.Context, uid int64, from, to string) ([]DayTotal, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT day, SUM(seconds) FROM play_sessions WHERE user_id = ? AND day >= ? AND day <= ? GROUP BY day ORDER BY day`, uid, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DayTotal{}
	for rows.Next() {
		var d DayTotal
		if err := rows.Scan(&d.Day, &d.Seconds); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// TopGames sums the play time per game in [from, to].
func (s *Store) TopGames(ctx context.Context, uid int64, from, to string, limit int) ([]NameTotal, error) {
	return s.nameTotals(ctx, `SELECT g.id, g.name, p.slug, SUM(s.seconds), COUNT(1) FROM play_sessions s JOIN games g ON g.id = s.game_id JOIN platforms p ON p.id = g.platform_id
		WHERE s.user_id = ? AND s.day >= ? AND s.day <= ? AND s.seconds >= 10 GROUP BY g.id ORDER BY SUM(s.seconds) DESC LIMIT ?`, uid, from, to, limit)
}

// TopPlatforms sums the play time per platform in [from, to].
func (s *Store) TopPlatforms(ctx context.Context, uid int64, from, to string) ([]NameTotal, error) {
	return s.nameTotals(ctx, `SELECT p.id, p.slug, p.slug, SUM(s.seconds), COUNT(1) FROM play_sessions s JOIN games g ON g.id = s.game_id JOIN platforms p ON p.id = g.platform_id
		WHERE s.user_id = ? AND s.day >= ? AND s.day <= ? AND s.seconds >= 10 GROUP BY p.id ORDER BY SUM(s.seconds) DESC`, uid, from, to)
}

func (s *Store) nameTotals(ctx context.Context, q string, args ...any) ([]NameTotal, error) {
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NameTotal{}
	for rows.Next() {
		var n NameTotal
		if err := rows.Scan(&n.ID, &n.Name, &n.Platform, &n.Seconds, &n.Sessions); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// StatusCounts counts the person's games per status (and favourites).
func (s *Store) StatusCounts(ctx context.Context, uid int64) (map[string]int, error) {
	out := map[string]int{}
	rows, err := s.DB.QueryContext(ctx, `SELECT u.status, COUNT(1) FROM user_games u JOIN games g ON g.id = u.game_id
		WHERE u.user_id = ? AND u.status != '' AND g.trashed_at = '' GROUP BY u.status`, uid)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			rows.Close()
			return nil, err
		}
		out[st] = n
	}
	rows.Close()
	var fav, played int
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(u.favorite),0), COALESCE(SUM(CASE WHEN u.last_played_at != '' THEN 1 ELSE 0 END),0) FROM user_games u JOIN games g ON g.id = u.game_id WHERE u.user_id = ? AND g.trashed_at = ''`, uid).Scan(&fav, &played)
	out["favorites"], out["played"] = fav, played
	return out, nil
}

// TotalPlay is the person's play time and sittings in [from, to].
func (s *Store) TotalPlay(ctx context.Context, uid int64, from, to string) (seconds int64, sessions, days, games int, err error) {
	err = s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(seconds),0), COUNT(1), COUNT(DISTINCT day), COUNT(DISTINCT game_id) FROM play_sessions
		WHERE user_id = ? AND day >= ? AND day <= ? AND seconds >= 10`, uid, from, to).Scan(&seconds, &sessions, &days, &games)
	return
}

// ---------- saves and states ----------

const saveCols = `id, user_id, game_id, kind, file_name, emulator, slot, path, size_bytes, md5, shot_path, device, created_at, updated_at`

func scanSave(row interface{ Scan(...any) error }) (*Save, error) {
	var v Save
	err := row.Scan(&v.ID, &v.UserID, &v.GameID, &v.Kind, &v.FileName, &v.Emulator, &v.Slot, &v.Path, &v.SizeBytes, &v.MD5, &v.ShotPath, &v.Device, &v.CreatedAt, &v.UpdatedAt)
	v.HasShot = v.ShotPath != ""
	return &v, err
}

// ListSaves lists the person's saves (gameID 0 = all games; kind ” = both).
func (s *Store) ListSaves(ctx context.Context, uid, gameID int64, kind string) ([]Save, error) {
	q := `SELECT ` + saveCols + ` FROM saves WHERE user_id = ?`
	args := []any{uid}
	if gameID > 0 {
		q += ` AND game_id = ?`
		args = append(args, gameID)
	}
	if kind != "" {
		q += ` AND kind = ?`
		args = append(args, kind)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY updated_at DESC, id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Save{}
	for rows.Next() {
		v, err := scanSave(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// GetSave returns one of the person's saves.
func (s *Store) GetSave(ctx context.Context, uid, id int64) (*Save, error) {
	v, err := scanSave(s.DB.QueryRowContext(ctx, `SELECT `+saveCols+` FROM saves WHERE id = ? AND user_id = ?`, id, uid))
	if err != nil {
		return nil, notFound(err)
	}
	return v, nil
}

// FindSave returns the person's save with this slot or file name (to overwrite).
func (s *Store) FindSave(ctx context.Context, uid, gameID int64, kind, fileName, slot string) (*Save, error) {
	q := `SELECT ` + saveCols + ` FROM saves WHERE user_id = ? AND game_id = ? AND kind = ?`
	args := []any{uid, gameID, kind}
	if slot != "" {
		q += ` AND slot = ?`
		args = append(args, slot)
	} else {
		q += ` AND file_name = ?`
		args = append(args, fileName)
	}
	v, err := scanSave(s.DB.QueryRowContext(ctx, q+` ORDER BY updated_at DESC LIMIT 1`, args...))
	if err != nil {
		return nil, notFound(err)
	}
	return v, nil
}

// PutSave creates or replaces a save (v.ID 0 = new).
func (s *Store) PutSave(ctx context.Context, v *Save) error {
	now := Now()
	if v.ID == 0 {
		r, err := s.DB.ExecContext(ctx, `INSERT INTO saves (user_id, game_id, kind, file_name, emulator, slot, path, size_bytes, md5, shot_path, device, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`, v.UserID, v.GameID, v.Kind, v.FileName, v.Emulator, v.Slot, v.Path, v.SizeBytes, v.MD5, v.ShotPath, v.Device, now, now)
		if err != nil {
			return err
		}
		v.ID, _ = r.LastInsertId()
		v.CreatedAt, v.UpdatedAt = now, now
		return nil
	}
	v.UpdatedAt = now
	_, err := s.DB.ExecContext(ctx, `UPDATE saves SET file_name = ?, emulator = ?, slot = ?, path = ?, size_bytes = ?, md5 = ?, shot_path = ?, device = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		v.FileName, v.Emulator, v.Slot, v.Path, v.SizeBytes, v.MD5, v.ShotPath, v.Device, now, v.ID, v.UserID)
	return err
}

// DeleteSave removes a save row (the caller removes the files).
func (s *Store) DeleteSave(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM saves WHERE id = ? AND user_id = ?`, id, uid)
	return affected(res, err)
}

// ---------- collections ----------

// ListCollections lists the person's collections with their games.
func (s *Store) ListCollections(ctx context.Context, uid int64) ([]Collection, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, name, description, created_at, updated_at FROM collections WHERE user_id = ? ORDER BY LOWER(name)`, uid)
	if err != nil {
		return nil, err
	}
	out := []Collection{}
	for rows.Next() {
		var c Collection
		if err := rows.Scan(&c.ID, &c.Name, &c.Description, &c.CreatedAt, &c.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, c)
	}
	rows.Close()
	for i := range out {
		ids, err := s.ids(ctx, `SELECT cg.game_id FROM collection_games cg JOIN games g ON g.id = cg.game_id WHERE cg.collection_id = ? AND cg.user_id = ? AND g.trashed_at = '' ORDER BY cg.position, cg.added_at`, out[i].ID, uid)
		if err != nil {
			return nil, err
		}
		if ids == nil {
			ids = []int64{}
		}
		out[i].GameIDs, out[i].Games = ids, len(ids)
		out[i].Covers, err = s.ids(ctx, `SELECT cg.game_id FROM collection_games cg JOIN games g ON g.id = cg.game_id WHERE cg.collection_id = ? AND g.cover_path != '' AND g.trashed_at = '' ORDER BY cg.position, cg.added_at LIMIT 4`, out[i].ID)
		if err != nil {
			return nil, err
		}
		if out[i].Covers == nil {
			out[i].Covers = []int64{}
		}
	}
	return out, nil
}

// GetCollection returns one of the person's collections.
func (s *Store) GetCollection(ctx context.Context, uid, id int64) (*Collection, error) {
	all, err := s.ListCollections(ctx, uid)
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

// CreateCollection adds a collection.
func (s *Store) CreateCollection(ctx context.Context, uid int64, name, desc string) (int64, error) {
	r, err := s.DB.ExecContext(ctx, `INSERT INTO collections (user_id, name, description) VALUES (?,?,?)`, uid, name, desc)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

// UpdateCollection renames a collection.
func (s *Store) UpdateCollection(ctx context.Context, uid, id int64, name, desc string) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE collections SET name = ?, description = ?, updated_at = ? WHERE id = ? AND user_id = ?`, name, desc, Now(), id, uid)
	return affected(res, err)
}

// DeleteCollection removes a collection (the games stay).
func (s *Store) DeleteCollection(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM collections WHERE id = ? AND user_id = ?`, id, uid)
	return affected(res, err)
}

// SetCollectionGames replaces the games of a collection (in this order).
func (s *Store) SetCollectionGames(ctx context.Context, uid, id int64, gameIDs []int64) error {
	if _, err := s.GetCollection(ctx, uid, id); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM collection_games WHERE collection_id = ? AND user_id = ?`, id, uid); err != nil {
		return err
	}
	for i, gid := range gameIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO collection_games (collection_id, game_id, user_id, position) SELECT ?, id, ?, ? FROM games WHERE id = ? ON CONFLICT DO NOTHING`, id, uid, i, gid); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE collections SET updated_at = ? WHERE id = ?`, Now(), id); err != nil {
		return err
	}
	return tx.Commit()
}

// ToggleCollectionGame adds a game to a collection or takes it out.
func (s *Store) ToggleCollectionGame(ctx context.Context, uid, id, gameID int64, in bool) error {
	if _, err := s.GetCollection(ctx, uid, id); err != nil {
		return err
	}
	var err error
	if in {
		_, err = s.DB.ExecContext(ctx, `INSERT INTO collection_games (collection_id, game_id, user_id, position) VALUES (?, ?, ?, (SELECT COALESCE(MAX(position),0)+1 FROM collection_games WHERE collection_id = ?)) ON CONFLICT DO NOTHING`, id, gameID, uid, id)
	} else {
		_, err = s.DB.ExecContext(ctx, `DELETE FROM collection_games WHERE collection_id = ? AND game_id = ? AND user_id = ?`, id, gameID, uid)
	}
	if err == nil {
		_, err = s.DB.ExecContext(ctx, `UPDATE collections SET updated_at = ? WHERE id = ?`, Now(), id)
	}
	return err
}

// GameCollections lists the ids of the person's collections holding the game.
func (s *Store) GameCollections(ctx context.Context, uid, gameID int64) ([]int64, error) {
	ids, err := s.ids(ctx, `SELECT collection_id FROM collection_games WHERE user_id = ? AND game_id = ?`, uid, gameID)
	if ids == nil {
		ids = []int64{}
	}
	return ids, err
}

// ---------- uploads ----------

// CreateUpload starts an upload.
func (s *Store) CreateUpload(ctx context.Context, u *Upload) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO uploads (id, user_id, kind, platform, rel, size_bytes) VALUES (?,?,?,?,?,?)`, u.ID, u.UserID, u.Kind, u.Platform, u.Rel, u.SizeBytes)
	return err
}

// GetUpload returns one of the person's uploads.
func (s *Store) GetUpload(ctx context.Context, uid int64, id string) (*Upload, error) {
	var u Upload
	var gid sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT id, user_id, kind, platform, rel, size_bytes, received, status, error, game_id, created_at, updated_at FROM uploads WHERE id = ? AND user_id = ?`, id, uid).
		Scan(&u.ID, &u.UserID, &u.Kind, &u.Platform, &u.Rel, &u.SizeBytes, &u.Received, &u.Status, &u.Error, &gid, &u.CreatedAt, &u.UpdatedAt)
	if gid.Valid {
		u.GameID = &gid.Int64
	}
	return &u, notFound(err)
}

// UploadProgress stores how much arrived.
func (s *Store) UploadProgress(ctx context.Context, id string, received int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE uploads SET received = ?, updated_at = ? WHERE id = ?`, received, Now(), id)
	return err
}

// FinishUpload marks an upload done (or failed).
func (s *Store) FinishUpload(ctx context.Context, id, status, msg string, gameID int64) error {
	var g any
	if gameID > 0 {
		g = gameID
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE uploads SET status = ?, error = ?, game_id = ?, updated_at = ? WHERE id = ?`, status, msg, g, Now(), id)
	return err
}

// StaleUploads lists unfinished uploads untouched since before.
func (s *Store) StaleUploads(ctx context.Context, before string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM uploads WHERE updated_at < ? AND status = 'open'`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// DeleteUpload forgets an upload.
func (s *Store) DeleteUpload(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM uploads WHERE id = ?`, id)
	return err
}

// ---------- devices ----------

const deviceCols = `id, uuid, identifier, name, client, client_version, platform, hostname, ip, first_seen, last_seen, requests, downloads`

func scanDevice(row interface{ Scan(...any) error }) (*Device, error) {
	var d Device
	err := row.Scan(&d.ID, &d.UUID, &d.Identifier, &d.Name, &d.Client, &d.ClientVersion, &d.Platform, &d.Hostname, &d.IP, &d.FirstSeen, &d.LastSeen, &d.Requests, &d.Downloads)
	return &d, err
}

// SeeDevice records a request from an app (download = it fetched a game).
// identifier ” = known only by its user agent.
func (s *Store) SeeDevice(ctx context.Context, uid int64, identifier, client, ip string, download bool) {
	if len(client) > 120 {
		client = client[:120]
	}
	if identifier == "" {
		identifier = "ua:" + client
	}
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO devices (uuid, user_id, identifier, name, client, ip, requests, downloads) VALUES (?,?,?,?,?,?,1,?)
		ON CONFLICT(user_id, identifier) DO UPDATE SET ip = excluded.ip, last_seen = ?, requests = requests + 1, downloads = downloads + excluded.downloads`,
		NewUUID(), uid, identifier, client, client, ip, boolInt(download), Now())
}

// RegisterDevice creates (or finds) a named device and returns it.
func (s *Store) RegisterDevice(ctx context.Context, uid int64, d Device) (*Device, bool, error) {
	if d.Identifier == "" {
		d.Identifier = "dev:" + firstNonEmptyS(d.Hostname, d.Name) + ":" + d.Platform + ":" + d.Client
	}
	if ex, err := scanDevice(s.DB.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE user_id = ? AND identifier = ?`, uid, d.Identifier)); err == nil {
		_, _ = s.DB.ExecContext(ctx, `UPDATE devices SET name = CASE WHEN ? != '' THEN ? ELSE name END, client = ?, client_version = ?, platform = ?, hostname = ?, last_seen = ? WHERE id = ?`,
			d.Name, d.Name, d.Client, d.ClientVersion, d.Platform, d.Hostname, Now(), ex.ID)
		ex, _ = s.GetDevice(ctx, uid, ex.UUID)
		return ex, false, nil
	}
	d.UUID = NewUUID()
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO devices (uuid, user_id, identifier, name, client, client_version, platform, hostname, ip) VALUES (?,?,?,?,?,?,?,?,?)`,
		d.UUID, uid, d.Identifier, d.Name, d.Client, d.ClientVersion, d.Platform, d.Hostname, d.IP); err != nil {
		return nil, false, err
	}
	nd, err := s.GetDevice(ctx, uid, d.UUID)
	return nd, true, err
}

// GetDevice returns one of the person's devices by its uuid.
func (s *Store) GetDevice(ctx context.Context, uid int64, uuid string) (*Device, error) {
	d, err := scanDevice(s.DB.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE user_id = ? AND uuid = ?`, uid, uuid))
	return d, notFound(err)
}

// ListDevices lists the apps that used the person's account.
func (s *Store) ListDevices(ctx context.Context, uid int64) ([]Device, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE user_id = ? ORDER BY last_seen DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// DeleteDevice forgets a device and signs out the tokens it got.
func (s *Store) DeleteDevice(ctx context.Context, uid, id int64) error {
	var uuid string
	if err := s.DB.QueryRowContext(ctx, `SELECT uuid FROM devices WHERE id = ? AND user_id = ?`, id, uid).Scan(&uuid); err != nil {
		return notFound(err)
	}
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM compat_tokens WHERE user_id = ? AND device_uuid = ?`, uid, uuid)
	res, err := s.DB.ExecContext(ctx, `DELETE FROM devices WHERE id = ? AND user_id = ?`, id, uid)
	return affected(res, err)
}

// ---------- RomM-style tokens ----------

// CompatToken is a token handed to a RomM app.
type CompatToken struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"user_id"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Scopes     string `json:"scopes"`
	DeviceUUID string `json:"device_id"`
	ExpiresAt  string `json:"expires_at"`
	LastUsedAt string `json:"last_used_at"`
	CreatedAt  string `json:"created_at"`
}

const compatCols = `id, user_id, kind, name, scopes, device_uuid, expires_at, last_used_at, created_at`

func scanCompat(row interface{ Scan(...any) error }) (*CompatToken, error) {
	var t CompatToken
	err := row.Scan(&t.ID, &t.UserID, &t.Kind, &t.Name, &t.Scopes, &t.DeviceUUID, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt)
	return &t, err
}

// CreateCompatToken stores a new token and returns its plain value.
// Client tokens look like RomM's (rmm_ + 64 hex), so apps recognise them.
func (s *Store) CreateCompatToken(ctx context.Context, t CompatToken, ttl time.Duration) (string, *CompatToken, error) {
	plain := "pgm_" + t.Kind[:1] + RandomToken(28)
	if t.Kind == "client" {
		plain = "rmm_" + RandomHex(32)
	}
	exp := ""
	if ttl > 0 {
		exp = time.Now().Add(ttl).UTC().Format(time.RFC3339)
	}
	r, err := s.DB.ExecContext(ctx, `INSERT INTO compat_tokens (token_hash, user_id, kind, name, scopes, device_uuid, expires_at) VALUES (?,?,?,?,?,?,?)`,
		HashToken(plain), t.UserID, t.Kind, t.Name, t.Scopes, t.DeviceUUID, exp)
	if err != nil {
		return "", nil, err
	}
	id, _ := r.LastInsertId()
	nt, err := scanCompat(s.DB.QueryRowContext(ctx, `SELECT `+compatCols+` FROM compat_tokens WHERE id = ?`, id))
	return plain, nt, err
}

// RegenerateCompatToken gives a client token a new secret (pairing hands it over).
func (s *Store) RegenerateCompatToken(ctx context.Context, uid, id int64) (string, *CompatToken, error) {
	plain := "rmm_" + RandomHex(32)
	res, err := s.DB.ExecContext(ctx, `UPDATE compat_tokens SET token_hash = ? WHERE id = ? AND user_id = ? AND kind = 'client'`, HashToken(plain), id, uid)
	if err := affected(res, err); err != nil {
		return "", nil, err
	}
	t, err := scanCompat(s.DB.QueryRowContext(ctx, `SELECT `+compatCols+` FROM compat_tokens WHERE id = ?`, id))
	return plain, t, err
}

// CompatTokenUser resolves a token (kind ” = any).
func (s *Store) CompatTokenUser(ctx context.Context, plain, kind string) (*User, *CompatToken, error) {
	t, err := scanCompat(s.DB.QueryRowContext(ctx, `SELECT `+compatCols+` FROM compat_tokens WHERE token_hash = ? AND (? = '' OR kind = ?)`, HashToken(plain), kind, kind))
	if err != nil {
		return nil, nil, notFound(err)
	}
	if t.ExpiresAt != "" && t.ExpiresAt < time.Now().UTC().Format(time.RFC3339) {
		return nil, nil, ErrNotFound
	}
	if t.LastUsedAt < time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano) {
		_, _ = s.DB.ExecContext(ctx, `UPDATE compat_tokens SET last_used_at = ? WHERE id = ?`, Now(), t.ID)
	}
	u, err := s.GetUser(ctx, t.UserID)
	return u, t, err
}

// DeleteCompatTokenByHash removes one token (a used refresh token).
func (s *Store) DeleteCompatTokenByHash(ctx context.Context, plain string) {
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM compat_tokens WHERE token_hash = ?`, HashToken(plain))
}

// ListCompatTokens lists the person's client tokens.
func (s *Store) ListCompatTokens(ctx context.Context, uid int64) ([]CompatToken, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+compatCols+` FROM compat_tokens WHERE user_id = ? AND kind = 'client' ORDER BY id DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CompatToken{}
	for rows.Next() {
		t, err := scanCompat(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// GetCompatToken returns one of the person's client tokens.
func (s *Store) GetCompatToken(ctx context.Context, uid, id int64) (*CompatToken, error) {
	t, err := scanCompat(s.DB.QueryRowContext(ctx, `SELECT `+compatCols+` FROM compat_tokens WHERE id = ? AND user_id = ? AND kind = 'client'`, id, uid))
	return t, notFound(err)
}

// DeleteCompatToken removes one of the person's client tokens.
func (s *Store) DeleteCompatToken(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM compat_tokens WHERE id = ? AND user_id = ?`, id, uid)
	return affected(res, err)
}

// PurgeCompatTokens removes expired tokens.
func (s *Store) PurgeCompatTokens(ctx context.Context) {
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM compat_tokens WHERE expires_at != '' AND expires_at < ?`, time.Now().UTC().Format(time.RFC3339))
}

func firstNonEmptyS(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
