-- An idea can be in a band too, like a song. Deleting the band keeps its
-- ideas; they just have no band.
ALTER TABLE ideas ADD COLUMN band_id INTEGER REFERENCES bands(id) ON DELETE SET NULL;
CREATE INDEX ideas_band ON ideas(user_id, band_id);
