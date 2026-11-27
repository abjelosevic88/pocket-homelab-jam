package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// SongEvent is one line of a song's history.
type SongEvent struct {
	ID       int64  `json:"id"`
	UserID   int64  `json:"-"`
	SongID   int64  `json:"song_id"`
	At       string `json:"at"`
	Kind     string `json:"kind"` // song, track, take, part, mix, note, mixer, group
	Text     string `json:"text"`
	Key      string `json:"-"`         // the same key in a row (a fader moved again) stays one line
	Old      string `json:"-"`         // where that run started
	Via      string `json:"via"`       // the linked app that made the change, if any
	Undo     string `json:"-"`         // what puts it back (JSON); "" = can't be undone
	UndoneBy int64  `json:"undone_by"` // the line that undid it (-1: undone, nothing was left to change)
	Ref      int64  `json:"ref"`       // an undo line: the line it undid
	CanUndo  bool   `json:"can_undo"`
}

// MaxSongEvents is how many lines a song keeps; older ones go.
const MaxSongEvents = 3000

const eventCols = `id, user_id, song_id, at, kind, text, key, old, via, undo, undone_by, ref`

func scanEvent(r scanner) (*SongEvent, error) {
	var e SongEvent
	if err := r.Scan(&e.ID, &e.UserID, &e.SongID, &e.At, &e.Kind, &e.Text, &e.Key, &e.Old, &e.Via, &e.Undo, &e.UndoneBy, &e.Ref); err != nil {
		return nil, err
	}
	e.CanUndo = e.Undo != "" && e.UndoneBy == 0
	return &e, nil
}

func nowStamp() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

// AddSongEvent writes a line, and lets the oldest go past MaxSongEvents.
func (s *Store) AddSongEvent(ctx context.Context, e *SongEvent) error {
	e.At = nowStamp()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO song_events (user_id, song_id, at, kind, text, key, old, via, undo, ref) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.UserID, e.SongID, e.At, e.Kind, e.Text, e.Key, e.Old, e.Via, e.Undo, e.Ref)
	if err != nil {
		return err
	}
	e.ID, _ = res.LastInsertId()
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM song_events WHERE song_id = ? AND id <= (SELECT id FROM song_events WHERE song_id = ? ORDER BY id DESC LIMIT 1 OFFSET ?)`,
		e.SongID, e.SongID, MaxSongEvents)
	return nil
}

// LastSongEvent is the newest line of a song (nil when it has none).
func (s *Store) LastSongEvent(ctx context.Context, uid, songID int64) (*SongEvent, error) {
	e, err := scanEvent(s.DB.QueryRowContext(ctx, `SELECT `+eventCols+` FROM song_events WHERE user_id = ? AND song_id = ? ORDER BY id DESC LIMIT 1`, uid, songID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return e, err
}

// RewriteSongEvent gives a line new words and a new time (the run went on).
func (s *Store) RewriteSongEvent(ctx context.Context, id int64, text string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE song_events SET text = ?, at = ? WHERE id = ?`, text, nowStamp(), id)
	return err
}

// DropSongEvent removes a line (the run ended where it started: nothing changed).
func (s *Store) DropSongEvent(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM song_events WHERE id = ?`, id)
	return err
}

// SongEvents is a song's history, newest first; before > 0 pages back.
func (s *Store) SongEvents(ctx context.Context, uid, songID, before int64, limit int) ([]SongEvent, error) {
	if before <= 0 {
		before = 1 << 62
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+eventCols+` FROM song_events WHERE user_id = ? AND song_id = ? AND id < ? ORDER BY id DESC LIMIT ?`, uid, songID, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SongEvent{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// PartTakes are the attempts of a part.
func (s *Store) PartTakes(ctx context.Context, uid, part int64) ([]Take, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+takeCols+` FROM takes WHERE user_id = ? AND part = ? ORDER BY id`, uid, part)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Take{}
	for rows.Next() {
		k, err := scanTake(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *k)
	}
	return out, rows.Err()
}

// RegroupSongEvent: a job's line takes another change (new words, what undoes all of it).
func (s *Store) RegroupSongEvent(ctx context.Context, id int64, text, undo string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE song_events SET text = ?, undo = ?, at = ? WHERE id = ?`, text, undo, nowStamp(), id)
	return err
}

// GetSongEvent is one line of a song's history.
func (s *Store) GetSongEvent(ctx context.Context, uid, songID, id int64) (*SongEvent, error) {
	e, err := scanEvent(s.DB.QueryRowContext(ctx, `SELECT `+eventCols+` FROM song_events WHERE user_id = ? AND song_id = ? AND id = ?`, uid, songID, id))
	return e, notFound(err)
}

// SongEventByKey is the newest line with that key (a job's line).
func (s *Store) SongEventByKey(ctx context.Context, uid, songID int64, key string) (*SongEvent, error) {
	e, err := scanEvent(s.DB.QueryRowContext(ctx, `SELECT `+eventCols+` FROM song_events WHERE user_id = ? AND song_id = ? AND key = ? ORDER BY id DESC LIMIT 1`, uid, songID, key))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return e, err
}

// MarkUndone says which line undid a line (0: it stands again).
func (s *Store) MarkUndone(ctx context.Context, uid, id, by int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE song_events SET undone_by = ? WHERE user_id = ? AND id = ?`, by, uid, id)
	return err
}

// ---- putting things back ----

// TakeState is where a take is: its track, start, part, whether it plays, its layer.
type TakeState struct {
	ID       int64 `json:"id"`
	TrackID  int64 `json:"track"`
	Number   int   `json:"n"`
	OffsetMS int64 `json:"at"`
	Part     int64 `json:"part"`
	Plays    bool  `json:"plays"`
	Layer    int   `json:"layer"`
}

// TakeStates are where the takes on these tracks are now.
func (s *Store) TakeStates(ctx context.Context, uid int64, trackIDs ...int64) ([]TakeState, error) {
	out := []TakeState{}
	for _, t := range trackIDs {
		rows, err := s.DB.QueryContext(ctx, `SELECT id, track_id, number, offset_ms, part, plays, layer FROM takes WHERE user_id = ? AND track_id = ? ORDER BY id`, uid, t)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var k TakeState
			var plays int
			if err := rows.Scan(&k.ID, &k.TrackID, &k.Number, &k.OffsetMS, &k.Part, &plays, &k.Layer); err != nil {
				rows.Close()
				return nil, err
			}
			k.Plays = plays == 1
			out = append(out, k)
		}
		rows.Close()
	}
	return out, nil
}

// TakeStatesOf are where these takes are now (the ones still here).
func (s *Store) TakeStatesOf(ctx context.Context, uid int64, ids []int64) ([]TakeState, error) {
	out := []TakeState{}
	for _, id := range ids {
		var k TakeState
		var plays int
		err := s.DB.QueryRowContext(ctx, `SELECT id, track_id, number, offset_ms, part, plays, layer FROM takes WHERE user_id = ? AND id = ?`, uid, id).Scan(&k.ID, &k.TrackID, &k.Number, &k.OffsetMS, &k.Part, &plays, &k.Layer)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		k.Plays = plays == 1
		out = append(out, k)
	}
	return out, nil
}

// SetTakeStates puts takes back where they were (the ones still here), on tracks of the same song.
func (s *Store) SetTakeStates(ctx context.Context, uid, songID int64, states []TakeState) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	touched := map[int64]bool{}
	for _, k := range states {
		var was, song int64
		if err := tx.QueryRowContext(ctx, `SELECT track_id, song_id FROM takes WHERE user_id = ? AND id = ?`, uid, k.ID).Scan(&was, &song); err != nil {
			continue // gone since
		}
		var toSong int64
		if err := tx.QueryRowContext(ctx, `SELECT song_id FROM tracks WHERE user_id = ? AND id = ?`, uid, k.TrackID).Scan(&toSong); err != nil || toSong != songID || song != songID {
			return ErrNotFound // the track it was on is gone
		}
		if _, err := tx.ExecContext(ctx, `UPDATE takes SET track_id = ?, number = ?, offset_ms = ?, part = ?, plays = ?, layer = ? WHERE user_id = ? AND id = ?`,
			k.TrackID, k.Number, k.OffsetMS, k.Part, boolInt(k.Plays), k.Layer, uid, k.ID); err != nil {
			return err
		}
		touched[was], touched[k.TrackID] = true, true
	}
	for t := range touched {
		if err := syncTrack(ctx, tx, uid, t); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Snapshot is deleted things as they were, to put back with their own ids.
type Snapshot struct {
	Tracks []Track    `json:"tracks,omitempty"`
	Takes  []SnapTake `json:"takes,omitempty"`
	Marks  []Mark     `json:"marks,omitempty"`
	Mixes  []SnapMix  `json:"mixes,omitempty"`
}

// SnapTake and SnapMix keep the file too (it is not in their JSON).
type SnapTake struct {
	Take
	F string `json:"f"`
}
type SnapMix struct {
	Mix
	F string `json:"f"`
}

// Restore puts deleted things back as they were, with their own ids; their files leave the trash.
func (s *Store) Restore(ctx context.Context, uid int64, x *Snapshot) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, t := range x.Tracks {
		if _, err := tx.ExecContext(ctx, `INSERT INTO tracks (id, user_id, song_id, name, instrument, color, volume, pan, muted, solo, position, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			t.ID, uid, t.SongID, t.Name, t.Instrument, t.Color, t.Volume, t.Pan, boolInt(t.Muted), boolInt(t.Solo), t.Position, t.CreatedAt); err != nil {
			return err
		}
	}
	touched := map[int64]bool{}
	for _, k := range x.Takes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO takes (id, user_id, song_id, track_id, number, file, mime, size, duration_ms, offset_ms, latency_ms, sample_rate, channels, peaks, source, source_ref, device, note, liked, created_at, part, plays, layer)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			k.ID, uid, k.SongID, k.TrackID, k.Number, k.F, k.Mime, k.Size, k.DurationMS, k.OffsetMS, k.LatencyMS, k.SampleRate, k.Channels, k.Peaks, k.Source, k.SourceRef, k.Device, k.Note, boolInt(k.Liked), k.CreatedAt, k.Part, boolInt(k.Plays), k.Layer); err != nil {
			return err
		}
		touched[k.TrackID] = true
		if _, err := tx.ExecContext(ctx, `DELETE FROM trash WHERE file = ?`, k.F); err != nil {
			return err
		}
	}
	for _, m := range x.Marks {
		if _, err := tx.ExecContext(ctx, `INSERT INTO marks (id, user_id, song_id, track_id, at_ms, end_ms, text, color, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, uid, m.SongID, m.TrackID, m.AtMS, m.EndMS, m.Text, m.Color, m.CreatedAt, m.UpdatedAt); err != nil {
			return err
		}
	}
	for _, m := range x.Mixes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO mixes (id, user_id, song_id, name, file, mime, size, duration_ms, peaks, tracks, files_path, files_url, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, uid, m.SongID, m.Name, m.F, m.Mime, m.Size, m.DurationMS, m.Peaks, m.Tracks, m.FilesPath, m.FilesURL, m.CreatedAt); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM trash WHERE file = ?`, m.F); err != nil {
			return err
		}
	}
	for t := range touched {
		if err := syncTrack(ctx, tx, uid, t); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Trash keeps a deleted take's or mix's file a while (see TrashedBefore).
func (s *Store) Trash(ctx context.Context, uid int64, file string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO trash (file, user_id, at) VALUES (?, ?, ?) ON CONFLICT(file) DO UPDATE SET at = excluded.at`, file, uid, nowStamp())
	return err
}

// TrashedBefore are files in the trash since before t.
func (s *Store) TrashedBefore(ctx context.Context, t time.Time) ([]string, error) {
	return s.strings(ctx, `SELECT file FROM trash WHERE at < ?`, t.UTC().Format("2006-01-02T15:04:05.000Z"))
}

// Untrash forgets a file (gone for good, or back in use).
func (s *Store) Untrash(ctx context.Context, file string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM trash WHERE file = ?`, file)
	return err
}

// MarksOnTrack are the notes pinned to a track (they go with it).
func (s *Store) MarksOnTrack(ctx context.Context, uid, trackID int64) ([]Mark, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+markCols+` FROM marks WHERE user_id = ? AND track_id = ?`, uid, trackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Mark{}
	for rows.Next() {
		m, err := scanMark(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// SongWork is one song made or changed between two moments: how many lines
// its history got then and when the first came (or when it was made).
type SongWork struct {
	SongID  int64
	Changes int
	First   string
}

// SongsWorkedOn lists the songs made or changed between two UTC instants (a
// day's journal), from their history and the songs' own date, earliest first.
func (s *Store) SongsWorkedOn(ctx context.Context, uid int64, from, to string) ([]SongWork, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT song_id, SUM(n), MIN(first) FROM (
			SELECT song_id, COUNT(1) AS n, MIN(at) AS first FROM song_events WHERE user_id = ? AND at >= ? AND at < ? GROUP BY song_id
			UNION ALL
			SELECT id, 0, created_at FROM songs WHERE user_id = ? AND created_at >= ? AND created_at < ?
		) GROUP BY song_id ORDER BY MIN(first), song_id`, uid, from, to, uid, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SongWork{}
	for rows.Next() {
		var w SongWork
		if err := rows.Scan(&w.SongID, &w.Changes, &w.First); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
