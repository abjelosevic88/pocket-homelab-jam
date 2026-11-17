package server

import (
	"archive/zip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-jam/internal/store"
)

// Limits on what comes in.
const (
	maxAudioBytes = 1 << 30               // one take or mix
	maxLengthMS   = 3 * 3600 * 1000       // three hours
	maxPeaksBytes = 50 * 3 * 3600 * 4 / 3 // base64 of 50 values a second for three hours
	maxTracks     = 64
)

var instruments = []string{"voice", "guitar", "bass", "keys", "drums", "strings", "wind", "backing", "other"}

// ---- songs ----

type songInput struct {
	Title       *string  `json:"title"`
	Artist      *string  `json:"artist"`
	Notes       *string  `json:"notes"`
	Tempo       *float64 `json:"tempo"`
	Beats       *int     `json:"beats"`
	BeatUnit    *int     `json:"beat_unit"`
	KeyName     *string  `json:"key_name"`
	CountIn     *int     `json:"count_in"`
	Click       *bool    `json:"click"`
	LoopStartMS *int64   `json:"loop_start_ms"`
	LoopEndMS   *int64   `json:"loop_end_ms"`
	Starred     *bool    `json:"starred"`
	Color       *string  `json:"color"`
	BandID      *int64   `json:"band_id"`   // 0 = no band
	BandName    *string  `json:"band_name"` // or by name, made when new ('' = no band)
}

// apply copies what was sent onto so and checks it; "" when fine.
func (in *songInput) apply(so *store.Song) string {
	if in.Title != nil {
		so.Title = cleanText(*in.Title, maxName)
	}
	if in.Artist != nil {
		so.Artist = cleanText(*in.Artist, maxName)
	}
	if in.Notes != nil {
		so.Notes = clip(*in.Notes, maxNotes)
	}
	if in.Tempo != nil {
		so.Tempo = math.Round(*in.Tempo*10) / 10
	}
	if in.Beats != nil {
		so.Beats = *in.Beats
	}
	if in.BeatUnit != nil {
		so.BeatUnit = *in.BeatUnit
	}
	if in.KeyName != nil {
		so.KeyName = cleanText(*in.KeyName, 40)
	}
	if in.CountIn != nil {
		so.CountIn = *in.CountIn
	}
	if in.Click != nil {
		so.Click = *in.Click
	}
	if in.LoopStartMS != nil {
		so.LoopStartMS = *in.LoopStartMS
	}
	if in.LoopEndMS != nil {
		so.LoopEndMS = *in.LoopEndMS
	}
	if in.Starred != nil {
		so.Starred = *in.Starred
	}
	if in.Color != nil {
		so.Color = strings.TrimSpace(*in.Color)
	}
	switch {
	case so.Title == "":
		return "give the song a name"
	case so.Tempo < 20 || so.Tempo > 400:
		return "the tempo is 20 to 400 beats a minute"
	case so.Beats < 1 || so.Beats > 16:
		return "a bar has 1 to 16 beats"
	case so.BeatUnit != 2 && so.BeatUnit != 4 && so.BeatUnit != 8 && so.BeatUnit != 16:
		return "the beat is a half, quarter, eighth or sixteenth note"
	case so.CountIn < 0 || so.CountIn > 4:
		return "the count-in is 0 to 4 bars"
	case so.LoopStartMS < 0 || so.LoopEndMS < 0 || so.LoopEndMS > maxLengthMS || (so.LoopEndMS > 0 && so.LoopEndMS <= so.LoopStartMS):
		return "the loop ends after it starts"
	case so.Color != "" && !reColor.MatchString(so.Color):
		return "a colour is #rrggbb"
	}
	return ""
}

func (s *Server) handleSongs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	list, total, err := s.store.ListSongs(ctx, actorID(ctx), store.SongFilter{Q: q.Get("q"), Starred: queryBool(r, "starred"), Band: bandParam(q.Get("band")), Sort: q.Get("sort"), Limit: queryInt(r, "limit"), Offset: queryInt(r, "offset")})
	if err != nil {
		s.fail(w, err, "songs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"songs": list, "total": total})
}

// bandParam reads ?band=<id> or ?band=none.
func bandParam(v string) int64 {
	if v == "none" {
		return -1
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	return n
}

func (s *Server) handleCreateSong(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in songInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	st := s.settingsOf(ctx, uid)
	so := &store.Song{UserID: uid, Tempo: float64(st.Tempo), Beats: st.Beats, BeatUnit: 4, CountIn: st.CountIn, Click: st.Click, Source: sourceOf(ctx)}
	if m := in.apply(so); m != "" {
		writeErr(w, http.StatusBadRequest, m)
		return
	}
	if band, set, m := s.bandFor(ctx, uid, in.BandID, in.BandName); m != "" {
		writeErr(w, http.StatusBadRequest, m)
		return
	} else if set {
		so.BandID = band
	}
	if err := s.store.CreateSong(ctx, so); err != nil {
		s.fail(w, err, "save song")
		return
	}
	out, err := s.store.SongWithTracks(ctx, uid, so.ID)
	if err != nil {
		s.fail(w, err, "song")
		return
	}
	s.publish(ctx, uid, "jam.song_added", fmt.Sprintf("song:%d", so.ID), map[string]any{"id": so.ID, "title": so.Title, "artist": so.Artist, "url": s.link(fmt.Sprintf("/songs/%d", so.ID))})
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleGetSong(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	so, err := s.store.SongWithTracks(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "song")
		return
	}
	if linkApp(ctx) == "" && queryBool(r, "open") {
		s.store.TouchSong(ctx, uid, so.ID, true)
	}
	writeJSON(w, http.StatusOK, so)
}

func (s *Server) handleUpdateSong(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	so, err := s.store.GetSong(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "song")
		return
	}
	if !mayChange(ctx, so.Source) {
		writeErr(w, http.StatusForbidden, "this song was not added by "+linkApp(ctx))
		return
	}
	var in songInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if m := in.apply(so); m != "" {
		writeErr(w, http.StatusBadRequest, m)
		return
	}
	if band, set, m := s.bandFor(ctx, uid, in.BandID, in.BandName); m != "" {
		writeErr(w, http.StatusBadRequest, m)
		return
	} else if set {
		so.BandID = band
	}
	if err := s.store.UpdateSong(ctx, so); err != nil {
		s.fail(w, err, "save song")
		return
	}
	out, err := s.store.SongWithTracks(ctx, uid, so.ID)
	if err != nil {
		s.fail(w, err, "song")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteSong(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	so, err := s.store.GetSong(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "song")
		return
	}
	if !mayChange(ctx, so.Source) {
		writeErr(w, http.StatusForbidden, "this song was not added by "+linkApp(ctx))
		return
	}
	files, err := s.store.DeleteSong(ctx, uid, so.ID)
	if err != nil {
		s.fail(w, err, "delete song")
		return
	}
	s.dropMedia(ctx, files...)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleOrder puts a song's tracks in the order given.
func (s *Server) handleOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	songID := idParam(r, "id")
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	tracks, err := s.store.ListTracks(ctx, uid, songID)
	if err != nil {
		s.fail(w, err, "tracks")
		return
	}
	pos := map[int64]int{}
	for i, id := range in.IDs {
		pos[id] = i
	}
	for i := range tracks {
		t := &tracks[i]
		p, ok := pos[t.ID]
		if !ok {
			p = len(in.IDs) + i
		}
		if t.Position != p {
			t.Position = p
			if err := s.store.UpdateTrack(ctx, t); err != nil {
				s.fail(w, err, "save track")
				return
			}
		}
	}
	out, err := s.store.SongWithTracks(ctx, uid, songID)
	if err != nil {
		s.fail(w, err, "song")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- tracks ----

type trackInput struct {
	Name       *string `json:"name"`
	Instrument *string `json:"instrument"`
	Color      *string `json:"color"`
	Volume     *int    `json:"volume"`
	Pan        *int    `json:"pan"`
	Muted      *bool   `json:"muted"`
	Solo       *bool   `json:"solo"`
	Position   *int    `json:"position"`
	TakeID     *int64  `json:"take_id"`
}

func (in *trackInput) apply(t *store.Track) string {
	if in.Name != nil {
		t.Name = cleanText(*in.Name, 60)
	}
	if in.Instrument != nil {
		t.Instrument = *in.Instrument
	}
	if in.Color != nil {
		t.Color = strings.TrimSpace(*in.Color)
	}
	if in.Volume != nil {
		t.Volume = *in.Volume
	}
	if in.Pan != nil {
		t.Pan = *in.Pan
	}
	if in.Muted != nil {
		t.Muted = *in.Muted
	}
	if in.Solo != nil {
		t.Solo = *in.Solo
	}
	if in.Position != nil {
		t.Position = *in.Position
	}
	switch {
	case !oneOf(t.Instrument, instruments):
		return "pick an instrument from the list"
	case t.Volume < 0 || t.Volume > 150:
		return "the volume is 0 to 150 percent"
	case t.Pan < -100 || t.Pan > 100:
		return "the balance is -100 (left) to 100 (right)"
	case t.Color != "" && !reColor.MatchString(t.Color):
		return "a colour is #rrggbb"
	}
	if t.Name == "" {
		t.Name = instrumentName(t.Instrument)
	}
	return ""
}

func instrumentName(i string) string {
	switch i {
	case "voice":
		return "Voice"
	case "guitar":
		return "Guitar"
	case "bass":
		return "Bass"
	case "keys":
		return "Keys"
	case "drums":
		return "Drums"
	case "strings":
		return "Strings"
	case "wind":
		return "Wind"
	case "backing":
		return "Backing track"
	}
	return "Track"
}

func (s *Server) handleCreateTrack(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	so, err := s.store.GetSong(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "song")
		return
	}
	if so.TrackCount >= maxTracks {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("a song has up to %d tracks", maxTracks))
		return
	}
	var in trackInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	in.Position, in.TakeID = nil, nil
	t := &store.Track{UserID: uid, SongID: so.ID, Instrument: "other", Volume: 100}
	if m := in.apply(t); m != "" {
		writeErr(w, http.StatusBadRequest, m)
		return
	}
	if err := s.store.CreateTrack(ctx, t); err != nil {
		s.fail(w, err, "save track")
		return
	}
	out, _ := s.store.GetTrack(ctx, uid, t.ID)
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleUpdateTrack(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	t, err := s.store.GetTrack(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "track")
		return
	}
	var in trackInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if m := in.apply(t); m != "" {
		writeErr(w, http.StatusBadRequest, m)
		return
	}
	if in.TakeID != nil {
		k, err := s.store.GetTake(ctx, uid, *in.TakeID)
		if err != nil || k.TrackID != t.ID {
			writeErr(w, http.StatusBadRequest, "that take is not on this track")
			return
		}
	}
	if err := s.store.UpdateTrack(ctx, t); err != nil {
		s.fail(w, err, "save track")
		return
	}
	if in.TakeID != nil {
		if err := s.store.PickTake(ctx, uid, *in.TakeID); err != nil {
			s.fail(w, err, "take")
			return
		}
	}
	out, _ := s.store.GetTrack(ctx, uid, t.ID)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteTrack(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	files, err := s.store.DeleteTrack(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "delete track")
		return
	}
	s.dropMedia(ctx, files...)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- takes ----

// audioMeta is what the browser knows about a recording or a file it decoded.
type audioMeta struct {
	Name       string `json:"name"`
	DurationMS int64  `json:"duration_ms"`
	OffsetMS   int64  `json:"offset_ms"`
	LatencyMS  int64  `json:"latency_ms"`
	SampleRate int    `json:"sample_rate"`
	Channels   int    `json:"channels"`
	Peaks      string `json:"peaks"`
	Source     string `json:"source"`
	SourceRef  string `json:"source_ref"`
	Device     string `json:"device"`
	Note       string `json:"note"`
	Tracks     int    `json:"tracks"`
	ClientID   string `json:"client_id"`  // ideas: the device's own id
	CreatedAt  string `json:"created_at"` // ideas: when it was recorded
	BandID     *int64 `json:"band_id"`    // ideas: the band it is in (0 or null = none)
	Part       string `json:"part"`       // takes: auto (default for recordings) | new (default for files) | only | join
	PartID     int64  `json:"part_id"`    // takes: the part to join
}

func (m *audioMeta) check() string {
	switch {
	case m.DurationMS <= 0 || m.DurationMS > maxLengthMS:
		return "a recording is up to three hours long"
	case m.OffsetMS < -maxLengthMS || m.OffsetMS > maxLengthMS:
		return "the take starts too far from the song's start"
	case m.LatencyMS < 0 || m.LatencyMS > 2000:
		return "the delay is 0 to 2000 ms"
	case m.SampleRate < 0 || m.SampleRate > 384000 || m.Channels < 0 || m.Channels > 8:
		return "unusual audio format"
	case len(m.Peaks) > maxPeaksBytes:
		return "the waveform is too long"
	}
	if m.Peaks != "" {
		if _, err := base64.StdEncoding.DecodeString(m.Peaks); err != nil {
			return "the waveform is not base64"
		}
	}
	if m.Source == "" {
		m.Source = "recorded"
	}
	if !oneOf(m.Source, []string{"recorded", "upload", "files", "music"}) {
		return "unknown source"
	}
	if m.Channels == 0 {
		m.Channels = 1
	}
	m.SourceRef, m.Device, m.Note, m.Name = clip(m.SourceRef, 300), cleanText(m.Device, 120), clip(m.Note, maxLine), cleanText(m.Name, maxName)
	return ""
}

// readAudioUpload reads a multipart upload: "meta" (JSON) and "file".
func (s *Server) readAudioUpload(w http.ResponseWriter, r *http.Request, uid int64) (*audioMeta, string, string, int64, bool) {
	return s.readAudioUploadUnless(w, r, uid, nil)
}

// readAudioUploadUnless is readAudioUpload, except that when skip says so
// for the meta that came before the file, the file is not kept: it returns
// the meta with no file name (and ok).
func (s *Server) readAudioUploadUnless(w http.ResponseWriter, r *http.Request, uid int64, skip func(*audioMeta) bool) (*audioMeta, string, string, int64, bool) {
	mr, err := r.MultipartReader()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "send the audio as multipart/form-data (meta, file)")
		return nil, "", "", 0, false
	}
	var meta *audioMeta
	var rel, mime string
	var size int64
	fail := func(code int, msg string) (*audioMeta, string, string, int64, bool) {
		if rel != "" {
			_ = os.Remove(s.mediaPath(rel))
		}
		writeErr(w, code, msg)
		return nil, "", "", 0, false
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fail(http.StatusBadRequest, "the upload broke off: "+err.Error())
		}
		switch part.FormName() {
		case "meta":
			b, _ := io.ReadAll(io.LimitReader(part, 4<<20))
			meta = &audioMeta{}
			if err := json.Unmarshal(b, meta); err != nil {
				return fail(http.StatusBadRequest, "meta is not JSON")
			}
		case "file":
			if rel != "" {
				continue
			}
			if meta != nil && skip != nil && skip(meta) {
				_, _ = io.Copy(io.Discard, part)
				part.Close()
				return meta, "", "", 0, true
			}
			mime = part.Header.Get("Content-Type")
			ext := strings.ToLower(filepath.Ext(part.FileName()))
			if !isAudioExt(ext) && !isVideoExt(ext) {
				ext = extForMime(mime)
			}
			if ext == "" {
				return fail(http.StatusBadRequest, "that is not an audio file (WAV, MP3, M4A, AAC, FLAC, OGG, WebM)")
			}
			if m := mimeByExt("x" + ext); m != "" && (mime == "" || mime == "application/octet-stream" || strings.HasPrefix(mime, "video/")) {
				mime = m
			}
			rel, size, err = s.saveMedia(uid, ext, part, maxAudioBytes)
			if err != nil {
				rel = ""
				if ue, ok := err.(*userErr); ok {
					return fail(http.StatusBadRequest, ue.msg)
				}
				return fail(http.StatusInternalServerError, "save audio: "+err.Error())
			}
		}
		part.Close()
	}
	if meta == nil {
		meta = &audioMeta{}
	}
	if rel == "" {
		return fail(http.StatusBadRequest, "no audio file came with it")
	}
	if !looksLikeAudio(s.mediaPath(rel)) {
		return fail(http.StatusBadRequest, "that is not an audio file (WAV, MP3, M4A, AAC, FLAC, OGG, WebM)")
	}
	if strings.HasSuffix(rel, ".wav") {
		if d, sr, ch, ok := wavInfo(s.mediaPath(rel)); ok {
			if meta.DurationMS == 0 {
				meta.DurationMS = d
			}
			if meta.SampleRate == 0 {
				meta.SampleRate, meta.Channels = sr, ch
			}
		}
	}
	if m := meta.check(); m != "" {
		return fail(http.StatusBadRequest, m)
	}
	return meta, rel, firstNonEmpty(strings.SplitN(mime, ";", 2)[0], "audio/wav"), size, true
}

// handleCreateTake stores a recording (or a file) on a track; it becomes the
// take that plays.
func (s *Server) handleCreateTake(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	t, err := s.store.GetTrack(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "track")
		return
	}
	meta, rel, mime, size, ok := s.readAudioUpload(w, r, uid)
	if !ok {
		return
	}
	k := &store.Take{UserID: uid, SongID: t.SongID, TrackID: t.ID, File: rel, Mime: mime, Size: size, DurationMS: meta.DurationMS, OffsetMS: meta.OffsetMS,
		LatencyMS: meta.LatencyMS, SampleRate: meta.SampleRate, Channels: meta.Channels, Peaks: meta.Peaks, Source: meta.Source, SourceRef: meta.SourceRef, Device: meta.Device, Note: meta.Note}
	mode := meta.Part
	switch mode {
	case store.PartNew, store.PartOnly, store.PartJoin, store.PartAuto:
	default:
		mode = store.PartNew // a file brought in is a part of its own
		if meta.Source == "recorded" {
			mode = store.PartAuto // recorded over a part: another attempt at it
		}
	}
	if err := s.store.CreateTake(ctx, k, mode, meta.PartID); err != nil {
		_ = os.Remove(s.mediaPath(rel))
		s.fail(w, err, "save take")
		return
	}
	if meta.Source == "recorded" {
		if so, err := s.store.GetSong(ctx, uid, t.SongID); err == nil {
			s.addTime(ctx, uid, so, 0, 1)
			s.publish(ctx, uid, "jam.take_recorded", fmt.Sprintf("take:%d", k.ID), map[string]any{"song_id": so.ID, "song": so.Title, "track": t.Name, "take": k.Number, "duration_ms": k.DurationMS, "url": s.link(fmt.Sprintf("/songs/%d", so.ID))})
		}
	}
	out, _ := s.store.GetTake(ctx, uid, k.ID)
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleTakeAudio(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	k, err := s.store.GetTake(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "take")
		return
	}
	name := ""
	if queryBool(r, "download") {
		name = fmt.Sprintf("take-%d%s", k.Number, filepath.Ext(k.File))
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
		name = ""
	}
	s.serveMediaFile(w, r, k.File, k.Mime, name)
}

func (s *Server) handleUpdateTake(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	k, err := s.store.GetTake(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "take")
		return
	}
	var in struct {
		OffsetMS *int64  `json:"offset_ms"`
		Note     *string `json:"note"`
		Liked    *bool   `json:"liked"`
		TrackID  *int64  `json:"track_id"`
		Plays    *bool   `json:"plays"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if in.TrackID != nil && *in.TrackID != k.TrackID {
		// onto another track: the whole part goes (older clients); offset_ms then shifts the part
		var shift int64
		if in.OffsetMS != nil {
			shift = *in.OffsetMS - k.OffsetMS
			in.OffsetMS = nil
		}
		if _, err := s.store.MovePart(ctx, uid, k.Part, store.PartMove{TrackID: *in.TrackID, ShiftMS: shift}); err != nil {
			s.fail(w, err, "track")
			return
		}
		if k, err = s.store.GetTake(ctx, uid, k.ID); err != nil {
			s.fail(w, err, "take")
			return
		}
	}
	if in.Plays != nil && *in.Plays {
		if err := s.store.PickTake(ctx, uid, k.ID); err != nil {
			s.fail(w, err, "take")
			return
		}
		k.Plays = true
	}
	if in.OffsetMS != nil {
		if *in.OffsetMS < -maxLengthMS || *in.OffsetMS > maxLengthMS {
			writeErr(w, http.StatusBadRequest, "the take starts too far from the song's start")
			return
		}
		k.OffsetMS = *in.OffsetMS
	}
	if in.Note != nil {
		k.Note = clip(*in.Note, maxLine)
	}
	if in.Liked != nil {
		k.Liked = *in.Liked
	}
	if err := s.store.UpdateTake(ctx, k); err != nil {
		s.fail(w, err, "save take")
		return
	}
	writeJSON(w, http.StatusOK, k)
}

// handleMovePart moves a part — every attempt at it together — later or earlier
// (shift_ms) and/or onto another track of the song; it lands on top (or at layer).
func (s *Server) handleMovePart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		TrackID int64 `json:"track_id"`
		ShiftMS int64 `json:"shift_ms"`
		Layer   int   `json:"layer"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if in.ShiftMS < -maxLengthMS || in.ShiftMS > maxLengthMS {
		writeErr(w, http.StatusBadRequest, "that is too far to move it")
		return
	}
	takes, err := s.store.MovePart(ctx, actorID(ctx), idParam(r, "id"), store.PartMove{TrackID: in.TrackID, ShiftMS: in.ShiftMS, Layer: in.Layer})
	if err != nil {
		s.fail(w, err, "part")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"takes": takes})
}

func (s *Server) handleDeleteTake(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	file, err := s.store.DeleteTake(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "delete take")
		return
	}
	s.dropMedia(ctx, file)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleStems is every track's playing take in one .zip, with song.json
// saying where each starts — for Pocket Studio or any other recording app.
func (s *Server) handleStems(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	so, err := s.store.SongWithTracks(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "song")
		return
	}
	type stem struct {
		File       string `json:"file"`
		Track      string `json:"track"`
		Instrument string `json:"instrument"`
		OffsetMS   int64  `json:"offset_ms"`
		DurationMS int64  `json:"duration_ms"`
		Volume     int    `json:"volume"`
		Pan        int    `json:"pan"`
		Muted      bool   `json:"muted"`
		Layer      int    `json:"layer"`
	}
	stems := []stem{}
	type fileOut struct{ name, path string }
	files := []fileOut{}
	for i, t := range so.Tracks {
		n, parts := 0, 0
		for _, k := range t.Takes {
			if k.Plays {
				parts++
			}
		}
		for _, k := range t.Takes {
			if !k.Plays {
				continue
			}
			n++
			name := fmt.Sprintf("%02d %s%s", i+1, fileSafe(t.Name), filepath.Ext(k.File))
			if parts > 1 {
				name = fmt.Sprintf("%02d %s - part %d%s", i+1, fileSafe(t.Name), n, filepath.Ext(k.File))
			}
			stems = append(stems, stem{File: name, Track: t.Name, Instrument: t.Instrument, OffsetMS: k.OffsetMS, DurationMS: k.DurationMS, Volume: t.Volume, Pan: t.Pan, Muted: t.Muted, Layer: k.Layer})
			files = append(files, fileOut{name, s.mediaPath(k.File)})
		}
	}
	if len(files) == 0 {
		writeErr(w, http.StatusBadRequest, "nothing is recorded in this song yet")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, strings.ReplaceAll(fileSafe(so.Title), `"`, "")))
	zw := zip.NewWriter(w)
	if f, err := zw.Create("song.json"); err == nil {
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{"app": "Pocket Jam", "title": so.Title, "artist": so.Artist, "tempo": so.Tempo, "beats": so.Beats, "beat_unit": so.BeatUnit, "key": so.KeyName,
			"note": "Each file starts at offset_ms from the start of the song (a negative offset: cut that much from the file's start). A track can have several parts; where two overlap, the one with the higher layer is heard.", "stems": stems})
	}
	for _, f := range files {
		in, err := os.Open(f.path)
		if err != nil {
			continue
		}
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: f.name, Method: zip.Store, Modified: time.Now()})
		if err == nil {
			_, _ = io.Copy(fw, in)
		}
		in.Close()
	}
	_ = zw.Close()
}

// ---- mixes ----

func (s *Server) handleMixes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.store.ListMixes(ctx, actorID(ctx), int64(queryInt(r, "song_id")), queryInt(r, "limit"))
	if err != nil {
		s.fail(w, err, "mixes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mixes": list})
}

// handleCreateMix stores the stereo file the browser mixed from a song.
func (s *Server) handleCreateMix(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	so, err := s.store.GetSong(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "song")
		return
	}
	meta, rel, mime, size, ok := s.readAudioUpload(w, r, uid)
	if !ok {
		return
	}
	n := 1
	if old, err := s.store.ListMixes(ctx, uid, so.ID, 500); err == nil {
		n = len(old) + 1
	}
	m := &store.Mix{UserID: uid, SongID: &so.ID, Name: firstNonEmpty(meta.Name, fmt.Sprintf("%s — mix %d", so.Title, n)), File: rel, Mime: mime, Size: size, DurationMS: meta.DurationMS, Peaks: meta.Peaks, Tracks: clampInt(meta.Tracks, 0, maxTracks)}
	if err := s.store.CreateMix(ctx, m); err != nil {
		_ = os.Remove(s.mediaPath(rel))
		s.fail(w, err, "save mix")
		return
	}
	out, _ := s.store.GetMix(ctx, uid, m.ID)
	s.publish(ctx, uid, "jam.mix_made", fmt.Sprintf("mix:%d", m.ID), map[string]any{"id": m.ID, "name": m.Name, "song_id": so.ID, "song": so.Title, "duration_ms": m.DurationMS, "url": s.link("/mixes")})
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleMixAudio(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	m, err := s.store.GetMix(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "mix")
		return
	}
	if queryBool(r, "download") {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s%s"`, strings.ReplaceAll(fileSafe(m.Name), `"`, ""), filepath.Ext(m.File)))
	}
	s.serveMediaFile(w, r, m.File, m.Mime, "")
}

func (s *Server) handleUpdateMix(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	m, err := s.store.GetMix(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "mix")
		return
	}
	var in struct {
		Name *string `json:"name"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if in.Name != nil {
		if m.Name = cleanText(*in.Name, maxName); m.Name == "" {
			writeErr(w, http.StatusBadRequest, "give the mix a name")
			return
		}
	}
	if err := s.store.UpdateMix(ctx, m); err != nil {
		s.fail(w, err, "save mix")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleDeleteMix(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	file, err := s.store.DeleteMix(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "delete mix")
		return
	}
	s.dropMedia(ctx, file)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- time spent ----

// handleAddTime is the song page's minute tick while someone plays or records.
func (s *Server) handleAddTime(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		SongID  int64  `json:"song_id"`
		Title   string `json:"title"`
		Seconds int    `json:"seconds"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if in.Seconds <= 0 || in.Seconds > 180 {
		writeErr(w, http.StatusBadRequest, "send the time in steps of up to three minutes")
		return
	}
	var so *store.Song
	if in.SongID > 0 {
		var err error
		if so, err = s.store.GetSong(ctx, uid, in.SongID); err != nil {
			s.fail(w, err, "song")
			return
		}
	} else {
		so = &store.Song{Title: firstNonEmpty(cleanText(in.Title, maxName), "Practice")}
	}
	x := s.addTime(ctx, uid, so, in.Seconds, 0)
	writeJSON(w, http.StatusOK, map[string]any{"sitting": x})
}

// addTime books time (and takes) on a song and sends it on as practice.
func (s *Server) addTime(ctx context.Context, uid int64, so *store.Song, seconds, takes int) *store.Sitting {
	var id *int64
	if so.ID > 0 {
		id = &so.ID
	}
	x, err := s.store.AddTime(ctx, uid, id, so.Title, seconds, takes, time.Now())
	if err != nil {
		s.log.Warn("time spent", "err", err)
		return nil
	}
	if x.Seconds-x.SentSeconds >= 60 {
		go s.sendPractice(ctxDetached(ctx), uid, *x)
	}
	return x
}

// handleTime is the time spent per day and the sittings between two days.
func (s *Server) handleTime(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	q := r.URL.Query()
	to := firstNonEmpty(q.Get("to"), today())
	from := firstNonEmpty(q.Get("from"), store.AddDays(to, -27))
	if !validDate(from) || !validDate(to) || from > to {
		writeErr(w, http.StatusBadRequest, "from and to are YYYY-MM-DD")
		return
	}
	days, err := s.store.MinutesPerDay(ctx, uid, from, to)
	if err != nil {
		s.fail(w, err, "time")
		return
	}
	sittings, err := s.store.ListSittings(ctx, uid, from, to)
	if err != nil {
		s.fail(w, err, "time")
		return
	}
	total := 0
	for _, d := range days {
		total += d.Minutes
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "days": days, "sittings": sittings, "minutes": total, "streak": s.streak(ctx, uid)})
}

// streak is the number of days in a row, up to today (or yesterday), with time played.
func (s *Server) streak(ctx context.Context, uid int64) int {
	t := today()
	days, err := s.store.MinutesPerDay(ctx, uid, store.AddDays(t, -400), t)
	if err != nil || len(days) == 0 {
		return 0
	}
	have := map[string]bool{}
	for _, d := range days {
		if d.Minutes > 0 || d.Takes > 0 {
			have[d.Day] = true
		}
	}
	d := t
	if !have[d] {
		d = store.AddDays(t, -1)
	}
	n := 0
	for have[d] {
		n++
		d = store.AddDays(d, -1)
	}
	return n
}

// ---- WAV ----

// wavInfo reads a WAV file's length, sample rate and channels from its header.
func wavInfo(path string) (ms int64, rate, channels int, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, 0, false
	}
	defer f.Close()
	hdr := make([]byte, 12)
	if _, err := io.ReadFull(f, hdr); err != nil || string(hdr[0:4]) != "RIFF" || string(hdr[8:12]) != "WAVE" {
		return 0, 0, 0, false
	}
	var byteRate int
	for i := 0; i < 64; i++ {
		ch := make([]byte, 8)
		if _, err := io.ReadFull(f, ch); err != nil {
			return 0, 0, 0, false
		}
		id, size := string(ch[0:4]), int64(le32(ch[4:8]))
		switch id {
		case "fmt ":
			b := make([]byte, size)
			if _, err := io.ReadFull(f, b); err != nil || len(b) < 16 {
				return 0, 0, 0, false
			}
			channels = int(b[2]) | int(b[3])<<8
			rate = int(le32(b[4:8]))
			byteRate = int(le32(b[8:12]))
			if size%2 == 1 {
				_, _ = f.Seek(1, io.SeekCurrent)
			}
		case "data":
			if byteRate == 0 {
				return 0, 0, 0, false
			}
			if fi, err := f.Stat(); err == nil {
				if pos, _ := f.Seek(0, io.SeekCurrent); size <= 0 || size > fi.Size()-pos {
					size = fi.Size() - pos
				}
			}
			return size * 1000 / int64(byteRate), rate, channels, true
		default:
			if _, err := f.Seek(size+size%2, io.SeekCurrent); err != nil {
				return 0, 0, 0, false
			}
		}
	}
	return 0, 0, 0, false
}

// looksLikeAudio checks the first bytes for a known audio container.
func looksLikeAudio(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	b := make([]byte, 12)
	n, _ := io.ReadFull(f, b)
	b = b[:n]
	has := func(at int, sig string) bool { return len(b) >= at+len(sig) && string(b[at:at+len(sig)]) == sig }
	switch {
	case has(0, "RIFF") && has(8, "WAVE"), has(0, "ID3"), has(0, "fLaC"), has(0, "OggS"), has(4, "ftyp"), has(0, "\x1a\x45\xdf\xa3"),
		has(0, "FORM"), has(0, "caff"):
		return true
	case len(b) >= 2 && b[0] == 0xFF && b[1]&0xE0 == 0xE0: // MPEG audio / ADTS AAC frame
		return true
	}
	return false
}

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
