// Every mix: play it, download it, keep it in Pocket Files, rename, delete.
import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Mix } from '../lib/types'
import { useOnChange, announceChange } from '../lib/data'
import { useCrumb } from '../components/Layout'
import { Confirm, Empty, Loading, MenuButton, Modal, Field, useToast, errMsg } from '../components/ui'
import { Icon } from '../components/icons'
import { Waveform } from '../components/Waveform'
import { clock } from '../lib/engine'
import { ago, fmtBytes } from '../lib/format'

export default function Mixes() {
  const [list, setList] = useState<Mix[] | null>(null)
  useCrumb(undefined)
  const load = useCallback(() => { api.get<{ mixes: Mix[] }>(`${V1}/mixes`).then(r => setList(r.mixes)).catch(() => setList([])) }, [])
  useEffect(() => { load() }, [load])
  useOnChange(load)
  if (!list) return <Loading />
  if (!list.length) return <div className="page"><Empty title="No mixes yet" icon="mixer">Open a song and choose ⋯ → <b>Mix down</b>: everything you hear becomes one stereo file to keep, share or put in Pocket Files.</Empty></div>
  return (
    <div className="page">
      <section className="card"><ul className="list">{list.map(m => <li key={m.id}><MixRow m={m} onChange={load} /></li>)}</ul></section>
    </div>
  )
}

/** One mix with a play button and its waveform; the newest one plays alone. */
export function MixRow({ m, compact, onChange }: { m: Mix; compact?: boolean; onChange?: () => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const audio = useRef<HTMLAudioElement | null>(null)
  const [playing, setPlaying] = useState(false)
  const [renaming, setRenaming] = useState(false)
  const [del, setDel] = useState(false)
  const [name, setName] = useState(m.name)
  const [w, setW] = useState(220)
  const wrap = useCallback((el: HTMLDivElement | null) => {
    if (!el) return
    const ro = new ResizeObserver(() => setW(Math.max(60, Math.round(el.getBoundingClientRect().width))))
    ro.observe(el)
  }, [])
  useEffect(() => () => { audio.current?.pause() }, [])
  const toggle = () => {
    if (!audio.current) {
      audio.current = new Audio(`${V1}/mixes/${m.id}/audio`)
      audio.current.onended = () => setPlaying(false)
      audio.current.onpause = () => setPlaying(false)
      audio.current.onplay = () => { document.querySelectorAll('audio').forEach(a => { if (a !== audio.current) a.pause() }); setPlaying(true) }
    }
    if (playing) audio.current.pause(); else void audio.current.play()
  }
  const toFiles = async () => {
    try { const r = await api.post<{ text: string }>(`${V1}/mixes/${m.id}/files`); toast(r.text); onChange?.() } catch (e) { toast(errMsg(e), 'err') }
  }
  return (
    <div className="list-row mix-row">
      <button type="button" className={`play-btn ${playing ? 'on' : ''}`} onClick={toggle} aria-label={playing ? 'Pause' : 'Play'}><Icon name={playing ? 'pause' : 'play'} size={16} /></button>
      <div className="grow min0">
        <div className="row" style={{ gap: 6 }}><b className="ellipsis">{m.name}</b>{m.files_path && <span className="badge" title={m.files_path}>in Files</span>}</div>
        {!compact && <div ref={wrap} className="mix-wave"><Waveform peaks={m.peaks} pxPerSec={w / Math.max(1, m.duration_ms / 1000)} width={w} height={26} color="var(--accent)" /></div>}
        <div className="muted small">{clock(m.duration_ms / 1000, false)} · {m.tracks} {m.tracks === 1 ? 'track' : 'tracks'}{m.song_id ? <> · <Link to={`/songs/${m.song_id}`}>{m.song_title}</Link></> : ''} · {ago(m.created_at)}{!compact && ` · ${fmtBytes(m.size)}`}</div>
      </div>
      {!compact && <MenuButton label="Mix" align="right" width={240} items={() => [
        { label: 'Download', icon: 'download', onSelect: () => { window.location.href = `${V1}/mixes/${m.id}/audio?download=1` } },
        { label: m.files_path ? 'Save to Pocket Files again' : 'Save to Pocket Files', icon: 'archive', onSelect: toFiles, disabled: !settings?.files_via, hint: settings?.files_via ? undefined : 'not connected' },
        ...(m.files_url ? [{ label: 'Open in Pocket Files', icon: 'external', onSelect: () => { window.open(m.files_url, '_blank', 'noopener') } }] : []),
        { label: 'Rename', icon: 'edit', onSelect: () => setRenaming(true) },
        'sep' as const,
        { label: 'Delete', icon: 'trash', danger: true, onSelect: () => setDel(true) },
      ]}><Icon name="more" size={16} /></MenuButton>}
      {renaming && <Modal title="Rename the mix" size="sm" onClose={() => setRenaming(false)} footer={<><button type="button" className="btn" onClick={() => setRenaming(false)}>Cancel</button><button type="button" className="btn primary" disabled={!name.trim()} onClick={async () => { try { await api.put(`${V1}/mixes/${m.id}`, { name }); setRenaming(false); onChange?.() } catch (e) { toast(errMsg(e), 'err') } }}>Save</button></>}>
        <Field label="Name"><input value={name} onChange={e => setName(e.target.value)} autoFocus /></Field>
      </Modal>}
      {del && <Confirm title={`Delete “${m.name}”?`} message="The file is gone for good (a copy in Pocket Files stays)." confirmLabel="Delete" onCancel={() => setDel(false)} onConfirm={async () => { setDel(false); await api.del(`${V1}/mixes/${m.id}`).catch(() => {}); announceChange(); onChange?.() }} />}
    </div>
  )
}
