// The songs: a list to search, a new song, and the form for a song's details.
import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Band, Song } from '../lib/types'
import { useOnChange, announceChange } from '../lib/data'
import { useCrumb } from '../components/Layout'
import { Confirm, Empty, Field, Loading, MenuButton, Modal, Segmented, useDebounce, useToast, errMsg } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { clock } from '../lib/engine'
import { ago } from '../lib/format'

export default function Songs() {
  const [params, setParams] = useSearchParams()
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 250)
  const [sort, setSort] = useState<'recent' | 'title' | 'added' | 'time'>('recent')
  const starred = params.get('filter') === 'starred'
  const bandParam = params.get('band') || '' // a band's id, 'none', or '' for every song
  const [songs, setSongs] = useState<Song[] | null>(null)
  const [adding, setAdding] = useState(params.get('add') === '1')
  const bands = useBands()
  const band = bands?.find(b => String(b.id) === bandParam) || null
  const [editBand, setEditBand] = useState<Band | 'new' | null>(null)
  const [delBand, setDelBand] = useState<Band | null>(null)
  const { toast } = useToast()
  const navigate = useNavigate()
  useCrumb(band ? band.name : bandParam === 'none' ? 'No band' : undefined)
  const setBand = (v: string) => { if (v) params.set('band', v); else params.delete('band'); setParams(params, { replace: true }) }
  useEffect(() => { if (params.get('add') === '1') { setAdding(true); params.delete('add'); setParams(params, { replace: true }) } }, [params, setParams])
  const load = useCallback(() => {
    api.get<{ songs: Song[] }>(`${V1}/songs?q=${encodeURIComponent(dq)}&sort=${sort}${starred ? '&starred=1' : ''}${bandParam ? `&band=${encodeURIComponent(bandParam)}` : ''}`).then(r => setSongs(r.songs)).catch(() => setSongs([]))
  }, [dq, sort, starred, bandParam])
  useEffect(() => { load() }, [load])
  useOnChange(load)
  return (
    <div className="page">
      {band && <div className="band-head" style={band.color ? { ['--band' as string]: band.color } : undefined}>
        <span className="band-dot"><Icon name="users" size={18} /></span>
        <div className="grow min0">
          <h2 className="ellipsis">{band.name}</h2>
          <div className="muted small">{band.songs} {band.songs === 1 ? 'song' : 'songs'}{band.ideas > 0 && <> · <Link to={`/ideas?band=${band.id}`}>{band.ideas} {band.ideas === 1 ? 'idea' : 'ideas'}</Link></>}{band.notes && <> · <span className="ellipsis">{band.notes}</span></>}</div>
        </div>
        <MenuButton label="Band" align="right" items={[
          { label: 'Rename, colour, notes', icon: 'edit', onSelect: () => setEditBand(band) },
          'sep',
          { label: 'Delete band', icon: 'trash', danger: true, onSelect: () => setDelBand(band) },
        ]}><Icon name="more" size={18} /></MenuButton>
      </div>}
      <div className="toolbar">
        <div className="search-box grow"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Find a song" aria-label="Find a song" /></div>
        <Segmented size="sm" value={starred ? 'starred' : 'all'} onChange={v => { if (v === 'starred') params.set('filter', 'starred'); else params.delete('filter'); setParams(params, { replace: true }) }} options={[{ value: 'all', label: 'All' }, { value: 'starred', label: 'Starred' }]} />
        {!!bands?.length && <Select ariaLabel="Band" value={bandParam} onChange={setBand} placeholder="Every band" clearLabel="Every band"
          options={[...bands.map(b => ({ value: String(b.id), label: b.name, hint: String(b.songs) })), { value: 'none', label: 'In no band' }]} />}
        <Select ariaLabel="Order" value={sort} onChange={v => setSort(v as typeof sort)} options={[{ value: 'recent', label: 'Played last' }, { value: 'added', label: 'Newest' }, { value: 'title', label: 'A to Z' }, { value: 'time', label: 'Most time' }]} />
        <button type="button" className="btn primary hide-mobile" onClick={() => setAdding(true)}><Icon name="plus" size={15} />New song</button>
      </div>
      {!songs ? <Loading /> : !songs.length ? (
        band ? <Empty title={`Nothing for ${band.name} yet`} icon="users">Make a song here and it goes in this band; or open a song, then Song details → Band.<div className="row mt" style={{ justifyContent: 'center' }}><button type="button" className="btn primary" onClick={() => setAdding(true)}><Icon name="plus" size={15} />New song</button></div></Empty> :
        dq || starred || bandParam ? <Empty title="Nothing found" icon="search" /> :
        <Empty title="Your first song" icon="mic">A song is a set of tracks you record one on top of the other — a riff, an idea, a cover, an exercise.<div className="row mt" style={{ justifyContent: 'center' }}><button type="button" className="btn primary" onClick={() => setAdding(true)}><Icon name="plus" size={15} />New song</button></div></Empty>
      ) : <div className="song-grid">{songs.map(s => <SongCard key={s.id} s={s} />)}</div>}
      {adding && <NewSong onClose={() => setAdding(false)} band={band} />}
      {editBand && <BandForm band={editBand === 'new' ? undefined : editBand} onClose={() => setEditBand(null)} onSaved={() => { setEditBand(null); announceChange() }} />}
      {delBand && <Confirm title={`Delete “${delBand.name}”?`} message="Its songs and ideas stay, in no band." confirmLabel="Delete" onCancel={() => setDelBand(null)}
        onConfirm={async () => { try { await api.del(`${V1}/bands/${delBand.id}`); setDelBand(null); announceChange(); navigate('/songs') } catch (e) { toast(errMsg(e), 'err') } }} />}
    </div>
  )
}

export function SongCard({ s }: { s: Song }) {
  return (
    <Link to={`/songs/${s.id}`} className="song-card" style={s.color ? { ['--song' as string]: s.color } : undefined}>
      <div className="song-card-top">
        <span className="song-dot"><Icon name="waveform" size={16} /></span>
        <span className="grow min0">
          <b className="ellipsis block-line">{s.title}</b>
          <span className="muted small ellipsis block-line">{s.band_name && <span className="band-tag">{s.band_name}</span>}{s.artist || (s.key_name ? `${s.key_name}` : `${Math.round(s.tempo)} bpm`)}</span>
        </span>
        {s.starred && <Icon name="star" size={15} className="star-on" />}
      </div>
      <div className="song-card-meta muted small">
        <span>{s.track_count} {s.track_count === 1 ? 'track' : 'tracks'}</span>
        {s.length_ms > 0 && <span>{clock(s.length_ms / 1000, false)}</span>}
        <span>{Math.round(s.tempo)} bpm · {s.beats}/{s.beat_unit}</span>
        {s.minutes > 0 && <span>{s.minutes} min played</span>}
        <span className="grow" />
        <span>{ago(s.opened_at || s.updated_at)}</span>
      </div>
    </Link>
  )
}

function NewSong({ onClose, band }: { onClose: () => void; band: Band | null }) {
  const navigate = useNavigate()
  return (
    <Modal title={band ? `New song for ${band.name}` : 'New song'} onClose={onClose}>
      <SongForm band={band} onSaved={s => { onClose(); announceChange(); navigate(`/songs/${s.id}`) }} />
    </Modal>
  )
}

/** Tap the beat a few times: the tempo it makes (20..400). */
export function useTapTempo(set: (bpm: number) => void) {
  const taps = useRef<number[]>([])
  return () => {
    const now = performance.now()
    taps.current = [...taps.current.filter(t => now - t < 2500), now].slice(-8)
    if (taps.current.length >= 3) {
      const gaps = taps.current.slice(1).map((t, i) => t - taps.current[i])
      const avg = gaps.reduce((a, b) => a + b, 0) / gaps.length
      set(Math.max(20, Math.min(400, Math.round(60000 / avg))))
    }
  }
}

export const METRES = ['2/4', '3/4', '4/4', '5/4', '6/8', '7/8', '9/8', '12/8']

const KEYS = ['C', 'C♯', 'D', 'E♭', 'E', 'F', 'F♯', 'G', 'A♭', 'A', 'B♭', 'B'].flatMap(k => [k, `${k} minor`])

/** The details of a song (new, or changing one). */
export function SongForm({ song, band, onSaved }: { song?: Song; band?: Band | null; onSaved: (s: Song) => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const bands = useBands()
  const [f, setF] = useState({
    band_id: song ? song.band_id || 0 : band?.id || 0,
    title: song?.title || '', artist: song?.artist || '', key_name: song?.key_name || '', tempo: song?.tempo || settings?.tempo || 100,
    beats: song?.beats || settings?.beats || 4, beat_unit: song?.beat_unit || 4, count_in: song?.count_in ?? settings?.count_in ?? 0, click: song?.click ?? settings?.click ?? true,
  })
  const [busy, setBusy] = useState(false)
  const tap = useTapTempo(bpm => setF(x => ({ ...x, tempo: bpm })))
  const save = async () => {
    setBusy(true)
    try { onSaved(song ? await api.put<Song>(`${V1}/songs/${song.id}`, f) : await api.post<Song>(`${V1}/songs`, f)) } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  return (
    <form className="stack" onSubmit={e => { e.preventDefault(); if (f.title.trim()) void save() }}>
      <div className="two-col tight">
        <Field label="Name"><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} placeholder="Sunday riff" autoFocus /></Field>
        <Field label="Artist or note (optional)"><input value={f.artist} onChange={e => setF({ ...f, artist: e.target.value })} placeholder="A cover? Who with?" /></Field>
      </div>
      <Field label="Band (optional)" help="A folder for the songs you make for a band, a project or a gig. Type a new name to make one.">
        <Select ariaLabel="Band" value={f.band_id ? String(f.band_id) : ''} onChange={v => setF(x => ({ ...x, band_id: Number(v) || 0 }))} placeholder="No band" clearLabel="No band"
          options={(bands || []).map(b => ({ value: String(b.id), label: b.name }))} createLabel="New band"
          onCreate={async name => { const b = await api.post<Band>(`${V1}/bands`, { name }); announceChange(); return String(b.id) }} />
      </Field>
      <div className="two-col tight">
        <Field label="Tempo (beats a minute)">
          <div className="row" style={{ gap: 6 }}>
            <input type="number" min={20} max={400} step={1} value={f.tempo} onChange={e => setF({ ...f, tempo: Number(e.target.value) })} style={{ width: 90 }} />
            <button type="button" className="btn" onClick={tap} title="Tap the beat a few times">Tap</button>
          </div>
        </Field>
        <Field label="Key (optional)"><Select ariaLabel="Key" value={f.key_name} onChange={v => setF({ ...f, key_name: v })} placeholder="None" clearLabel="None" options={KEYS.map(k => ({ value: k, label: k }))} /></Field>
      </div>
      <div className="two-col tight">
        <Field label="Beats in a bar">
          <Select ariaLabel="Time signature" value={`${f.beats}/${f.beat_unit}`} onChange={v => { const [b, u] = v.split('/').map(Number); setF({ ...f, beats: b, beat_unit: u }) }}
            options={METRES.map(v => ({ value: v, label: v }))} />
        </Field>
        <Field label="Count-in before recording"><Segmented size="sm" value={f.count_in} onChange={v => setF({ ...f, count_in: v })} options={[{ value: 0, label: 'None' }, { value: 1, label: '1 bar' }, { value: 2, label: '2 bars' }]} /></Field>
      </div>
      <label className="check-row"><input type="checkbox" checked={f.click} onChange={e => setF({ ...f, click: e.target.checked })} />The click plays while recording</label>
      <div className="row"><button type="submit" className="btn primary" disabled={busy || !f.title.trim()}>{song ? 'Save' : 'Make the song'}</button></div>
    </form>
  )
}

/** The person's bands (folders for songs), reloaded when something changes. */
export function useBands() {
  const [bands, setBands] = useState<Band[] | null>(null)
  const load = useCallback(() => { api.get<{ bands: Band[] }>(`${V1}/bands`).then(r => setBands(r.bands)).catch(() => setBands(b => b || [])) }, [])
  useEffect(() => { load() }, [load])
  useOnChange(load)
  return bands
}

const BAND_COLORS = ['#e8590c', '#d6336c', '#7048e8', '#1c7ed6', '#0ca678', '#74b816', '#f59f00', '#868e96']

/** Make a band, or rename one (with its colour and a few notes). */
export function BandForm({ band, onClose, onSaved }: { band?: Band; onClose: () => void; onSaved: (b: Band) => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: band?.name || '', color: band?.color || '', notes: band?.notes || '' })
  const [busy, setBusy] = useState(false)
  const save = async () => {
    setBusy(true)
    try { onSaved(band ? await api.put<Band>(`${V1}/bands/${band.id}`, f) : await api.post<Band>(`${V1}/bands`, f)) } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={band ? 'Band' : 'New band'} onClose={onClose}>
      <form className="stack" onSubmit={e => { e.preventDefault(); if (f.name.trim()) void save() }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} placeholder="The Sunday trio" autoFocus /></Field>
        <Field label="Colour">
          <div className="swatches">
            <button type="button" className={`swatch none ${!f.color ? 'on' : ''}`} onClick={() => setF({ ...f, color: '' })} aria-label="No colour" title="No colour" />
            {BAND_COLORS.map(c => <button key={c} type="button" className={`swatch ${f.color === c ? 'on' : ''}`} style={{ background: c }} onClick={() => setF({ ...f, color: c })} aria-label={c} />)}
          </div>
        </Field>
        <Field label="Notes (optional)"><textarea rows={3} value={f.notes} onChange={e => setF({ ...f, notes: e.target.value })} placeholder="Who plays what, rehearsal day…" /></Field>
        <div className="row"><button type="submit" className="btn primary" disabled={busy || !f.name.trim()}>{band ? 'Save' : 'Make the band'}</button></div>
      </form>
    </Modal>
  )
}
