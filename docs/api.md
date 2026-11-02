# API

Two APIs on the same port:

- **`/api/v1`** — Pocket Games' own (the web app uses it).
- **`/api`** — a RomM 5.4.1-compatible API for RomM apps on handhelds and phones.

## Signing in

`/api/v1` takes a session cookie (the web app), `Authorization: Bearer <token>` with a personal token (Settings → API & shortcuts, `pgm_…`), or a Pocket Core token. Everything is per person: favourites, statuses, notes, play time, collections, saves and states belong to whoever is signed in; other people's ids answer 404. The games themselves are shared by everyone on the box; an admin or the person who added a game may change or delete it.

## /api/v1

| | |
|---|---|
| `GET /home` | Continue playing (with the newest state's picture), recently added, favourites, backlog, plays-in-the-browser, the week in numbers |
| `GET /dashboard` | Counts for a Homepage widget: `games`, `platforms`, `browser`, `playing`, `backlog`, `favorites`, `finished`, `minutes_week` |
| `GET /games` | `q`, `platform` (slug, comma list), `status`, `favorite=1`, `played=1`, `browser=1`, `collection`, `sort` (`name added played playtime rating year size platform`), `desc=1`, `limit`, `offset` → `{items, total}` |
| `GET /games/{id}` | The game, its files, your saves and states, your sittings, the system, BIOS check, other copies, whether it plays in the browser |
| `PUT /games/{id}/mine` | Your own: `favorite`, `status` (`backlog playing finished completed dropped`), `rating` (0–10, half stars), `notes`, `hidden`, `core` |
| `PUT /games/{id}` | Edit the facts (name, summary, genres, developer, publisher, release_date, players); edited facts are kept on later lookups |
| `POST /games/{id}/lookup` | Look it up again (`{"cover": true}` replaces the cover too, `{"article": "Silent Hill 2"}` names the Wikipedia article) |
| `GET /games/{id}/covers?q=` · `POST /games/{id}/cover` | Box art candidates; set one (`{"url"}`) or upload a picture (the body) |
| `GET /games/{id}/download` | The game: one file as it is (ranges), several as a zip with an `.m3u` |
| `POST /games/{id}/playtime` | `{"minutes": 45, "day": "2026-11-02", "device": "SNES"}` — time played elsewhere |
| `GET/POST /games/{id}/saves` | Your saves and states; upload with `?kind=save\|state&slot=&emulator=` (multipart `file`, optional `screenshot`) |
| `DELETE /games/{id}` · `POST /games/{id}/restore` · `DELETE /games/{id}/purge` | Trash, restore, delete for good |
| `GET /sessions` · `GET /stats?days=30` · `GET /day/{YYYY-MM-DD}` | Sittings, play-time stats, one day (for a journal) |
| `GET/POST/PUT/DELETE /collections…` | Your collections; `PUT /collections/{id}/games/{gid}` `{"in": true}` |
| `GET /platforms` · `GET /platforms/{slug}` · `GET /catalog` | Systems with counts and BIOS checks; every known system |
| `POST /uploads` → `PUT /uploads/{id}?offset=N` → `POST /uploads/{id}/finish` | Resumable uploads: `{"platform":"psx","name":"Game (USA)/Game (USA).cue","size":123}`, chunks at an offset, then file it (`?scan=0` for all but the last file of a game) |
| `POST /downloads` → `GET /downloads` | `{"url", "platform"?}` — the server fetches a link (Google Drive / Dropbox / OneDrive share links and archive.org items are turned into the file; local-network addresses only for admins), resumes after drops and files it like an upload; `PUT /downloads/{id}` `{"platform"}` when it waits for a system, `{"retry": true}` after a failure; `DELETE` cancels |
| `GET /library` · `POST /library/scan` · `POST /library/lookup` | Scan status, look now, look up unmatched games again |
| `POST /import/romm` | `{"url","username","password","saves":true,"covers":true}` — bring favourites, statuses, collections, saves and states over from a RomM server |
| `GET /export.json` · `POST /import` | Your own data as JSON, and back |
| `POST /picks` | `{"mood": "30 minutes"}` → three games from your library (Pocket AI / OpenAI, or your lists) |

The player is `GET /play/{id}` (`?state=<save id>` to start from a state, `?fresh=1` to start clean); its files come from `/emulatorjs/*`.

## Pocket Core

Pocket Games registers with Core as `games` and publishes:

- `games.played` — a sitting of a minute or more ended (`game_id`, `name`, `platform`, `platform_name`, `minutes`, `started_at`, `ended_at`, `day`, `device`, `url`)
- `games.added` — a game arrived (`game_id`, `name`, `platform`, `platform_name`, `url`)
- `games.status_changed` — `status`, `previous`

Other Pocket apps can be allowed (Core → Links): `games:read` (library, stats, sittings, a day) and `games:write` (statuses, favourites, time played elsewhere). No link can upload, download or delete.

## /api — RomM-compatible

RomM apps (Argosy, Grout, the muOS app, romm-mobile, Ludo, Playnite, RomMMobile, rommulus…) add Pocket Games as a RomM server at `BASE_URL`. `GET /api/heartbeat` answers `SYSTEM.VERSION = 5.4.1`.

Sign-in, as in RomM:

- **HTTP Basic** on every request — the email (or the part before the @) and the Pocket Games password
- **Pairing code** — Settings → Devices → *Make a pairing code*; the app calls `POST /api/client-tokens/exchange` and gets an `rmm_…` token
- **Device flow** — the app calls `POST /api/auth/device/init`, shows a code, and the person approves it at `/pair/device`; the app polls `POST /api/auth/device/token`
- **`POST /api/token`** — password and refresh grants (`expires`, one-time refresh tokens); an empty scope gets the person's scopes
- **`POST /api/login`** — a `romm_session` cookie
- Bearer tokens: `rmm_…` client tokens, `/api/token` access tokens, Pocket Games personal tokens, Pocket Core tokens

Covered: platforms (bare arrays), `GET /api/roms` (the paginated envelope, `limit` up to 10000, `platform_ids` plus the `platform_id` / `search` spellings some apps send, `search_term` incl. hashes, `order_by`/`order_dir`, `favorite`, `last_played`, `collection_id`, `virtual_collection_id` (genres), `updated_after`, `with_files`), `GET /api/roms/{id}` (detailed, with your saves, states and collections), downloads (`/content/{name}` with `file_ids` and `hidden_folder`; one file with ranges, several as a stored zip with an `.m3u`, built once on disk when the app resumes), `/files/content`, `/by-hash`, `PUT /api/roms/{id}/props` (`now_playing`, `backlogged`, `status`, `rating`, `hidden`, `update_last_played`), firmware, saves / states / screenshots (multipart `saveFile` / `stateFile` / `screenshotFile`, `/content`, `/delete`, `/summary`), collections (favourites as the `is_favorite` collection), devices, `POST /api/play-sessions` (each sitting counted once), and the covers at `/assets/romm/resources/roms/{platform}/{rom}/cover/{small,big}.png` (no sign-in, as in RomM).

Not covered: deleting games (a "delete" on a device only removes the device's copy), editing metadata, scans and tasks, users, smart collections (empty), cross-device save-sync negotiation.
