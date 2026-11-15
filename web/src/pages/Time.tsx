// Time spent playing and recording: the last four weeks, sittings, and
// where it goes on to (Pocket Guitar or Pocket Time).
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { TimeReport } from '../lib/types'
import { useCrumb } from '../components/Layout'
import { Empty, Loading } from '../components/ui'
import { Icon } from '../components/icons'
import { Heatmap } from '../components/Chart'
import { fmtDate, fmtISOTime } from '../lib/format'

export default function Time() {
  const { settings } = useApp()
  const [r, setR] = useState<TimeReport | null>(null)
  useCrumb(undefined)
  useEffect(() => { api.get<TimeReport>(`${V1}/time`).then(setR).catch(() => {}) }, [])
  if (!r) return <Loading />
  const to = settings?.practice_to || 'off'
  const goes = to === 'guitar' ? 'Pocket Guitar' : to === 'time' ? 'Pocket Time' : ''
  return (
    <div className="page">
      <div className="stats-row">
        <div className="stat"><span className="stat-l">4 weeks</span><span className="stat-v">{Math.floor(r.minutes / 60)} h {r.minutes % 60} min</span></div>
        <div className="stat"><span className="stat-l">Days played</span><span className="stat-v">{r.days.filter(d => d.minutes > 0).length}</span></div>
        <div className="stat"><span className="stat-l">In a row</span><span className="stat-v">{r.streak} {r.streak === 1 ? 'day' : 'days'}</span></div>
      </div>
      <div className="callout info"><Icon name="info" size={15} /><div>Time counts while a song plays or records. {goes ? <>It goes on to <b>{goes}</b> as practice.</> : <>It can go on to Pocket Guitar or Pocket Time as practice: <Link to="/settings/apps">Settings → Connected apps</Link>.</>}</div></div>
      <section className="card pad-card">
        <h3 className="card-h">Day by day</h3>
        <Heatmap days={r.days.map(d => ({ date: d.day, minutes: d.minutes }))} weekStart={settings?.week_start || 1} />
      </section>
      <section className="card">
        <div className="card-head"><h3>Sittings</h3></div>
        {!r.sittings.length ? <Empty title="Nothing yet" icon="timer">Open a song and press play or record.</Empty> :
          <ul className="list">{r.sittings.map(x => (
            <li key={x.id} className="list-row">
              <span className="list-ico"><Icon name="waveform" size={16} /></span>
              <div className="grow min0">
                <div className="ellipsis">{x.song_id ? <Link to={`/songs/${x.song_id}`}>{x.title}</Link> : x.title}</div>
                <div className="muted small">{fmtDate(x.day)} · {fmtISOTime(x.started_at)}{x.takes ? ` · ${x.takes} ${x.takes === 1 ? 'take' : 'takes'}` : ''}{x.sent_to ? ` · in Pocket ${x.sent_to === 'guitar' ? 'Guitar' : 'Time'}` : ''}</div>
              </div>
              <b className="nowrap">{Math.round(x.seconds / 60)} min</b>
            </li>
          ))}</ul>}
      </section>
    </div>
  )
}
