import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { CatalogPlatform, Platform } from '../lib/types'
import { size } from '../components/game'
import { Icon } from '../components/icons'
import { announceChange, useCrumb, useOnChange } from '../components/Layout'
import { Select } from '../components/Select'
import { Confirm, Empty, Loading, Modal, useAsync, useToast } from '../components/ui'
import Library from './Library'
import { uploadFile } from './Upload'

export default function Platforms() {
  const { user } = useApp()
  const [all, setAll] = useState(false)
  const { data, reload } = useAsync(() => api.get<Platform[]>(`${V1}/platforms${all ? '?all=1' : ''}`), [all])
  const [add, setAdd] = useState(false)
  useOnChange(reload)
  if (!data) return <Loading />
  return (
    <div className="platforms">
      <div className="page-head">
        <div><h1>Systems</h1><p className="muted">{data.filter(p => p.games).length} systems with games. Each one is a folder in the library.</p></div>
        <div className="row">
          <button type="button" className="chip" onClick={() => setAll(!all)}><Icon name={all ? 'eye' : 'folder'} size={13} />{all ? 'Only systems with games' : 'Show empty folders too'}</button>
          {user?.role === 'admin' && <button type="button" className="btn" onClick={() => setAdd(true)}><Icon name="plus" size={15} />Add a system</button>}
        </div>
      </div>
      {data.length === 0 ? <Empty icon="cpu" title="No systems yet">Upload a game and its system appears here.</Empty> : (
        <div className="plat-grid">{data.map(p => <PlatformCard key={p.id} p={p} />)}</div>
      )}
      {add && <AddPlatform onClose={() => setAdd(false)} onDone={() => { setAdd(false); reload(); announceChange() }} />}
    </div>
  )
}

function PlatformCard({ p }: { p: Platform }) {
  return (
    <Link to={`/platforms/${p.slug}`} className="plat-card">
      <div className={`plat-mosaic n${Math.min(p.covers.length, 4)}`}>
        {p.covers.length ? p.covers.slice(0, 4).map(id => <img key={id} src={`${V1}/games/${id}/cover`} alt="" loading="lazy" />) : <div className="plat-ph"><Icon name="cpu" size={28} /></div>}
      </div>
      <div className="plat-meta">
        <b className="ellipsis">{p.custom_name || p.name}</b>
        <span className="muted small">{p.games} game{p.games === 1 ? '' : 's'} · {size(p.size_bytes)}{p.maker && p.maker !== 'Various' ? ` · ${p.maker}` : ''}</span>
        <span className="plat-badges">
          {p.browser ? <span className="badge ok"><Icon name="play" size={11} />Plays here</span> : <span className="badge">Download to play</span>}
          {p.bios && (p.needs_bios || p.bios.files > 0 || !p.bios.ok) && <span className={`badge ${p.bios.ok ? 'ok' : 'warn'}`}>{p.bios.ok ? 'BIOS ready' : 'BIOS missing'}</span>}
          {!p.known && <span className="badge">Unknown system</span>}
        </span>
      </div>
    </Link>
  )
}

function AddPlatform({ onClose, onDone }: { onClose: () => void; onDone: () => void }) {
  const { toast } = useToast()
  const { data } = useAsync(() => api.get<CatalogPlatform[]>(`${V1}/catalog`), [])
  const [slug, setSlug] = useState('')
  const save = async () => {
    try { await api.post(`${V1}/platforms`, { slug }); toast('Folder made'); onDone() } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <Modal title="Add a system" onClose={onClose} footer={<><button type="button" className="btn ghost" onClick={onClose}>Cancel</button><button type="button" className="btn primary" disabled={!slug} onClick={save}>Make the folder</button></>}>
      <p className="muted">Makes the system’s folder in the library, so you can upload to it or copy games into it.</p>
      {!data ? <Loading /> : <Select ariaLabel="System" value={slug} onChange={setSlug} placeholder="Choose a system…" options={data.map(c => ({ value: c.slug, label: c.name, hint: c.slug, group: c.maker }))} />}
    </Modal>
  )
}

export function PlatformPage() {
  const { slug } = useParams()
  const { user } = useApp()
  const { toast } = useToast()
  const { data, reload, error } = useAsync(() => api.get<Platform>(`${V1}/platforms/${slug}`), [slug])
  const [rename, setRename] = useState(false)
  const [del, setDel] = useState<number | null>(null)
  const [busy, setBusy] = useState('')
  useCrumb(data ? data.custom_name || data.name : undefined)
  if (error) return <Empty icon="alert" title="No such system">{error}</Empty>
  if (!data) return <Loading />
  const admin = user?.role === 'admin'
  const uploadBios = async (files: FileList) => {
    for (const f of Array.from(files)) {
      setBusy(f.name)
      try { await uploadFile(f, { kind: 'firmware', platform: data.slug, rel: f.name }, () => {}) ; toast(`${f.name} added`) } catch (e) { toast(`${f.name}: ${(e as Error).message}`, 'err') }
    }
    setBusy('')
    reload()
  }
  return (
    <div className="platform-page">
      <div className="page-head">
        <div>
          <h1>{data.custom_name || data.name}</h1>
          <p className="muted">{data.maker && data.maker !== 'Various' ? `${data.maker}${data.year ? `, ${data.year}` : ''} · ` : ''}{data.games} game{data.games === 1 ? '' : 's'} · {size(data.size_bytes)} · folder <code>{data.slug}/</code></p>
        </div>
        <div className="row">
          <Link to={`/upload?platform=${data.slug}`} className="btn"><Icon name="upload" size={15} />Upload games</Link>
          {admin && <button type="button" className="btn ghost" onClick={() => setRename(true)}><Icon name="edit" size={15} />Rename</button>}
        </div>
      </div>
      <div className="plat-info">
        <div className={`callout ${data.browser ? 'info' : ''}`}><Icon name={data.browser ? 'play' : 'download'} size={15} /><span>
          {data.browser ? <>These games play right here in the browser{data.cores.length > 1 ? ` (emulators: ${data.cores.join(', ').replace(/_/g, ' ')})` : ''}. Saves and states are kept on the server.</>
            : <>Browsers can’t run {data.short} games yet. Download them, or use a RomM app on a handheld (Settings → Devices).</>}
          {data.extensions.length > 0 && <span className="muted"> Files: {data.extensions.map(e => '.' + e).join(' ')}</span>}
        </span></div>
      </div>
      {data.bios && <section className="card pad bios-card">
        <div className="sec-head">
          <h2 className="sec-h"><Icon name="cpu" size={16} />BIOS</h2>
          <span className={`badge ${data.bios.ok ? (data.bios.files || data.needs_bios ? 'ok' : '') : 'warn'}`}>{!data.bios.ok ? 'Missing' : data.bios.files || data.needs_bios ? 'Ready' : 'Optional'}</span>
          <span className="spacer" />
          {admin && <label className="btn sm"><Icon name="upload" size={14} />{busy ? `Uploading ${busy}…` : 'Add BIOS files'}<input type="file" multiple hidden onChange={e => { const fs = e.target.files; if (fs?.length) uploadBios(fs); e.target.value = '' }} /></label>}
        </div>
        <p className="muted small">The system’s start-up program, dumped from a console you own. Games {data.needs_bios ? 'need' : 'can use'} one of these. A zip of several BIOS files is unpacked.</p>
        <table className="table compact"><tbody>{data.bios.items.map(b => (
          <tr key={b.file}>
            <td><span className="bios-cell"><Icon name={b.present ? 'check-circle' : 'circle'} size={14} className={b.present ? 'ok' : 'muted'} /><code>{b.any ? 'any dump' : b.file}</code>{b.required && <span className="badge sm">needed</span>}</span></td>
            <td className="muted small">{b.note}</td>
            <td className="small">{b.present ? <span className={b.verified ? 'ok' : ''}>{b.verified ? 'verified' : b.have}</span> : <span className="muted">not here</span>}</td>
          </tr>
        ))}</tbody></table>
        {(data.firmware_files?.length || 0) > 0 && <>
          <h3 className="sub-h">Files in <code>bios/{data.slug}/</code></h3>
          <ul className="file-list">{data.firmware_files!.map(f => (
            <li key={f.id} className="file-row">
              <Icon name="cpu" size={14} /><a className="ellipsis grow" href={`${V1}/firmware/${f.id}/file`}>{f.file_name}</a>
              <span className="muted small mono" title="MD5">{f.md5.slice(0, 8)}</span><span className="muted small tnum">{size(f.size_bytes)}</span>
              {admin && <button type="button" className="icon-btn" title="Remove" onClick={() => setDel(f.id)}><Icon name="trash" size={14} /></button>}
            </li>
          ))}</ul>
        </>}
      </section>}
      <Library fixed={{ platform: data.slug }} />
      {rename && <RenamePlatform p={data} onClose={() => setRename(false)} onDone={() => { setRename(false); reload(); announceChange() }} />}
      {del && <Confirm title="Remove this BIOS file?" message="It moves to the library’s trash folder." confirmLabel="Remove" onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/firmware/${del}`); setDel(null); reload() }} />}
    </div>
  )
}

function RenamePlatform({ p, onClose, onDone }: { p: Platform; onClose: () => void; onDone: () => void }) {
  const { toast } = useToast()
  const [name, setName] = useState(p.custom_name || p.name)
  const save = async () => { try { await api.put(`${V1}/platforms/${p.slug}`, { name }); onDone() } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Modal title="Rename the system" size="sm" onClose={onClose} footer={<><button type="button" className="btn ghost" onClick={onClose}>Cancel</button><button type="button" className="btn primary" onClick={save}>Save</button></>}>
      <input value={name} onChange={e => setName(e.target.value)} autoFocus />
      <p className="muted small mt-sm">Only the shown name changes; the folder stays <code>{p.slug}/</code>. Empty = “{p.name}”.</p>
    </Modal>
  )
}
