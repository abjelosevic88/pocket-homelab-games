import { useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, ApiError, V1 } from '../lib/api'
import { COMPANION_EXT, ext, SHEET_EXT, stem } from '../lib/games'
import type { CatalogPlatform, Download, Upload as UploadT } from '../lib/types'
import { size } from '../components/game'
import { Icon } from '../components/icons'
import { announceChange } from '../components/Layout'
import { Select } from '../components/Select'
import { useAsync, useToast } from '../components/ui'

const CHUNK = 16 << 20

/** Sends one file in resumable chunks; a broken connection picks up where the server stopped. */
export async function uploadFile(file: File, target: { kind: 'game' | 'firmware'; platform: string; rel: string }, onProgress: (sent: number) => void, scan = true): Promise<UploadT> {
  let up = await api.post<UploadT>(`${V1}/uploads`, { kind: target.kind, platform: target.platform, name: target.rel, size: file.size })
  let sent = up.received
  let tries = 0
  while (sent < file.size) {
    const body = file.slice(sent, Math.min(file.size, sent + CHUNK))
    try {
      const r = await fetch(`${V1}/uploads/${up.id}?offset=${sent}`, { method: 'PUT', body, credentials: 'same-origin', headers: { 'Content-Type': 'application/octet-stream' } })
      const j = await r.json().catch(() => ({}))
      if (typeof j.received === 'number') sent = j.received
      if (!r.ok && r.status !== 409) throw new ApiError(r.status, j.error || r.statusText)
      tries = 0
      onProgress(sent)
    } catch (e) {
      if (++tries > 6) throw e
      await new Promise(res => setTimeout(res, 1000 * 2 ** Math.min(tries, 4)))
      try { up = await api.get<UploadT>(`${V1}/uploads/${up.id}`); sent = up.received } catch { /* keep trying */ }
    }
  }
  return api.post<UploadT>(`${V1}/uploads/${up.id}/finish${scan ? '' : '?scan=0'}`)
}

type Item = { file: File; rel: string }
type Group = { key: string; name: string; items: Item[]; platform: string; choices: string[]; state: 'waiting' | 'sending' | 'done' | 'failed'; sent: number; error: string; gameId: number | null }

export default function Upload() {
  const { toast } = useToast()
  const [params] = useSearchParams()
  const { data: catalog } = useAsync(() => api.get<CatalogPlatform[]>(`${V1}/catalog`), [])
  const [groups, setGroups] = useState<Group[]>([])
  const [running, setRunning] = useState(false)
  const [over, setOver] = useState(false)
  const lastPlatform = useRef(params.get('platform') || localStorage.getItem('pgm-upload-platform') || '')
  const groupsRef = useRef(groups)
  groupsRef.current = groups

  useEffect(() => {
    const warn = (e: BeforeUnloadEvent) => { if (running) { e.preventDefault(); e.returnValue = '' } }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [running])

  const bySlug = new Map((catalog || []).map(c => [c.slug, c]))
  const guess = (name: string): string[] => (catalog || []).filter(c => c.extensions.includes(ext(name))).map(c => c.slug)

  /** Files → games: a folder is one game; loose cue/bin/sbi sets are kept together. */
  const add = (items: Item[]) => {
    const out: Group[] = []
    const folders = new Map<string, Item[]>()
    const loose: Item[] = []
    for (const it of items) {
      const parts = it.rel.split('/')
      if (parts.length > 1) {
        // a dropped system folder ("psx/…") holds games: one level down
        if (bySlug.has(parts[0].toLowerCase()) && parts.length > 2) {
          const key = parts[0] + '/' + parts[1]
          folders.set(key, [...(folders.get(key) || []), { file: it.file, rel: parts.slice(1).join('/') }])
        } else if (bySlug.has(parts[0].toLowerCase()) && parts.length === 2) {
          loose.push({ file: it.file, rel: parts[1], ...{ platformHint: parts[0].toLowerCase() } } as Item)
        } else folders.set(parts[0], [...(folders.get(parts[0]) || []), it])
      } else loose.push(it)
    }
    for (const [key, list] of folders) {
      const sys = key.includes('/') ? key.split('/')[0].toLowerCase() : ''
      const main = list.find(i => SHEET_EXT.includes(ext(i.file.name))) || list.reduce((a, b) => (a.file.size > b.file.size ? a : b))
      const choices = sys ? [sys] : guess(main.file.name)
      out.push(newGroup(key.split('/').pop()!, list.map(i => ({ file: i.file, rel: sys ? i.rel : i.rel })), choices))
    }
    const stems = new Map<string, Item[]>()
    for (const it of loose) { const k = stem(it.file.name).toLowerCase(); stems.set(k, [...(stems.get(k) || []), it]) }
    for (const list of stems.values()) {
      const hint = (list[0] as Item & { platformHint?: string }).platformHint
      const sheet = list.find(i => SHEET_EXT.includes(ext(i.file.name)))
      const companions = list.filter(i => COMPANION_EXT.includes(ext(i.file.name)))
      if (list.length > 1 && (sheet || companions.length === list.length - 1)) {
        const main = sheet || list.find(i => !COMPANION_EXT.includes(ext(i.file.name)))!
        const folder = stem(main.file.name)
        out.push(newGroup(folder, list.map(i => ({ file: i.file, rel: `${folder}/${i.file.name}` })), hint ? [hint] : guess(main.file.name)))
      } else {
        for (const i of list) out.push(newGroup(i.file.name, [{ file: i.file, rel: i.file.name }], hint ? [hint] : guess(i.file.name)))
      }
    }
    setGroups(g => [...g, ...out])
  }
  const newGroup = (name: string, items: Item[], choices: string[]): Group => ({
    key: `${name}-${Math.random().toString(36).slice(2)}`, name, items, choices,
    platform: choices.length === 1 ? choices[0] : choices.includes(lastPlatform.current) ? lastPlatform.current : (choices.length === 0 && lastPlatform.current ? lastPlatform.current : ''),
    state: 'waiting', sent: 0, error: '', gameId: null,
  })
  const patch = (key: string, p: Partial<Group>) => setGroups(gs => gs.map(g => (g.key === key ? { ...g, ...p } : g)))

  const start = async () => {
    setRunning(true)
    for (const g of groupsRef.current) {
      if (g.state !== 'waiting' && g.state !== 'failed') continue
      if (!g.platform) continue
      lastPlatform.current = g.platform
      localStorage.setItem('pgm-upload-platform', g.platform)
      patch(g.key, { state: 'sending', error: '', sent: 0 })
      let base = 0
      try {
        let last: UploadT | null = null
        for (let i = 0; i < g.items.length; i++) {
          const it = g.items[i]
          last = await uploadFile(it.file, { kind: 'game', platform: g.platform, rel: it.rel }, sent => patch(g.key, { sent: base + sent }), i === g.items.length - 1)
          base += it.file.size
        }
        patch(g.key, { state: 'done', sent: base, gameId: last?.game_id || null })
        announceChange()
      } catch (e) {
        patch(g.key, { state: 'failed', error: (e as Error).message })
      }
    }
    setRunning(false)
    const failed = groupsRef.current.filter(g => g.state === 'failed').length
    toast(failed ? `${failed} could not be uploaded` : 'Uploaded', failed ? 'err' : 'ok')
  }

  // drag and drop, folders too
  const onDrop = async (e: React.DragEvent) => {
    e.preventDefault()
    setOver(false)
    const entries = Array.from(e.dataTransfer.items).map(i => i.webkitGetAsEntry?.()).filter(Boolean) as FileSystemEntry[]
    if (!entries.length) { add(Array.from(e.dataTransfer.files).map(f => ({ file: f, rel: f.name }))); return }
    const items: Item[] = []
    const walk = async (entry: FileSystemEntry, path: string): Promise<void> => {
      if (entry.isFile) {
        const f = await new Promise<File>((res, rej) => (entry as FileSystemFileEntry).file(res, rej))
        if (!f.name.startsWith('.')) items.push({ file: f, rel: path + f.name })
      } else if (entry.isDirectory) {
        const reader = (entry as FileSystemDirectoryEntry).createReader()
        let batch: FileSystemEntry[]
        do {
          batch = await new Promise<FileSystemEntry[]>((res, rej) => reader.readEntries(res, rej))
          for (const c of batch) await walk(c, path + entry.name + '/')
        } while (batch.length)
      }
    }
    for (const en of entries) await walk(en, '')
    add(items)
  }

  // ---- links: the server fetches the file ----
  const [link, setLink] = useState('')
  const [downloads, setDownloads] = useState<Download[]>([])
  const loadDownloads = () => api.get<Download[]>(`${V1}/downloads`).then(setDownloads).catch(() => {})
  useEffect(() => { loadDownloads() }, [])
  const active = downloads.some(d => d.status === 'queued' || d.status === 'downloading')
  const prevDone = useRef(0)
  useEffect(() => {
    const done = downloads.filter(d => d.status === 'done').length
    if (done > prevDone.current && prevDone.current) announceChange()
    prevDone.current = done
    if (!active) return
    const t = window.setTimeout(loadDownloads, 1500)
    return () => window.clearTimeout(t)
  }, [downloads]) // eslint-disable-line react-hooks/exhaustive-deps
  const addLinks = async (text: string) => {
    const urls = text.split(/\s+/).map(u => u.trim()).filter(u => /^https?:\/\/\S+$/i.test(u))
    if (!urls.length) { toast('Paste a link that starts with http:// or https://', 'err'); return }
    for (const url of urls) {
      try { await api.post(`${V1}/downloads`, { url, platform: params.get('platform') || '' }) } catch (e) { toast((e as Error).message, 'err') }
    }
    setLink('')
    toast(urls.length === 1 ? 'Downloading on the server' : `${urls.length} links are downloading on the server`)
    loadDownloads()
  }
  // ⌘V anywhere on the page (outside a text field) adds the link
  useEffect(() => {
    const h = (e: ClipboardEvent) => {
      const t = e.target as HTMLElement
      if (t.closest('input, textarea, [contenteditable="true"]')) return
      const text = e.clipboardData?.getData('text') || ''
      if (/https?:\/\//i.test(text)) { e.preventDefault(); addLinks(text) }
    }
    window.addEventListener('paste', h)
    return () => window.removeEventListener('paste', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps
  const setSystem = async (d: Download, platform: string) => { try { await api.put(`${V1}/downloads/${d.id}`, { platform }); loadDownloads() } catch (e) { toast((e as Error).message, 'err') } }
  const retry = async (d: Download) => { try { await api.put(`${V1}/downloads/${d.id}`, { retry: true }); loadDownloads() } catch (e) { toast((e as Error).message, 'err') } }
  const remove = async (d: Download) => { try { await api.del(`${V1}/downloads/${d.id}`); loadDownloads() } catch (e) { toast((e as Error).message, 'err') } }

  const waiting = groups.filter(g => g.state === 'waiting' || g.state === 'failed')
  const needSystem = waiting.filter(g => !g.platform).length
  const total = groups.reduce((n, g) => n + g.items.reduce((m, i) => m + i.file.size, 0), 0)
  const sent = groups.reduce((n, g) => n + (g.state === 'done' ? g.items.reduce((m, i) => m + i.file.size, 0) : g.sent), 0)
  const options = (catalog || []).map(c => ({ value: c.slug, label: c.name, hint: c.short, group: c.maker }))
  return (
    <div className="upload-page">
      <div className="page-head"><div><h1>Upload games</h1><p className="muted">Files go straight into the library, into the system’s folder. Big discs are fine: a broken connection continues where it stopped.</p></div></div>
      <form className="link-bar" onSubmit={e => { e.preventDefault(); addLinks(link) }}>
        <Icon name="link" size={16} className="muted" />
        <input value={link} onChange={e => setLink(e.target.value)} onPaste={e => { const t = e.clipboardData.getData('text'); if (/^https?:\/\/\S+$/i.test(t.trim()) && !link) { e.preventDefault(); addLinks(t) } }}
          placeholder="Paste a link to a game file — the server downloads it (Google Drive, Dropbox, OneDrive and archive.org links work too)" aria-label="Link to a game file" />
        <button type="submit" className="btn primary" disabled={!link.trim()}><Icon name="download" size={15} />Download</button>
      </form>
      {downloads.length > 0 && <ul className="upload-list">{downloads.map(d => {
        const pct = d.size_bytes ? Math.floor((d.received / d.size_bytes) * 100) : 0
        const name = d.file_name || d.url.replace(/^https?:\/\//, '')
        const opts = d.choices.length ? [...options.filter(o => d.choices.includes(o.value)).map(o => ({ ...o, group: 'Fits this file' })), ...options.filter(o => !d.choices.includes(o.value))] : options
        return (
          <li key={d.id} className={`upload-row s-${d.status === 'done' ? 'done' : d.status === 'failed' ? 'failed' : 'sending'}`}>
            <Icon name="link" size={18} className="muted" />
            <div className="grow upload-main">
              <div className="row"><b className="ellipsis" title={d.url}>{name}</b><span className="muted small tnum">{d.size_bytes ? `${size(d.received)} of ${size(d.size_bytes)}` : d.received ? size(d.received) : ''}</span></div>
              {d.status === 'waiting' ? (
                <div className="row wrap upload-opts">
                  <Select ariaLabel="System" value="" onChange={v => setSystem(d, v)} placeholder={d.choices.length > 1 ? `Which system? (${d.choices.length} fit)` : 'Choose the system…'} options={opts} />
                  <span className="muted small">Downloaded — which system is it for?</span>
                </div>
              ) : d.status === 'failed' ? <span className="bad small">{d.error}</span>
                : d.status === 'done' ? <span className="muted small">In the library</span>
                  : <div className="progress"><span style={{ width: `${d.status === 'queued' ? 0 : d.size_bytes ? pct : 100}%` }} className={!d.size_bytes && d.status === 'downloading' ? 'indeterminate' : ''} /></div>}
            </div>
            {d.status === 'done' && d.game_id ? <Link to={`/games/${d.game_id}`} className="btn sm">Open</Link>
              : d.status === 'failed' ? <button type="button" className="btn sm" onClick={() => retry(d)}>Try again</button>
                : (d.status === 'downloading' || d.status === 'queued') ? <span className="muted small tnum">{d.status === 'queued' ? 'waiting' : d.size_bytes ? `${pct}%` : ''}</span> : null}
            <button type="button" className="icon-btn" title={d.status === 'done' ? 'Remove from the list' : 'Cancel'} onClick={() => remove(d)}><Icon name="x" size={15} /></button>
          </li>
        )
      })}</ul>}
      <div className={`dropzone ${over ? 'over' : ''}`} onDragOver={e => { e.preventDefault(); setOver(true) }} onDragLeave={() => setOver(false)} onDrop={onDrop}>
        <Icon name="upload" size={30} />
        <b>Drop games here</b>
        <span className="muted small">Single files (.gba, .sfc, .chd, .iso, .zip…), a game’s folder (discs, cue/bin) or a whole system folder (<code>psx/</code>, <code>snes/</code>…)</span>
        <span className="row wrap" style={{ justifyContent: 'center' }}>
          <label className="btn primary"><Icon name="note" size={15} />Choose files
            <input type="file" hidden multiple onChange={e => { const fs = Array.from(e.target.files || []); e.target.value = ''; add(fs.map(f => ({ file: f, rel: f.name }))) }} />
          </label>
          <label className="btn"><Icon name="folder" size={15} />Choose a folder
            <input type="file" hidden multiple {...{ webkitdirectory: '' }} onChange={e => { const fs = Array.from(e.target.files || []); e.target.value = ''; add(fs.map(f => ({ file: f, rel: (f as File & { webkitRelativePath: string }).webkitRelativePath || f.name }))) }} />
          </label>
        </span>
      </div>

      {groups.length > 0 && <>
        <div className="upload-bar">
          <span className="grow">{groups.length} game{groups.length === 1 ? '' : 's'} · {size(total)}{running && ` · ${Math.floor((sent / Math.max(total, 1)) * 100)}%`}</span>
          {needSystem > 0 && <span className="warn-text small"><Icon name="alert" size={13} /> Choose the system for {needSystem}</span>}
          <button type="button" className="btn ghost" disabled={running} onClick={() => setGroups(gs => gs.filter(g => g.state !== 'done'))}>Clear finished</button>
          <button type="button" className="btn primary" disabled={running || waiting.length === 0 || needSystem === waiting.length} onClick={start}><Icon name="upload" size={15} />{running ? 'Uploading…' : `Upload ${waiting.length - needSystem}`}</button>
        </div>
        <ul className="upload-list">{groups.map(g => {
          const bytes = g.items.reduce((m, i) => m + i.file.size, 0)
          const pct = g.state === 'done' ? 100 : Math.floor((g.sent / Math.max(bytes, 1)) * 100)
          const p = bySlug.get(g.platform)
          return (
            <li key={g.key} className={`upload-row s-${g.state}`}>
              <Icon name={g.items.length > 1 ? 'folder' : 'gamepad'} size={18} className="muted" />
              <div className="grow upload-main">
                <div className="row"><b className="ellipsis">{g.name}</b><span className="muted small tnum">{g.items.length > 1 ? `${g.items.length} files · ` : ''}{size(bytes)}</span></div>
                {g.state === 'waiting' || g.state === 'failed' ? (
                  <div className="row wrap upload-opts">
                    <Select ariaLabel="System" value={g.platform} onChange={v => patch(g.key, { platform: v })} placeholder={g.choices.length > 1 ? `Which system? (${g.choices.length} fit)` : 'Choose the system…'}
                      options={g.choices.length > 1 ? [...options.filter(o => g.choices.includes(o.value)).map(o => ({ ...o, group: 'Fits this file' })), ...options.filter(o => !g.choices.includes(o.value))] : options} />
                    {p && !p.browser && <span className="muted small">{p.short}: download to play</span>}
                    {g.error && <span className="bad small">{g.error}</span>}
                  </div>
                ) : (
                  <div className="progress"><span style={{ width: `${pct}%` }} /></div>
                )}
              </div>
              {g.state === 'done' ? (g.gameId ? <Link to={`/games/${g.gameId}`} className="btn sm">Open</Link> : <span className="ok small"><Icon name="check" size={14} />Done</span>)
                : g.state === 'sending' ? <span className="muted small tnum">{pct}%</span>
                  : <button type="button" className="icon-btn" title="Remove" onClick={() => setGroups(gs => gs.filter(x => x.key !== g.key))} disabled={running}><Icon name="x" size={15} /></button>}
            </li>
          )
        })}</ul>
      </>}

      <section className="card pad upload-help">
        <h2 className="sec-h">Good to know</h2>
        <ul className="set-list">
          <li>A zipped disc game (PS1, Saturn, Sega CD…) is unpacked into its own folder; zipped cartridge games stay zipped and play as they are.</li>
          <li>Several discs of one game: put them in one folder and upload the folder — an <code>.m3u</code> playlist is made for devices.</li>
          <li>PS1 games with LibCrypt protection need their <code>.sbi</code> file next to the disc; drop both together.</li>
          <li>BIOS files go on the system’s page (Systems → the system → Add BIOS files).</li>
          <li>A link is downloaded by the server, so you can close the page; a dropped connection picks up where it stopped. Copy a link and press ⌘V (Ctrl+V) anywhere on this page.</li>
          <li>Files can also be copied straight into the library folder; they show up at the next look (every few minutes).</li>
        </ul>
      </section>
    </div>
  )
}
