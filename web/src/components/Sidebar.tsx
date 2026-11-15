// Left column: a new song, the sections, and the starred songs.
import { useCallback, useEffect, useState } from 'react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { useOnChange } from '../lib/data'
import type { Home } from '../lib/types'
import { Icon } from './icons'

export function Sidebar({ onCreate, onNavigate }: { onCreate: () => void; onNavigate: () => void }) {
  const { status, settings } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [home, setHome] = useState<Home | null>(null)
  const load = useCallback(() => { api.get<Home>(`${V1}/home`).then(setHome).catch(() => {}) }, [])
  useEffect(() => { load() }, [load])
  useOnChange(load)

  const title = settings?.title || status?.title || 'Pocket Jam'
  const path = loc.pathname
  const nav: { to: string; label: string; icon: string; active: boolean; count?: number | string }[] = [
    { to: '/', label: 'Home', icon: 'home', active: path === '/' },
    { to: '/songs', label: 'Songs', icon: 'waveform', active: path.startsWith('/songs'), count: home?.songs || undefined },
    { to: '/mixes', label: 'Mixes', icon: 'mixer', active: path.startsWith('/mixes'), count: home?.mix_count || undefined },
    { to: '/tuner', label: 'Tuner', icon: 'fork', active: path.startsWith('/tuner') },
    { to: '/metronome', label: 'Metronome', icon: 'metronome', active: path.startsWith('/metronome') },
    { to: '/time', label: 'Time spent', icon: 'timer', active: path.startsWith('/time') },
  ]
  return (
    <aside className="sidebar" aria-label="Pocket Jam">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Jam' ? title : <>Pocket <b>Jam</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^v?\d+\.\d+/.test(status.version) ? 'v' + status.version.replace(/^v/, '').replace(/[-+].*$/, '') : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" onClick={onCreate} title="New song (N)">
        <Icon name="plus" size={15} strokeWidth={2.2} />New song
      </button>

      <nav className="sb-nav" aria-label="Sections">
        {nav.map(x => (
          <NavLink key={x.to} to={x.to} onClick={onNavigate} className={() => `sb-nav-item ${x.active ? 'active' : ''}`}>
            <Icon name={x.icon} size={16} />{x.label}
            {!!x.count && <span className="sb-count">{x.count}</span>}
          </NavLink>
        ))}
      </nav>

      {!!home?.starred.length && <section className="sb-sec">
        <h4 className="sb-h"><Link to="/songs?filter=starred" onClick={onNavigate} className="sb-h-link">Starred</Link></h4>
        {home.starred.slice(0, 8).map(s => (
          <button key={s.id} type="button" className={`sb-row sb-list ${path === `/songs/${s.id}` ? 'active' : ''}`} onClick={() => { onNavigate(); navigate(`/songs/${s.id}`) }} title={s.title}>
            <Icon name="star" size={15} className="sb-list-ico" />
            <span className="grow ellipsis">{s.title}</span>
          </button>
        ))}
      </section>}
      {!!home?.recent.length && <section className="sb-sec">
        <h4 className="sb-h">Played last</h4>
        {home.recent.filter(s => !s.starred).slice(0, 5).map(s => (
          <button key={s.id} type="button" className={`sb-row sb-list ${path === `/songs/${s.id}` ? 'active' : ''}`} onClick={() => { onNavigate(); navigate(`/songs/${s.id}`) }} title={s.title}>
            <Icon name="waveform" size={15} className="sb-list-ico" />
            <span className="grow ellipsis">{s.title}</span>
          </button>
        ))}
      </section>}

      <div className="sb-foot">
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('pjam:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}
