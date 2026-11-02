# Backup & restore

Pocket Games keeps two kinds of things apart:

- **`DATA_DIR`** (`/data` in the container) — the app's own data:
  - `pocket-games.db` (+ `-wal` / `-shm` while running) — the catalogue, everyone's favourites, statuses, notes, play time, collections, settings, users
  - `files/covers/`, `files/shots/` — covers and pictures (can be looked up again)
  - `files/saves/<user>/<game>/` — **in-game saves and save states** (back these up)
  - `emulatorjs/` — the player's files, downloaded once per system (no need to back up)
  - `cache/zips/` — zips built for resumable multi-file downloads (no need to back up)
  - `backups/` — where the CLI writes snapshots
- **`LIBRARY_DIR`** (`/library`) — the games and BIOS files themselves, plus `.trash/` (deleted games, kept `TRASH_DAYS`) and `.uploads/` (uploads in progress). Back it up however you back up big media, or not at all if you can get the games again.

## Consistent copies

Never copy a live WAL database file. Use one of:

- **UI**: Settings → Backup & data → *Download database*
- **API**: `GET /api/v1/backup` with a bearer token (admin)
- **CLI**: `docker exec pocket-games pocket-games backup` → prints the path under `/data/backups/`
- **sqlite3**: `sqlite3 data/pocket-games.db "VACUUM INTO 'snap.db'"`

Then back up `files/` with your usual tool (restic, rsync…). Exclude `pocket-games.db*`, `emulatorjs/` and `cache/` from raw file backups and include the snapshot instead.

## Restore

1. Stop the container.
2. Replace `data/pocket-games.db` with the snapshot (remove any stale `-wal` / `-shm`).
3. Restore `files/`.
4. Start the container. Migrations run forward automatically; the library is scanned again on start.

## JSON export

Settings → Backup & data exports one person's favourites, statuses, ratings, notes, play time and collections as JSON. Games are named by system folder and file name, so the export works on another box with the same games. Importing it twice changes nothing the second time.
