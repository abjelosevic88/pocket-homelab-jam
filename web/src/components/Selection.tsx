// Selecting a part of a recording: drag along it (a click still seeks), the
// band with a handle at each end, and the bar under it with the times (typed
// or taken from the playhead) and what to do with the part. The idea rows and
// the song page share these.
import { useEffect, useRef, useState } from 'react'
import { Icon } from './icons'
import { useToast } from './ui'
import { clock } from '../lib/engine'
import { MIN_SEL, fixSel, parseTime, type Sel } from '../lib/part'

const DRAG_PX = 4 // farther than this and it's a drag, not a click

/**
 * Drag-to-select on a strip. `timeAt` turns a pointer's x into seconds;
 * `onClick` gets the time of a press that did not move. A drag starts only
 * when the pointer moves sideways, so on a phone the page still scrolls up
 * and down (give the strip `touch-action: pan-y`). Spread `strip` on the
 * strip; `edge('a' | 'b')` goes on the handles.
 */
export function useRangeSelect(o: { total: number; sel: Sel | null; timeAt: (clientX: number) => number; onSelect: (s: Sel) => void; onClick?: (sec: number) => void }) {
  const opts = useRef(o)
  opts.current = o
  const begin = (e: React.PointerEvent<HTMLElement>, edge: 'new' | 'a' | 'b') => {
    if (e.button !== 0) return
    const el = e.currentTarget
    const id = e.pointerId, x0 = e.clientX, y0 = e.clientY
    const at = (x: number) => Math.max(0, Math.min(opts.current.total, opts.current.timeAt(x)))
    const click = Math.max(0, opts.current.timeAt(x0)) // a click may go past the end (a song's timeline goes on)
    const t0 = at(x0)
    const s0 = opts.current.sel
    let moved = edge !== 'new'
    let last: Sel | null = null
    try { el.setPointerCapture(id) } catch { /* gone */ }
    const move = (ev: PointerEvent) => {
      if (ev.pointerId !== id) return
      const dx = Math.abs(ev.clientX - x0), dy = Math.abs(ev.clientY - y0)
      if (!moved) { if (dx <= DRAG_PX || dy > dx || opts.current.total <= 0) return; moved = true }
      const { total, onSelect } = opts.current
      const t = at(ev.clientX)
      if (edge === 'new') last = { a: Math.min(t0, t), b: Math.max(t0, t) }
      else if (s0 && edge === 'a') last = { a: Math.max(0, Math.min(t, s0.b - MIN_SEL)), b: s0.b }
      else if (s0) last = { a: s0.a, b: Math.min(total, Math.max(t, s0.a + MIN_SEL)) }
      if (last) onSelect(last)
    }
    const finish = (ev: PointerEvent, cancel: boolean) => {
      if (ev.pointerId !== id) return
      el.removeEventListener('pointermove', move)
      el.removeEventListener('pointerup', up)
      el.removeEventListener('pointercancel', cancelled)
      const { total, onSelect, onClick } = opts.current
      if (last) onSelect(fixSel(last, total))
      else if (!moved && !cancel) onClick?.(click)
    }
    const up = (ev: PointerEvent) => finish(ev, false)
    const cancelled = (ev: PointerEvent) => finish(ev, true)
    el.addEventListener('pointermove', move)
    el.addEventListener('pointerup', up)
    el.addEventListener('pointercancel', cancelled)
  }
  return {
    strip: (e: React.PointerEvent<HTMLElement>) => begin(e, 'new'),
    edge: (which: 'a' | 'b') => (e: React.PointerEvent<HTMLElement>) => { e.stopPropagation(); e.preventDefault(); begin(e, which) },
  }
}

/** The selected part drawn over a strip: a translucent band, a handle at each end. */
export function SelBand({ left, width, onEdge, className = '' }: { left: number | string; width: number | string; onEdge: (which: 'a' | 'b') => (e: React.PointerEvent<HTMLElement>) => void; className?: string }) {
  return (
    <div className={`sel-band ${className}`} style={{ left, width }}>
      <span className="sel-h a" onPointerDown={onEdge('a')} title="Drag to move the start" aria-hidden />
      <span className="sel-h b" onPointerDown={onEdge('b')} title="Drag to move the end" aria-hidden />
    </div>
  )
}

/** A time you can type (72.5, 1:12.5, 1:01:12): Enter or leaving applies it, Esc puts it back. */
function TimeField({ value, label, onApply, inputRef }: { value: number; label: string; onApply: (sec: number) => boolean; inputRef?: React.Ref<HTMLInputElement> }) {
  const { toast } = useToast()
  const [text, setText] = useState<string | null>(null) // null: showing the value
  const shown = clock(value)
  const apply = () => {
    if (text === null) return
    setText(null)
    if (text.trim() === shown) return
    const v = parseTime(text)
    if (v === null) { toast('Type a time like 1:05.3 or 65.3', 'err'); return }
    onApply(v)
  }
  return (
    <input ref={inputRef} className="sel-time" value={text ?? shown} aria-label={label} title={`${label}: type a time like 1:05.3 or 65.3`} inputMode="decimal" spellCheck={false} autoComplete="off"
      size={Math.max(6, (text ?? shown).length)}
      onFocus={e => e.currentTarget.select()} onChange={e => setText(e.target.value)} onBlur={apply}
      onKeyDown={e => {
        if (e.key === 'Enter') { e.preventDefault(); apply() }
        else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); setText(null); e.currentTarget.blur() }
      }} />
  )
}

/**
 * The bar under a selection: start – end (typed, or from the playhead) and
 * its length, then the actions (`children`) and Clear. `focus` changing puts
 * the cursor in the start, to type it straight away.
 */
export function SelBar({ sel, total, onChange, onClear, playhead, focus, busy, children, className = '' }: {
  sel: Sel; total: number; onChange: (s: Sel) => void; onClear: () => void; playhead: () => number; focus?: number; busy?: React.ReactNode
  children?: React.ReactNode; className?: string
}) {
  const { toast } = useToast()
  const start = useRef<HTMLInputElement>(null)
  useEffect(() => { if (focus) { start.current?.focus(); start.current?.select() } }, [focus])
  const range = `Pick a time from 0:00.0 to ${clock(total)}`
  const setA = (v: number) => {
    if (v < 0 || v > total) { toast(range, 'err'); return false }
    if (v > sel.b - MIN_SEL) { toast(`The start has to be before the end (by ${MIN_SEL} s at least)`, 'err'); return false }
    onChange({ a: v, b: sel.b }); return true
  }
  const setB = (v: number) => {
    if (v < 0 || v > total + 0.05) { toast(range, 'err'); return false }
    if (v < sel.a + MIN_SEL) { toast(`The end has to be after the start (by ${MIN_SEL} s at least)`, 'err'); return false }
    onChange({ a: sel.a, b: Math.min(total, v) }); return true
  }
  return (
    <div className={`sel-bar ${className}`} role="group" aria-label="The selected part">
      <div className="sel-times">
        <TimeField value={sel.a} label="Start" onApply={setA} inputRef={start} />
        <span className="muted">–</span>
        <TimeField value={sel.b} label="End" onApply={setB} />
        <span className="muted small sel-len">· {(sel.b - sel.a).toFixed(1)} s</span>
        <span className="sel-ph">
          <button type="button" className="btn ghost sm" onClick={() => setA(playhead())} title="Start the part at the playhead">Set start</button>
          <span className="faint small">/</span>
          <button type="button" className="btn ghost sm" onClick={() => setB(playhead())} title="End the part at the playhead">Set end</button>
          <span className="faint small">at the playhead</span>
        </span>
        <button type="button" className="btn ghost sm sel-clear" onClick={onClear} title="Clear the selection (Esc)"><Icon name="x" size={14} />Clear</button>
      </div>
      <div className="sel-actions">
        {children}
        {busy}
      </div>
    </div>
  )
}
