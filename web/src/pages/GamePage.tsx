import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { fmtDate, fmtDateTime } from '../lib/format'
import { flags, playtime, STATUS, year } from '../lib/games'
import type { Game, GameDetail, Save } from '../lib/types'
import { Cover, GameCard, size, Stars, useCollections, useMine } from '../components/game'
import { Icon } from '../components/icons'
import { announceChange, useCrumb } from '../components/Layout'
import { Select } from '../components/Select'
import { Confirm, Empty, Field, Loading, type MenuItem, MenuButton, Modal, Spinner, useAsync, useToast } from '../components/ui'

export default function GamePage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { toast } = useToast()
  const { settings } = useApp()
  const { data, reload, error } = useAsync(() => api.get<GameDetail>(`${V1}/games/${id}`), [id])
  const [detail, setDetail] = useState<GameDetail | null>(null)
  useEffect(() => { if (data) setDetail(data) }, [data])
  useCrumb(detail?.game.name)
  const g = detail?.game || null
  const setGame = (ng: Game) => { if (detail) setDetail({ ...detail, game: { ...detail.game, ...ng } }); announceChange() }
  const mine = useMine(g, setGame)
  const cols = useCollections()
  const [modal, setModal] = useState<'' | 'edit' | 'cover' | 'trash' | 'time' | 'lookup'>('')
  const [busy, setBusy] = useState(false)
  if (error) return <Empty icon="alert" title="This game is not here">{error}</Empty>
  if (!detail || !g) return <Loading />
  const d = detail
  const states = d.saves.filter(s => s.kind === 'state')
  const saves = d.saves.filter(s => s.kind === 'save')
  const lookup = async (cover: boolean) => {
    setBusy(true)
    try { setGame(await api.post<Game>(`${V1}/games/${g.id}/lookup`, { cover })); toast('Looked up again'); reload() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const toggleCol = async (cid: number, inIt: boolean) => {
    try { await api.put(`${V1}/collections/${cid}/games/${g.id}`, { in: !inIt }); setDetail({ ...d, collections: inIt ? d.collections.filter(x => x !== cid) : [...d.collections, cid] }); cols.reload(); announceChange() } catch (e) { toast((e as Error).message, 'err') }
  }
  const newCollection = async () => {
    const name = window.prompt('Name of the new collection')
    if (!name?.trim()) return
    try { const c = await api.post<{ id: number }>(`${V1}/collections`, { name, game_ids: [g.id] }); setDetail({ ...d, collections: [...d.collections, c.id] }); cols.reload(); announceChange(); toast(`Added to “${name}”`) } catch (e) { toast((e as Error).message, 'err') }
  }
  const colItems = (): MenuItem[] => [
    { heading: 'Collections' },
    ...cols.list.map(c => ({ label: c.name, checked: d.collections.includes(c.id), onSelect: () => toggleCol(c.id, d.collections.includes(c.id)) })),
    ...(cols.list.length ? ['sep' as const] : []),
    { label: 'New collection…', icon: 'plus', onSelect: newCollection },
  ]
  const moreItems = (): MenuItem[] => [
    { label: 'Download', icon: 'download', onSelect: () => { window.location.href = `${V1}/games/${g.id}/download` } },
    { label: 'Add time played elsewhere', icon: 'clock', onSelect: () => setModal('time') },
    { label: g.mine.hidden ? 'Show in the library again' : 'Hide from my library', icon: 'eye', onSelect: () => mine({ hidden: !g.mine.hidden }) },
    ...(g.can_edit ? ['sep' as const, { heading: 'The game' },
      { label: 'Edit details', icon: 'edit', onSelect: () => setModal('edit') },
      { label: 'Change the cover', icon: 'image', onSelect: () => setModal('cover') },
      { label: 'Look it up again', icon: 'refresh', onSelect: () => setModal('lookup') },
      'sep' as const, { label: 'Move to the trash', icon: 'trash', danger: true, onSelect: () => setModal('trash') }] : []),
  ]
  const facts: [string, string][] = ([['Released', g.release_date ? fmtDate(g.release_date.length === 4 ? `${g.release_date}-01-01` : g.release_date.length === 7 ? `${g.release_date}-01` : g.release_date).replace(/^1 Jan |^Jan 1, /, '') : ''],
    ['Genre', g.genres], ['Developer', g.developer], ['Publisher', g.publisher], ['Modes', g.players],
    ['Region', g.regions ? `${flags(g.regions)} ${g.regions}` : ''], ['Languages', g.languages], ['Version', g.tags]] as [string, string][]).filter(([, v]) => v)
  const status = STATUS.find(s => s.value === g.mine.status)
  return (
    <div className="game-page">
      <div className="game-hero">
        {g.cover && <div className="game-hero-bg" style={{ backgroundImage: `url("${g.cover}")` }} aria-hidden />}
        <div className="game-hero-in">
          <div className="game-cover-wrap"><Cover game={g} className="game-cover" alt={g.name} /></div>
          <div className="game-head">
            <div className="game-kicker"><Link to={`/platforms/${g.platform_slug}`}>{g.platform_name}</Link>{year(g) && <span>· {year(g)}</span>}{g.regions && <span title={g.regions}>· {flags(g.regions)}</span>}<span>· {size(g.size_bytes)}</span></div>
            <h1>{g.name}</h1>
            {g.trashed_at && <div className="callout warn"><Icon name="trash" size={15} /><span>In the trash since {fmtDateTime(g.trashed_at)}. <Link to="/trash">Restore it</Link> from the trash.</span></div>}
            {g.missing_since && <div className="callout warn"><Icon name="alert" size={15} /><span>The game’s files left the library on {fmtDateTime(g.missing_since)}. Your play time and saves are kept in case it comes back.</span></div>}
            <div className="game-actions">
              {d.play.browser ? <>
                <a className="btn primary lg play-btn" href={`/play/${g.id}`}><Icon name="play" size={18} strokeWidth={2.2} />{states.length ? 'Continue' : 'Play'}</a>
                {states.length > 0 && <a className="btn lg" href={`/play/${g.id}?fresh=1`} title="Start without loading a state"><Icon name="refresh" size={16} />From the start</a>}
              </> : <a className="btn primary lg" href={`${V1}/games/${g.id}/download`}><Icon name="download" size={18} />Download</a>}
              <button type="button" className={`btn lg icon-only fav-btn ${g.mine.favorite ? 'on' : ''}`} onClick={() => mine({ favorite: !g.mine.favorite })} aria-pressed={g.mine.favorite} title={g.mine.favorite ? 'Remove from favourites' : 'Add to favourites'}><Icon name="heart" size={18} strokeWidth={2.2} /></button>
              <MenuButton label="Collections" className="btn lg icon-only" items={colItems} width={240}><Icon name="layers" size={18} /></MenuButton>
              <MenuButton label="More" className="btn lg icon-only" items={moreItems} align="right" width={250}><Icon name="more" size={18} /></MenuButton>
            </div>
            {!d.play.browser && d.play.why && <p className="muted small play-why"><Icon name="info" size={13} />{d.play.why}</p>}
            {d.play.bios_missing && <p className="small play-why warn-text"><Icon name="alert" size={13} />{d.platform.short} needs a BIOS file to start. <Link to={`/platforms/${g.platform_slug}`}>Add it on the system’s page</Link>.</p>}
            <div className="game-mine">
              <Select ariaLabel="Status" value={g.mine.status} onChange={v => mine({ status: v as Game['mine']['status'] })} options={STATUS.map(s => ({ value: s.value, label: s.label }))} />
              <Stars value={g.mine.rating} onChange={v => mine({ rating: v })} />
              <span className="game-pt muted small"><Icon name="clock" size={13} />{g.mine.play_seconds ? `${playtime(g.mine.play_seconds)} played` : 'Not played yet'}{g.mine.last_played_at ? ` · last ${fmtDateTime(g.mine.last_played_at)}` : ''}</span>
              {status && status.value && <span className="sr-only">{status.label}</span>}
            </div>
          </div>
        </div>
      </div>

      <div className="game-body">
        <div className="game-main">
          <section className="card pad">
            <h2 className="sec-h">About</h2>
            {g.summary ? <>
              <p className="game-summary">{g.summary}</p>
              {g.summary_source === 'wikipedia' && g.summary_url && <p className="muted small">From <a href={g.summary_url} target="_blank" rel="noreferrer">Wikipedia</a>, CC BY-SA.</p>}
            </> : <p className="muted">{g.meta_status === 'pending' && settings?.metadata !== false ? 'Looking it up…' : 'Nothing about this game yet.'} {g.can_edit && <button type="button" className="link-btn" onClick={() => setModal('edit')}>Write something</button>}</p>}
            {facts.length > 0 && <dl className="kv game-facts">{facts.map(([k, v]) => <div key={k} className="kv-row"><dt>{k}</dt><dd>{v}</dd></div>)}</dl>}
          </section>

          {g.shots.length > 0 && <section className="game-shots">{g.shots.map((s, i) => <a key={s} href={s} target="_blank" rel="noreferrer"><img src={s} alt={`Picture ${i + 1}`} loading="lazy" /></a>)}</section>}

          <section className="card pad">
            <div className="sec-head"><h2 className="sec-h">Saved states</h2><span className="muted small">{d.play.browser ? 'Made in the player (Save) or by a device.' : 'Uploaded by a device.'}</span></div>
            {states.length === 0 ? <p className="muted small">None yet. In the player, press <b>Save</b> to keep a moment; leaving the player keeps one too.</p>
              : <div className="state-grid">{states.map(s => <StateCard key={s.id} save={s} game={g} onChange={reload} />)}</div>}
          </section>

          <section className="card pad">
            <div className="sec-head"><h2 className="sec-h">In-game saves</h2><span className="muted small">Memory cards and battery saves, kept on the server and shared with your devices.</span></div>
            {saves.length === 0 ? <p className="muted small">None yet.</p> : <SaveTable saves={saves} onChange={reload} />}
            <UploadSave game={g} onDone={reload} />
          </section>

          <section className="card pad">
            <div className="sec-head"><h2 className="sec-h">Play history</h2><button type="button" className="btn sm ghost" onClick={() => setModal('time')}><Icon name="plus" size={14} />Add time</button></div>
            {d.sessions.length === 0 ? <p className="muted small">Nothing yet. Time in the player and on connected devices shows up here.</p> : (
              <table className="table compact"><tbody>{d.sessions.map(s => (
                <tr key={s.id}><td>{fmtDateTime(s.started_at)}</td><td className="muted">{s.device}</td><td className="num">{playtime(s.seconds)}</td>
                  <td className="right"><button type="button" className="icon-btn" title="Remove this sitting" onClick={async () => { await api.del(`${V1}/sessions/${s.id}`); reload(); announceChange() }}><Icon name="x" size={14} /></button></td></tr>
              ))}</tbody></table>
            )}
          </section>
        </div>

        <aside className="game-side">
          <section className="card pad">
            <h2 className="sec-h">Your notes</h2>
            <Notes game={g} onSave={notes => mine({ notes })} />
          </section>
          <section className="card pad">
            <h2 className="sec-h">Files</h2>
            <ul className="file-list">{d.files.map(f => (
              <li key={f.id}>
                <a href={`${V1}/games/${g.id}/files/${f.id}`} className="file-row" title={f.md5 ? `MD5 ${f.md5}\nSHA-1 ${f.sha1}\nCRC32 ${f.crc32}` : 'not checksummed (too big)'}>
                  <Icon name={f.rel === g.main_file ? 'gamepad' : 'note'} size={14} /><span className="ellipsis grow">{f.rel}</span><span className="muted small tnum">{size(f.size_bytes)}</span>
                </a>
              </li>
            ))}</ul>
            <p className="muted small mt-sm">{g.is_folder ? 'A folder' : 'In'} <code>{g.platform_slug}/{g.fs_name}</code>{d.added_by ? ` · added by ${d.added_by}` : ''} · {fmtDate(g.created_at.slice(0, 10))}</p>
            {g.md5 && <p className="muted small mono ellipsis" title="MD5 of the main file">MD5 {g.md5}</p>}
          </section>
          {d.bios && (d.platform.needs_bios || d.bios.files > 0) && <section className="card pad">
            <h2 className="sec-h">BIOS</h2>
            <p className={`small row ${d.bios.ok ? 'ok' : 'warn-text'}`}><Icon name={d.bios.ok ? 'check-circle' : 'alert'} size={14} />{d.bios.ok ? 'Ready' : 'Missing'} — <Link to={`/platforms/${g.platform_slug}`}>{d.platform.short} BIOS files</Link></p>
          </section>}
          {d.play.browser && (d.play.cores?.length || 0) > 1 && <section className="card pad">
            <h2 className="sec-h">Emulator</h2>
            <p className="muted small">If the game misbehaves, try another one.</p>
            <Select ariaLabel="Emulator" value={g.mine.core} onChange={v => mine({ core: v })} clearLabel="Automatic"
              options={[{ value: '', label: 'Automatic' }, ...(d.play.cores || []).map(c => ({ value: c, label: c.replace(/_/g, ' ') }))]} />
          </section>}
          {d.others.length > 0 && <section className="card pad">
            <h2 className="sec-h">Other copies</h2>
            <div className="ggrid size-s">{d.others.map(o => <GameCard key={o.id} game={o} />)}</div>
          </section>}
        </aside>
      </div>

      {modal === 'edit' && <EditGame game={g} onClose={() => setModal('')} onSaved={ng => { setGame(ng); setModal('') }} />}
      {modal === 'cover' && <CoverPicker game={g} onClose={() => setModal('')} onSaved={ng => { setGame(ng); setModal('') }} />}
      {modal === 'time' && <AddTime game={g} onClose={() => setModal('')} onSaved={() => { setModal(''); reload(); announceChange() }} />}
      {modal === 'lookup' && <LookupModal game={g} busy={busy} onClose={() => setModal('')} onAuto={async () => { await lookup(true); setModal('') }} onArticle={async article => {
        setBusy(true)
        try { setGame(await api.post<Game>(`${V1}/games/${g.id}/lookup`, { article })); toast('Updated from Wikipedia'); setModal(''); reload() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
      }} />}
      {modal === 'trash' && <Confirm title={`Move “${g.name}” to the trash?`} confirmLabel="Move to the trash"
        message={<>The files move to the library’s trash folder and stay restorable for the next days. Everyone on this box stops seeing it; play times and saves are kept until the trash is emptied.</>}
        onCancel={() => setModal('')} onConfirm={async () => { try { await api.del(`${V1}/games/${g.id}`); announceChange(); toast('Moved to the trash'); navigate('/library') } catch (e) { toast((e as Error).message, 'err') } }} />}
    </div>
  )
}

function Notes({ game, onSave }: { game: Game; onSave: (v: string) => void }) {
  const [v, setV] = useState(game.mine.notes)
  useEffect(() => setV(game.mine.notes), [game.id]) // eslint-disable-line react-hooks/exhaustive-deps
  return <textarea className="notes" rows={4} value={v} placeholder="Where you are, passwords, tips…" onChange={e => setV(e.target.value)} onBlur={() => { if (v !== game.mine.notes) onSave(v) }} />
}

function StateCard({ save, game, onChange }: { save: Save; game: Game; onChange: () => void }) {
  const { toast } = useToast()
  const [del, setDel] = useState(false)
  const rename = async () => {
    const name = window.prompt('Name this state', save.slot === 'auto' ? '' : save.slot)
    if (name === null) return
    try { await api.put(`${V1}/saves/${save.id}`, { slot: name }); onChange() } catch (e) { toast((e as Error).message, 'err') }
  }
  const title = save.slot === 'auto' ? 'When you left' : save.slot || fmtDateTime(save.updated_at)
  return (
    <div className="state-card">
      <a href={game.browser ? `/play/${game.id}?state=${save.id}` : `${V1}/saves/${save.id}/file`} className="state-pic" title={game.browser ? 'Continue from here' : 'Download'}>
        {save.has_shot ? <img src={`${V1}/saves/${save.id}/shot`} alt="" loading="lazy" /> : <div className="state-none"><Icon name="save" size={22} /></div>}
        {game.browser && <span className="state-play"><Icon name="play" size={18} strokeWidth={2.2} /></span>}
      </a>
      <div className="state-meta">
        <b className="ellipsis">{title}</b>
        <span className="muted small ellipsis">{save.slot ? fmtDateTime(save.updated_at) + ' · ' : ''}{save.device}</span>
      </div>
      <MenuButton label="State" className="icon-btn" align="right" items={[
        { label: 'Name it', icon: 'edit', onSelect: rename },
        { label: 'Download', icon: 'download', onSelect: () => { window.location.href = `${V1}/saves/${save.id}/file` } },
        'sep', { label: 'Delete', icon: 'trash', danger: true, onSelect: () => setDel(true) }]}><Icon name="more" size={15} /></MenuButton>
      {del && <Confirm title="Delete this state?" message="It cannot be brought back." confirmLabel="Delete" onCancel={() => setDel(false)} onConfirm={async () => { await api.del(`${V1}/saves/${save.id}`); setDel(false); onChange() }} />}
    </div>
  )
}

function SaveTable({ saves, onChange }: { saves: Save[]; onChange: () => void }) {
  const [del, setDel] = useState<Save | null>(null)
  return (
    <>
      <table className="table compact"><tbody>{saves.map(s => (
        <tr key={s.id}>
          <td className="ellipsis"><Icon name="save" size={13} /> {s.file_name}</td>
          <td className="muted">{s.emulator || s.device}</td>
          <td className="muted">{fmtDateTime(s.updated_at)}</td>
          <td className="num muted">{size(s.size_bytes)}</td>
          <td className="right nowrap">
            <a className="icon-btn" href={`${V1}/saves/${s.id}/file`} title="Download"><Icon name="download" size={14} /></a>
            <button type="button" className="icon-btn" title="Delete" onClick={() => setDel(s)}><Icon name="trash" size={14} /></button>
          </td>
        </tr>
      ))}</tbody></table>
      {del && <Confirm title={`Delete “${del.file_name}”?`} message="The progress in it is lost unless a device still has a copy." confirmLabel="Delete" onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/saves/${del.id}`); setDel(null); onChange() }} />}
    </>
  )
}

function UploadSave({ game, onDone }: { game: Game; onDone: () => void }) {
  const { toast } = useToast()
  const [busy, setBusy] = useState(false)
  const send = async (f: File, kind: 'save' | 'state') => {
    setBusy(true)
    const fd = new FormData()
    fd.append('file', f, f.name)
    try {
      const r = await fetch(`${V1}/games/${game.id}/saves?kind=${kind}&device=upload`, { method: 'POST', body: fd, credentials: 'same-origin' })
      if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error || r.statusText)
      toast('Uploaded'); onDone()
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <div className="row wrap mt-sm">
      <label className="btn sm"><Icon name="upload" size={14} />{busy ? 'Uploading…' : 'Upload a save file'}<input type="file" hidden onChange={e => { const f = e.target.files?.[0]; e.target.value = ''; if (f) send(f, /\.(state|ss\d|st\d)/i.test(f.name) ? 'state' : 'save') }} /></label>
    </div>
  )
}

function EditGame({ game, onClose, onSaved }: { game: Game; onClose: () => void; onSaved: (g: Game) => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: game.name, summary: game.summary, genres: game.genres, developer: game.developer, publisher: game.publisher, release_date: game.release_date, players: game.players })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try { onSaved(await api.put<Game>(`${V1}/games/${game.id}`, f)); toast('Saved') } catch (err) { toast((err as Error).message, 'err') } finally { setBusy(false) }
  }
  const field = (k: keyof typeof f, label: string, ph = '') => <Field label={label}><input value={f[k]} onChange={e => setF({ ...f, [k]: e.target.value })} placeholder={ph} /></Field>
  return (
    <Modal title="Edit details" onClose={onClose} size="lg" footer={<><button type="button" className="btn ghost" onClick={onClose}>Cancel</button><button type="submit" form="edit-game" className="btn primary" disabled={busy}>Save</button></>}>
      <form id="edit-game" onSubmit={save} className="stack">
        {field('name', 'Name')}
        <Field label="Description"><textarea rows={6} value={f.summary} onChange={e => setF({ ...f, summary: e.target.value })} /></Field>
        <div className="two-col tight grid">{field('genres', 'Genres', 'Platformer, Action')}{field('release_date', 'Released', 'YYYY or YYYY-MM-DD')}</div>
        <div className="two-col tight grid">{field('developer', 'Developer')}{field('publisher', 'Publisher')}</div>
        {field('players', 'Modes', 'Single-player, Multiplayer')}
        <p className="muted small">What you type here stays: looking the game up again does not overwrite it.</p>
      </form>
    </Modal>
  )
}

function CoverPicker({ game, onClose, onSaved }: { game: Game; onClose: () => void; onSaved: (g: Game) => void }) {
  const { toast } = useToast()
  const [q, setQ] = useState('')
  const [list, setList] = useState<{ name: string; url: string }[] | null>(null)
  const [busy, setBusy] = useState(false)
  const load = (query = '') => { setList(null); api.get<{ name: string; url: string }[]>(`${V1}/games/${game.id}/covers${query ? `?q=${encodeURIComponent(query)}` : ''}`).then(setList).catch(e => { toast((e as Error).message, 'err'); setList([]) }) }
  useEffect(() => { load() }, []) // eslint-disable-line react-hooks/exhaustive-deps
  const pick = async (url: string) => {
    setBusy(true)
    try { onSaved(await api.post<Game>(`${V1}/games/${game.id}/cover`, { url })); toast('Cover changed') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const upload = async (f: File) => {
    setBusy(true)
    try {
      const r = await fetch(`${V1}/games/${game.id}/cover`, { method: 'POST', body: f, credentials: 'same-origin', headers: { 'Content-Type': f.type || 'application/octet-stream' } })
      if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error || r.statusText)
      onSaved(await r.json()); toast('Cover changed')
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title="Change the cover" onClose={onClose} size="xl">
      <div className="toolbar">
        <form className="search" onSubmit={e => { e.preventDefault(); load(q) }}><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search the cover collection…" /></form>
        <label className="btn"><Icon name="upload" size={15} />Your own picture<input type="file" accept="image/*" hidden onChange={e => { const f = e.target.files?.[0]; e.target.value = ''; if (f) upload(f) }} /></label>
        {busy && <Spinner />}
      </div>
      {!list ? <Loading /> : list.length === 0 ? <p className="muted">Nothing found. Try fewer words, or upload a picture.</p> : (
        <div className="cover-pick">{list.map(c => (
          <button key={c.url} type="button" className="cover-opt" onClick={() => pick(c.url)} disabled={busy} title={c.name}>
            <img src={c.url} alt="" loading="lazy" referrerPolicy="no-referrer" /><span className="small ellipsis">{c.name}</span>
          </button>
        ))}</div>
      )}
      <p className="muted small mt-sm">Box art from the libretro thumbnail collection.</p>
    </Modal>
  )
}

function LookupModal({ game, busy, onClose, onAuto, onArticle }: { game: Game; busy: boolean; onClose: () => void; onAuto: () => void; onArticle: (t: string) => void }) {
  const [t, setT] = useState(game.wiki_title)
  return (
    <Modal title="Look it up again" onClose={onClose} footer={<button type="button" className="btn ghost" onClick={onClose}>Close</button>}>
      <div className="stack">
        <p className="muted">Pocket Games finds covers in the libretro collection and descriptions on Wikipedia, by the game’s name and system.</p>
        <button type="button" className="btn primary" onClick={onAuto} disabled={busy}>{busy ? <Spinner /> : <Icon name="refresh" size={15} />}Look up the cover and description again</button>
        <Field label="Or name the Wikipedia article" help="The title or the address of the article about this game, e.g. “Silent Hill 2” or https://en.wikipedia.org/wiki/Silent_Hill_2.">
          <div className="row"><input value={t} onChange={e => setT(e.target.value)} placeholder="Article title" /><button type="button" className="btn" disabled={busy || !t.trim()} onClick={() => onArticle(t)}>Use it</button></div>
        </Field>
      </div>
    </Modal>
  )
}

function AddTime({ game, onClose, onSaved }: { game: Game; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [min, setMin] = useState(60)
  const [day, setDay] = useState(new Date().toISOString().slice(0, 10))
  const [device, setDevice] = useState('')
  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    try { await api.post(`${V1}/games/${game.id}/playtime`, { minutes: min, day, device: device || 'elsewhere' }); toast('Added'); onSaved() } catch (err) { toast((err as Error).message, 'err') }
  }
  return (
    <Modal title="Add time played elsewhere" onClose={onClose} size="sm" footer={<><button type="button" className="btn ghost" onClick={onClose}>Cancel</button><button type="submit" form="add-time" className="btn primary">Add</button></>}>
      <form id="add-time" onSubmit={save} className="stack">
        <div className="two-col tight grid">
          <Field label="Minutes"><input type="number" min={1} max={1440} value={min} onChange={e => setMin(Number(e.target.value))} /></Field>
          <Field label="Day"><input type="date" value={day} max={new Date().toISOString().slice(0, 10)} onChange={e => setDay(e.target.value)} /></Field>
        </div>
        <Field label="Where" help="e.g. the console, a handheld without the app."><input value={device} onChange={e => setDevice(e.target.value)} placeholder="Original console" /></Field>
      </form>
    </Modal>
  )
}
