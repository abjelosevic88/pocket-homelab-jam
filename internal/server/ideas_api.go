package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-jam/internal/store"
)

// Ideas are quick recordings on their own — a riff, a melody — made here or
// on the phone before there is a song for them. One can become the first
// track of a new song. The phone sends each idea with its own client_id, so
// sending it again (a retry, a flaky connection) never makes a second one.

const maxClientID = 200

// isoUTC is a time as stored: UTC, milliseconds, like the database's own default.
func isoUTC(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// handleIdeas lists ideas; ?band=<id> one band's, ?band=none (or 0) the ones
// in no band.
func (s *Server) handleIdeas(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	v := r.URL.Query().Get("band")
	band := bandParam(v)
	if v == "0" {
		band = -1
	}
	list, err := s.store.ListIdeas(ctx, actorID(ctx), band, queryInt(r, "limit"))
	if err != nil {
		s.fail(w, err, "ideas")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ideas": list})
}

// handleCreateIdea stores a recording as an idea (201), or answers with the
// idea already sent under the same client_id (200) without keeping the file.
func (s *Server) handleCreateIdea(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var have *store.Idea
	known := func(m *audioMeta) bool {
		if x, err := s.store.IdeaByClientID(ctx, uid, clip(strings.TrimSpace(m.ClientID), maxClientID)); err == nil {
			have = x
			return true
		}
		return false
	}
	meta, rel, mime, size, ok := s.readAudioUploadUnless(w, r, uid, known)
	if !ok {
		return
	}
	if have == nil && known(meta) { // the meta came after the file
		_ = os.Remove(s.mediaPath(rel))
	}
	if have != nil {
		writeJSON(w, http.StatusOK, have)
		return
	}
	bad := func(msg string) {
		_ = os.Remove(s.mediaPath(rel))
		writeErr(w, http.StatusBadRequest, msg)
	}
	when := time.Now()
	created := ""
	if c := strings.TrimSpace(meta.CreatedAt); c != "" {
		t, err := time.Parse(time.RFC3339Nano, c)
		if err != nil {
			bad("created_at is a time like 2026-11-15T18:30:00Z")
			return
		}
		when, created = t, isoUTC(t)
	}
	band, _, m := s.bandFor(ctx, uid, meta.BandID, nil)
	if m != "" {
		bad(m)
		return
	}
	x := &store.Idea{UserID: uid, BandID: band, ClientID: clip(strings.TrimSpace(meta.ClientID), maxClientID), Name: firstNonEmpty(meta.Name, "Idea "+when.In(time.Local).Format("2 Jan 2006")),
		File: rel, Mime: mime, Size: size, DurationMS: meta.DurationMS, SampleRate: meta.SampleRate, Channels: meta.Channels, Peaks: meta.Peaks,
		Source: meta.Source, Device: meta.Device, App: sourceOf(ctx), CreatedAt: created}
	if err := s.store.CreateIdea(ctx, x); err != nil {
		_ = os.Remove(s.mediaPath(rel))
		if errors.Is(err, store.ErrConflict) { // the same idea came in twice at once
			if old, err := s.store.IdeaByClientID(ctx, uid, x.ClientID); err == nil {
				writeJSON(w, http.StatusOK, old)
				return
			}
		}
		s.fail(w, err, "save idea")
		return
	}
	out, err := s.store.GetIdea(ctx, uid, x.ID)
	if err != nil {
		s.fail(w, err, "idea")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleGetIdea(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	x, err := s.store.GetIdea(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "idea")
		return
	}
	writeJSON(w, http.StatusOK, x)
}

func (s *Server) handleIdeaAudio(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	x, err := s.store.GetIdea(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "idea")
		return
	}
	if queryBool(r, "download") {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s%s"`, strings.ReplaceAll(fileSafe(x.Name), `"`, ""), filepath.Ext(x.File)))
	}
	s.serveMediaFile(w, r, x.File, x.Mime, "")
}

func (s *Server) handleUpdateIdea(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	x, err := s.store.GetIdea(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "idea")
		return
	}
	if !mayChange(ctx, x.App) {
		writeErr(w, http.StatusForbidden, "this idea was not added by "+linkApp(ctx))
		return
	}
	var in struct {
		Name   *string `json:"name"`
		BandID *int64  `json:"band_id"` // null or 0 = no band
	}
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if in.Name != nil {
		if x.Name = cleanText(*in.Name, maxName); x.Name == "" {
			writeErr(w, http.StatusBadRequest, "give the idea a name")
			return
		}
	}
	if keys["band_id"] {
		band, _, m := s.bandFor(ctx, x.UserID, in.BandID, nil)
		if m != "" {
			writeErr(w, http.StatusBadRequest, m)
			return
		}
		x.BandID = band
	}
	if err := s.store.UpdateIdea(ctx, x); err != nil {
		s.fail(w, err, "save idea")
		return
	}
	out, err := s.store.GetIdea(ctx, x.UserID, x.ID)
	if err != nil {
		s.fail(w, err, "idea")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteIdea(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	x, err := s.store.GetIdea(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "idea")
		return
	}
	if !mayChange(ctx, x.App) {
		writeErr(w, http.StatusForbidden, "this idea was not added by "+linkApp(ctx))
		return
	}
	file, err := s.store.DeleteIdea(ctx, uid, x.ID)
	if err != nil {
		s.fail(w, err, "delete idea")
		return
	}
	s.dropMedia(ctx, file)
	w.WriteHeader(http.StatusNoContent)
}

// handleIdeaToSong makes a new song whose one track plays the idea from the
// start; the idea itself goes (its file stays, now the take's). The song is
// in the idea's band unless band_id (null = none) or band_name says else.
func (s *Server) handleIdeaToSong(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	x, err := s.store.GetIdea(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "idea")
		return
	}
	if !mayChange(ctx, x.App) {
		writeErr(w, http.StatusForbidden, "this idea was not added by "+linkApp(ctx))
		return
	}
	var in struct {
		songInput
		Instrument *string `json:"instrument"`
	}
	keys, err := decodeKeys(r, &in)
	if err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if in.Title != nil && strings.TrimSpace(*in.Title) == "" {
		in.Title = nil
	}
	st := s.settingsOf(ctx, uid)
	so := &store.Song{UserID: uid, Title: x.Name, BandID: x.BandID, Tempo: float64(st.Tempo), Beats: st.Beats, BeatUnit: 4, CountIn: st.CountIn, Click: st.Click, Source: sourceOf(ctx)}
	if m := in.apply(so); m != "" {
		writeErr(w, http.StatusBadRequest, m)
		return
	}
	if keys["band_id"] || keys["band_name"] {
		band, _, m := s.bandFor(ctx, uid, in.BandID, in.BandName)
		if m != "" {
			writeErr(w, http.StatusBadRequest, m)
			return
		}
		so.BandID = band
	}
	inst := "other"
	if in.Instrument != nil && *in.Instrument != "" {
		inst = *in.Instrument
	}
	if !oneOf(inst, instruments) {
		writeErr(w, http.StatusBadRequest, "pick an instrument from the list")
		return
	}
	t := &store.Track{Name: instrumentName(inst), Instrument: inst, Volume: 100}
	if _, err := s.store.IdeaToSong(ctx, x, so, t); err != nil {
		s.fail(w, err, "make the song")
		return
	}
	out, err := s.store.SongWithTracks(ctx, uid, so.ID)
	if err != nil {
		s.fail(w, err, "song")
		return
	}
	s.note(ctx, uid, so.ID, change{kind: "song", text: "Made the song from the idea " + quoteText(x.Name, 80)})
	s.publish(ctx, uid, "jam.song_added", fmt.Sprintf("song:%d", so.ID), map[string]any{"id": so.ID, "title": so.Title, "artist": so.Artist, "url": s.link(fmt.Sprintf("/songs/%d", so.ID))})
	writeJSON(w, http.StatusCreated, out)
}
