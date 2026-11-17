package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// SongEvent is one line of a song's history.
type SongEvent struct {
	ID     int64  `json:"id"`
	UserID int64  `json:"-"`
	SongID int64  `json:"song_id"`
	At     string `json:"at"`
	Kind   string `json:"kind"` // song, track, take, part, mix, note, mixer, group
	Text   string `json:"text"`
	Key    string `json:"-"` // the same key in a row (a fader moved again) stays one line
	Old    string `json:"-"` // where that run started
	Via    string `json:"via"` // the linked app that made the change, if any
}

// MaxSongEvents is how many lines a song keeps; older ones go.
const MaxSongEvents = 3000

const eventCols = `id, user_id, song_id, at, kind, text, key, old, via`

func scanEvent(r scanner) (*SongEvent, error) {
	var e SongEvent
	if err := r.Scan(&e.ID, &e.UserID, &e.SongID, &e.At, &e.Kind, &e.Text, &e.Key, &e.Old, &e.Via); err != nil {
		return nil, err
	}
	return &e, nil
}

func nowStamp() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

// AddSongEvent writes a line, and lets the oldest go past MaxSongEvents.
func (s *Store) AddSongEvent(ctx context.Context, e *SongEvent) error {
	e.At = nowStamp()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO song_events (user_id, song_id, at, kind, text, key, old, via) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.UserID, e.SongID, e.At, e.Kind, e.Text, e.Key, e.Old, e.Via)
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
