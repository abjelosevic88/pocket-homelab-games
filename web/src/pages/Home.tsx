import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { playtime } from '../lib/games'
import type { Home as HomeT, Pick } from '../lib/types'
import { Cover, Shelf, useAIStatus } from '../components/game'
import { Icon } from '../components/icons'
import { useOnChange } from '../components/Layout'
import { Empty, Loading, Spinner, useAsync, useToast } from '../components/ui'

export default function Home() {
  const { user } = useApp()
  const { data, reload, error } = useAsync(() => api.get<HomeT>(`${V1}/home`), [])
  useOnChange(reload)
  if (error) return <div className="callout bad"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return <Loading />
  if (data.total === 0) return <FirstRun scanning={data.scan.running} admin={user?.role === 'admin'} />
  const max = Math.max(1, ...data.week.by_day.map(d => d.seconds))
  const days = Array.from({ length: 7 }, (_, i) => { const d = new Date(); d.setDate(d.getDate() - 6 + i); return d.toISOString().slice(0, 10) })
  return (
    <div className="home">
      <div className="home-top">
        <div className="home-hello">
          <h1>{greeting()}{user?.name ? `, ${user.name.split(' ')[0]}` : ''}</h1>
          <p className="muted">{data.total} game{data.total === 1 ? '' : 's'} in the library{data.counts.playing ? ` · ${data.counts.playing} on the go` : ''}{data.counts.backlog ? ` · ${data.counts.backlog} waiting in the backlog` : ''}</p>
        </div>
        <Link to="/stats" className="week-card" title="Your play time">
          <div className="week-n"><b>{playtime(data.week.seconds)}</b><span>this week</span></div>
          <div className="week-bars" aria-hidden>
            {days.map(d => { const v = data.week.by_day.find(x => x.day === d)?.seconds || 0; return <span key={d} className={v ? 'on' : ''} style={{ height: `${Math.max(6, (v / max) * 100)}%` }} title={`${d}: ${playtime(v)}`} /> })}
          </div>
        </Link>
      </div>
      <Picks />
      <Shelf title="Continue playing" icon="gamepad" to="/library?sort=played&played=1" games={data.continue} states={data.states} />
      <Shelf title="Recently added" icon="sparkles" to="/library?sort=added" games={data.added} />
      <Shelf title="Favourites" icon="heart" to="/library?favorite=1" games={data.favorites} />
      <Shelf title="Backlog" icon="bookmark" to="/library?status=backlog" games={data.backlog} />
      <Shelf title="Play in the browser" icon="play" to="/library?browser=1" games={data.browser} />
    </div>
  )
}

function greeting() {
  const h = new Date().getHours()
  return h < 5 ? 'Late night' : h < 12 ? 'Good morning' : h < 18 ? 'Good afternoon' : 'Good evening'
}

/** "What should I play?" — three picks from the person's own library. */
function Picks() {
  const { toast } = useToast()
  const ai = useAIStatus()
  const [mood, setMood] = useState('')
  const [picks, setPicks] = useState<Pick[] | null>(null)
  const [by, setBy] = useState('')
  const [busy, setBusy] = useState(false)
  const ask = async (e?: React.FormEvent) => {
    e?.preventDefault()
    setBusy(true)
    try { const r = await api.post<{ picks: Pick[]; by: string }>(`${V1}/picks`, { mood }); setPicks(r.picks); setBy(r.by) } catch (err) { toast((err as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <section className="picks card">
      <form className="picks-head" onSubmit={ask}>
        <Icon name="sparkles" size={18} className="picks-ico" />
        <div className="grow">
          <h2>What should I play?</h2>
          <p className="muted small">{ai?.ready ? 'Tell it your mood or how much time you have — three picks from your own games.' : 'Three picks from your backlog, games you started and favourites you have not touched in a while.'}</p>
        </div>
        <div className="picks-ask">
          <input value={mood} onChange={e => setMood(e.target.value)} placeholder={ai?.ready ? 'e.g. 30 minutes, something relaxing' : 'Optional: your mood'} aria-label="Mood or time" />
          <button type="submit" className="btn primary" disabled={busy}>{busy ? <Spinner /> : <Icon name="shuffle" size={15} />}{picks ? 'Again' : 'Suggest'}</button>
        </div>
      </form>
      {picks && (picks.length === 0 ? <p className="muted small pad-x">No games to pick from yet.</p> : (
        <div className="picks-row">
          {picks.map(p => (
            <Link key={p.game.id} to={`/games/${p.game.id}`} className="pick">
              <Cover game={p.game} className="pick-cover" />
              <div className="pick-text"><b>{p.game.name}</b><span className="muted small">{p.game.platform_name}</span><p>{p.why}</p></div>
            </Link>
          ))}
        </div>
      ))}
      {picks && by && by !== 'lists' && by !== 'none' && <p className="picks-by muted small"><Icon name="sparkles" size={12} />Picked by {by === 'pocket' ? 'Pocket AI' : 'your AI'}</p>}
    </section>
  )
}

function FirstRun({ scanning, admin }: { scanning: boolean; admin: boolean }) {
  return (
    <div className="first-run">
      <Empty icon="gamepad" title={scanning ? 'Looking through the library…' : 'No games yet'}>
        {scanning ? <p className="muted">The first look can take a moment.</p> : <>
          <p className="muted">Upload games here, or put them into the library folder (one folder per system, e.g. <code>snes/</code>, <code>gba/</code>, <code>psx/</code>) and they appear within minutes.</p>
          <div className="row wrap" style={{ justifyContent: 'center' }}>
            <Link to="/upload" className="btn primary"><Icon name="upload" size={15} />Upload games</Link>
            {admin && <Link to="/settings/library" className="btn"><Icon name="folder" size={15} />Library settings</Link>}
          </div>
        </>}
      </Empty>
    </div>
  )
}
