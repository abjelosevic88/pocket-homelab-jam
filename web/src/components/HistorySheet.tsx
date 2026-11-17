// A song's history: every change to it, newest first, by day.
import { useEffect, useState } from 'react'
import { api, V1 } from '../lib/api'
import { Empty, Loading, Modal, Spinner, errMsg } from './ui'
import { Icon } from './icons'
import { fmtDate, fmtISOTime, todayISO, addDays } from '../lib/format'

export interface SongEvent { id: number; song_id: number; at: string; kind: string; text: string; via: string; can_undo: boolean; undone_by: number; ref: number }

const ICON: Record<string, string> = { song: 'edit', track: 'layers', take: 'mic', part: 'move', mixer: 'sliders', note: 'pin', mix: 'mixer', group: 'scissors', undo: 'undo', redo: 'repeat' }

/** The local day of a time, YYYY-MM-DD. */
function dayOf(iso: string): string {
  const t = new Date(iso)
  return `${t.getFullYear()}-${String(t.getMonth() + 1).padStart(2, '0')}-${String(t.getDate()).padStart(2, '0')}`
}
function dayLabel(d: string): string {
  const today = todayISO()
  if (d === today) return 'Today'
  if (d === addDays(today, -1)) return 'Yesterday'
  return fmtDate(d, d.slice(0, 4) !== today.slice(0, 4))
}

/** onChanged: an undo changed the song (reload it). */
export function HistorySheet({ songId, title, onClose, onChanged }: { songId: number; title: string; onClose: () => void; onChanged: () => Promise<void> | void }) {
  const [busy, setBusy] = useState(0)
  const [events, setEvents] = useState<SongEvent[] | null>(null)
  const [more, setMore] = useState(false)
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState('')
  const page = async (before = 0) => {
    setLoading(true)
    try {
      const r = await api.get<{ events: SongEvent[]; more: boolean }>(`${V1}/songs/${songId}/history${before ? `?before=${before}` : ''}`)
      setEvents(e => before && e ? [...e, ...r.events] : r.events)
      setMore(r.more)
      setErr('')
    } catch (e) { setErr(errMsg(e)) } finally { setLoading(false) }
  }
  useEffect(() => { void page() }, [songId]) // eslint-disable-line react-hooks/exhaustive-deps
  const undo = async (e: SongEvent) => {
    if (busy) return
    setBusy(e.id)
    try {
      const r = await api.post<{ ok: boolean; error?: string }>(`${V1}/songs/${songId}/history/${e.id}/undo`)
      if (!r.ok && r.error) setErr(r.error)
      await onChanged()
      await page()
      if (r.ok) setErr('')
    } catch (x) { setErr(errMsg(x)) } finally { setBusy(0) }
  }

  const days: { day: string; items: SongEvent[] }[] = []
  for (const e of events || []) {
    const d = dayOf(e.at)
    if (days.at(-1)?.day !== d) days.push({ day: d, items: [] })
    days.at(-1)!.items.push(e)
  }
  return (
    <Modal title={<span className="row" style={{ gap: 8 }}><Icon name="history" size={16} />History of “{title}”</span>} onClose={onClose}>
      {err && <p className="error small">{err}</p>}
      {!events ? (err ? null : <Loading />) : !events.length ? (
        <Empty title="Nothing here yet" icon="history">Changes you make to this song from now on show up here: recordings, moves, the mixer, notes and the song's details.</Empty>
      ) : (
        <div className="hist">
          {days.map(d => (
            <section key={d.day}>
              <h4 className="hist-day muted small">{dayLabel(d.day)}</h4>
              <ul>
                {d.items.map(e => (
                  <li key={e.id} className={`hist-row k-${e.kind} ${e.undone_by ? 'undone' : ''}`}>
                    <span className="hist-ico"><Icon name={ICON[e.kind] || 'dot'} size={13} /></span>
                    <span className="hist-text">{e.text}{e.via && <span className="muted"> · by {e.via}</span>}
                      {e.undone_by !== 0 && <span className="hist-tag">{e.kind === 'undo' ? 'redone' : 'undone'}</span>}</span>
                    <span className="hist-side">
                      <time className="muted small" dateTime={e.at} title={new Date(e.at).toLocaleString()}>{fmtISOTime(e.at)}</time>
                      {e.can_undo && <button type="button" className="btn ghost sm hist-undo" disabled={!!busy} onClick={() => undo(e)}
                        title={e.kind === 'undo' ? 'Do it again' : 'Put this change back; you can redo it after'}>
                        {busy === e.id ? <Spinner size={12} /> : <Icon name={e.kind === 'undo' ? 'repeat' : 'undo'} size={13} />}{e.kind === 'undo' ? 'Redo' : 'Undo'}</button>}
                    </span>
                  </li>
                ))}
              </ul>
            </section>
          ))}
          {more && <button type="button" className="btn ghost sm hist-more" disabled={loading} onClick={() => page(events.at(-1)!.id)}>{loading ? <Spinner size={12} /> : null}Show older changes</button>}
        </div>
      )}
    </Modal>
  )
}
