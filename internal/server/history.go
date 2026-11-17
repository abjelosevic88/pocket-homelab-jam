package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/abjelosevic88/pocket-homelab-jam/internal/store"
)

// ---- a song's history ----
//
// Every change to a song writes one line. A run of the same change (a fader
// moved again and again, a note nudged twice) stays one line saying where it
// started, and a run that ends where it started leaves no line at all. A page
// can send X-Jam-Group (an id) and X-Jam-Group-Label (the words, URL-encoded)
// to make a job of many calls (cutting the song to a part) one line.

const runWindow = 3 * time.Minute

type groupKey struct{}

type group struct {
	id, label string
	kind      string // group (a page's job), undo, redo
	ref       int64  // undo/redo: the line it undid
}

// withGroup keeps the request's X-Jam-Group headers on its context.
func withGroup(r *http.Request) *http.Request {
	id := strings.TrimSpace(r.Header.Get("X-Jam-Group"))
	if id == "" || len(id) > 80 {
		return r
	}
	label, _ := url.QueryUnescape(r.Header.Get("X-Jam-Group-Label"))
	label = cleanText(label, 200)
	if label == "" {
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), groupKey{}, group{id: id, label: label, kind: "group"}))
}

// change is one line to write. With key, textFrom words it from where the run started (old) to now.
type change struct {
	kind     string
	text     string
	key      string
	old, now string
	textFrom func(old string) string
	undo     []undoOp // what puts it back; none = can't be undone
}

// with is c with what undoes it.
func (c change) with(ops ...undoOp) change {
	for _, o := range ops {
		if !o.empty() {
			c.undo = append(c.undo, o)
		}
	}
	return c
}

// note writes a change into the song's history. It never fails the request.
func (s *Server) note(ctx context.Context, uid, songID int64, c change) {
	if songID == 0 || c.text == "" {
		return
	}
	via := linkApp(ctx)
	if g, ok := ctx.Value(groupKey{}).(group); ok {
		// a job of many changes: one line, undone newest first
		key := "group:" + g.id
		if last, _ := s.store.SongEventByKey(ctx, uid, songID, key); last != nil {
			undo := ""
			if last.Undo != "" && len(c.undo) > 0 {
				var was []undoOp
				_ = json.Unmarshal([]byte(last.Undo), &was)
				undo = encodeOps(append(append([]undoOp{}, c.undo...), was...))
			}
			_ = s.store.RegroupSongEvent(ctx, last.ID, g.label, undo)
			return
		}
		_ = s.store.AddSongEvent(ctx, &store.SongEvent{UserID: uid, SongID: songID, Kind: g.kind, Text: g.label, Key: key, Via: via, Ref: g.ref, Undo: encodeOps(c.undo)})
		return
	}
	if c.key != "" {
		if last, _ := s.store.LastSongEvent(ctx, uid, songID); last != nil && last.Key == c.key && recent(last.At) {
			if last.Old == c.now {
				_ = s.store.DropSongEvent(ctx, last.ID) // back where it started: nothing changed
				return
			}
			text := c.text
			if c.textFrom != nil {
				text = c.textFrom(last.Old)
			}
			_ = s.store.RewriteSongEvent(ctx, last.ID, text)
			return
		}
	}
	_ = s.store.AddSongEvent(ctx, &store.SongEvent{UserID: uid, SongID: songID, Kind: c.kind, Text: c.text, Key: c.key, Old: c.old, Via: via, Undo: encodeOps(c.undo)})
}

func recent(at string) bool {
	t, err := time.Parse("2006-01-02T15:04:05.000Z", at)
	return err == nil && time.Since(t) < runWindow
}

// fromTo is a change of one value: "what old → new", kept as one line while it goes on.
func fromTo(kind, key, what, old, now string, show func(string) string) change {
	if show == nil {
		show = func(v string) string { return v }
	}
	words := func(o string) string { return fmt.Sprintf("%s %s → %s", what, show(o), show(now)) }
	return change{kind: kind, key: key, old: old, now: now, text: words(old), textFrom: words}
}

// toggle is an on/off change: a second flip back leaves no line.
func toggle(kind, key string, on bool, onText, offText string) change {
	t := offText
	if on {
		t = onText
	}
	return change{kind: kind, key: key, old: strconv.FormatBool(!on), now: strconv.FormatBool(on), text: t}
}

// clockText is a time in a song: 0:03.0, 1:12.4.
func clockText(ms int64) string {
	neg := ""
	if ms < 0 {
		neg, ms = "-", -ms
	}
	return fmt.Sprintf("%s%d:%04.1f", neg, ms/60000, float64(ms%60000)/1000)
}

func quoteText(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > n {
		r := []rune(s)
		s = strings.TrimSpace(string(r[:n])) + "…"
	}
	return "“" + s + "”"
}

func panText(v string) string {
	n, _ := strconv.Atoi(v)
	switch {
	case n == 0:
		return "middle"
	case n < 0:
		return fmt.Sprintf("%d %% left", -n)
	}
	return fmt.Sprintf("%d %% right", n)
}

func countInText(v string) string {
	switch v {
	case "0":
		return "none"
	case "1":
		return "1 bar"
	}
	return v + " bars"
}

func orNone(v string) string {
	if v == "" {
		return "none"
	}
	return "“" + v + "”"
}


// handleHistory is a song's history, newest first (?before=<id> for older lines).
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	so, err := s.store.GetSong(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "song")
		return
	}
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	limit := 100
	if n, _ := strconv.Atoi(r.URL.Query().Get("limit")); n > 0 && n <= 500 {
		limit = n
	}
	events, err := s.store.SongEvents(ctx, uid, so.ID, before, limit+1)
	if err != nil {
		s.fail(w, err, "history")
		return
	}
	more := len(events) > limit
	if more {
		events = events[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "more": more})
}

// ---- what changed, in words ----

// songChanges notes what changed in a song's details.
func (s *Server) songChanges(ctx context.Context, uid int64, a, b *store.Song) {
	id, sid := b.ID, itoa(b.ID)
	if a.Title != b.Title {
		s.note(ctx, uid, id, fromTo("song", "song:"+sid+":title", "Renamed the song", a.Title, b.Title, func(v string) string { return "“" + v + "”" }).with(call("PUT", "/songs/"+sid, map[string]any{"title": a.Title})))
	}
	if a.Artist != b.Artist {
		s.note(ctx, uid, id, fromTo("song", "song:"+sid+":artist", "Artist", a.Artist, b.Artist, orNone).with(call("PUT", "/songs/"+sid, map[string]any{"artist": a.Artist})))
	}
	if a.Notes != b.Notes {
		s.note(ctx, uid, id, change{kind: "song", key: "song:" + sid + ":notes", text: "Changed the song's notes", old: a.Notes, now: b.Notes, textFrom: func(string) string { return "Changed the song's notes" }}.with(call("PUT", "/songs/"+sid, map[string]any{"notes": a.Notes})))
	}
	if a.Tempo != b.Tempo {
		f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
		s.note(ctx, uid, id, fromTo("song", "song:"+sid+":tempo", "Tempo", f(a.Tempo), f(b.Tempo), func(v string) string { return v + " BPM" }).with(call("PUT", "/songs/"+sid, map[string]any{"tempo": a.Tempo})))
	}
	if a.Beats != b.Beats || a.BeatUnit != b.BeatUnit {
		s.note(ctx, uid, id, fromTo("song", "song:"+sid+":meter", "Time signature", fmt.Sprintf("%d/%d", a.Beats, a.BeatUnit), fmt.Sprintf("%d/%d", b.Beats, b.BeatUnit), nil).with(call("PUT", "/songs/"+sid, map[string]any{"beats": a.Beats, "beat_unit": a.BeatUnit})))
	}
	if a.KeyName != b.KeyName {
		s.note(ctx, uid, id, fromTo("song", "song:"+sid+":key", "Key", a.KeyName, b.KeyName, func(v string) string {
			if v == "" {
				return "none"
			}
			return v
		}).with(call("PUT", "/songs/"+sid, map[string]any{"key_name": a.KeyName})))
	}
	if a.CountIn != b.CountIn {
		s.note(ctx, uid, id, fromTo("song", "song:"+sid+":countin", "Count-in", strconv.Itoa(a.CountIn), strconv.Itoa(b.CountIn), countInText).with(call("PUT", "/songs/"+sid, map[string]any{"count_in": a.CountIn})))
	}
	if a.Click != b.Click {
		s.note(ctx, uid, id, toggle("song", "song:"+sid+":click", b.Click, "Turned the click on", "Turned the click off").with(call("PUT", "/songs/"+sid, map[string]any{"click": a.Click})))
	}
	if a.LoopStartMS != b.LoopStartMS || a.LoopEndMS != b.LoopEndMS {
		loop := func(v string) string {
			var x, y int64
			fmt.Sscanf(v, "%d-%d", &x, &y)
			if y <= 0 {
				return "off"
			}
			return clockText(x) + "–" + clockText(y)
		}
		s.note(ctx, uid, id, fromTo("song", "song:"+sid+":loop", "Loop", fmt.Sprintf("%d-%d", a.LoopStartMS, a.LoopEndMS), fmt.Sprintf("%d-%d", b.LoopStartMS, b.LoopEndMS), loop).with(call("PUT", "/songs/"+sid, map[string]any{"loop_start_ms": a.LoopStartMS, "loop_end_ms": a.LoopEndMS})))
	}
	if a.Starred != b.Starred {
		s.note(ctx, uid, id, toggle("song", "song:"+sid+":star", b.Starred, "Starred the song", "Took the star off").with(call("PUT", "/songs/"+sid, map[string]any{"starred": a.Starred})))
	}
	if a.Color != b.Color {
		s.note(ctx, uid, id, change{kind: "song", key: "song:" + sid + ":color", old: a.Color, now: b.Color, text: "Changed the song's colour"}.with(call("PUT", "/songs/"+sid, map[string]any{"color": a.Color})))
	}
	ab, bb := int64(0), int64(0)
	if a.BandID != nil {
		ab = *a.BandID
	}
	if b.BandID != nil {
		bb = *b.BandID
	}
	if ab != bb {
		text := "Took the song out of its band"
		if bb != 0 {
			name := ""
			if band, err := s.store.GetBand(ctx, uid, bb); err == nil {
				name = band.Name
			}
			text = "Put the song in the band " + orNone(name)
		}
		s.note(ctx, uid, id, change{kind: "song", key: "song:" + sid + ":band", old: itoa(ab), now: itoa(bb), text: text}.with(call("PUT", "/songs/"+sid, map[string]any{"band_id": ab})))
	}
}

// trackChanges notes what changed on a track (name, sound, mixer).
func (s *Server) trackChanges(ctx context.Context, uid int64, a, b *store.Track) {
	id, tid := b.SongID, "track:"+itoa(b.ID)
	if a.Name != b.Name {
		s.note(ctx, uid, id, fromTo("track", tid+":name", "Renamed the track", a.Name, b.Name, func(v string) string { return "“" + v + "”" }).with(call("PUT", "/tracks/"+itoa(b.ID), map[string]any{"name": a.Name})))
	}
	if a.Instrument != b.Instrument {
		s.note(ctx, uid, id, fromTo("track", tid+":instrument", b.Name+": instrument", instrumentName(a.Instrument), instrumentName(b.Instrument), nil).with(call("PUT", "/tracks/"+itoa(b.ID), map[string]any{"instrument": a.Instrument})))
	}
	if a.Color != b.Color {
		s.note(ctx, uid, id, change{kind: "track", key: tid + ":color", old: a.Color, now: b.Color, text: "Changed the colour of " + b.Name}.with(call("PUT", "/tracks/"+itoa(b.ID), map[string]any{"color": a.Color})))
	}
	if a.Volume != b.Volume {
		s.note(ctx, uid, id, fromTo("mixer", tid+":volume", b.Name+" volume", strconv.Itoa(a.Volume), strconv.Itoa(b.Volume), func(v string) string { return v + " %" }).with(call("PUT", "/tracks/"+itoa(b.ID), map[string]any{"volume": a.Volume})))
	}
	if a.Pan != b.Pan {
		s.note(ctx, uid, id, fromTo("mixer", tid+":pan", b.Name+" balance", strconv.Itoa(a.Pan), strconv.Itoa(b.Pan), panText).with(call("PUT", "/tracks/"+itoa(b.ID), map[string]any{"pan": a.Pan})))
	}
	if a.Muted != b.Muted {
		s.note(ctx, uid, id, toggle("mixer", tid+":mute", b.Muted, "Muted "+b.Name, "Unmuted "+b.Name).with(call("PUT", "/tracks/"+itoa(b.ID), map[string]any{"muted": a.Muted})))
	}
	if a.Solo != b.Solo {
		s.note(ctx, uid, id, toggle("mixer", tid+":solo", b.Solo, "Soloed "+b.Name, "Stopped soloing "+b.Name).with(call("PUT", "/tracks/"+itoa(b.ID), map[string]any{"solo": a.Solo})))
	}
}

func (s *Server) trackName(ctx context.Context, uid, id int64) string {
	if t, err := s.store.GetTrack(ctx, uid, id); err == nil {
		return t.Name
	}
	return "a track"
}

// playingOf is the attempt that plays in a part (or its newest).
func playingOf(ks []store.Take) *store.Take {
	if len(ks) == 0 {
		return nil
	}
	for i := range ks {
		if ks[i].Plays {
			return &ks[i]
		}
	}
	return &ks[len(ks)-1]
}

// partMoved notes a part moved in time and/or to another track.
func (s *Server) partMoved(ctx context.Context, uid int64, before, after []store.Take) {
	a, b := playingOf(before), playingOf(after)
	if a == nil || b == nil || (a.TrackID == b.TrackID && a.OffsetMS == b.OffsetMS) {
		return
	}
	from := s.trackName(ctx, uid, a.TrackID)
	var text string
	if a.TrackID == b.TrackID {
		text = fmt.Sprintf("Moved a part on %s from %s to %s", from, clockText(a.OffsetMS), clockText(b.OffsetMS))
	} else {
		to := s.trackName(ctx, uid, b.TrackID)
		text = fmt.Sprintf("Moved a part from %s to %s", from, to)
		if a.OffsetMS != b.OffsetMS {
			text += fmt.Sprintf(" (%s → %s)", clockText(a.OffsetMS), clockText(b.OffsetMS))
		} else {
			text += " (at " + clockText(b.OffsetMS) + ")"
		}
	}
	back := make([]store.TakeState, len(before))
	for i, k := range before {
		back[i] = store.TakeState{ID: k.ID, TrackID: k.TrackID, Number: k.Number, OffsetMS: k.OffsetMS, Part: k.Part, Plays: k.Plays, Layer: k.Layer}
	}
	s.note(ctx, uid, b.SongID, change{kind: "part", text: text}.with(takesBack(back)))
}

func markWhere(m *store.Mark) string {
	if m.EndMS > m.AtMS {
		return clockText(m.AtMS) + "–" + clockText(m.EndMS)
	}
	return clockText(m.AtMS)
}

// takeChanges notes what changed on a take (picked, lined up, hearted, its note).
func (s *Server) takeChanges(ctx context.Context, uid int64, a, b *store.Take, before []store.TakeState) {
	if a.TrackID != b.TrackID {
		return // a move: noted by partMoved
	}
	name := s.trackName(ctx, uid, b.TrackID)
	kid := "take:" + itoa(b.ID)
	if b.Plays && !a.Plays {
		s.note(ctx, uid, b.SongID, change{kind: "take", text: fmt.Sprintf("%s: take %d plays now", name, b.Number)}.with(takesBack(before)))
	}
	if a.OffsetMS != b.OffsetMS {
		words := func(old string) string {
			o, _ := strconv.ParseInt(old, 10, 64)
			d := b.OffsetMS - o
			if d > -1000 && d < 1000 {
				return fmt.Sprintf("Lined up take %d on %s by %+d ms", b.Number, name, d)
			}
			return fmt.Sprintf("Moved take %d on %s from %s to %s", b.Number, name, clockText(o), clockText(b.OffsetMS))
		}
		old := itoa(a.OffsetMS)
		s.note(ctx, uid, b.SongID, change{kind: "take", key: kid + ":offset", old: old, now: itoa(b.OffsetMS), text: words(old), textFrom: words}.with(call("PUT", "/takes/"+itoa(b.ID), map[string]any{"offset_ms": a.OffsetMS})))
	}
	if a.Liked != b.Liked {
		s.note(ctx, uid, b.SongID, toggle("take", kid+":liked", b.Liked, fmt.Sprintf("Hearted take %d on %s", b.Number, name), fmt.Sprintf("Took the heart off take %d on %s", b.Number, name)).with(call("PUT", "/takes/"+itoa(b.ID), map[string]any{"liked": a.Liked})))
	}
	if a.Note != b.Note {
		s.note(ctx, uid, b.SongID, change{kind: "take", key: kid + ":note", old: a.Note, now: b.Note, text: fmt.Sprintf("Changed the note on take %d on %s", b.Number, name)}.with(call("PUT", "/takes/"+itoa(b.ID), map[string]any{"note": a.Note})))
	}
}

// markChanges notes an edited note on the timeline.
func (s *Server) markChanges(ctx context.Context, uid int64, a, b *store.Mark) {
	mid := "mark:" + itoa(b.ID)
	mp := "/marks/" + itoa(b.ID)
	if a.Text != b.Text {
		s.note(ctx, uid, b.SongID, change{kind: "note", key: mid + ":text", old: a.Text, now: b.Text, text: fmt.Sprintf("Edited the note at %s: %s", markWhere(b), quoteText(b.Text, 80))}.with(call("PUT", mp, map[string]any{"text": a.Text})))
	}
	if a.AtMS != b.AtMS || a.EndMS != b.EndMS {
		where := func(m *store.Mark) string { return fmt.Sprintf("%d-%d", m.AtMS, m.EndMS) }
		words := func(old string) string {
			var x, y int64
			fmt.Sscanf(old, "%d-%d", &x, &y)
			return fmt.Sprintf("Moved the note %s from %s to %s", quoteText(b.Text, 40), markWhere(&store.Mark{AtMS: x, EndMS: y}), markWhere(b))
		}
		s.note(ctx, uid, b.SongID, change{kind: "note", key: mid + ":at", old: where(a), now: where(b), text: words(where(a)), textFrom: words}.with(call("PUT", mp, map[string]any{"at_ms": a.AtMS, "end_ms": a.EndMS})))
	}
	if a.Color != b.Color || !sameID(a.TrackID, b.TrackID) {
		s.note(ctx, uid, b.SongID, change{kind: "note", key: mid + ":look", text: "Changed the note " + quoteText(b.Text, 40)}.with(call("PUT", mp, map[string]any{"color": a.Color, "track_id": markTrack(a)})))
	}
}

func sameID(a, b *int64) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

func markTrack(m *store.Mark) int64 {
	if m.TrackID == nil {
		return 0
	}
	return *m.TrackID
}
