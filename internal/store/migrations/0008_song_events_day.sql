-- A day's journal (Pocket Day) asks which songs were changed between two
-- moments; without this it reads every line of every song's history.
CREATE INDEX IF NOT EXISTS song_events_user_at ON song_events(user_id, at);
