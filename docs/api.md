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
| `POST /tracks/ID/takes` | Multipart: `meta` (JSON `{duration_ms, offset_ms, latency_ms, sample_rate, channels, peaks, source, source_ref, device, note}`) + `file` (WAV, MP3, M4A, AAC, FLAC, OGG, WebM). It becomes the take that plays |
| `GET /takes/ID/audio` | The audio, with ranges (`?download=1` to save) |
| `PUT /takes/ID` · `DELETE /takes/ID` | `{offset_ms, note, liked}` |
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

A quick recording on its own (a riff, a melody) before there is a song for it. An idea is `{id, client_id, name, mime, size, duration_ms, sample_rate, channels, peaks, source, device, created_at}`.

| Call | What |
|---|---|
| `GET /ideas` | `{ideas}`, newest first |
| `POST /ideas` | Multipart like a take: `meta` (JSON `{name, duration_ms, sample_rate, channels, peaks, source, device, client_id, created_at}`) + `file`. 201 with the idea. With a `client_id` this person already sent, 200 with that idea and the file is not kept (safe to send again). No `name`: "Idea" and the day; `created_at` (RFC 3339) is when it was recorded, else now |
| `GET /ideas/ID` | The idea |
| `GET /ideas/ID/audio` | The audio, with ranges (`?download=1` to save) |
| `PUT /ideas/ID` | `{name}` |
| `DELETE /ideas/ID` | 204; the file goes too |
| `POST /ideas/ID/song` | `{title, tempo, beats, beat_unit, key_name, count_in, click, instrument}` (all optional: the idea's name, your settings, `other`). 201 with the new song like `GET /songs/ID`: one track with one take, the idea's recording from the start. The idea is gone after; nothing changes when it fails |

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
