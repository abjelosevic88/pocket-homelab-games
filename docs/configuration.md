# Configuration

Everything is an environment variable; every one has a default, so `docker run` with two volumes works.

| Variable | Default | What it does |
|---|---|---|
| `HOST` / `PORT` | `0.0.0.0` / `8080` | Where the server listens |
| `BASE_URL` | – | The public address (sign-in callbacks, links in pushes and events, cover URLs for RomM apps) |
| `DATA_DIR` | `./data` | Database, covers, saves and states, the player's files |
| `DB_PATH` | `$DATA_DIR/pocket-games.db` | The SQLite file |
| `LIBRARY_DIR` | `$DATA_DIR/library` | The games: `roms/<system>/…` + `bios/<system>/…` (RomM's layout) or `<system>/…` directly. Must be writable (uploads, trash). |
| `SCAN_INTERVAL` | `15m` | How often the library folder is read (`0` = only after uploads and "Look now") |
| `HASH_MAX_BYTES` | `1073741824` | Files bigger than this are not checksummed (CRC32/MD5/SHA-1) |
| `METADATA` | `true` | Look up covers (libretro thumbnails) and descriptions (Wikipedia/Wikidata). No API keys needed. |
| `TRASH_DAYS` | `30` | Deleted games stay restorable this long (`0` = keep until emptied by hand) |
| `EMULATORJS_URL` | `https://cdn.emulatorjs.org/4.2.3/data` | Where the in-browser player's files come from; they are kept under `$DATA_DIR/emulatorjs` after the first use |
| `ROMM_COMPAT` | `true` | The RomM-compatible API under `/api` for RomM apps on handhelds |
| `AUTH_MODE` | `local` | `local` (accounts and passwords), `proxy` (trust `AUTH_PROXY_HEADER`), `none` (single user, no login) |
| `SESSION_TTL` | `720h` | How long a sign-in lasts |
| `SECURE_COOKIES` | `false` | Set `true` behind HTTPS |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` / `ADMIN_NAME` | – | The first admin, made while the database has no users |
| `OIDC_ISSUER` / `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` / `OIDC_NAME` | – | Sign in with an OpenID Connect provider (not needed with Pocket Core) |
| `NOTIFY_URL` / `NOTIFY_FORMAT` | – / `apprise` | The owner's pushes (Apprise, ntfy or a JSON webhook); everyone can set their own |
| `OPENAI_API_KEY` / `OPENAI_MODEL` / `OPENAI_BASE_URL` | – | The owner's AI for "What should I play?" (Pocket AI through Core needs none of these) |
| `CORE_URL` / `CORE_REGISTRATION_KEY` | – | [Pocket Core](https://github.com/abjelosevic88/pocket-homelab-core): register on start, sign in with Core, events, links, Pocket AI |
| `CORE_APP_URL` | `BASE_URL` | How Core reaches this app |
| `DEMO_DATA` | `false` | Fill an empty library with placeholder games and the owner's account with months of play |
| `USER_AGENT` | `PocketGames/1.0 …` | Sent with lookups |
| `LOG_LEVEL` / `LOG_FORMAT` | `info` / `text` | `debug`…`error`; `text` or `json` |
| `TRUST_PROXY` | `true` | Take the client address from `X-Forwarded-For` |
| `METRICS_ENABLED` | `true` | Prometheus metrics at `/metrics` |
| `MAX_BODY_BYTES` | `67108864` | The biggest JSON import |

## The library folder

```
library/
├── roms/
│   ├── gba/Metroid Fusion (USA).gba            ← a game is a file …
│   ├── psx/Silent Hill (Europe)/                ← … or a folder (discs, cue/bin, chd + sbi, m3u)
│   └── snes/…
└── bios/
    └── psx/scph5501.bin
```

Folder names are the systems' short names (RomM / IGDB style): `nes snes n64 gb gbc gba nds psx ps2 psp genesis sms gamegear segacd sega32 saturn dc ngc wii arcade tg16 atari2600 …`. Unknown folders still work, without covers and in-browser play.

Loose files that belong together are one game: `Game.cue` + `Game (Track 1).bin`…, or `Game.chd` + `Game.sbi`.
