// One song: its tracks on a timeline, the transport (play, record, click),
// each track's mixer and takes, adding tracks from the microphone, a file,
// Pocket Files or Pocket Music, and mixing down.
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { createPortal } from 'react-dom'
import { api, asOneChange, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Idea, Instrument, Mark, Mix, Song, Take, Track } from '../lib/types'
import {
  Mic, Player, attemptsOf, audible, audioContext, audioSession, barBeat, cachedTake, clock, encodeWav, latencyMs, levelTake, loadTake, mixDown,
  peaksOf, placeTake, playingTakes, rememberTake, renderTrack, uploadAudio,
} from '../lib/engine'
import { useCrumb, announceChange } from '../components/Layout'
import { Confirm, Empty, Field, Loading, Modal, Segmented, Spinner, Switch, useIsMobile, useToast, errMsg, MenuButton, ContextMenu, rectOf, type Anchor, type MenuItem } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { Waveform } from '../components/Waveform'
import { ImportSheet } from '../components/ImportSheet'
import { HistorySheet } from '../components/HistorySheet'
import { SongForm } from './Songs'
import { MarkEditor, MarkFlag, MarkList, holdToMenu, markColor } from '../components/Marks'
import { ago, fmtBytes } from '../lib/format'
import { clientId, partFile, rangeLabel, saveBlob, type Sel } from '../lib/part'
import { SelBand, SelBar, useRangeSelect } from '../components/Selection'

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

/** The tracks with a part (all its attempts) moved by shift ms and/or onto another track, where it lands on top. */
function movePartLocally(tracks: Track[], part: number, to: number, shift: number): Track[] {
  const moving = tracks.flatMap(t => t.takes.filter(k => k.part === part))
  if (!moving.length) return tracks
  const top = Math.max(0, ...(tracks.find(t => t.id === to)?.takes.filter(k => k.part !== part).map(k => k.layer) || [])) + 1
  return tracks.map(t => {
    const takes = t.takes.filter(k => k.part !== part)
    if (t.id === to) takes.push(...moving.map(k => ({ ...k, track_id: to, offset_ms: k.offset_ms + shift, layer: Math.max(k.layer, top) })))
    return { ...t, takes }
  })
}

/** Each song gets its own page: switching songs stops the one playing (its player is let go) and starts the next from the top. */
export default function SongRoute() {
  const { id } = useParams()
  return <SongPage key={id} />
}

function SongPage() {
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
  const [partSheet, setPartSheet] = useState<{ track: number; part: number } | null>(null) // a part's takes
  const [adding, setAdding] = useState<null | { intent: 'record' | 'any' }>(null)
  const [editing, setEditing] = useState(false)
  const [history, setHistory] = useState(false)
  const [mixing, setMixing] = useState<null | { range?: Sel }>(null)
  const [delSong, setDelSong] = useState(false)
  const player = useRef<Player | null>(null)
  const mic = useRef<Mic | null>(null)
  const recFrom = useRef(0)
  const tracks = useMemo(() => song?.tracks || [], [song])
  const marks = useMemo(() => song?.marks || [], [song])
  const marksRef = useRef(marks)
  marksRef.current = marks
  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; at: number; track: Track | null } | null>(null)
  const [markEdit, setMarkEdit] = useState<{ anchor: Anchor; mark?: Mark; draft: { at_ms: number; end_ms: number; track_id: number | null } } | null>(null)
  const menuAt = useRef(0)
  const tracksRef = useRef(tracks)
  tracksRef.current = tracks
  // a part of the song: to play (and loop), download, keep as a mix or an idea, or cut the song to
  const [sel, setSel] = useState<Sel | null>(null)
  const [focusSel, setFocusSel] = useState(0)
  const [partPlay, setPartPlay] = useState(false)
  const [loop, setLoop] = useState(false)
  const loopRef = useRef(loop)
  loopRef.current = loop
  const [job, setJob] = useState<null | { what: 'download' | 'idea' | 'cut'; text?: string }>(null)
  const [cutAsk, setCutAsk] = useState<Sel | null>(null)
  useCrumb(song?.title)

  const load = useCallback(async (open = false) => {
    try { setSong(await api.get<Song>(`${V1}/songs/${sid}${open ? '?open=1' : ''}`)) } catch { setMissing(true) }
  }, [sid])
  useEffect(() => { load(true) }, [load])

  // decode every playing take (once per page)
  useEffect(() => {
    for (const t of tracks) for (const k of playingTakes(t)) {
      if (ready[k.id]) continue
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
  const allReady = tracks.every(t => playingTakes(t).every(k => ready[k.id]))

  const play = () => {
    if (!song) return
    audioSession('playback')
    const p = getPlayer()
    p.play(tracks, pos, opts())
    setPlaying(true)
    setPartPlay(false)
  }
  const playFrom = (at: number) => {
    if (!song || rec !== 'off' || !allReady) return
    audioSession('playback')
    getPlayer().play(tracks, at, opts())
    setPos(at)
    setPlaying(true)
    setPartPlay(false)
  }
  const stop = () => {
    const p = player.current
    if (rec === 'rolling') { void finishRecording(); return }
    if (p) { p.stop(); setPos(Math.max(0, p.position())) }
    setPlaying(false)
    setPartPlay(false)
  }
  const toStart = () => { if (rec !== 'off') return; setPartPlay(false); const wasPlaying = playing; player.current?.stop(); setPos(0); if (wasPlaying) { getPlayer().play(tracks, 0, opts()) } else player.current?.seek(0) }
  const seek = (sec: number) => {
    if (rec !== 'off') return
    const s = Math.max(0, sec)
    setPos(s)
    setPartPlay(false)
    if (playing) getPlayer().play(tracks, s, opts())
    else player.current?.seek(s)
  }

  // ---- the part: plays from its start and stops at its end, or goes round again with Loop ----
  const playPart = () => {
    if (!song || !sel || rec !== 'off' || !allReady) return
    if (partPlay && playing) { stop(); return }
    audioSession('playback')
    getPlayer().play(tracks, sel.a, opts())
    setPos(sel.a)
    setPlaying(true)
    setPartPlay(true)
  }
  useEffect(() => {
    if (!playing || !partPlay || !sel || !song) return
    let raf = 0
    const frame = () => {
      const p = player.current
      if (p && p.position() >= sel.b) {
        if (loopRef.current) p.play(tracksRef.current, sel.a, opts())
        else { p.stop(); p.seek(sel.b); setPos(sel.b); setPlaying(false); setPartPlay(false); return }
      }
      raf = requestAnimationFrame(frame)
    }
    raf = requestAnimationFrame(frame)
    return () => cancelAnimationFrame(raf)
  }, [playing, partPlay, sel, song]) // eslint-disable-line react-hooks/exhaustive-deps

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
    const here = Math.max(0, playing ? p.position() : pos)
    // no count-in: it records from where the song is, at once; with one, from the start of that bar after the clicks
    const from = song.count_in > 0 ? Math.max(0, Math.floor(here / bar + 1e-6) * bar) : here
    recFrom.current = from
    // you hear everything, but not the part on this track you start recording over (you're playing it again)
    const heard = tracks.map(t => t.id !== target.id ? t : { ...t, takes: t.takes.filter(k => !(k.plays && k.offset_ms / 1000 <= from + 0.05 && (k.offset_ms + k.duration_ms) / 1000 > from)) })
    if (song.count_in > 0) p.play(heard, from - song.count_in * bar, { ...opts(), countInFrom: from })
    else p.play(heard, from, opts())
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
    if (!target || len < got.rate * 0.3) { setRec('off'); toast('Too short to keep: record for at least a moment', 'err'); return }
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
      const again = take.part !== take.id
      toast(`${again ? `Another try at that part of ${target.name} (take ${take.number}) — click the part to pick between them` : `Take ${take.number} kept on ${target.name}`}${raised ? ` (made ${raised} dB louder)` : ''}`, 'ok', { label: 'Undo', run: async () => { await api.del(`${V1}/takes/${take.id}`).catch(() => {}); load() } })
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
    if (t.closest('input, textarea, [contenteditable="true"]') || document.querySelector('.modal-backdrop, .menu, .sel-pop')) return
    if ((e.metaKey || e.ctrlKey) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 'z') { e.preventDefault(); undoLast(); return }
    if (e.metaKey || e.ctrlKey || e.altKey) return
    if (e.key === 'Escape' && sel) { e.preventDefault(); setSel(null); return }
    if (e.key === ' ') { e.preventDefault(); if (playing || rec === 'rolling') stop(); else play() }
    else if (e.key === 'r') { e.preventDefault(); if (rec === 'rolling') stop(); else void record() }
    else if (e.key === 'Home' || e.key === '0') { e.preventDefault(); toStart() }
    else if (e.key === 'c' && song) { e.preventDefault(); saveSong({ click: !song.click }) }
    else if (e.key === 'x' && tracks.length) { e.preventDefault(); toggleMixer() }
    else if (e.key === 'm' && song && !markEdit) { e.preventDefault(); noteAtPlayhead() }
  }
  useEffect(() => { const h = (e: KeyboardEvent) => keys.current(e); window.addEventListener('keydown', h); return () => window.removeEventListener('keydown', h) }, [])

  // ---- changes ----
  const saveSong = async (patch: Partial<Song>) => {
    if (!song) return
    setSong({ ...song, ...patch })
    try { setSong(await api.put<Song>(`${V1}/songs/${song.id}`, patch)) } catch (e) { toast(errMsg(e), 'err'); load() }
  }
  // ---- notes on the timeline ----
  const sortMarks = (l: Mark[]) => [...l].sort((a, b) => a.at_ms - b.at_ms || a.id - b.id)
  const openMenu = (x: number, y: number, at: number, track: Track | null) => {
    if (Date.now() - menuAt.current < 800) return // a phone's own long-press menu and ours, once
    menuAt.current = Date.now()
    setCtxMenu({ x, y, at: Math.max(0, at), track })
  }
  const newMark = (anchor: Anchor, at: number, track: Track | null) => setMarkEdit({ anchor, draft: { at_ms: Math.round(at * 1000), end_ms: 0, track_id: track?.id ?? null } })
  const noteAtPlayhead = () => {
    const at = playing ? player.current?.position() ?? pos : pos
    const el = document.querySelector('.tl .playhead')
    const r = el ? rectOf(el) : { x: window.innerWidth / 2, y: 200, w: 0, h: 0 }
    newMark({ x: r.x - 10, y: r.y, w: 20, h: 30 }, at, null)
  }
  const saveMark = async (v: { text: string; at_ms: number; end_ms: number; track_id: number | null; color: string }, m?: Mark): Promise<boolean> => {
    if (!song) return false
    try {
      const body = { ...v, track_id: v.track_id ?? 0 }
      const out = m ? await api.put<Mark>(`${V1}/marks/${m.id}`, body) : await api.post<Mark>(`${V1}/songs/${song.id}/marks`, body)
      setSong(s => s && { ...s, marks: sortMarks([...(s.marks || []).filter(x => x.id !== out.id), out]) })
      return true
    } catch (e) { toast(errMsg(e), 'err'); return false }
  }
  const changeMark = async (m: Mark, patch: Partial<Mark>) => {
    setSong(s => s && { ...s, marks: sortMarks((s.marks || []).map(x => x.id === m.id ? { ...x, ...patch } : x)) })
    try { await api.put(`${V1}/marks/${m.id}`, patch) } catch (e) { toast(errMsg(e), 'err'); load() }
  }
  const deleteMark = async (m: Mark) => {
    setSong(s => s && { ...s, marks: (s.marks || []).filter(x => x.id !== m.id) })
    setMarkEdit(null)
    try { await api.del(`${V1}/marks/${m.id}`) } catch (e) { toast(errMsg(e), 'err'); load() }
  }
  /** Delete a note, with Undo on the toast (it comes back as it was). */
  const removeMark = (m: Mark) => {
    void deleteMark(m)
    toast('Note deleted', 'ok', { label: 'Undo', run: () => { void saveMark({ text: m.text, at_ms: m.at_ms, end_ms: m.end_ms, track_id: m.track_id, color: m.color }) } })
  }
  const openMark = (m: Mark, anchor: Anchor) => setMarkEdit({ anchor, mark: m, draft: { at_ms: m.at_ms, end_ms: m.end_ms, track_id: m.track_id } })
  const colorOfMark = (m: Mark) => markColor(m, tracks, colorOf)
  const laneEvents = (track: Track | null) => ({
    onContextMenu: (e: React.MouseEvent<HTMLElement>) => {
      if (e.shiftKey) return
      e.preventDefault()
      const r = e.currentTarget.getBoundingClientRect()
      openMenu(e.clientX, e.clientY, (e.clientX - r.left) / px, track)
    },
    onHold: (e: React.PointerEvent<HTMLElement>) => {
      const r = e.currentTarget.getBoundingClientRect()
      holdToMenu(e, (x, y) => openMenu(x, y, (x - r.left) / px, track))
    },
  })
  const ctxItems = (c: NonNullable<typeof ctxMenu>): MenuItem[] => {
    const a = { x: c.x - 10, y: c.y - 6, w: 20, h: 12 }
    const where = `${clock(c.at)} · bar ${barBeat(c.at, song!.tempo, song!.beats)}`
    return [
      { heading: where },
      ...(c.track ? [{ label: `Add a note on ${c.track.name}`, icon: 'message', onSelect: () => newMark(a, c.at, c.track) }] : []),
      { label: c.track ? 'Add a note for the whole song' : 'Add a note here', icon: 'flag', hint: c.track ? undefined : 'M', onSelect: () => newMark(a, c.at, null) },
      'sep',
      { label: 'Play from here', icon: 'play', disabled: rec !== 'off' || !allReady, onSelect: () => { if (playing) seek(c.at); else playFrom(c.at) } },
      'sep',
      ...selectItems(),
    ]
  }

  // ---- the mixer: a fader, balance, mute and solo per track, heard at once, saved a moment after you stop moving ----
  const [mixerOpen, setMixerOpen] = useState(() => localStorage.getItem('pjam-mixer') === '1')
  const toggleMixer = () => setMixerOpen(v => { localStorage.setItem('pjam-mixer', v ? '0' : '1'); return !v })
  const pending = useRef<Record<number, { timer: number; patch: Partial<Track> }>>({})
  const liveTrack = (t: Track, patch: Partial<Track>) => {
    setSong(s => s && { ...s, tracks: s.tracks!.map(x => x.id === t.id ? { ...x, ...patch } : x) })
    const cur = pending.current[t.id]
    if (cur) window.clearTimeout(cur.timer)
    const merged = { ...(cur?.patch || {}), ...patch }
    pending.current[t.id] = { patch: merged, timer: window.setTimeout(() => {
      delete pending.current[t.id]
      api.put(`${V1}/tracks/${t.id}`, merged).catch(e => { toast(errMsg(e), 'err'); load() })
    }, 300) }
  }
  useEffect(() => () => {
    // leaving with a fader just moved: still save it
    for (const [id, p] of Object.entries(pending.current)) {
      window.clearTimeout(p.timer)
      fetch(`${V1}/tracks/${id}`, { method: 'PUT', keepalive: true, credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(p.patch) }).catch(() => {})
    }
  }, [])

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
  const songLen = song ? song.length_ms / 1000 : 0
  const ruler = useRef<HTMLDivElement>(null)
  const drag = useRangeSelect({
    total: songLen, sel, onSelect: setSel, onClick: seek,
    timeAt: x => (x - (ruler.current?.getBoundingClientRect().left ?? 0)) / px,
  })
  const selectAll = () => { if (songLen > 0) setSel({ a: 0, b: songLen }) }

  // ---- moving a part: drag a take left/right (whole beats; Alt for free) and up/down to another track (or the empty row for a new one) ----
  const [clip, setClip] = useState<null | { take: Take; from: number; dx: number; dy: number; to: number | 'new' }>(null)
  // what Ctrl/⌘+Z takes back: the moves made on this page, newest last
  const undos = useRef<{ run: () => Promise<void>; done: boolean }[]>([])
  const undoLast = () => {
    const u = undos.current.pop()
    if (!u) { toast('Nothing to undo', 'ok'); return }
    void u.run()
  }
  const playingRef = useRef(playing)
  playingRef.current = playing
  const startClip = (e: React.PointerEvent<HTMLElement>, t: Track, k: Take) => {
    if (e.button !== 0 || e.pointerType === 'touch' || rec !== 'off' || !song) return
    e.preventDefault()
    const x0 = e.clientX, y0 = e.clientY
    const beat = 60 / song.tempo
    const rowOf = (el: Element | null) => el?.closest<HTMLElement>('[data-drop]')
    const home = rowOf(e.currentTarget)
    let moved = false
    let last: NonNullable<typeof clip> | null = null
    const at = (ev: PointerEvent | KeyboardEvent, cx: number, cy: number) => {
      let dx = (cx - x0) / px
      if (!ev.altKey) dx = Math.round(dx / beat) * beat
      const off = Math.max(Math.min(k.offset_ms, 0), Math.round(k.offset_ms + dx * 1000))
      let to: number | 'new' = t.id, dy = 0
      const rows = [...(scroller.current?.querySelectorAll<HTMLElement>('[data-drop]') || [])]
      const row = rows.find(r => { const b = r.getBoundingClientRect(); return cy >= b.top && cy < b.bottom })
      if (row && home) {
        to = row.dataset.drop === 'new' ? 'new' : Number(row.dataset.drop)
        dy = row.getBoundingClientRect().top - home.getBoundingClientRect().top
      }
      return { take: k, from: t.id, dx: (off - k.offset_ms) / 1000, dy, to }
    }
    let cx = x0, cy = y0
    const move = (ev: PointerEvent) => {
      cx = ev.clientX; cy = ev.clientY
      if (!moved && Math.hypot(cx - x0, cy - y0) < 5) return
      moved = true
      last = at(ev, cx, cy)
      setClip(last)
    }
    const key = (ev: KeyboardEvent) => {
      if (ev.key === 'Escape') { moved = false; last = null; done(); setClip(null); return }
      if (moved && ev.key === 'Alt') { last = at(ev, cx, cy); setClip(last) }
    }
    const done = () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
      window.removeEventListener('pointercancel', cancel)
      window.removeEventListener('keydown', key)
      window.removeEventListener('keyup', key)
    }
    const up = () => {
      done()
      setClip(null)
      if (!moved || !last) return
      // the click after a drag is not a click on the part
      const eat = (ev: Event) => { ev.preventDefault(); ev.stopPropagation() }
      window.addEventListener('click', eat, { capture: true, once: true })
      window.setTimeout(() => window.removeEventListener('click', eat, { capture: true }), 300)
      const offset = Math.round(k.offset_ms + last.dx * 1000)
      if (last.dx !== 0 || last.to !== t.id) void moveTake(k, t, offset, last.to)
    }
    const cancel = () => { done(); setClip(null) }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
    window.addEventListener('pointercancel', cancel)
    window.addEventListener('keydown', key)
    window.addEventListener('keyup', key)
  }
  /** Move a part (all its attempts) to a new start and/or onto another track ('new' makes a track for it); it lands on top. Quiet: no message, ⌘/Ctrl+Z undoes. */
  const moveTake = async (k: Take, from: Track, offset: number, to: number | 'new') => {
    const restart = (ts: Track[]) => { if (playingRef.current && player.current) getPlayer().play(ts, Math.max(0, player.current.position()), opts()) }
    const shift = offset - k.offset_ms
    const layer = Math.max(...from.takes.filter(x => x.part === k.part).map(x => x.layer))
    if (to !== 'new') setSong(s => s && { ...s, tracks: movePartLocally(s.tracks || [], k.part, to, shift) })
    try {
      let target = to === 'new' ? null : tracks.find(x => x.id === to) || null
      if (to === 'new') target = await api.post<Track>(`${V1}/songs/${sid}/tracks`, { instrument: from.instrument })
      await api.put(`${V1}/parts/${k.part}`, { shift_ms: shift, ...(target && target.id !== from.id ? { track_id: target.id } : {}) })
      const fresh = await api.get<Song>(`${V1}/songs/${sid}`)
      setSong(fresh)
      restart(fresh.tracks || [])
      announceChange()
      const made = to === 'new' ? target : null
            const entry = { done: false, run: async () => {
        if (entry.done) return
        entry.done = true
        undos.current = undos.current.filter(x => x !== entry)
        try {
          await api.put(`${V1}/parts/${k.part}`, { shift_ms: -shift, track_id: from.id, layer })
          if (made) await api.del(`${V1}/tracks/${made.id}`)
          const back = await api.get<Song>(`${V1}/songs/${sid}`)
          setSong(back)
          restart(back.tracks || [])
          announceChange()
        } catch (e) { toast(errMsg(e), 'err') }
      } }
      undos.current.push(entry)
    } catch (e) { toast(errMsg(e), 'err'); load() }
  }
  function selectItems(): MenuItem[] {
    return [
      { label: 'Select a part…', icon: 'scissors', disabled: songLen <= 0, onSelect: () => { selectAll(); setFocusSel(n => n + 1) } },
      { label: 'Select all', icon: 'maximize', disabled: songLen <= 0, onSelect: selectAll },
    ]
  }

  // ---- what to do with the part ----
  const partJob = async (what: 'download' | 'idea', fn: () => Promise<void>) => {
    if (job) return
    setJob({ what })
    try { await fn() } catch (e) { toast(errMsg(e), 'err') } finally { setJob(null) }
  }
  const downloadPart = () => { const s = sel; if (song && s) void partJob('download', async () => saveBlob((await mixDown(song, tracks, s)).wav, partFile(song.title, s))) }
  const partToIdea = () => {
    const s = sel
    if (!song || !s) return
    void partJob('idea', async () => {
      const r = await mixDown(song, tracks, s)
      const idea = await uploadAudio(`${V1}/ideas`, r.wav, 'idea.wav', {
        name: `${song.title} (${rangeLabel(s)})`, duration_ms: r.ms, sample_rate: r.rate, channels: 2, peaks: peaksOf(r.channels, r.rate),
        source: 'upload', client_id: clientId(), created_at: new Date().toISOString(), ...(song.band_id ? { band_id: song.band_id } : {}),
      }) as Idea
      toast(`Kept “${idea.name}” in Ideas`, 'ok', { label: 'Open', run: () => navigate('/ideas') })
      announceChange()
    })
  }
  /** Every track whose take is in the part gets a new take with just the part, picked; the notes move with it. */
  const cutSong = (s: Sel) => asOneChange(`Cut the song to ${rangeLabel(s)}`, () => cutSongNow(s))
  const cutSongNow = async (s: Sel) => {
    if (!song || job) return
    if (playing) stop()
    const hit = tracks.filter(t => playingTakes(t).some(k => k.offset_ms / 1000 < s.b && (k.offset_ms + k.duration_ms) / 1000 > s.a))
    const untouched = tracks.filter(t => playingTakes(t).length && !hit.includes(t)).length
    let done = 0
    for (const t of hit) {
      setJob({ what: 'cut', text: `Cutting ${done + 1} of ${hit.length} ${hit.length === 1 ? 'track' : 'tracks'}…` })
      try {
        // the track as heard in the part (all its parts, the top one where they overlap) becomes its one part; the rest stay as older takes of it
        const r = await renderTrack(t, s)
        const len = r?.channels[0]?.length || 0
        if (!r || !len) { done++; continue }
        const take = await uploadAudio(`${V1}/tracks/${t.id}/takes`, encodeWav(r.channels, r.rate), 'take.wav', {
          duration_ms: Math.round(len / r.rate * 1000), offset_ms: Math.round((r.at - s.a) * 1000), latency_ms: 0, sample_rate: r.rate,
          channels: r.channels.length, peaks: peaksOf(r.channels, r.rate), source: 'upload', device: playingTakes(t).at(-1)?.device || '', part: 'only',
        }) as Take
        const nb = audioContext().createBuffer(r.channels.length, len, r.rate)
        r.channels.forEach((c, i) => nb.copyToChannel(c as Float32Array<ArrayBuffer>, i))
        rememberTake(take.id, nb)
        setReady(x => ({ ...x, [take.id]: true }))
        done++
      } catch (e) {
        setJob(null)
        toast(`${t.name} could not be cut: ${errMsg(e)}${done ? ` (${done} of ${hit.length} ${hit.length === 1 ? 'track was' : 'tracks were'} cut)` : ''}`, 'err')
        await load()
        announceChange()
        return
      }
    }
    // the notes: the ones in the part move with it, the rest go
    setJob({ what: 'cut', text: 'Moving the notes…' })
    let notesFailed = 0
    for (const m of marks) {
      const at = m.at_ms / 1000, end = m.end_ms > m.at_ms ? m.end_ms / 1000 : at
      try {
        if (end < s.a || at > s.b) { await api.del(`${V1}/marks/${m.id}`); continue }
        const na = Math.round((Math.max(at, s.a) - s.a) * 1000)
        let ne = m.end_ms > m.at_ms ? Math.round((Math.min(end, s.b) - s.a) * 1000) : 0
        if (ne <= na) ne = 0
        if (na !== m.at_ms || ne !== m.end_ms) await api.put(`${V1}/marks/${m.id}`, { at_ms: na, end_ms: ne })
      } catch { notesFailed++ }
    }
    setJob(null)
    setSel(null)
    setPos(0)
    player.current?.seek(0)
    await load()
    announceChange()
    const left = untouched ? ` · ${untouched} ${untouched === 1 ? 'track had' : 'tracks had'} nothing in the part and stayed as ${untouched === 1 ? 'it was' : 'they were'}` : ''
    toast(`Cut to ${(s.b - s.a).toFixed(1)} s${left}${notesFailed ? ` · ${notesFailed} ${notesFailed === 1 ? 'note' : 'notes'} could not be moved` : ''}`, notesFailed ? 'err' : 'ok')
  }

  if (missing) return <Empty title="This song is not here" icon="music"><button type="button" className="btn" onClick={() => navigate('/songs')}>All songs</button></Empty>
  if (!song) return <Loading />
  const sheetTrack = tracks.find(t => t.id === sheet)
  const partTrack = partSheet ? tracks.find(t => t.id === partSheet.track && t.takes.some(k => k.part === partSheet.part)) : undefined
  const busy = rec !== 'off'
  const bar = song.beats * 60 / song.tempo

  const songMenu = (): MenuItem[] => [
    { label: 'Song details', icon: 'edit', onSelect: () => setEditing(true) },
    { label: 'History', icon: 'history', onSelect: () => setHistory(true) },
    { label: song.starred ? 'Unstar' : 'Star', icon: 'star', onSelect: () => saveSong({ starred: !song.starred }) },
    'sep',
    { label: 'Mix down to one file', icon: 'mixer', onSelect: () => setMixing({}), disabled: !song.take_count },
    { label: 'Download the tracks (.zip)', icon: 'download', onSelect: () => { window.location.href = `${V1}/songs/${song.id}/stems.zip` }, disabled: !song.take_count },
    'sep',
    ...selectItems(),
    ...(sel ? [
      { label: 'Download part', icon: 'download', disabled: !!job, onSelect: downloadPart },
      { label: 'Save part as a mix', icon: 'mixer', disabled: !!job, onSelect: () => setMixing({ range: sel }) },
      { label: 'Save part as an idea', icon: 'lightbulb', disabled: !!job, onSelect: partToIdea },
      { label: 'Cut the song to this part', icon: 'scissors', disabled: !!job || busy, onSelect: () => setCutAsk(sel) },
    ] : []),
    'sep',
    { label: 'Delete song', icon: 'trash', danger: true, onSelect: () => setDelSong(true) },
  ]

  return (
    <div className="studio">
      <div className="studio-head">
        <div className="grow min0">
          <h2 className="studio-title ellipsis">{song.starred && <Icon name="star" size={16} className="star-on" />}{song.title}</h2>
          <div className="studio-meta">
            {song.band_id && <button type="button" className="chip band-chip" onClick={() => navigate(`/songs?band=${song.band_id}`)} title="The band's songs"><Icon name="users" size={13} />{song.band_name}</button>}
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
          {tracks.length > 0 && <button type="button" className={`btn ghost sm ${mixerOpen ? 'active' : ''}`} aria-pressed={mixerOpen} title="Each track's volume, balance, mute and solo (X)" onClick={toggleMixer}><Icon name="mixer" size={14} />Mixer</button>}
          <button type="button" className="btn ghost icon-only sm" title="Zoom out" aria-label="Zoom out" onClick={() => setPx(p => Math.max(6, (p || 40) / 1.5))}><Icon name="minus" size={15} /></button>
          <button type="button" className="btn ghost icon-only sm" title="Zoom in" aria-label="Zoom in" onClick={() => setPx(p => Math.min(400, (p || 40) * 1.5))}><Icon name="plus" size={15} /></button>
        </div>
        <div className="tl" ref={scroller}>
          <div className="tl-inner" style={{ width: headW + laneW }}>
            <div className={`tl-row tl-ruler-row ${marks.some(m => !m.track_id) ? 'has-marks' : ''}`}>
              <div className="tl-head tl-ruler-head" style={{ width: headW }}>
                <span className="muted small">{tracks.length} {tracks.length === 1 ? 'track' : 'tracks'}</span>
              </div>
              <Ruler width={laneW} px={px} bar={bar} beats={song.beats} rulerRef={ruler} onStrip={drag.strip} {...laneEvents(null)}>
                {marks.filter(m => !m.track_id).map(m => <MarkFlag key={m.id} m={m} px={px} color={colorOfMark(m)} onOpen={openMark} onChange={changeMark} onDelete={removeMark} />)}
              </Ruler>
            </div>
            {tracks.map(t => (
              <TrackRow key={t.id} t={t} tracks={tracks} headW={headW} laneW={laneW} px={px} armed={armed === t.id} recording={rec === 'rolling' && armed === t.id}
                drag={clip && clip.from === t.id ? { part: clip.take.part, dx: clip.dx, dy: clip.dy } : null} drop={!!clip && clip.to === t.id && clip.from !== t.id} onTakeDown={startClip}
                dragTip={clip && clip.from === t.id ? `${clock(Math.max(0, clip.take.offset_ms / 1000 + clip.dx))} · bar ${barBeat(Math.max(0, clip.take.offset_ms / 1000 + clip.dx), song.tempo, song.beats)}${clip.to !== t.id ? ` → ${clip.to === 'new' ? 'new track' : tracks.find(x => x.id === clip.to)?.name || ''}` : ''}` : ''}
                ready={ready} onOpenPart={k => setPartSheet({ track: t.id, part: k.part })} onArm={() => !busy && setArmed(armed === t.id ? null : t.id)} onOpen={() => setSheet(t.id)}
                onMute={() => saveTrack(t, { muted: !t.muted })} onSolo={() => saveTrack(t, { solo: !t.solo })} onSeek={seek} {...laneEvents(t)}>
                {marks.filter(m => m.track_id === t.id).map(m => <MarkFlag key={m.id} m={m} px={px} color={colorOfMark(m)} onOpen={openMark} onChange={changeMark} onDelete={removeMark} />)}
              </TrackRow>
            ))}
            <div className={`tl-row ${clip?.to === 'new' ? 'drop-to' : ''}`} data-drop="new">
              <div className="tl-head tl-add" style={{ width: headW }}>
                <button type="button" className="btn ghost sm" onClick={() => setAdding({ intent: 'any' })} disabled={busy}><Icon name="plus" size={14} />Add a track</button>
              </div>
              <div className="lane lane-empty" style={{ width: laneW }}>{clip?.to === 'new' && <span className="lane-hint">Drop for a new track</span>}</div>
            </div>
            <div className="mark-guides" style={{ left: headW }} aria-hidden="true">
              {marks.filter(m => !m.track_id).map(m => <div key={m.id} className={`mark-guide ${m.end_ms > m.at_ms ? 'stretch' : ''}`} style={{ left: m.at_ms / 1000 * px, width: m.end_ms > m.at_ms ? (m.end_ms - m.at_ms) / 1000 * px : undefined, ['--mk' as string]: colorOfMark(m) }} />)}
            </div>
            {sel && <SelBand className="tl-sel" left={headW + sel.a * px} width={(sel.b - sel.a) * px} onEdge={drag.edge} />}
            <Playhead player={player} playing={playing} rest={pos} px={px} headW={headW} rec={rec === 'rolling'} onLost={setLost} follow={followNow} />
          </div>
        </div>
      </div>

      {mixerOpen && tracks.length > 0 && <MixerPanel tracks={tracks} player={player} playing={playing} onChange={liveTrack} onOpen={id => setSheet(id)} onClose={toggleMixer} />}

      {!tracks.length && <div className="studio-first">
        <Icon name="mic" size={22} />
        <div><b>Start with a first track.</b> Record your guitar or voice, or bring in a backing track to play along to. Put headphones on when there is something to listen to, so the microphone only hears you.</div>
        <button type="button" className="btn primary" onClick={() => setAdding({ intent: 'any' })}><Icon name="plus" size={15} />Add a track</button>
      </div>}

      <Transport song={song} player={player} playing={playing} rec={rec} armed={tracks.find(t => t.id === armed)} rest={pos} ready={allReady} recFrom={recFrom}
        onPlay={play} onStop={stop} onRecord={record} onStart={toStart} onClick={() => saveSong({ click: !song.click })} onNote={noteAtPlayhead} onSeek={seek} mic={mic} marks={marksRef} notes={marks} noteColor={colorOfMark}>
        {sel && <SelBar sel={sel} total={songLen} onChange={setSel} onClear={() => setSel(null)} focus={focusSel} playhead={() => (playing ? player.current?.position() ?? pos : pos)}
          busy={job?.text && <span className="sel-busy"><Spinner size={12} />{job.text}</span>}>
          <button type="button" className="btn sm" onClick={playPart} disabled={(rec !== 'off' || !allReady) && !partPlay} title={partPlay ? 'Stop' : loop ? 'Play the part, round and round' : 'Play the part, then stop at its end'}>
            <Icon name={partPlay && playing ? 'square' : 'play'} size={13} className={partPlay && playing ? 'solid' : ''} />{partPlay && playing ? 'Stop' : 'Play selection'}</button>
          <button type="button" className={`btn ghost sm ${loop ? 'active' : ''}`} onClick={() => setLoop(v => !v)} aria-pressed={loop} title="Play the part again and again until you stop it"><Icon name="repeat" size={14} />Loop</button>
          <button type="button" className="btn sm" onClick={downloadPart} disabled={!!job} title="The part mixed down, as you hear it (WAV)">{job?.what === 'download' ? <Spinner size={12} /> : <Icon name="download" size={14} />}Download part</button>
          <button type="button" className="btn sm" onClick={() => setMixing({ range: sel })} disabled={!!job} title="Keep the part as a mix of this song"><Icon name="mixer" size={14} />Save as a mix</button>
          <button type="button" className="btn sm" onClick={partToIdea} disabled={!!job} title="The part mixed down, as a new idea">{job?.what === 'idea' ? <Spinner size={12} /> : <Icon name="lightbulb" size={14} />}Save as an idea</button>
          <button type="button" className="btn sm" onClick={() => setCutAsk(sel)} disabled={!!job || busy} title="Cut the whole song down to this part">{job?.what === 'cut' ? <Spinner size={12} /> : <Icon name="scissors" size={14} />}Cut to this part</button>
        </SelBar>}
      </Transport>

      <section className="card studio-notes">
        <div className="card-head"><h3>Notes</h3></div>
        <div className="mark-sec">
          <h4 className="muted small">On the timeline{marks.length ? ` · ${marks.length}` : ''}</h4>
          <MarkList marks={marks} tracks={tracks} tempo={song.tempo} beats={song.beats} colorOf={colorOfMark} onSeek={seek} onOpen={openMark} onDelete={removeMark} />
        </div>
        <NotesBox key={song.id} value={song.notes} onSave={v => saveSong({ notes: v })} />
      </section>

      {partTrack && partSheet && <PartSheet t={partTrack} part={partSheet.part} tracks={tracks} onClose={() => setPartSheet(null)} onReload={load} ready={ready} onReady={id => setReady(r => ({ ...r, [id]: true }))}
        onMove={(take, to) => { setPartSheet(null); void moveTake(take, partTrack, take.offset_ms, to) }} />}
      {sheetTrack && <TrackSheet t={sheetTrack} onOpenPart={k => { setSheet(null); setPartSheet({ track: sheetTrack.id, part: k.part }) }} onClose={() => setSheet(null)} onSave={p => saveTrack(sheetTrack, p)}
        onDeleted={() => { setSheet(null); load() }} />}
      {adding && <AddTrack intent={adding.intent} onClose={() => setAdding(null)} onAdd={addTrack} songId={sid} onImported={async kid => { setReady(r => ({ ...r, [kid]: true })); await load(); announceChange() }} onRecord={() => { setAdding(null); setTimeout(() => void record(), 50) }} />}
      {ctxMenu && <ContextMenu at={ctxMenu} items={ctxItems(ctxMenu)} onClose={() => setCtxMenu(null)} width={250} />}
      {markEdit && <MarkEditor key={markEdit.mark?.id ?? 'new'} anchor={markEdit.anchor} mark={markEdit.mark} draft={markEdit.draft} tracks={tracks} tempo={song.tempo} beats={song.beats}
        onClose={() => setMarkEdit(null)} onSave={v => saveMark(v, markEdit.mark)} onDelete={markEdit.mark ? () => removeMark(markEdit.mark!) : undefined}
        onPlay={sec => { if (playing) seek(sec); else playFrom(sec) }} />}
      {history && <HistorySheet songId={song.id} title={song.title} onClose={() => setHistory(false)} />}
      {editing && <Modal title="Song details" onClose={() => setEditing(false)}><SongForm song={song} onSaved={s => { setSong(s); setEditing(false) }} /></Modal>}
      {mixing && <MixDown song={song} tracks={tracks} range={mixing.range} onClose={() => setMixing(null)} />}
      {cutAsk && <Confirm title={`Cut “${song.title}” to ${rangeLabel(cutAsk)}?`} confirmLabel="Cut" onCancel={() => setCutAsk(null)}
        message={<>Each track gets a new take holding just this part, picked; the older takes stay in each track’s takes.{marks.some(m => (m.end_ms > m.at_ms ? m.end_ms : m.at_ms) / 1000 < cutAsk.a || m.at_ms / 1000 > cutAsk.b) ? ' Notes outside the part are removed.' : ''}</>}
        onConfirm={() => { const s = cutAsk; setCutAsk(null); void cutSong(s) }} />}
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

/** The bars and beats; a click goes there, a drag sideways selects a part (`onStrip`), right-click or a long press is the notes menu. */
function Ruler({ width, px, bar, beats, rulerRef, onStrip, onContextMenu, onHold, children }: {
  width: number; px: number; bar: number; beats: number; rulerRef: React.Ref<HTMLDivElement>; onStrip: (e: React.PointerEvent<HTMLElement>) => void
  onContextMenu: (e: React.MouseEvent<HTMLElement>) => void; onHold: (e: React.PointerEvent<HTMLElement>) => void; children?: React.ReactNode
}) {
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
  return <div ref={rulerRef} className="ruler" style={{ width }} onContextMenu={onContextMenu}
    onPointerDown={e => { if (e.button !== 0) return; onHold(e); onStrip(e) }}>{marks}{children}</div>
}

function TrackRow({ t, tracks, headW, laneW, px, armed, recording, ready, drag, drop, dragTip, onTakeDown, onOpenPart, onArm, onOpen, onMute, onSolo, onSeek, onContextMenu, onHold, children }: {
  t: Track; tracks: Track[]; headW: number; laneW: number; px: number; armed: boolean; recording: boolean; ready: Record<number, boolean>
  drag: { part: number; dx: number; dy: number } | null; drop: boolean; dragTip: string; onTakeDown: (e: React.PointerEvent<HTMLElement>, t: Track, k: Take) => void
  onOpenPart: (k: Take) => void; onArm: () => void; onOpen: () => void; onMute: () => void; onSolo: () => void; onSeek: (s: number) => void
  onContextMenu: (e: React.MouseEvent<HTMLElement>) => void; onHold: (e: React.PointerEvent<HTMLElement>) => void; children?: React.ReactNode
}) {
  const parts = playingTakes(t)
  const heard = audible(t, tracks)
  const color = colorOf(t)
  return (
    <div className={`tl-row ${heard ? '' : 'quiet'} ${drop ? 'drop-to' : ''} ${drag ? 'dragging-from' : ''}`} data-drop={t.id}>
      <div className="tl-head trk-head" style={{ width: headW, ['--trk' as string]: color }}>
        <button type="button" className="trk-name" onClick={onOpen} title={`${t.name}: name, volume, balance, its parts`}>
          <span className="trk-ico"><Icon name={instrumentOf(t.instrument).icon} size={14} /></span>
          <span className="ellipsis">{t.name}</span>
        </button>
        <div className="trk-btns">
          <button type="button" className={`trk-btn ${t.muted ? 'on mute' : ''}`} onClick={onMute} title={t.muted ? 'Unmute' : 'Mute'} aria-pressed={t.muted}>M</button>
          <button type="button" className={`trk-btn ${t.solo ? 'on solo' : ''}`} onClick={onSolo} title={t.solo ? 'Stop soloing' : 'Solo: hear only this'} aria-pressed={t.solo}>S</button>
          <button type="button" className={`trk-btn arm ${armed ? 'on' : ''}`} onClick={onArm} title={armed ? 'The next recording goes here' : 'Record on this track'} aria-pressed={armed}><Icon name="record" size={10} /></button>
        </div>
      </div>
      <div className={`lane ${armed ? 'armed' : ''} ${recording ? 'recording' : ''}`} style={{ width: laneW }} onContextMenu={onContextMenu}
        onPointerDown={e => { if (e.button !== 0) return; onHold(e); if ((e.target as HTMLElement).closest('.take')) return; const r = e.currentTarget.getBoundingClientRect(); onSeek((e.clientX - r.left) / px) }}>
        {parts.map((k, i) => {
          const d = drag && drag.part === k.part ? drag : null
          const tries = t.takes.filter(x => x.part === k.part).length
          return (
            <div key={k.part} className={`take ${d ? 'dragging' : ''}`} style={{ left: k.offset_ms / 1000 * px, width: Math.max(4, k.duration_ms / 1000 * px), ['--trk' as string]: color, zIndex: d ? 50 : i + 1, transform: d ? `translate(${d.dx * px}px, ${d.dy}px)` : undefined }}
              onClick={() => onOpenPart(k)} onPointerDown={e => onTakeDown(e, t, k)} title={`${tries > 1 ? `${tries} takes of this part — take ${k.number} plays` : `Take ${k.number}`}. Click for its takes, drag to move it (Alt: off the beat)`}>
              <Waveform peaks={k.peaks} pxPerSec={px} width={Math.min(Math.max(4, Math.round(k.duration_ms / 1000 * px)), 16000)} height={46} color={color} />
              <span className="take-tag">{d ? dragTip : tries > 1 ? `${tries} takes` : ''}{!ready[k.id] && <Spinner size={10} />}</span>
            </div>
          )
        })}
        {!parts.length && !recording && <span className="lane-hint">{armed ? 'Press record' : 'Empty'}</span>}
        {children}
      </div>
    </div>
  )
}

/** Every track side by side: a fader (0–150 %, 100 = as recorded; double-click for 100), a level meter while it plays,
 * balance (double-click for the middle), mute and solo. */
function MixerPanel({ tracks, player, playing, onChange, onOpen, onClose }: {
  tracks: Track[]; player: React.MutableRefObject<Player | null>; playing: boolean
  onChange: (t: Track, p: Partial<Track>) => void; onOpen: (id: number) => void; onClose: () => void
}) {
  const meters = useRef<Record<number, HTMLDivElement | null>>({})
  useEffect(() => {
    const zero = () => { for (const el of Object.values(meters.current)) if (el) el.style.transform = 'scaleY(0)' }
    if (!playing) { zero(); return }
    const shown: Record<number, number> = {}
    let raf = 0
    const frame = () => {
      const lv = player.current?.levels() || new Map<number, number>()
      for (const [id, el] of Object.entries(meters.current)) {
        if (!el) continue
        const peak = lv.get(Number(id)) || 0
        const db = peak > 0 ? 20 * Math.log10(peak) : -60
        const h = Math.max(0, Math.min(1, (db + 60) / 60))
        const prev = shown[Number(id)] || 0
        const v = h > prev ? h : prev * 0.92 // up at once, down slowly
        shown[Number(id)] = v
        el.style.transform = `scaleY(${v})`
        el.classList.toggle('hot', peak >= 0.99)
      }
      raf = requestAnimationFrame(frame)
    }
    raf = requestAnimationFrame(frame)
    return () => { cancelAnimationFrame(raf); zero() }
  }, [playing, player])
  return (
    <div className="mixer">
      <div className="mixer-head">
        <b><Icon name="mixer" size={14} /> Mixer</b>
        <span className="muted small">100 % is as recorded · double-click a fader for 100 %</span>
        <button type="button" className="btn ghost icon-only sm" title="Hide the mixer (X)" aria-label="Hide the mixer" onClick={onClose}><Icon name="x" size={15} /></button>
      </div>
      <div className="mixer-strips">
        {tracks.map(t => {
          const heard = audible(t, tracks)
          return (
            <div key={t.id} className={`strip ${heard ? '' : 'quiet'}`} style={{ ['--trk' as string]: colorOf(t) }}>
              <button type="button" className="strip-name" onClick={() => onOpen(t.id)} title={`${t.name}: takes and more`}>
                <span className="trk-ico"><Icon name={instrumentOf(t.instrument).icon} size={13} /></span><span className="ellipsis">{t.name}</span>
              </button>
              <div className="strip-fader">
                <div className="strip-meter" aria-hidden="true"><div ref={el => { meters.current[t.id] = el }} /></div>
                <input type="range" className="vfader" min={0} max={150} step={1} value={t.volume} aria-label={`${t.name} volume`}
                  onChange={e => onChange(t, { volume: Number(e.target.value) })} onDoubleClick={() => onChange(t, { volume: 100 })} />
              </div>
              <span className={`strip-vol ${t.volume === 0 ? 'muted' : ''}`}>{t.volume}%</span>
              <input type="range" className="strip-pan" min={-100} max={100} step={5} value={t.pan} aria-label={`${t.name} balance`}
                title={t.pan === 0 ? 'Balance: middle (double-click for the middle)' : `Balance: ${Math.abs(t.pan)}% ${t.pan < 0 ? 'left' : 'right'}`}
                onChange={e => onChange(t, { pan: Number(e.target.value) })} onDoubleClick={() => onChange(t, { pan: 0 })} />
              <span className="muted strip-pan-val">{t.pan === 0 ? 'C' : t.pan < 0 ? `L${-t.pan}` : `R${t.pan}`}</span>
              <div className="trk-btns">
                <button type="button" className={`trk-btn ${t.muted ? 'on mute' : ''}`} onClick={() => onChange(t, { muted: !t.muted })} title={t.muted ? 'Unmute' : 'Mute'} aria-pressed={t.muted}>M</button>
                <button type="button" className={`trk-btn ${t.solo ? 'on solo' : ''}`} onClick={() => onChange(t, { solo: !t.solo })} title={t.solo ? 'Stop soloing' : 'Solo: hear only this'} aria-pressed={t.solo}>S</button>
              </div>
            </div>
          )
        })}
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

function Transport({ song, player, playing, rec, armed, rest, ready, recFrom, onPlay, onStop, onRecord, onStart, onClick, onNote, onSeek, mic, marks, notes, noteColor, children }: {
  song: Song; player: React.RefObject<Player | null>; playing: boolean; rec: RecState; armed?: Track; rest: number; ready: boolean; recFrom: React.RefObject<number>
  onPlay: () => void; onStop: () => void; onRecord: () => void; onStart: () => void; onClick: () => void; onNote: () => void; onSeek: (sec: number) => void
  mic: React.RefObject<Mic | null>; marks: React.RefObject<Mark[]>; notes: Mark[]; noteColor: (m: Mark) => string; children?: React.ReactNode
}) {
  const noteRef = useRef<HTMLDivElement>(null)
  const noteText = useRef<HTMLSpanElement>(null)
  const clockRef = useRef<HTMLSpanElement>(null)
  const barRef = useRef<HTMLElement>(null)
  const countRef = useRef<HTMLDivElement>(null)
  const meterRef = useRef<HTMLSpanElement>(null)
  const progRef = useRef<HTMLSpanElement>(null)
  const [tip, setTip] = useState<{ m: Mark; x: number; y: number } | null>(null)
  const len = song.length_ms / 1000
  useEffect(() => {
    const show = (s: number) => {
      if (clockRef.current) clockRef.current.textContent = clock(Math.max(0, s))
      if (barRef.current) barRef.current.textContent = barBeat(Math.max(0, s), song.tempo, song.beats)
      if (progRef.current && len > 0) progRef.current.style.transform = `scaleX(${Math.min(1, Math.max(0, s / len))})`
      const c = countRef.current
      if (c) {
        const left = recFrom.current - s
        if (rec === 'rolling' && left > 0) { c.textContent = String(Math.ceil(left / (60 / song.tempo) - 1e-6)); c.hidden = false } else c.hidden = true
      }
      // the note you're at: a stretch while it lasts, a moment for 4 seconds
      const n = noteRef.current
      if (n && noteText.current) {
        const ms = s * 1000
        let cur: Mark | undefined
        for (const m of marks.current) if (m.text && m.at_ms <= ms && ms < (m.end_ms > m.at_ms ? m.end_ms : m.at_ms + 4000)) cur = m
        const txt = cur ? cur.text.split('\n')[0] : ''
        if (noteText.current.textContent !== txt) noteText.current.textContent = txt
        n.hidden = !txt
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
  }, [playing, rest, rec, song.tempo, song.beats, player, mic, recFrom, marks, len])
  const recording = rec === 'rolling'
  const status = recording ? <span className="tp-rec"><span className="tp-rec-dot" />Recording {armed?.name}</span>
    : rec === 'saving' ? <span className="tp-rec saving"><Spinner size={10} /> Keeping the take…</span>
    : rec === 'opening' ? <span>Opening the microphone…</span>
    : armed ? <span className="ellipsis">Records on <b>{armed.name}</b></span> : null
  return (
    <>
      <div ref={countRef} className="countin" hidden aria-live="assertive" />
      {tip && createPortal(
        <div className="tp-tip" style={{ left: Math.max(150, Math.min(window.innerWidth - 150, tip.x)), top: tip.y, ['--mk' as string]: noteColor(tip.m) }} role="tooltip">
          {tip.m.track_id && <span className="trk">{song.tracks?.find(t => t.id === tip.m.track_id)?.name}</span>}
          <span className="tp-tip-text"><b>{clock(tip.m.at_ms / 1000)}{tip.m.end_ms > tip.m.at_ms ? `–${clock(tip.m.end_ms / 1000)}` : ''}</b>{tip.m.text || 'Note'}</span>
        </div>, document.body)}
      <div className={`transport ${recording ? 'rec' : ''} ${playing ? 'playing' : ''} ${children ? 'has-sel' : ''}`}>
        {len > 0 && <div className="tp-progress" title="Click to go there" onPointerDown={e => { if (rec !== 'off') return; const r = e.currentTarget.getBoundingClientRect(); onSeek((e.clientX - r.left) / r.width * len) }}>
          <span ref={progRef} className="tp-fill" />
          {notes.filter(m => m.at_ms / 1000 < len).map(m => {
            const stretch = m.end_ms > m.at_ms
            const left = m.at_ms / 1000 / len * 100
            const width = stretch ? (Math.min(m.end_ms / 1000, len) - m.at_ms / 1000) / len * 100 : 0
            return <span key={m.id} className={`tp-tick ${stretch ? 'stretch' : ''}`} style={{ left: `${left}%`, width: stretch ? `${width}%` : undefined, ['--mk' as string]: noteColor(m) }}
              aria-label={`${clock(m.at_ms / 1000, false)} · ${m.text.split('\n')[0] || 'Note'}`}
              onPointerEnter={e => { const r = e.currentTarget.getBoundingClientRect(); setTip({ m, x: r.left + Math.min(r.width, 40) / 2, y: r.top }) }}
              onPointerLeave={() => setTip(t => t?.m.id === m.id ? null : t)}
              onPointerDown={e => { e.stopPropagation(); setTip(null); if (rec === 'off') onSeek(m.at_ms / 1000) }} />
          })}
        </div>}
        {children}
        <div className="tp-time">
          <span ref={clockRef} className="tp-clock">0:00.0</span>
          <span className="tp-sub">bar <b ref={barRef}>1.1</b>{len > 0 && <span className="faint"> · of {clock(len, false)}</span>}</span>
          {recording && <span className="tp-meter"><span ref={meterRef} /></span>}
        </div>
        <div className="tp-btns">
          <button type="button" className={`tp-btn sm ${song.click ? 'lit' : ''}`} onClick={onClick} title={song.click ? 'Click on while recording (C)' : 'Click off (C) — the count-in still clicks'} aria-pressed={song.click} aria-label="Click"><Icon name="metronome" size={18} /></button>
          <button type="button" className="tp-btn" onClick={onStart} disabled={rec !== 'off'} title="To the start (0)" aria-label="To the start"><Icon name="skip-back" size={20} className="solid" /></button>
          {playing || recording
            ? <button type="button" className="tp-btn tp-play" onClick={onStop} title="Stop (space)" aria-label="Stop"><Icon name="square" size={18} className="solid" /></button>
            : <button type="button" className="tp-btn tp-play" onClick={onPlay} disabled={rec !== 'off' || !ready} title={ready ? 'Play (space)' : 'Loading the takes…'} aria-label="Play">{ready ? <Icon name="play" size={24} className="play" /> : <Spinner size={18} />}</button>}
          <button type="button" className={`tp-btn recbtn ${recording ? 'on' : ''}`} onClick={recording ? onStop : onRecord} disabled={rec === 'saving' || rec === 'opening' || (playing && !recording) || !ready} title="Record (R)" aria-label={recording ? 'Stop recording' : 'Record'}><Icon name="record" size={20} /></button>
          <button type="button" className="tp-btn sm" onClick={onNote} title="A note on the timeline, here (M)" aria-label="Add a note here"><Icon name="flag" size={17} /></button>
        </div>
        <div className="tp-side">
          <div ref={noteRef} className="tp-note" hidden aria-live="polite"><Icon name="flag" size={13} /><span ref={noteText} className="ellipsis" /></div>
          <div className="tp-status">{status}<span className="tp-tempo">{Math.round(song.tempo * 10) / 10} bpm · {song.beats}/{song.beat_unit}</span></div>
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

function TrackSheet({ t, onOpenPart, onClose, onSave, onDeleted }: {
  t: Track; onOpenPart: (k: Take) => void; onClose: () => void; onSave: (p: Partial<Track>) => void; onDeleted: () => void
}) {
  const { toast } = useToast()
  const [name, setName] = useState(t.name)
  const [del, setDel] = useState(false)
  const [vol, setVol] = useState(t.volume)
  const [pan, setPan] = useState(t.pan)
  useEffect(() => { setVol(t.volume); setPan(t.pan) }, [t.volume, t.pan])
  const parts = [...playingTakes(t)].sort((a, b) => a.offset_ms - b.offset_ms)
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
          <h4 className="sheet-h">Parts {parts.length > 0 && <span className="muted small">— all of them play; where two overlap, the one on top</span>}</h4>
          {!parts.length && <p className="muted small">Nothing recorded on this track yet.</p>}
          <ul className="takes">
            {parts.map(k => {
              const tries = attemptsOf(t, k.part).length
              return (
                <li key={k.part} className="take-row">
                  <button type="button" className="take-pick" onClick={() => onOpenPart(k)} title="Its takes, line it up, move it">
                    <span className="grow min0">
                      <b>{clock(k.offset_ms / 1000)} – {clock((k.offset_ms + k.duration_ms) / 1000)}</b> <span className="muted small">{tries > 1 ? `${tries} takes · take ${k.number} plays` : `take ${k.number}`}</span>
                      <Waveform peaks={k.peaks} pxPerSec={Math.max(4, 240 / Math.max(1, k.duration_ms / 1000))} width={240} height={22} color={colorOf(t)} className="take-mini" />
                    </span>
                    <Icon name="chevron-right" size={15} />
                  </button>
                </li>
              )
            })}
          </ul>
        </div>
        <div className="row"><button type="button" className="btn ghost danger-text sm" onClick={() => setDel(true)}><Icon name="trash" size={14} />Delete this track</button></div>
      </div>
      {del && <Confirm title={`Delete ${t.name}?`} message={`The track and its ${t.takes.length} ${t.takes.length === 1 ? 'take' : 'takes'} go.`} confirmLabel="Delete"
        onCancel={() => setDel(false)} onConfirm={async () => {
          setDel(false)
          try { await api.del(`${V1}/tracks/${t.id}`); onDeleted(); announceChange() } catch (e) { toast(errMsg(e), 'err') }
        }} />}
    </Modal>
  )
}

/** One part of a track: its takes (pick the one that plays), lining it up, moving it to another track, deleting it. */
function PartSheet({ t, part, tracks, onClose, onReload, onMove, ready, onReady }: {
  t: Track; part: number; tracks: Track[]; onClose: () => void; onReload: () => Promise<void>; onMove: (take: Take, to: number | 'new') => void
  ready: Record<number, boolean>; onReady: (id: number) => void
}) {
  const { toast } = useToast()
  const [del, setDel] = useState<'part' | Take | null>(null)
  const tries = attemptsOf(t, part)
  const k = tries.find(x => x.plays) || tries.at(-1)
  if (!k) return null
  const pick = async (take: Take) => {
    try { await api.put(`${V1}/takes/${take.id}`, { plays: true }); await onReload() } catch (e) { toast(errMsg(e), 'err') }
    if (!ready[take.id]) loadTake(take.id).then(() => onReady(take.id)).catch(e => toast(errMsg(e), 'err'))
  }
  const nudge = async (take: Take, ms: number) => {
    try { await api.put(`${V1}/takes/${take.id}`, { offset_ms: take.offset_ms + ms }); await onReload() } catch (e) { toast(errMsg(e), 'err') }
  }
  const like = async (take: Take) => { try { await api.put(`${V1}/takes/${take.id}`, { liked: !take.liked }); await onReload() } catch (e) { toast(errMsg(e), 'err') } }
  const moveItems: MenuItem[] = [
    ...tracks.filter(x => x.id !== t.id).map((x): MenuItem => ({ label: `Move to ${x.name}`, icon: instrumentOf(x.instrument).icon, onSelect: () => onMove(k, x.id) })),
    { label: 'Move to a new track', icon: 'plus', onSelect: () => onMove(k, 'new') },
  ]
  return (
    <Modal title={<span className="row" style={{ gap: 8 }}><span className="trk-ico" style={{ ['--trk' as string]: colorOf(t) }}><Icon name={instrumentOf(t.instrument).icon} size={14} /></span>{t.name} · {clock(k.offset_ms / 1000)} – {clock((k.offset_ms + k.duration_ms) / 1000)}</span>} onClose={onClose}>
      <div className="stack">
        <div>
          <h4 className="sheet-h">Takes of this part {tries.length > 1 && <span className="muted small">— the one picked plays; record over the part again for another</span>}</h4>
          <ul className="takes">
            {[...tries].reverse().map(take => (
              <li key={take.id} className={`take-row ${take.plays ? 'cur' : ''}`}>
                <button type="button" className="take-pick" onClick={() => pick(take)} aria-pressed={take.plays} title="Play this take">
                  <span className={`radio ${take.plays ? 'on' : ''}`} />
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
        <div>
          <h4 className="sheet-h">Line it up <span className="muted small">— take {k.number} starts at {(k.offset_ms / 1000).toFixed(3)} s</span></h4>
          <p className="muted small">If this take sounds early or late against the others, move it a little (to move the whole part, drag it on the timeline). Measure your device once in Settings → Recording and new takes line up by themselves.</p>
          <div className="row wrap">
            <button type="button" className="btn sm" onClick={() => nudge(k, -50)}>−50 ms</button>
            <button type="button" className="btn sm" onClick={() => nudge(k, -10)}>−10 ms</button>
            <button type="button" className="btn sm" onClick={() => nudge(k, 10)}>+10 ms</button>
            <button type="button" className="btn sm" onClick={() => nudge(k, 50)}>+50 ms</button>
          </div>
          <p className="muted small mt">{k.device && <>{k.device} · </>}{k.sample_rate ? `${(k.sample_rate / 1000).toFixed(1)} kHz` : ''}{k.channels === 2 ? ' stereo' : ' mono'} · {fmtBytes(k.size)}{k.latency_ms ? ` · ${k.latency_ms} ms delay taken out` : ''}</p>
        </div>
        <div className="row wrap">
          <MenuButton label="Move this part to another track" className="btn sm" items={moveItems} width={240}><Icon name="corner-down-right" size={14} />Move to…</MenuButton>
          <button type="button" className="btn ghost danger-text sm" onClick={() => setDel('part')}><Icon name="trash" size={14} />Delete this part</button>
        </div>
      </div>
      {del && <Confirm title={del === 'part' ? 'Delete this part?' : `Delete take ${(del as Take).number}?`}
        message={del === 'part' ? `${tries.length === 1 ? 'Its recording is' : `All ${tries.length} takes of it are`} gone for good.` : 'The recording is gone for good.'} confirmLabel="Delete"
        onCancel={() => setDel(null)} onConfirm={async () => {
          const d = del; setDel(null)
          try {
            if (d === 'part') { for (const x of tries) await api.del(`${V1}/takes/${x.id}`); onClose() } else await api.del(`${V1}/takes/${(d as Take).id}`)
            await onReload()
            announceChange()
          } catch (e) { toast(errMsg(e), 'err'); await onReload() }
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

function MixDown({ song, tracks, range, onClose }: { song: Song; tracks: Track[]; range?: Sel; onClose: () => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const [step, setStep] = useState<'ask' | 'mixing' | 'saving' | 'done'>('ask')
  const [name, setName] = useState(range ? `${song.title} (${rangeLabel(range)})` : `${song.title} — mix`)
  const [mix, setMix] = useState<Mix | null>(null)
  const [pct, setPct] = useState(0)
  const heard = tracks.filter(t => audible(t, tracks) && playingTakes(t).length)
  const go = async () => {
    try {
      setStep('mixing')
      const r = await mixDown(song, tracks, range)
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
    <Modal title={range ? 'Mix down the part' : 'Mix down'} onClose={onClose} size="sm">
      {step === 'ask' && <div className="stack">
        <p className="muted">{range ? `From ${clock(range.a)} to ${clock(range.b)}: t` : 'T'}he {heard.length} {heard.length === 1 ? 'track' : 'tracks'} you hear now — with their volume and balance, muted ones left out — become one stereo file (WAV).</p>
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

