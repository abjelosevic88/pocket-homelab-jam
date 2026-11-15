-- An idea: a quick recording on its own (a riff, a melody) that is not in a
-- song yet; it can become the first track of a new song. The file lives in
-- DATA_DIR/media/<user id>/ like takes and mixes.
CREATE TABLE ideas (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  client_id      TEXT NOT NULL DEFAULT '',         -- the recording device's own id (uploads again are the same idea)
  name           TEXT NOT NULL DEFAULT '',
  file           TEXT NOT NULL,                    -- relative to the media dir
  mime           TEXT NOT NULL DEFAULT 'audio/wav',
  size           INTEGER NOT NULL DEFAULT 0,
  duration_ms    INTEGER NOT NULL DEFAULT 0,
  sample_rate    INTEGER NOT NULL DEFAULT 0,
  channels       INTEGER NOT NULL DEFAULT 1,
  peaks          TEXT NOT NULL DEFAULT '',         -- base64 bytes, 0..255 loudness, 50 per second
  source         TEXT NOT NULL DEFAULT 'recorded', -- recorded | upload | files | music
  device         TEXT NOT NULL DEFAULT '',         -- which microphone
  app            TEXT NOT NULL DEFAULT '',         -- '' = made here, else the Pocket app that added it
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX ideas_user ON ideas(user_id, created_at);
CREATE UNIQUE INDEX ideas_client ON ideas(user_id, client_id) WHERE client_id <> '';
