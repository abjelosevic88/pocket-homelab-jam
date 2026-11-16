package store

import (
	"context"
)

// ---- bands: folders for songs ----

const bandCols = `b.id, b.user_id, b.name, b.color, b.notes, b.created_at,
	(SELECT COUNT(1) FROM songs s WHERE s.band_id = b.id),
	COALESCE((SELECT MAX(COALESCE(s.opened_at, s.updated_at)) FROM songs s WHERE s.band_id = b.id), '')`

func scanBand(r scanner) (*Band, error) {
	var b Band
	if err := r.Scan(&b.ID, &b.UserID, &b.Name, &b.Color, &b.Notes, &b.CreatedAt, &b.Songs, &b.UpdatedAt); err != nil {
		return nil, err
	}
	return &b, nil
}

// ListBands lists a person's bands by name.
func (s *Store) ListBands(ctx context.Context, uid int64) ([]Band, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+bandCols+` FROM bands b WHERE b.user_id = ? ORDER BY b.name COLLATE NOCASE, b.id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Band{}
	for rows.Next() {
		b, err := scanBand(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

// GetBand loads one band.
func (s *Store) GetBand(ctx context.Context, uid, id int64) (*Band, error) {
	b, err := scanBand(s.DB.QueryRowContext(ctx, `SELECT `+bandCols+` FROM bands b WHERE b.user_id = ? AND b.id = ?`, uid, id))
	return b, notFound(err)
}

// BandByName finds a band by its name (any case); nil when there is none.
func (s *Store) BandByName(ctx context.Context, uid int64, name string) *Band {
	b, err := scanBand(s.DB.QueryRowContext(ctx, `SELECT `+bandCols+` FROM bands b WHERE b.user_id = ? AND b.name = ? COLLATE NOCASE`, uid, name))
	if err != nil {
		return nil
	}
	return b
}

// CreateBand adds a band.
func (s *Store) CreateBand(ctx context.Context, b *Band) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO bands (user_id, name, color, notes) VALUES (?, ?, ?, ?)`, b.UserID, b.Name, b.Color, b.Notes)
	if err != nil {
		return err
	}
	b.ID, _ = res.LastInsertId()
	return nil
}

// UpdateBand saves a band's name, colour and notes.
func (s *Store) UpdateBand(ctx context.Context, b *Band) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE bands SET name = ?, color = ?, notes = ? WHERE user_id = ? AND id = ?`, b.Name, b.Color, b.Notes, b.UserID, b.ID)
	return affected(res, err)
}

// DeleteBand removes a band; its songs stay, in no band.
func (s *Store) DeleteBand(ctx context.Context, uid, id int64) error {
	if _, err := s.DB.ExecContext(ctx, `UPDATE songs SET band_id = NULL WHERE user_id = ? AND band_id = ?`, uid, id); err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx, `DELETE FROM bands WHERE user_id = ? AND id = ?`, uid, id)
	return affected(res, err)
}

// ---- marks: notes pinned to a moment of a song ----

const markCols = `id, user_id, song_id, track_id, at_ms, end_ms, text, color, created_at, updated_at`

func scanMark(r scanner) (*Mark, error) {
	var m Mark
	if err := r.Scan(&m.ID, &m.UserID, &m.SongID, &m.TrackID, &m.AtMS, &m.EndMS, &m.Text, &m.Color, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

// ListMarks lists a song's marks in time order.
func (s *Store) ListMarks(ctx context.Context, uid, songID int64) ([]Mark, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+markCols+` FROM marks WHERE user_id = ? AND song_id = ? ORDER BY at_ms, id`, uid, songID)
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

// GetMark loads one mark.
func (s *Store) GetMark(ctx context.Context, uid, id int64) (*Mark, error) {
	m, err := scanMark(s.DB.QueryRowContext(ctx, `SELECT `+markCols+` FROM marks WHERE user_id = ? AND id = ?`, uid, id))
	return m, notFound(err)
}

// CountMarks counts a song's marks.
func (s *Store) CountMarks(ctx context.Context, uid, songID int64) int {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM marks WHERE user_id = ? AND song_id = ?`, uid, songID).Scan(&n)
	return n
}

// CreateMark adds a mark.
func (s *Store) CreateMark(ctx context.Context, m *Mark) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO marks (user_id, song_id, track_id, at_ms, end_ms, text, color, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, COALESCE(NULLIF(?, ''), strftime('%Y-%m-%dT%H:%M:%fZ','now')))`,
		m.UserID, m.SongID, m.TrackID, m.AtMS, m.EndMS, m.Text, m.Color, m.CreatedAt)
	if err != nil {
		return err
	}
	m.ID, _ = res.LastInsertId()
	s.TouchSong(ctx, m.UserID, m.SongID, false)
	return nil
}

// UpdateMark saves a mark.
func (s *Store) UpdateMark(ctx context.Context, m *Mark) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE marks SET track_id = ?, at_ms = ?, end_ms = ?, text = ?, color = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id = ? AND id = ?`,
		m.TrackID, m.AtMS, m.EndMS, m.Text, m.Color, m.UserID, m.ID)
	if err == nil {
		s.TouchSong(ctx, m.UserID, m.SongID, false)
	}
	return affected(res, err)
}

// DeleteMark removes a mark.
func (s *Store) DeleteMark(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM marks WHERE user_id = ? AND id = ?`, uid, id)
	return affected(res, err)
}
