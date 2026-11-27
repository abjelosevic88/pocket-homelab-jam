package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-jam/internal/config"
	"github.com/abjelosevic88/pocket-homelab-jam/internal/store"
)

// fakeCapCore is a Pocket Core that vouches for link tokens
// pcx_<alice|bob>_<search|day|widget|read|write> from Pocket Hub and
// remembers the manifest Jam registers with.
type fakeCapCore struct {
	*httptest.Server
	mu       sync.Mutex
	manifest map[string]any
}

func newCapApp(t *testing.T) (*app, *fakeCapCore) {
	t.Helper()
	fc := &fakeCapCore{}
	scopes := map[string]string{"search": core.ScopeSearch, "day": core.ScopeDay, "widget": core.ScopeWidget, "capture": core.ScopeCapture,
		"read": "jam:read", "write": "jam:write"}
	fc.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/apps/register":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			fc.mu.Lock()
			fc.manifest = m
			fc.mu.Unlock()
			io.WriteString(w, `{}`)
		case "/api/v1/auth/introspect":
			var in struct{ Token string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			parts := strings.Split(in.Token, "_")
			if len(parts) != 3 || parts[0] != "pcx" || scopes[parts[2]] == "" {
				io.WriteString(w, `{"active":false}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "kind": "delegated", "audience": "jam", "scope": scopes[parts[2]],
				"user": map[string]any{"id": 1, "sub": parts[1] + "@example.com", "email": parts[1] + "@example.com", "name": parts[1], "role": "member"},
				"app":  map[string]any{"id": 9, "slug": "hub", "name": "Pocket Hub"}})
		default:
			io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(fc.Close)
	return newApp(t, func(c *config.Config) { c.CoreURL, c.CoreRegistrationKey = fc.URL, "key" }), fc
}

// items are the rows of a capability answer.
func items(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	l, ok := m["items"].([]any)
	if !ok {
		t.Fatalf("no items list in %v", m)
	}
	out := []map[string]any{}
	for _, v := range l {
		out = append(out, v.(map[string]any))
	}
	return out
}

// item finds the row with that title.
func item(t *testing.T, m map[string]any, title string) map[string]any {
	t.Helper()
	for _, it := range items(t, m) {
		if it["title"] == title {
			return it
		}
	}
	t.Fatalf("no %q in %v", title, m["items"])
	return nil
}

// titles are the rows' titles in order.
func titles(t *testing.T, m map[string]any) []string {
	t.Helper()
	out := []string{}
	for _, it := range items(t, m) {
		out = append(out, it["title"].(string))
	}
	return out
}

// songWithTake makes a song with one track and a take of that many seconds.
func (a *app) songWithTake(tok, title string, seconds float64, more map[string]any) string {
	a.t.Helper()
	body := map[string]any{"title": title}
	for k, v := range more {
		body[k] = v
	}
	sid := id(a.ok(tok, "POST", "/api/v1/songs", body))
	tid := id(a.ok(tok, "POST", "/api/v1/songs/"+sid+"/tracks", map[string]any{"instrument": "guitar"}))
	if code, k := a.upload(tok, "/api/v1/tracks/"+tid+"/takes", map[string]any{}, tone(seconds), "take.wav"); code != 201 {
		a.t.Fatalf("take: %d %v", code, k)
	}
	return sid
}

func TestPocketSearch(t *testing.T) {
	a := newApp(t, nil)
	hour := a.songWithTake(a.alice, "Blue Hour", 2, map[string]any{"notes": "slow, in A minor"})
	blue := id(a.ok(a.alice, "POST", "/api/v1/songs", map[string]any{"title": "Blue"}))
	old := id(a.ok(a.alice, "POST", "/api/v1/songs", map[string]any{"title": "Old Blues", "artist": "Ana"}))
	a.ok(a.alice, "POST", "/api/v1/songs", map[string]any{"title": "Riff", "notes": "bridge feels blue"})
	a.ok(a.alice, "POST", "/api/v1/songs", map[string]any{"title": "Nothing here"})

	res := a.ok(a.alice, "GET", "/api/v1/pocket/search?q=blue", nil)
	if got := titles(t, res); strings.Join(got, "|") != "Blue|Blue Hour|Old Blues|Riff" {
		t.Fatalf("the name first, then names that start with it, have it, then the notes: %v", got)
	}
	first := items(t, res)[0]
	if first["id"] != blue || first["kind"] != "song" || first["url"] != "/songs/"+blue || first["snippet"] != "edited just now" || first["date"] == nil {
		t.Fatalf("a song without tracks: %v", first)
	}
	if it := item(t, res, "Blue Hour"); it["id"] != hour || it["url"] != "/songs/"+hour || it["snippet"] != "1 track · 0:02 · edited just now" {
		t.Fatalf("a song with a take: %v", it)
	}
	if it := item(t, res, "Old Blues"); it["url"] != "/songs/"+old {
		t.Fatalf("old blues: %v", it)
	}
	// by its notes
	if got := titles(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=minor", nil)); strings.Join(got, "|") != "Blue Hour" {
		t.Fatalf("by notes: %v", got)
	}
	if got := items(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=blue&limit=2", nil)); len(got) != 2 || got[0]["title"] != "Blue" {
		t.Fatalf("limit 2: %v", got)
	}
	// no words, or nothing to look for
	for _, q := range []string{"", "%3F%3F", "*", "%25"} {
		if got := items(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q="+q, nil)); len(got) != 0 {
			t.Fatalf("%q finds %v", q, got)
		}
	}
	// another person's songs never appear
	if got := items(t, a.ok(a.bob, "GET", "/api/v1/pocket/search?q=blue", nil)); len(got) != 0 {
		t.Fatalf("bob finds alice's songs: %v", got)
	}
	a.expect(401, "", "GET", "/api/v1/pocket/search?q=blue", nil)
	// the old route answers as before (mixes and ideas too, its own shape)
	if old := a.ok(a.alice, "GET", "/api/v1/search?q=blue", nil); len(old["hits"].([]any)) != 4 {
		t.Fatalf("/search changed: %v", old)
	}
}

func TestPocketDay(t *testing.T) {
	a := newApp(t, nil)
	ctx := context.Background()
	today := store.Today()
	uid := a.ids["Alice"]

	// a song recorded and played, with a mix
	riff := a.songWithTake(a.alice, "Riff", 1, nil)
	a.ok(a.alice, "POST", "/api/v1/time", map[string]any{"song_id": n(riff), "seconds": 120})
	a.ok(a.alice, "POST", "/api/v1/time", map[string]any{"song_id": n(riff), "seconds": 60})
	if code, m := a.upload(a.alice, "/api/v1/songs/"+riff+"/mixes", map[string]any{"tracks": 1}, tone(0.4), "mix.wav"); code != 201 {
		t.Fatalf("mix: %d %v", code, m)
	}
	// a song made long ago and only changed today
	old := &store.Song{UserID: uid, Title: "Old one", Tempo: 90, Beats: 4, BeatUnit: 4, CreatedAt: "2020-01-02T10:00:00.000Z"}
	if err := a.st.CreateSong(ctx, old); err != nil {
		t.Fatal(err)
	}
	a.ok(a.alice, "PUT", "/api/v1/songs/"+itoa(old.ID), map[string]any{"tempo": 96})
	// a song made today and nothing else
	fresh := id(a.ok(a.alice, "POST", "/api/v1/songs", map[string]any{"title": "Fresh"}))
	// time with no song, an idea, and yesterday's time (not today's)
	a.ok(a.alice, "POST", "/api/v1/time", map[string]any{"title": "Scales", "seconds": 180})
	if code, m := a.upload(a.alice, "/api/v1/ideas", map[string]any{"name": "Hum"}, tone(0.5), "idea.wav"); code != 201 {
		t.Fatalf("idea: %d %v", code, m)
	}
	y := time.Now().Add(-24 * time.Hour)
	if err := a.st.InsertSitting(ctx, &store.Sitting{UserID: uid, SongID: &old.ID, Title: "Old one", Day: y.Format("2006-01-02"),
		StartedAt: y.UTC().Format(time.RFC3339Nano), LastAt: y.UTC().Format(time.RFC3339Nano), Seconds: 1200}); err != nil {
		t.Fatal(err)
	}

	day := a.ok(a.alice, "GET", "/api/v1/pocket/day/"+today, nil)
	if day["date"] != today || day["summary"] != "3 songs and 1 idea, 6 min played, 1 take recorded, 1 mix made" || len(items(t, day)) != 5 {
		t.Fatalf("day: %v", day)
	}
	ms := day["metrics"].([]any)
	for i, want := range [][2]string{{"Played", "6 min"}, {"Takes", "1"}, {"Mixes", "1"}} {
		if m := ms[i].(map[string]any); len(ms) != 3 || m["label"] != want[0] || m["value"] != want[1] {
			t.Fatalf("metrics: %v", ms)
		}
	}
	it := item(t, day, "Riff")
	if it["id"] != "song:"+riff || it["kind"] != "recorded" || num(it, "minutes") != 3 || it["sub"] != "New song · 1 take · 1 mix" || it["url"] != "/songs/"+riff || len(it["time"].(string)) != 5 {
		t.Fatalf("riff: %v", it)
	}
	if it := item(t, day, "Old one"); it["id"] != "song:"+itoa(old.ID) || it["kind"] != "edited" || it["sub"] != "Changed" || it["minutes"] != nil {
		t.Fatalf("a song only changed: %v", it)
	}
	if it := item(t, day, "Fresh"); it["kind"] != "new" || it["sub"] != "New song" || it["url"] != "/songs/"+fresh {
		t.Fatalf("a new song: %v", it)
	}
	if it := item(t, day, "Scales"); it["kind"] != "practice" || num(it, "minutes") != 3 || it["url"] != "/time" || !strings.HasPrefix(it["id"].(string), "sitting:") {
		t.Fatalf("time with no song: %v", it)
	}
	if it := item(t, day, "Hum"); it["kind"] != "idea" || it["sub"] != "Idea · 0:00" || it["url"] != "/ideas" || !strings.HasPrefix(it["id"].(string), "idea:") {
		t.Fatalf("an idea: %v", it)
	}
	// yesterday: only the time on the old song
	yd := a.ok(a.alice, "GET", "/api/v1/pocket/day/"+y.Format("2006-01-02"), nil)
	if yd["summary"] != "1 song, 20 min played" || len(items(t, yd)) != 1 || item(t, yd, "Old one")["kind"] != "practice" || item(t, yd, "Old one")["sub"] != nil {
		t.Fatalf("yesterday: %v", yd)
	}
	// the day the old song was made, in UTC and where it was already the next day
	if d := a.ok(a.alice, "GET", "/api/v1/pocket/day/2020-01-02?tz=UTC", nil); len(items(t, d)) != 1 || item(t, d, "Old one")["kind"] != "new" || item(t, d, "Old one")["time"] != "10:00" {
		t.Fatalf("2 Jan 2020 in UTC: %v", d)
	}
	if d := a.ok(a.alice, "GET", "/api/v1/pocket/day/2020-01-02?tz=Pacific/Kiritimati", nil); len(items(t, d)) != 0 {
		t.Fatalf("2 Jan 2020 in Kiritimati (it was the 3rd there): %v", d)
	}
	if d := a.ok(a.alice, "GET", "/api/v1/pocket/day/2020-01-03?tz=Pacific/Kiritimati", nil); item(t, d, "Old one")["time"] != "00:00" {
		t.Fatalf("3 Jan 2020 in Kiritimati: %v", d)
	}
	// a day with nothing, and somebody else's day
	if d := a.ok(a.alice, "GET", "/api/v1/pocket/day/2019-03-04", nil); d["summary"] != nil || len(items(t, d)) != 0 {
		t.Fatalf("an empty day: %v", d)
	}
	if d := a.ok(a.bob, "GET", "/api/v1/pocket/day/"+today+"?tz=Europe/Sarajevo", nil); d["summary"] != nil || len(items(t, d)) != 0 {
		t.Fatalf("bob sees alice's day: %v", d)
	}
	a.expect(400, a.alice, "GET", "/api/v1/pocket/day/yesterday", nil)
	// the old route answers as before
	if old := a.ok(a.alice, "GET", "/api/v1/day/"+today, nil); num(old, "minutes") != 6 || num(old, "takes") != 1 || len(old["items"].([]any)) != 4 {
		t.Fatalf("/day changed: %v", old)
	}
}

func TestPocketWidget(t *testing.T) {
	a := newApp(t, nil)
	// no songs: nothing to show
	if w := a.ok(a.bob, "GET", "/api/v1/pocket/widget", nil); w["line"] != nil || w["metrics"] != nil || len(items(t, w)) != 0 {
		t.Fatalf("without songs: %v", w)
	}
	ctx := context.Background()
	uid := a.ids["Alice"]
	for i, c := range []struct{ title, at string }{{"Oldest", "2020-01-02T10:00:00.000Z"}, {"Last year", time.Now().AddDate(-1, 0, -1).UTC().Format(time.RFC3339Nano)}} {
		so := &store.Song{UserID: uid, Title: c.title, Tempo: 90, Beats: 4, BeatUnit: 4, CreatedAt: c.at}
		if err := a.st.CreateSong(ctx, so); err != nil {
			t.Fatal(err)
		}
		if _, err := a.st.DB.Exec(`UPDATE songs SET updated_at = ? WHERE id = ?`, c.at, so.ID); err != nil {
			t.Fatal(i, err)
		}
	}
	riff := a.songWithTake(a.alice, "Riff", 1, nil)
	for _, title := range []string{"Two", "Three", "Four", "Five"} {
		a.ok(a.alice, "POST", "/api/v1/songs", map[string]any{"title": title})
	}
	a.ok(a.alice, "GET", "/api/v1/songs/"+riff+"?open=1", nil) // opened last: on top

	w := a.ok(a.alice, "GET", "/api/v1/pocket/widget", nil)
	if w["line"] != nil || w["metrics"] != nil || w["url"] != "/songs" {
		t.Fatalf("widget: %v", w)
	}
	rows := items(t, w)
	if got := titles(t, w); strings.Join(got, "|") != "Riff|Five|Four|Three|Two" {
		t.Fatalf("the songs worked on last: %v", got)
	}
	if rows[0]["id"] != riff || rows[0]["url"] != "/songs/"+riff || rows[0]["detail"] != "just now · 1 track" || rows[1]["detail"] != "just now" {
		t.Fatalf("rows: %v", rows)
	}
	// bob's widget never shows alice's songs
	a.ok(a.bob, "POST", "/api/v1/songs", map[string]any{"title": "Bob's"})
	if got := titles(t, a.ok(a.bob, "GET", "/api/v1/pocket/widget", nil)); strings.Join(got, "|") != "Bob's" {
		t.Fatalf("bob's widget: %v", got)
	}
}

func TestWhenWords(t *testing.T) {
	now := time.Date(2026, 11, 27, 15, 0, 0, 0, time.Local)
	at := func(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
	for _, c := range []struct {
		at, want string
	}{
		{at(now.Add(-20 * time.Second)), "just now"},
		{at(now.Add(-20 * time.Minute)), "20 min ago"},
		{at(now.Add(-3 * time.Hour)), "today"},
		{at(time.Date(2026, 11, 26, 23, 30, 0, 0, time.Local)), "yesterday"},
		{at(time.Date(2026, 11, 24, 9, 0, 0, 0, time.Local)), "3 days ago"},
		{at(time.Date(2026, 11, 4, 9, 0, 0, 0, time.Local)), "4 Nov"},
		{at(time.Date(2025, 11, 4, 9, 0, 0, 0, time.Local)), "4 Nov 2025"},
		{"", ""},
	} {
		if got := whenWords(c.at, now); got != c.want {
			t.Errorf("whenWords(%q) = %q, want %q", c.at, got, c.want)
		}
	}
}

// A link token with one pocket:* scope reaches its one route and nothing else.
func TestPocketLinkTokens(t *testing.T) {
	a, _ := newCapApp(t)
	riff := a.songWithTake(a.alice, "Riff", 1, nil)
	today := store.Today()
	for _, c := range []struct {
		tok, method, path string
		body              any
		want              int
	}{
		{"pcx_alice_search", "GET", "/api/v1/pocket/search?q=riff", nil, 200},
		{"pcx_alice_day", "GET", "/api/v1/pocket/day/" + today, nil, 200},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/widget", nil, 200},
		{"pcx_alice_widget", "GET", "/api/v1/auth/me", nil, 200},
		// another capability's route
		{"pcx_alice_search", "GET", "/api/v1/pocket/day/" + today, nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/pocket/search?q=riff", nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/search?q=riff", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/day/" + today, nil, 403},
		// nothing can be kept here: there is no such route, and search only reads
		{"pcx_alice_capture", "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "req-0000000000000001", "text": "x"}, 404},
		{"pcx_alice_search", "POST", "/api/v1/pocket/search?q=riff", nil, 405},
		// the rest of the app
		{"pcx_alice_widget", "GET", "/api/v1/home", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/summary", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/songs", nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/search?q=riff", nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/songs/" + riff, nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/day/" + today, nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/time", nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/songs/" + riff + "/history", nil, 403},
		{"pcx_alice_search", "POST", "/api/v1/songs", map[string]any{"title": "x"}, 403},
		{"pcx_alice_widget", "GET", "/api/v1/settings", nil, 403},
		// the app's own scopes do not open the new routes
		{"pcx_alice_read", "GET", "/api/v1/pocket/search?q=riff", nil, 403},
		{"pcx_alice_read", "GET", "/api/v1/pocket/day/" + today, nil, 403},
		{"pcx_alice_read", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_write", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_read", "GET", "/api/v1/search?q=riff", nil, 200},
		{"pcx_nobody", "GET", "/api/v1/pocket/widget", nil, 401},
	} {
		if code, m, _ := a.call(c.tok, c.method, c.path, c.body); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%v)", c.tok, c.method, c.path, code, c.want, m)
		}
	}
	// the token acts for its person only
	if got := titles(t, a.ok("pcx_alice_search", "GET", "/api/v1/pocket/search?q=riff", nil)); len(got) != 1 || got[0] != "Riff" {
		t.Fatalf("alice through the link: %v", got)
	}
	if got := items(t, a.ok("pcx_bob_search", "GET", "/api/v1/pocket/search?q=riff", nil)); len(got) != 0 {
		t.Fatalf("bob through the link finds alice's song: %v", got)
	}
	if got := titles(t, a.ok("pcx_alice_widget", "GET", "/api/v1/pocket/widget", nil)); len(got) != 1 || got[0] != "Riff" {
		t.Fatalf("alice's widget through the link: %v", got)
	}
	if w := a.ok("pcx_bob_widget", "GET", "/api/v1/pocket/widget", nil); len(items(t, w)) != 0 {
		t.Fatalf("bob's widget through the link: %v", w)
	}
	if d := a.ok("pcx_alice_day", "GET", "/api/v1/pocket/day/"+today, nil); item(t, d, "Riff")["kind"] != "recorded" {
		t.Fatalf("alice's day through the link: %v", d)
	}
	if d := a.ok("pcx_bob_day", "GET", "/api/v1/pocket/day/"+today, nil); len(items(t, d)) != 0 {
		t.Fatalf("bob's day through the link: %v", d)
	}
	// reading changed nothing: the song was not opened
	if so, _ := a.st.GetSong(context.Background(), a.ids["Alice"], n(riff)); so.OpenedAt != nil {
		t.Fatalf("a capability opened the song: %v", *so.OpenedAt)
	}
}

func TestPocketLinkScopes(t *testing.T) {
	req := func(m, p string) *http.Request { return httptest.NewRequest(m, p, nil) }
	for _, c := range []struct {
		scope string
		r     *http.Request
		want  bool
	}{
		{core.ScopeSearch, req("GET", "/api/v1/pocket/search?q=x"), true},
		{core.ScopeDay, req("GET", "/api/v1/pocket/day/2026-01-02"), true},
		{core.ScopeWidget, req("GET", "/api/v1/pocket/widget"), true},
		{core.ScopeWidget, req("GET", "/api/v1/auth/me"), true},
		{core.ScopeCapture, req("POST", "/api/v1/pocket/capture"), false},
		{core.ScopeSearch, req("POST", "/api/v1/pocket/search"), false},
		{core.ScopeDay, req("GET", "/api/v1/pocket/day/today"), false},
		{core.ScopeDay, req("GET", "/api/v1/day/2026-01-02"), false},
		{core.ScopeWidget, req("GET", "/api/v1/home"), false},
		{core.ScopeSearch, req("GET", "/api/v1/search"), false},
		{"jam:read", req("GET", "/api/v1/pocket/widget"), false},
		{"jam:read", req("GET", "/api/v1/pocket/search"), false},
		{"jam:write", req("POST", "/api/v1/pocket/capture"), false},
	} {
		if got := linkAllows([]string{c.scope}, c.r); got != c.want {
			t.Errorf("%s %s %s = %v, want %v", c.scope, c.r.Method, c.r.URL.Path, got, c.want)
		}
	}
}

// Jam tells Core what it can do, so Search, Day and Hub find it on their own.
func TestManifestCapabilities(t *testing.T) {
	a, fc := newCapApp(t)
	if err := a.s.registerWithCore(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc.mu.Lock()
	m := fc.manifest
	fc.mu.Unlock()
	if m["slug"] != "jam" || m["category"] != "Media" {
		t.Fatalf("manifest: %v %v", m["slug"], m["category"])
	}
	search, _ := m["search"].(map[string]any)
	day, _ := m["day"].(map[string]any)
	widget, _ := m["widget"].(map[string]any)
	if search == nil || day == nil || widget == nil || m["capture"] != nil {
		t.Fatalf("capabilities: %v %v %v %v", search, day, widget, m["capture"])
	}
	if b := search["bangs"].([]any); len(b) != 2 || b[0] != "jam" || b[1] != "song" || search["kinds"].([]any)[0] != "song" {
		t.Fatalf("search: %v", search)
	}
	if day["label"] != "Jam" || day["one"] != "song" || day["many"] != "songs" || widget["title"] != "Jam" {
		t.Fatalf("day %v, widget %v", day, widget)
	}
	offered := map[string]bool{}
	for _, o := range m["offers"].([]any) {
		offered[o.(map[string]any)["scope"].(string)] = true
	}
	for _, sc := range []string{"jam:read", "jam:write", core.ScopeSearch, core.ScopeDay, core.ScopeWidget} {
		if !offered[sc] {
			t.Errorf("%s is not offered", sc)
		}
	}
	if offered[core.ScopeCapture] {
		t.Error("pocket:capture is offered, but nothing can be kept here")
	}
}
