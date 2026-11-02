import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { SORTS, STATUS } from '../lib/games'
import type { Game, Page } from '../lib/types'
import { GameGrid } from '../components/game'
import { Icon } from '../components/icons'
import { useOnChange, useShell } from '../components/Layout'
import { Select } from '../components/Select'
import { Empty, Loading, Segmented, useDebounce } from '../components/ui'

const PAGE = 120

export default function Library({ fixed }: { fixed?: Record<string, string> }) {
  const { settings, setSettings } = useApp()
  const { platforms } = useShell()
  const [params, setParams] = useSearchParams()
  const get = (k: string) => fixed?.[k] ?? params.get(k) ?? ''
  const [q, setQ] = useState(get('q'))
  const dq = useDebounce(q, 250)
  const sort = get('sort') || settings?.default_sort || 'name'
  const size = settings?.grid_size || 'm'
  const [items, setItems] = useState<Game[] | null>(null)
  const [total, setTotal] = useState(0)
  const [more, setMore] = useState(false)

  const set = (k: string, v: string) => {
    const p = new URLSearchParams(params)
    if (v) p.set(k, v)
    else p.delete(k)
    setParams(p, { replace: true })
  }
  useEffect(() => { if (dq !== get('q')) set('q', dq) }, [dq]) // eslint-disable-line react-hooks/exhaustive-deps

  const query = (offset: number) => {
    const p = new URLSearchParams()
    for (const k of ['q', 'platform', 'status', 'favorite', 'browser', 'played', 'collection', 'desc', 'letter']) { const v = get(k); if (v) p.set(k, v) }
    p.set('sort', sort)
    if (settings?.show_missing) p.set('missing', '1')
    p.set('limit', String(PAGE))
    p.set('offset', String(offset))
    return `${V1}/games?${p}`
  }
  const load = () => api.get<Page<Game>>(query(0)).then(r => { setItems(r.items); setTotal(r.total) }).catch(() => setItems([]))
  const key = [...['q', 'platform', 'status', 'favorite', 'browser', 'played', 'collection', 'desc', 'letter'].map(get), sort, settings?.show_missing].join('|')
  useEffect(() => { load() }, [key]) // eslint-disable-line react-hooks/exhaustive-deps
  useOnChange(load)
  const loadMore = async () => {
    if (!items) return
    setMore(true)
    try { const r = await api.get<Page<Game>>(query(items.length)); setItems([...items, ...r.items]); setTotal(r.total) } finally { setMore(false) }
  }
  const setSize = (v: string) => { if (settings) { setSettings({ ...settings, grid_size: v as 's' | 'm' | 'l' }); api.put(`${V1}/settings`, { grid_size: v }).catch(() => {}) } }

  const filtered = ['q', 'platform', 'status', 'favorite', 'browser', 'played'].some(k => get(k) && !fixed?.[k])
  return (
    <div className="library">
      <div className="toolbar">
        <div className="search">
          <Icon name="search" size={15} />
          <input value={q} onChange={e => setQ(e.target.value)} placeholder="Search names, genres, developers…" aria-label="Search games" />
        </div>
        {!fixed?.platform && <Select ariaLabel="System" value={get('platform')} onChange={v => set('platform', v)} placeholder="All systems" clearLabel="All systems"
          options={platforms.map(p => ({ value: p.slug, label: p.custom_name || p.name, hint: String(p.games) }))} />}
        {!fixed?.status && <Select ariaLabel="Status" value={get('status')} onChange={v => set('status', v)} placeholder="Any status" clearLabel="Any status"
          options={STATUS.filter(s => s.value).map(s => ({ value: s.value, label: s.label }))} />}
        <Select ariaLabel="Sort" value={sort} onChange={v => set('sort', v)} options={SORTS.map(s => ({ value: s.value, label: s.label }))} />
      </div>
      <div className="filters">
        {!fixed?.favorite && <button type="button" className={`chip ${get('favorite') ? 'active' : ''}`} onClick={() => set('favorite', get('favorite') ? '' : '1')}><Icon name="heart" size={13} />Favourites</button>}
        {!fixed?.browser && <button type="button" className={`chip ${get('browser') ? 'active' : ''}`} onClick={() => set('browser', get('browser') ? '' : '1')}><Icon name="play" size={13} />Plays in the browser</button>}
        <button type="button" className={`chip ${get('played') ? 'active' : ''}`} onClick={() => set('played', get('played') ? '' : '1')}><Icon name="clock" size={13} />Played</button>
        <button type="button" className="chip" onClick={() => set('desc', get('desc') ? '' : '1')} title="Reverse the order"><Icon name={get('desc') ? 'chevron-up' : 'chevron-down'} size={13} />{get('desc') ? 'Reversed' : 'Order'}</button>
        <span className="spacer" />
        <span className="muted small tnum">{items ? `${total} game${total === 1 ? '' : 's'}` : ''}</span>
        <Segmented size="sm" label="Cover size" value={size} onChange={setSize} options={[{ value: 's', label: 'S', title: 'Small covers' }, { value: 'm', label: 'M', title: 'Medium covers' }, { value: 'l', label: 'L', title: 'Large covers' }]} />
      </div>
      {!items ? <Loading /> : items.length === 0 ? (
        <Empty icon="gamepad" title={filtered ? 'No games match' : 'No games here yet'}>
          {filtered ? <button type="button" className="btn" onClick={() => { setQ(''); setParams(new URLSearchParams(), { replace: true }) }}>Clear the filters</button>
            : <Link to="/upload" className="btn primary"><Icon name="upload" size={15} />Upload games</Link>}
        </Empty>
      ) : <>
        <GameGrid games={items} size={size} />
        {items.length < total && <div className="load-more"><button type="button" className="btn" onClick={loadMore} disabled={more}>{more ? 'Loading…' : `Show more (${total - items.length} left)`}</button></div>}
      </>}
    </div>
  )
}
