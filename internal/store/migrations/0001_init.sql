CREATE TABLE users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'member',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Box-wide settings (title, setup flag); everything else is personal.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

CREATE TABLE cache (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  fetched_at TEXT NOT NULL
);

-- ============ the catalogue (shared by everyone on the box) ============
-- Platforms and games mirror the library folder: one folder per platform,
-- one file or folder per game. Ids are never reused (AUTOINCREMENT): saves,
-- links from other apps and RomM apps on devices point at them.

CREATE TABLE platforms (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  slug        TEXT NOT NULL UNIQUE,              -- the folder name (ps2, psx, gba …)
  custom_name TEXT NOT NULL DEFAULT '',          -- '' = the built-in name
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE games (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  platform_id    INTEGER NOT NULL REFERENCES platforms(id) ON DELETE CASCADE,
  fs_name        TEXT NOT NULL,                  -- file or folder name inside the platform folder
  is_folder      INTEGER NOT NULL DEFAULT 0,     -- a folder of files (discs, cue/bin, m3u …)
  main_file      TEXT NOT NULL DEFAULT '',       -- the file an emulator opens (relative to the folder)
  name           TEXT NOT NULL,                  -- clean title ("Silent Hill 2")
  sort_name      TEXT NOT NULL DEFAULT '',
  regions        TEXT NOT NULL DEFAULT '',       -- "Europe, Australia"
  languages      TEXT NOT NULL DEFAULT '',       -- "En, Fr, De"
  tags           TEXT NOT NULL DEFAULT '',       -- "Rev 1, Beta" (the other bits of the file name)
  size_bytes     INTEGER NOT NULL DEFAULT 0,
  file_mtime     TEXT NOT NULL DEFAULT '',
  crc32          TEXT NOT NULL DEFAULT '',       -- of the main file (when not too big)
  md5            TEXT NOT NULL DEFAULT '',
  sha1           TEXT NOT NULL DEFAULT '',
  hashed_at      TEXT NOT NULL DEFAULT '',
  -- what the game is (looked up online, or typed in)
  summary        TEXT NOT NULL DEFAULT '',
  summary_source TEXT NOT NULL DEFAULT '',       -- wikipedia | romm | manual
  summary_url    TEXT NOT NULL DEFAULT '',
  genres         TEXT NOT NULL DEFAULT '',       -- "Survival horror, Action"
  developer      TEXT NOT NULL DEFAULT '',
  publisher      TEXT NOT NULL DEFAULT '',
  release_date   TEXT NOT NULL DEFAULT '',       -- YYYY-MM-DD, YYYY-MM or YYYY
  players        TEXT NOT NULL DEFAULT '',       -- "Single-player, Multiplayer"
  cover_path     TEXT NOT NULL DEFAULT '',       -- FILES_DIR relative
  cover_source   TEXT NOT NULL DEFAULT '',       -- libretro | upload | romm | wikipedia
  shots          TEXT NOT NULL DEFAULT '[]',     -- JSON [FILES_DIR relative paths]
  wiki_title     TEXT NOT NULL DEFAULT '',
  meta_status    TEXT NOT NULL DEFAULT 'pending',-- pending | matched | partial | unmatched
  meta_locked    INTEGER NOT NULL DEFAULT 0,     -- typed in by hand: lookups never overwrite
  meta_at        TEXT NOT NULL DEFAULT '',
  missing_since  TEXT NOT NULL DEFAULT '',       -- the file disappeared from the library
  trashed_at     TEXT NOT NULL DEFAULT '',       -- deleted: the files wait in the trash folder
  trash_path     TEXT NOT NULL DEFAULT '',       -- where (relative to the library)
  added_by       INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (platform_id, fs_name)
);
CREATE INDEX games_platform ON games(platform_id, sort_name);
CREATE INDEX games_meta ON games(meta_status);

-- Every file of a game (one row for a single-file game).
CREATE TABLE game_files (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  game_id    INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
  rel        TEXT NOT NULL,                      -- relative to the game folder ('' parent for single files)
  size_bytes INTEGER NOT NULL DEFAULT 0,
  mtime      TEXT NOT NULL DEFAULT '',
  crc32      TEXT NOT NULL DEFAULT '',
  md5        TEXT NOT NULL DEFAULT '',
  sha1       TEXT NOT NULL DEFAULT '',
  UNIQUE (game_id, rel)
);

-- BIOS / firmware files (bios/<platform>/).
CREATE TABLE firmware (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  platform_id INTEGER NOT NULL REFERENCES platforms(id) ON DELETE CASCADE,
  file_name   TEXT NOT NULL,
  size_bytes  INTEGER NOT NULL DEFAULT 0,
  mtime       TEXT NOT NULL DEFAULT '',
  crc32       TEXT NOT NULL DEFAULT '',
  md5         TEXT NOT NULL DEFAULT '',
  sha1        TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (platform_id, file_name)
);

-- ============ per person ============

-- One person's relation to a game.
CREATE TABLE user_games (
  user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  game_id        INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
  favorite       INTEGER NOT NULL DEFAULT 0,
  status         TEXT NOT NULL DEFAULT '',       -- '' | backlog | playing | finished | completed | dropped
  rating         INTEGER NOT NULL DEFAULT 0,     -- 0 = none, 1..10 (half stars)
  notes          TEXT NOT NULL DEFAULT '',
  hidden         INTEGER NOT NULL DEFAULT 0,
  play_seconds   INTEGER NOT NULL DEFAULT 0,
  play_count     INTEGER NOT NULL DEFAULT 0,
  last_played_at TEXT NOT NULL DEFAULT '',
  core           TEXT NOT NULL DEFAULT '',       -- the browser emulator picked for this game ('' = default)
  updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (user_id, game_id)
);
CREATE INDEX user_games_played ON user_games(user_id, last_played_at);

-- One sitting with a game (in the browser, or reported by a device).
CREATE TABLE play_sessions (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  game_id    INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
  started_at TEXT NOT NULL,
  ended_at   TEXT NOT NULL DEFAULT '',
  seconds    INTEGER NOT NULL DEFAULT 0,
  day        TEXT NOT NULL,                      -- local YYYY-MM-DD of the start
  device     TEXT NOT NULL DEFAULT 'browser',
  published  INTEGER NOT NULL DEFAULT 0          -- the "played" event went out
);
CREATE INDEX play_sessions_user ON play_sessions(user_id, started_at);
CREATE INDEX play_sessions_day ON play_sessions(user_id, day);
CREATE INDEX play_sessions_game ON play_sessions(user_id, game_id, started_at);

-- In-game saves (battery/memory card) and save states, one file each.
CREATE TABLE saves (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  game_id     INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL DEFAULT 'save',      -- save | state
  file_name   TEXT NOT NULL,
  emulator    TEXT NOT NULL DEFAULT '',          -- the core / emulator that wrote it
  slot        TEXT NOT NULL DEFAULT '',          -- 'auto' = written by the player itself
  path        TEXT NOT NULL,                     -- FILES_DIR relative
  size_bytes  INTEGER NOT NULL DEFAULT 0,
  md5         TEXT NOT NULL DEFAULT '',
  shot_path   TEXT NOT NULL DEFAULT '',          -- a picture of the moment (states)
  device      TEXT NOT NULL DEFAULT 'browser',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX saves_game ON saves(user_id, game_id, kind, updated_at);

CREATE TABLE collections (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX collections_user ON collections(user_id, name);

CREATE TABLE collection_games (
  collection_id INTEGER NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
  game_id       INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  position      INTEGER NOT NULL DEFAULT 0,
  added_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (collection_id, game_id)
);
CREATE INDEX collection_games_user ON collection_games(user_id, game_id);

-- Uploads in progress (resumable: the browser sends chunks at an offset).
CREATE TABLE uploads (
  id          TEXT PRIMARY KEY,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL DEFAULT 'game',      -- game | firmware
  platform    TEXT NOT NULL,                     -- platform slug
  rel         TEXT NOT NULL,                     -- target path inside the platform folder
  size_bytes  INTEGER NOT NULL,
  received    INTEGER NOT NULL DEFAULT 0,
  status      TEXT NOT NULL DEFAULT 'open',      -- open | done | failed
  error       TEXT NOT NULL DEFAULT '',
  game_id     INTEGER,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX uploads_user ON uploads(user_id, status);

-- Apps and handhelds that talk to the RomM-compatible API (per person):
-- registered ones (device flow, POST /api/devices) and ones only seen by
-- their user agent (identifier "ua:<agent>").
CREATE TABLE devices (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  uuid         TEXT NOT NULL UNIQUE,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  identifier   TEXT NOT NULL,                    -- the client's own id for itself
  name         TEXT NOT NULL DEFAULT '',
  client       TEXT NOT NULL DEFAULT '',         -- app name / user agent
  client_version TEXT NOT NULL DEFAULT '',
  platform     TEXT NOT NULL DEFAULT '',
  hostname     TEXT NOT NULL DEFAULT '',
  ip           TEXT NOT NULL DEFAULT '',
  first_seen   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  last_seen    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  requests     INTEGER NOT NULL DEFAULT 0,
  downloads    INTEGER NOT NULL DEFAULT 0,
  UNIQUE (user_id, identifier)
);

-- Tokens for RomM apps: rmm_ client tokens (pairing, device flow, made by
-- hand) and the access/refresh pairs of /api/token. Only hashes are kept.
CREATE TABLE compat_tokens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  token_hash   TEXT NOT NULL UNIQUE,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind         TEXT NOT NULL DEFAULT 'client',   -- client | access | refresh
  name         TEXT NOT NULL DEFAULT '',
  scopes       TEXT NOT NULL DEFAULT '',
  device_uuid  TEXT NOT NULL DEFAULT '',
  expires_at   TEXT NOT NULL DEFAULT '',         -- '' = never
  last_used_at TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX compat_tokens_user ON compat_tokens(user_id, kind);
