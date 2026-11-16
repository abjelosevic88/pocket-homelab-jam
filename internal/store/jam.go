package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// ---- songs ----

const songCols = `s.id, s.user_id, s.title, s.artist, s.notes, s.tempo, s.beats, s.beat_unit, s.key_name, s.count_in, s.click,
	s.loop_start_ms, s.loop_end_ms, s.starred, s.color, s.source, s.band_id, COALESCE((SELECT b.name FROM bands b WHERE b.id = s.band_id), ''), s.opened_at, s.created_at, s.updated_at,
	(SELECT COUNT(1) FROM tracks t WHERE t.song_id = s.id),
	(SELECT COUNT(1) FROM takes k WHERE k.song_id = s.id),
	COALESCE((SELECT MAX(k.offset_ms + k.duration_ms) FROM tracks t JOIN takes k ON k.id = t.take_id WHERE t.song_id = s.id), 0),
	COALESCE((SELECT SUM(seconds) FROM sittings x WHERE x.song_id = s.id), 0) / 60`

type scanner interface{ Scan(...any) error }

func scanSong(r scanner) (*Song, error) {
	var so Song
	var click, starred int
	if err := r.Scan(&so.ID, &so.UserID, &so.Title, &so.Artist, &so.Notes, &so.Tempo, &so.Beats, &so.BeatUnit, &so.KeyName, &so.CountIn, &click,
		&so.LoopStartMS, &so.LoopEndMS, &starred, &so.Color, &so.Source, &so.BandID, &so.BandName, &so.OpenedAt, &so.CreatedAt, &so.UpdatedAt,
		&so.TrackCount, &so.TakeCount, &so.LengthMS, &so.Minutes); err != nil {
		return nil, err
	}
	so.Click, so.Starred = click == 1, starred == 1
	return &so, nil
}

// SongFilter narrows the song list.
type SongFilter struct {
	Q       string
	Starred bool
	Band    int64  // 0 = any, -1 = songs in no band
	Sort    string // recent | title | added | time
	Limit   int
	Offset  int
}

// ListSongs lists one person's songs.
func (s *Store) ListSongs(ctx context.Context, uid int64, f SongFilter) ([]Song, int, error) {
	where := []string{"s.user_id = ?"}
	args := []any{uid}
	if q := strings.TrimSpace(f.Q); q != "" {
		where = append(where, `(s.title LIKE ? OR s.artist LIKE ? OR s.notes LIKE ? OR s.key_name LIKE ?
			OR EXISTS (SELECT 1 FROM bands b WHERE b.id = s.band_id AND b.name LIKE ?) OR EXISTS (SELECT 1 FROM marks m WHERE m.song_id = s.id AND m.text LIKE ?))`)
		like := "%" + q + "%"
		args = append(args, like, like, like, like, like, like)
	}
	if f.Starred {
		where = append(where, "s.starred = 1")
	}
	if f.Band > 0 {
		where = append(where, "s.band_id = ?")
		args = append(args, f.Band)
	} else if f.Band < 0 {
		where = append(where, "s.band_id IS NULL")
	}
	w := strings.Join(where, " AND ")
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM songs s WHERE `+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := "COALESCE(s.opened_at, s.updated_at) DESC, s.id DESC"
	switch f.Sort {
	case "title":
		order = "s.title COLLATE NOCASE, s.id"
	case "added":
		order = "s.created_at DESC, s.id DESC"
	case "time":
		order = "COALESCE((SELECT SUM(seconds) FROM sittings x WHERE x.song_id = s.id), 0) DESC, s.id DESC"
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 200
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+songCols+` FROM songs s WHERE `+w+` ORDER BY `+order+` LIMIT ? OFFSET ?`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Song{}
	for rows.Next() {
		so, err := scanSong(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *so)
	}
	return out, total, rows.Err()
}

// GetSong loads one of a person's songs (without tracks).
func (s *Store) GetSong(ctx context.Context, uid, id int64) (*Song, error) {
	so, err := scanSong(s.DB.QueryRowContext(ctx, `SELECT `+songCols+` FROM songs s WHERE s.user_id = ? AND s.id = ?`, uid, id))
	return so, notFound(err)
}

// SongWithTracks loads a song with its tracks and every take.
func (s *Store) SongWithTracks(ctx context.Context, uid, id int64) (*Song, error) {
	so, err := s.GetSong(ctx, uid, id)
	if err != nil {
		return nil, err
	}
	tracks, err := s.ListTracks(ctx, uid, id)
	if err != nil {
		return nil, err
	}
	takes, err := s.ListTakes(ctx, uid, id)
	if err != nil {
		return nil, err
	}
	by := map[int64][]Take{}
	for _, k := range takes {
		by[k.TrackID] = append(by[k.TrackID], k)
	}
	for i := range tracks {
		tracks[i].Takes = by[tracks[i].ID]
		if tracks[i].Takes == nil {
			tracks[i].Takes = []Take{}
		}
	}
	so.Tracks = tracks
	if so.Marks, err = s.ListMarks(ctx, uid, id); err != nil {
		return nil, err
	}
	return so, nil
}

// CreateSong adds a song.
func (s *Store) CreateSong(ctx context.Context, so *Song) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO songs (user_id, title, artist, notes, tempo, beats, beat_unit, key_name, count_in, click, loop_start_ms, loop_end_ms, starred, color, source, band_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, COALESCE(NULLIF(?, ''), strftime('%Y-%m-%dT%H:%M:%fZ','now')), strftime('%Y-%m-%dT%H:%M:%fZ','now'))`,
		so.UserID, so.Title, so.Artist, so.Notes, so.Tempo, so.Beats, so.BeatUnit, so.KeyName, so.CountIn, boolInt(so.Click), so.LoopStartMS, so.LoopEndMS, boolInt(so.Starred), so.Color, so.Source, so.BandID, so.CreatedAt)
	if err != nil {
		return err
	}
	so.ID, _ = res.LastInsertId()
	return nil
}

// UpdateSong saves a song's own fields.
func (s *Store) UpdateSong(ctx context.Context, so *Song) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE songs SET title = ?, artist = ?, notes = ?, tempo = ?, beats = ?, beat_unit = ?, key_name = ?, count_in = ?, click = ?,
		loop_start_ms = ?, loop_end_ms = ?, starred = ?, color = ?, band_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id = ? AND id = ?`,
		so.Title, so.Artist, so.Notes, so.Tempo, so.Beats, so.BeatUnit, so.KeyName, so.CountIn, boolInt(so.Click), so.LoopStartMS, so.LoopEndMS, boolInt(so.Starred), so.Color, so.BandID, so.UserID, so.ID)
	return affected(res, err)
}

// TouchSong marks a song changed (a take came in) and opened.
func (s *Store) TouchSong(ctx context.Context, uid, id int64, opened bool) {
	q := `UPDATE songs SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id = ? AND id = ?`
	if opened {
		q = `UPDATE songs SET opened_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id = ? AND id = ?`
	}
	_, _ = s.DB.ExecContext(ctx, q, uid, id)
}

// DeleteSong removes a song with its tracks and takes; it returns the audio
// files to remove.
func (s *Store) DeleteSong(ctx context.Context, uid, id int64) ([]string, error) {
	files, err := s.strings(ctx, `SELECT file FROM takes WHERE user_id = ? AND song_id = ?`, uid, id)
	if err != nil {
		return nil, err
	}
	res, err := s.DB.ExecContext(ctx, `DELETE FROM songs WHERE user_id = ? AND id = ?`, uid, id)
	if err := affected(res, err); err != nil {
		return nil, err
	}
	return files, nil
}

// ---- tracks ----

const trackCols = `id, user_id, song_id, name, instrument, color, volume, pan, muted, solo, position, take_id, created_at`

func scanTrack(r scanner) (*Track, error) {
	var t Track
	var muted, solo int
	if err := r.Scan(&t.ID, &t.UserID, &t.SongID, &t.Name, &t.Instrument, &t.Color, &t.Volume, &t.Pan, &muted, &solo, &t.Position, &t.TakeID, &t.CreatedAt); err != nil {
		return nil, err
	}
	t.Muted, t.Solo = muted == 1, solo == 1
	t.Takes = []Take{}
	return &t, nil
}

// ListTracks lists a song's tracks in order.
func (s *Store) ListTracks(ctx context.Context, uid, songID int64) ([]Track, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+trackCols+` FROM tracks WHERE user_id = ? AND song_id = ? ORDER BY position, id`, uid, songID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Track{}
	for rows.Next() {
		t, err := scanTrack(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// GetTrack loads one track.
func (s *Store) GetTrack(ctx context.Context, uid, id int64) (*Track, error) {
	t, err := scanTrack(s.DB.QueryRowContext(ctx, `SELECT `+trackCols+` FROM tracks WHERE user_id = ? AND id = ?`, uid, id))
	return t, notFound(err)
}

// CreateTrack adds a track at the end of the song.
func (s *Store) CreateTrack(ctx context.Context, t *Track) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO tracks (user_id, song_id, name, instrument, color, volume, pan, muted, solo, position)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, COALESCE((SELECT MAX(position) + 1 FROM tracks WHERE song_id = ?), 0))`,
		t.UserID, t.SongID, t.Name, t.Instrument, t.Color, t.Volume, t.Pan, boolInt(t.Muted), boolInt(t.Solo), t.SongID)
	if err != nil {
		return err
	}
	t.ID, _ = res.LastInsertId()
	s.TouchSong(ctx, t.UserID, t.SongID, false)
	return nil
}

// UpdateTrack saves a track's settings (the take that plays included).
func (s *Store) UpdateTrack(ctx context.Context, t *Track) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE tracks SET name = ?, instrument = ?, color = ?, volume = ?, pan = ?, muted = ?, solo = ?, position = ?, take_id = ? WHERE user_id = ? AND id = ?`,
		t.Name, t.Instrument, t.Color, t.Volume, t.Pan, boolInt(t.Muted), boolInt(t.Solo), t.Position, t.TakeID, t.UserID, t.ID)
	return affected(res, err)
}

// DeleteTrack removes a track with its takes; it returns the files to remove.
func (s *Store) DeleteTrack(ctx context.Context, uid, id int64) ([]string, error) {
	files, err := s.strings(ctx, `SELECT file FROM takes WHERE user_id = ? AND track_id = ?`, uid, id)
	if err != nil {
		return nil, err
	}
	res, err := s.DB.ExecContext(ctx, `DELETE FROM tracks WHERE user_id = ? AND id = ?`, uid, id)
	if err := affected(res, err); err != nil {
		return nil, err
	}
	return files, nil
}

// ---- takes ----

const takeCols = `id, user_id, song_id, track_id, number, file, mime, size, duration_ms, offset_ms, latency_ms, sample_rate, channels, peaks, source, source_ref, device, note, liked, created_at`

func scanTake(r scanner) (*Take, error) {
	var k Take
	var liked int
	if err := r.Scan(&k.ID, &k.UserID, &k.SongID, &k.TrackID, &k.Number, &k.File, &k.Mime, &k.Size, &k.DurationMS, &k.OffsetMS, &k.LatencyMS, &k.SampleRate, &k.Channels, &k.Peaks, &k.Source, &k.SourceRef, &k.Device, &k.Note, &liked, &k.CreatedAt); err != nil {
		return nil, err
	}
	k.Liked = liked == 1
	return &k, nil
}

// ListTakes lists every take of a song.
func (s *Store) ListTakes(ctx context.Context, uid, songID int64) ([]Take, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+takeCols+` FROM takes WHERE user_id = ? AND song_id = ? ORDER BY track_id, number`, uid, songID)
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

// GetTake loads one take.
func (s *Store) GetTake(ctx context.Context, uid, id int64) (*Take, error) {
	k, err := scanTake(s.DB.QueryRowContext(ctx, `SELECT `+takeCols+` FROM takes WHERE user_id = ? AND id = ?`, uid, id))
	return k, notFound(err)
}

// CreateTake adds a take to a track and makes it the one that plays.
func (s *Store) CreateTake(ctx context.Context, k *Take) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(number), 0) + 1 FROM takes WHERE track_id = ?`, k.TrackID).Scan(&k.Number); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO takes (user_id, song_id, track_id, number, file, mime, size, duration_ms, offset_ms, latency_ms, sample_rate, channels, peaks, source, source_ref, device, note, liked, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, COALESCE(NULLIF(?, ''), strftime('%Y-%m-%dT%H:%M:%fZ','now')))`,
		k.UserID, k.SongID, k.TrackID, k.Number, k.File, k.Mime, k.Size, k.DurationMS, k.OffsetMS, k.LatencyMS, k.SampleRate, k.Channels, k.Peaks, k.Source, k.SourceRef, k.Device, k.Note, boolInt(k.Liked), k.CreatedAt)
	if err != nil {
		return err
	}
	k.ID, _ = res.LastInsertId()
	if _, err := tx.ExecContext(ctx, `UPDATE tracks SET take_id = ? WHERE user_id = ? AND id = ?`, k.ID, k.UserID, k.TrackID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE songs SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id = ? AND id = ?`, k.UserID, k.SongID); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateTake saves where a take starts, its note and the heart.
func (s *Store) UpdateTake(ctx context.Context, k *Take) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE takes SET offset_ms = ?, note = ?, liked = ? WHERE user_id = ? AND id = ?`, k.OffsetMS, k.Note, boolInt(k.Liked), k.UserID, k.ID)
	return affected(res, err)
}

// DeleteTake removes a take; when it was the one that plays, the newest
// other take plays instead. It returns the file to remove.
func (s *Store) DeleteTake(ctx context.Context, uid, id int64) (string, error) {
	k, err := s.GetTake(ctx, uid, id)
	if err != nil {
		return "", err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM takes WHERE user_id = ? AND id = ?`, uid, id); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tracks SET take_id = (SELECT id FROM takes WHERE track_id = tracks.id ORDER BY number DESC LIMIT 1)
		WHERE user_id = ? AND id = ? AND (take_id = ? OR take_id IS NULL)`, uid, k.TrackID, id); err != nil {
		return "", err
	}
	return k.File, tx.Commit()
}

// ---- mixes ----

const mixCols = `m.id, m.user_id, m.song_id, COALESCE(s.title, ''), m.name, m.file, m.mime, m.size, m.duration_ms, m.peaks, m.tracks, m.files_path, m.files_url, m.created_at`

func scanMix(r scanner) (*Mix, error) {
	var m Mix
	if err := r.Scan(&m.ID, &m.UserID, &m.SongID, &m.SongTitle, &m.Name, &m.File, &m.Mime, &m.Size, &m.DurationMS, &m.Peaks, &m.Tracks, &m.FilesPath, &m.FilesURL, &m.CreatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

// ListMixes lists a person's mixes, newest first (songID 0 = all songs).
func (s *Store) ListMixes(ctx context.Context, uid, songID int64, limit int) ([]Mix, error) {
	q := `SELECT ` + mixCols + ` FROM mixes m LEFT JOIN songs s ON s.id = m.song_id WHERE m.user_id = ?`
	args := []any{uid}
	if songID > 0 {
		q += ` AND m.song_id = ?`
		args = append(args, songID)
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY m.created_at DESC, m.id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Mix{}
	for rows.Next() {
		m, err := scanMix(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// GetMix loads one mix.
func (s *Store) GetMix(ctx context.Context, uid, id int64) (*Mix, error) {
	m, err := scanMix(s.DB.QueryRowContext(ctx, `SELECT `+mixCols+` FROM mixes m LEFT JOIN songs s ON s.id = m.song_id WHERE m.user_id = ? AND m.id = ?`, uid, id))
	return m, notFound(err)
}

// CreateMix adds a mix.
func (s *Store) CreateMix(ctx context.Context, m *Mix) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO mixes (user_id, song_id, name, file, mime, size, duration_ms, peaks, tracks, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, COALESCE(NULLIF(?, ''), strftime('%Y-%m-%dT%H:%M:%fZ','now')))`,
		m.UserID, m.SongID, m.Name, m.File, m.Mime, m.Size, m.DurationMS, m.Peaks, m.Tracks, m.CreatedAt)
	if err != nil {
		return err
	}
	m.ID, _ = res.LastInsertId()
	return nil
}

// UpdateMix saves a mix's name and where it went in Pocket Files.
func (s *Store) UpdateMix(ctx context.Context, m *Mix) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE mixes SET name = ?, files_path = ?, files_url = ? WHERE user_id = ? AND id = ?`, m.Name, m.FilesPath, m.FilesURL, m.UserID, m.ID)
	return affected(res, err)
}

// DeleteMix removes a mix and returns its file.
func (s *Store) DeleteMix(ctx context.Context, uid, id int64) (string, error) {
	m, err := s.GetMix(ctx, uid, id)
	if err != nil {
		return "", err
	}
	_, err = s.DB.ExecContext(ctx, `DELETE FROM mixes WHERE user_id = ? AND id = ?`, uid, id)
	return m.File, err
}

// ---- sittings: time spent ----

const sittingCols = `id, user_id, song_id, title, day, started_at, last_at, seconds, takes, sent_to, sent_ref, sent_seconds`

func scanSitting(r scanner) (*Sitting, error) {
	var x Sitting
	if err := r.Scan(&x.ID, &x.UserID, &x.SongID, &x.Title, &x.Day, &x.StartedAt, &x.LastAt, &x.Seconds, &x.Takes, &x.SentTo, &x.SentRef, &x.SentSeconds); err != nil {
		return nil, err
	}
	return &x, nil
}

// SittingGap is how long a pause may be before the next minute starts a new sitting.
const SittingGap = 10 * time.Minute

// AddTime adds seconds played on a song now: to the open sitting when the
// last minute was less than SittingGap ago on the same day, else to a new one.
func (s *Store) AddTime(ctx context.Context, uid int64, songID *int64, title string, seconds, takes int, now time.Time) (*Sitting, error) {
	day := now.Format("2006-01-02")
	nowS := now.UTC().Format(time.RFC3339Nano)
	since := now.Add(-SittingGap).UTC().Format(time.RFC3339Nano)
	q := `SELECT ` + sittingCols + ` FROM sittings WHERE user_id = ? AND day = ? AND last_at >= ? AND song_id IS ? ORDER BY last_at DESC LIMIT 1`
	x, err := scanSitting(s.DB.QueryRowContext(ctx, q, uid, day, since, songID))
	switch {
	case err == nil:
		_, err = s.DB.ExecContext(ctx, `UPDATE sittings SET seconds = seconds + ?, takes = takes + ?, last_at = ?, title = ? WHERE id = ?`, seconds, takes, nowS, title, x.ID)
		if err != nil {
			return nil, err
		}
		x.Seconds += seconds
		x.Takes += takes
		x.LastAt, x.Title = nowS, title
		return x, nil
	case err != sql.ErrNoRows:
		return nil, err
	}
	started := now.Add(-time.Duration(seconds) * time.Second).UTC().Format(time.RFC3339Nano)
	res, err := s.DB.ExecContext(ctx, `INSERT INTO sittings (user_id, song_id, title, day, started_at, last_at, seconds, takes) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		uid, songID, title, day, started, nowS, seconds, takes)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Sitting{ID: id, UserID: uid, SongID: songID, Title: title, Day: day, StartedAt: started, LastAt: nowS, Seconds: seconds, Takes: takes}, nil
}

// InsertSitting stores a whole sitting (demo data, import).
func (s *Store) InsertSitting(ctx context.Context, x *Sitting) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO sittings (user_id, song_id, title, day, started_at, last_at, seconds, takes, sent_to) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		x.UserID, x.SongID, x.Title, x.Day, x.StartedAt, x.LastAt, x.Seconds, x.Takes, x.SentTo)
	if err != nil {
		return err
	}
	x.ID, _ = res.LastInsertId()
	return nil
}

// MarkSent records that a sitting reached Pocket Guitar or Pocket Time.
func (s *Store) MarkSent(ctx context.Context, uid, id int64, to, ref string, seconds int) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE sittings SET sent_to = ?, sent_ref = ?, sent_seconds = ? WHERE user_id = ? AND id = ?`, to, ref, seconds, uid, id)
	return err
}

// ListSittings lists a person's sittings between two days (inclusive), newest first.
func (s *Store) ListSittings(ctx context.Context, uid int64, from, to string) ([]Sitting, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+sittingCols+` FROM sittings WHERE user_id = ? AND day >= ? AND day <= ? ORDER BY started_at DESC`, uid, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Sitting{}
	for rows.Next() {
		x, err := scanSitting(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *x)
	}
	return out, rows.Err()
}

// DayMinutes is time spent per day.
type DayMinutes struct {
	Day     string `json:"day"`
	Minutes int    `json:"minutes"`
	Takes   int    `json:"takes"`
}

// MinutesPerDay sums the time per day between two days.
func (s *Store) MinutesPerDay(ctx context.Context, uid int64, from, to string) ([]DayMinutes, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT day, SUM(seconds), SUM(takes) FROM sittings WHERE user_id = ? AND day >= ? AND day <= ? GROUP BY day ORDER BY day`, uid, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DayMinutes{}
	for rows.Next() {
		var d DayMinutes
		var sec int
		if err := rows.Scan(&d.Day, &sec, &d.Takes); err != nil {
			return nil, err
		}
		d.Minutes = (sec + 30) / 60
		out = append(out, d)
	}
	return out, rows.Err()
}

// TakesOn lists the takes recorded between two UTC instants (a day's journal).
func (s *Store) TakesOn(ctx context.Context, uid int64, from, to string) ([]Take, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+takeCols+` FROM takes WHERE user_id = ? AND created_at >= ? AND created_at < ? ORDER BY created_at`, uid, from, to)
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

// AllMediaFiles lists every audio file a person has (export, cleanup).
func (s *Store) AllMediaFiles(ctx context.Context, uid int64) ([]string, error) {
	return s.strings(ctx, `SELECT file FROM takes WHERE user_id = ? UNION SELECT file FROM mixes WHERE user_id = ? UNION SELECT file FROM ideas WHERE user_id = ?`, uid, uid, uid)
}

// MediaInUse reports whether any row still points at a file.
func (s *Store) MediaInUse(ctx context.Context, rel string) bool {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT (SELECT COUNT(1) FROM takes WHERE file = ?) + (SELECT COUNT(1) FROM mixes WHERE file = ?) + (SELECT COUNT(1) FROM ideas WHERE file = ?)`, rel, rel, rel).Scan(&n)
	return n > 0
}

// ---- helpers ----

func (s *Store) strings(ctx context.Context, q string, args ...any) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
