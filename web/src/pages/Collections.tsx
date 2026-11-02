import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Collection } from '../lib/types'
import { Icon } from '../components/icons'
import { announceChange, useCrumb, useOnChange } from '../components/Layout'
import { Confirm, Empty, Field, Loading, Modal, useAsync, useToast } from '../components/ui'
import Library from './Library'

export default function Collections() {
  const { data, reload } = useAsync(() => api.get<Collection[]>(`${V1}/collections`), [])
  const [params, setParams] = useSearchParams()
  const [edit, setEdit] = useState<Partial<Collection> | null>(null)
  const navigate = useNavigate()
  useOnChange(reload)
  useEffect(() => { if (params.get('new')) { setEdit({}); setParams({}, { replace: true }) } }, [params]) // eslint-disable-line react-hooks/exhaustive-deps
  if (!data) return <Loading />
  return (
    <div>
      <div className="page-head">
        <div><h1>Collections</h1><p className="muted">Your own lists of games. Only you see them; they show up in RomM apps too.</p></div>
        <button type="button" className="btn primary" onClick={() => setEdit({})}><Icon name="plus" size={15} />New collection</button>
      </div>
      {data.length === 0 ? <Empty icon="layers" title="No collections yet">Group games your way: “Couch co-op”, “Finish this year”, “Horror nights”. Add games from a game’s page.</Empty> : (
        <div className="plat-grid">{data.map(c => (
          <Link key={c.id} to={`/collections/${c.id}`} className="plat-card">
            <div className={`plat-mosaic n${Math.min(c.covers.length, 4)}`}>
              {c.covers.length ? c.covers.map(id => <img key={id} src={`${V1}/games/${id}/cover`} alt="" loading="lazy" />) : <div className="plat-ph"><Icon name="layers" size={28} /></div>}
            </div>
            <div className="plat-meta"><b className="ellipsis">{c.name}</b><span className="muted small">{c.games} game{c.games === 1 ? '' : 's'}{c.description ? ` · ${c.description}` : ''}</span></div>
          </Link>
        ))}</div>
      )}
      {edit && <EditCollection c={edit} onClose={() => setEdit(null)} onSaved={c => { setEdit(null); reload(); announceChange(); navigate(`/collections/${c.id}`) }} />}
    </div>
  )
}

function EditCollection({ c, onClose, onSaved }: { c: Partial<Collection>; onClose: () => void; onSaved: (c: Collection) => void }) {
  const { toast } = useToast()
  const [name, setName] = useState(c.name || '')
  const [desc, setDesc] = useState(c.description || '')
  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    try { onSaved(c.id ? await api.put<Collection>(`${V1}/collections/${c.id}`, { name, description: desc }) : await api.post<Collection>(`${V1}/collections`, { name, description: desc })) } catch (err) { toast((err as Error).message, 'err') }
  }
  return (
    <Modal title={c.id ? 'Edit the collection' : 'New collection'} size="sm" onClose={onClose} footer={<><button type="button" className="btn ghost" onClick={onClose}>Cancel</button><button type="submit" form="col-form" className="btn primary" disabled={!name.trim()}>Save</button></>}>
      <form id="col-form" onSubmit={save} className="stack">
        <Field label="Name"><input value={name} onChange={e => setName(e.target.value)} autoFocus placeholder="Couch co-op" /></Field>
        <Field label="Description (optional)"><input value={desc} onChange={e => setDesc(e.target.value)} /></Field>
      </form>
    </Modal>
  )
}

export function CollectionPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { data, reload, error } = useAsync(() => api.get<Collection>(`${V1}/collections/${id}`), [id])
  const [edit, setEdit] = useState(false)
  const [del, setDel] = useState(false)
  useCrumb(data?.name)
  if (error) return <Empty icon="alert" title="No such collection">{error}</Empty>
  if (!data) return <Loading />
  return (
    <div>
      <div className="page-head">
        <div><h1>{data.name}</h1><p className="muted">{data.description || `${data.games} game${data.games === 1 ? '' : 's'}`}</p></div>
        <div className="row">
          <button type="button" className="btn ghost" onClick={() => setEdit(true)}><Icon name="edit" size={15} />Edit</button>
          <button type="button" className="btn ghost danger-text" onClick={() => setDel(true)}><Icon name="trash" size={15} />Delete</button>
        </div>
      </div>
      {data.games === 0 ? <Empty icon="layers" title="Empty for now">Open a game and use the collections button (<Icon name="layers" size={13} />) to add it here.</Empty> : <Library fixed={{ collection: String(data.id) }} />}
      {edit && <EditCollection c={data} onClose={() => setEdit(false)} onSaved={() => { setEdit(false); reload(); announceChange() }} />}
      {del && <Confirm title={`Delete “${data.name}”?`} message="Only the list goes; the games stay in the library." confirmLabel="Delete" onCancel={() => setDel(false)} onConfirm={async () => { await api.del(`${V1}/collections/${data.id}`); announceChange(); navigate('/collections') }} />}
    </div>
  )
}
