CREATE TABLE users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'member',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Box-wide settings (title, setup flag); everything else is personal.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

CREATE TABLE cache (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  fetched_at TEXT NOT NULL
);



-- ---------- Pocket Jam ----------
-- Everything belongs to the person who made it (user_id); nobody else on the
-- box sees it, box admins included. Times are UTC ISO strings, days are the
-- person's local YYYY-MM-DD. Audio files (takes, mixes) live in
-- DATA_DIR/media/<user id>/.

-- A song (or an idea, an exercise): its tempo and metre, and its tracks.
CREATE TABLE songs (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title          TEXT NOT NULL,
  artist         TEXT NOT NULL DEFAULT '',
  notes          TEXT NOT NULL DEFAULT '',
  tempo          REAL NOT NULL DEFAULT 100,        -- beats per minute
  beats          INTEGER NOT NULL DEFAULT 4,       -- beats in a bar
  beat_unit      INTEGER NOT NULL DEFAULT 4,       -- 4 = quarter notes, 8 = eighths
  key_name       TEXT NOT NULL DEFAULT '',         -- "A minor"
  count_in       INTEGER NOT NULL DEFAULT 1,       -- bars of clicks before recording
  click          INTEGER NOT NULL DEFAULT 1,       -- the click plays while recording
  loop_start_ms  INTEGER NOT NULL DEFAULT 0,
  loop_end_ms    INTEGER NOT NULL DEFAULT 0,       -- 0 = no loop
  starred        INTEGER NOT NULL DEFAULT 0,
  color          TEXT NOT NULL DEFAULT '',
  source         TEXT NOT NULL DEFAULT '',         -- '' = made here, else the Pocket app that added it
  opened_at      TEXT,
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX songs_user ON songs(user_id, updated_at);

-- A track: one instrument or voice, with its mixer settings and the take
-- that plays.
CREATE TABLE tracks (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  song_id        INTEGER NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  name           TEXT NOT NULL DEFAULT '',
  instrument     TEXT NOT NULL DEFAULT 'other',    -- voice | guitar | bass | keys | drums | strings | wind | backing | other
  color          TEXT NOT NULL DEFAULT '',
  volume         INTEGER NOT NULL DEFAULT 100,     -- 0..150 percent
  pan            INTEGER NOT NULL DEFAULT 0,       -- -100 (left) .. 100 (right)
  muted          INTEGER NOT NULL DEFAULT 0,
  solo           INTEGER NOT NULL DEFAULT 0,
  position       INTEGER NOT NULL DEFAULT 0,
  take_id        INTEGER,                          -- the take that plays (NULL = none yet)
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX tracks_song ON tracks(user_id, song_id, position);

-- A take: one recording (or imported file) on a track. Recording again keeps
-- the earlier takes so the best one can be chosen.
CREATE TABLE takes (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  song_id        INTEGER NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  track_id       INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
  number         INTEGER NOT NULL DEFAULT 1,
  file           TEXT NOT NULL,                    -- relative to the media dir
  mime           TEXT NOT NULL DEFAULT 'audio/wav',
  size           INTEGER NOT NULL DEFAULT 0,
  duration_ms    INTEGER NOT NULL DEFAULT 0,
  offset_ms      INTEGER NOT NULL DEFAULT 0,       -- where it starts in the song
  latency_ms     INTEGER NOT NULL DEFAULT 0,       -- the device delay taken out when it was recorded
  sample_rate    INTEGER NOT NULL DEFAULT 0,
  channels       INTEGER NOT NULL DEFAULT 1,
  peaks          TEXT NOT NULL DEFAULT '',         -- base64 bytes, 0..255 loudness, 50 per second
  source         TEXT NOT NULL DEFAULT 'recorded', -- recorded | upload | files | music
  source_ref     TEXT NOT NULL DEFAULT '',
  device         TEXT NOT NULL DEFAULT '',         -- which microphone
  note           TEXT NOT NULL DEFAULT '',
  liked          INTEGER NOT NULL DEFAULT 0,
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX takes_track ON takes(user_id, track_id, number);
CREATE INDEX takes_song ON takes(user_id, song_id);

-- A mix: the tracks mixed down to one stereo file.
CREATE TABLE mixes (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  song_id        INTEGER REFERENCES songs(id) ON DELETE SET NULL,
  name           TEXT NOT NULL DEFAULT '',
  file           TEXT NOT NULL,
  mime           TEXT NOT NULL DEFAULT 'audio/wav',
  size           INTEGER NOT NULL DEFAULT 0,
  duration_ms    INTEGER NOT NULL DEFAULT 0,
  peaks          TEXT NOT NULL DEFAULT '',
  tracks         INTEGER NOT NULL DEFAULT 0,       -- how many tracks went in
  files_path     TEXT NOT NULL DEFAULT '',         -- where it was saved in Pocket Files
  files_url      TEXT NOT NULL DEFAULT '',
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX mixes_user ON mixes(user_id, created_at);

-- A sitting: time spent playing and recording one song in one go. Sent on
-- to Pocket Guitar or Pocket Time as practice when the person wants that.
CREATE TABLE sittings (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  song_id        INTEGER REFERENCES songs(id) ON DELETE SET NULL,
  title          TEXT NOT NULL DEFAULT '',
  day            TEXT NOT NULL,
  started_at     TEXT NOT NULL,
  last_at        TEXT NOT NULL,
  seconds        INTEGER NOT NULL DEFAULT 0,
  takes          INTEGER NOT NULL DEFAULT 0,
  sent_to        TEXT NOT NULL DEFAULT '',         -- guitar | time
  sent_ref       TEXT NOT NULL DEFAULT '',         -- the id over there
  sent_seconds   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX sittings_user ON sittings(user_id, day);
