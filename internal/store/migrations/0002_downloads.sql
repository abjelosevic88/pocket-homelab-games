-- Games fetched by the server from a link the person pasted.
CREATE TABLE downloads (
  id          TEXT PRIMARY KEY,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  url         TEXT NOT NULL,                     -- what was pasted
  source_url  TEXT NOT NULL DEFAULT '',          -- the direct file address it became
  file_name   TEXT NOT NULL DEFAULT '',
  platform    TEXT NOT NULL DEFAULT '',          -- '' = still to choose
  choices     TEXT NOT NULL DEFAULT '',          -- systems the file name fits ("psx,saturn")
  size_bytes  INTEGER NOT NULL DEFAULT 0,        -- 0 = unknown
  received    INTEGER NOT NULL DEFAULT 0,
  status      TEXT NOT NULL DEFAULT 'queued',    -- queued | downloading | waiting (for a system) | done | failed | canceled
  error       TEXT NOT NULL DEFAULT '',
  game_id     INTEGER,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX downloads_user ON downloads(user_id, created_at);
CREATE INDEX downloads_status ON downloads(status);
