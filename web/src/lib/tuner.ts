// The tuner: the pitch of what the microphone hears (YIN), the nearest note
// and how far off it is in cents, and the strings of common instruments.

export const NOTE_NAMES = ['C', 'C♯', 'D', 'E♭', 'E', 'F', 'F♯', 'G', 'A♭', 'A', 'B♭', 'B']

export interface Preset { id: string; name: string; strings: number[] } // MIDI notes, low to high

export const PRESETS: Preset[] = [
  { id: 'chromatic', name: 'Any note', strings: [] },
  { id: 'guitar', name: 'Guitar — standard', strings: [40, 45, 50, 55, 59, 64] },
  { id: 'guitar_drop_d', name: 'Guitar — drop D', strings: [38, 45, 50, 55, 59, 64] },
  { id: 'guitar_dadgad', name: 'Guitar — DADGAD', strings: [38, 45, 50, 55, 57, 62] },
  { id: 'guitar_open_g', name: 'Guitar — open G', strings: [38, 43, 50, 55, 59, 62] },
  { id: 'bass', name: 'Bass — 4 strings', strings: [28, 33, 38, 43] },
  { id: 'bass5', name: 'Bass — 5 strings', strings: [23, 28, 33, 38, 43] },
  { id: 'ukulele', name: 'Ukulele', strings: [67, 60, 64, 69] },
  { id: 'violin', name: 'Violin', strings: [55, 62, 69, 76] },
  { id: 'viola', name: 'Viola', strings: [48, 55, 62, 69] },
  { id: 'cello', name: 'Cello', strings: [36, 43, 50, 57] },
  { id: 'mandolin', name: 'Mandolin', strings: [55, 62, 69, 76] },
  { id: 'banjo', name: 'Banjo — open G', strings: [67, 50, 55, 59, 62] },
]

export function noteName(midi: number, octave = true): string {
  const n = NOTE_NAMES[((Math.round(midi) % 12) + 12) % 12]
  return octave ? `${n}${Math.floor(Math.round(midi) / 12) - 1}` : n
}

export function hzToMidi(hz: number, a4: number) { return 69 + 12 * Math.log2(hz / a4) }
export function midiToHz(m: number, a4: number) { return a4 * Math.pow(2, (m - 69) / 12) }

/**
 * YIN pitch detection on one frame. Returns Hz, or null when there is no
 * clear note (too quiet, noise, a chord).
 */
export function detectPitch(x: Float32Array, rate: number, minHz = 30, maxHz = 1500): { hz: number; clarity: number } | null {
  let rms = 0
  for (let i = 0; i < x.length; i++) rms += x[i] * x[i]
  rms = Math.sqrt(rms / x.length)
  if (rms < 0.006) return null
  const maxTau = Math.min(Math.floor(rate / minHz), Math.floor(x.length / 2))
  const minTau = Math.max(2, Math.floor(rate / maxHz))
  const W = x.length - maxTau
  const d = new Float32Array(maxTau + 1)
  for (let tau = 1; tau <= maxTau; tau++) {
    let s = 0
    for (let i = 0; i < W; i++) { const v = x[i] - x[i + tau]; s += v * v }
    d[tau] = s
  }
  // cumulative mean normalised difference
  const cm = new Float32Array(maxTau + 1)
  cm[0] = 1
  let run = 0
  for (let tau = 1; tau <= maxTau; tau++) { run += d[tau]; cm[tau] = run === 0 ? 1 : d[tau] * tau / run }
  const threshold = 0.12
  let tau = -1
  for (let t = minTau; t <= maxTau; t++) {
    if (cm[t] < threshold) {
      while (t + 1 <= maxTau && cm[t + 1] < cm[t]) t++
      tau = t
      break
    }
  }
  if (tau < 0) {
    // no dip under the threshold: take the deepest if it is still fairly clear
    let best = minTau
    for (let t = minTau; t <= maxTau; t++) if (cm[t] < cm[best]) best = t
    if (cm[best] > 0.25) return null
    tau = best
  }
  // parabolic interpolation
  const a = cm[tau - 1] ?? cm[tau], b = cm[tau], c = cm[tau + 1] ?? cm[tau]
  const den = a + c - 2 * b
  const shift = den !== 0 ? (a - c) / (2 * den) : 0
  const hz = rate / (tau + shift)
  if (!Number.isFinite(hz) || hz < minHz || hz > maxHz) return null
  return { hz, clarity: 1 - Math.min(1, b) }
}

/** The string of a preset nearest to a note (by MIDI). */
export function nearestString(p: Preset, midi: number): number | null {
  if (!p.strings.length) return null
  let best = p.strings[0]
  for (const s of p.strings) if (Math.abs(s - midi) < Math.abs(best - midi)) best = s
  return best
}
