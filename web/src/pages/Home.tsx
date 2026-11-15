// The start page: today and the week, the songs played last, the tools and
// the newest mixes.
import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Home as HomeT } from '../lib/types'
import { useOnChange } from '../lib/data'
import { useCrumb } from '../components/Layout'
import { Empty, Loading } from '../components/ui'
import { Icon } from '../components/icons'
import { Bars } from '../components/Chart'
import { SongCard } from './Songs'
import { MixRow } from './Mixes'
import { addDays, todayISO, weekday } from '../lib/format'

export default function Home() {
  const [h, setH] = useState<HomeT | null>(null)
  const navigate = useNavigate()
  useCrumb(undefined)
  const load = useCallback(() => { api.get<HomeT>(`${V1}/home`).then(setH).catch(() => {}) }, [])
  useEffect(() => { load() }, [load])
  useOnChange(load)
  if (!h) return <Loading />
  const tools = (
    <div className="tools">
      <button type="button" className="tool" onClick={() => navigate('/songs?add=1')}><span className="tool-ico rec"><Icon name="record" size={18} /></span><b>New song</b><span className="muted small">Record track by track</span></button>
      <Link to="/tuner" className="tool"><span className="tool-ico"><Icon name="fork" size={18} /></span><b>Tuner</b><span className="muted small">Guitar, bass, ukulele, voice…</span></Link>
      <Link to="/metronome" className="tool"><span className="tool-ico"><Icon name="metronome" size={18} /></span><b>Metronome</b><span className="muted small">Tap, accents, subdivisions</span></Link>
    </div>
  )
  if (h.songs === 0) return (
    <div className="page">
      <Empty title="Record music, layer by layer" icon="mic">
        Make a song, record a first track — guitar, voice, anything — then listen to it on headphones while you record the next one on top. A click keeps you in time; mix it all to one file when it sounds right.
      </Empty>
      {tools}
    </div>
  )
  const t = todayISO()
  const week = Array.from({ length: 7 }, (_, i) => addDays(t, i - 6)).map(d => ({ label: i18day(d, t), value: h.days.find(x => x.day === d)?.minutes || 0 }))
  return (
    <div className="page">
      <div className="stats-row">
        <div className="stat"><span className="stat-l">Today</span><span className="stat-v">{h.today_minutes} min</span></div>
        <div className="stat"><span className="stat-l">This week</span><span className="stat-v">{h.week_minutes} min</span><span className="stat-s small">{h.week_takes} {h.week_takes === 1 ? 'take' : 'takes'}</span></div>
        <div className="stat"><span className="stat-l">In a row</span><span className="stat-v">{h.streak} {h.streak === 1 ? 'day' : 'days'}</span></div>
        <Link to="/songs" className="stat"><span className="stat-l">Songs</span><span className="stat-v">{h.songs}</span><span className="stat-s small">{h.takes} takes</span></Link>
      </div>
      {tools}
      <section className="stack">
        <div className="row"><h3 className="sec-h grow">Played last</h3><Link to="/songs" className="small">All songs</Link></div>
        <div className="song-grid">{h.recent.slice(0, 6).map(s => <SongCard key={s.id} s={s} />)}</div>
      </section>
      <div className="two-col">
        <section className="card pad-card">
          <div className="row"><h3 className="card-h grow">The last 7 days</h3><Link to="/time" className="small">Time spent</Link></div>
          <Bars items={week} format={v => `${v}`} height={130} />
        </section>
        <section className="card pad-card">
          <div className="row"><h3 className="card-h grow">Newest mixes</h3><Link to="/mixes" className="small">All mixes</Link></div>
          {h.mixes.length ? <ul className="list">{h.mixes.map(m => <li key={m.id}><MixRow m={m} compact /></li>)}</ul> : <p className="muted small">None yet. In a song, ⋯ → <b>Mix down</b> makes one file of all its tracks.</p>}
        </section>
      </div>
    </div>
  )
}

function i18day(d: string, today: string) { return d === today ? 'Today' : weekday(d) }
