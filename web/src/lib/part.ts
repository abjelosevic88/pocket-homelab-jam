// A part of a recording (an idea's, a song's): the times, typing them, the
// samples in it, and its file name.
import { clock } from './engine'

/** A part in seconds, `a` before `b`. */
export interface Sel { a: number; b: number }

/** The shortest part (seconds). */
export const MIN_SEL = 0.2

/** Clamped to 0..total and at least MIN_SEL long (the end moves first). */
export function fixSel(s: Sel, total: number): Sel {
  let a = Math.max(0, Math.min(s.a, s.b)), b = Math.min(total, Math.max(s.a, s.b))
  if (b - a < MIN_SEL) { b = Math.min(total, a + MIN_SEL); a = Math.max(0, b - MIN_SEL) }
  return { a, b }
}

/** A typed time: 72.5, 1:12, 1:12.5, 1:01:12 → seconds; null when it is not one. */
export function parseTime(s: string): number | null {
  const t = s.trim().replace(',', '.')
  if (!t) return null
  const parts = t.split(':')
  if (parts.length > 3) return null
  const last = parts[parts.length - 1]
  if (!/^\d+(\.\d*)?$|^\.\d+$/.test(last)) return null
  if (parts.slice(0, -1).some(p => !/^\d+$/.test(p))) return null
  const sec = Number(last)
  if (parts.length > 1 && sec >= 60) return null
  if (parts.length === 3 && Number(parts[1]) >= 60) return null
  const v = parts.slice(0, -1).reduce((n, p) => n * 60 + Number(p), 0) * 60 + sec
  return Number.isFinite(v) ? v : null
}

/** "0:12–0:45", for names. */
export const rangeLabel = (s: Sel) => `${clock(s.a, false)}–${clock(s.b, false)}`

/** "<name> 0.12–0.45.wav": a file name any system takes. */
export function partFile(name: string, s: Sel): string {
  const safe = name.replace(/[\\/:*?"<>|\u0000-\u001f]+/g, '-').replace(/\s+/g, ' ').trim().slice(0, 100) || 'Part'
  const t = (x: number) => clock(x, false).replace(':', '.')
  return `${safe} ${t(s.a)}–${t(s.b)}.wav`
}

/** The samples of `s` (seconds of `buf`), every channel. */
export function sliceAudio(buf: AudioBuffer, s: Sel): Float32Array[] {
  const from = Math.max(0, Math.min(buf.length, Math.round(s.a * buf.sampleRate)))
  const to = Math.max(from, Math.min(buf.length, Math.round(s.b * buf.sampleRate)))
  return Array.from({ length: buf.numberOfChannels }, (_, i) => buf.getChannelData(i).slice(from, to))
}

/** Saves a blob as a file named `name`. */
export function saveBlob(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url; a.download = name
  document.body.appendChild(a); a.click(); a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 60_000)
}

/** A fresh client_id: the server keeps one idea per id, so sending again is safe. */
export function clientId(): string {
  const c = (globalThis.crypto as Crypto | undefined)
  if (c?.randomUUID) return c.randomUUID()
  return `web-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`
}
