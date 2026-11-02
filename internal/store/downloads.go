package store

import (
	"context"
	"database/sql"
	"strings"
)

// Download is a game the server fetches from a link.
type Download struct {
	ID        string   `json:"id"`
	UserID    int64    `json:"-"`
	URL       string   `json:"url"`
	SourceURL string   `json:"source_url"`
	FileName  string   `json:"file_name"`
	Platform  string   `json:"platform"`
	Choices   []string `json:"choices"`
	SizeBytes int64    `json:"size_bytes"`
	Received  int64    `json:"received"`
	Status    string   `json:"status"`
	Error     string   `json:"error"`
	GameID    *int64   `json:"game_id"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

const downloadCols = `id, user_id, url, source_url, file_name, platform, choices, size_bytes, received, status, error, game_id, created_at, updated_at`

func scanDownload(row interface{ Scan(...any) error }) (*Download, error) {
	var d Download
	var choices string
	var gid sql.NullInt64
	if err := row.Scan(&d.ID, &d.UserID, &d.URL, &d.SourceURL, &d.FileName, &d.Platform, &choices, &d.SizeBytes, &d.Received, &d.Status, &d.Error, &gid, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	d.Choices = []string{}
	for _, c := range strings.Split(choices, ",") {
		if c != "" {
			d.Choices = append(d.Choices, c)
		}
	}
	if gid.Valid {
		d.GameID = &gid.Int64
	}
	return &d, nil
}

// CreateDownload queues a link.
func (s *Store) CreateDownload(ctx context.Context, d *Download) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO downloads (id, user_id, url, platform) VALUES (?,?,?,?)`, d.ID, d.UserID, d.URL, d.Platform)
	return err
}

// GetDownload returns one of the person's downloads (uid 0 = anyone's, for the worker).
func (s *Store) GetDownload(ctx context.Context, uid int64, id string) (*Download, error) {
	d, err := scanDownload(s.DB.QueryRowContext(ctx, `SELECT `+downloadCols+` FROM downloads WHERE id = ? AND (? = 0 OR user_id = ?)`, id, uid, uid))
	if err != nil {
		return nil, notFound(err)
	}
	return d, nil
}

// ListDownloads lists the person's downloads of the last week, newest first.
func (s *Store) ListDownloads(ctx context.Context, uid int64) ([]Download, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+downloadCols+` FROM downloads WHERE user_id = ? AND (status NOT IN ('done','failed','canceled') OR updated_at > strftime('%Y-%m-%dT%H:%M:%fZ','now','-7 days')) ORDER BY created_at DESC LIMIT 100`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Download{}
	for rows.Next() {
		d, err := scanDownload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// QueuedDownloads lists downloads waiting for the worker (oldest first).
func (s *Store) QueuedDownloads(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM downloads WHERE status = 'queued' ORDER BY created_at`)
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

// DownloadPatch changes the given fields.
type DownloadPatch struct {
	SourceURL *string
	FileName  *string
	Platform  *string
	Choices   []string
	SizeBytes *int64
	Received  *int64
	Status    *string
	Error     *string
	GameID    *int64
}

// UpdateDownload applies a patch.
func (s *Store) UpdateDownload(ctx context.Context, id string, p DownloadPatch) error {
	sets := []string{"updated_at = ?"}
	args := []any{Now()}
	str := func(col string, v *string) {
		if v != nil {
			sets = append(sets, col+" = ?")
			args = append(args, *v)
		}
	}
	str("source_url", p.SourceURL)
	str("file_name", p.FileName)
	str("platform", p.Platform)
	str("status", p.Status)
	str("error", p.Error)
	if p.Choices != nil {
		sets = append(sets, "choices = ?")
		args = append(args, strings.Join(p.Choices, ","))
	}
	if p.SizeBytes != nil {
		sets = append(sets, "size_bytes = ?")
		args = append(args, *p.SizeBytes)
	}
	if p.Received != nil {
		sets = append(sets, "received = ?")
		args = append(args, *p.Received)
	}
	if p.GameID != nil {
		sets = append(sets, "game_id = ?")
		args = append(args, *p.GameID)
	}
	args = append(args, id)
	_, err := s.DB.ExecContext(ctx, `UPDATE downloads SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return err
}

// RequeueInterrupted puts downloads that were running when the app stopped back in the queue.
func (s *Store) RequeueInterrupted(ctx context.Context) {
	_, _ = s.DB.ExecContext(ctx, `UPDATE downloads SET status = 'queued' WHERE status = 'downloading'`)
}

// DeleteDownload forgets a download.
func (s *Store) DeleteDownload(ctx context.Context, uid int64, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM downloads WHERE id = ? AND user_id = ?`, id, uid)
	return affected(res, err)
}
