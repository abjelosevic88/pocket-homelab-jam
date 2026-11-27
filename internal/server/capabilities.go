package server

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-jam/internal/store"
)

// The three routes behind `capabilities` (links.go): /api/v1/pocket/search,
// /day/{date} and /widget, for Pocket Search, Pocket Day and Pocket Hub. Each
// runs as the request's person and reads only what is theirs; the SDK's
// handlers check and cap what goes in and out. They read what GET /search,
// /day/{date} and /home read, and change nothing: no song is opened, no time
// is counted. Nothing can be kept here from a quick line: a song is made on
// its page, with a tempo and tracks, and an idea is a recording.

const (
	searchSongs = 200 // songs a search reads before it puts the best names first
	widgetSongs = 5   // songs the widget shows
)

func songURL(id int64) string { return "/songs/" + itoa(id) }

// dots joins the parts of a line that are there: "3 tracks · 2:45".
func dots(parts ...string) string {
	out := parts[:0:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

// count is a number with its word: "1 track", "3 tracks"; nothing for none.
func count(n int, one, many string) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n) + " " + plural(n, one, many)
}

// upperFirst starts a line with a capital letter.
func upperFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

// minutesWords is time played as the pages say it: "45 min", "1 h 5 min", "2 h".
func minutesWords(min int) string {
	h, m := min/60, min%60
	switch {
	case h == 0:
		return fmt.Sprintf("%d min", m)
	case m == 0:
		return fmt.Sprintf("%d h", h)
	}
	return fmt.Sprintf("%d h %d min", h, m)
}

// whenWords says when something happened, in the server's time: "just now",
// "20 min ago", "today", "yesterday", "3 days ago", "4 Nov", "4 Nov 2025".
func whenWords(iso string, now time.Time) string {
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		return ""
	}
	t, now = t.In(time.Local), now.In(time.Local)
	ago := now.Sub(t)
	day := func(x time.Time) time.Time { return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, time.Local) }
	days := int(math.Round(day(now).Sub(day(t)).Hours() / 24))
	switch {
	case ago < time.Minute:
		return "just now"
	case ago < time.Hour:
		return fmt.Sprintf("%d min ago", int(ago.Minutes()))
	case days == 0:
		return "today"
	case days == 1:
		return "yesterday"
	case days > 1 && days < 7:
		return fmt.Sprintf("%d days ago", days)
	case t.Year() == now.Year():
		return t.Format("2 Jan")
	}
	return t.Format("2 Jan 2006")
}

// ---------- search ----------

// hasWord reports whether there is a letter or a digit to look for.
func hasWord(q string) bool {
	return strings.IndexFunc(q, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0
}

// pocketSearch answers Pocket Search with the songs GET /search finds (by
// name, artist, key, notes, band or a note on the timeline): the song whose
// name is the words first, then names that start with them, then names that
// have them, then the rest, each in the songs list's order (played last
// first). The line under it is the tracks, the length and when it was last
// changed, as the songs list shows them.
func (s *Server) pocketSearch(r *http.Request, q string, limit int) ([]core.SearchItem, error) {
	ctx := r.Context()
	if !hasWord(q) {
		return nil, nil
	}
	songs, _, err := s.store.ListSongs(ctx, actorID(ctx), store.SongFilter{Q: q, Limit: searchSongs})
	if err != nil {
		return nil, err
	}
	low, now := strings.ToLower(q), time.Now()
	out := make([]core.SearchItem, 0, len(songs))
	for _, so := range songs {
		it := core.SearchItem{ID: itoa(so.ID), Title: so.Title, Kind: "song", Date: so.UpdatedAt, URL: songURL(so.ID), Score: 1}
		length := ""
		if so.LengthMS > 0 {
			length = duration(so.LengthMS)
		}
		edited := ""
		if w := whenWords(so.UpdatedAt, now); w != "" {
			edited = "edited " + w
		}
		it.Snippet = dots(count(so.TrackCount, "track", "tracks"), length, edited)
		switch name := strings.ToLower(strings.TrimSpace(so.Title)); {
		case name == low:
			it.Score = 4
		case strings.HasPrefix(name, low):
			it.Score = 3
		case strings.Contains(name, low):
			it.Score = 2
		}
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ---------- day ----------

// songDay is what happened to one song on a day.
type songDay struct {
	id      int64
	first   time.Time
	seconds int // played or recorded (the sittings)
	takes   int
	mixes   int
	changes int // lines in its history
}

func (d *songDay) at(t time.Time) {
	if !t.IsZero() && (d.first.IsZero() || t.Before(d.first)) {
		d.first = t
	}
}

func parseStamp(iso string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, iso)
	return t
}

// pocketDay answers Pocket Day: each song worked on that day once (made,
// changed, played or recorded), with the minutes played on it, the takes
// recorded and the mixes made; the time played without a song; and the
// ideas recorded. GET /day/{date} reads the same sittings, takes, mixes and
// ideas; the song's history says which songs were changed. The day runs in
// the caller's time zone when it gives one, else in the server's.
func (s *Server) pocketDay(r *http.Request, date string, tz *time.Location) (*core.Day, error) {
	ctx, uid := r.Context(), actorID(r.Context())
	loc := time.Local
	if tz != nil {
		loc = tz
	}
	start, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return nil, err
	}
	end := start.AddDate(0, 0, 1)
	stamp := func(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
	from, to := stamp(start), stamp(end)
	in := func(t time.Time) bool { return !t.IsZero() && !t.Before(start) && t.Before(end) }
	clock := func(t time.Time) string { return t.In(loc).Format("15:04") }

	byID := map[int64]*songDay{}
	song := func(id int64) *songDay {
		if byID[id] == nil {
			byID[id] = &songDay{id: id}
		}
		return byID[id]
	}
	day := &core.Day{Date: date}
	minutes, takes, mixes, songs, ideas := 0, 0, 0, 0, 0

	// time played: the sittings that started that day (a sitting's own day is the server's)
	sittings, err := s.store.ListSittings(ctx, uid, store.AddDays(date, -1), store.AddDays(date, 1))
	if err != nil {
		return nil, err
	}
	for i := len(sittings) - 1; i >= 0; i-- { // oldest first
		x := sittings[i]
		t := parseStamp(x.StartedAt)
		if !in(t) {
			continue
		}
		if x.SongID != nil {
			d := song(*x.SongID)
			d.seconds += x.Seconds
			d.at(t)
			continue
		}
		m := (x.Seconds + 30) / 60
		minutes += m
		day.Items = append(day.Items, core.DayItem{ID: "sitting:" + itoa(x.ID), Time: clock(t), Title: firstNonEmpty(x.Title, "Practice"), Kind: "practice",
			Minutes: float64(m), URL: "/time"})
	}
	ks, err := s.store.TakesOn(ctx, uid, from, to)
	if err != nil {
		return nil, err
	}
	for _, k := range ks {
		d := song(k.SongID)
		d.takes++
		d.at(parseStamp(k.CreatedAt))
	}
	ms, err := s.store.ListMixes(ctx, uid, 0, 500)
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
		t := parseStamp(m.CreatedAt)
		if !in(t) {
			continue
		}
		mixes++
		if m.SongID != nil {
			d := song(*m.SongID)
			d.mixes++
			d.at(t)
			continue
		}
		day.Items = append(day.Items, core.DayItem{ID: "mix:" + itoa(m.ID), Time: clock(t), Title: m.Name, Sub: "Mix · " + duration(m.DurationMS), Kind: "mix", URL: "/mixes"})
	}
	worked, err := s.store.SongsWorkedOn(ctx, uid, from, to)
	if err != nil {
		return nil, err
	}
	for _, w := range worked {
		d := song(w.SongID)
		d.changes += w.Changes
		d.at(parseStamp(w.First))
	}

	list := make([]*songDay, 0, len(byID))
	for _, d := range byID {
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool {
		if !list[i].first.Equal(list[j].first) {
			return list[i].first.Before(list[j].first)
		}
		return list[i].id < list[j].id
	})
	for _, d := range list {
		so, err := s.store.GetSong(ctx, uid, d.id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		m := (d.seconds + 30) / 60
		made := in(parseStamp(so.CreatedAt))
		it := core.DayItem{ID: "song:" + itoa(so.ID), Title: so.Title, Minutes: float64(m), URL: songURL(so.ID), Kind: "edited"}
		if !d.first.IsZero() {
			it.Time = clock(d.first)
		}
		switch {
		case d.takes > 0:
			it.Kind = "recorded"
		case d.seconds > 0:
			it.Kind = "practice"
		case made:
			it.Kind = "new"
		}
		var what []string
		if made {
			what = append(what, "new song")
		}
		what = append(what, count(d.takes, "take", "takes"), count(d.mixes, "mix", "mixes"))
		if it.Sub = upperFirst(dots(what...)); it.Sub == "" && d.seconds == 0 {
			it.Sub = "Changed"
		}
		day.Items = append(day.Items, it)
		minutes += m
		takes += d.takes
		songs++
	}

	is, err := s.store.ListIdeas(ctx, uid, 0, 0)
	if err != nil {
		return nil, err
	}
	for _, x := range is { // newest first
		t := parseStamp(x.CreatedAt)
		if !t.IsZero() && t.Before(start) {
			break
		}
		if !in(t) {
			continue
		}
		ideas++
		day.Items = append(day.Items, core.DayItem{ID: "idea:" + itoa(x.ID), Time: clock(t), Title: x.Name, Sub: "Idea · " + duration(x.DurationMS), Kind: "idea", URL: "/ideas"})
	}
	if len(day.Items) == 0 {
		return nil, nil
	}

	// "2 songs and 1 idea, 1 h 5 min played, 6 takes recorded, 1 mix made"
	var parts []string
	switch ns, ni := count(songs, "song", "songs"), count(ideas, "idea", "ideas"); {
	case ns != "" && ni != "":
		parts = append(parts, ns+" and "+ni)
	case ns != "" || ni != "":
		parts = append(parts, ns+ni)
	}
	if minutes > 0 {
		parts = append(parts, minutesWords(minutes)+" played")
		day.Metrics = append(day.Metrics, core.Metric{Label: "Played", Value: minutesWords(minutes)})
	}
	if takes > 0 {
		parts = append(parts, count(takes, "take", "takes")+" recorded")
		day.Metrics = append(day.Metrics, core.Metric{Label: "Takes", Value: strconv.Itoa(takes)})
	}
	if mixes > 0 {
		parts = append(parts, count(mixes, "mix", "mixes")+" made")
		day.Metrics = append(day.Metrics, core.Metric{Label: "Mixes", Value: strconv.Itoa(mixes)})
	}
	day.Summary = upperFirst(strings.Join(parts, ", "))
	return day, nil
}

// ---------- widget ----------

// pocketWidget answers Pocket Hub: the songs worked on last, as the start
// page and the songs list order them (opened or changed last first), each
// with when and how many tracks. Someone without songs gets nothing to show.
func (s *Server) pocketWidget(r *http.Request) (*core.WidgetData, error) {
	ctx := r.Context()
	songs, _, err := s.store.ListSongs(ctx, actorID(ctx), store.SongFilter{Limit: widgetSongs})
	if err != nil {
		return nil, err
	}
	if len(songs) == 0 {
		return nil, nil
	}
	now := time.Now()
	w := &core.WidgetData{URL: "/songs"}
	for _, so := range songs {
		when := so.UpdatedAt // as the songs list says it: opened last, else changed last
		if so.OpenedAt != nil && *so.OpenedAt != "" {
			when = *so.OpenedAt
		}
		w.Items = append(w.Items, core.WidgetItem{ID: itoa(so.ID), Title: so.Title, URL: songURL(so.ID),
			Detail: dots(whenWords(when, now), count(so.TrackCount, "track", "tracks"))})
	}
	return w, nil
}
