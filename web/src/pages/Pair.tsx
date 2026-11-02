// /pair/device?user_code=… — a RomM app on a handheld asks to sign in; the person approves it here.
import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { Icon } from '../components/icons'
import { Spinner, useToast } from '../components/ui'

type Pending = { name: string; client: string; platform: string; client_version: string; expires_at: string }

export default function Pair() {
  const { toast } = useToast()
  const [params] = useSearchParams()
  const [code, setCode] = useState((params.get('user_code') || '').toUpperCase())
  const [pending, setPending] = useState<Pending | null>(null)
  const [err, setErr] = useState('')
  const [name, setName] = useState('')
  const [done, setDone] = useState<'' | 'yes' | 'no'>('')
  const [busy, setBusy] = useState(false)
  const look = async (c = code) => {
    setErr(''); setPending(null)
    if (c.replace(/-/g, '').length < 8) return
    setBusy(true)
    try { const p = await api.get<Pending>(`${V1}/pair/${encodeURIComponent(c)}`); setPending(p); setName(p.name) } catch (e) { setErr((e as Error).message) } finally { setBusy(false) }
  }
  useEffect(() => { if (code) look(code) }, []) // eslint-disable-line react-hooks/exhaustive-deps
  const decide = async (approve: boolean) => {
    setBusy(true)
    try { await api.post(`${V1}/pair`, { code, approve, name }); setDone(approve ? 'yes' : 'no') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <div className="pair-page">
      <div className="card pad pair-card">
        <Icon name="smartphone" size={30} className="pair-ico" />
        <h1>Connect a device</h1>
        {done ? <>
          <p>{done === 'yes' ? <>Done — <b>{name || pending?.name}</b> signs in by itself in a few seconds.</> : 'The device was turned away.'}</p>
          <Link to="/settings/devices" className="btn">See your devices</Link>
        </> : <>
          <p className="muted">Type the code the app shows (8 letters and digits).</p>
          <form className="row" onSubmit={e => { e.preventDefault(); look() }}>
            <input className="pair-code" value={code} onChange={e => setCode(e.target.value.toUpperCase())} placeholder="ABCD2345" maxLength={9} autoFocus aria-label="Code" />
            <button type="submit" className="btn" disabled={busy}>{busy ? <Spinner /> : 'Look'}</button>
          </form>
          {err && <p className="bad small">{err}</p>}
          {pending && <div className="pair-ask">
            <p><b>{pending.client || 'An app'}</b>{pending.platform ? ` on ${pending.platform}` : ''}{pending.client_version ? ` (${pending.client_version})` : ''} wants to use your games: browse and download them, sync saves and states, report play time.</p>
            <label className="field"><span className="field-label">Name it</span><input value={name} onChange={e => setName(e.target.value)} placeholder="Odin 2" /></label>
            <div className="row">
              <button type="button" className="btn primary" onClick={() => decide(true)} disabled={busy}><Icon name="check" size={15} />Allow</button>
              <button type="button" className="btn ghost" onClick={() => decide(false)} disabled={busy}>Turn away</button>
            </div>
          </div>}
        </>}
      </div>
    </div>
  )
}
