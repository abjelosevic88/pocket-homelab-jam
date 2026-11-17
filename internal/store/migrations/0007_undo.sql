-- Undo for every line of a song's history: undo = what puts the change back
-- (JSON, read by the server only), undone_by = the line that undid it, ref =
-- for an undo line, the line it undid. trash = media files of deleted takes and
-- mixes, kept a while so an undo can bring them back.
ALTER TABLE song_events ADD COLUMN undo TEXT NOT NULL DEFAULT '';
ALTER TABLE song_events ADD COLUMN undone_by INTEGER NOT NULL DEFAULT 0;
ALTER TABLE song_events ADD COLUMN ref INTEGER NOT NULL DEFAULT 0;
CREATE TABLE IF NOT EXISTS trash (
  file       TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL,
  at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
