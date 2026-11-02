export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }
export interface APIToken { id: number; user_id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }

export type Status = '' | 'backlog' | 'playing' | 'finished' | 'completed' | 'dropped'

export interface Mine {
  favorite: boolean; status: Status; rating: number; notes: string; hidden: boolean
  play_seconds: number; play_count: number; last_played_at: string; core: string
}

export interface Game {
  id: number; platform_id: number; platform_slug: string; platform_name: string; fs_name: string; is_folder: boolean; main_file: string
  name: string; sort_name: string; regions: string; languages: string; tags: string; size_bytes: number; file_mtime: string
  crc32: string; md5: string; sha1: string; hashed_at: string
  summary: string; summary_source: string; summary_url: string; genres: string; developer: string; publisher: string; release_date: string; players: string
  cover_source: string; wiki_title: string; meta_status: 'pending' | 'matched' | 'partial' | 'unmatched'; meta_locked: boolean; meta_at: string
  missing_since: string; trashed_at: string; added_by: number | null; created_at: string; updated_at: string
  mine: Mine
  cover: string; shots: string[]; file_count: number; browser: boolean; can_edit: boolean
}

export interface GameFile { id: number; game_id: number; rel: string; size_bytes: number; mtime: string; crc32: string; md5: string; sha1: string }

export interface Save {
  id: number; game_id: number; kind: 'save' | 'state' | 'screenshot'; file_name: string; emulator: string; slot: string
  size_bytes: number; md5: string; has_shot: boolean; device: string; created_at: string; updated_at: string
}

export interface Session { id: number; game_id: number; game_name: string; platform: string; started_at: string; ended_at: string; seconds: number; day: string; device: string }

export interface BiosItem { file: string; md5: string; required: boolean; any: boolean; note: string; present: boolean; verified: boolean; file_id: number; have: string }
export interface BiosCheck { ok: boolean; items: BiosItem[]; files: number }

export interface CatalogPlatform {
  slug: string; name: string; short: string; maker: string; year: number; kind: string; extensions: string[]; disc: boolean
  system: string; cores: string[] | null; threads: boolean; libretro: string; wiki: string; bios: { file: string; md5: string; required: boolean; any: boolean; note: string }[] | null
  browser: boolean; needs_bios: boolean
}

export interface Platform {
  id: number; slug: string; custom_name: string; games: number; size_bytes: number; firmware: number; created_at: string; updated_at: string
  name: string; short: string; maker: string; year: number; kind: string; known: boolean; browser: boolean; disc: boolean
  extensions: string[]; cores: string[]; needs_bios: boolean; bios: BiosCheck | null; covers: number[]; firmware_files?: Firmware[]
}

export interface Firmware { id: number; platform_id: number; platform_slug: string; file_name: string; size_bytes: number; mtime: string; crc32: string; md5: string; sha1: string; created_at: string }

export interface GameDetail {
  game: Game; files: GameFile[]; collections: number[]; saves: Save[]; sessions: Session[]
  platform: CatalogPlatform; bios: BiosCheck | null; added_by: string; others: Game[]
  play: { browser: boolean; system: string; cores: string[] | null; threads: boolean; why?: string; bios_missing?: boolean }
}

export interface Collection { id: number; name: string; description: string; games: number; game_ids: number[]; covers: number[]; created_at: string; updated_at: string }

export interface Page<T> { items: T[]; total: number; limit: number; offset: number }

export interface DayTotal { day: string; seconds: number }
export interface NameTotal { id: number; name: string; platform: string; seconds: number; sessions: number }

export interface ScanStatus { running: boolean; started_at: string; ended_at: string; ms: number; platforms: number; games: number; added: number; missing: number; error: string }

export interface Home {
  continue: Game[]; states: Record<string, number>; added: Game[]; favorites: Game[]; backlog: Game[]; browser: Game[]
  week: { seconds: number; sessions: number; days: number; games: number; by_day: DayTotal[] }
  counts: Record<string, number>; total: number; scan: ScanStatus
}

export interface Dashboard {
  games: number; platforms: number; browser: number; trash: number; favorites: number; playing: number; backlog: number; finished: number
  collections: number; minutes_week: number; scanning: boolean
}

export interface Stats {
  from: string; to: string; seconds: number; sessions: number; days: number; games: number
  by_day: DayTotal[]; top_games: NameTotal[]; by_platform: NameTotal[]; recent: Session[]; streak: number
  all_time: { seconds: number; sessions: number; games: number }; counts: Record<string, number>
}

export interface Upload { id: string; kind: 'game' | 'firmware'; platform: string; rel: string; size_bytes: number; received: number; status: 'open' | 'done' | 'failed'; error: string; game_id: number | null }

export interface Device {
  id: number; uuid: string; identifier: string; name: string; client: string; client_version: string; platform: string; hostname: string; ip: string
  first_seen: string; last_seen: string; requests: number; downloads: number
}
export interface ClientToken { id: number; user_id: number; kind: string; name: string; scopes: string; device_id: string; expires_at: string; last_used_at: string; created_at: string }

export interface LibraryStatus {
  scan: ScanStatus; interval_minutes: number; metadata: boolean; pending: number; unmatched: number; unhashed: number
  unknown_platforms: string[]; duplicates: Game[][]; hash_max_bytes: number; trash_days: number; romm_compat: boolean
  paths?: { library: string; roms: string; bios: string; trash: string }
}

export interface Pick { game: Game; why: string }

export interface Settings {
  core_configured: boolean; core_registered?: boolean
  title: string; date_format: string; time_format: string
  grid_size: 's' | 'm' | 'l'; show_missing: boolean; default_sort: string; regions: string
  auto_save: boolean; resume_state: boolean; state_on_exit: boolean; touch_controls: 'auto' | 'on' | 'off'; player_volume: number; player_shader: string
  notify_url: string; notify_format: string; notify_configured: boolean; notify_new_games: boolean; notify_uploads: boolean
  ai_picks: boolean; ai_provider: 'auto' | 'pocket' | 'openai'; ai_pocket_model: string
  openai_api_key: string; openai_model: string; openai_base_url: string
  setup_complete: boolean; base_url: string; romm_compat: boolean; metadata: boolean; trash_days: number
}

export interface AIStatus { ready: boolean; provider: string; detail: string }

export interface Download {
  id: string; url: string; source_url: string; file_name: string; platform: string; choices: string[]
  size_bytes: number; received: number; status: 'queued' | 'downloading' | 'waiting' | 'done' | 'failed' | 'canceled'; error: string; game_id: number | null
  created_at: string; updated_at: string
}
