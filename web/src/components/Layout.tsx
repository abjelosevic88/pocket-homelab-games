// The app shell: sidebar, top bar, search palette, shortcuts — the Pocket Calendar layout.
import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Collection, Dashboard, Game, Page, Platform } from '../lib/types'
import { Icon } from './icons'
import { Sidebar } from './Sidebar'
import { CommandPalette, type PaletteCommand } from './CommandPalette'
import { ShortcutsSheet } from './ShortcutsSheet'
import { Avatar, type Anchor, type MenuItem, Menu, MenuButton, modalOpen, rectOf, useIsMobile, useMediaQuery } from './ui'
import { AskButton } from './AskButton'

const SIDEBAR_KEY = 'pgm-sidebar'

/** Something was added or changed from the shell (or a page): the sidebar counts and the pages reload. */
export const announceChange = () => window.dispatchEvent(new CustomEvent('pgm:changed'))
export function useOnChange(cb: () => void) {
  const ref = useRef(cb)
  ref.current = cb
  useEffect(() => {
    const h = () => ref.current()
    window.addEventListener('pgm:changed', h)
    return () => window.removeEventListener('pgm:changed', h)
  }, [])
}

/** The page's own name for the top bar breadcrumb (a game's name). */
const CrumbCtx = createContext<(t: string) => void>(() => {})
export function useCrumb(title: string | undefined) {
  const set = useContext(CrumbCtx)
  useEffect(() => { set(title || ''); return () => set('') }, [title, set])
}

/** Counts, systems and collections, for every page that needs them (filters, pickers, the sidebar). */
const ShellCtx = createContext<{ dash: Dashboard | null; platforms: Platform[]; collections: Collection[]; reload: () => void }>({ dash: null, platforms: [], collections: [], reload: () => {} })
export const useShell = () => useContext(ShellCtx)

/** The add menu: the sidebar's "Add" button and the phone's + button. */
export function createItems(navigate: (to: string) => void): MenuItem[] {
  return [
    { label: 'Upload games', icon: 'upload', hint: 'U', onSelect: () => navigate('/upload') },
    { label: 'New collection', icon: 'layers', onSelect: () => navigate('/collections?new=1') },
    { label: 'Connect a handheld', icon: 'smartphone', onSelect: () => navigate('/settings/devices') },
  ]
}

export default function Layout() {
  const navigate = useNavigate()
  const loc = useLocation()
  const mobile = useIsMobile()
  const narrow = useMediaQuery('(max-width: 1099px)')
  const drawer = mobile || narrow
  const [sidebarOpen, setSidebarOpen] = useState(() => !drawer && localStorage.getItem(SIDEBAR_KEY) !== '0')
  useEffect(() => { setSidebarOpen(!drawer && localStorage.getItem(SIDEBAR_KEY) !== '0') }, [drawer])
  const drawerOpenedAt = useRef(0)
  const toggleSidebar = () => setSidebarOpen(o => { drawerOpenedAt.current = performance.now(); const n = !o; if (!drawer) localStorage.setItem(SIDEBAR_KEY, n ? '1' : '0'); return n })
  const closeDrawer = () => { if (drawer) setSidebarOpen(false) }
  useEffect(() => { document.querySelector('.content')?.scrollTo({ top: 0 }) }, [loc.pathname])

  const [crumb, setCrumbRaw] = useState('')
  const setCrumb = useCallback((t: string) => setCrumbRaw(t), [])

  const [dash, setDash] = useState<Dashboard | null>(null)
  const [platforms, setPlatforms] = useState<Platform[]>([])
  const [collections, setCollections] = useState<Collection[]>([])
  const load = useCallback(() => {
    api.get<Dashboard>(`${V1}/dashboard`).then(setDash).catch(() => {})
    api.get<Platform[]>(`${V1}/platforms`).then(p => setPlatforms(p.filter(x => x.games > 0))).catch(() => {})
    api.get<Collection[]>(`${V1}/collections`).then(setCollections).catch(() => {})
  }, [])
  useEffect(() => { load() }, [loc.pathname, load])
  useOnChange(load)
  // the counts follow scans and uploads that finish in the background
  useEffect(() => { const t = window.setInterval(load, 60_000); return () => window.clearInterval(t) }, [load])

  const [palette, setPalette] = useState(false)
  const [shortcuts, setShortcuts] = useState(false)
  const [fabMenu, setFabMenu] = useState<Anchor | null>(null)

  useEffect(() => {
    const open = () => setShortcuts(true)
    window.addEventListener('pgm:shortcuts', open)
    return () => window.removeEventListener('pgm:shortcuts', open)
  }, [])
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPalette(p => !p); return }
      if (e.metaKey || e.ctrlKey || e.altKey) return
      if (t.closest('input, textarea, select, [contenteditable="true"]') || modalOpen() || document.querySelector('.menu, .popover, .sel-pop')) return
      const go = (to: string) => { e.preventDefault(); navigate(to) }
      switch (e.key) {
        case '/': e.preventDefault(); setPalette(true); break
        case '?': e.preventDefault(); setShortcuts(true); break
        case 'u': go('/upload'); break
        case 'h': go('/'); break
        case 'l': go('/library'); break
        case 'p': go('/platforms'); break
        case 'c': go('/collections'); break
        case 's': go('/stats'); break
        case ',': go('/settings'); break
        case '[': e.preventDefault(); toggleSidebar(); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const commands: PaletteCommand[] = [
    { id: 'home', label: 'Home', icon: 'home', hint: 'H', run: () => navigate('/') },
    { id: 'library', label: 'All games', icon: 'grid', hint: 'L', keywords: 'library roms', run: () => navigate('/library') },
    { id: 'browser', label: 'Games that play in the browser', icon: 'play', keywords: 'emulator web', run: () => navigate('/library?browser=1') },
    { id: 'playing', label: 'Playing', icon: 'gamepad', run: () => navigate('/library?status=playing') },
    { id: 'fav', label: 'Favourites', icon: 'heart', run: () => navigate('/library?favorite=1') },
    { id: 'backlog', label: 'Backlog', icon: 'bookmark', run: () => navigate('/library?status=backlog') },
    { id: 'platforms', label: 'Systems', icon: 'cpu', hint: 'P', keywords: 'platforms consoles bios', run: () => navigate('/platforms') },
    { id: 'collections', label: 'Collections', icon: 'layers', hint: 'C', run: () => navigate('/collections') },
    { id: 'stats', label: 'Play time', icon: 'chart', hint: 'S', keywords: 'stats hours', run: () => navigate('/stats') },
    { id: 'upload', label: 'Upload games', icon: 'upload', hint: 'U', keywords: 'add rom bios', run: () => navigate('/upload') },
    { id: 'random', label: 'Surprise me (a random game)', icon: 'shuffle', keywords: 'random pick', run: () => { api.get<Page<Game>>(`${V1}/games?limit=500`).then(p => { if (p.items.length) navigate(`/games/${p.items[Math.floor(Math.random() * p.items.length)].id}`) }).catch(() => {}) } },
    { id: 'scan', label: 'Look for new games now', icon: 'refresh', keywords: 'scan rescan library', run: () => { api.post(`${V1}/library/scan`).then(() => setTimeout(announceChange, 2000)).catch(() => {}) } },
    { id: 'devices', label: 'Settings: handhelds & RomM apps', icon: 'smartphone', keywords: 'odin argosy romm pair device', run: () => navigate('/settings/devices') },
    { id: 'trash', label: 'Trash', icon: 'trash', keywords: 'deleted restore', run: () => navigate('/trash') },
    { id: 'settings', label: 'Settings', icon: 'settings', hint: ',', run: () => navigate('/settings') },
    { id: 'shortcuts', label: 'Keyboard shortcuts', icon: 'keyboard', hint: '?', run: () => setShortcuts(true) },
  ]

  return (
    <CrumbCtx.Provider value={setCrumb}>
      <ShellCtx.Provider value={{ dash, platforms, collections, reload: load }}>
        <div className={`app ${sidebarOpen ? 'with-sidebar' : 'no-sidebar'} ${mobile ? 'is-mobile' : ''} ${drawer ? 'drawer-mode' : ''}`}>
          {drawer && sidebarOpen && <div className="drawer-backdrop" onClick={e => { if (e.timeStamp - drawerOpenedAt.current > 400) setSidebarOpen(false) }} />}
          <div className={`sidebar-wrap ${sidebarOpen ? 'open' : ''}`}>
            <Sidebar onNavigate={closeDrawer} dash={dash} platforms={platforms} collections={collections} />
          </div>
          <div className="main">
            <TopBar onMenu={toggleSidebar} onPalette={() => setPalette(true)} mobile={mobile} compact={narrow} sidebarOpen={sidebarOpen} crumb={crumb} />
            <main className="content"><div className="content-inner"><Outlet /></div></main>
          </div>
          {mobile && loc.pathname !== '/upload' && <button type="button" className="fab" aria-label="New" title="New" aria-haspopup="menu" aria-expanded={!!fabMenu} onClick={e => setFabMenu(fabMenu ? null : rectOf(e.currentTarget))}><Icon name="plus" size={26} strokeWidth={2.2} /></button>}
          {fabMenu && <Menu anchor={fabMenu} align="right" items={createItems(navigate)} onClose={() => setFabMenu(null)} width={250} />}
          {palette && <CommandPalette onClose={() => setPalette(false)} commands={commands} />}
          {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
        </div>
      </ShellCtx.Provider>
    </CrumbCtx.Provider>
  )
}

const SECTION: Record<string, [string, string]> = {
  '': ['Home', '/'], library: ['Games', '/library'], games: ['Games', '/library'], platforms: ['Systems', '/platforms'], collections: ['Collections', '/collections'],
  stats: ['Play time', '/stats'], upload: ['Upload', '/upload'], trash: ['Trash', '/trash'], pair: ['Connect a device', '/settings/devices'],
}

function TopBar({ onMenu, onPalette, mobile, compact, sidebarOpen, crumb }: { onMenu: () => void; onPalette: () => void; mobile: boolean; compact: boolean; sidebarOpen: boolean; crumb: string }) {
  const { user, themeMode, setThemeMode, status, setUser } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [section, sectionTo] = SECTION[loc.pathname.split('/')[1] || ''] || ['', '/']
  const signOut = async () => { await api.post(`${V1}/auth/logout`).catch(() => {}); setUser(null); navigate('/login') }
  const avatarMenu = (): MenuItem[] => [
    { heading: user?.name || user?.email || '' },
    { label: 'Profile', icon: 'user', onSelect: () => navigate('/settings/profile') },
    { label: 'Settings', icon: 'settings', onSelect: () => navigate('/settings'), hint: ',' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', onSelect: () => window.dispatchEvent(new CustomEvent('pgm:shortcuts')), hint: '?' },
    'sep',
    { heading: 'Appearance' },
    { label: 'System', checked: themeMode === 'system', onSelect: () => setThemeMode('system') },
    { label: 'Light', checked: themeMode === 'light', onSelect: () => setThemeMode('light') },
    { label: 'Dark', checked: themeMode === 'dark', onSelect: () => setThemeMode('dark') },
    ...(status?.auth_mode === 'local' ? ['sep' as const, { label: 'Sign out', icon: 'log-out', onSelect: signOut }] : []),
  ]
  return (
    <header className="topbar">
      <button type="button" className="btn ghost icon-only" onClick={onMenu} aria-label={sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar'} title="Menu ([)"><Icon name={mobile || compact ? 'menu' : 'sidebar'} size={18} /></button>
      <h1 className="range-title">
        {crumb ? <><button type="button" className="crumb" onClick={() => navigate(sectionTo)}>{section}</button><Icon name="chevron-right" size={16} className="crumb-sep" /><span className="ellipsis">{crumb}</span></> : section}
      </h1>
      <span className="spacer" />
      <button type="button" className="btn ghost icon-only" onClick={onPalette} aria-label="Search" title="Search (⌘K or /)"><Icon name="search" size={18} /></button>
      <AskButton />
      <MenuButton label="Account" className="avatar-btn" align="right" width={220} items={avatarMenu}>
        <Avatar name={user?.name} email={user?.email} size={30} />
      </MenuButton>
    </header>
  )
}
