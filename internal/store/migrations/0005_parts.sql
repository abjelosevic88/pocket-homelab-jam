-- Several parts on a track. A part is a group of takes (attempts at the same
-- bit) of which one plays; every part on a track plays, and where two overlap
-- the one with the higher layer is heard. part = the id of the part's first
-- take (a key, it may outlive that take). tracks.take_id stays as the playing
-- take of the top part, for older clients.
ALTER TABLE takes ADD COLUMN part INTEGER NOT NULL DEFAULT 0;
ALTER TABLE takes ADD COLUMN plays INTEGER NOT NULL DEFAULT 0;
ALTER TABLE takes ADD COLUMN layer INTEGER NOT NULL DEFAULT 1;
-- until now a track played one take and the others were attempts at it: one part per track
UPDATE takes SET plays = 1 WHERE id IN (SELECT take_id FROM tracks WHERE take_id IS NOT NULL);
UPDATE takes SET plays = 1 WHERE id IN (
  SELECT MAX(k.id) FROM takes k JOIN tracks t ON t.id = k.track_id WHERE t.take_id IS NULL GROUP BY k.track_id);
UPDATE takes SET part = (SELECT k2.id FROM takes k2 WHERE k2.track_id = takes.track_id AND k2.plays = 1 LIMIT 1);
UPDATE tracks SET take_id = (SELECT k.id FROM takes k WHERE k.track_id = tracks.id AND k.plays = 1 LIMIT 1);
CREATE INDEX IF NOT EXISTS takes_part ON takes(part);
