# Configuration

Environment variables; every one has a default. Personal settings (what a new song starts with, the click, hearing yourself, stereo, the tuner, connections, where practice time goes) live in the UI per person. The device delay is measured per phone or computer and kept in that browser.

| Variable | Default | What it does |
|---|---|---|
| `BASE_URL` | – | Where the app is (links in events and other apps, the OIDC callback) |
| `HOST` / `PORT` | `0.0.0.0` / `8080` | Where it listens |
| `DATA_DIR` | `./data` | The database, the audio (`media/<user>/`) and the Pocket Core token |
| `DB_PATH` | `$DATA_DIR/pocket-jam.db` | The SQLite file |
| `AUTH_MODE` | `local` | `local` (passwords), `proxy` (a header from your reverse proxy), `none` (one user, for trying it out) |
| `SECURE_COOKIES` | `false` | Set `true` behind HTTPS |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` / `ADMIN_NAME` | – | The first account, made on an empty database |
| `OIDC_ISSUER` / `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` | – | Sign in with an OpenID Connect provider (not needed with Pocket Core) |
| `CORE_URL` / `CORE_REGISTRATION_KEY` / `CORE_APP_URL` | – | Pocket Core |
| `TRUST_PROXY` | `true` | Take the client address from `X-Forwarded-For` |
| `METRICS_ENABLED` | `true` | Prometheus `/metrics` |
| `MAX_BODY_BYTES` | `67108864` | Largest JSON request. Audio uploads stream to disk, up to 1 GB a file |
| `DEMO_DATA` | `false` | Seed six songs whose takes really play (drums, bass, plucked guitar, keys and a sung-like line made by the app), two mixes and two months of time spent |
| `LOG_LEVEL` / `LOG_FORMAT` | `info` / `text` | |

Connections to Pocket Files, Music, Guitar and Time are made per person under Settings → Connected apps (through Pocket Core with nothing to paste, or directly with a URL and a token).

## The microphone needs HTTPS

Browsers only let a page use the microphone on `https://` (or `http://localhost`). Put the app behind a reverse proxy with a certificate before recording from a phone.

## Behind a reverse proxy

Takes and mixes are uploaded in one request: a ten-minute WAV take is about 60 MB, a mix twice that. Allow request bodies of at least 1 GB.
