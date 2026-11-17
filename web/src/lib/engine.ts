// The sound of a song: every track's playing take through its own volume and
// balance, a metronome click on the song's beats, and the microphone recorded
// sample by sample (public/recorder-worklet.js) while the rest plays. A take
// is placed where the music was heard: the device's delay (out to the
// headphones, back in from the microphone) is taken off its start.
import { V1 } from './api'
import type { Song, Take, Track } from './types'

type AC = typeof AudioContext
const AudioCtx: AC = window.AudioContext || (window as unknown as { webkitAudioContext: AC }).webkitAudioContext

let shared: AudioContext | null = null
let workletReady: Promise<void> | null = null

/** The page's one AudioContext (made on the first tap; browsers want a gesture). */
export function audioContext(): AudioContext {
  if (!shared || shared.state === 'closed') {
    shared = new AudioCtx({ latencyHint: 'interactive' })
    workletReady = null
  }
  if (shared.state === 'suspended') void shared.resume()
  return shared
}

function ensureWorklet(ctx: AudioContext): Promise<void> {
  if (!workletReady) workletReady = ctx.audioWorklet.addModule('/recorder-worklet.js')
  return workletReady
}

/**
 * iOS (Safari 16.4+): "play-and-record" while the microphone is open — iOS
 * then plays through the earpiece, quietly — and "playback" otherwise: the
 * loudspeaker (or headphones), and heard even with the silent switch on.
 */
export function audioSession(type: 'play-and-record' | 'playback' | 'auto') {
  const s = (navigator as unknown as { audioSession?: { type: string } }).audioSession
  if (s) try { s.type = type } catch { /* older Safari */ }
}

// ---- the device's delay ----

const LAT_KEY = 'pjam-latency'

/** The measured round trip of this device in ms, or null when never measured. */
export function savedLatency(): number | null {
  const v = Number(localStorage.getItem(LAT_KEY))
  return localStorage.getItem(LAT_KEY) !== null && Number.isFinite(v) ? v : null
}
export function saveLatency(ms: number | null) {
  if (ms === null) localStorage.removeItem(LAT_KEY)
  else localStorage.setItem(LAT_KEY, String(Math.round(ms)))
}

/** A guess when nothing was measured: what the browser reports, else typical values. */
export function guessedLatency(ctx: AudioContext, input?: MediaStreamTrack): number {
  const out = (ctx.outputLatency || 0) + (ctx.baseLatency || 0)
  const inp = (input?.getSettings() as { latency?: number } | undefined)?.latency || 0
  let ms = (out + inp) * 1000
  if (ms < 5) ms = /iPhone|iPad|Android/i.test(navigator.userAgent) ? 45 : 25
  return Math.round(ms)
}

export function latencyMs(ctx: AudioContext, input?: MediaStreamTrack): number {
  return savedLatency() ?? guessedLatency(ctx, input)
}

// ---- takes as sound ----

const buffers = new Map<number, AudioBuffer>()
const loading = new Map<number, Promise<AudioBuffer>>()

/** A take's sound, fetched and decoded once per page. */
export function loadTake(id: number): Promise<AudioBuffer> {
  const have = buffers.get(id)
  if (have) return Promise.resolve(have)
  let p = loading.get(id)
  if (!p) {
    p = fetch(`${V1}/takes/${id}/audio`, { credentials: 'same-origin' })
      .then(r => { if (!r.ok) throw new Error(`take ${id}: ${r.status}`); return r.arrayBuffer() })
      .then(b => decode(audioContext(), b))
      .then(buf => { buffers.set(id, buf); loading.delete(id); return buf })
      .catch(e => { loading.delete(id); throw e })
    loading.set(id, p)
  }
  return p
}
export function cachedTake(id: number) { return buffers.get(id) }
export function rememberTake(id: number, buf: AudioBuffer) { buffers.set(id, buf) }

/** decodeAudioData with the old callback form for older Safari. */
export function decode(ctx: BaseAudioContext, data: ArrayBuffer): Promise<AudioBuffer> {
  return new Promise((ok, bad) => {
    const p = ctx.decodeAudioData(data, ok, () => bad(new Error('This file cannot be played here (try WAV, MP3 or M4A)')))
    if (p && typeof p.catch === 'function') p.catch(() => bad(new Error('This file cannot be played here (try WAV, MP3 or M4A)')))
  })
}

// ---- the mix ----

export interface PlayOpts { click: boolean; countInFrom?: number; tempo: number; beats: number; clickSound: string; clickVolume: number }

interface Voice { src: AudioBufferSourceNode; gain: GainNode; pan: StereoPannerNode; meter: AnalyserNode; trackId: number }

/** Is a track heard? Solo wins over everything; muted never. */
export function audible(t: Track, tracks: Track[]): boolean {
  const solo = tracks.some(x => x.solo)
  return solo ? t.solo : !t.muted
}

export function activeTake(t: Track): Take | undefined {
  return t.takes.find(k => k.id === t.take_id)
}

/** Plays the song from a point, with the click; reports where it is. */
export class Player {
  ctx: AudioContext
  master: GainNode
  private voices: Voice[] = []
  private t0 = 0 // ctx time of song time `from`
  private from = 0
  private clickTimer = 0
  private nextBeat = 0
  private opts: PlayOpts | null = null
  playing = false

  constructor() {
    this.ctx = audioContext()
    this.master = this.ctx.createGain()
    this.master.connect(this.ctx.destination)
  }

  /** The song time now (seconds; negative during a count-in). */
  position(): number {
    return this.playing ? this.from + (this.ctx.currentTime - this.t0) : this.from
  }
  /** Song time → ctx time while playing. */
  ctxTimeOf(songTime: number) { return this.t0 + (songTime - this.from) }
  songTimeOf(ctxTime: number) { return this.from + (ctxTime - this.t0) }

  seek(sec: number) { this.from = sec }

  /** Starts every heard track at `from` (seconds of the song). */
  play(tracks: Track[], from: number, opts: PlayOpts) {
    this.stop(false)
    void this.ctx.resume()
    this.from = from
    this.t0 = this.ctx.currentTime + 0.06
    this.opts = opts
    this.playing = true
    for (const t of tracks) {
      const k = activeTake(t)
      const buf = k && buffers.get(k.id)
      if (!k || !buf) continue
      const off = k.offset_ms / 1000
      const src = this.ctx.createBufferSource()
      src.buffer = buf
      const gain = this.ctx.createGain()
      const pan = this.ctx.createStereoPanner()
      const meter = this.ctx.createAnalyser()
      meter.fftSize = 1024
      src.connect(gain).connect(pan).connect(this.master)
      pan.connect(meter)
      this.voices.push({ src, gain, pan, meter, trackId: t.id })
      if (from < off) src.start(this.t0 + (off - from), 0)
      else if (from - off < buf.duration) src.start(this.t0, from - off)
      else continue
    }
    this.mixer(tracks)
    // the click: scheduled a little ahead, on the song's own beat grid
    const beat = 60 / opts.tempo
    this.nextBeat = Math.ceil((from - 1e-6) / beat)
    const tick = () => {
      if (!this.playing || !this.opts) return
      const ahead = this.position() + 0.15
      while (this.nextBeat * beat < ahead) {
        const st = this.nextBeat * beat
        const inCountIn = this.opts.countInFrom !== undefined && st < this.opts.countInFrom - 1e-6
        if (this.opts.click || inCountIn) {
          const bar = ((this.nextBeat % this.opts.beats) + this.opts.beats) % this.opts.beats
          click(this.ctx, this.master, this.ctxTimeOf(st), bar === 0, this.opts.clickSound, this.opts.clickVolume / 100 * (inCountIn ? 1 : 0.8))
        }
        this.nextBeat++
      }
    }
    tick()
    this.clickTimer = window.setInterval(tick, 25)
  }

  /** Volume, balance, mute and solo, live. */
  mixer(tracks: Track[]) {
    for (const v of this.voices) {
      const t = tracks.find(x => x.id === v.trackId)
      if (!t) continue
      const g = audible(t, tracks) ? t.volume / 100 : 0
      v.gain.gain.setTargetAtTime(g, this.ctx.currentTime, 0.015)
      v.pan.pan.setTargetAtTime(t.pan / 100, this.ctx.currentTime, 0.015)
    }
  }

  /** How loud each track is right now, after its fader (peak, 0..1+), for the mixer's meters. */
  levels(): Map<number, number> {
    const out = new Map<number, number>()
    if (!this.playing) return out
    for (const v of this.voices) {
      const d = this.meterBuf && this.meterBuf.length === v.meter.fftSize ? this.meterBuf : (this.meterBuf = new Float32Array(v.meter.fftSize))
      v.meter.getFloatTimeDomainData(d)
      let peak = 0
      for (let i = 0; i < d.length; i++) { const a = Math.abs(d[i]); if (a > peak) peak = a }
      out.set(v.trackId, Math.max(out.get(v.trackId) || 0, peak))
    }
    return out
  }
  private meterBuf: Float32Array<ArrayBuffer> | null = null

  stop(keepPosition = true) {
    if (keepPosition && this.playing) this.from = this.position()
    this.playing = false
    window.clearInterval(this.clickTimer)
    for (const v of this.voices) { try { v.src.stop() } catch { /* not started */ } v.src.disconnect(); v.pan.disconnect(); v.meter.disconnect() }
    this.voices = []
  }

  dispose() { this.stop(); this.master.disconnect() }
}

/** One metronome click at ctx time `at`. */
export function click(ctx: BaseAudioContext, out: AudioNode, at: number, accent: boolean, sound: string, volume: number) {
  if (volume <= 0) return
  const g = ctx.createGain()
  g.connect(out)
  const o = ctx.createOscillator()
  const peak = Math.min(1, volume) * (accent ? 1 : 0.6)
  if (sound === 'beep') {
    o.type = 'sine'
    o.frequency.value = accent ? 1760 : 1320
    g.gain.setValueAtTime(0, at)
    g.gain.linearRampToValueAtTime(peak * 0.6, at + 0.002)
    g.gain.setValueAtTime(peak * 0.6, at + 0.05)
    g.gain.linearRampToValueAtTime(0, at + 0.07)
    o.connect(g)
    o.start(at); o.stop(at + 0.08)
    return
  }
  if (sound === 'wood') {
    o.type = 'triangle'
    o.frequency.setValueAtTime(accent ? 1250 : 900, at)
    o.frequency.exponentialRampToValueAtTime(accent ? 700 : 500, at + 0.04)
    g.gain.setValueAtTime(peak, at)
    g.gain.exponentialRampToValueAtTime(0.001, at + 0.06)
    o.connect(g)
    o.start(at); o.stop(at + 0.07)
    return
  }
  o.type = 'square'
  o.frequency.value = accent ? 2000 : 1400
  const f = ctx.createBiquadFilter()
  f.type = 'bandpass'
  f.frequency.value = accent ? 2600 : 1900
  g.gain.setValueAtTime(peak, at)
  g.gain.exponentialRampToValueAtTime(0.001, at + 0.03)
  o.connect(f).connect(g)
  o.start(at); o.stop(at + 0.04)
}

// ---- the microphone ----

export interface MicOpts { deviceId?: string; stereo: boolean; monitor: boolean }

/** An open microphone: a level, optional monitoring, and the recorder. */
export class Mic {
  ctx: AudioContext
  stream: MediaStream
  private src: MediaStreamAudioSourceNode
  private node: AudioWorkletNode
  private sink: GainNode
  private mon: GainNode
  analyser: AnalyserNode
  private chunks: Float32Array[][] = []
  private first = -1
  private done: (() => void) | null = null
  label: string

  private constructor(ctx: AudioContext, stream: MediaStream, node: AudioWorkletNode, out: AudioNode) {
    this.ctx = ctx
    this.stream = stream
    this.node = node
    this.label = stream.getAudioTracks()[0]?.label || 'Microphone'
    this.src = ctx.createMediaStreamSource(stream)
    this.analyser = ctx.createAnalyser()
    this.analyser.fftSize = 2048
    this.src.connect(this.analyser)
    this.src.connect(node)
    this.sink = ctx.createGain()
    this.sink.gain.value = 0
    node.connect(this.sink).connect(ctx.destination)
    this.mon = ctx.createGain()
    this.mon.gain.value = 0
    this.src.connect(this.mon).connect(out)
    node.port.onmessage = e => {
      const m = e.data as { first?: number; data?: Float32Array[]; done?: boolean }
      if (m.data) { if (this.first < 0) this.first = m.first!; this.chunks.push(m.data) }
      if (m.done && this.done) { this.done(); this.done = null }
    }
  }

  static async open(out: AudioNode, o: MicOpts): Promise<Mic> {
    if (!navigator.mediaDevices?.getUserMedia) throw new Error('This browser cannot use the microphone here (it needs https)')
    const ctx = audioContext()
    audioSession('play-and-record')
    const stream = await navigator.mediaDevices.getUserMedia({
      audio: { deviceId: o.deviceId ? { exact: o.deviceId } : undefined, echoCancellation: false, noiseSuppression: false, autoGainControl: false, channelCount: o.stereo ? { ideal: 2 } : { ideal: 1 } },
    })
    await ctx.resume()
    await ensureWorklet(ctx)
    const node = new AudioWorkletNode(ctx, 'jam-recorder', { numberOfInputs: 1, numberOfOutputs: 1, channelCount: o.stereo ? 2 : 1, channelCountMode: 'explicit' })
    const m = new Mic(ctx, stream, node, out)
    m.monitor(o.monitor)
    return m
  }

  monitor(on: boolean) { this.mon.gain.setTargetAtTime(on ? 1 : 0, this.ctx.currentTime, 0.02) }

  /** 0..1 loudness right now (for the meter). */
  level(): number {
    const a = new Float32Array(this.analyser.fftSize)
    this.analyser.getFloatTimeDomainData(a)
    let m = 0
    for (let i = 0; i < a.length; i++) m = Math.max(m, Math.abs(a[i]))
    return m
  }

  start() { this.chunks = []; this.first = -1; this.node.port.postMessage('start') }

  /** Stops recording; the samples and the ctx time of the first one. */
  stop(): Promise<{ channels: Float32Array[]; startTime: number; rate: number }> {
    return new Promise(ok => {
      this.done = () => {
        const chans = this.chunks[0]?.length || 1
        const len = this.chunks.reduce((n, c) => n + c[0].length, 0)
        const out: Float32Array[] = []
        for (let c = 0; c < chans; c++) {
          const b = new Float32Array(len)
          let at = 0
          for (const ch of this.chunks) { b.set(ch[c] || ch[0], at); at += ch[0].length }
          out.push(b)
        }
        this.chunks = []
        ok({ channels: out, startTime: this.first < 0 ? this.ctx.currentTime : this.first / this.ctx.sampleRate, rate: this.ctx.sampleRate })
      }
      this.node.port.postMessage('stop')
    })
  }

  close() {
    this.stream.getTracks().forEach(t => t.stop())
    try { this.src.disconnect(); this.node.disconnect(); this.mon.disconnect(); this.sink.disconnect() } catch { /* gone */ }
    audioSession('playback')
  }
}

/** The microphones the browser offers (names only after permission). */
export async function microphones(): Promise<MediaDeviceInfo[]> {
  try { return (await navigator.mediaDevices.enumerateDevices()).filter(d => d.kind === 'audioinput') } catch { return [] }
}

// ---- recording a take ----

export interface Recorded { channels: Float32Array[]; rate: number; offsetMs: number; latencyMs: number }

/**
 * Places what the mic caught on the song's timeline: `firstSongTime` is the
 * song time the first sample was captured at; the device delay moves it
 * earlier. Everything before `from` (the count-in) is cut.
 */
export function placeTake(channels: Float32Array[], rate: number, firstSongTime: number, from: number, latency: number): Recorded {
  const start = firstSongTime - latency / 1000
  const cut = Math.max(0, Math.round((from - start) * rate))
  const out = channels.map(c => c.subarray(Math.min(cut, c.length)))
  const offset = start + cut / rate
  return { channels: out, rate, offsetMs: Math.round(offset * 1000), latencyMs: latency }
}

/**
 * Raises a quiet take so its loudest moment is near full scale (at most 30 dB
 * louder). Phones without automatic volume record a guitar across the room
 * very softly. Returns the gain in dB (0 when nothing changed).
 */
export function levelTake(channels: Float32Array[], target = 0.89, maxGain = 31.6): number {
  let peak = 0
  for (const c of channels) for (let i = 0; i < c.length; i++) { const a = c[i] < 0 ? -c[i] : c[i]; if (a > peak) peak = a }
  if (peak <= 0 || peak >= target * 0.7) return 0
  const g = Math.min(maxGain, target / peak)
  for (const c of channels) for (let i = 0; i < c.length; i++) c[i] *= g
  return Math.round(20 * Math.log10(g))
}

// ---- measuring the delay ----

/**
 * Plays eight clicks through the speaker and listens with the microphone (no
 * headphones). The delay is the median time from each click to the sound
 * arriving. Returns ms, or throws when the clicks were not heard.
 */
export async function measureLatency(onStep?: (n: number) => void): Promise<number> {
  const ctx = audioContext()
  const out = ctx.createGain()
  out.connect(ctx.destination)
  const mic = await Mic.open(out, { stereo: false, monitor: false })
  try {
    await new Promise(r => setTimeout(r, 400))
    mic.start()
    const t0 = ctx.currentTime + 0.5
    const gap = 0.5
    const times: number[] = []
    for (let i = 0; i < 8; i++) {
      const at = t0 + i * gap
      times.push(at)
      const o = ctx.createOscillator()
      const g = ctx.createGain()
      o.type = 'square'
      o.frequency.value = 1000
      g.gain.setValueAtTime(0.9, at)
      g.gain.setValueAtTime(0, at + 0.006)
      o.connect(g).connect(out)
      o.start(at); o.stop(at + 0.01)
    }
    for (let i = 0; i < 8; i++) { await new Promise(r => setTimeout(r, gap * 1000)); onStep?.(i + 1) }
    await new Promise(r => setTimeout(r, 900))
    const rec = await mic.stop()
    const x = rec.channels[0]
    const rate = rec.rate
    const found: number[] = []
    for (const at of times) {
      const a = Math.max(0, Math.round((at - rec.startTime) * rate))
      const b = Math.min(x.length, a + Math.round(0.45 * rate))
      if (b - a < 100) continue
      let peak = 0
      for (let i = a; i < b; i++) peak = Math.max(peak, Math.abs(x[i]))
      // the floor before the click
      let floor = 0
      const fa = Math.max(0, a - Math.round(0.05 * rate))
      for (let i = fa; i < a; i++) floor = Math.max(floor, Math.abs(x[i]))
      if (peak < 0.02 || peak < floor * 4) continue
      for (let i = a; i < b; i++) if (Math.abs(x[i]) > peak * 0.35) { found.push((i - a) / rate * 1000); break }
    }
    if (found.length < 4) throw new Error('The clicks were not heard clearly. Turn the volume up, take the headphones off, and try again somewhere quiet.')
    found.sort((p, q) => p - q)
    return Math.round(found[Math.floor(found.length / 2)])
  } finally {
    mic.close()
    out.disconnect()
  }
}

// ---- files ----

/** 16-bit PCM WAV from channels. */
export function encodeWav(channels: Float32Array[], rate: number): Blob {
  const n = channels[0]?.length || 0
  const ch = channels.length
  const buf = new ArrayBuffer(44 + n * ch * 2)
  const v = new DataView(buf)
  const w = (o: number, s: string) => { for (let i = 0; i < s.length; i++) v.setUint8(o + i, s.charCodeAt(i)) }
  w(0, 'RIFF'); v.setUint32(4, 36 + n * ch * 2, true); w(8, 'WAVE'); w(12, 'fmt ')
  v.setUint32(16, 16, true); v.setUint16(20, 1, true); v.setUint16(22, ch, true); v.setUint32(24, rate, true)
  v.setUint32(28, rate * ch * 2, true); v.setUint16(32, ch * 2, true); v.setUint16(34, 16, true); w(36, 'data'); v.setUint32(40, n * ch * 2, true)
  let o = 44
  for (let i = 0; i < n; i++) {
    for (let c = 0; c < ch; c++) {
      const s = Math.max(-1, Math.min(1, channels[c][i]))
      v.setInt16(o, s < 0 ? s * 0x8000 : s * 0x7fff, true)
      o += 2
    }
  }
  return new Blob([buf], { type: 'audio/wav' })
}

/** The waveform: the loudest sample in each 1/50 s, 0..255, base64. */
export function peaksOf(channels: Float32Array[], rate: number): string {
  const step = Math.max(1, Math.round(rate / 50))
  const n = channels[0]?.length || 0
  const out = new Uint8Array(Math.ceil(n / step))
  for (let i = 0, j = 0; i < n; i += step, j++) {
    let m = 0
    const end = Math.min(n, i + step)
    for (const c of channels) for (let k = i; k < end; k++) { const a = c[k] < 0 ? -c[k] : c[k]; if (a > m) m = a }
    out[j] = Math.min(255, Math.round(m * 255))
  }
  let s = ''
  for (let i = 0; i < out.length; i += 0x8000) s += String.fromCharCode(...out.subarray(i, i + 0x8000))
  return btoa(s)
}

export function bufferChannels(b: AudioBuffer): Float32Array[] {
  return Array.from({ length: b.numberOfChannels }, (_, i) => b.getChannelData(i))
}

export function peaksBytes(b64: string): Uint8Array {
  try {
    const s = atob(b64)
    const out = new Uint8Array(s.length)
    for (let i = 0; i < s.length; i++) out[i] = s.charCodeAt(i)
    return out
  } catch { return new Uint8Array(0) }
}

/** Sends audio with its facts (multipart: meta + file). */
export async function uploadAudio(url: string, file: Blob, name: string, meta: Record<string, unknown>, onProgress?: (f: number) => void): Promise<unknown> {
  const fd = new FormData()
  fd.append('meta', JSON.stringify(meta))
  fd.append('file', file, name)
  return new Promise((ok, bad) => {
    const x = new XMLHttpRequest()
    x.open('POST', url)
    x.withCredentials = true
    x.upload.onprogress = e => { if (e.lengthComputable) onProgress?.(e.loaded / e.total) }
    x.onload = () => {
      let j: { error?: string } = {}
      try { j = JSON.parse(x.responseText) } catch { /* empty */ }
      if (x.status >= 200 && x.status < 300) ok(j)
      else {
        if (x.status === 401) window.dispatchEvent(new CustomEvent('pjam:unauthorized'))
        bad(new Error(j.error || `Upload failed (${x.status})`))
      }
    }
    x.onerror = () => bad(new Error('The upload broke off: check the connection'))
    x.send(fd)
  })
}

// ---- mixing down ----

/** Renders the heard tracks to stereo at 44.1 kHz, peaks kept under 0 dB; `range` (seconds of the song) renders just that part. */
export async function mixDown(song: Song, tracks: Track[], range?: { a: number; b: number }): Promise<{ wav: Blob; channels: Float32Array[]; rate: number; ms: number; used: number }> {
  const rate = 44100
  const from = range ? range.a : 0
  let heard = tracks.filter(t => audible(t, tracks) && activeTake(t))
  for (const t of heard) await loadTake(activeTake(t)!.id)
  if (range) heard = heard.filter(t => { const k = activeTake(t)!, off = k.offset_ms / 1000; return off < range.b && off + buffers.get(k.id)!.duration > range.a })
  let end = 0
  for (const t of heard) {
    const k = activeTake(t)!
    const b = buffers.get(k.id)!
    end = Math.max(end, k.offset_ms / 1000 + b.duration)
  }
  if (end <= 0) throw new Error(range ? 'Nothing is heard in this part' : 'Nothing to mix: record something first (or unmute a track)')
  const OC = window.OfflineAudioContext || (window as unknown as { webkitOfflineAudioContext: typeof OfflineAudioContext }).webkitOfflineAudioContext
  const oc = new OC(2, Math.max(1, Math.ceil(range ? (range.b - range.a) * rate : (end + 0.2) * rate)), rate)
  for (const t of heard) {
    const k = activeTake(t)!
    const src = oc.createBufferSource()
    src.buffer = buffers.get(k.id)!
    const g = oc.createGain()
    g.gain.value = t.volume / 100
    const p = oc.createStereoPanner()
    p.pan.value = t.pan / 100
    src.connect(g).connect(p).connect(oc.destination)
    const off = k.offset_ms / 1000 - from
    if (off >= 0) src.start(off, 0)
    else src.start(0, -off)
  }
  const out = await oc.startRendering()
  const chans = bufferChannels(out)
  let peak = 0
  for (const c of chans) for (let i = 0; i < c.length; i++) { const a = Math.abs(c[i]); if (a > peak) peak = a }
  if (peak > 0.98) { const f = 0.98 / peak; for (const c of chans) for (let i = 0; i < c.length; i++) c[i] *= f }
  void song
  return { wav: encodeWav(chans, rate), channels: chans, rate, ms: Math.round(out.duration * 1000), used: heard.length }
}

/** Seconds as 1:05.3 (or 1:05 when `tenths` is false). */
export function clock(sec: number, tenths = true): string {
  const neg = sec < 0
  const s = Math.abs(sec)
  const m = Math.floor(s / 60)
  const r = s - m * 60
  const body = tenths ? `${m}:${r.toFixed(1).padStart(4, '0')}` : `${m}:${String(Math.floor(r)).padStart(2, '0')}`
  return (neg ? '-' : '') + body
}

/** Bar and beat of a song time: "5.3". */
export function barBeat(sec: number, tempo: number, beats: number): string {
  const b = Math.floor(sec / (60 / tempo) + 1e-6)
  if (b < 0) return `-${-b}`
  return `${Math.floor(b / beats) + 1}.${(b % beats) + 1}`
}
