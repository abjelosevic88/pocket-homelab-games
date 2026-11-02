// Words and small helpers for games.
import type { Game, Status } from './types'

export const STATUS: { value: Status; label: string; icon: string }[] = [
  { value: '', label: 'No status', icon: 'circle' },
  { value: 'backlog', label: 'Backlog', icon: 'bookmark' },
  { value: 'playing', label: 'Playing', icon: 'gamepad' },
  { value: 'finished', label: 'Finished', icon: 'flag' },
  { value: 'completed', label: 'Completed 100%', icon: 'trophy' },
  { value: 'dropped', label: 'Dropped', icon: 'x' },
]
export const statusLabel = (s: Status) => STATUS.find(x => x.value === s)?.label || ''

export const SORTS: { value: string; label: string }[] = [
  { value: 'name', label: 'Name' },
  { value: 'added', label: 'Recently added' },
  { value: 'played', label: 'Recently played' },
  { value: 'playtime', label: 'Most played' },
  { value: 'rating', label: 'Your rating' },
  { value: 'year', label: 'Release year' },
  { value: 'size', label: 'Size' },
  { value: 'platform', label: 'System' },
]

/** "2 h 15 min", "45 min", "under a minute". */
export function playtime(sec: number): string {
  if (!sec || sec < 60) return sec > 0 ? 'under a minute' : '—'
  const m = Math.round(sec / 60)
  if (m < 60) return `${m} min`
  const h = Math.floor(m / 60)
  const r = m % 60
  return r ? `${h} h ${r} min` : `${h} h`
}

/** "2h 15m" for tight places. */
export function playtimeShort(sec: number): string {
  if (!sec || sec < 60) return '0m'
  const m = Math.round(sec / 60)
  if (m < 60) return `${m}m`
  return `${Math.floor(m / 60)}h${m % 60 ? ` ${m % 60}m` : ''}`
}

export const year = (g: Pick<Game, 'release_date'>) => (g.release_date || '').slice(0, 4)

/** A stable colour for a game without a cover. */
export function tint(name: string): string {
  let h = 0
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0
  return `hsl(${h % 360} 28% 36%)`
}

/** Flags for the common regions. */
const FLAGS: Record<string, string> = { USA: '🇺🇸', Europe: '🇪🇺', Japan: '🇯🇵', World: '🌍', Australia: '🇦🇺', Korea: '🇰🇷', China: '🇨🇳', Brazil: '🇧🇷', France: '🇫🇷', Germany: '🇩🇪', Spain: '🇪🇸', Italy: '🇮🇹', UK: '🇬🇧', Canada: '🇨🇦', Asia: '🌏' }
export const flags = (regions: string) => regions.split(',').map(r => r.trim()).filter(Boolean).map(r => FLAGS[r] || r).join(' ')

/** Upload helpers: what a file belongs with. */
export const SHEET_EXT = ['cue', 'm3u', 'gdi', 'ccd', 'toc', 'mds']
export const COMPANION_EXT = ['bin', 'img', 'sub', 'sbi', 'raw', 'mdf', 'wav', 'ips', 'bps', 'ups', 'xdelta']
export const ext = (name: string) => { const i = name.lastIndexOf('.'); return i > 0 ? name.slice(i + 1).toLowerCase() : '' }
export const stem = (name: string) => { const i = name.lastIndexOf('.'); return (i > 0 ? name.slice(0, i) : name).replace(/\s*\(track\s*\d+\)$/i, '') }
