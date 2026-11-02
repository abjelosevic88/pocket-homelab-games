// Left column: the add button, sections with counts, systems and collections — the Pocket Calendar sidebar.
import { type ReactNode, useState } from 'react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import type { Collection, Dashboard, Platform } from '../lib/types'
import { Icon } from './icons'
import { createItems } from './Layout'
import { type Anchor, Menu, rectOf } from './ui'

export function Sidebar({ onNavigate, dash, platforms, collections }: { onNavigate: () => void; dash: Dashboard | null; platforms: Platform[]; collections: Collection[] }) {
  const { status, settings } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const title = settings?.title || status?.title || 'Pocket Games'
  const q = new URLSearchParams(loc.search)
  const lib = (key?: string, val?: string) => loc.pathname === '/library' && (key ? q.get(key) === val : !q.get('status') && !q.get('favorite') && !q.get('browser'))
  const nav: { to: string; label: string; icon: string; active: boolean; count?: number }[] = [
    { to: '/', label: 'Home', icon: 'home', active: loc.pathname === '/' },
    { to: '/library', label: 'All games', icon: 'grid', active: lib(), count: dash?.games },
    { to: '/library?browser=1', label: 'Play here', icon: 'play', active: lib('browser', '1'), count: dash?.browser },
    { to: '/library?status=playing', label: 'Playing', icon: 'gamepad', active: lib('status', 'playing'), count: dash?.playing },
    { to: '/library?favorite=1', label: 'Favourites', icon: 'heart', active: lib('favorite', '1'), count: dash?.favorites },
    { to: '/library?status=backlog', label: 'Backlog', icon: 'bookmark', active: lib('status', 'backlog'), count: dash?.backlog },
    { to: '/stats', label: 'Play time', icon: 'chart', active: loc.pathname === '/stats' },
  ]
  return (
    <aside className="sidebar" aria-label="Pocket Games">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Games' ? title : <>Pocket <b>Games</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^v?\d+\.\d+/.test(status.version) ? 'v' + status.version.replace(/^v/, '').replace(/[-+].*$/, '') : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(menu ? null : rectOf(e.currentTarget))}>
        <Icon name="plus" size={17} strokeWidth={2.2} />Add<Icon name="chevron-down" size={15} className="create-caret" />
      </button>
      {menu && <Menu anchor={menu} items={createItems(to => { onNavigate(); navigate(to) })} onClose={() => setMenu(null)} width={248} />}

      <nav className="sb-nav" aria-label="Sections">
        {nav.map(n => (
          <NavLink key={n.to} to={n.to} onClick={onNavigate} className={() => `sb-nav-item ${n.active ? 'active' : ''}`}>
            <Icon name={n.icon} size={16} />{n.label}
            {!!n.count && <span className="sb-count">{n.count}</span>}
          </NavLink>
        ))}
      </nav>

      <Group title="Systems" to="/platforms" active={loc.pathname === '/platforms'}>
        {platforms.length === 0
          ? <p className="sb-empty">Systems with games show up here.</p>
          : <ul className="cal-list">{platforms.map(p => (
            <li key={p.id} className="cal-row sb-row">
              <NavLink to={`/platforms/${p.slug}`} onClick={onNavigate} className={() => `cal-check ${loc.pathname === `/platforms/${p.slug}` ? 'is-active' : ''}`} title={p.name}>
                <span className="sb-ico"><Icon name={p.browser ? 'gamepad' : 'hard-drive'} size={13} /></span>
                <span className="cal-name ellipsis">{p.custom_name || p.name}</span>
                <span className="sb-count">{p.games}</span>
              </NavLink>
            </li>
          ))}</ul>}
      </Group>

      <Group title="Collections" to="/collections" active={loc.pathname === '/collections'}>
        {collections.length === 0
          ? <p className="sb-empty">Your own lists of games: “Couch co-op”, “Horror nights”…</p>
          : <ul className="cal-list">{collections.map(c => (
            <li key={c.id} className="cal-row sb-row">
              <NavLink to={`/collections/${c.id}`} onClick={onNavigate} className={() => `cal-check ${loc.pathname === `/collections/${c.id}` ? 'is-active' : ''}`} title={c.name}>
                <span className="sb-ico"><Icon name="layers" size={13} /></span>
                <span className="cal-name ellipsis">{c.name}</span>
                <span className="sb-count">{c.games}</span>
              </NavLink>
            </li>
          ))}</ul>}
      </Group>

      <div className="sb-foot">
        {!!dash?.trash && <Link to="/trash" className="sb-foot-link" onClick={onNavigate}><Icon name="trash" size={15} />Trash<span className="sb-count">{dash.trash}</span></Link>}
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('pgm:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}

function Group({ title, to, active, children }: { title: string; to: string; active: boolean; children: ReactNode }) {
  const key = `pgm-sb-${title}`
  const [open, setOpen] = useState(() => localStorage.getItem(key) !== '0')
  const toggle = () => setOpen(o => { localStorage.setItem(key, o ? '0' : '1'); return !o })
  return (
    <section className="sb-sec">
      <h4 className="sb-h">
        <button type="button" className="sb-h-btn" aria-expanded={open} onClick={toggle}><Icon name={open ? 'chevron-down' : 'chevron-right'} size={13} />{title}</button>
        <Link to={to} className={`sb-h-link ${active ? 'active' : ''}`} title={`All ${title.toLowerCase()}`}><Icon name="chevron-right" size={13} /></Link>
      </h4>
      {open && children}
    </section>
  )
}
