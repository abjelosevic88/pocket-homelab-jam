// Bringing a recording in as a new track: a file from this device, from
// Pocket Files or from Pocket Music. The browser decodes it (for the
// waveform and its length) and sends the file as it is.
import { useEffect, useRef, useState } from 'react'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Instrument, Take, Track } from '../lib/types'
import { audioContext, bufferChannels, decode, peaksOf, rememberTake, uploadAudio } from '../lib/engine'
import { CoreHint, Loading, Segmented, Spinner, useDebounce, useToast, errMsg } from './ui'
import { Icon } from './icons'
import { fmtBytes } from '../lib/format'

type Source = 'device' | 'files' | 'music'

export function ImportSheet({ instrument, onAdd, onDone, onBack }: { songId: number; instrument: Instrument; onAdd: (i: Instrument) => Promise<Track | null>; onDone: (takeId: number) => Promise<void>; onBack: () => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const [src, setSrc] = useState<Source>('device')
  const [busy, setBusy] = useState('')
  const fileRef = useRef<HTMLInputElement>(null)
  const coreOn = !!settings?.core_configured && settings.core_registered !== false

  /** Decode, make a track, send the file as its first take. */
  const bring = async (blob: Blob, name: string, source: Take['source'], ref: string) => {
    try {
      setBusy('Reading the file…')
      const buf = await decode(audioContext(), await blob.arrayBuffer())
      const chans = bufferChannels(buf)
      setBusy('Saving…')
      const t = await onAdd(instrument)
      if (!t) return
      const take = await uploadAudio(`${V1}/tracks/${t.id}/takes`, blob, name, {
        duration_ms: Math.round(buf.duration * 1000), offset_ms: 0, sample_rate: buf.sampleRate, channels: buf.numberOfChannels, peaks: peaksOf(chans, buf.sampleRate), source, source_ref: ref,
      }, f => setBusy(`Saving… ${Math.round(f * 100)}%`)) as Take
      rememberTake(take.id, buf)
      await onDone(take.id)
      toast(`${name} is on a new track`)
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy('') }
  }
  const fetchAndBring = async (url: string, name: string, source: Take['source'], ref: string) => {
    try {
      setBusy('Fetching…')
      const r = await fetch(url, { credentials: 'same-origin' })
      if (!r.ok) { const j = await r.json().catch(() => ({})); throw new Error(j.error || `${r.status}`) }
      await bring(await r.blob(), name, source, ref)
    } catch (e) { toast(errMsg(e), 'err'); setBusy('') }
  }

  return (
    <div className="stack">
      <div className="row"><button type="button" className="btn ghost sm" onClick={onBack} disabled={!!busy}><Icon name="chevron-left" size={14} />Back</button></div>
      <Segmented value={src} onChange={setSrc} options={[
        { value: 'device', label: 'This device' },
        { value: 'files', label: 'Pocket Files', disabled: !settings?.files_via, title: settings?.files_via ? undefined : 'Needs Pocket Files (Settings → Connected apps)' },
        { value: 'music', label: 'Pocket Music', disabled: !settings?.music_via, title: settings?.music_via ? undefined : 'Needs Pocket Music (Settings → Connected apps)' },
      ]} />
      {(!settings?.files_via || !settings?.music_via) && !coreOn && <CoreHint />}
      {busy ? <div className="row" style={{ gap: 10, padding: '14px 0' }}><Spinner size={16} />{busy}</div> : <>
        {src === 'device' && <div className="stack">
          <p className="muted small">A recording or a backing track: WAV, MP3, M4A, AAC, FLAC or OGG. It starts at the beginning of the song; move it later if needed.</p>
          <div className="row"><button type="button" className="btn primary" onClick={() => fileRef.current?.click()}><Icon name="upload" size={15} />Choose a file…</button></div>
          <input ref={fileRef} type="file" hidden accept="audio/*,.wav,.mp3,.m4a,.aac,.flac,.ogg,.opus,.caf,.aif,.aiff" onChange={e => { const f = e.target.files?.[0]; e.target.value = ''; if (f) void bring(f, f.name, 'upload', '') }} />
        </div>}
        {src === 'files' && <FilesBrowser onPick={(space, path, name) => fetchAndBring(`${V1}/apps/files/raw?space=${encodeURIComponent(space)}&path=${encodeURIComponent(path)}`, name, 'files', `${space}:${path}`)} />}
        {src === 'music' && <MusicSearch onPick={(id, name) => fetchAndBring(`${V1}/apps/music/stream/${encodeURIComponent(id)}`, name, 'music', id)} />}
      </>}
    </div>
  )
}

interface Entry { name: string; path: string; kind: 'folder' | 'audio' | 'video'; size: number }

function FilesBrowser({ onPick }: { onPick: (space: string, path: string, name: string) => void }) {
  const space = 'home'
  const [path, setPath] = useState('/')
  const [list, setList] = useState<Entry[] | null>(null)
  const [err, setErr] = useState('')
  useEffect(() => {
    setList(null); setErr('')
    api.get<{ entries: Entry[] }>(`${V1}/apps/files?space=${encodeURIComponent(space)}&path=${encodeURIComponent(path)}`).then(r => setList(r.entries)).catch(e => setErr(errMsg(e)))
  }, [space, path])
  const up = path === '/' ? null : path.replace(/\/[^/]+\/?$/, '') || '/'
  return (
    <div className="stack">
      <div className="row muted small"><Icon name="archive" size={14} /><span className="ellipsis">{path}</span></div>
      {err && <p className="bad small">{err}</p>}
      {!list && !err && <Loading />}
      {list && <ul className="pick-list">
        {up && <li><button type="button" onClick={() => setPath(up)}><Icon name="arrow-up" size={15} />Up</button></li>}
        {list.map(e => (
          <li key={e.path}>
            <button type="button" onClick={() => e.kind === 'folder' ? setPath(e.path) : onPick(space, e.path, e.name)}>
              <Icon name={e.kind === 'folder' ? 'archive' : 'music'} size={15} /><span className="grow ellipsis">{e.name}</span>{e.kind !== 'folder' && <span className="muted small">{fmtBytes(e.size)}</span>}
            </button>
          </li>
        ))}
        {!list.length && <li className="muted small pad">No folders or audio files here.</li>}
      </ul>}
    </div>
  )
}

function MusicSearch({ onPick }: { onPick: (id: string, name: string) => void }) {
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 350)
  const [res, setRes] = useState<{ id: string; title: string; artist: string; duration_ms: number }[] | null>([])
  const [err, setErr] = useState('')
  useEffect(() => {
    if (!dq.trim()) { setRes([]); return }
    setRes(null); setErr('')
    api.get<{ songs: { id: string; title: string; artist: string; duration_ms: number }[] }>(`${V1}/apps/music?q=${encodeURIComponent(dq)}`).then(r => setRes(r.songs)).catch(e => setErr(errMsg(e)))
  }, [dq])
  return (
    <div className="stack">
      <input value={q} onChange={e => setQ(e.target.value)} placeholder="Find a song in your library (instrumentals make good backing tracks)" autoFocus />
      {err && <p className="bad small">{err}</p>}
      {!res && !err && <Loading />}
      {res && <ul className="pick-list">
        {res.map(s => (
          <li key={s.id}><button type="button" onClick={() => onPick(s.id, s.title)}><Icon name="music" size={15} /><span className="grow min0"><span className="ellipsis block-line">{s.title}</span><span className="muted small ellipsis block-line">{s.artist}</span></span></button></li>
        ))}
        {dq.trim() && !res.length && <li className="muted small pad">Nothing found.</li>}
      </ul>}
    </div>
  )
}
