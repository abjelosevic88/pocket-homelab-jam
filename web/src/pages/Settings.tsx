import { AskButton } from '../components/AskButton'
import React, { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { APIToken, AppSlug, AppState, ConnMode, Settings as SettingsT, User } from '../lib/types'
import { DATE_FORMATS, TIME_FORMATS, fmtBytes, fmtDateTime, type DateFormat, type TimeFormat } from '../lib/format'
import { Avatar, Confirm, CoreHint, Field, Loading, Modal, Segmented, SettingRow, Switch, Tabs, useAsync, useIsMobile, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { ShortcutsSheet } from '../components/ShortcutsSheet'
import { APP_INFO, STATE_WORDS } from '../lib/apps'
import { audioContext, guessedLatency, measureLatency, microphones, saveLatency, savedLatency } from '../lib/engine'
import { PRESETS } from '../lib/tuner'

type TabId = 'general' | 'recording' | 'apps' | 'profile' | 'people' | 'tokens' | 'data' | 'about'

const TABS: { id: TabId; label: string; icon: string; sub: string }[] = [
  { id: 'general', label: 'General', icon: 'settings', sub: 'What a new song starts with, the tuner, dates and the app title.' },
  { id: 'recording', label: 'Recording', icon: 'mic', sub: 'The click, hearing yourself, stereo, and this device’s delay so takes line up.' },
  { id: 'apps', label: 'Connected apps', icon: 'link', sub: 'Files, Music, Guitar and Time — and where your practice time goes.' },
  { id: 'profile', label: 'Profile', icon: 'user', sub: 'Your name, email and password.' },
  { id: 'people', label: 'People', icon: 'users', sub: 'Everyone who has an account on this box. Each person has their own songs and recordings.' },
  { id: 'tokens', label: 'API & shortcuts', icon: 'key', sub: 'Personal tokens for Siri Shortcuts, scripts and other tools.' },
  { id: 'data', label: 'Backup & data', icon: 'database', sub: 'Export everything (with the audio), bring an export back, download the database.' },
  { id: 'about', label: 'About', icon: 'info', sub: 'Version, numbers and the server.' },
]

export default function Settings() {
  const { tab: raw } = useParams()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const tab: TabId = TABS.find(t => t.id === raw)?.id || 'general'
  const current = TABS.find(t => t.id === tab)!
  const go = (id: TabId) => navigate(`/settings/${id}`, { replace: true })
  useEffect(() => { document.querySelector('.set-content')?.scrollTo({ top: 0 }) }, [tab])
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !document.querySelector('.modal-backdrop, .menu, .sel-pop')) navigate('/') }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [navigate])
  return (
    <div className="set-page">
      <header className="set-header">
        <button type="button" className="btn ghost icon-only" aria-label="Back" title="Back (esc)" onClick={() => navigate('/')}><Icon name="chevron-left" size={18} /></button>
        <img src="/favicon.svg" alt="" width={24} height={24} className="set-logo" />
        <h1>Settings</h1>
        <span style={{ flex: 1 }} />
        <AskButton />
      </header>
      {mobile && <div className="set-tabs-mobile"><Tabs tabs={TABS.map(t => ({ id: t.id, label: t.label, icon: t.icon }))} value={tab} onChange={go} /></div>}
      <div className="set-body">
        {!mobile && (
          <nav className="set-nav" aria-label="Settings sections">
            {TABS.map(t => (
              <button key={t.id} type="button" className={`set-nav-item ${t.id === tab ? 'active' : ''}`} aria-current={t.id === tab ? 'page' : undefined} onClick={() => go(t.id)}>
                <Icon name={t.icon} size={16} />{t.label}
              </button>
            ))}
          </nav>
        )}
        <main className="set-content">
          <div className="set-inner" key={tab}>
            <div className="set-title"><h2>{current.label}</h2><p>{current.sub}</p></div>
            {tab === 'general' && <General />}
            {tab === 'recording' && <Recording />}
            {tab === 'apps' && <Integrations />}
            {tab === 'profile' && <Profile />}
            {tab === 'people' && <Users />}
            {tab === 'tokens' && <Tokens />}
            {tab === 'data' && <Data />}
            {tab === 'about' && <About />}
          </div>
        </main>
      </div>
    </div>
  )
}

function Section({ title, sub, icon, actions, children }: { title: React.ReactNode; sub?: React.ReactNode; icon?: string; actions?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <section className="set-section">
      <div className="set-section-head">
        {icon && <span className="set-app-ico"><Icon name={icon} size={17} /></span>}
        <div className="grow"><h3>{title}</h3>{sub && <p>{sub}</p>}</div>
        {actions && <div className="row">{actions}</div>}
      </div>
      {children && <div className="set-section-body">{children}</div>}
    </section>
  )
}

function useSettingsForm() {
  const { settings, setSettings, user } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<SettingsT | null>(settings)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!f && settings) setF(settings) }, [settings]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = async (patch?: Partial<SettingsT>) => {
    if (!f) return
    setBusy(true)
    try { const s = await api.put<SettingsT>(`${V1}/settings`, { ...f, ...patch }); setSettings(s); setF(s); toast('Saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return { f, setF, save, busy, isAdmin: user?.role === 'admin' }
}

function useTester() {
  const [test, setTest] = useState<Record<string, string>>({})
  const run = async (which: string, url: string, key: string, extra: Record<string, string> = {}) => {
    setTest(t => ({ ...t, [which]: 'testing…' }))
    try { const r = await api.post<{ ok: boolean; message?: string; error?: string }>(`${V1}/integrations/test`, { which, url, key, ...extra }); setTest(t => ({ ...t, [which]: r.ok ? `✓ ${r.message}` : `✕ ${r.error}` })) } catch (e) { setTest(t => ({ ...t, [which]: `✕ ${(e as Error).message}` })) }
  }
  const status = (which: string) => <span className={`small ${test[which]?.startsWith('✓') ? 'ok' : test[which]?.startsWith('✕') ? 'bad' : 'muted'}`}>{test[which]}</span>
  return { run, status }
}

function General() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <Section title="A new song starts with" icon="waveform" sub="Each song can change these.">
        <SettingRow title="Tempo" help="Beats a minute."><input type="number" min={20} max={400} value={f.tempo} onChange={e => setF({ ...f, tempo: Number(e.target.value) })} style={{ width: 100 }} /></SettingRow>
        <SettingRow title="Beats in a bar"><Segmented size="sm" value={f.beats} onChange={v => setF({ ...f, beats: v })} options={[2, 3, 4, 5, 6].map(n => ({ value: n, label: String(n) }))} /></SettingRow>
        <SettingRow title="Count-in before recording"><Segmented size="sm" value={f.count_in} onChange={v => setF({ ...f, count_in: v })} options={[{ value: 0, label: 'None' }, { value: 1, label: '1 bar' }, { value: 2, label: '2 bars' }]} /></SettingRow>
        <SettingRow title="The click plays while recording" help="The count-in always clicks."><Switch checked={f.click} onChange={v => setF({ ...f, click: v })} label="Click" /></SettingRow>
      </Section>
      <Section title="Tuner" icon="fork">
        <SettingRow title="Instrument"><Select ariaLabel="Instrument" value={f.tuner_preset} onChange={v => setF({ ...f, tuner_preset: v })} options={PRESETS.map(p => ({ value: p.id, label: p.name }))} /></SettingRow>
        <SettingRow title="Reference A" help="440 Hz almost always; some orchestras use 442."><input type="number" min={415} max={466} value={f.a4} onChange={e => setF({ ...f, a4: Number(e.target.value) })} style={{ width: 100 }} /></SettingRow>
      </Section>
      <Section title="Dates" icon="calendar">
        <SettingRow title="Week starts on"><Segmented size="sm" value={f.week_start} onChange={v => setF({ ...f, week_start: v })} options={[{ value: 1, label: 'Monday' }, { value: 6, label: 'Saturday' }, { value: 7, label: 'Sunday' }]} /></SettingRow>
        <SettingRow title="Date format"><Select ariaLabel="Date format" value={f.date_format} onChange={v => setF({ ...f, date_format: v as DateFormat })} options={DATE_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        <SettingRow title="Time format"><Select ariaLabel="Time format" value={f.time_format} onChange={v => setF({ ...f, time_format: v as TimeFormat })} options={TIME_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        {isAdmin && <SettingRow title="App title" help="Shown in the sidebar for everyone on this box."><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} /></SettingRow>}
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function Recording() {
  const { f, setF, save, busy } = useSettingsForm()
  const { toast } = useToast()
  const [lat, setLat] = useState<number | null>(savedLatency())
  const [measuring, setMeasuring] = useState(0)
  const [mics, setMics] = useState<MediaDeviceInfo[]>([])
  useEffect(() => { microphones().then(setMics) }, [])
  if (!f) return <Loading />
  const guess = (() => { try { return guessedLatency(audioContext()) } catch { return 40 } })()
  const measure = async () => {
    setMeasuring(1)
    try { const ms = await measureLatency(n => setMeasuring(n + 1)); saveLatency(ms); setLat(ms); toast(`This device: ${ms} ms`) } catch (e) { toast((e as Error).message, 'err') } finally { setMeasuring(0); microphones().then(setMics) }
  }
  return (
    <div className="stack loose">
      <Section title="This device’s delay" icon="activity" sub="Sound takes a moment to reach your headphones and to come back in from the microphone. Pocket Jam takes that time off every new take so it lines up with what you heard. It is measured once per phone or computer (kept in this browser).">
        <SettingRow title="Delay" help={lat !== null ? 'Measured on this device.' : `Not measured yet; using a guess of ${guess} ms.`}><b className="tnum">{lat ?? guess} ms</b></SettingRow>
        <div className="callout info"><Icon name="info" size={15} /><div><b>To measure:</b> take the headphones off, turn the volume up, and keep the room quiet. Eight clicks play through the speaker and the microphone listens for them. Measure again when you change headphones or the sound card.</div></div>
        <div className="row wrap">
          <button type="button" className="btn primary" onClick={measure} disabled={!!measuring}>{measuring ? `Listening… ${Math.min(8, measuring)}/8` : <><Icon name="activity" size={15} />Measure the delay</>}</button>
          {lat !== null && <button type="button" className="btn ghost" onClick={() => { saveLatency(null); setLat(null) }}>Forget it</button>}
          <span className="row" style={{ gap: 6 }}>
            <button type="button" className="btn sm" onClick={() => { const v = Math.max(0, (lat ?? guess) - 5); saveLatency(v); setLat(v) }}>−5 ms</button>
            <button type="button" className="btn sm" onClick={() => { const v = (lat ?? guess) + 5; saveLatency(v); setLat(v) }}>+5 ms</button>
          </span>
        </div>
      </Section>
      <Section title="While recording" icon="mic">
        <SettingRow title="Hear yourself" help="Your microphone in the headphones as you record. With Bluetooth headphones the delay makes this hard to play with; wired ones are better."><Switch checked={f.monitor} onChange={v => setF({ ...f, monitor: v })} label="Hear yourself" /></SettingRow>
        <SettingRow title="Make quiet takes louder" help="Raises a take so its loudest moment is near full volume (up to 30 dB). Phones record an instrument across the room very softly; switch it off with a sound card whose level you set yourself."><Switch checked={f.level_takes} onChange={v => setF({ ...f, level_takes: v })} label="Level takes" /></SettingRow>
        <SettingRow title="Record in stereo" help="Only with a stereo microphone or a sound card with two inputs; a phone records one."><Switch checked={f.stereo} onChange={v => setF({ ...f, stereo: v })} label="Stereo" /></SettingRow>
        <SettingRow title="Keep the screen on" help="While a song is open."><Switch checked={f.keep_awake} onChange={v => setF({ ...f, keep_awake: v })} label="Keep awake" /></SettingRow>
        {mics.length > 0 && <SettingRow title="Microphones here" help="The browser picks the one in use; change it in the system settings."><span className="muted small">{mics.map(m => m.label || 'Microphone').join(', ')}</span></SettingRow>}
      </Section>
      <Section title="The click" icon="metronome">
        <SettingRow title="Sound"><Segmented size="sm" value={f.click_sound} onChange={v => setF({ ...f, click_sound: v })} options={[{ value: 'click', label: 'Click' }, { value: 'wood', label: 'Wood' }, { value: 'beep', label: 'Beep' }]} /></SettingRow>
        <SettingRow title="Volume"><input type="range" min={0} max={100} value={f.click_volume} onChange={e => setF({ ...f, click_volume: Number(e.target.value) })} /></SettingRow>
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

const APPS: { slug: AppSlug; title: string; sub: string; token: string; port: string }[] = [
  { slug: 'files', title: 'Pocket Files — recordings in and mixes out', sub: 'Bring a recording or a backing track in from your files, and save your mixes there.', token: 'Pocket Files → Settings → API tokens', port: '8187' },
  { slug: 'music', title: 'Pocket Music — backing tracks', sub: 'Use a song from your music library as a backing track to play and record along to.', token: 'Pocket Music → Settings → API tokens', port: '8200' },
  { slug: 'guitar', title: 'Pocket Guitar — practice time', sub: 'The time you spend playing and recording here, logged as guitar practice.', token: 'Pocket Guitar → Settings → API tokens', port: '8181' },
  { slug: 'time', title: 'Pocket Time — tracked time', sub: 'The time you spend playing and recording here, as entries in Pocket Time.', token: 'Pocket Time → Settings → API tokens', port: '8183' },
]
function Integrations() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  const { data: states } = useAsync(() => api.get<{ apps: AppState[] }>(`${V1}/apps`).then(r => r.apps).catch(() => [] as AppState[]), [APPS.map(a => f?.[`${a.slug}_mode`]).join()])
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const eff = (m: ConnMode): ConnMode => !coreOn && m === 'core' ? 'off' : m
  const set = (k: string, v: string) => setF({ ...f, [k]: v } as SettingsT)
  return (
    <div className="stack loose">
      <div className="callout info"><Icon name="info" size={15} /><div>These connections are yours. Jam reads or adds things in them when you press a button (and, if you switch it on below, your practice time as you play), and everything here works without them. Through <b>Pocket Core</b> Jam acts as you in your own Pocket apps with nothing to paste (switch it off for yourself in Pocket Core → Settings → Apps acting for me); <b>Direct</b> uses a URL and an API token you enter.{!coreOn && <CoreHint />}</div></div>
      {APPS.map(a => {
        const mode = f[`${a.slug}_mode`]
        const via = f[`${a.slug}_via`]
        const name = APP_INFO[a.slug].name
        const st = states?.find(x => x.slug === a.slug)
        return (
          <Section key={a.slug} title={a.title} icon={APP_INFO[a.slug].icon} actions={st && st.state !== 'on' && st.state !== 'off' ? <span className={`badge ${st.state === 'partly' ? 'ok' : 'warn'}`}>{STATE_WORDS[st.state]}</span> : connBadge(eff(mode), via)} sub={a.sub}>
            <Connection app={name} mode={mode} via={via} detail={f[`${a.slug}_detail`]} core={coreOn} hasToken={!!f[`${a.slug}_token`]}
              onMode={m => save({ [`${a.slug}_mode`]: m } as Partial<SettingsT>)} onDisconnect={() => save({ [`${a.slug}_url`]: '', [`${a.slug}_token`]: '' } as Partial<SettingsT>)}
              test={<><button type="button" className="btn sm" onClick={() => run(a.slug, f[`${a.slug}_url`], f[`${a.slug}_token`])}>Test connection</button>{status(a.slug)}</>}
              direct={<div className="two-col tight">
                <Field label="URL" help="Inside Docker use http://host.docker.internal:PORT or the service name."><input value={f[`${a.slug}_url`]} onChange={e => set(`${a.slug}_url`, e.target.value)} placeholder={`http://host.docker.internal:${a.port}`} /></Field>
                <Field label="API token" help={a.token}><input value={f[`${a.slug}_token`]} onChange={e => set(`${a.slug}_token`, e.target.value)} autoComplete="off" /></Field>
              </div>} />
            {st?.fix && st.state !== 'on' && <p className="muted small"><Icon name="info" size={12} className="inline-ico" /> {st.fix}.</p>}
          </Section>
        )
      })}
      <WhereThingsGo f={f} setF={setF} />
      <Section title="Other Pocket apps" icon="layers" sub="Jam offers your songs to the rest of the box through Pocket Core: Pocket Day can show the time you played, Pocket Search finds your songs and mixes, Pocket AI can tell you what you worked on. Switched off until you allow them in Pocket Core → Links.">
        <dl className="set-dl">
          <div><dt>jam:read</dt><dd className="muted">Your songs, tracks, takes and mixes (and listening to them) and the time you spent.</dd></div>
          <div><dt>jam:write</dt><dd className="muted">Add a song or an idea to record; that app may change only the songs it added, never your recordings.</dd></div>
          <div><dt>events:read</dt><dd className="muted"><code>jam.song_added</code>, <code>take_recorded</code>, <code>mix_made</code> — for Pocket Flows.</dd></div>
        </dl>
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function Connection({ app, mode: saved, via, detail, core, onMode, onDisconnect, hasToken, test, direct }: {
  app: string; mode: ConnMode; via: string; detail: string; core: boolean; onMode: (m: ConnMode) => void; onDisconnect: () => void
  hasToken: boolean; test: React.ReactNode; direct: React.ReactNode
}) {
  const [advanced, setAdvanced] = useState(saved === 'direct')
  const mode: ConnMode = !core && saved === 'core' ? 'off' : saved
  const ok = via !== '' && mode !== 'off'
  const showDirect = !core || mode === 'direct' || advanced
  const line = mode === 'off' ? `Off: Jam does not reach ${app}.`
    : mode === 'core' ? (ok ? `Connected through Pocket Core, as you${detail ? ` (${detail})` : ''}. Nothing to fill in; switch it off for yourself in Pocket Core → Settings → Apps acting for me.` : `Not connected: ${detail}.`)
    : (ok ? `Direct connection with your own ${app} URL and token; use Test connection to check it.` : `Not connected: ${detail || 'add the URL and a token'}.`)
  return (
    <div className="conn-block">
      <div className="conn-mode">
        <Segmented size="sm" value={mode} onChange={m => onMode(m as ConnMode)} options={[
          { value: 'core', label: 'Pocket Core', title: core ? 'Through Pocket Core: per person, nothing to paste' : 'Needs Pocket Core: there is none on this box', disabled: !core },
          ...(showDirect ? [{ value: 'direct', label: 'Direct', title: 'Your own URL and API token' }] : []),
          { value: 'off', label: 'Off' },
        ]} />
        {!showDirect && <button type="button" className="conn-advanced" onClick={() => setAdvanced(true)}>Advanced: connect directly with a URL and key</button>}
      </div>
      <div className={`conn-status ${mode === 'off' ? '' : ok ? 'ok' : 'bad'}`}><span className="dot" />{line}</div>
      {mode === 'direct' && <>
        {direct}
        <div className="row wrap">{test}{hasToken && <button type="button" className="btn sm ghost danger-text" onClick={onDisconnect} title="Remove the saved URL and token">Disconnect</button>}</div>
      </>}
      {mode === 'core' && <div className="row wrap">{test}</div>}
    </div>
  )
}

const connBadge = (mode: ConnMode, via: string) => mode === 'off' ? <span className="badge">off</span>
  : via ? <span className="badge ok">{via === 'core' ? 'Pocket Core' : 'direct'}</span>
  : <span className="badge warn">not connected</span>

function Users() {
  const { user, users, reloadMeta, status } = useApp()
  const { toast } = useToast()
  const [edit, setEdit] = useState<Partial<User> | null>(null)
  const [del, setDel] = useState<User | null>(null)
  const isAdmin = user?.role === 'admin'
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); await reloadMeta(); toast(msg) } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Section title="People on this box" actions={isAdmin && <button type="button" className="btn primary sm" onClick={() => setEdit({ role: 'member' })}><Icon name="plus" size={14} />Add person</button>}
      sub="Everyone signs in with their own account and has their own songs, recordings, settings and connections. Admins manage accounts; they never see other people's songs or recordings.">
      <div className="set-people">
        {users.map(u => (
          <div key={u.id} className="set-person">
            <Avatar name={u.name} email={u.email} size={34} />
            <div className="grow">
              <div className="set-person-name">{u.name || <span className="muted">—</span>}{u.id === user?.id && <span className="badge">you</span>}{u.role === 'admin' && <span className="badge accent">admin</span>}</div>
              <div className="muted small ellipsis">{u.email}</div>
            </div>
            {isAdmin && <span className="row" style={{ justifyContent: 'flex-end' }}><button type="button" className="btn ghost sm" onClick={() => setEdit(u)}>Edit</button>{u.id !== user?.id && <button type="button" className="btn ghost sm danger-text" onClick={() => setDel(u)}>Remove</button>}</span>}
          </div>
        ))}
      </div>
      {status?.auth_mode !== 'local' && <p className="muted small mt">Auth mode is <code>{status?.auth_mode}</code>: users are created automatically on first visit; passwords are not used here.</p>}
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reloadMeta() }} />}
      {del && <Confirm confirmLabel="Remove" title={`Remove ${del.name || del.email}?`} message="Their account goes, with their songs, recordings and mixes." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'User removed') }} />}
    </Section>
  )
}

function UserModal({ user, local, onClose, onSaved }: { user: Partial<User>; local: boolean; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: user.name || '', email: user.email || '', password: '', role: user.role || 'member' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { if (user.id) await api.put(`${V1}/users/${user.id}`, f); else await api.post(`${V1}/users`, f); toast(user.id ? 'User saved' : 'User added'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={user.id ? 'Edit person' : 'Add a person'} onClose={onClose} size="sm">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={60} /></Field>
        <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required /></Field>
        {local && <Field label={user.id ? 'New password (leave empty to keep)' : 'Password'} help="At least 8 characters."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={user.id ? 0 : 8} required={!user.id} autoComplete="new-password" /></Field>}
        <Field group label="Role"><Segmented value={f.role} onChange={v => setF({ ...f, role: v })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{user.id ? 'Save' : 'Add person'}</button></div>
      </form>
    </Modal>
  )
}

function Profile() {
  const { user, setUser, status } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: user?.name || '', email: user?.email || '', current_password: '', new_password: '' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try {
      setUser(await api.put(`${V1}/auth/me`, f))
      setF(x => ({ ...x, current_password: '', new_password: '' })); toast('Profile saved')
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={save} className="stack loose">
      <div className="set-profile">
        <Avatar name={user?.name} email={user?.email} size={56} />
        <div className="grow"><h3>{user?.name || user?.email}</h3><p className="muted small">{user?.email} · {user?.role}</p></div>
      </div>
      <Section title="You">
        <div className="stack">
          <Field label="Name" help="The singer’s name when you sing alone."><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
          <Field label="Email" help="The same email as in Pocket Core, so signing in finds your account."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
        </div>
      </Section>
      {status?.auth_mode === 'local' && <Section title="Password" sub="Leave empty to keep the current one."><div className="stack">
        <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF({ ...f, current_password: e.target.value })} autoComplete="current-password" /></Field>
        <Field label="New password" help="At least 8 characters."><input type="password" value={f.new_password} onChange={e => setF({ ...f, new_password: e.target.value })} autoComplete="new-password" /></Field>
      </div></Section>}
      <div className="row"><button className="btn primary" disabled={busy}>Save changes</button></div>
    </form>
  )
}

function Tokens() {
  const { toast } = useToast()
  const { settings } = useApp()
  const { data, reload } = useAsync(() => api.get<APIToken[]>(`${V1}/tokens`), [])
  const [name, setName] = useState('')
  const [fresh, setFresh] = useState('')
  const [del, setDel] = useState<APIToken | null>(null)
  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    try { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: name || 'token' }); setFresh(r.token); setName(''); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  const base = settings?.base_url || window.location.origin
  return (
    <div className="stack loose">
      <Section title="Personal tokens" icon="key" sub={<>Use as <code>Authorization: Bearer &lt;token&gt;</code>. A token acts as you: it can read and change everything, feed a Homepage widget (<code>GET /api/v1/summary</code>) or send a recording from a Siri Shortcut. See <code>docs/api.md</code>.</>}>
        <form onSubmit={create} className="row mb"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. iPhone Shortcut)" /><button className="btn primary"><Icon name="plus" size={15} />Create</button></form>
        {fresh && <div className="set-fresh mb"><div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — it is shown once.</b></div><pre className="mono">{fresh}</pre></div>}
        {data && data.length > 0 ? (
          <table className="table compact">
            <thead><tr><th>Name</th><th>Prefix</th><th>Last used</th><th>Created</th><th /></tr></thead>
            <tbody>{data.map(t => <tr key={t.id}><td>{t.name}</td><td className="mono">{t.prefix}…</td><td className="muted">{t.last_used_at ? fmtDateTime(t.last_used_at) : 'never'}</td><td className="muted">{fmtDateTime(t.created_at)}</td><td className="right"><button type="button" className="btn ghost sm danger-text" onClick={() => setDel(t)}>Revoke</button></td></tr>)}</tbody>
          </table>
        ) : <p className="muted">No tokens yet.</p>}
        {del && <Confirm confirmLabel="Revoke" title={`Revoke “${del.name}”?`} message="Anything using this token stops working immediately." onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
      </Section>
      <Section title="Endpoints" icon="code" sub={<>At <code>{base}/api/v1</code>.</>}>
        <dl className="set-dl">
          <div><dt><code>GET /summary</code></dt><dd className="muted">Songs, minutes this week, the streak, the songs played last — for Homepage.</dd></div>
          <div><dt><code>GET /songs?q=…</code></dt><dd className="muted">Your songs; <code>GET /songs/ID</code> with its tracks and takes.</dd></div>
          <div><dt><code>POST /songs</code></dt><dd className="muted"><code>{'{"title": "New idea", "tempo": 92, "key_name": "A minor"}'}</code> — a Shortcut that starts a song.</dd></div>
          <div><dt><code>POST /tracks/ID/takes</code></dt><dd className="muted">Multipart <code>meta</code> (JSON: <code>offset_ms</code>, <code>duration_ms</code>, <code>peaks</code>…) + <code>file</code>: a recording made elsewhere as a take.</dd></div>
          <div><dt><code>GET /takes/ID/audio</code> · <code>GET /mixes/ID/audio</code></dt><dd className="muted">The audio (with ranges; <code>?download=1</code> to save).</dd></div>
          <div><dt><code>GET /songs/ID/stems.zip</code></dt><dd className="muted">Every track’s playing take with <code>song.json</code> (where each starts) — for Pocket Studio or another recording app.</dd></div>
          <div><dt><code>GET /time</code> · <code>GET /day/YYYY-MM-DD</code></dt><dd className="muted">The time spent, per day and per sitting.</dd></div>
        </dl>
      </Section>
    </div>
  )
}

function Data() {
  const { user } = useApp()
  const { toast } = useToast()
  const file = React.useRef<HTMLInputElement>(null)
  const bring = async (fl: File) => {
    try {
      toast('Bringing it in…')
      const res = await fetch(`${V1}/import`, { method: 'POST', body: fl, headers: { 'Content-Type': fl.type || 'application/octet-stream' }, credentials: 'same-origin' })
      const j = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error(j.error || res.statusText)
      toast(j.text || 'Brought in')
    } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <div className="stack loose">
      <Section title="Export" icon="archive" sub="Everything you made: songs with their tracks, every take and mix and the time spent. The .zip carries the audio too; the JSON is only the facts (small). The CSV is the time spent, one row per sitting.">
        <div className="row wrap"><a className="btn primary" href={`${V1}/export.json?audio=1`}><Icon name="download" size={15} />Everything (.zip)</a><a className="btn" href={`${V1}/export.json`}><Icon name="download" size={15} />JSON without audio</a><a className="btn" href={`${V1}/export.csv`}><Icon name="download" size={15} />Time spent (CSV)</a></div>
      </Section>
      <Section title="Bring a file in" icon="upload" sub="A Pocket Jam export (also from another box): its songs, takes, mixes and time come back as yours. The same file twice adds nothing twice. Takes and mixes come back only from the .zip.">
        <button type="button" className="btn" onClick={() => file.current?.click()}><Icon name="upload" size={15} />Bring a file…</button>
        <input ref={file} type="file" hidden accept=".json,.zip,application/json,application/zip" onChange={e => { const fl = e.target.files?.[0]; e.target.value = ''; if (fl) bring(fl) }} />
      </Section>
      {user?.role === 'admin' && <Section title="Database backup" icon="database" sub="The whole box, every account — admins only.">
        <p className="muted">A consistent copy of the SQLite database (<code>VACUUM INTO</code>). Restore by stopping the app and replacing <code>pocket-jam.db</code> in the data folder.</p>
        <a className="btn" href={`${V1}/backup`}><Icon name="database" size={15} />Download database</a>
      </Section>}
    </div>
  )
}

function About() {
  const { data, error } = useAsync(() => api.get<Record<string, unknown>>(`${V1}/settings/system`), [])
  const [keys, setKeys] = useState(false)
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return <Loading />
  const counts = (data.counts || {}) as Record<string, number>
  const core = (data.core || {}) as { configured?: boolean; registered?: boolean; url?: string; error?: string }
  const tiles: [string, number | undefined, string][] = [['Songs', counts.songs, 'waveform'], ['Tracks', counts.tracks, 'layers'], ['Takes', counts.takes, 'mic'], ['Mixes', counts.mixes, 'mixer'], ['Sittings', counts.sittings, 'timer']]
  const rows: [string, React.ReactNode][] = [
    ['Version', <code key="v">{String(data.version)}</code>],
    ['Uptime', String(data.uptime)],
    ['Runtime', `${String(data.go)} · ${String(data.os)}/${String(data.arch)}`],
    ['Time zone', String(data.timezone)],
    ['Sign-in', String(data.auth_mode)],
    ['Pocket Core', core.configured ? (core.registered ? <span key="c" className="ok">connected · {core.url}</span> : <span key="c" className="bad">{core.error || 'not registered yet'}</span>) : <span key="c" className="muted">not configured</span>],
    ['Database', <span key="db"><code>{String(data.db_path)}</code> · {fmtBytes(Number(data.db_bytes))}</span>],
    ['People', `${counts.users ?? 0} accounts on this box`],
  ]
  return (
    <div className="stack loose">
      <div className="set-about-hero">
        <img src="/favicon.svg" alt="" width={48} height={48} />
        <div>
          <h3>Pocket Jam</h3>
          <p className="muted small">Record music on your phone or computer, track by track over headphones, with a click, a count-in and the device’s delay taken out; keep every take, mix down, tune up, keep time. One Go binary, one SQLite file, one volume.</p>
        </div>
      </div>
      <div className="set-stats">
        {tiles.map(([label, n, icon]) => (
          <div key={label} className="set-stat"><Icon name={icon} size={16} /><span className="set-stat-n tnum">{(n ?? 0).toLocaleString()}</span><span className="set-stat-l">{label}</span></div>
        ))}
      </div>
      <Section title="This server">
        <dl className="set-dl">
          {rows.map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}
        </dl>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn" onClick={() => setKeys(true)}><Icon name="keyboard" size={15} />Keyboard shortcuts</button>
        <a className="btn" href="https://github.com/abjelosevic88/pocket-homelab-jam" target="_blank" rel="noreferrer"><Icon name="external" size={15} />Project on GitHub</a>
      </div>
      <p className="muted small">Open source (MIT). Part of the Pocket apps: Pocket Core connects them — Calendar, Contacts, Family, Email, Chat, Photos, Notes, Files, Voice, Tasks, Alerts, Blog, Weather, Bookshelf, Music, Movement, Health, Wealth, Pocket AI and more.</p>
      {keys && <ShortcutsSheet onClose={() => setKeys(false)} />}
    </div>
  )
}

function WhereThingsGo({ f, setF }: { f: SettingsT; setF: (s: SettingsT) => void }) {
  return (
    <Section title="Where things go" icon="layers">
      <SettingRow title="Practice time goes to" help="The time a song plays or records, sent on as you play (one entry per sitting).">
        <Segmented size="sm" value={f.practice_to} onChange={v => setF({ ...f, practice_to: v })} options={[
          { value: 'off', label: 'Nowhere' },
          { value: 'guitar', label: 'Pocket Guitar', disabled: !f.guitar_via, title: f.guitar_via ? undefined : 'Connect Pocket Guitar above first' },
          { value: 'time', label: 'Pocket Time', disabled: !f.time_via, title: f.time_via ? undefined : 'Connect Pocket Time above first' },
        ]} />
      </SettingRow>
      <SettingRow title="Files folder" help="Where mixes go in Pocket Files."><input value={f.files_folder} onChange={e => setF({ ...f, files_folder: e.target.value })} disabled={!f.files_via} /></SettingRow>
    </Section>
  )
}
