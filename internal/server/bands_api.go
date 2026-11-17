package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-jam/internal/store"
)

const (
	maxBands     = 500
	maxMarks     = 500 // on one song
	maxMarkText  = 2000
	maxBandNotes = 4000
)

// ---- bands ----

type bandInput struct {
	Name  *string `json:"name"`
	Color *string `json:"color"`
	Notes *string `json:"notes"`
}

func (in *bandInput) apply(b *store.Band) string {
	if in.Name != nil {
		b.Name = cleanText(*in.Name, maxName)
	}
	if in.Color != nil {
		b.Color = strings.TrimSpace(*in.Color)
	}
	if in.Notes != nil {
		b.Notes = clip(*in.Notes, maxBandNotes)
	}
	switch {
	case b.Name == "":
		return "give the band a name"
	case b.Color != "" && !reColor.MatchString(b.Color):
		return "the colour is #rrggbb"
	}
	return ""
}

func (s *Server) handleBands(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.store.ListBands(ctx, actorID(ctx))
	if err != nil {
		s.fail(w, err, "bands")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bands": list})
}

func (s *Server) handleGetBand(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, err := s.store.GetBand(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "band")
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) handleCreateBand(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in bandInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	b := &store.Band{UserID: uid}
	if m := in.apply(b); m != "" {
		writeErr(w, http.StatusBadRequest, m)
		return
	}
	if have := s.store.BandByName(ctx, uid, b.Name); have != nil {
		writeJSON(w, http.StatusOK, have) // the same name is the same band
		return
	}
	if list, _ := s.store.ListBands(ctx, uid); len(list) >= maxBands {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("up to %d bands", maxBands))
		return
	}
	if err := s.store.CreateBand(ctx, b); err != nil {
		s.fail(w, err, "save band")
		return
	}
	out, _ := s.store.GetBand(ctx, uid, b.ID)
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleUpdateBand(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	b, err := s.store.GetBand(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "band")
		return
	}
	var in bandInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if m := in.apply(b); m != "" {
		writeErr(w, http.StatusBadRequest, m)
		return
	}
	if have := s.store.BandByName(ctx, uid, b.Name); have != nil && have.ID != b.ID {
		writeErr(w, http.StatusConflict, "there is a band called "+have.Name+" already")
		return
	}
	if err := s.store.UpdateBand(ctx, b); err != nil {
		s.fail(w, err, "save band")
		return
	}
	out, _ := s.store.GetBand(ctx, uid, b.ID)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteBand(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.store.DeleteBand(ctx, actorID(ctx), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete band")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// bandFor turns a song's band_id (0 = none) or band_name (made when new) into
// the band to keep; msg is set when it isn't one of the person's bands.
func (s *Server) bandFor(ctx context.Context, uid int64, id *int64, name *string) (band *int64, set bool, msg string) {
	if name != nil {
		n := cleanText(*name, maxName)
		if n == "" {
			return nil, true, ""
		}
		if b := s.store.BandByName(ctx, uid, n); b != nil {
			return &b.ID, true, ""
		}
		b := &store.Band{UserID: uid, Name: n}
		if err := s.store.CreateBand(ctx, b); err != nil {
			return nil, true, "could not make the band"
		}
		return &b.ID, true, ""
	}
	if id == nil {
		return nil, false, ""
	}
	if *id == 0 {
		return nil, true, ""
	}
	b, err := s.store.GetBand(ctx, uid, *id)
	if err != nil {
		return nil, true, "that band is not here"
	}
	return &b.ID, true, ""
}

// ---- marks: notes pinned to a moment of a song ----

type markInput struct {
	TrackID *int64  `json:"track_id"` // 0 = the whole song
	AtMS    *int64  `json:"at_ms"`
	EndMS   *int64  `json:"end_ms"` // 0 = a moment, else the end of a stretch
	Text    *string `json:"text"`
	Color   *string `json:"color"`
}

// apply copies what was sent onto m and checks it against the song's tracks.
func (in *markInput) apply(m *store.Mark, tracks []store.Track) string {
	if in.TrackID != nil {
		if *in.TrackID == 0 {
			m.TrackID = nil
		} else {
			ok := false
			for _, t := range tracks {
				if t.ID == *in.TrackID {
					ok = true
				}
			}
			if !ok {
				return "that track is not in this song"
			}
			id := *in.TrackID
			m.TrackID = &id
		}
	}
	if in.AtMS != nil {
		m.AtMS = *in.AtMS
	}
	if in.EndMS != nil {
		m.EndMS = *in.EndMS
	}
	if in.Text != nil {
		m.Text = strings.TrimSpace(clip(*in.Text, maxMarkText))
	}
	if in.Color != nil {
		m.Color = strings.TrimSpace(*in.Color)
	}
	if m.EndMS > 0 && m.EndMS <= m.AtMS {
		m.EndMS = 0
	}
	switch {
	case m.AtMS < 0 || m.AtMS > maxLengthMS || m.EndMS > maxLengthMS:
		return "the note is somewhere in the song"
	case m.Color != "" && !reColor.MatchString(m.Color):
		return "the colour is #rrggbb"
	}
	return ""
}

func (s *Server) handleCreateMark(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	so, err := s.store.SongWithTracks(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "song")
		return
	}
	if !mayChange(ctx, so.Source) {
		writeErr(w, http.StatusForbidden, "this song was not added by "+linkApp(ctx))
		return
	}
	if len(so.Marks) >= maxMarks {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("a song has up to %d notes", maxMarks))
		return
	}
	var in markInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	m := &store.Mark{UserID: uid, SongID: so.ID}
	if msg := in.apply(m, so.Tracks); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.CreateMark(ctx, m); err != nil {
		s.fail(w, err, "save note")
		return
	}
	s.note(ctx, uid, so.ID, change{kind: "note", text: fmt.Sprintf("Added a note at %s: %s", markWhere(m), quoteText(m.Text, 80))}.with(call("DELETE", "/marks/"+itoa(m.ID), nil)))
	out, _ := s.store.GetMark(ctx, uid, m.ID)
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleUpdateMark(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	m, err := s.store.GetMark(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "note")
		return
	}
	so, err := s.store.SongWithTracks(ctx, uid, m.SongID)
	if err != nil {
		s.fail(w, err, "song")
		return
	}
	if !mayChange(ctx, so.Source) {
		writeErr(w, http.StatusForbidden, "this song was not added by "+linkApp(ctx))
		return
	}
	var in markInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	was := *m
	if msg := in.apply(m, so.Tracks); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateMark(ctx, m); err != nil {
		s.fail(w, err, "save note")
		return
	}
	s.markChanges(ctx, uid, &was, m)
	out, _ := s.store.GetMark(ctx, uid, m.ID)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteMark(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	m, err := s.store.GetMark(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "note")
		return
	}
	if so, err := s.store.GetSong(ctx, uid, m.SongID); err == nil && !mayChange(ctx, so.Source) {
		writeErr(w, http.StatusForbidden, "this song was not added by "+linkApp(ctx))
		return
	}
	if err := s.store.DeleteMark(ctx, uid, m.ID); err != nil {
		s.fail(w, err, "delete note")
		return
	}
	s.note(ctx, uid, m.SongID, change{kind: "note", text: fmt.Sprintf("Deleted the note at %s: %s", markWhere(m), quoteText(m.Text, 80))}.with(undoOp{Restore: &store.Snapshot{Marks: []store.Mark{*m}}}))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
