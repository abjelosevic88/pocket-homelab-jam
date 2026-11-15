// The tuner: a needle, the note and the cents, with the strings of the
// instrument picked (the nearest string lights up).
import { useEffect, useRef, useState } from 'react'
import { useApp } from '../lib/app-context'
import { api, V1 } from '../lib/api'
import { useCrumb } from '../components/Layout'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { audioContext } from '../lib/engine'
import { PRESETS, detectPitch, hzToMidi, midiToHz, nearestString, noteName } from '../lib/tuner'
import { errMsg } from '../components/ui'

export default function Tuner() {
  const { settings, setSettings } = useApp()
  useCrumb(undefined)
  const [preset, setPreset] = useState(settings?.tuner_preset || 'guitar')
  const [a4, setA4] = useState(settings?.a4 || 440)
  const [on, setOn] = useState(false)
  const [err, setErr] = useState('')
  const [reading, setReading] = useState<{ midi: number; cents: number; hz: number } | null>(null)
  const p = PRESETS.find(x => x.id === preset) || PRESETS[0]
  const stop = useRef<() => void>(() => {})

  useEffect(() => () => stop.current(), [])
  const start = async () => {
    setErr('')
    try {
      const ctx = audioContext()
      const stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: false, noiseSuppression: false, autoGainControl: false } })
      await ctx.resume()
      const src = ctx.createMediaStreamSource(stream)
      const an = ctx.createAnalyser()
      an.fftSize = 4096
      src.connect(an)
      const buf = new Float32Array(an.fftSize)
      let raf = 0, smooth = 0, lastMidi = -1, quiet = 0
      const frame = () => {
        an.getFloatTimeDomainData(buf)
        const r = detectPitch(buf, ctx.sampleRate)
        if (r) {
          quiet = 0
          const m = hzToMidi(r.hz, a4Ref.current)
          const near = Math.round(m)
          if (near !== lastMidi) { smooth = (m - near) * 100; lastMidi = near } else smooth = smooth * 0.75 + (m - near) * 100 * 0.25
          setReading({ midi: near, cents: smooth, hz: r.hz })
        } else if (++quiet > 40) { setReading(null); lastMidi = -1 }
        raf = requestAnimationFrame(frame)
      }
      raf = requestAnimationFrame(frame)
      stop.current = () => { cancelAnimationFrame(raf); src.disconnect(); stream.getTracks().forEach(t => t.stop()); setOn(false); setReading(null) }
      setOn(true)
    } catch (e) {
      const n = (e as { name?: string }).name
      setErr(n === 'NotAllowedError' ? 'The microphone is blocked: allow it for this site in the browser, then try again.' : errMsg(e))
    }
  }
  const a4Ref = useRef(a4)
  a4Ref.current = a4
  const keep = (patch: { tuner_preset?: string; a4?: number }) => { api.put(`${V1}/settings`, patch).then(s => setSettings(s as never)).catch(() => {}) }

  const target = reading ? nearestString(p, reading.midi) : null
  const cents = reading ? (target !== null && target !== reading.midi ? (reading.midi - target) * 100 + reading.cents : reading.cents) : 0
  const inTune = reading && Math.abs(cents) < 5
  const angle = Math.max(-50, Math.min(50, cents)) * 0.9
  return (
    <div className="page tuner-page">
      <div className="tuner-controls">
        <Select ariaLabel="Instrument" value={preset} onChange={v => { setPreset(v); keep({ tuner_preset: v }) }} options={PRESETS.map(x => ({ value: x.id, label: x.name }))} />
        <div className="row" style={{ gap: 6 }}>
          <span className="muted small">A =</span>
          <button type="button" className="btn sm" onClick={() => { const v = Math.max(415, a4 - 1); setA4(v); keep({ a4: v }) }} aria-label="Lower the reference">−</button>
          <b className="tnum">{a4} Hz</b>
          <button type="button" className="btn sm" onClick={() => { const v = Math.min(466, a4 + 1); setA4(v); keep({ a4: v }) }} aria-label="Raise the reference">+</button>
        </div>
      </div>
      <div className={`tuner ${inTune ? 'good' : ''} ${reading ? '' : 'idle'}`}>
        <svg viewBox="0 0 300 170" width={300} height={170} className="tuner-dial" aria-hidden>
          {Array.from({ length: 21 }, (_, i) => {
            const c = (i - 10) * 5
            const a = (c * 0.9 - 90) * Math.PI / 180
            const r1 = 128, r2 = i % 5 === 0 ? 108 : 116
            return <line key={i} x1={150 + r1 * Math.cos(a)} y1={150 + r1 * Math.sin(a)} x2={150 + r2 * Math.cos(a)} y2={150 + r2 * Math.sin(a)} className={c === 0 ? 'tick zero' : 'tick'} />
          })}
          <g style={{ transform: `rotate(${angle}deg)`, transformOrigin: '150px 150px', transition: 'transform .08s linear' }}><line x1={150} y1={150} x2={150} y2={30} className="needle" /></g>
          <circle cx={150} cy={150} r={7} className="hub" />
        </svg>
        <div className="tuner-note">{reading ? noteName(target ?? reading.midi, false) : '–'}<sub>{reading ? Math.floor((target ?? reading.midi) / 12) - 1 : ''}</sub></div>
        <div className="tuner-cents">{reading ? (inTune ? 'In tune' : `${cents > 0 ? '+' : ''}${Math.round(cents)} cents · ${cents > 0 ? 'too high' : 'too low'}`) : on ? 'Play a note' : ' '}</div>
        <div className="muted small tnum">{reading ? `${reading.hz.toFixed(1)} Hz` : ' '}</div>
      </div>
      {!!p.strings.length && <div className="strings">
        {p.strings.map((s, i) => (
          <div key={i} className={`string ${target === s && reading ? (inTune ? 'good' : 'near') : ''}`}>
            <b>{noteName(s, false)}</b><span className="muted small">{midiToHz(s, a4).toFixed(1)}</span>
          </div>
        ))}
      </div>}
      <div className="row" style={{ justifyContent: 'center' }}>
        {on ? <button type="button" className="btn" onClick={() => stop.current()}><Icon name="mic-off" size={15} />Stop listening</button>
          : <button type="button" className="btn primary lg" onClick={start}><Icon name="mic" size={16} />Start the tuner</button>}
      </div>
      {err && <p className="bad small center">{err}</p>}
      <p className="muted small center">Hold the phone near the instrument; play one string at a time and let it ring.</p>
    </div>
  )
}
