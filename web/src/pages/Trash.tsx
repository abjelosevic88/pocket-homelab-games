import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { fmtDateTime } from '../lib/format'
import type { Game } from '../lib/types'
import { Cover, size } from '../components/game'
import { Icon } from '../components/icons'
import { announceChange } from '../components/Layout'
import { Confirm, Empty, Loading, useAsync, useToast } from '../components/ui'

export default function Trash() {
  const { toast } = useToast()
  const { data, reload } = useAsync(() => api.get<{ trash: Game[]; missing: Game[]; days: number }>(`${V1}/trash`), [])
  const [purge, setPurge] = useState<Game | null>(null)
  if (!data) return <Loading />
  const restore = async (g: Game) => { try { await api.post(`${V1}/games/${g.id}/restore`); toast(`${g.name} is back`); reload(); announceChange() } catch (e) { toast((e as Error).message, 'err') } }
  const row = (g: Game, kind: 'trash' | 'missing') => (
    <li key={g.id} className="trash-row">
      <Cover game={g} className="trash-cover" />
      <div className="grow">
        <Link to={`/games/${g.id}`}><b>{g.name}</b></Link>
        <div className="muted small">{g.platform_name} · {size(g.size_bytes)} · {kind === 'trash' ? `deleted ${fmtDateTime(g.trashed_at)}` : `files gone since ${fmtDateTime(g.missing_since)}`}</div>
      </div>
      {g.can_edit && kind === 'trash' && <button type="button" className="btn sm" onClick={() => restore(g)}><Icon name="undo" size={14} />Restore</button>}
      {g.can_edit && <button type="button" className="btn sm ghost danger-text" onClick={() => setPurge(g)}>{kind === 'trash' ? 'Delete for good' : 'Forget it'}</button>}
    </li>
  )
  return (
    <div>
      <div className="page-head"><div><h1>Trash</h1><p className="muted">Deleted games wait here {data.days ? `for ${data.days} days` : ''} before their files are removed for good.</p></div></div>
      {data.trash.length === 0 && data.missing.length === 0 ? <Empty icon="trash" title="Nothing here">Deleted games and games whose files disappeared show up here.</Empty> : <>
        {data.trash.length > 0 && <section className="card pad"><h2 className="sec-h">Deleted</h2><ul className="trash-list">{data.trash.map(g => row(g, 'trash'))}</ul></section>}
        {data.missing.length > 0 && <section className="card pad"><h2 className="sec-h">Files no longer in the library</h2><p className="muted small">Moved or removed outside the app. If the files come back, the games return with their play time and saves.</p><ul className="trash-list">{data.missing.map(g => row(g, 'missing'))}</ul></section>}
      </>}
      {purge && <Confirm title={`Remove “${purge.name}” for good?`} confirmLabel="Remove for good"
        message="The files are deleted from the disk, and everyone’s play time, notes, saves and states for this game go with them. This cannot be undone."
        onCancel={() => setPurge(null)} onConfirm={async () => { try { await api.del(`${V1}/games/${purge.id}/purge`); setPurge(null); reload(); announceChange() } catch (e) { toast((e as Error).message, 'err') } }} />}
    </div>
  )
}
