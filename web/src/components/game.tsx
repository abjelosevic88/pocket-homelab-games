// Covers, game tiles, rows and the small controls shared by the pages.
import React, { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { flags, playtimeShort, STATUS, tint, year } from '../lib/games'
import type { AIStatus, Collection, Game, Status } from '../lib/types'
import { Icon } from './icons'
import { useToast } from './ui'
import { useEffect } from 'react'

/** A cover, or the game's initials on its own colour. */
export function Cover({ game, className = '', alt = '' }: { game: Pick<Game, 'name' | 'cover' | 'platform_slug'>; className?: string; alt?: string }) {
  const [broken, setBroken] = useState(false)
  if (game.cover && !broken) return <img className={`cover ${className}`} src={game.cover} alt={alt} loading="lazy" decoding="async" onError={() => setBroken(true)} />
  const letters = game.name.replace(/[^A-Za-z0-9 ]/g, '').split(' ').filter(Boolean).slice(0, 2).map(w => w[0]).join('').toUpperCase()
  return <div className={`cover cover-none ${className}`} style={{ background: tint(game.name) }} aria-hidden><span className="cover-letters">{letters || '?'}</span><span className="cover-name">{game.name}</span></div>
}

/** One game in a grid: cover, name, system and the person's marks. */
export function GameCard({ game, state, onPlay }: { game: Game; state?: number; onPlay?: boolean }) {
  const m = game.mine
  const st = STATUS.find(s => s.value === m.status)
  return (
    <div className={`gcard ${game.missing_since ? 'is-missing' : ''}`}>
      <Link to={`/games/${game.id}`} className="gcard-link" title={game.name}>
        <div className="gcard-art">
          {state ? <img className="cover state-shot" src={`${V1}/saves/${state}/shot`} alt="" loading="lazy" /> : <Cover game={game} />}
          {m.favorite && <span className="gcard-fav" title="Favourite"><Icon name="heart" size={13} strokeWidth={2.4} /></span>}
          {game.missing_since && <span className="gcard-flag">Files missing</span>}
          {m.status && st && <span className={`gcard-status s-${m.status}`} title={st.label}><Icon name={st.icon} size={12} strokeWidth={2.2} /></span>}
        </div>
        <div className="gcard-meta">
          <span className="gcard-name">{game.name}</span>
          <span className="gcard-sub">{game.platform_name ? shortName(game) : ''}{year(game) ? ` · ${year(game)}` : ''}{m.play_seconds >= 60 ? ` · ${playtimeShort(m.play_seconds)}` : ''}</span>
        </div>
      </Link>
      {onPlay && game.browser && <a className="gcard-play" href={`/play/${game.id}`} title={`Play ${game.name}`} aria-label={`Play ${game.name}`}><Icon name="play" size={16} strokeWidth={2.2} /></a>}
    </div>
  )
}

function shortName(g: Game) {
  const n = g.platform_name
  return n.length > 22 ? g.platform_slug.toUpperCase() : n
}

export function GameGrid({ games, size = 'm', states, play = true }: { games: Game[]; size?: string; states?: Record<string, number>; play?: boolean }) {
  return <div className={`ggrid size-${size}`}>{games.map(g => <GameCard key={g.id} game={g} state={states?.[g.id]} onPlay={play} />)}</div>
}

/** A titled horizontal row (the home page). */
export function Shelf({ title, icon, to, games, states, empty }: { title: string; icon: string; to?: string; games: Game[]; states?: Record<string, number>; empty?: React.ReactNode }) {
  if (games.length === 0 && !empty) return null
  return (
    <section className="shelf">
      <div className="shelf-head">
        <h2><Icon name={icon} size={17} />{title}</h2>
        {to && games.length > 0 && <Link to={to} className="shelf-more">See all<Icon name="chevron-right" size={15} /></Link>}
      </div>
      {games.length === 0 ? <div className="shelf-empty">{empty}</div> : <div className="shelf-row">{games.map(g => <GameCard key={g.id} game={g} state={states?.[g.id]} onPlay />)}</div>}
    </section>
  )
}

/** Five stars in half steps (rating 0..10). */
export function Stars({ value, onChange, size = 18 }: { value: number; onChange?: (v: number) => void; size?: number }) {
  const [hover, setHover] = useState<number | null>(null)
  const shown = hover ?? value
  return (
    <div className={`stars-pick ${onChange ? 'editable' : ''}`} onMouseLeave={() => setHover(null)} role={onChange ? 'radiogroup' : undefined} aria-label="Rating">
      {[1, 2, 3, 4, 5].map(i => {
        const full = shown >= i * 2, half = !full && shown >= i * 2 - 1
        return (
          <span key={i} className="star-slot" style={{ width: size, height: size }}>
            <svg viewBox="0 0 24 24" width={size} height={size} className={`star ${full ? 'full' : half ? 'half' : ''}`} aria-hidden>
              <defs><linearGradient id={`h${i}`}><stop offset="50%" stopColor="currentColor" /><stop offset="50%" stopColor="transparent" /></linearGradient></defs>
              <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01z" fill={full ? 'currentColor' : half ? `url(#h${i})` : 'none'} stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" />
            </svg>
            {onChange && <>
              <button type="button" className="star-hit left" aria-label={`${i - 0.5} stars`} onMouseEnter={() => setHover(i * 2 - 1)} onClick={() => onChange(value === i * 2 - 1 ? 0 : i * 2 - 1)} />
              <button type="button" className="star-hit right" aria-label={`${i} stars`} onMouseEnter={() => setHover(i * 2)} onClick={() => onChange(value === i * 2 ? 0 : i * 2)} />
            </>}
          </span>
        )
      })}
    </div>
  )
}

/** Changes the person's own bits of a game and reports back. */
export function useMine(game: Game | null, onChange: (g: Game) => void) {
  const { toast } = useToast()
  return async (patch: Partial<{ favorite: boolean; status: Status; rating: number; notes: string; hidden: boolean; core: string }>) => {
    if (!game) return
    try { onChange(await api.put<Game>(`${V1}/games/${game.id}/mine`, patch)) } catch (e) { toast((e as Error).message, 'err') }
  }
}

export function FlagLine({ game }: { game: Game }) {
  const f = flags(game.regions)
  return f ? <span className="flags" title={game.regions}>{f}</span> : null
}

/** The person's collections, loaded once per page. */
export function useCollections() {
  const [list, setList] = useState<Collection[]>([])
  const reload = () => api.get<Collection[]>(`${V1}/collections`).then(setList).catch(() => {})
  useEffect(() => { reload() }, [])
  return { list, reload }
}

export function useAIStatus() {
  const [st, setSt] = useState<AIStatus | null>(null)
  useEffect(() => { api.get<AIStatus>(`${V1}/ai/status`).then(setSt).catch(() => {}) }, [])
  return st
}

/** Bytes as "4.3 GB". */
export function size(n: number): string {
  if (!n) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(u.length - 1, Math.floor(Math.log(n) / Math.log(1024)))
  return `${(n / 1024 ** i).toFixed(i >= 2 ? 1 : 0)} ${u[i]}`
}
