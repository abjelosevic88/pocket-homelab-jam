package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-jam/internal/store"
)

func ctxDetached(ctx context.Context) context.Context { return context.WithoutCancel(ctx) }

// handleHome is the start page: the songs played lately, the week, the
// newest mixes and a few numbers.
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	recent, total, _ := s.store.ListSongs(ctx, uid, store.SongFilter{Limit: 8})
	starred, _, _ := s.store.ListSongs(ctx, uid, store.SongFilter{Starred: true, Limit: 8})
	mixes, _ := s.store.ListMixes(ctx, uid, 0, 4)
	t := today()
	week, _ := s.store.MinutesPerDay(ctx, uid, store.AddDays(t, -6), t)
	month, _ := s.store.MinutesPerDay(ctx, uid, store.AddDays(t, -27), t)
	todayMin, weekMin, monthMin, weekTakes := 0, 0, 0, 0
	for _, d := range week {
		weekMin += d.Minutes
		weekTakes += d.Takes
		if d.Day == t {
			todayMin = d.Minutes
		}
	}
	for _, d := range month {
		monthMin += d.Minutes
	}
	counts := s.store.CountsFor(ctx, uid)
	writeJSON(w, http.StatusOK, map[string]any{"recent": recent, "starred": starred, "songs": total, "takes": counts["takes"], "mix_count": counts["mixes"], "mixes": mixes, "idea_count": counts["ideas"],
		"days": month, "today_minutes": todayMin, "week_minutes": weekMin, "week_takes": weekTakes, "month_minutes": monthMin, "streak": s.streak(ctx, uid)})
}

// handleSummary is a short overview for other apps (Pocket Hub, AI, Day).
func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	recent, total, _ := s.store.ListSongs(ctx, uid, store.SongFilter{Limit: 3})
	t := today()
	week, _ := s.store.MinutesPerDay(ctx, uid, store.AddDays(t, -6), t)
	mins, takes := 0, 0
	for _, d := range week {
		mins += d.Minutes
		takes += d.Takes
	}
	last := []map[string]any{}
	for _, so := range recent {
		last = append(last, map[string]any{"id": so.ID, "title": so.Title, "artist": so.Artist, "tracks": so.TrackCount, "url": s.link(fmt.Sprintf("/songs/%d", so.ID))})
	}
	writeJSON(w, http.StatusOK, map[string]any{"songs": total, "minutes_this_week": mins, "takes_this_week": takes, "streak": s.streak(ctx, uid), "recent": last, "url": s.link("/")})
}

// handleDay is what happened on one day, for Pocket Day's journal.
func (s *Server) handleDay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	day := chiParam(r, "date")
	t, err := time.ParseInLocation("2006-01-02", day, time.Local)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "a day is YYYY-MM-DD")
		return
	}
	sittings, _ := s.store.ListSittings(ctx, uid, day, day)
	from, to := t.UTC().Format(time.RFC3339Nano), t.AddDate(0, 0, 1).UTC().Format(time.RFC3339Nano)
	items := []map[string]any{}
	minutes := 0
	for i := len(sittings) - 1; i >= 0; i-- {
		x := sittings[i]
		m := (x.Seconds + 30) / 60
		minutes += m
		txt := fmt.Sprintf("%s — %d min", firstNonEmpty(x.Title, "Practice"), m)
		if x.Takes > 0 {
			txt += fmt.Sprintf(", %d %s", x.Takes, plural(x.Takes, "take", "takes"))
		}
		item := map[string]any{"kind": "sitting", "id": x.ID, "title": x.Title, "text": txt, "minutes": m, "takes": x.Takes, "time": localClock(x.StartedAt)}
		if x.SongID != nil {
			item["url"] = s.link(fmt.Sprintf("/songs/%d", *x.SongID))
		}
		items = append(items, item)
	}
	mixes, _ := s.store.ListMixes(ctx, uid, 0, 500)
	for _, m := range mixes {
		if m.CreatedAt >= from && m.CreatedAt < to {
			items = append(items, map[string]any{"kind": "mix", "id": m.ID, "title": m.Name, "text": "Mixed " + m.Name, "time": localClock(m.CreatedAt), "url": s.link("/mixes")})
		}
	}
	ideas, _ := s.store.ListIdeas(ctx, uid, 0, 0)
	for _, x := range ideas {
		if x.CreatedAt >= from && x.CreatedAt < to {
			items = append(items, map[string]any{"kind": "idea", "id": x.ID, "title": x.Name, "text": "Idea: " + x.Name + " (" + duration(x.DurationMS) + ")", "time": localClock(x.CreatedAt), "url": s.link("/ideas")})
		}
	}
	takes, _ := s.store.TakesOn(ctx, uid, from, to)
	writeJSON(w, http.StatusOK, map[string]any{"date": day, "items": items, "minutes": minutes, "takes": len(takes)})
}

func localClock(iso string) string {
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		return ""
	}
	return t.In(time.Local).Format("15:04")
}

// handleSearch finds songs (title, artist, key, notes), mixes and ideas.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	hits := []map[string]any{}
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
		return
	}
	lim := queryInt(r, "limit")
	if lim <= 0 || lim > 50 {
		lim = 20
	}
	low := strings.ToLower(q)
	songs, _, _ := s.store.ListSongs(ctx, uid, store.SongFilter{Q: q, Limit: lim})
	for _, so := range songs {
		snippet := ""
		if !strings.Contains(strings.ToLower(so.Title+" "+so.Artist), low) {
			snippet = matchLine(so.Notes, low)
		}
		hits = append(hits, map[string]any{"kind": "song", "id": so.ID, "title": so.Title, "subtitle": joinNonEmpty(" · ", so.Artist, fmt.Sprintf("%d %s", so.TrackCount, plural(so.TrackCount, "track", "tracks"))), "snippet": snippet,
			"url": s.link(fmt.Sprintf("/songs/%d", so.ID)), "path": fmt.Sprintf("/songs/%d", so.ID)})
	}
	mixes, _ := s.store.ListMixes(ctx, uid, 0, 500)
	for _, m := range mixes {
		if len(hits) >= lim*2 {
			break
		}
		if strings.Contains(strings.ToLower(m.Name+" "+m.SongTitle), low) {
			hits = append(hits, map[string]any{"kind": "mix", "id": m.ID, "title": m.Name, "subtitle": "Mix · " + duration(m.DurationMS), "url": s.link("/mixes"), "path": "/mixes"})
		}
	}
	ideas, _ := s.store.ListIdeas(ctx, uid, 0, 0)
	for _, x := range ideas {
		if len(hits) >= lim*2 {
			break
		}
		if strings.Contains(strings.ToLower(x.Name), low) {
			hits = append(hits, map[string]any{"kind": "idea", "id": x.ID, "title": x.Name, "subtitle": "Idea · " + duration(x.DurationMS), "url": s.link("/ideas"), "path": "/ideas"})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
}

func matchLine(text, low string) string {
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(strings.ToLower(l), low) {
			return clip(strings.TrimSpace(l), 160)
		}
	}
	return ""
}
