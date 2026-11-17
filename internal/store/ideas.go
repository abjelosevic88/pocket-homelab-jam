package store

import "context"

// ---- ideas: recordings on their own, before there is a song ----

const ideaCols = `i.id, i.user_id, i.client_id, i.name, i.file, i.mime, i.size, i.duration_ms, i.sample_rate, i.channels, i.peaks, i.source, i.device, i.app,
	i.band_id, COALESCE((SELECT b.name FROM bands b WHERE b.id = i.band_id), ''), i.created_at`

func scanIdea(r scanner) (*Idea, error) {
	var x Idea
	if err := r.Scan(&x.ID, &x.UserID, &x.ClientID, &x.Name, &x.File, &x.Mime, &x.Size, &x.DurationMS, &x.SampleRate, &x.Channels, &x.Peaks, &x.Source, &x.Device, &x.App,
		&x.BandID, &x.BandName, &x.CreatedAt); err != nil {
		return nil, err
	}
	return &x, nil
}

// ListIdeas lists a person's ideas, newest first; band narrows it like
// SongFilter.Band (0 = any, -1 = ideas in no band).
func (s *Store) ListIdeas(ctx context.Context, uid int64, band int64, limit int) ([]Idea, error) {
	if limit <= 0 || limit > 100000 {
		limit = 1000
	}
	where, args := "i.user_id = ?", []any{uid}
	if band > 0 {
		where += " AND i.band_id = ?"
		args = append(args, band)
	} else if band < 0 {
		where += " AND i.band_id IS NULL"
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+ideaCols+` FROM ideas i WHERE `+where+` ORDER BY i.created_at DESC, i.id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Idea{}
	for rows.Next() {
		x, err := scanIdea(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *x)
	}
	return out, rows.Err()
}

// GetIdea loads one idea.
func (s *Store) GetIdea(ctx context.Context, uid, id int64) (*Idea, error) {
	x, err := scanIdea(s.DB.QueryRowContext(ctx, `SELECT `+ideaCols+` FROM ideas i WHERE i.user_id = ? AND i.id = ?`, uid, id))
	return x, notFound(err)
}

// IdeaByClientID finds the idea a device already sent under its own id.
func (s *Store) IdeaByClientID(ctx context.Context, uid int64, clientID string) (*Idea, error) {
	if clientID == "" {
		return nil, ErrNotFound
	}
	x, err := scanIdea(s.DB.QueryRowContext(ctx, `SELECT `+ideaCols+` FROM ideas i WHERE i.user_id = ? AND i.client_id = ?`, uid, clientID))
	return x, notFound(err)
}

// CreateIdea adds an idea; ErrConflict when the person already has one with
// that client id.
func (s *Store) CreateIdea(ctx context.Context, x *Idea) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO ideas (user_id, client_id, name, file, mime, size, duration_ms, sample_rate, channels, peaks, source, device, app, band_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, COALESCE(NULLIF(?, ''), strftime('%Y-%m-%dT%H:%M:%fZ','now')))`,
		x.UserID, x.ClientID, x.Name, x.File, x.Mime, x.Size, x.DurationMS, x.SampleRate, x.Channels, x.Peaks, x.Source, x.Device, x.App, x.BandID, x.CreatedAt)
	if err != nil {
		return conflict(err)
	}
	x.ID, _ = res.LastInsertId()
	return nil
}

// UpdateIdea saves an idea's name and band.
func (s *Store) UpdateIdea(ctx context.Context, x *Idea) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE ideas SET name = ?, band_id = ? WHERE user_id = ? AND id = ?`, x.Name, x.BandID, x.UserID, x.ID)
	return affected(res, err)
}

// DeleteIdea removes an idea and returns its file.
func (s *Store) DeleteIdea(ctx context.Context, uid, id int64) (string, error) {
	x, err := s.GetIdea(ctx, uid, id)
	if err != nil {
		return "", err
	}
	_, err = s.DB.ExecContext(ctx, `DELETE FROM ideas WHERE user_id = ? AND id = ?`, uid, id)
	return x.File, err
}

// IdeaToSong makes an idea the first track of a new song, all at once: the
// song, its one track, a take on it holding the idea's file (at the start,
// the take that plays), and the idea gone. Nothing changes when any step
// fails, so the idea is never lost. so and t are filled in (IDs).
func (s *Store) IdeaToSong(ctx context.Context, x *Idea, so *Song, t *Track) (*Take, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `INSERT INTO songs (user_id, title, artist, notes, tempo, beats, beat_unit, key_name, count_in, click, loop_start_ms, loop_end_ms, starred, color, source, band_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		so.UserID, so.Title, so.Artist, so.Notes, so.Tempo, so.Beats, so.BeatUnit, so.KeyName, so.CountIn, boolInt(so.Click), so.LoopStartMS, so.LoopEndMS, boolInt(so.Starred), so.Color, so.Source, so.BandID)
	if err != nil {
		return nil, err
	}
	so.ID, _ = res.LastInsertId()
	t.UserID, t.SongID = so.UserID, so.ID
	if res, err = tx.ExecContext(ctx, `INSERT INTO tracks (user_id, song_id, name, instrument, color, volume, pan, muted, solo, position) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		t.UserID, t.SongID, t.Name, t.Instrument, t.Color, t.Volume, t.Pan, boolInt(t.Muted), boolInt(t.Solo)); err != nil {
		return nil, err
	}
	t.ID, _ = res.LastInsertId()
	k := &Take{UserID: so.UserID, SongID: so.ID, TrackID: t.ID, Number: 1, File: x.File, Mime: x.Mime, Size: x.Size, DurationMS: x.DurationMS,
		SampleRate: x.SampleRate, Channels: x.Channels, Peaks: x.Peaks, Source: x.Source, Device: x.Device, CreatedAt: x.CreatedAt}
	if res, err = tx.ExecContext(ctx, `INSERT INTO takes (user_id, song_id, track_id, number, file, mime, size, duration_ms, offset_ms, latency_ms, sample_rate, channels, peaks, source, device, created_at)
		VALUES (?, ?, ?, 1, ?, ?, ?, ?, 0, 0, ?, ?, ?, ?, ?, COALESCE(NULLIF(?, ''), strftime('%Y-%m-%dT%H:%M:%fZ','now')))`,
		k.UserID, k.SongID, k.TrackID, k.File, k.Mime, k.Size, k.DurationMS, k.SampleRate, k.Channels, k.Peaks, k.Source, k.Device, k.CreatedAt); err != nil {
		return nil, err
	}
	k.ID, _ = res.LastInsertId()
	if _, err := tx.ExecContext(ctx, `UPDATE takes SET part = id, plays = 1, layer = 1 WHERE id = ?`, k.ID); err != nil {
		return nil, err
	}
	k.Part, k.Plays, k.Layer = k.ID, true, 1
	if _, err := tx.ExecContext(ctx, `UPDATE tracks SET take_id = ? WHERE id = ?`, k.ID, t.ID); err != nil {
		return nil, err
	}
	t.TakeID = &k.ID
	if res, err = tx.ExecContext(ctx, `DELETE FROM ideas WHERE user_id = ? AND id = ?`, x.UserID, x.ID); err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound // turned into a song at the same moment elsewhere
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return k, nil
}
