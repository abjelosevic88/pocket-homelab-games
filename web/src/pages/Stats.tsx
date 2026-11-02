import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { fmtDate, fmtDateTime } from '../lib/format'
import { playtime, playtimeShort } from '../lib/games'
import type { Stats as StatsT } from '../lib/types'
import { Icon } from '../components/icons'
import { Empty, Loading, Segmented, useAsync } from '../components/ui'

export default function Stats() {
  const [days, setDays] = useState(30)
  const { data } = useAsync(() => api.get<StatsT>(`${V1}/stats?days=${days}`), [days])
  return (
    <div className="stats-page">
      <div className="page-head">
        <div><h1>Play time</h1><p className="muted">From the player in the browser and from connected devices.</p></div>
        <Segmented value={days} onChange={setDays} label="Range" options={[{ value: 7, label: 'Week' }, { value: 30, label: '30 days' }, { value: 90, label: '90 days' }, { value: 365, label: 'Year' }]} />
      </div>
      {!data ? <Loading /> : data.all_time.sessions === 0 ? <Empty icon="chart" title="Nothing played yet">Play a game in the browser, or connect a handheld, and your time shows up here.</Empty> : <>
        <div className="stats">
          <div className="stat"><Icon name="clock" size={16} /><span className="n">{playtime(data.seconds)}</span><span className="l">played in this range</span></div>
          <div className="stat"><Icon name="gamepad" size={16} /><span className="n">{data.games}</span><span className="l">different games</span></div>
          <div className="stat"><Icon name="calendar" size={16} /><span className="n">{data.days}<small>of {Math.min(days, 3660)}</small></span><span className="l">days with play</span></div>
          <div className="stat"><Icon name="flame" size={16} /><span className="n">{data.streak}<small>day{data.streak === 1 ? '' : 's'}</small></span><span className="l">in a row now</span></div>
          <div className="stat"><Icon name="trophy" size={16} /><span className="n">{(data.counts.finished || 0) + (data.counts.completed || 0)}</span><span className="l">games finished</span></div>
          <div className="stat"><Icon name="activity" size={16} /><span className="n">{playtimeShort(data.all_time.seconds)}</span><span className="l">all time, {data.all_time.games} games</span></div>
        </div>
        <section className="card pad"><h2 className="sec-h">Day by day</h2><DayBars data={data} days={days} /></section>
        <div className="two-col cols-3-2 grid">
          <section className="card pad">
            <h2 className="sec-h">Most played</h2>
            <ol className="rank">{data.top_games.map(t => {
              const max = data.top_games[0]?.seconds || 1
              return <li key={t.id}><Link to={`/games/${t.id}`} className="rank-row"><span className="ellipsis grow">{t.name}</span><span className="muted small">{t.platform.toUpperCase()}</span><span className="tnum">{playtimeShort(t.seconds)}</span></Link><span className="rank-bar" style={{ width: `${(t.seconds / max) * 100}%` }} /></li>
            })}</ol>
          </section>
          <section className="card pad">
            <h2 className="sec-h">By system</h2>
            <ol className="rank">{data.by_platform.map(t => {
              const max = data.by_platform[0]?.seconds || 1
              return <li key={t.id}><Link to={`/platforms/${t.platform}`} className="rank-row"><span className="ellipsis grow">{t.name}</span><span className="tnum">{playtimeShort(t.seconds)}</span></Link><span className="rank-bar" style={{ width: `${(t.seconds / max) * 100}%` }} /></li>
            })}</ol>
          </section>
        </div>
        <section className="card pad">
          <h2 className="sec-h">Lately</h2>
          <table className="table compact"><tbody>{data.recent.map(s => (
            <tr key={s.id}><td><Link to={`/games/${s.game_id}`}>{s.game_name}</Link></td><td className="muted">{s.device}</td><td className="muted">{fmtDateTime(s.started_at)}</td><td className="num">{playtime(s.seconds)}</td></tr>
          ))}</tbody></table>
        </section>
      </>}
    </div>
  )
}

function DayBars({ data, days }: { data: StatsT; days: number }) {
  const map = new Map(data.by_day.map(d => [d.day, d.seconds]))
  const list: { day: string; sec: number }[] = []
  const end = new Date(data.to + 'T12:00:00')
  for (let i = days - 1; i >= 0; i--) { const d = new Date(end); d.setDate(d.getDate() - i); const k = d.toISOString().slice(0, 10); list.push({ day: k, sec: map.get(k) || 0 }) }
  // a year is shown by week
  const bars = days > 120 ? weeks(list) : list
  const max = Math.max(1, ...bars.map(b => b.sec))
  return (
    <div className="daybars" role="img" aria-label="Play time per day">
      {bars.map(b => <span key={b.day} className={`daybar ${b.sec ? 'on' : ''}`} title={`${fmtDate(b.day)}: ${playtime(b.sec)}`}><i style={{ height: `${Math.max(2, (b.sec / max) * 100)}%` }} /></span>)}
    </div>
  )
}

function weeks(list: { day: string; sec: number }[]) {
  const out: { day: string; sec: number }[] = []
  for (let i = 0; i < list.length; i += 7) out.push({ day: list[i].day, sec: list.slice(i, i + 7).reduce((n, d) => n + d.sec, 0) })
  return out
}
