// Notes on the timeline ("marks"): pinned to a moment of a song, or a
// stretch of it, on the whole song (a flag on the ruler) or on one track (a
// pin in its lane). Right-click (or press and hold on a phone) to add one,
// drag a flag to move it, its end to stretch it, click it to edit.
import { useEffect, useRef, useState } from 'react'
import type { Mark, Track } from '../lib/types'
import { barBeat, clock } from '../lib/engine'
import { Popover, rectOf, type Anchor } from './ui'
import { Icon } from './icons'

export const SONG_MARK = '#7048e8'
export const MARK_COLORS = ['#7048e8', '#1c7ed6', '#0ca678', '#f59f00', '#e8590c', '#e5484d', '#d6336c', '#868e96']

/** "1:11.1", "71.1" or "1:02:03" → seconds; null when it can't be read. */
export function parseClock(v: string): number | null {
  const t = v.trim().replace(',', '.')
  if (!t) return null
  const parts = t.split(':')
  if (parts.length > 3 || parts.some(p => p === '' || isNaN(Number(p)))) return null
  const s = parts.reduce((acc, p) => acc * 60 + Number(p), 0)
  return s >= 0 ? s : null
}

/** Press and hold on a touch screen: `fire` at the pressed point unless the finger moves or lifts first. */
export function holdToMenu(e: React.PointerEvent, fire: (x: number, y: number) => void) {
  if (e.pointerType === 'mouse') return
  const x0 = e.clientX, y0 = e.clientY
  const done = () => {
    window.clearTimeout(t)
    window.removeEventListener('pointermove', move)
    window.removeEventListener('pointerup', done)
    window.removeEventListener('pointercancel', done)
  }
  const move = (ev: PointerEvent) => { if (Math.hypot(ev.clientX - x0, ev.clientY - y0) > 10) done() }
  const t = window.setTimeout(() => {
    done()
    // the finger lifting after the menu opened is not a tap on what's under it
    const eat = (ev: Event) => { ev.preventDefault(); ev.stopPropagation() }
    window.addEventListener('click', eat, { capture: true, once: true })
    window.setTimeout(() => window.removeEventListener('click', eat, { capture: true }), 1200)
    fire(x0, y0)
  }, 550)
  window.addEventListener('pointermove', move)
  window.addEventListener('pointerup', done)
  window.addEventListener('pointercancel', done)
}

export const markColor = (m: Mark, tracks: Track[], trackColor: (t: Track) => string) => {
  if (m.color) return m.color
  const t = m.track_id ? tracks.find(x => x.id === m.track_id) : undefined
  return t ? trackColor(t) : SONG_MARK
}

/** One note on the timeline: a flag on the ruler (whole song) or a pin in a lane (a track). */
export function MarkFlag({ m, px, color, onOpen, onChange, onDelete }: {
  m: Mark; px: number; color: string
  onOpen: (m: Mark, a: Anchor) => void; onChange: (m: Mark, patch: Partial<Mark>) => void; onDelete: (m: Mark) => void
}) {
  const [drag, setDrag] = useState<{ dx: number; end: boolean } | null>(null)
  const start = (e: React.PointerEvent, end: boolean) => {
    e.stopPropagation()
    if (e.button !== 0) return
    const el = e.currentTarget as HTMLElement
    const x0 = e.clientX
    let moved = false
    el.setPointerCapture(e.pointerId)
    const move = (ev: PointerEvent) => {
      const dx = ev.clientX - x0
      if (!moved && Math.abs(dx) > 4) moved = true
      if (moved) setDrag({ dx, end })
    }
    const finish = (ev: PointerEvent, cancel: boolean) => {
      el.removeEventListener('pointermove', move)
      el.removeEventListener('pointerup', up)
      el.removeEventListener('pointercancel', cancelled)
      setDrag(null)
      if (cancel) return
      if (!moved) { if (!end) onOpen(m, rectOf(el)); return }
      const dms = Math.round((ev.clientX - x0) / px * 1000)
      if (end) {
        const e2 = Math.max(m.at_ms + 100, m.end_ms + dms)
        onChange(m, { end_ms: e2 })
      } else {
        const at = Math.max(0, m.at_ms + dms)
        onChange(m, { at_ms: at, end_ms: m.end_ms > m.at_ms ? m.end_ms + (at - m.at_ms) : 0 })
      }
    }
    const up = (ev: PointerEvent) => finish(ev, false)
    const cancelled = (ev: PointerEvent) => finish(ev, true)
    el.addEventListener('pointermove', move)
    el.addEventListener('pointerup', up)
    el.addEventListener('pointercancel', cancelled)
  }
  const stretch = m.end_ms > m.at_ms
  const left = m.at_ms / 1000 * px + (drag && !drag.end ? drag.dx : 0)
  const width = stretch ? Math.max(6, (m.end_ms - m.at_ms) / 1000 * px + (drag?.end ? drag.dx : 0)) : 0
  const label = m.text.split('\n')[0] || 'Note'
  return (
    <div className={`mark ${stretch ? 'stretch' : ''} ${drag ? 'dragging' : ''}`} style={{ left, width: stretch ? width : undefined, ['--mk' as string]: color }}>
      {stretch && <div className="mark-band" />}
      <button type="button" className="mark-flag" onPointerDown={e => start(e, false)} onContextMenu={e => { e.preventDefault(); e.stopPropagation(); onOpen(m, rectOf(e.currentTarget)) }} title={`${m.text || 'Note'} — drag to move, click to edit or delete`}
        onKeyDown={e => {
          if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onOpen(m, rectOf(e.currentTarget)) }
          else if (e.key === 'Delete' || e.key === 'Backspace') { e.preventDefault(); e.stopPropagation(); onDelete(m) }
        }}>
        <span className="ellipsis">{label}</span>
      </button>
      {stretch && <span className="mark-end" onPointerDown={e => start(e, true)} title="Drag to make it longer or shorter" />}
    </div>
  )
}

/** Writing a note (new or one that's there): text, where, which track, colour. */
export function MarkEditor({ anchor, mark, draft, tracks, tempo, beats, onClose, onSave, onDelete, onPlay }: {
  anchor: Anchor; mark?: Mark; draft: { at_ms: number; end_ms: number; track_id: number | null }; tracks: Track[]; tempo: number; beats: number
  onClose: () => void; onSave: (v: { text: string; at_ms: number; end_ms: number; track_id: number | null; color: string }) => Promise<boolean>
  onDelete?: () => void; onPlay: (sec: number) => void
}) {
  const [text, setText] = useState(mark?.text || '')
  const [at, setAt] = useState(clock(draft.at_ms / 1000))
  const [until, setUntil] = useState(draft.end_ms > draft.at_ms ? clock(draft.end_ms / 1000) : '')
  const [track, setTrack] = useState(draft.track_id ? String(draft.track_id) : '')
  const [color, setColor] = useState(mark?.color || '')
  const [busy, setBusy] = useState(false)
  const ta = useRef<HTMLTextAreaElement>(null)
  useEffect(() => { const t = ta.current; if (t) { t.focus(); t.setSelectionRange(t.value.length, t.value.length) } }, [])
  const atS = parseClock(at), untilS = until.trim() ? parseClock(until) : 0
  const bad = atS === null || untilS === null || (!!untilS && untilS <= atS!)
  const save = async () => {
    if (bad || busy) return
    setBusy(true)
    const ok = await onSave({ text: text.trim(), at_ms: Math.round(atS! * 1000), end_ms: untilS ? Math.round(untilS * 1000) : 0, track_id: track ? Number(track) : null, color })
    setBusy(false)
    if (ok) onClose()
  }
  return (
    <Popover anchor={anchor} onClose={onClose} prefer="below" width={340} label={mark ? 'Note' : 'New note'} className="mark-pop">
      <form className="stack tight" onSubmit={e => { e.preventDefault(); void save() }}>
        <textarea ref={ta} rows={3} value={text} onChange={e => setText(e.target.value)} placeholder="Chorus, too loud here, try a harmony, the chords…"
          onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); void save() } else if (e.key === 'Escape') { e.preventDefault(); onClose() } }} />
        <div className="mark-when">
          <label><span className="muted small">At</span><input value={at} onChange={e => setAt(e.target.value)} inputMode="decimal" aria-label="At" className={atS === null ? 'invalid' : ''} /></label>
          <label><span className="muted small">Until</span><input value={until} onChange={e => setUntil(e.target.value)} inputMode="decimal" placeholder="optional" aria-label="Until" className={untilS === null || (!!untilS && atS !== null && untilS <= atS) ? 'invalid' : ''} /></label>
          {atS !== null && <span className="muted small mark-bar">bar {barBeat(atS, tempo, beats)}</span>}
        </div>
        <div className="mark-on" role="radiogroup" aria-label="On">
          <button type="button" role="radio" aria-checked={!track} className={`mark-on-btn ${!track ? 'on' : ''}`} onClick={() => setTrack('')}>The whole song</button>
          {tracks.map(t => <button key={t.id} type="button" role="radio" aria-checked={track === String(t.id)} className={`mark-on-btn ${track === String(t.id) ? 'on' : ''}`} onClick={() => setTrack(String(t.id))}>{t.name}</button>)}
        </div>
        <div className="swatches sm">
          <button type="button" className={`swatch none ${!color ? 'on' : ''}`} onClick={() => setColor('')} aria-label="Usual colour" title="Usual colour (the track's)" />
          {MARK_COLORS.map(c => <button key={c} type="button" className={`swatch ${color === c ? 'on' : ''}`} style={{ background: c }} onClick={() => setColor(c)} aria-label={c} />)}
        </div>
        <div className="row" style={{ gap: 6 }}>
          <button type="submit" className="btn primary sm" disabled={bad || busy}>{mark ? 'Save' : 'Add the note'}</button>
          {atS !== null && <button type="button" className="btn ghost sm" onClick={() => { onPlay(atS); onClose() }} title="Play from here"><Icon name="play" size={13} />Play from here</button>}
          <span className="grow" />
          {onDelete && <button type="button" className="btn ghost sm danger" onClick={onDelete} title="Delete the note"><Icon name="trash" size={14} />Delete</button>}
        </div>
      </form>
    </Popover>
  )
}

/** Every note in time order, under the song: click the time to go there. */
export function MarkList({ marks, tracks, tempo, beats, colorOf, onSeek, onOpen, onDelete }: {
  marks: Mark[]; tracks: Track[]; tempo: number; beats: number; colorOf: (m: Mark) => string
  onSeek: (sec: number) => void; onOpen: (m: Mark, a: Anchor) => void; onDelete: (m: Mark) => void
}) {
  if (!marks.length) return <p className="muted small mark-empty">Right-click the timeline (press and hold on a phone) or press <kbd>M</kbd> while it plays to pin a note to that moment.</p>
  return (
    <ul className="mark-list">
      {marks.map(m => {
        const t = m.track_id ? tracks.find(x => x.id === m.track_id) : undefined
        return (
          <li key={m.id} style={{ ['--mk' as string]: colorOf(m) }}>
            <button type="button" className="mark-time" onClick={() => onSeek(m.at_ms / 1000)} title={`Go to bar ${barBeat(m.at_ms / 1000, tempo, beats)}`}>
              {clock(m.at_ms / 1000)}{m.end_ms > m.at_ms && <span className="faint">–{clock(m.end_ms / 1000)}</span>}
            </button>
            <button type="button" className="mark-text" onClick={e => onOpen(m, rectOf(e.currentTarget))}>
              {t && <span className="mark-trk">{t.name}</span>}
              <span>{m.text || <i className="muted">No words</i>}</span>
            </button>
            <button type="button" className="btn ghost icon-only xs mark-del" onClick={() => onDelete(m)} title="Delete the note" aria-label="Delete the note"><Icon name="trash" size={13} /></button>
          </li>
        )
      })}
    </ul>
  )
}
