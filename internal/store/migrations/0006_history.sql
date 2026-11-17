-- A song's history: one line per change (a recording, a move, a fader, a note…),
-- newest last. key + old let a run of the same change (a fader moved again and
-- again) stay one line that says where it started.
CREATE TABLE IF NOT EXISTS song_events (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  song_id    INTEGER NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  kind       TEXT NOT NULL DEFAULT '',
  text       TEXT NOT NULL DEFAULT '',
  key        TEXT NOT NULL DEFAULT '',
  old        TEXT NOT NULL DEFAULT '',
  via        TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS song_events_song ON song_events(song_id, id);
