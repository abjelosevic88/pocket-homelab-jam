-- A band: a folder for songs ("this one is for the trio"). Deleting a band
-- keeps its songs; they just have no band.
CREATE TABLE bands (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name           TEXT NOT NULL,
  color          TEXT NOT NULL DEFAULT '',
  notes          TEXT NOT NULL DEFAULT '',
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE UNIQUE INDEX bands_name ON bands(user_id, name COLLATE NOCASE);
ALTER TABLE songs ADD COLUMN band_id INTEGER REFERENCES bands(id) ON DELETE SET NULL;
CREATE INDEX songs_band ON songs(user_id, band_id);

-- A mark: a note pinned to a moment of a song ("chorus", "too loud here",
-- "try a harmony"), on the whole song (track_id NULL) or on one track; with
-- end_ms > at_ms it covers a stretch.
CREATE TABLE marks (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  song_id        INTEGER NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  track_id       INTEGER REFERENCES tracks(id) ON DELETE CASCADE,
  at_ms          INTEGER NOT NULL DEFAULT 0,
  end_ms         INTEGER NOT NULL DEFAULT 0,
  text           TEXT NOT NULL DEFAULT '',
  color          TEXT NOT NULL DEFAULT '',
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX marks_song ON marks(user_id, song_id, at_ms);
