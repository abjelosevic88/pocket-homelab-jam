<p align="center"><img src="web/public/favicon.svg" width="72" alt=""></p>
<h1 align="center">Pocket Jam</h1>
<p align="center">Record music on your phone or computer, one layer at a time: lay down a guitar, then listen to it on headphones while you record the bass, then the voice. A click and a count-in keep you in time, the device’s delay is taken out so everything lines up, every take is kept so you can pick the best one, and each track has its own volume, balance, mute and solo. Mix it down to one file, or take the tracks to another recording app. A tuner and a metronome are built in.<br>One Go binary, one SQLite file, one volume. Part of the Pocket apps; works on its own too.</p>
<p align="center">
  <a href="https://github.com/abjelosevic88/pocket-homelab-jam/actions"><img src="https://github.com/abjelosevic88/pocket-homelab-jam/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI"></a>
  <a href="https://github.com/abjelosevic88/pocket-homelab-jam/pkgs/container/pocket-homelab-jam"><img src="https://img.shields.io/badge/ghcr.io-amd64%20%7C%20arm64%20%7C%20armv7-blue" alt="image"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="MIT"></a>
</p>

---

<p align="center"><img src="docs/screenshots/desktop-song.png" width="900" alt="A song: four tracks on a timeline with their waveforms, mute, solo and record buttons, and the transport below"></p>

<table>
  <tr>
    <td width="50%"><img src="docs/screenshots/desktop-home.png" alt="Home: today, the week, the songs played last, the tools and the newest mixes"></td>
    <td width="50%"><img src="docs/screenshots/desktop-track.png" alt="A track: volume, balance, every take with its waveform, lining a take up"></td>
  </tr>
  <tr>
    <td align="center"><sub>Home: time played today and this week, the songs played last, the newest mixes.</sub></td>
    <td align="center"><sub>A track: volume and balance, every take kept (pick the one that plays), nudge a take into place.</sub></td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/screenshots/desktop-tuner.png" alt="The tuner with a needle and the guitar's strings"></td>
    <td width="50%"><img src="docs/screenshots/desktop-song-dark.png" alt="The song page in dark mode"></td>
  </tr>
  <tr>
    <td align="center"><sub>The tuner: guitar, bass, ukulele, violin family and more, or any note.</sub></td>
    <td align="center"><sub>Dark mode follows your system.</sub></td>
  </tr>
</table>

<p align="center">
  <img src="docs/screenshots/phone-song.png" width="200" alt="A song on a phone">
  <img src="docs/screenshots/phone-home.png" width="200" alt="Home on a phone">
  <img src="docs/screenshots/phone-tuner.png" width="200" alt="The tuner on a phone">
  <img src="docs/screenshots/phone-metronome.png" width="200" alt="The metronome on a phone">
</p>

## What it does

**Recording, layer by layer**
- A **song** is a set of tracks with a tempo, a time signature and a key. Add a track for each instrument or voice — or bring one in from a file, from **Pocket Files** or from your **Pocket Music** library as a backing track.
- **Record** on the armed track while the others play: a count-in, then the click on every beat (or not), recording from the start of the bar where the playhead is. The browser’s own voice filters are off (no noise suppression, no echo cancelling, no automatic volume), so an instrument sounds like itself. Recordings are kept sample-exact as WAV.
- **Lined up by itself**: sound takes time to reach your headphones and to come back from the microphone. Measure it once per device (eight clicks through the speaker) and every new take is moved by exactly that. A take can still be nudged by 10 or 50 ms.
- **Ideas**: a riff or a melody caught on its own, in the browser or from the phone, before there is a song for it — play them back, name them, and turn one into the first track of a new song when it grows.
- **Every take is kept**: recording again adds take 2, 3…; pick the one that plays, mark the good ones, delete the rest.
- **Mixer**: volume, balance, mute and solo per track, live while it plays. Hear yourself in the headphones if you like.
- **Mix down** what you hear to one stereo WAV — download it or keep it in Pocket Files — or download all the tracks as a **.zip** with `song.json` (where each one starts) for Pocket Studio or any other recording app.
- Works in a phone’s browser (add it to the home screen); the screen stays on while a song is open.

**Tuner and metronome**
- A tuner that listens to the microphone: the note, a needle in cents, the strings of the instrument picked (guitar in several tunings, bass, ukulele, violin, viola, cello, mandolin, banjo) and the reference A.
- A metronome with tap tempo, 2–7 beats in a bar, an accent on the first, subdivisions and three sounds.

**Time spent** — counted while a song plays or records: per day, per sitting, a streak. It can go on to Pocket Guitar or Pocket Time as practice.

## With the other Pocket apps

Through Pocket Core (one sign-in, nothing to paste) or directly with a URL and a token, per person:

| App | What for |
|---|---|
| Pocket Files | Bring recordings and backing tracks in; keep mixes there |
| Pocket Music | A song from your library as a backing track |
| Pocket Guitar | Your time here logged as guitar practice |
| Pocket Time | Your time here as tracked time |

Jam offers `jam:read` and `jam:write` to the rest of the box (Pocket Day, Search, AI), and publishes `jam.song_added`, `take_recorded`, `mix_made`.

## Run it

```sh
docker run -d --name pocket-jam -p 8080:8080 -v ./data:/data \
  -e BASE_URL=https://jam.example.com -e ADMIN_EMAIL=you@example.com -e ADMIN_PASSWORD=change-me \
  ghcr.io/abjelosevic88/pocket-homelab-jam:latest
```

Or [docker-compose.yml](docker-compose.yml), or a release binary (`pocket-jam`). `make run` starts it with demo data and no login.

Browsers only allow the microphone over **HTTPS** (or on localhost): put it behind a reverse proxy with a certificate before recording from a phone. Wired headphones are best while recording over other tracks; Bluetooth adds a delay you hear.

- [Configuration](docs/configuration.md) · [API](docs/api.md) · [Backup](docs/backup.md) · [Reverse proxy](docs/reverse-proxy.md)

## Develop

`make dev` (API on :8080, Vite on :5173), `make test` (go vet, go test, tsc). Go 1.26, React 19, TypeScript, Vite; SQLite through modernc (no CGO). The sound is Web Audio: the recorder is an AudioWorklet (`web/public/recorder-worklet.js`) that stamps every sample with the audio clock, so a take lands where it was played; mixing down uses an OfflineAudioContext.

MIT © 2026 Aleksandar Bjelosevic
