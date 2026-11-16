// Ideas: quick recordings on their own — a riff, a melody — before there is
// a song for them. Record one here (or on the phone), play it back, name it,
// and make it the first track of a new song when it grows. An idea can be in
// a band, like a song (?band=<id> or none shows one band's).
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Band, Idea, Instrument, Song } from '../lib/types'
import { useOnChange, announceChange } from '../lib/data'
import { useCrumb } from '../components/Layout'
import { Confirm, ContextMenu, Empty, Field, Loading, MenuButton, Modal, Spinner, useContextMenu, useToast, errMsg, type MenuItem } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { Mic, audioContext, audioSession, clock, decode, encodeWav, levelTake, peaksBytes, peaksOf, uploadAudio } from '../lib/engine'
import { clientId, partFile, rangeLabel, saveBlob, sliceAudio, type Sel } from '../lib/part'
import { SelBand, SelBar, useRangeSelect } from '../components/Selection'
import { fmtDateTime } from '../lib/format'
import { INSTRUMENTS, micError } from './SongPage'
import { METRES, useBands, useTapTempo } from './Songs'

/** The one idea playing (starting another stops it). */
let current: HTMLAudioElement | null = null
function stopCurrent() { current?.pause() }

export default function Ideas() {
  const [params, setParams] = useSearchParams()
  const bandParam = params.get('band') || '' // a band's id, 'none', or '' for every idea
  const [list, setList] = useState<Idea[] | null>(null)
  const [recording, setRecording] = useState(false)
  const [toSong, setToSong] = useState<Idea | null>(null)
  const bands = useBands()
  const band = bands?.find(b => String(b.id) === bandParam) || null
  useCrumb(band ? band.name : bandParam === 'none' ? 'No band' : undefined)
  const setBand = (v: string) => { if (v) params.set('band', v); else params.delete('band'); setParams(params, { replace: true }) }
  const load = useCallback(() => {
    api.get<{ ideas: Idea[] }>(`${V1}/ideas${bandParam ? `?band=${encodeURIComponent(bandParam)}` : ''}`).then(r => setList(r.ideas)).catch(() => setList([]))
  }, [bandParam])
  useEffect(() => { load() }, [load])
  useOnChange(load)
  useEffect(() => () => stopCurrent(), [])
  const record = () => { stopCurrent(); setRecording(true) }
  const recordBtn = <button type="button" className="btn primary" onClick={record} disabled={recording}><Icon name="record" size={15} />Record an idea</button>
  const changed = announceChange // reloads this list and the bands' counts
  return (
    <div className="page">
      {(!!list?.length || bandParam) && <div className="toolbar">
        <span className="grow muted small">{list ? `${list.length} ${list.length === 1 ? 'idea' : 'ideas'}${band ? ` for ${band.name}` : bandParam === 'none' ? ' in no band' : ''}, newest first` : ''}</span>
        {!!bands?.length && <Select ariaLabel="Band" value={bandParam} onChange={setBand} placeholder="Every band" clearLabel="Every band"
          options={[...bands.map(b => ({ value: String(b.id), label: b.name, hint: String(b.ideas) })), { value: 'none', label: 'In no band' }]} />}
        {recordBtn}
      </div>}
      {recording && <Recorder band={band} onClose={() => setRecording(false)} onKept={changed} />}
      {!list ? <Loading /> : !list.length ? (
        !recording && (bandParam
          ? <Empty title={band ? `No ideas for ${band.name} yet` : 'No ideas here'} icon="lightbulb">{band ? 'Record one while this band is chosen and it goes in this band; or choose Band in an idea’s menu.' : 'Every idea is in a band.'}</Empty>
          : <Empty title="No ideas yet" icon="lightbulb">A riff, a melody, a few words to a tune: record it here or on the phone before it’s gone — no song, no click, just the microphone. When it grows, make it the first track of a new song.<div className="row mt" style={{ justifyContent: 'center' }}>{recordBtn}</div></Empty>)
      ) : <section className="card"><ul className="list">{list.map(x => <li key={x.id}><IdeaRow x={x} bands={bands || []} onChange={changed} onSong={() => { stopCurrent(); setToSong(x) }} /></li>)}</ul></section>}
      {toSong && <ToSong x={toSong} onClose={() => setToSong(null)} />}
    </div>
  )
}

// ---- recording ----

type RecState = 'opening' | 'rolling' | 'saving' | 'failed'

/** The microphone straight to an idea: no click, no count-in, the browser's voice filters off. It goes in `band` when one is chosen. */
function Recorder({ band, onClose, onKept }: { band: Band | null; onClose: () => void; onKept: () => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const [state, setState] = useState<RecState>('opening')
  const [pct, setPct] = useState(0)
  const mic = useRef<Mic | null>(null)
  const t0 = useRef(0)
  const started = useRef(new Date())
  const timeRef = useRef<HTMLSpanElement>(null)
  const meterRef = useRef<HTMLSpanElement>(null)
  const pending = useRef<{ wav: Blob; meta: Record<string, unknown>; raised: number } | null>(null)

  useEffect(() => {
    let gone = false
    const ctx = audioContext()
    const out = ctx.createGain()
    out.connect(ctx.destination)
    Mic.open(out, { stereo: !!settings?.stereo, monitor: false }).then(m => {
      if (gone) { m.close(); return }
      mic.current = m
      m.start()
      t0.current = ctx.currentTime
      started.current = new Date()
      setState('rolling')
    }).catch(e => { if (!gone) { toast(micError(e), 'err'); onClose() } })
    return () => { gone = true; mic.current?.close(); mic.current = null; out.disconnect() }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (state !== 'rolling') return
    let raf = 0
    const frame = () => {
      const m = mic.current
      if (m) {
        if (timeRef.current) timeRef.current.textContent = clock(Math.max(0, m.ctx.currentTime - t0.current))
        if (meterRef.current) meterRef.current.style.transform = `scaleX(${Math.min(1, Math.sqrt(m.level()))})`
      }
      raf = requestAnimationFrame(frame)
    }
    raf = requestAnimationFrame(frame)
    return () => cancelAnimationFrame(raf)
  }, [state])

  const send = async () => {
    const p = pending.current
    if (!p) return
    setState('saving')
    setPct(0)
    try {
      const idea = await uploadAudio(`${V1}/ideas`, p.wav, 'idea.wav', p.meta, setPct) as Idea
      pending.current = null
      toast(`Kept “${idea.name}”${p.raised ? ` (made ${p.raised} dB louder)` : ''}`, 'ok')
      onKept()
      onClose()
    } catch (e) {
      toast(errMsg(e), 'err')
      setState('failed')
    }
  }

  const stop = async (keep: boolean) => {
    const m = mic.current
    if (!m || state !== 'rolling') return
    setState('saving')
    const got = await m.stop()
    // the microphone closes right away: while it is open, an iPhone plays through the earpiece
    m.close()
    mic.current = null
    if (!keep) { onClose(); return }
    const len = got.channels[0]?.length || 0
    if (len < got.rate * 0.3) { toast('Too short to keep: record at least a moment', 'err'); onClose(); return }
    const raised = settings?.level_takes !== false ? levelTake(got.channels) : 0
    pending.current = {
      wav: encodeWav(got.channels, got.rate), raised,
      meta: {
        duration_ms: Math.round(len / got.rate * 1000), sample_rate: got.rate, channels: got.channels.length, peaks: peaksOf(got.channels, got.rate),
        source: 'recorded', device: m.label, client_id: clientId(), created_at: started.current.toISOString(),
        ...(band ? { band_id: band.id } : {}),
      },
    }
    await send()
  }

  return (
    <section className={`card idea-rec ${state === 'rolling' ? 'on' : ''}`} aria-live="polite">
      <span className="idea-rec-dot" aria-hidden />
      <div className="grow min0">
        <div className="row" style={{ gap: 10 }}>
          <span ref={timeRef} className="idea-rec-time">0:00.0</span>
          <span className="muted small ellipsis">
            {state === 'opening' ? 'Opening the microphone…' : state === 'rolling' ? 'Recording — play it, sing it' : state === 'saving' ? <><Spinner size={10} /> Keeping the idea… {pct > 0 && pct < 1 ? `${Math.round(pct * 100)}%` : ''}</> : 'It did not reach the server. The recording is still here.'}
          </span>
        </div>
        <span className="idea-meter"><span ref={meterRef} /></span>
      </div>
      {state === 'failed' ? <>
        <button type="button" className="btn ghost" onClick={() => { pending.current = null; onClose() }}>Discard</button>
        <button type="button" className="btn primary" onClick={() => void send()}><Icon name="upload" size={15} />Send again</button>
      </> : <>
        <button type="button" className="btn ghost" onClick={() => void stop(false)} disabled={state !== 'rolling'}>Discard</button>
        <button type="button" className="btn primary" onClick={() => void stop(true)} disabled={state !== 'rolling'}><Icon name="square" size={13} className="solid" />Stop and keep</button>
      </>}
    </section>
  )
}

// ---- one idea ----

type PartJob = 'download' | 'save' | 'cut'

function IdeaRow({ x, bands, onChange, onSong }: { x: Idea; bands: Band[]; onChange: () => void; onSong: () => void }) {
  const { toast } = useToast()
  const audio = useRef<HTMLAudioElement | null>(null)
  const [playing, setPlaying] = useState(false)
  const [at, setAt] = useState<number | null>(null)
  const [editing, setEditing] = useState(false)
  const [name, setName] = useState(x.name)
  const [del, setDel] = useState(false)
  const [sel, setSel] = useState<Sel | null>(null) // a part, to play, save or keep
  const [focusSel, setFocusSel] = useState(0)
  const [job, setJob] = useState<PartJob | null>(null)
  const [cut, setCut] = useState<Sel | null>(null)
  const stopAt = useRef<number | null>(null) // playing the part: where it pauses
  const sound = useRef<Promise<AudioBuffer> | null>(null)
  const total = x.duration_ms / 1000
  useEffect(() => setName(x.name), [x.name])
  useEffect(() => () => { const a = audio.current; if (a) { a.pause(); if (current === a) current = null } }, [])

  const el = () => {
    if (!audio.current) {
      const a = new Audio(`${V1}/ideas/${x.id}/audio`)
      a.preload = 'metadata'
      a.onplay = () => { if (current && current !== a) current.pause(); current = a; setPlaying(true) }
      a.onpause = () => { setPlaying(false); setAt(a.currentTime) }
      a.onended = () => { stopAt.current = null; setPlaying(false); setAt(null); a.currentTime = 0 }
      audio.current = a
    }
    return audio.current
  }
  const toggle = () => {
    const a = el()
    stopAt.current = null
    if (playing) { a.pause(); return }
    audioSession('playback')
    a.play().catch(e => toast(errMsg(e), 'err'))
  }
  const seek = (sec: number) => {
    const a = el()
    stopAt.current = null
    a.currentTime = sec
    setAt(sec)
  }
  const time = () => (playing && audio.current ? audio.current.currentTime : at)
  // the part plays from its start and pauses at its end (the playhead stays there)
  const partPlaying = playing && stopAt.current !== null
  const playPart = () => {
    if (!sel) return
    const a = el()
    if (partPlaying) { stopAt.current = null; a.pause(); return }
    stopAt.current = sel.b
    a.currentTime = sel.a
    setAt(sel.a)
    if (!playing) { audioSession('playback'); a.play().catch(e => toast(errMsg(e), 'err')) }
  }
  useEffect(() => {
    if (!playing) return
    let raf = 0
    const frame = () => {
      const a = audio.current, end = stopAt.current
      if (a && end !== null && a.currentTime >= end) { stopAt.current = null; a.pause(); a.currentTime = end }
      raf = requestAnimationFrame(frame)
    }
    raf = requestAnimationFrame(frame)
    return () => cancelAnimationFrame(raf)
  }, [playing])

  // ---- the part as sound: the recording fetched and decoded once, then sliced ----
  const decoded = () => {
    if (!sound.current) {
      const p = fetch(`${V1}/ideas/${x.id}/audio`, { credentials: 'same-origin' })
        .then(r => { if (!r.ok) throw new Error(`The recording did not load (${r.status})`); return r.arrayBuffer() })
        .then(b => decode(audioContext(), b))
      p.catch(() => { if (sound.current === p) sound.current = null })
      sound.current = p
    }
    return sound.current
  }
  const part = async (s: Sel) => {
    const b = await decoded()
    const channels = sliceAudio(b, s)
    const len = channels[0]?.length || 0
    if (!len) throw new Error('Nothing in that part')
    return { wav: encodeWav(channels, b.sampleRate), meta: { duration_ms: Math.round(len / b.sampleRate * 1000), sample_rate: b.sampleRate, channels: channels.length, peaks: peaksOf(channels, b.sampleRate) } }
  }
  const run = async (j: PartJob, fn: () => Promise<void>) => {
    if (job) return
    setJob(j)
    try { await fn() } catch (e) { toast(errMsg(e), 'err') } finally { setJob(null) }
  }
  const band = x.band_id ? { band_id: x.band_id } : {}
  const downloadPart = () => { const s = sel; if (s) void run('download', async () => saveBlob((await part(s)).wav, partFile(x.name, s))) }
  const savePart = () => {
    const s = sel
    if (!s) return
    void run('save', async () => {
      const p = await part(s)
      const idea = await uploadAudio(`${V1}/ideas`, p.wav, 'idea.wav', {
        ...p.meta, name: `${x.name} (${rangeLabel(s)})`, source: 'upload', device: x.device, client_id: clientId(),
        created_at: new Date(Date.parse(x.created_at) + s.a * 1000).toISOString(), ...band,
      }) as Idea
      toast(`Kept “${idea.name}” as a new idea`, 'ok')
      onChange()
    })
  }
  // the part replaces the idea: the new one first, and only then the old one goes
  const cutTo = (s: Sel) => void run('cut', async () => {
    const p = await part(s)
    await uploadAudio(`${V1}/ideas`, p.wav, 'idea.wav', { ...p.meta, name: x.name, source: x.source || 'upload', device: x.device, client_id: clientId(), created_at: x.created_at, ...band })
    audio.current?.pause()
    try { await api.del(`${V1}/ideas/${x.id}`) } catch (e) { toast(`The part is kept, but the whole recording is still here too: ${errMsg(e)}`, 'err'); onChange(); return }
    toast(`Cut to ${(s.b - s.a).toFixed(1)} s`, 'ok')
    onChange()
  })
  const selectAll = () => { if (total > 0) setSel({ a: 0, b: total }) }

  const saveName = async () => {
    setEditing(false)
    const n = name.trim()
    if (!n || n === x.name) { setName(x.name); return }
    try { await api.put(`${V1}/ideas/${x.id}`, { name: n }); onChange() } catch (e) { setName(x.name); toast(errMsg(e), 'err') }
  }
  const setBand = async (id: number | null) => {
    if (id === x.band_id) return
    try { await api.put(`${V1}/ideas/${x.id}`, { band_id: id }); onChange() } catch (e) { toast(errMsg(e), 'err') }
  }
  const bandColor = bands.find(b => b.id === x.band_id)?.color
  const ctx = useContextMenu()
  // the "…" menu; right-click on the row shows it at the pointer, with play first
  const items = (): MenuItem[] => [
    { label: 'Make it a song', icon: 'waveform', onSelect: onSong },
    { label: 'Rename', icon: 'edit', onSelect: () => setEditing(true) },
    { label: 'Band', icon: 'users', hint: x.band_name || undefined, submenu: [
      ...bands.map(b => ({ label: b.name, checked: x.band_id === b.id, onSelect: () => void setBand(b.id) })),
      ...(bands.length ? ['sep' as const] : []),
      { label: 'No band', checked: !x.band_id, onSelect: () => void setBand(null) },
    ] },
    { label: 'Download', icon: 'download', onSelect: () => { window.location.href = `${V1}/ideas/${x.id}/audio?download=1` } },
    'sep' as const,
    { label: 'Select a part…', icon: 'scissors', disabled: total <= 0, onSelect: () => { selectAll(); setFocusSel(n => n + 1) } },
    { label: 'Select all', icon: 'maximize', disabled: total <= 0, onSelect: selectAll },
    ...(sel ? [
      { label: 'Download part', icon: 'download', disabled: !!job, onSelect: downloadPart },
      { label: 'Save part as new idea', icon: 'plus', disabled: !!job, onSelect: savePart },
      { label: 'Keep only the part', icon: 'scissors', disabled: !!job, onSelect: () => setCut(sel) },
    ] : []),
    'sep' as const,
    { label: 'Delete', icon: 'trash', danger: true, onSelect: () => setDel(true) },
  ]
  const ico = (j: PartJob, name: string) => job === j ? <Spinner size={12} /> : <Icon name={name} size={14} />

  return (
    <div className="list-row idea-row" onContextMenu={e => { if (!editing) ctx.open(e) }}
      onKeyDown={e => { if (e.key === 'Escape' && sel && !editing && e.currentTarget.contains(e.target as Node)) { e.preventDefault(); setSel(null) } }}>
      <button type="button" className={`play-btn ${playing ? 'on' : ''}`} onClick={toggle} aria-label={playing ? 'Pause' : 'Play'}><Icon name={playing ? 'pause' : 'play'} size={16} /></button>
      <div className="grow min0">
        <div className="row" style={{ gap: 8 }}>
          {editing
            ? <input className="idea-name-input" value={name} onChange={e => setName(e.target.value)} autoFocus aria-label="Name" maxLength={120}
                onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); void saveName() } else if (e.key === 'Escape') { e.preventDefault(); setName(x.name); setEditing(false) } }}
                onBlur={() => void saveName()} />
            : <button type="button" className="idea-name ellipsis" onClick={() => setEditing(true)} title="Rename">{x.name}</button>}
          {x.band_name && !editing && <span className={`band-tag ${bandColor ? 'tinted' : ''}`} style={bandColor ? { ['--band' as string]: bandColor } : undefined} title={`In ${x.band_name}`}>{x.band_name}</span>}
        </div>
        <div className="muted small">{fmtDateTime(x.created_at)} · {clock(x.duration_ms / 1000, false)}{x.device ? ` · ${x.device}` : ''}</div>
        <BarWave peaks={x.peaks} durationMs={x.duration_ms} time={time} live={playing} onSeek={seek} sel={sel} onSelect={setSel} />
        {sel && <SelBar sel={sel} total={total} onChange={setSel} onClear={() => setSel(null)} playhead={() => time() ?? 0} focus={focusSel}>
          <button type="button" className="btn sm" onClick={playPart} title={partPlaying ? 'Pause' : 'Play the part, then pause at its end'}><Icon name={partPlaying ? 'pause' : 'play'} size={14} />{partPlaying ? 'Pause' : 'Play selection'}</button>
          <button type="button" className="btn sm" onClick={downloadPart} disabled={!!job} title="Save just this part (WAV)">{ico('download', 'download')}Download part</button>
          <button type="button" className="btn sm" onClick={savePart} disabled={!!job} title="A new idea with just this part; this one stays">{ico('save', 'plus')}Save as new idea</button>
          <button type="button" className="btn sm" onClick={() => setCut(sel)} disabled={!!job} title="Cut the idea down to this part">{ico('cut', 'scissors')}Keep only this</button>
        </SelBar>}
      </div>
      <button type="button" className="btn hide-mobile" onClick={onSong} title="Make it the first track of a new song"><Icon name="plus" size={15} />Make a song</button>
      <MenuButton label="Idea" align="right" width={220} items={items}><Icon name="more" size={16} /></MenuButton>
      {ctx.at && <ContextMenu at={ctx.at} width={220} onClose={ctx.close}
        items={[{ label: playing ? 'Pause' : 'Play', icon: playing ? 'pause' : 'play', onSelect: toggle }, 'sep' as const, ...items()]} />}
      {del && <Confirm title={`Delete “${x.name}”?`} message="The recording is gone for good." confirmLabel="Delete" onCancel={() => setDel(false)}
        onConfirm={async () => { setDel(false); audio.current?.pause(); try { await api.del(`${V1}/ideas/${x.id}`) } catch (e) { toast(errMsg(e), 'err') } announceChange(); onChange() }} />}
      {cut && <Confirm title={`Cut “${x.name}” to ${rangeLabel(cut)}?`} message="The rest of the recording is gone for good." confirmLabel="Cut" onCancel={() => setCut(null)}
        onConfirm={() => { const s = cut; setCut(null); cutTo(s) }} />}
    </div>
  )
}

// ---- the waveform as rounded bars, with a playhead ----

const BAR = 2.5 // bar width
const STEP = 4 // one bar every 4 px

/** Draws the peaks as separate rounded bars; `progress` 0..1 splits played (full colour) from the rest (dimmed), null = not started. */
function drawBars(c: HTMLCanvasElement, p: Uint8Array, width: number, height: number, progress: number | null) {
  const dpr = window.devicePixelRatio || 1
  const cw = Math.round(width * dpr), ch = Math.round(height * dpr)
  if (c.width !== cw) c.width = cw
  if (c.height !== ch) c.height = ch
  const g = c.getContext('2d')
  if (!g) return
  g.setTransform(dpr, 0, 0, dpr, 0, 0)
  g.clearRect(0, 0, width, height)
  const accent = getComputedStyle(c).getPropertyValue('--accent').trim() || '#e8590c'
  const mid = height / 2
  const n = Math.max(1, Math.floor((width - BAR) / STEP) + 1)
  const played = progress === null ? width : progress * width
  g.lineCap = 'round'
  g.lineWidth = BAR
  g.strokeStyle = accent
  for (let i = 0; i < n; i++) {
    const x = i * STEP + BAR / 2
    let m = 0
    if (p.length) {
      const a = Math.floor(i * p.length / n)
      const b = Math.max(a + 1, Math.floor((i + 1) * p.length / n))
      for (let j = a; j < b && j < p.length; j++) if (p[j] > m) m = p[j]
    }
    const h = Math.sqrt(m / 255) * (mid - BAR / 2)
    g.globalAlpha = x <= played ? 1 : 0.32
    g.beginPath()
    g.moveTo(x, mid - h)
    g.lineTo(x, mid + h)
    g.stroke()
  }
  g.globalAlpha = 1
  if (progress !== null) {
    const x = Math.max(0.75, Math.min(width - 0.75, progress * width))
    g.save()
    g.shadowColor = 'rgba(0, 0, 0, .45)'
    g.shadowBlur = 2
    g.fillStyle = '#fff'
    g.fillRect(x - 0.75, 0, 1.5, height)
    g.restore()
  }
}

/** An idea's waveform: rounded bars, the playhead while it plays (redrawn every frame), "elapsed / total"; a click seeks, a drag sideways selects a part. */
function BarWave({ peaks, durationMs, time, live, onSeek, sel, onSelect, height = 36 }: {
  peaks: string; durationMs: number; time: () => number | null; live: boolean; onSeek: (sec: number) => void; sel: Sel | null; onSelect: (s: Sel) => void; height?: number
}) {
  const wrap = useRef<HTMLDivElement>(null)
  const canvas = useRef<HTMLCanvasElement>(null)
  const label = useRef<HTMLSpanElement>(null)
  const [w, setW] = useState(0)
  const bytes = useMemo(() => peaksBytes(peaks), [peaks])
  const total = durationMs / 1000
  useLayoutEffect(() => {
    const el = wrap.current
    if (!el) return
    const ro = new ResizeObserver(() => setW(Math.max(40, Math.round(el.getBoundingClientRect().width))))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  useEffect(() => {
    const c = canvas.current
    if (!c || w <= 0) return
    const draw = () => {
      const t = time()
      drawBars(c, bytes, w, height, t === null || total <= 0 ? null : Math.min(1, t / total))
      if (label.current) label.current.textContent = `${clock(t ?? 0, false)} / ${clock(total, false)}`
    }
    draw()
    if (!live) return
    let raf = 0
    const frame = () => { draw(); raf = requestAnimationFrame(frame) }
    raf = requestAnimationFrame(frame)
    return () => cancelAnimationFrame(raf)
  }, [bytes, w, height, live, time, total])
  const drag = useRangeSelect({
    total, sel, onSelect, onClick: onSeek,
    timeAt: x => { const r = wrap.current!.getBoundingClientRect(); return (x - r.left) / r.width * total },
  })
  return (
    <div className="idea-wave-row">
      <div ref={wrap} className="idea-wave" style={{ height }} role="slider" aria-label="Position" aria-valuemin={0} aria-valuemax={Math.round(total)} aria-valuenow={Math.round(time() ?? 0)} tabIndex={-1}
        onPointerDown={e => { e.currentTarget.focus({ preventScroll: true }); drag.strip(e) }}>
        <canvas ref={canvas} style={{ width: w || '100%', height }} aria-hidden />
        {sel && total > 0 && <SelBand left={`${sel.a / total * 100}%`} width={`${(sel.b - sel.a) / total * 100}%`} onEdge={drag.edge} />}
      </div>
      <span ref={label} className="idea-time small muted">0:00 / {clock(total, false)}</span>
    </div>
  )
}

// ---- an idea becomes a song ----

function ToSong({ x, onClose }: { x: Idea; onClose: () => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const bands = useBands()
  const [f, setF] = useState({ title: x.name, instrument: 'other' as Instrument, tempo: settings?.tempo || 100, beats: settings?.beats || 4, beat_unit: 4, band_id: x.band_id || 0 })
  const [busy, setBusy] = useState(false)
  const tap = useTapTempo(bpm => setF(v => ({ ...v, tempo: bpm })))
  const save = async () => {
    setBusy(true)
    try {
      const so = await api.post<Song>(`${V1}/ideas/${x.id}/song`, { ...f, title: f.title.trim() || x.name, band_id: f.band_id || null })
      announceChange()
      onClose()
      navigate(`/songs/${so.id}`)
    } catch (e) {
      toast(errMsg(e), 'err')
      setBusy(false)
    }
  }
  return (
    <Modal title="Make it a song" onClose={onClose} footer={<>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" disabled={busy} onClick={() => void save()}>{busy ? <Spinner size={12} /> : null}Make the song</button>
    </>}>
      <form className="stack" onSubmit={e => { e.preventDefault(); void save() }}>
        <Field label="Name"><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} placeholder={x.name} autoFocus /></Field>
        <div className="two-col tight">
          <Field label="What is it?"><Select ariaLabel="Instrument" value={f.instrument} onChange={v => setF({ ...f, instrument: v as Instrument })} options={INSTRUMENTS.map(i => ({ value: i.value, label: i.label }))} /></Field>
          <Field label="Tempo (beats a minute)">
            <div className="row" style={{ gap: 6 }}>
              <input type="number" min={20} max={400} step={1} value={f.tempo} onChange={e => setF({ ...f, tempo: Number(e.target.value) })} style={{ width: 90 }} />
              <button type="button" className="btn" onClick={tap} title="Tap the beat a few times">Tap</button>
            </div>
          </Field>
        </div>
        <div className="two-col tight">
          <Field label="Beats in a bar">
            <Select ariaLabel="Time signature" value={`${f.beats}/${f.beat_unit}`} onChange={v => { const [b, u] = v.split('/').map(Number); setF({ ...f, beats: b, beat_unit: u }) }}
              options={METRES.map(v => ({ value: v, label: v }))} />
          </Field>
          <Field label="Band (optional)">
            <Select ariaLabel="Band" value={f.band_id ? String(f.band_id) : ''} onChange={v => setF(o => ({ ...o, band_id: Number(v) || 0 }))} placeholder="No band" clearLabel="No band"
              options={(bands || []).map(b => ({ value: String(b.id), label: b.name }))} createLabel="New band"
              onCreate={async name => { const b = await api.post<Band>(`${V1}/bands`, { name }); announceChange(); return String(b.id) }} />
          </Field>
        </div>
        <p className="muted small">The idea becomes the first track, from the very start of the song. Record the next ones on top of it; the idea leaves this list.</p>
        <button type="submit" hidden />
      </form>
    </Modal>
  )
}
