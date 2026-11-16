// One song: its tracks on a timeline, the transport (play, record, click),
// each track's mixer and takes, adding tracks from the microphone, a file,
// Pocket Files or Pocket Music, and mixing down.
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Instrument, Mix, Song, Take, Track } from '../lib/types'
import {
  Mic, Player, activeTake, audible, audioContext, audioSession, barBeat, cachedTake, clock, encodeWav, latencyMs, levelTake, loadTake, mixDown,
  peaksOf, placeTake, rememberTake, uploadAudio,
} from '../lib/engine'
import { useCrumb, announceChange } from '../components/Layout'
import { Confirm, Empty, Field, Loading, Modal, Segmented, Spinner, Switch, useIsMobile, useToast, errMsg, MenuButton, type MenuItem } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { Waveform } from '../components/Waveform'
import { ImportSheet } from '../components/ImportSheet'
import { SongForm } from './Songs'
import { ago, fmtBytes } from '../lib/format'

export const INSTRUMENTS: { value: Instrument; label: string; icon: string; color: string }[] = [
  { value: 'voice', label: 'Voice', icon: 'mic', color: '#e8590c' },
  { value: 'guitar', label: 'Guitar', icon: 'key-music', color: '#1c7ed6' },
  { value: 'bass', label: 'Bass', icon: 'waves', color: '#7048e8' },
  { value: 'keys', label: 'Keys', icon: 'grid', color: '#0ca678' },
  { value: 'drums', label: 'Drums', icon: 'disc', color: '#d6336c' },
  { value: 'strings', label: 'Strings', icon: 'music', color: '#f08c00' },
  { value: 'wind', label: 'Wind', icon: 'wind', color: '#1098ad' },
  { value: 'backing', label: 'Backing track', icon: 'layers', color: '#5c7cfa' },
  { value: 'other', label: 'Other', icon: 'waveform', color: '#868e96' },
]
export const instrumentOf = (i: string) => INSTRUMENTS.find(x => x.value === i) || INSTRUMENTS[INSTRUMENTS.length - 1]
const colorOf = (t: Track) => t.color || instrumentOf(t.instrument).color

type RecState = 'off' | 'opening' | 'rolling' | 'saving'

export default function SongPage() {
  const { id } = useParams()
  const sid = Number(id)
  const navigate = useNavigate()
  const { settings } = useApp()
  const { toast } = useToast()
  const mobile = useIsMobile()
  const [song, setSong] = useState<Song | null>(null)
  const [missing, setMissing] = useState(false)
  const [ready, setReady] = useState<Record<number, boolean>>({}) // take id → decoded
  const [playing, setPlaying] = useState(false)
  const [rec, setRec] = useState<RecState>('off')
  const [armed, setArmed] = useState<number | null>(null)
  const [pos, setPos] = useState(0) // where the playhead rests
  const [pxPerSec, setPx] = useState(0)
  const [sheet, setSheet] = useState<number | null>(null) // a track's own sheet
  const [adding, setAdding] = useState<null | { intent: 'record' | 'any' }>(null)
  const [editing, setEditing] = useState(false)
  const [mixing, setMixing] = useState(false)
  const [delSong, setDelSong] = useState(false)
  const player = useRef<Player | null>(null)
  const mic = useRef<Mic | null>(null)
  const recFrom = useRef(0)
  const tracks = useMemo(() => song?.tracks || [], [song])
  const tracksRef = useRef(tracks)
  tracksRef.current = tracks
  useCrumb(song?.title)

  const load = useCallback(async (open = false) => {
    try { setSong(await api.get<Song>(`${V1}/songs/${sid}${open ? '?open=1' : ''}`)) } catch { setMissing(true) }
  }, [sid])
  useEffect(() => { load(true) }, [load])

  // decode every playing take (once per page)
  useEffect(() => {
    for (const t of tracks) {
      const k = activeTake(t)
      if (!k || ready[k.id]) continue
      if (cachedTake(k.id)) { setReady(r => ({ ...r, [k.id]: true })); continue }
      loadTake(k.id).then(() => setReady(r => ({ ...r, [k.id]: true }))).catch(e => toast(errMsg(e), 'err'))
    }
  }, [tracks]) // eslint-disable-line react-hooks/exhaustive-deps
  // the armed track: the one picked, else the newest empty one
  useEffect(() => {
    if (armed && tracks.some(t => t.id === armed)) return
    const empty = [...tracks].reverse().find(t => !t.takes.length)
    setArmed(empty ? empty.id : null)
  }, [tracks]) // eslint-disable-line react-hooks/exhaustive-deps

  const getPlayer = () => { if (!player.current) player.current = new Player(); return player.current }
  useEffect(() => () => { player.current?.dispose(); mic.current?.close() }, [])
  // the mixer follows the tracks live
  useEffect(() => { player.current?.mixer(tracks) }, [tracks])

  const opts = () => ({ click: !!song?.click, tempo: song!.tempo, beats: song!.beats, clickSound: settings?.click_sound || 'click', clickVolume: settings?.click_volume ?? 70 })
  const allReady = tracks.every(t => { const k = activeTake(t); return !k || ready[k.id] })

  const play = () => {
    if (!song) return
    audioSession('playback')
    const p = getPlayer()
    p.play(tracks, pos, opts())
    setPlaying(true)
  }
  const stop = () => {
    const p = player.current
    if (rec === 'rolling') { void finishRecording(); return }
    if (p) { p.stop(); setPos(Math.max(0, p.position())) }
    setPlaying(false)
  }
  const toStart = () => { if (rec !== 'off') return; const wasPlaying = playing; player.current?.stop(); setPos(0); if (wasPlaying) { getPlayer().play(tracks, 0, opts()) } else player.current?.seek(0) }
  const seek = (sec: number) => {
    if (rec !== 'off') return
    const s = Math.max(0, sec)
    setPos(s)
    if (playing) getPlayer().play(tracks, s, opts())
    else player.current?.seek(s)
  }

  // ---- recording ----
  const record = async () => {
    if (!song || rec !== 'off') return
    const target = tracks.find(t => t.id === armed)
    if (!target) { setAdding({ intent: 'record' }); return }
    const p = getPlayer()
    try {
      setRec('opening')
      if (!mic.current) mic.current = await Mic.open(p.master, { stereo: !!settings?.stereo, monitor: !!settings?.monitor })
    } catch (e) {
      setRec('off')
      toast(micError(e), 'err')
      return
    }
    const bar = song.beats * 60 / song.tempo
    const from = Math.max(0, Math.floor((playing ? p.position() : pos) / bar + 1e-6) * bar)
    recFrom.current = from
    p.play(tracks.filter(t => t.id !== target.id), from - song.count_in * bar, { ...opts(), countInFrom: from })
    mic.current.start()
    setPlaying(true)
    setRec('rolling')
  }
  const finishRecording = async () => {
    const p = player.current, m = mic.current
    const target = tracksRef.current.find(t => t.id === armed)
    if (!p || !m || !song) return
    setRec('saving')
    const got = await m.stop()
    const firstSong = p.songTimeOf(got.startTime)
    p.stop()
    setPlaying(false)
    setPos(recFrom.current)
    p.seek(recFrom.current)
    const placed = placeTake(got.channels, got.rate, firstSong, recFrom.current, latencyMs(p.ctx, m.stream.getAudioTracks()[0]))
    // the microphone closes after every take: while it is open, an iPhone plays through the earpiece
    m.close()
    mic.current = null
    const len = placed.channels[0]?.length || 0
    if (!target || len < got.rate * 0.3) { setRec('off'); toast('Too short to keep: record at least a moment after the count-in', 'err'); return }
    try {
      const raised = settings?.level_takes !== false ? levelTake(placed.channels) : 0
      const wav = encodeWav(placed.channels, got.rate)
      const take = await uploadAudio(`${V1}/tracks/${target.id}/takes`, wav, 'take.wav', {
        duration_ms: Math.round(len / got.rate * 1000), offset_ms: placed.offsetMs, latency_ms: placed.latencyMs, sample_rate: got.rate, channels: placed.channels.length,
        peaks: peaksOf(placed.channels, got.rate), source: 'recorded', device: m.label,
      }) as Take
      const buf = audioContext().createBuffer(placed.channels.length, len, got.rate)
      placed.channels.forEach((c, i) => buf.copyToChannel(c as Float32Array<ArrayBuffer>, i))
      rememberTake(take.id, buf)
      setReady(r => ({ ...r, [take.id]: true }))
      await load()
      toast(`Take ${take.number} kept on ${target.name}${raised ? ` (made ${raised} dB louder)` : ''}`, 'ok', { label: 'Undo', run: async () => { await api.del(`${V1}/takes/${take.id}`).catch(() => {}); load() } })
      announceChange()
    } catch (e) {
      toast(errMsg(e), 'err')
    } finally {
      setRec('off')
    }
  }

  // ---- time spent (sent every minute while playing or recording) ----
  const active = useRef(0)
  useEffect(() => {
    if (!song) return
    const t = window.setInterval(() => {
      if (player.current?.playing && document.visibilityState === 'visible') active.current++
      if (active.current >= 60) { active.current -= 60; api.post(`${V1}/time`, { song_id: song.id, seconds: 60 }).catch(() => {}) }
    }, 1000)
    return () => {
      window.clearInterval(t)
      if (active.current >= 10) fetch(`${V1}/time`, { method: 'POST', keepalive: true, credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ song_id: song.id, seconds: active.current }) }).catch(() => {})
      active.current = 0
    }
  }, [song?.id]) // eslint-disable-line react-hooks/exhaustive-deps

  // ---- keep the screen on ----
  useEffect(() => {
    if (!settings?.keep_awake || !('wakeLock' in navigator)) return
    let lock: { release: () => Promise<void> } | null = null
    const get = () => { (navigator as unknown as { wakeLock: { request: (t: string) => Promise<typeof lock> } }).wakeLock.request('screen').then(l => { lock = l }).catch(() => {}) }
    get()
    const vis = () => { if (document.visibilityState === 'visible') get() }
    document.addEventListener('visibilitychange', vis)
    return () => { document.removeEventListener('visibilitychange', vis); lock?.release().catch(() => {}) }
  }, [settings?.keep_awake])

  // ---- keys ----
  const keys = useRef<(e: KeyboardEvent) => void>(() => {})
  keys.current = (e: KeyboardEvent) => {
    const t = e.target as HTMLElement
    if (t.closest('input, textarea, [contenteditable="true"]') || document.querySelector('.modal-backdrop, .menu, .sel-pop') || e.metaKey || e.ctrlKey || e.altKey) return
    if (e.key === ' ') { e.preventDefault(); if (playing || rec === 'rolling') stop(); else play() }
    else if (e.key === 'r') { e.preventDefault(); if (rec === 'rolling') stop(); else void record() }
    else if (e.key === 'Home' || e.key === '0') { e.preventDefault(); toStart() }
    else if (e.key === 'c' && song) { e.preventDefault(); saveSong({ click: !song.click }) }
  }
  useEffect(() => { const h = (e: KeyboardEvent) => keys.current(e); window.addEventListener('keydown', h); return () => window.removeEventListener('keydown', h) }, [])

  // ---- changes ----
  const saveSong = async (patch: Partial<Song>) => {
    if (!song) return
    setSong({ ...song, ...patch })
    try { setSong(await api.put<Song>(`${V1}/songs/${song.id}`, patch)) } catch (e) { toast(errMsg(e), 'err'); load() }
  }
  const saveTrack = async (t: Track, patch: Partial<Track>) => {
    setSong(s => s && { ...s, tracks: s.tracks!.map(x => x.id === t.id ? { ...x, ...patch } : x) })
    try { await api.put(`${V1}/tracks/${t.id}`, patch) } catch (e) { toast(errMsg(e), 'err'); load() }
  }
  const addTrack = async (instrument: Instrument): Promise<Track | null> => {
    try {
      const t = await api.post<Track>(`${V1}/songs/${sid}/tracks`, { instrument })
      await load()
      setArmed(t.id)
      return t
    } catch (e) { toast(errMsg(e), 'err'); return null }
  }

  // ---- the timeline's size ----
  const scroller = useRef<HTMLDivElement>(null)
  const [lost, setLost] = useState(false) // you scrolled away from the playing playhead
  const followNow = useRef<() => void>(() => {})
  const headW = mobile ? 112 : 196
  const [viewW, setViewW] = useState(600)
  useLayoutEffect(() => {
    const el = scroller.current
    if (!el) return
    const ro = new ResizeObserver(() => setViewW(el.clientWidth))
    ro.observe(el)
    setViewW(el.clientWidth)
    return () => ro.disconnect()
  }, [song?.id])
  useEffect(() => { if (!pxPerSec && viewW > headW && song) setPx(Math.max(12, Math.min(120, (viewW - headW) / Math.max(16, Math.min(40, song.length_ms / 1000 + 4))))) }, [viewW, song]) // eslint-disable-line react-hooks/exhaustive-deps
  const px = pxPerSec || 40
  const lengthSec = Math.max(song ? song.length_ms / 1000 : 0, pos) + Math.max(20, (viewW - headW) / px * 0.6)
  const laneW = Math.ceil(lengthSec * px)

  if (missing) return <Empty title="This song is not here" icon="music"><button type="button" className="btn" onClick={() => navigate('/songs')}>All songs</button></Empty>
  if (!song) return <Loading />
  const sheetTrack = tracks.find(t => t.id === sheet)
  const busy = rec !== 'off'
  const bar = song.beats * 60 / song.tempo

  const songMenu = (): MenuItem[] => [
    { label: 'Song details', icon: 'edit', onSelect: () => setEditing(true) },
    { label: song.starred ? 'Unstar' : 'Star', icon: 'star', onSelect: () => saveSong({ starred: !song.starred }) },
    'sep',
    { label: 'Mix down to one file', icon: 'mixer', onSelect: () => setMixing(true), disabled: !song.take_count },
    { label: 'Download the tracks (.zip)', icon: 'download', onSelect: () => { window.location.href = `${V1}/songs/${song.id}/stems.zip` }, disabled: !song.take_count },
    'sep',
    { label: 'Delete song', icon: 'trash', danger: true, onSelect: () => setDelSong(true) },
  ]

  return (
    <div className="studio">
      <div className="studio-head">
        <div className="grow min0">
          <h2 className="studio-title ellipsis">{song.starred && <Icon name="star" size={16} className="star-on" />}{song.title}</h2>
          <div className="studio-meta">
            {song.artist && <span>{song.artist}</span>}
            <button type="button" className="chip" onClick={() => setEditing(true)} title="Tempo">{Math.round(song.tempo * 10) / 10} bpm</button>
            <button type="button" className="chip" onClick={() => setEditing(true)} title="Time signature">{song.beats}/{song.beat_unit}</button>
            {song.key_name && <button type="button" className="chip" onClick={() => setEditing(true)} title="Key">{song.key_name}</button>}
          </div>
        </div>
        <MenuButton label="Song" items={songMenu} align="right" width={240}><Icon name="more" size={18} /></MenuButton>
      </div>

      <div className="tl-wrap">
        <div className="tl-zoom">
          {lost && <button type="button" className="btn ghost sm tl-follow" title="Back to the playhead" onClick={() => followNow.current()}><Icon name="target" size={14} />Follow</button>}
          <button type="button" className="btn ghost icon-only sm" title="Zoom out" aria-label="Zoom out" onClick={() => setPx(p => Math.max(6, (p || 40) / 1.5))}><Icon name="minus" size={15} /></button>
          <button type="button" className="btn ghost icon-only sm" title="Zoom in" aria-label="Zoom in" onClick={() => setPx(p => Math.min(400, (p || 40) * 1.5))}><Icon name="plus" size={15} /></button>
        </div>
        <div className="tl" ref={scroller}>
          <div className="tl-inner" style={{ width: headW + laneW }}>
            <div className="tl-row tl-ruler-row">
              <div className="tl-head tl-ruler-head" style={{ width: headW }}>
                <span className="muted small">{tracks.length} {tracks.length === 1 ? 'track' : 'tracks'}</span>
              </div>
              <Ruler width={laneW} px={px} bar={bar} beats={song.beats} onSeek={seek} />
            </div>
            {tracks.map(t => (
              <TrackRow key={t.id} t={t} tracks={tracks} headW={headW} laneW={laneW} px={px} armed={armed === t.id} recording={rec === 'rolling' && armed === t.id}
                loading={!!activeTake(t) && !ready[activeTake(t)!.id]} onArm={() => !busy && setArmed(armed === t.id ? null : t.id)} onOpen={() => setSheet(t.id)}
                onMute={() => saveTrack(t, { muted: !t.muted })} onSolo={() => saveTrack(t, { solo: !t.solo })} onSeek={seek} />
            ))}
            <div className="tl-row">
              <div className="tl-head tl-add" style={{ width: headW }}>
                <button type="button" className="btn ghost sm" onClick={() => setAdding({ intent: 'any' })} disabled={busy}><Icon name="plus" size={14} />Add a track</button>
              </div>
              <div className="lane lane-empty" style={{ width: laneW }} />
            </div>
            <Playhead player={player} playing={playing} rest={pos} px={px} headW={headW} rec={rec === 'rolling'} onLost={setLost} follow={followNow} />
          </div>
        </div>
      </div>

      {!tracks.length && <div className="studio-first">
        <Icon name="mic" size={22} />
        <div><b>Start with a first track.</b> Record your guitar or voice, or bring in a backing track to play along to. Put headphones on when there is something to listen to, so the microphone only hears you.</div>
        <button type="button" className="btn primary" onClick={() => setAdding({ intent: 'any' })}><Icon name="plus" size={15} />Add a track</button>
      </div>}

      <Transport song={song} player={player} playing={playing} rec={rec} armed={tracks.find(t => t.id === armed)} rest={pos} ready={allReady} recFrom={recFrom}
        onPlay={play} onStop={stop} onRecord={record} onStart={toStart} onClick={() => saveSong({ click: !song.click })} mic={mic} />

      <section className="card studio-notes">
        <div className="card-head"><h3>Notes</h3></div>
        <NotesBox key={song.id} value={song.notes} onSave={v => saveSong({ notes: v })} />
      </section>

      {sheetTrack && <TrackSheet t={sheetTrack} song={song} onClose={() => setSheet(null)} onSave={p => saveTrack(sheetTrack, p)} onReload={load}
        onDeleted={() => { setSheet(null); load() }} ready={ready} onReady={kid => setReady(r => ({ ...r, [kid]: true }))} />}
      {adding && <AddTrack intent={adding.intent} onClose={() => setAdding(null)} onAdd={addTrack} songId={sid} onImported={async kid => { setReady(r => ({ ...r, [kid]: true })); await load(); announceChange() }} onRecord={() => { setAdding(null); setTimeout(() => void record(), 50) }} />}
      {editing && <Modal title="Song details" onClose={() => setEditing(false)}><SongForm song={song} onSaved={s => { setSong(s); setEditing(false) }} /></Modal>}
      {mixing && <MixDown song={song} tracks={tracks} onClose={() => setMixing(false)} />}
      {delSong && <Confirm title={`Delete “${song.title}”?`} message="Its tracks and every take go too. Mixes you made stay (under Mixes)." confirmLabel="Delete" onCancel={() => setDelSong(false)}
        onConfirm={async () => { await api.del(`${V1}/songs/${song.id}`).catch(() => {}); announceChange(); navigate('/songs') }} />}
    </div>
  )
}

export function micError(e: unknown): string {
  const n = (e as { name?: string })?.name
  if (n === 'NotAllowedError' || n === 'SecurityError') return 'The microphone is blocked: allow it for this site in the browser (on iPhone: Settings → Safari → Microphone), then try again'
  if (n === 'NotFoundError' || n === 'OverconstrainedError') return 'No microphone was found'
  return errMsg(e)
}

// ---- the timeline ----

function Ruler({ width, px, bar, beats, onSeek }: { width: number; px: number; bar: number; beats: number; onSeek: (s: number) => void }) {
  const marks = []
  const barPx = bar * px
  const every = Math.max(1, Math.ceil(56 / barPx))
  const n = Math.ceil(width / barPx)
  for (let i = 0; i <= n; i++) {
    const x = i * barPx
    const label = i % every === 0
    marks.push(<div key={i} className={`rl-bar ${label ? 'lab' : ''}`} style={{ left: x }}>{label && <span>{i + 1}</span>}</div>)
    if (barPx / beats > 10) for (let b = 1; b < beats; b++) marks.push(<div key={`${i}.${b}`} className="rl-beat" style={{ left: x + b * barPx / beats }} />)
  }
  return <div className="ruler" style={{ width }} onPointerDown={e => { const r = e.currentTarget.getBoundingClientRect(); onSeek((e.clientX - r.left) / px) }}>{marks}</div>
}

function TrackRow({ t, tracks, headW, laneW, px, armed, recording, loading, onArm, onOpen, onMute, onSolo, onSeek }: {
  t: Track; tracks: Track[]; headW: number; laneW: number; px: number; armed: boolean; recording: boolean; loading: boolean
  onArm: () => void; onOpen: () => void; onMute: () => void; onSolo: () => void; onSeek: (s: number) => void
}) {
  const k = activeTake(t)
  const heard = audible(t, tracks)
  const color = colorOf(t)
  return (
    <div className={`tl-row ${heard ? '' : 'quiet'}`}>
      <div className="tl-head trk-head" style={{ width: headW, ['--trk' as string]: color }}>
        <button type="button" className="trk-name" onClick={onOpen} title={`${t.name}: volume, balance, takes`}>
          <span className="trk-ico"><Icon name={instrumentOf(t.instrument).icon} size={14} /></span>
          <span className="ellipsis">{t.name}</span>
        </button>
        <div className="trk-btns">
          <button type="button" className={`trk-btn ${t.muted ? 'on mute' : ''}`} onClick={onMute} title={t.muted ? 'Unmute' : 'Mute'} aria-pressed={t.muted}>M</button>
          <button type="button" className={`trk-btn ${t.solo ? 'on solo' : ''}`} onClick={onSolo} title={t.solo ? 'Stop soloing' : 'Solo: hear only this'} aria-pressed={t.solo}>S</button>
          <button type="button" className={`trk-btn arm ${armed ? 'on' : ''}`} onClick={onArm} title={armed ? 'The next recording goes here' : 'Record on this track'} aria-pressed={armed}><Icon name="record" size={10} /></button>
        </div>
      </div>
      <div className={`lane ${armed ? 'armed' : ''} ${recording ? 'recording' : ''}`} style={{ width: laneW }} onPointerDown={e => { if ((e.target as HTMLElement).closest('.take')) return; const r = e.currentTarget.getBoundingClientRect(); onSeek((e.clientX - r.left) / px) }}>
        {k && <div className="take" style={{ left: k.offset_ms / 1000 * px, width: Math.max(4, k.duration_ms / 1000 * px), ['--trk' as string]: color }} onClick={onOpen} title={`Take ${k.number}`}>
          <Waveform peaks={k.peaks} pxPerSec={px} width={Math.min(Math.max(4, Math.round(k.duration_ms / 1000 * px)), 16000)} height={46} color={color} />
          <span className="take-tag">{t.takes.length > 1 ? `take ${k.number}` : ''}{loading && <Spinner size={10} />}</span>
        </div>}
        {!k && !recording && <span className="lane-hint">{armed ? 'Press record' : 'Empty'}</span>}
      </div>
    </div>
  )
}

/** The line where the song is, moving while it plays (its own frames). The view turns the page when the line reaches the edge,
 * but once you scroll the timeline yourself it lets you look around: it follows again when the line is back in view (and you've
 * stopped scrolling), on Play or a seek, or with the Follow button. */
function Playhead({ player, playing, rest, px, headW, rec, onLost, follow }: {
  player: React.RefObject<Player | null>; playing: boolean; rest: number; px: number; headW: number; rec: boolean
  onLost: (lost: boolean) => void; follow: React.RefObject<() => void>
}) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const place = (s: number) => { el.style.transform = `translateX(${headW + Math.max(0, s) * px}px)` }
    if (!playing) { place(rest); return }
    let raf = 0
    const scroller = el.closest('.tl') as HTMLElement | null
    let expected = scroller?.scrollLeft ?? 0 // where we last put the view; anything else is the user
    let free = false
    let userAt = 0
    let lost = false
    const setLost = (v: boolean) => { if (v !== lost) { lost = v; onLost(v) } }
    const onScroll = () => {
      if (!scroller || Math.abs(scroller.scrollLeft - expected) <= 2) return
      free = true
      userAt = performance.now()
      expected = scroller.scrollLeft
    }
    scroller?.addEventListener('scroll', onScroll, { passive: true })
    follow.current = () => { free = false }
    const frame = () => {
      const s = player.current?.position() ?? rest
      place(s)
      if (scroller && s > 0) {
        const x = headW + s * px
        const inView = x >= scroller.scrollLeft + headW && x <= scroller.scrollLeft + scroller.clientWidth - 30
        if (free && inView && performance.now() - userAt > 1200) free = false
        if (!free && !inView) {
          scroller.scrollLeft = x - headW - (scroller.clientWidth - headW) * 0.2
          expected = scroller.scrollLeft
        }
        setLost(free && !inView)
      }
      raf = requestAnimationFrame(frame)
    }
    raf = requestAnimationFrame(frame)
    return () => {
      cancelAnimationFrame(raf)
      scroller?.removeEventListener('scroll', onScroll)
      follow.current = () => {}
      setLost(false)
    }
  }, [playing, rest, px, headW, player, onLost, follow])
  return <div ref={ref} className={`playhead ${rec ? 'rec' : ''}`} />
}

// ---- the transport ----

function Transport({ song, player, playing, rec, armed, rest, ready, recFrom, onPlay, onStop, onRecord, onStart, onClick, mic }: {
  song: Song; player: React.RefObject<Player | null>; playing: boolean; rec: RecState; armed?: Track; rest: number; ready: boolean; recFrom: React.RefObject<number>
  onPlay: () => void; onStop: () => void; onRecord: () => void; onStart: () => void; onClick: () => void; mic: React.RefObject<Mic | null>
}) {
  const clockRef = useRef<HTMLSpanElement>(null)
  const barRef = useRef<HTMLSpanElement>(null)
  const countRef = useRef<HTMLDivElement>(null)
  const meterRef = useRef<HTMLSpanElement>(null)
  useEffect(() => {
    const show = (s: number) => {
      if (clockRef.current) clockRef.current.textContent = clock(Math.max(0, s))
      if (barRef.current) barRef.current.textContent = barBeat(Math.max(0, s), song.tempo, song.beats)
      const c = countRef.current
      if (c) {
        const left = recFrom.current - s
        if (rec === 'rolling' && left > 0) { c.textContent = String(Math.ceil(left / (60 / song.tempo) - 1e-6)); c.hidden = false } else c.hidden = true
      }
    }
    if (!playing) { show(rest); return }
    let raf = 0
    const frame = () => {
      show(player.current?.position() ?? rest)
      if (meterRef.current && mic.current && rec === 'rolling') meterRef.current.style.transform = `scaleX(${Math.min(1, Math.sqrt(mic.current.level()))})`
      raf = requestAnimationFrame(frame)
    }
    raf = requestAnimationFrame(frame)
    return () => cancelAnimationFrame(raf)
  }, [playing, rest, rec, song.tempo, song.beats, player, mic, recFrom])
  const recording = rec === 'rolling'
  return (
    <>
      <div ref={countRef} className="countin" hidden aria-live="assertive" />
      <div className={`transport ${recording ? 'rec' : ''}`}>
        <div className="tp-time">
          <span ref={clockRef} className="tp-clock">0:00.0</span>
          <span className="tp-sub"><span ref={barRef}>1.1</span>{recording ? <span className="tp-rec">● REC {armed?.name}</span> : rec === 'saving' ? <span className="tp-rec saving"><Spinner size={10} /> Keeping the take…</span> : rec === 'opening' ? <span className="muted">Opening the microphone…</span> : armed ? <span className="muted ellipsis">Records on {armed.name}</span> : null}</span>
          {recording && <span className="tp-meter"><span ref={meterRef} /></span>}
        </div>
        <div className="tp-btns">
          <button type="button" className="tp-btn" onClick={onStart} disabled={rec !== 'off'} title="To the start (0)" aria-label="To the start"><Icon name="skip-back" size={20} className="solid" /></button>
          {playing || recording
            ? <button type="button" className="tp-btn tp-play" onClick={onStop} title="Stop (space)" aria-label="Stop"><Icon name="square" size={18} className="solid" /></button>
            : <button type="button" className="tp-btn tp-play" onClick={onPlay} disabled={rec !== 'off' || !ready} title={ready ? 'Play (space)' : 'Loading the takes…'} aria-label="Play">{ready ? <Icon name="play" size={22} className="play" /> : <Spinner size={18} />}</button>}
          <button type="button" className={`tp-btn recbtn ${recording ? 'on' : ''}`} onClick={recording ? onStop : onRecord} disabled={rec === 'saving' || rec === 'opening' || (playing && !recording) || !ready} title="Record (R)" aria-label={recording ? 'Stop recording' : 'Record'}><Icon name="record" size={22} /></button>
          <button type="button" className={`tp-btn ${song.click ? 'lit' : ''}`} onClick={onClick} title={song.click ? 'Click on while recording (C)' : 'Click off (C) — the count-in still clicks'} aria-pressed={song.click} aria-label="Click"><Icon name="metronome" size={20} /></button>
        </div>
      </div>
    </>
  )
}

function NotesBox({ value, onSave }: { value: string; onSave: (v: string) => void }) {
  const [v, setV] = useState(value)
  const last = useRef(value)
  useEffect(() => {
    if (v === last.current) return
    const t = window.setTimeout(() => { last.current = v; onSave(v) }, 900)
    return () => window.clearTimeout(t)
  }, [v]) // eslint-disable-line react-hooks/exhaustive-deps
  return <textarea className="notes" rows={4} value={v} onChange={e => setV(e.target.value)} onBlur={() => { if (v !== last.current) { last.current = v; onSave(v) } }} placeholder="Lyrics, chords, what to try next…" />
}

// ---- one track: mixer and takes ----

function TrackSheet({ t, song, onClose, onSave, onReload, onDeleted, ready, onReady }: {
  t: Track; song: Song; onClose: () => void; onSave: (p: Partial<Track>) => void; onReload: () => Promise<void>; onDeleted: () => void
  ready: Record<number, boolean>; onReady: (id: number) => void
}) {
  const { toast } = useToast()
  const [name, setName] = useState(t.name)
  const [del, setDel] = useState<'track' | Take | null>(null)
  const [vol, setVol] = useState(t.volume)
  const [pan, setPan] = useState(t.pan)
  useEffect(() => { setVol(t.volume); setPan(t.pan) }, [t.volume, t.pan])
  const k = activeTake(t)
  const pick = async (take: Take) => {
    onSave({ take_id: take.id })
    if (!ready[take.id]) loadTake(take.id).then(() => onReady(take.id)).catch(e => toast(errMsg(e), 'err'))
  }
  const nudge = async (take: Take, ms: number) => {
    try { await api.put(`${V1}/takes/${take.id}`, { offset_ms: take.offset_ms + ms }); await onReload() } catch (e) { toast(errMsg(e), 'err') }
  }
  const like = async (take: Take) => { try { await api.put(`${V1}/takes/${take.id}`, { liked: !take.liked }); await onReload() } catch (e) { toast(errMsg(e), 'err') } }
  return (
    <Modal title={<span className="row" style={{ gap: 8 }}><span className="trk-ico" style={{ ['--trk' as string]: colorOf(t) }}><Icon name={instrumentOf(t.instrument).icon} size={14} /></span>{t.name}</span>} onClose={onClose}>
      <div className="stack">
        <div className="two-col tight">
          <Field label="Name"><input value={name} onChange={e => setName(e.target.value)} onBlur={() => name.trim() && name !== t.name && onSave({ name: name.trim() })} /></Field>
          <Field label="Instrument"><Select ariaLabel="Instrument" value={t.instrument} onChange={v => onSave({ instrument: v as Instrument })} options={INSTRUMENTS.map(i => ({ value: i.value, label: i.label }))} /></Field>
        </div>
        <Field label={<span className="row spread">Volume <span className="muted small">{vol}%</span></span>}>
          <input type="range" min={0} max={150} step={1} value={vol} onChange={e => { const v = Number(e.target.value); setVol(v); onSave({ volume: v }) }} />
        </Field>
        <Field label={<span className="row spread">Balance <span className="muted small">{pan === 0 ? 'middle' : pan < 0 ? `${-pan}% left` : `${pan}% right`}</span></span>}>
          <input type="range" min={-100} max={100} step={5} value={pan} onChange={e => { const v = Number(e.target.value); setPan(v); onSave({ pan: v }) }} onDoubleClick={() => { setPan(0); onSave({ pan: 0 }) }} />
        </Field>
        <div className="row wrap">
          <Switch checked={t.muted} onChange={v => onSave({ muted: v })} label="Mute" /><span className="small">Mute</span>
          <span style={{ width: 16 }} />
          <Switch checked={t.solo} onChange={v => onSave({ solo: v })} label="Solo" /><span className="small">Solo</span>
        </div>

        <div>
          <h4 className="sheet-h">Takes {t.takes.length > 0 && <span className="muted small">— the one picked plays</span>}</h4>
          {!t.takes.length && <p className="muted small">Nothing recorded on this track yet.</p>}
          <ul className="takes">
            {[...t.takes].reverse().map(take => (
              <li key={take.id} className={`take-row ${take.id === t.take_id ? 'cur' : ''}`}>
                <button type="button" className="take-pick" onClick={() => pick(take)} aria-pressed={take.id === t.take_id} title="Play this take">
                  <span className={`radio ${take.id === t.take_id ? 'on' : ''}`} />
                  <span className="grow min0">
                    <b>Take {take.number}</b> <span className="muted small">{clock(take.duration_ms / 1000, false)} · {ago(take.created_at)}{take.source !== 'recorded' ? ` · from ${take.source === 'upload' ? 'a file' : take.source === 'files' ? 'Pocket Files' : 'Pocket Music'}` : ''}</span>
                    <Waveform peaks={take.peaks} pxPerSec={Math.max(4, 240 / Math.max(1, take.duration_ms / 1000))} width={240} height={22} color={colorOf(t)} className="take-mini" />
                  </span>
                </button>
                <button type="button" className={`btn ghost icon-only sm ${take.liked ? 'liked' : ''}`} onClick={() => like(take)} title={take.liked ? 'Unmark' : 'Mark as a good one'} aria-label="Good take"><Icon name="heart" size={15} /></button>
                <MenuButton label="Take" align="right" width={230} items={() => [
                  { label: 'Download', icon: 'download', onSelect: () => { window.location.href = `${V1}/takes/${take.id}/audio?download=1` } },
                  { label: 'Delete this take', icon: 'trash', danger: true, onSelect: () => setDel(take) },
                ]}><Icon name="more" size={16} /></MenuButton>
              </li>
            ))}
          </ul>
        </div>

        {k && <div>
          <h4 className="sheet-h">Line it up <span className="muted small">— take {k.number} starts at {(k.offset_ms / 1000).toFixed(3)} s</span></h4>
          <p className="muted small">If this take sounds early or late against the others, move it a little. (Measure your device once in Settings → Recording and new takes line up by themselves.)</p>
          <div className="row wrap">
            <button type="button" className="btn sm" onClick={() => nudge(k, -50)}>−50 ms</button>
            <button type="button" className="btn sm" onClick={() => nudge(k, -10)}>−10 ms</button>
            <button type="button" className="btn sm" onClick={() => nudge(k, 10)}>+10 ms</button>
            <button type="button" className="btn sm" onClick={() => nudge(k, 50)}>+50 ms</button>
          </div>
          <p className="muted small mt">{k.device && <>{k.device} · </>}{k.sample_rate ? `${(k.sample_rate / 1000).toFixed(1)} kHz` : ''}{k.channels === 2 ? ' stereo' : ' mono'} · {fmtBytes(k.size)}{k.latency_ms ? ` · ${k.latency_ms} ms delay taken out` : ''}</p>
        </div>}
        <div className="row"><button type="button" className="btn ghost danger-text sm" onClick={() => setDel('track')}><Icon name="trash" size={14} />Delete this track</button></div>
      </div>
      {del && <Confirm title={del === 'track' ? `Delete ${t.name}?` : `Delete take ${(del as Take).number}?`} message={del === 'track' ? `The track and its ${t.takes.length} ${t.takes.length === 1 ? 'take' : 'takes'} go.` : 'The recording is gone for good.'} confirmLabel="Delete"
        onCancel={() => setDel(null)} onConfirm={async () => {
          const d = del; setDel(null)
          try {
            if (d === 'track') { await api.del(`${V1}/tracks/${t.id}`); onDeleted() } else { await api.del(`${V1}/takes/${(d as Take).id}`); await onReload() }
            announceChange()
          } catch (e) { toast(errMsg(e), 'err') }
        }} />}
    </Modal>
  )
}

// ---- adding a track ----

function AddTrack({ intent, onClose, onAdd, songId, onImported, onRecord }: { intent: 'record' | 'any'; onClose: () => void; onAdd: (i: Instrument) => Promise<Track | null>; songId: number; onImported: (takeId: number) => Promise<void>; onRecord: () => void }) {
  const [inst, setInst] = useState<Instrument>('voice')
  const [how, setHow] = useState<'pick' | 'import'>('pick')
  const [busy, setBusy] = useState(false)
  return (
    <Modal title={intent === 'record' ? 'What will you record?' : 'Add a track'} onClose={onClose}>
      {how === 'pick' && <div className="stack">
        <div className="inst-grid">
          {INSTRUMENTS.map(i => (
            <button key={i.value} type="button" className={`inst ${inst === i.value ? 'on' : ''}`} style={{ ['--trk' as string]: i.color }} onClick={() => setInst(i.value)} aria-pressed={inst === i.value}>
              <span className="trk-ico"><Icon name={i.icon} size={16} /></span>{i.label}
            </button>
          ))}
        </div>
        <div className="add-ways">
          <button type="button" className="btn primary lg" disabled={busy} onClick={async () => { setBusy(true); const t = await onAdd(inst); setBusy(false); if (t) onRecord() }}><Icon name="record" size={14} />Record it now</button>
          {intent === 'any' && <button type="button" className="btn lg" disabled={busy} onClick={() => setHow('import')}><Icon name="upload" size={15} />Bring in a recording</button>}
          {intent === 'any' && <button type="button" className="btn ghost" disabled={busy} onClick={async () => { setBusy(true); await onAdd(inst); setBusy(false); onClose() }}>Just add the empty track</button>}
        </div>
        <p className="muted small">With headphones on, the microphone hears only you while the other tracks play. Without them it hears the speaker too.</p>
      </div>}
      {how === 'import' && <ImportSheet songId={songId} instrument={inst} onAdd={onAdd} onDone={async kid => { await onImported(kid); onClose() }} onBack={() => setHow('pick')} />}
    </Modal>
  )
}

// ---- mixing down ----

function MixDown({ song, tracks, onClose }: { song: Song; tracks: Track[]; onClose: () => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const [step, setStep] = useState<'ask' | 'mixing' | 'saving' | 'done'>('ask')
  const [name, setName] = useState(`${song.title} — mix`)
  const [mix, setMix] = useState<Mix | null>(null)
  const [pct, setPct] = useState(0)
  const heard = tracks.filter(t => audible(t, tracks) && activeTake(t))
  const go = async () => {
    try {
      setStep('mixing')
      const r = await mixDown(song, tracks)
      setStep('saving')
      const m = await uploadAudio(`${V1}/songs/${song.id}/mixes`, r.wav, 'mix.wav', { name, duration_ms: r.ms, sample_rate: r.rate, channels: 2, peaks: peaksOf(r.channels, r.rate), tracks: r.used }, setPct) as Mix
      setMix(m)
      setStep('done')
      announceChange()
    } catch (e) { toast(errMsg(e), 'err'); setStep('ask') }
  }
  const toFiles = async () => {
    if (!mix) return
    try { const r = await api.post<{ text: string; url: string; mix: Mix }>(`${V1}/mixes/${mix.id}/files`); setMix(r.mix); toast(r.text) } catch (e) { toast(errMsg(e), 'err') }
  }
  return (
    <Modal title="Mix down" onClose={onClose} size="sm">
      {step === 'ask' && <div className="stack">
        <p className="muted">The {heard.length} {heard.length === 1 ? 'track' : 'tracks'} you hear now — with their volume and balance, muted ones left out — become one stereo file (WAV).</p>
        <Field label="Name"><input value={name} onChange={e => setName(e.target.value)} /></Field>
        <div className="row"><button type="button" className="btn primary" onClick={go} disabled={!heard.length || !name.trim()}><Icon name="mixer" size={15} />Mix</button></div>
      </div>}
      {(step === 'mixing' || step === 'saving') && <div className="row" style={{ gap: 10, padding: '12px 0' }}><Spinner size={16} />{step === 'mixing' ? 'Mixing…' : `Saving… ${Math.round(pct * 100)}%`}</div>}
      {step === 'done' && mix && <div className="stack">
        <p><Icon name="check-circle" size={15} className="ok inline-ico" /> <b>{mix.name}</b> is ready ({clock(mix.duration_ms / 1000, false)}).</p>
        <audio controls src={`${V1}/mixes/${mix.id}/audio`} className="mix-audio" />
        <div className="row wrap">
          <a className="btn" href={`${V1}/mixes/${mix.id}/audio?download=1`}><Icon name="download" size={15} />Download</a>
          <button type="button" className="btn" onClick={toFiles} disabled={!settings?.files_via || !!mix.files_path} title={settings?.files_via ? `Into ${settings.files_folder || 'Jam'} in Pocket Files` : 'Needs Pocket Files (Settings → Connected apps)'}><Icon name="archive" size={15} />{mix.files_path ? 'In Pocket Files' : 'Save to Pocket Files'}</button>
          {mix.files_url && <a className="btn ghost" href={mix.files_url} target="_blank" rel="noopener">Open</a>}
        </div>
      </div>}
    </Modal>
  )
}

