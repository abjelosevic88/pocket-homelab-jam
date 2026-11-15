// A take's or a mix's waveform, drawn from its peaks (50 a second, 0..255)
// at real size: one canvas pixel per screen pixel.
import { useEffect, useRef } from 'react'
import { peaksBytes } from '../lib/engine'

const cache = new Map<string, Uint8Array>()
function bytes(b64: string) {
  let v = cache.get(b64)
  if (!v) { v = peaksBytes(b64); if (cache.size > 200) cache.clear(); cache.set(b64, v) }
  return v
}

/**
 * pxPerSec: how wide one second is; `from`/`width`: the part of the take to
 * draw (seconds from its start, CSS pixels), so long takes draw only what shows.
 */
export function Waveform({ peaks, pxPerSec, width, height, color, className = '', from = 0 }: { peaks: string; pxPerSec: number; width: number; height: number; color: string; className?: string; from?: number }) {
  const ref = useRef<HTMLCanvasElement>(null)
  useEffect(() => {
    const c = ref.current
    if (!c || width <= 0) return
    const dpr = window.devicePixelRatio || 1
    c.width = Math.round(width * dpr)
    c.height = Math.round(height * dpr)
    const g = c.getContext('2d')!
    g.setTransform(dpr, 0, 0, dpr, 0, 0)
    g.clearRect(0, 0, width, height)
    const p = bytes(peaks)
    if (!p.length) return
    g.fillStyle = color
    const mid = height / 2
    const perPx = 50 / pxPerSec // peaks per pixel
    const start = from * 50
    for (let x = 0; x < width; x++) {
      const a = Math.floor(start + x * perPx)
      const b = Math.max(a + 1, Math.floor(start + (x + 1) * perPx))
      if (a >= p.length) break
      let m = 0
      for (let i = a; i < b && i < p.length; i++) if (p[i] > m) m = p[i]
      const h = Math.max(1, Math.sqrt(m / 255) * (height - 2) / 2)
      g.fillRect(x, mid - h, 1, h * 2)
    }
  }, [peaks, pxPerSec, width, height, color, from])
  return <canvas ref={ref} className={`wave ${className}`} style={{ width, height }} aria-hidden />
}
