import { AskButton } from '../components/AskButton'
import React, { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, postFile, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { APIToken, AIStatus, ClientToken, Device, LibraryStatus, Settings as SettingsT, User } from '../lib/types'
import { DATE_FORMATS, TIME_FORMATS, fmtBytes, fmtDateTime, setDateFormats, type DateFormat, type TimeFormat } from '../lib/format'
import { SORTS } from '../lib/games'
import { Avatar, Confirm, CopyField, CoreHint, Field, Loading, Modal, Segmented, SettingRow, Spinner, Switch, Tabs, useAsync, useIsMobile, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { ShortcutsSheet } from '../components/ShortcutsSheet'
import { PocketModelSelect } from '../components/PocketModelSelect'
import { useAIStatus } from '../components/game'
import { announceChange } from '../components/Layout'

type TabId = 'general' | 'player' | 'devices' | 'library' | 'notifications' | 'ai' | 'profile' | 'people' | 'tokens' | 'data' | 'about'

const TABS: { id: TabId; label: string; icon: string; sub: string }[] = [
  { id: 'general', label: 'General', icon: 'settings', sub: 'How the library looks, the covers you prefer, date formats and the app title.' },
  { id: 'player', label: 'Player', icon: 'play', sub: 'Playing in the browser: saves, picking up where you left off, touch controls and sound.' },
  { id: 'devices', label: 'Devices', icon: 'smartphone', sub: 'Handhelds and phones with a RomM app (Argosy, Grout, muOS, romm-mobile…): connect them to download games and sync saves.' },
  { id: 'library', label: 'Library', icon: 'folder', sub: 'Where the games live, looking for new ones, covers and descriptions, duplicates — and bringing your things over from RomM.' },
  { id: 'notifications', label: 'Notifications', icon: 'bell', sub: 'A push when new games arrive or a big upload finishes.' },
  { id: 'ai', label: 'AI', icon: 'sparkles', sub: '“What should I play?” picks from your own games, with Pocket AI or your OpenAI key.' },
  { id: 'profile', label: 'Profile', icon: 'user', sub: 'Your name, email and password.' },
  { id: 'people', label: 'People', icon: 'users', sub: 'Everyone who has an account on this box.' },
  { id: 'tokens', label: 'API & shortcuts', icon: 'key', sub: 'Personal tokens for Homepage widgets, Siri Shortcuts and Raycast.' },
  { id: 'data', label: 'Backup & data', icon: 'database', sub: 'Export what you think of your games and your play time, import it again, download the database.' },
  { id: 'about', label: 'About', icon: 'info', sub: 'Version, numbers and the server.' },
]
// the old ?tab= names
const ALIAS: Record<string, TabId> = { users: 'people', api: 'tokens', integrations: 'ai', apps: 'ai', romm: 'devices' }

export default function Settings() {
  const { tab: raw } = useParams()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const legacy = new URLSearchParams(window.location.search).get('tab') || ''
  const want = raw || legacy
  const tab: TabId = TABS.find(t => t.id === (ALIAS[want] || want))?.id || 'general'
  const current = TABS.find(t => t.id === tab)!
  const go = (id: TabId) => navigate(`/settings/${id}`, { replace: true })

  // /settings?tab=users (old links) → /settings/people
  useEffect(() => { if (!raw && legacy) navigate(`/settings/${tab}`, { replace: true }) }, [raw, legacy, tab, navigate])
  useEffect(() => { document.querySelector('.set-content')?.scrollTo({ top: 0 }) }, [tab])
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !document.querySelector('.modal-backdrop, .menu, .sel-pop')) navigate('/') }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [navigate])

  return (
    <div className="set-page">
      <header className="set-header">
        <button type="button" className="btn ghost icon-only" aria-label="Back to your games" title="Back to your games (esc)" onClick={() => navigate('/')}><Icon name="chevron-left" size={18} /></button>
        <img src="/favicon.svg" alt="" width={24} height={24} className="set-logo" />
        <h1>Settings</h1>
        <span style={{ flex: 1 }} />
        <AskButton />
      </header>
      {mobile && <div className="set-tabs-mobile"><Tabs tabs={TABS.map(t => ({ id: t.id, label: t.label, icon: t.icon }))} value={tab} onChange={go} /></div>}
      <div className="set-body">
        {!mobile && (
          <nav className="set-nav" aria-label="Settings sections">
            {TABS.map(t => (
              <button key={t.id} type="button" className={`set-nav-item ${t.id === tab ? 'active' : ''}`} aria-current={t.id === tab ? 'page' : undefined} onClick={() => go(t.id)}>
                <Icon name={t.icon} size={16} />{t.label}
              </button>
            ))}
          </nav>
        )}
        <main className="set-content">
          <div className="set-inner" key={tab}>
            <div className="set-title">
              <h2>{current.label}</h2>
              <p>{current.sub}</p>
            </div>
            {tab === 'general' && <General />}
            {tab === 'player' && <Player />}
            {tab === 'devices' && <Devices />}
            {tab === 'library' && <LibraryTab />}
            {tab === 'notifications' && <Notifications />}
            {tab === 'ai' && <AISettings />}
            {tab === 'profile' && <Profile />}
            {tab === 'people' && <Users />}
            {tab === 'tokens' && <Tokens />}
            {tab === 'data' && <Data />}
            {tab === 'about' && <About />}
          </div>
        </main>
      </div>
    </div>
  )
}

/** A settings card: title, explanation, actions on the right — Pocket Calendar's section. */
function Section({ title, sub, icon, actions, children }: { title: React.ReactNode; sub?: React.ReactNode; icon?: string; actions?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <section className="set-section">
      <div className="set-section-head">
        {icon && <span className="set-app-ico"><Icon name={icon} size={17} /></span>}
        <div className="grow"><h3>{title}</h3>{sub && <p>{sub}</p>}</div>
        {actions && <div className="row">{actions}</div>}
      </div>
      {children && <div className="set-section-body">{children}</div>}
    </section>
  )
}

function useSettingsForm() {
  const { settings, setSettings, user } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<SettingsT | null>(settings)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!f && settings) setF(settings) }, [settings]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = async (patch?: Partial<SettingsT>) => {
    if (!f) return
    setBusy(true)
    try { const s = await api.put<SettingsT>(`${V1}/settings`, { ...f, ...patch }); setSettings(s); setF(s); toast('Saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return { f, setF, save, busy, isAdmin: user?.role === 'admin' }
}

/** "Test connection" results per integration. */
function useTester() {
  const [test, setTest] = useState<Record<string, string>>({})
  const run = async (which: string, url: string, key: string, extra: Record<string, string> = {}) => {
    setTest(t => ({ ...t, [which]: 'testing…' }))
    try { const r = await api.post<{ ok: boolean; message?: string; error?: string }>(`${V1}/integrations/test`, { which, url, key, ...extra }); setTest(t => ({ ...t, [which]: r.ok ? `✓ ${r.message}` : `✕ ${r.error}` })) } catch (e) { setTest(t => ({ ...t, [which]: `✕ ${(e as Error).message}` })) }
  }
  const status = (which: string) => <span className={`small ${test[which]?.startsWith('✓') ? 'ok' : test[which]?.startsWith('✕') ? 'bad' : 'muted'}`}>{test[which]}</span>
  return { run, status }
}

function General() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <Section title="The library" icon="grid">
        <SettingRow title="Cover size" help="Also from the S / M / L buttons above the games."><Segmented value={f.grid_size} onChange={v => setF({ ...f, grid_size: v })} size="sm" options={[{ value: 's', label: 'Small' }, { value: 'm', label: 'Medium' }, { value: 'l', label: 'Large' }]} /></SettingRow>
        <SettingRow title="Sort by"><Select ariaLabel="Sort by" value={f.default_sort} onChange={v => setF({ ...f, default_sort: v })} options={SORTS} /></SettingRow>
        <SettingRow title="Show games whose files are gone" help="Moved or removed outside the app; they keep your play time and saves."><Switch checked={f.show_missing} onChange={v => setF({ ...f, show_missing: v })} label="Show games whose files are gone" /></SettingRow>
        {isAdmin && <SettingRow title="Cover regions" help="When a game has several box arts, these regions win, in this order (for games without a region in their name)." stack><input value={f.regions} onChange={e => setF({ ...f, regions: e.target.value })} placeholder="Europe, USA, World" /></SettingRow>}
      </Section>
      <Section title="Dates and times" icon="calendar">
        <SettingRow title="Dates"><Select ariaLabel="Date format" value={f.date_format} onChange={v => setF({ ...f, date_format: v })} options={DATE_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        <SettingRow title="Times"><Select ariaLabel="Time format" value={f.time_format} onChange={v => setF({ ...f, time_format: v })} options={TIME_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
      </Section>
      {isAdmin && <Section title="App title" icon="edit" sub="Shown in the sidebar and the browser tab, for everyone on this box.">
        <input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} placeholder="Pocket Games" />
      </Section>}
      <div className="row"><button type="button" className="btn primary" onClick={() => { setDateFormats(f.date_format as DateFormat, f.time_format as TimeFormat); save() }} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function Player() {
  const { f, setF, save, busy } = useSettingsForm()
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <Section title="Saves" icon="save" sub="Your progress lives on the server, so it follows you to another computer, your phone or a handheld with a RomM app.">
        <SettingRow title="Keep in-game saves on the server" help="Memory cards and battery saves go up every 30 seconds while you play, and come back when you start the game anywhere."><Switch checked={f.auto_save} onChange={v => setF({ ...f, auto_save: v })} label="Keep in-game saves on the server" /></SettingRow>
        <SettingRow title="Keep the moment you leave" help="Leaving the player (or switching tabs) saves a state named “When you left”."><Switch checked={f.state_on_exit} onChange={v => setF({ ...f, state_on_exit: v })} label="Keep the moment you leave" /></SettingRow>
        <SettingRow title="Continue where you left off" help="Play starts from your newest state. The game page also has “From the start”."><Switch checked={f.resume_state} onChange={v => setF({ ...f, resume_state: v })} label="Continue where you left off" /></SettingRow>
      </Section>
      <Section title="Controls and picture" icon="gamepad" sub="Keyboard and game controllers work right away (the emulator’s menu lets you change the buttons). Bluetooth and USB controllers too.">
        <SettingRow title="On-screen buttons" help="Automatic shows them on phones and tablets."><Segmented value={f.touch_controls} onChange={v => setF({ ...f, touch_controls: v })} size="sm" options={[{ value: 'auto', label: 'Automatic' }, { value: 'on', label: 'Always' }, { value: 'off', label: 'Never' }]} /></SettingRow>
        <SettingRow title="Volume"><input type="range" min={0} max={100} value={f.player_volume} onChange={e => setF({ ...f, player_volume: Number(e.target.value) })} style={{ width: 200 }} aria-label="Volume" /> <span className="muted small tnum">{f.player_volume}%</span></SettingRow>
        <SettingRow title="Screen filter" help="Makes old games look like they did on a TV."><Select ariaLabel="Screen filter" value={f.player_shader} onChange={v => setF({ ...f, player_shader: v })} options={[{ value: '', label: 'None (sharp pixels)' }, { value: 'crt-easymode.glslp', label: 'CRT (soft)' }, { value: 'crt-aperture.glslp', label: 'CRT (aperture grille)' }, { value: 'crt-geom.glslp', label: 'CRT (curved)' }, { value: 'crt-mattias.glslp', label: 'CRT (Mattias)' }, { value: '2xScaleHQ.glslp', label: 'Smooth (2× HQ)' }, { value: '4xScaleHQ.glslp', label: 'Smooth (4× HQ)' }]} /></SettingRow>
      </Section>
      <div className="callout info"><Icon name="info" size={15} /><span>The player is EmulatorJS. Its files are downloaded once per system and then kept on this server, so the second start works without internet.</span></div>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function Devices() {
  const { toast } = useToast()
  const { settings } = useApp()
  const { data, reload } = useAsync(() => api.get<{ devices: Device[]; tokens: ClientToken[]; username: string; enabled: boolean; url: string }>(`${V1}/devices`), [])
  const [code, setCode] = useState<{ code: string; expires: number } | null>(null)
  const [name, setName] = useState('')
  const [secret, setSecret] = useState<{ name: string; token: string } | null>(null)
  const [del, setDel] = useState<Device | null>(null)
  const [delTok, setDelTok] = useState<ClientToken | null>(null)
  if (!data) return <Loading />
  const url = data.url || settings?.base_url || window.location.origin
  const connect = async () => {
    try { const r = await api.post<{ code: string; expires_in: number }>(`${V1}/devices/connect`, { name: name || 'Handheld' }); setCode({ code: r.code, expires: Date.now() + r.expires_in * 1000 }); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  const reveal = async (t: ClientToken) => {
    try { const r = await api.post<{ raw_token: string }>(`${V1}/devices/tokens/${t.id}/reveal`); setSecret({ name: t.name, token: r.raw_token }) } catch (e) { toast((e as Error).message, 'err') }
  }
  const named = data.devices.filter(d => !d.identifier.startsWith('ua:'))
  const seen = data.devices.filter(d => d.identifier.startsWith('ua:'))
  if (!data.enabled) return <div className="callout warn"><Icon name="alert" size={15} /><span>The RomM-compatible API is switched off on this server (<code>ROMM_COMPAT=false</code>).</span></div>
  return (
    <div className="stack loose">
      <Section title="Connect an app" icon="smartphone" sub="Pocket Games speaks RomM’s language: RomM apps connect to it as if it were a RomM server.">
        <ol className="steps">
          <li>In the app, add a RomM server with the address <CopyField value={url} label="Server address" /></li>
          <li><b>Sign in with a code</b> (Argosy, Grout, Ludo, Playnite…): the app shows a code — open <code>{url}/pair/device</code> on your phone or computer and allow it. Or press the button below and type the code it shows into the app.</li>
          <li><b>Sign in with a password</b> (romm-mobile, the muOS app, older apps): user name <code>{data.username}</code> (or your email) and your Pocket Games password{settings?.core_configured ? '. Signing in with Pocket Core gives you no password here — use a code or a token instead' : ''}.</li>
        </ol>
        <div className="row wrap mt-sm">
          <input value={name} onChange={e => setName(e.target.value)} placeholder="Name of the device (e.g. Odin 2)" style={{ maxWidth: 260 }} />
          <button type="button" className="btn primary" onClick={connect}><Icon name="key" size={15} />Make a pairing code</button>
          <Link to="/pair/device" className="btn ghost">Approve a code from an app</Link>
        </div>
        {code && <div className="pair-show"><span className="muted small">Type this into the app (works for 10 minutes, once):</span><b className="pair-big">{code.code.slice(0, 4)}-{code.code.slice(4)}</b></div>}
      </Section>
      <Section title="Your devices" icon="hard-drive" sub="Apps that signed in with a code or a token. Removing one signs it out.">
        {named.length === 0 && data.tokens.length === 0 ? <p className="muted">None yet.</p> : (
          <table className="table compact">
            <thead><tr><th>Device</th><th>App</th><th>Last seen</th><th className="num">Downloads</th><th /></tr></thead>
            <tbody>
              {named.map(d => <tr key={d.id}><td>{d.name || d.client}</td><td className="muted">{d.client}{d.client_version ? ` ${d.client_version}` : ''}{d.platform ? ` · ${d.platform}` : ''}</td><td className="muted">{fmtDateTime(d.last_seen)}</td><td className="num">{d.downloads}</td>
                <td className="right"><button type="button" className="btn ghost sm danger-text" onClick={() => setDel(d)}>Remove</button></td></tr>)}
              {data.tokens.filter(t => !t.device_id).map(t => <tr key={'t' + t.id}><td>{t.name}</td><td className="muted">token{t.last_used_at ? '' : ' · not used yet'}</td><td className="muted">{t.last_used_at ? fmtDateTime(t.last_used_at) : '—'}</td><td />
                <td className="right nowrap"><button type="button" className="btn ghost sm" onClick={() => reveal(t)} title="Makes a new secret; the old one stops working">Show a token</button><button type="button" className="btn ghost sm danger-text" onClick={() => setDelTok(t)}>Remove</button></td></tr>)}
            </tbody>
          </table>
        )}
        {secret && <div className="set-fresh mt-sm"><div className="row"><Icon name="check-circle" size={16} /><b>Token for {secret.name} — copy it now, it is shown once.</b></div><pre className="mono">{secret.token}</pre></div>}
      </Section>
      {seen.length > 0 && <Section title="Apps seen signing in with a password" icon="activity">
        <table className="table compact"><tbody>{seen.map(d => <tr key={d.id}><td className="ellipsis">{d.client}</td><td className="muted">{fmtDateTime(d.last_seen)}</td><td className="num muted">{d.requests} requests · {d.downloads} downloads</td>
          <td className="right"><button type="button" className="icon-btn" title="Forget" onClick={() => setDel(d)}><Icon name="x" size={14} /></button></td></tr>)}</tbody></table>
      </Section>}
      <div className="callout"><Icon name="info" size={15} /><span>RomM apps can’t delete games from this server: “delete” on a device only ever removes the device’s own copy. Games are deleted here, and wait in the <Link to="/trash">trash</Link> before they are gone.</span></div>
      {del && <Confirm title={`Remove ${del.name || del.client}?`} message="It is signed out and has to connect again." confirmLabel="Remove" onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/devices/${del.id}`); setDel(null); reload() }} />}
      {delTok && <Confirm title={`Remove ${delTok.name}?`} message="Apps using this token are signed out." confirmLabel="Remove" onCancel={() => setDelTok(null)} onConfirm={async () => { await api.del(`${V1}/devices/tokens/${delTok.id}`); setDelTok(null); reload() }} />}
    </div>
  )
}

function LibraryTab() {
  const { toast } = useToast()
  const { user } = useApp()
  const { data, reload } = useAsync(() => api.get<LibraryStatus>(`${V1}/library`), [])
  const [romm, setRomm] = useState({ url: '', username: '', password: '', saves: true, covers: true })
  const [busy, setBusy] = useState('')
  const [res, setRes] = useState<Record<string, number> | null>(null)
  if (!data) return <Loading />
  const admin = user?.role === 'admin'
  const scan = async () => { await api.post(`${V1}/library/scan`); toast('Looking for new games…'); setTimeout(() => { reload(); announceChange() }, 2500) }
  const lookupAll = async (all: boolean) => { try { const r = await api.post<{ queued: number }>(`${V1}/library/lookup`, { all }); toast(`${r.queued} game${r.queued === 1 ? '' : 's'} will be looked up again`); reload() } catch (e) { toast((e as Error).message, 'err') } }
  const importRomm = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy('romm'); setRes(null)
    try { const r = await api.post<Record<string, number>>(`${V1}/import/romm`, romm); setRes(r); announceChange(); toast('Brought over from RomM') } catch (err) { toast((err as Error).message, 'err') } finally { setBusy('') }
  }
  const sc = data.scan
  return (
    <div className="stack loose">
      <Section title="Looking for games" icon="refresh" sub={`The library folder is read every ${data.interval_minutes || '–'} minutes and after every upload. New games get a cover and a description on their own.`} actions={<button type="button" className="btn" onClick={scan} disabled={sc.running}>{sc.running ? <Spinner /> : <Icon name="refresh" size={15} />}Look now</button>}>
        <dl className="set-dl">
          <div><dt>Last look</dt><dd>{sc.ended_at ? `${fmtDateTime(sc.ended_at)} · ${sc.games} games in ${sc.platforms} folders · ${sc.ms} ms` : 'not yet'}{sc.error && <span className="bad"> · {sc.error}</span>}</dd></div>
          <div><dt>Waiting for a lookup</dt><dd>{data.pending}{data.metadata ? '' : ' (lookups are off: METADATA=false)'}</dd></div>
          <div><dt>Nothing found</dt><dd>{data.unmatched} {admin && data.unmatched > 0 && <button type="button" className="link-btn" onClick={() => lookupAll(false)}>try them again</button>}</dd></div>
          <div><dt>Being checksummed</dt><dd>{data.unhashed} (files over {fmtBytes(data.hash_max_bytes)} are skipped)</dd></div>
          {data.paths && <div><dt>Folders</dt><dd><code>{data.paths.roms}</code> · BIOS in <code>{data.paths.bios}</code> · trash in <code>{data.paths.trash}</code> ({data.trash_days} days)</dd></div>}
        </dl>
        {admin && <div className="row wrap mt-sm"><button type="button" className="btn sm ghost" onClick={() => lookupAll(true)}><Icon name="sparkles" size={14} />Look up every game again</button></div>}
      </Section>
      {data.unknown_platforms.length > 0 && <div className="callout warn"><Icon name="alert" size={15} /><span>Folders that are not a known system: {data.unknown_platforms.map(p => <code key={p}>{p}/ </code>)} — their games show up, but without covers. Rename the folder to the system’s short name (e.g. <code>snes</code>, <code>genesis</code>, <code>psx</code>).</span></div>}
      {data.duplicates.length > 0 && <Section title="The same game twice" icon="copy" sub="Identical files (same checksum) in more than one place.">
        <ul className="set-list">{data.duplicates.map((d, i) => <li key={i}>{d.map((g, j) => <span key={g.id}>{j > 0 && ' = '}<Link to={`/games/${g.id}`}>{g.platform_slug}/{g.fs_name}</Link></span>)}</li>)}</ul>
      </Section>}
      <Section title="Bring your things over from RomM" icon="archive" sub="Your favourites, statuses, ratings, collections, last played, saves and states — and RomM’s covers for games that have none here. Games are matched by system folder and file name, so RomM must look at the same library. Running it again adds nothing twice.">
        <form onSubmit={importRomm} className="stack">
          <div className="two-col tight grid">
            <Field label="RomM address"><input value={romm.url} onChange={e => setRomm({ ...romm, url: e.target.value })} placeholder="http://romm:8080" /></Field>
            <Field label="RomM user name"><input value={romm.username} onChange={e => setRomm({ ...romm, username: e.target.value })} autoComplete="off" /></Field>
          </div>
          <Field label="RomM password"><input type="password" value={romm.password} onChange={e => setRomm({ ...romm, password: e.target.value })} autoComplete="new-password" /></Field>
          <div className="row wrap">
            <label className="check-row"><input type="checkbox" checked={romm.saves} onChange={e => setRomm({ ...romm, saves: e.target.checked })} />Saves and states too</label>
            <label className="check-row"><input type="checkbox" checked={romm.covers} onChange={e => setRomm({ ...romm, covers: e.target.checked })} />Covers RomM found</label>
          </div>
          <div className="row"><button type="submit" className="btn primary" disabled={busy === 'romm' || !romm.url || !romm.username || !romm.password}>{busy === 'romm' ? <><Spinner />Bringing it over…</> : <><Icon name="download" size={15} />Bring it over</>}</button></div>
        </form>
        {res && <div className="callout info mt-sm"><Icon name="check-circle" size={15} /><span>{res.matched} of {res.games} RomM games matched{res.not_found ? ` (${res.not_found} not in this library)` : ''} · {res.favorites} favourites · {res.collections} new collections · {res.saves} saves · {res.states} states · {res.covers} covers</span></div>}
      </Section>
    </div>
  )
}
function Notifications() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <Section title="Push" icon="bell" actions={<span className={`badge ${f.notify_configured ? 'ok' : ''}`}>{f.notify_configured ? 'on' : 'not set'}</span>}
        sub={<>Apprise gateway (<code>…/notify/&lt;key&gt;</code>), an ntfy topic URL, or any webhook that takes JSON. On a box with Pocket Alerts use its Apprise address.</>}>
        <SettingRow title="URL" stack><input value={f.notify_url} onChange={e => setF({ ...f, notify_url: e.target.value })} placeholder="http://apprise:8000/notify/games" /></SettingRow>
        <SettingRow title="Format"><Segmented value={f.notify_format} onChange={v => setF({ ...f, notify_format: v })} options={[{ value: 'apprise', label: 'Apprise' }, { value: 'ntfy', label: 'ntfy' }, { value: 'json', label: 'JSON' }]} size="sm" /></SettingRow>
      </Section>
      <Section title="Tell me when" icon="inbox">
        <SettingRow title="New games arrive" help="Uploaded by anyone, or put into the library folder."><Switch checked={f.notify_new_games} onChange={v => setF({ ...f, notify_new_games: v })} label="New games arrive" /></SettingRow>
        <SettingRow title="A big upload finishes or fails" help="Uploads over 256 MB, so you can close the tab."><Switch checked={f.notify_uploads} onChange={v => setF({ ...f, notify_uploads: v })} label="A big upload finishes or fails" /></SettingRow>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        <button type="button" className="btn" onClick={() => run('notify', f.notify_url, '', { format: f.notify_format })} disabled={!f.notify_url}><Icon name="mail" size={15} />Send a test</button>
        {status('notify')}
      </div>
    </div>
  )
}

function AIStatusLine({ st }: { st: AIStatus | null }) {
  const { settings } = useApp()
  if (!st) return null
  if (st.ready) return <p className="muted small row" style={{ gap: 6 }}><Icon name="sparkles" size={13} />{st.provider === 'pocket' ? 'Pocket AI' : 'Your OpenAI key'} picks the games.</p>
  return (
    <div className="callout info"><Icon name="info" size={15} /><div>
      No AI is connected{st.detail ? ` (${st.detail})` : ''}, so picks come from your lists (backlog, unfinished, favourites).
      {!settings?.core_configured && <CoreHint />}
    </div></div>
  )
}
function AISettings() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  const ai = useAIStatus()
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  // Pocket AI only comes through Pocket Core: without one a saved "pocket" shows as what is in effect.
  const shown = !coreOn && f.ai_provider === 'pocket' ? 'openai' : (f.ai_provider || 'auto')
  return (
    <div className="stack loose">
      <Section title="What the AI does" icon="sparkles" sub="On the home page, “What should I play?” sends the names, systems, genres, statuses and play times of your games (and your mood, if you type one) to the AI you pick, and gets three suggestions back. Nothing else leaves the server.">
        <AIStatusLine st={ai} />
        <SettingRow title="Picks with AI" help="Off: the picks come from your lists without AI."><Switch checked={f.ai_picks} onChange={v => setF({ ...f, ai_picks: v })} label="Picks with AI" /></SettingRow>
      </Section>
      <Section title="AI" icon="sparkles">
        <div className="grid" style={{ gap: 12 }}>
          <Field group label="Which AI" help="Automatic prefers your enabled Pocket AI connection; without one it uses your OpenAI settings; with neither, picks come from your lists.">
            <Select ariaLabel="AI provider" value={shown} onChange={v => setF({ ...f, ai_provider: v as SettingsT['ai_provider'] })} options={[{ value: 'auto', label: coreOn ? 'Automatic — prefer Pocket AI' : 'Automatic' }, { value: 'pocket', label: 'Pocket AI', disabled: !coreOn, hint: coreOn ? undefined : 'needs Pocket Core' }, { value: 'openai', label: 'OpenAI / compatible API' }]} />
          </Field>
          {!coreOn && <CoreHint />}
          {coreOn && shown !== 'openai' && <PocketModelSelect value={f.ai_pocket_model || ''} onChange={v => setF({ ...f, ai_pocket_model: v })} />}
          {(shown === 'openai' || (!coreOn && shown === 'auto')) && <>
            <Field label="OpenAI API key" help="platform.openai.com → API keys. Stored on the server; shown masked."><input value={f.openai_api_key} onChange={e => setF({ ...f, openai_api_key: e.target.value })} autoComplete="off" placeholder="sk-…" /></Field>
            <div className="two-col tight">
              <Field label="Model" help="Default gpt-4o-mini."><input value={f.openai_model} onChange={e => setF({ ...f, openai_model: e.target.value })} placeholder="gpt-4o-mini" /></Field>
              <Field label="Base URL (optional)" help="Ollama, LM Studio, OpenRouter…"><input value={f.openai_base_url} onChange={e => setF({ ...f, openai_base_url: e.target.value })} placeholder="https://api.openai.com/v1" /></Field>
            </div>
          </>}
        </div>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        {(shown === 'openai' || (!coreOn && shown === 'auto')) && <><button type="button" className="btn" onClick={() => run('openai', f.openai_base_url, f.openai_api_key)} disabled={!f.openai_api_key}>Test the OpenAI key</button>{status('openai')}</>}
      </div>
    </div>
  )
}

function Users() {
  const { user, users, reloadMeta, status } = useApp()
  const { toast } = useToast()
  const [edit, setEdit] = useState<Partial<User> | null>(null)
  const [del, setDel] = useState<User | null>(null)
  const isAdmin = user?.role === 'admin'
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); await reloadMeta(); toast(msg) } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Section title="People on this box" actions={isAdmin && <button type="button" className="btn primary sm" onClick={() => setEdit({ role: 'member' })}><Icon name="plus" size={14} />Add person</button>}
      sub="Everyone signs in with their own account and has their own favourites, saves, play time and settings; the games are shared. Admins manage accounts; they do not see anyone else’s watches.">
      <div className="set-people">
        {users.map(u => (
          <div key={u.id} className="set-person">
            <Avatar name={u.name} email={u.email} size={34} />
            <div className="grow">
              <div className="set-person-name">{u.name || <span className="muted">—</span>}{u.id === user?.id && <span className="badge">you</span>}{u.role === 'admin' && <span className="badge accent">admin</span>}</div>
              <div className="muted small ellipsis">{u.email}</div>
            </div>
            {isAdmin && <span className="row" style={{ justifyContent: 'flex-end' }}><button type="button" className="btn ghost sm" onClick={() => setEdit(u)}>Edit</button>{u.id !== user?.id && <button type="button" className="btn ghost sm danger-text" onClick={() => setDel(u)}>Remove</button>}</span>}
          </div>
        ))}
      </div>
      {status?.auth_mode !== 'local' && <p className="muted small mt">Auth mode is <code>{status?.auth_mode}</code>: users are created automatically on first visit; passwords are not used here.</p>}
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reloadMeta() }} />}
      {del && <Confirm confirmLabel="Remove" title={`Remove ${del.name || del.email}?`} message="Their account is deleted together with their saves, play time and collections. Games they uploaded stay." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'User removed') }} />}
    </Section>
  )
}

function UserModal({ user, local, onClose, onSaved }: { user: Partial<User>; local: boolean; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: user.name || '', email: user.email || '', password: '', role: user.role || 'member' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { if (user.id) await api.put(`${V1}/users/${user.id}`, f); else await api.post(`${V1}/users`, f); toast(user.id ? 'User saved' : 'User added'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={user.id ? 'Edit person' : 'Add a person'} onClose={onClose} size="sm">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={60} /></Field>
        <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required /></Field>
        {local && <Field label={user.id ? 'New password (leave empty to keep)' : 'Password'} help="At least 8 characters."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={user.id ? 0 : 8} required={!user.id} autoComplete="new-password" /></Field>}
        <Field group label="Role"><Segmented value={f.role} onChange={v => setF({ ...f, role: v })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{user.id ? 'Save' : 'Add person'}</button></div>
      </form>
    </Modal>
  )
}

function Profile() {
  const { user, setUser, status } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: user?.name || '', email: user?.email || '', current_password: '', new_password: '' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { setUser(await api.put(`${V1}/auth/me`, f)); setF(x => ({ ...x, current_password: '', new_password: '' })); toast('Profile saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={save} className="stack loose">
      <div className="set-profile">
        <Avatar name={user?.name} email={user?.email} size={56} />
        <div className="grow"><h3>{user?.name || user?.email}</h3><p className="muted small">{user?.email} · {user?.role}</p></div>
      </div>
      <Section title="You">
        <div className="stack">
          <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
          <Field label="Email"><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
        </div>
      </Section>
      {status?.auth_mode === 'local' && <Section title="Password" sub="Leave empty to keep the current one."><div className="stack">
        <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF({ ...f, current_password: e.target.value })} autoComplete="current-password" /></Field>
        <Field label="New password" help="At least 8 characters."><input type="password" value={f.new_password} onChange={e => setF({ ...f, new_password: e.target.value })} autoComplete="new-password" /></Field>
      </div></Section>}
      <div className="row"><button className="btn primary" disabled={busy}>Save changes</button></div>
    </form>
  )
}

function Tokens() {
  const { toast } = useToast()
  const { settings } = useApp()
  const { data, reload } = useAsync(() => api.get<APIToken[]>(`${V1}/tokens`), [])
  const [name, setName] = useState('')
  const [fresh, setFresh] = useState('')
  const [del, setDel] = useState<APIToken | null>(null)
  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    try { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: name || 'token' }); setFresh(r.token); setName(''); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  const base = settings?.base_url || window.location.origin
  return (
    <div className="stack loose">
      <Section title="Personal tokens" icon="key" sub={<>Use as <code>Authorization: Bearer &lt;token&gt;</code>. A token acts as you: it can read and change everything, feed a Homepage widget (<code>GET /api/v1/dashboard</code>) or drive a phone shortcut. RomM apps take it too. See <code>docs/api.md</code>.</>}>
        <form onSubmit={create} className="row mb"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. Homepage widget)" /><button className="btn primary"><Icon name="plus" size={15} />Create</button></form>
        {fresh && <div className="set-fresh mb"><div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — it is shown once.</b></div><pre className="mono">{fresh}</pre></div>}
        {data && data.length > 0 ? (
          <table className="table compact">
            <thead><tr><th>Name</th><th>Prefix</th><th>Last used</th><th>Created</th><th /></tr></thead>
            <tbody>{data.map(t => <tr key={t.id}><td>{t.name}</td><td className="mono">{t.prefix}…</td><td className="muted">{t.last_used_at ? fmtDateTime(t.last_used_at) : 'never'}</td><td className="muted">{fmtDateTime(t.created_at)}</td><td className="right"><button type="button" className="btn ghost sm danger-text" onClick={() => setDel(t)}>Revoke</button></td></tr>)}</tbody>
          </table>
        ) : <p className="muted">No tokens yet.</p>}
        {del && <Confirm confirmLabel="Revoke" title={`Revoke “${del.name}”?`} message="Anything using this token stops working immediately." onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
      </Section>
      <Section title="Handy endpoints" icon="smartphone" sub={<>At <code>{base}/api/v1</code>, with <code>Authorization: Bearer &lt;token&gt;</code>.</>}>
        <dl className="set-dl">
          <div><dt><code>GET /dashboard</code></dt><dd className="muted">Games, systems, playing, backlog, minutes this week — for a Homepage widget.</dd></div>
          <div><dt><code>GET /games?q=…</code></dt><dd className="muted">Search the library (also <code>status</code>, <code>favorite</code>, <code>platform</code>, <code>sort</code>).</dd></div>
          <div><dt><code>GET /day/YYYY-MM-DD</code></dt><dd className="muted">What you played on a day, for a journal.</dd></div>
          <div><dt><code>POST /games/&#123;id&#125;/playtime</code></dt><dd className="muted"><code>{'{"minutes":45}'}</code> — time played on a console.</dd></div>
        </dl>
        <p className="muted small">RomM apps use the RomM-compatible API under <code>{base}/api</code> — see Settings → Devices.</p>
      </Section>
    </div>
  )
}

function Data() {
  const { toast } = useToast()
  const { user } = useApp()
  const [busy, setBusy] = useState(false)
  const [res, setRes] = useState<Record<string, number> | null>(null)
  const send = async (file: File) => {
    setBusy(true); setRes(null)
    try { const r = await postFile<Record<string, number>>(`${V1}/import`, file); setRes(r); announceChange(); toast('Imported') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <div className="stack loose">
      <Section title="Export" icon="download" sub="What you think of your games (favourites, statuses, ratings, notes), your play time and your collections, as one JSON file. Games are named by system and file name, so it works on another box with the same games.">
        <a className="btn" href={`${V1}/export.json`}><Icon name="download" size={15} />Export JSON</a>
      </Section>
      <Section title="Import" icon="upload" sub="A Pocket Games export. Running it twice changes nothing the second time. Coming from RomM? Use Settings → Library.">
        <label className="btn"><Icon name="upload" size={15} />{busy ? 'Importing…' : 'Choose a file…'}<input type="file" accept=".json,application/json" hidden onChange={e => { const file = e.target.files?.[0]; e.target.value = ''; if (file) send(file) }} /></label>
        {res && <p className="muted small mt-sm">{res.games} games · {res.sessions} sittings · {res.collections} new collections{res.not_found ? ` · ${res.not_found} games not in this library` : ''}</p>}
      </Section>
      {user?.role === 'admin' && <Section title="Database backup" icon="database" sub="The whole box, every account — admins only.">
        <p className="muted">A consistent copy of the SQLite database (<code>VACUUM INTO</code>). Covers, saves and states live next to it under <code>files/</code>; the games themselves are in the library folder. Restore by stopping the app and replacing <code>pocket-games.db</code> in the data directory.</p>
        <a className="btn primary" href={`${V1}/backup`}><Icon name="download" size={15} />Download database</a>
      </Section>}
    </div>
  )
}
function About() {
  const { data, error } = useAsync(() => api.get<Record<string, unknown>>(`${V1}/settings/system`), [])
  const [keys, setKeys] = useState(false)
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return <Loading />
  const counts = (data.counts || {}) as Record<string, number>
  const scan = (data.scan || {}) as Record<string, number>
  const tiles: [string, number | undefined, string][] = [['Games', scan.games, 'gamepad'], ['Systems', scan.platforms, 'cpu'], ['Your saves', counts.saves, 'save'], ['Your sittings', counts.play_sessions, 'clock']]
  const rows: [string, React.ReactNode][] = [
    ['Version', <code key="v">{String(data.version)}</code>],
    ['Uptime', String(data.uptime)],
    ['Runtime', `${String(data.go)} · ${String(data.os)}/${String(data.arch)}`],
    ['Time zone', String(data.timezone)],
    ['Sign-in', String(data.auth_mode)],
    ['Database', <span key="db">{data.db_path ? <><code>{String(data.db_path)}</code> · </> : null}{fmtBytes(Number(data.db_bytes))} · covers and saves {fmtBytes(Number(data.files_bytes))} · player files {fmtBytes(Number(data.player_bytes))}</span>],
    ['Library', <span key="lib">{data.library_dir ? <><code>{String(data.library_dir)}</code> · </> : null}{Number(data.free_bytes) >= 0 ? `${fmtBytes(Number(data.free_bytes))} free` : ''}</span>],
    ['RomM apps', data.romm_compat ? 'the RomM-compatible API is on (/api)' : 'off (ROMM_COMPAT=false)'],
    ['People', `${counts.users ?? 0} accounts on this box`],
  ]
  return (
    <div className="stack loose">
      <div className="set-about-hero">
        <img src="/favicon.svg" alt="" width={48} height={48} />
        <div>
          <h3>Pocket Games</h3>
          <p className="muted small">Your retro game library: upload games, play them in the browser with saves that follow you, keep favourites, collections and play time, and let RomM apps on handhelds download games and sync saves. Covers and descriptions without any API key. One Go binary, one SQLite file, one volume.</p>
        </div>
      </div>
      <div className="set-stats">
        {tiles.map(([label, n, icon]) => (
          <div key={label} className="set-stat"><Icon name={icon} size={16} /><span className="set-stat-n tnum">{n ?? 0}</span><span className="set-stat-l">{label}</span></div>
        ))}
      </div>
      <Section title="This server">
        <dl className="set-dl">
          {rows.map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}
        </dl>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn" onClick={() => setKeys(true)}><Icon name="keyboard" size={15} />Keyboard shortcuts</button>
        <a className="btn" href="https://github.com/abjelosevic88/pocket-homelab-games" target="_blank" rel="noreferrer"><Icon name="external" size={15} />Project on GitHub</a>
      </div>
      <p className="muted small">Open source (MIT). Part of the Pocket apps: Pocket Core connects them — Day shows what you played, Flows reacts to new games. The player is <a href="https://emulatorjs.org" target="_blank" rel="noreferrer">EmulatorJS</a>; box art comes from the <a href="https://thumbnails.libretro.com" target="_blank" rel="noreferrer">libretro thumbnails</a>, descriptions from Wikipedia.</p>
      {keys && <ShortcutsSheet onClose={() => setKeys(false)} />}
    </div>
  )
}
