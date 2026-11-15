// The metronome: tempo with a dial and tap, beats in a bar with an accent,
// subdivisions, the sound and its volume; the beat lights up.
import { useEffect, useRef, useState } from 'react'
import { useApp } from '../lib/app-context'
import { useCrumb } from '../components/Layout'
import { Icon } from '../components/icons'
import { Segmented } from '../components/ui'
import { audioContext, click } from '../lib/engine'

const KEY = 'pjam-metronome'
interface State { tempo: number; beats: number; sub: number; accent: boolean; sound: string; volume: number }

export default function Metronome() {
  const { settings } = useApp()
  useCrumb(undefined)
  const [s, setS] = useState<State>(() => {
    try { const v = JSON.parse(localStorage.getItem(KEY) || ''); if (v.tempo) return v } catch { /* first time */ }
    return { tempo: settings?.tempo || 100, beats: settings?.beats || 4, sub: 1, accent: true, sound: settings?.click_sound || 'click', volume: settings?.click_volume ?? 70 }
  })
  useEffect(() => { localStorage.setItem(KEY, JSON.stringify(s)) }, [s])
  const [on, setOn] = useState(false)
  const [beat, setBeat] = useState(-1)
  const sRef = useRef(s)
  sRef.current = s
  const timer = useRef(0)
  const taps = useRef<number[]>([])

  useEffect(() => {
    if (!on) { setBeat(-1); return }
    const ctx = audioContext()
    void ctx.resume()
    const out = ctx.createGain()
    out.connect(ctx.destination)
    let next = ctx.currentTime + 0.08
    let n = 0
    const vis: number[] = []
    const tick = () => {
      const st = sRef.current
      const step = 60 / st.tempo / st.sub
      while (next < ctx.currentTime + 0.12) {
        const inBeat = n % st.sub
        const b = Math.floor(n / st.sub) % st.beats
        if (inBeat === 0) {
          click(ctx, out, next, st.accent && b === 0, st.sound, st.volume / 100)
          const at = next, bb = b
          vis.push(window.setTimeout(() => setBeat(bb), Math.max(0, (at - ctx.currentTime) * 1000)))
        } else click(ctx, out, next, false, st.sound, st.volume / 100 * 0.45)
        next += step
        n++
      }
      if (vis.length > 64) vis.splice(0, 32)
    }
    tick()
    timer.current = window.setInterval(tick, 25)
    return () => { window.clearInterval(timer.current); vis.forEach(t => window.clearTimeout(t)); out.disconnect() }
  }, [on])
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if ((e.target as HTMLElement).closest('input, textarea') || document.querySelector('.modal-backdrop, .sel-pop')) return
      if (e.key === ' ') { e.preventDefault(); setOn(o => !o) }
      if (e.key === 'ArrowUp' || e.key === '+') setS(x => ({ ...x, tempo: Math.min(300, x.tempo + 1) }))
      if (e.key === 'ArrowDown' || e.key === '-') setS(x => ({ ...x, tempo: Math.max(20, x.tempo - 1) }))
      if (e.key === 't') tap()
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  })
  const tap = () => {
    const now = performance.now()
    taps.current = [...taps.current.filter(t => now - t < 2500), now].slice(-8)
    if (taps.current.length >= 3) {
      const g = taps.current.slice(1).map((t, i) => t - taps.current[i])
      setS(x => ({ ...x, tempo: Math.max(20, Math.min(300, Math.round(60000 / (g.reduce((a, b) => a + b, 0) / g.length)))) }))
    }
  }
  const set = (p: Partial<State>) => setS(x => ({ ...x, ...p }))
  return (
    <div className="page metro-page">
      <div className="metro-beats">{Array.from({ length: s.beats }, (_, i) => <span key={i} className={`mbeat ${beat === i ? 'on' : ''} ${i === 0 && s.accent ? 'acc' : ''}`} />)}</div>
      <div className="metro-tempo">
        <button type="button" className="btn icon-only lg" onClick={() => set({ tempo: Math.max(20, s.tempo - 1) })} aria-label="Slower"><Icon name="minus" size={20} /></button>
        <div className="metro-bpm"><b className="tnum">{s.tempo}</b><span className="muted">beats a minute</span></div>
        <button type="button" className="btn icon-only lg" onClick={() => set({ tempo: Math.min(300, s.tempo + 1) })} aria-label="Faster"><Icon name="plus" size={20} /></button>
      </div>
      <input type="range" className="metro-slider" min={20} max={300} value={s.tempo} onChange={e => set({ tempo: Number(e.target.value) })} aria-label="Tempo" />
      <div className="row" style={{ justifyContent: 'center', gap: 10 }}>
        <button type="button" className={`btn lg metro-go ${on ? '' : 'primary'}`} onClick={() => setOn(!on)}><Icon name={on ? 'square' : 'play'} size={18} />{on ? 'Stop' : 'Start'}</button>
        <button type="button" className="btn lg" onClick={tap} title="Tap the beat (T)">Tap</button>
      </div>
      <div className="metro-opts">
        <div className="metro-opt"><span className="muted small">Beats in a bar</span><Segmented size="sm" value={s.beats} onChange={v => set({ beats: v })} options={[2, 3, 4, 5, 6, 7].map(n => ({ value: n, label: String(n) }))} /></div>
        <div className="metro-opt"><span className="muted small">Each beat in</span><Segmented size="sm" value={s.sub} onChange={v => set({ sub: v })} options={[{ value: 1, label: '1' }, { value: 2, label: '2' }, { value: 3, label: '3' }, { value: 4, label: '4' }]} /></div>
        <div className="metro-opt"><span className="muted small">Sound</span><Segmented size="sm" value={s.sound} onChange={v => set({ sound: v })} options={[{ value: 'click', label: 'Click' }, { value: 'wood', label: 'Wood' }, { value: 'beep', label: 'Beep' }]} /></div>
        <div className="metro-opt"><span className="muted small">First beat louder</span><Segmented size="sm" value={s.accent ? 1 : 0} onChange={v => set({ accent: v === 1 })} options={[{ value: 1, label: 'Yes' }, { value: 0, label: 'No' }]} /></div>
        <div className="metro-opt"><span className="muted small">Volume</span><input type="range" min={0} max={100} value={s.volume} onChange={e => set({ volume: Number(e.target.value) })} aria-label="Volume" /></div>
      </div>
    </div>
  )
}
