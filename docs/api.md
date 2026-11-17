# API

Everything is under `/api/v1`, JSON unless said otherwise. Sign in with the session cookie, an API token (`Authorization: Bearer pjam_…`, Settings → API & shortcuts), or a Pocket Core link token. Everything is per person: other people's rows answer 404.

## Songs, tracks, takes

| Call | What |
|---|---|
| `GET /songs?q=&starred=1&sort=recent\|added\|title\|time` | `{songs, total}` with track/take counts, length and minutes played |
| `POST /songs` | `{title, artist, notes, tempo, beats, beat_unit, key_name, count_in, click, color, starred}` (only `title` needed; the rest from your settings) |
| `GET /songs/ID` | The song with `tracks[]`, each with `takes[]` and `take_id` (the one that plays). `?open=1` marks it opened |
| `PUT /songs/ID` · `DELETE /songs/ID` | Any of the fields above; delete removes its tracks and takes (mixes stay) |
| `PUT /songs/ID/order` | `{ids: [track ids]}` |
| `POST /songs/ID/tracks` | `{name, instrument: voice\|guitar\|bass\|keys\|drums\|strings\|wind\|backing\|other, color, volume, pan}` |
| `PUT /tracks/ID` · `DELETE /tracks/ID` | `{name, instrument, color, volume 0..150, pan -100..100, muted, solo, position, take_id}` |
| `POST /tracks/ID/takes` | Multipart: `meta` (JSON `{duration_ms, offset_ms, latency_ms, sample_rate, channels, peaks, source, source_ref, device, note, part, part_id}`) + `file` (WAV, MP3, M4A, AAC, FLAC, OGG, WebM). A track holds several **parts**, all heard (the higher `layer` where two overlap); a part is a group of takes of which one `plays`. `part`: `auto` (default for recordings: over most of a part, another take of it; else a part of its own), `new` (default for files), `only` (a part of its own; every other take on the track becomes an older take of it), `join` (another take of part `part_id`). The new take plays, on top |
| `GET /takes/ID/audio` | The audio, with ranges (`?download=1` to save) |
| `PUT /takes/ID` · `DELETE /takes/ID` | `{offset_ms, note, liked, plays}` — `offset_ms` lines up this take alone; `plays: true` picks it for its part; `track_id` moves its whole part (older clients). Deleting the take that plays: the newest other take of the part plays; the last one: the part is gone |
| `PUT /parts/PART` | Move a part (every take of it): `{shift_ms, track_id, layer}` — later/earlier, onto another track of the song, at a layer (default: on top). Returns `{takes}`. `tracks.take_id` stays the playing take of a track's top part, for older clients |
| `GET /songs/ID/history` | The song's history, newest first: `{events: [{id, at, kind, text, via}], more}`; `?before=ID` pages back, `?limit=` up to 500. Every change to the song writes a line; a run of the same change (a fader moved again) stays one line. Send `X-Jam-Group: <id>` + `X-Jam-Group-Label: <url-encoded words>` to make a job of many calls one line. Linked apps: `jam:read` |
| `POST /songs/ID/history/EVENT/undo` | Undo one line of the history (`can_undo`): fields back, things made deleted again, takes back where they were, deleted takes/tracks/notes/mixes back as they were (their files stay in the trash for 30 days). The undo is a line of its own (`kind: undo`, `ref`); undoing that does it again (`kind: redo`, which can't be undone; the first line can be undone again). Returns `{ok, event}` (`ok: false, error` when only part of it could be undone). Only the person, not linked apps |
| `GET /songs/ID/stems.zip` | Each track's playing take plus `song.json` (tempo, key, where each file starts, volume, balance) |

`peaks` is the waveform: base64 bytes, the loudest sample in each 1/50 s scaled 0..255. `offset_ms` is where the take starts in the song (negative: the take starts before the song).

## Mixes

| Call | What |
|---|---|
| `GET /mixes?song_id=` | Newest first |
| `POST /songs/ID/mixes` | Multipart like a take; `meta.name`, `meta.tracks` |
| `GET /mixes/ID/audio` | With ranges, `?download=1` |
| `PUT /mixes/ID` · `DELETE /mixes/ID` | `{name}` |
| `POST /mixes/ID/files` | Keep it in Pocket Files (the folder from settings) |

## Ideas

A quick recording on its own (a riff, a melody) before there is a song for it. An idea is `{id, client_id, name, mime, size, duration_ms, sample_rate, channels, peaks, source, device, band_id, band_name, created_at}`. Like a song it can be in one of your bands: `band_id` is the band's id or `null`, `band_name` its name or `""`. Deleting a band leaves its ideas in no band. A band (`GET /bands`, `GET /bands/ID`) counts its `songs` and its `ideas`.

| Call | What |
|---|---|
| `GET /ideas?band=` | `{ideas}`, newest first. `band=ID`: that band's; `band=none` (or `0`): the ones in no band; no `band`: all |
| `POST /ideas` | Multipart like a take: `meta` (JSON `{name, duration_ms, sample_rate, channels, peaks, source, device, client_id, created_at, band_id}`) + `file`. 201 with the idea. `band_id` (optional; `0` or `null` = none) must be one of your bands, else 400. With a `client_id` this person already sent, 200 with that idea and the file is not kept (safe to send again). No `name`: "Idea" and the day; `created_at` (RFC 3339) is when it was recorded, else now |
| `GET /ideas/ID` | The idea |
| `GET /ideas/ID/audio` | The audio, with ranges (`?download=1` to save) |
| `PUT /ideas/ID` | `{name, band_id}`: only what is sent changes; `"band_id": null` (or `0`) takes it out of its band; a band that is not yours is 400, an empty name too. 200 with the idea |
| `DELETE /ideas/ID` | 204; the file goes too |
| `POST /ideas/ID/song` | `{title, tempo, beats, beat_unit, key_name, count_in, click, instrument, band_id}` (all optional: the idea's name, your settings, `other`, the idea's band; `"band_id": null` = no band, a band that is not yours is 400). 201 with the new song like `GET /songs/ID`: one track with one take, the idea's recording from the start. The idea is gone after; nothing changes when it fails |

## Time spent

| Call | What |
|---|---|
| `POST /time` | `{song_id, seconds ≤ 180}` — the song page sends a minute at a time while playing or recording; it goes on to Pocket Guitar or Pocket Time when chosen |
| `GET /time?from=&to=` | `{days: [{day, minutes, takes}], sittings, minutes, streak}` (default the last 28 days) |

## For the rest of the box

`GET /home`, `GET /summary` (Homepage, Pocket AI), `GET /day/YYYY-MM-DD` (Pocket Day), `GET /search?q=` (Pocket Search: songs, mixes and ideas).

## Other

`GET/PUT /settings`, `GET /apps` (how each connected app is reached), `GET /apps/files?space=&path=`, `GET /apps/files/raw?space=&path=`, `GET /apps/music?q=`, `GET /apps/music/stream/ID`, `GET /export.json` (`?audio=1`: a .zip with the audio), `GET /export.csv` (sittings), `POST /import` (.json or .zip), `GET /backup` (admin), users and tokens.

Linked apps (Pocket Core) get `jam:read` (the GETs above, the audio too) and `jam:write` (`POST /songs`, `PUT/DELETE /songs/ID` for songs that app added; `POST /ideas`, and `PUT/DELETE /ideas/ID`, `POST /ideas/ID/song` for ideas that app added).
