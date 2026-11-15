package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-jam/internal/config"
)

func TestIdeas(t *testing.T) {
	a := newApp(t, nil)

	// upload: the duration and format come from the WAV when not sent
	code, i1 := a.upload(a.alice, "/api/v1/ideas", map[string]any{"name": "Riff", "peaks": "AAEC", "source": "recorded", "device": "iPhone", "client_id": "phone-1", "created_at": "2026-11-10T08:30:00+01:00"}, tone(0.5), "idea.wav")
	if code != 201 || i1["name"] != "Riff" || num(i1, "duration_ms") != 500 || num(i1, "sample_rate") != demoRate || num(i1, "channels") != 1 || i1["peaks"] != "AAEC" ||
		i1["client_id"] != "phone-1" || i1["created_at"] != "2026-11-10T07:30:00.000Z" || i1["mime"] != "audio/wav" || num(i1, "size") == 0 || i1["device"] != "iPhone" {
		t.Fatalf("idea 1: %d %v", code, i1)
	}
	var files []string
	count := func() int {
		files, _ = a.st.AllMediaFiles(t.Context(), a.ids["Alice"])
		return len(files)
	}
	if count() != 1 {
		t.Fatalf("files: %v", files)
	}

	// the same client_id again: the same idea, and no second file
	code, again := a.upload(a.alice, "/api/v1/ideas", map[string]any{"name": "Riff (retry)", "client_id": "phone-1"}, tone(0.5), "idea.wav")
	if code != 200 || num(again, "id") != num(i1, "id") || again["name"] != "Riff" {
		t.Fatalf("retry: %d %v", code, again)
	}
	entries, _ := os.ReadDir(a.s.mediaPath(itoa(a.ids["Alice"])))
	if len(entries) != 1 || count() != 1 {
		t.Fatalf("a retry keeps no file: %d on disk", len(entries))
	}
	// Bob may use the same client_id for his own idea
	if code, _ := a.upload(a.bob, "/api/v1/ideas", map[string]any{"client_id": "phone-1"}, tone(0.3), "idea.wav"); code != 201 {
		t.Fatalf("Bob's idea: %d", code)
	}

	// no name: "Idea" and the day; newest first
	code, i2 := a.upload(a.alice, "/api/v1/ideas", map[string]any{"duration_ms": 300}, tone(0.3), "idea.wav")
	if code != 201 || len(i2["name"].(string)) < 6 || i2["name"].(string)[:5] != "Idea " || i2["client_id"] != "" {
		t.Fatalf("idea 2: %d %v", code, i2)
	}
	code, i3 := a.upload(a.alice, "/api/v1/ideas", map[string]any{"name": "Old one", "created_at": "2025-01-01T10:00:00Z"}, tone(0.2), "idea.wav")
	if code != 201 {
		t.Fatalf("idea 3: %d %v", code, i3)
	}
	if code, bad := a.upload(a.alice, "/api/v1/ideas", map[string]any{"created_at": "yesterday"}, tone(0.2), "idea.wav"); code != 400 {
		t.Fatalf("a bad created_at: %d %v", code, bad)
	}
	if code, bad := a.upload(a.alice, "/api/v1/ideas", nil, []byte("hello"), "notes.txt"); code != 400 {
		t.Fatalf("a text file is not an idea: %d %v", code, bad)
	}
	list := a.ok(a.alice, "GET", "/api/v1/ideas", nil)["ideas"].([]any)
	if len(list) != 3 || num(list[0].(map[string]any), "id") != num(i2, "id") || num(list[1].(map[string]any), "id") != num(i1, "id") || num(list[2].(map[string]any), "id") != num(i3, "id") {
		t.Fatalf("list order: %v", list)
	}
	if count() != 3 {
		t.Fatalf("files after the bad uploads: %v", files)
	}

	// rename
	if r := a.ok(a.alice, "PUT", "/api/v1/ideas/"+id(i1), map[string]any{"name": "  Sunday riff "}); r["name"] != "Sunday riff" {
		t.Fatalf("rename: %v", r)
	}
	a.expect(400, a.alice, "PUT", "/api/v1/ideas/"+id(i1), map[string]any{"name": " "})
	if g := a.ok(a.alice, "GET", "/api/v1/ideas/"+id(i1), nil); g["name"] != "Sunday riff" {
		t.Fatalf("get: %v", g)
	}
	if hits := a.ok(a.alice, "GET", "/api/v1/search?q=sunday", nil)["hits"].([]any); len(hits) != 1 || hits[0].(map[string]any)["kind"] != "idea" {
		t.Fatalf("search: %v", hits)
	}
	if home := a.ok(a.alice, "GET", "/api/v1/home", nil); num(home, "idea_count") != 3 {
		t.Fatalf("home: %v", home)
	}

	// the audio, with ranges
	req, _ := http.NewRequest("GET", a.url+"/api/v1/ideas/"+id(i1)+"/audio", nil)
	req.Header.Set("Authorization", "Bearer "+a.alice)
	req.Header.Set("Range", "bytes=0-3")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	head, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 206 || string(head) != "RIFF" || res.Header.Get("Content-Type") != "audio/wav" {
		t.Fatalf("range: %d %q %s", res.StatusCode, head, res.Header.Get("Content-Type"))
	}
	req, _ = http.NewRequest("GET", a.url+"/api/v1/ideas/"+id(i1)+"/audio?download=1", nil)
	req.Header.Set("Authorization", "Bearer "+a.alice)
	res, _ = http.DefaultClient.Do(req)
	whole, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || int64(len(whole)) != int64(num(i1, "size")) || res.Header.Get("Content-Disposition") != `attachment; filename="Sunday riff.wav"` {
		t.Fatalf("download: %d %d %q", res.StatusCode, len(whole), res.Header.Get("Content-Disposition"))
	}

	// other people's ideas answer 404
	for _, p := range []string{"/api/v1/ideas/" + id(i1), "/api/v1/ideas/" + id(i1) + "/audio"} {
		a.expect(404, a.bob, "GET", p, nil)
	}
	a.expect(404, a.bob, "PUT", "/api/v1/ideas/"+id(i1), map[string]any{"name": "Mine"})
	a.expect(404, a.bob, "DELETE", "/api/v1/ideas/"+id(i1), nil)
	a.expect(404, a.bob, "POST", "/api/v1/ideas/"+id(i1)+"/song", map[string]any{})
	if l := a.ok(a.bob, "GET", "/api/v1/ideas", nil)["ideas"].([]any); len(l) != 1 {
		t.Fatalf("Bob's ideas: %v", l)
	}

	// delete: the row and the file
	a.expect(204, a.alice, "DELETE", "/api/v1/ideas/"+id(i3), nil)
	a.expect(404, a.alice, "GET", "/api/v1/ideas/"+id(i3), nil)
	if count() != 2 {
		t.Fatalf("files after delete: %v", files)
	}
	if entries, _ := os.ReadDir(a.s.mediaPath(itoa(a.ids["Alice"]))); len(entries) != 2 {
		t.Fatalf("the file stays on disk: %d", len(entries))
	}

	// idea → song
	a.expect(400, a.alice, "POST", "/api/v1/ideas/"+id(i1)+"/song", map[string]any{"instrument": "kazoo"})
	a.expect(400, a.alice, "POST", "/api/v1/ideas/"+id(i1)+"/song", map[string]any{"tempo": 999})
	code, so, _ := a.call(a.alice, "POST", "/api/v1/ideas/"+id(i1)+"/song", map[string]any{"tempo": 92, "beats": 3, "beat_unit": 4, "key_name": "A minor", "count_in": 2, "click": false, "instrument": "guitar"})
	if code != 201 || so["title"] != "Sunday riff" || num(so, "tempo") != 92 || num(so, "beats") != 3 || so["key_name"] != "A minor" || num(so, "count_in") != 2 || so["click"] != false {
		t.Fatalf("song: %d %v", code, so)
	}
	tracks := so["tracks"].([]any)
	if len(tracks) != 1 {
		t.Fatalf("tracks: %v", tracks)
	}
	tr := tracks[0].(map[string]any)
	takes := tr["takes"].([]any)
	if tr["instrument"] != "guitar" || tr["name"] != "Guitar" || len(takes) != 1 {
		t.Fatalf("track: %v", tr)
	}
	k := takes[0].(map[string]any)
	if num(tr, "take_id") != num(k, "id") || num(k, "duration_ms") != 500 || num(k, "offset_ms") != 0 || num(k, "number") != 1 || k["peaks"] != "AAEC" || k["created_at"] != "2026-11-10T07:30:00.000Z" {
		t.Fatalf("take: %v", k)
	}
	a.expect(404, a.alice, "GET", "/api/v1/ideas/"+id(i1), nil)
	if l := a.ok(a.alice, "GET", "/api/v1/ideas", nil)["ideas"].([]any); len(l) != 1 {
		t.Fatalf("ideas after the song: %v", l)
	}
	// the take plays the idea's file
	req, _ = http.NewRequest("GET", a.url+"/api/v1/takes/"+id(k)+"/audio", nil)
	req.Header.Set("Authorization", "Bearer "+a.alice)
	res, _ = http.DefaultClient.Do(req)
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || len(b) != len(whole) {
		t.Fatalf("take audio: %d %d", res.StatusCode, len(b))
	}
	if full := a.ok(a.alice, "GET", "/api/v1/songs/"+id(so), nil); num(full, "length_ms") != 500 || num(full, "take_count") != 1 {
		t.Fatalf("the song: %v", full)
	}

	// with no body: the idea's name, the person's tempo, "other"
	code, so2, _ := a.call(a.alice, "POST", "/api/v1/ideas/"+id(i2)+"/song", nil)
	if code != 201 || so2["title"] != i2["name"] || num(so2, "tempo") != 100 || so2["tracks"].([]any)[0].(map[string]any)["instrument"] != "other" {
		t.Fatalf("song 2: %d %v", code, so2)
	}
	// deleting the song removes the file the idea had
	a.ok(a.alice, "DELETE", "/api/v1/songs/"+id(so), nil)
	if count() != 1 {
		t.Fatalf("files after the song went: %v", files)
	}
}

func TestIdeasExportImport(t *testing.T) {
	a := newApp(t, nil)
	a.upload(a.alice, "/api/v1/ideas", map[string]any{"name": "Hum", "client_id": "c-1", "peaks": "AAEC"}, tone(0.3), "a.wav")
	a.upload(a.alice, "/api/v1/ideas", map[string]any{"name": "Whistle"}, tone(0.2), "b.wav")
	get := func(path string) []byte {
		req, _ := http.NewRequest("GET", a.url+path, nil)
		req.Header.Set("Authorization", "Bearer "+a.alice)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return b
	}
	zipped := get("/api/v1/export.json?audio=1")
	post := func(auth string) map[string]any {
		req, _ := http.NewRequest("POST", a.url+"/api/v1/import", bytes.NewReader(zipped))
		req.Header.Set("Authorization", "Bearer "+auth)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		m := map[string]any{}
		_ = json.NewDecoder(res.Body).Decode(&m)
		if res.StatusCode != 200 {
			t.Fatalf("import: %d %v", res.StatusCode, m)
		}
		return m
	}
	if r := post(a.alice); r["text"] != "Nothing new: everything in the file is here already" {
		t.Fatalf("import twice: %v", r)
	}
	if r := post(a.bob); num(r["added"].(map[string]any), "ideas") != 2 {
		t.Fatalf("import into Bob: %v", r)
	}
	if r := post(a.bob); num(r["added"].(map[string]any), "ideas") != 0 {
		t.Fatalf("import twice into Bob: %v", r)
	}
	l := a.ok(a.bob, "GET", "/api/v1/ideas", nil)["ideas"].([]any)
	if len(l) != 2 {
		t.Fatalf("Bob's ideas: %v", l)
	}
	for _, x := range l {
		m := x.(map[string]any)
		if m["name"] == "Hum" && (m["client_id"] != "c-1" || m["peaks"] != "AAEC") {
			t.Fatalf("imported idea: %v", m)
		}
	}
}

func TestIdeasUserDeleted(t *testing.T) {
	a := newApp(t, nil)
	a.upload(a.bob, "/api/v1/ideas", map[string]any{"name": "Hum"}, tone(0.3), "a.wav")
	dir := a.s.mediaPath(itoa(a.ids["Bob"]))
	if e, _ := os.ReadDir(dir); len(e) != 1 {
		t.Fatalf("Bob's files: %d", len(e))
	}
	a.expect(204, a.alice, "DELETE", "/api/v1/users/"+itoa(a.ids["Bob"]), nil)
	if e, _ := os.ReadDir(dir); len(e) != 0 {
		t.Fatalf("Bob's files after his account went: %d", len(e))
	}
}

func TestDemoIdeas(t *testing.T) {
	a := newApp(t, func(c *config.Config) { c.DemoData = true })
	l := a.ok(a.alice, "GET", "/api/v1/ideas", nil)["ideas"].([]any)
	if len(l) != 2 || l[0].(map[string]any)["name"] != "Hook for the chorus" || l[0].(map[string]any)["peaks"] == "" {
		t.Fatalf("demo ideas: %v", l)
	}
}

func TestIdeaLinkScopes(t *testing.T) {
	read, write := []string{"jam:read"}, []string{"jam:write"}
	cases := []struct {
		method, path string
		scopes       []string
		want         bool
	}{
		{"GET", "/api/v1/ideas", read, true},
		{"GET", "/api/v1/ideas/3", read, true},
		{"GET", "/api/v1/ideas/3/audio", read, true},
		{"GET", "/api/v1/ideas", write, false},
		{"POST", "/api/v1/ideas", write, true},
		{"POST", "/api/v1/ideas", read, false},
		{"PUT", "/api/v1/ideas/3", write, true},
		{"DELETE", "/api/v1/ideas/3", write, true},
		{"DELETE", "/api/v1/ideas/3", read, false},
		{"POST", "/api/v1/ideas/3/song", write, true},
		{"POST", "/api/v1/ideas/3/song", read, false},
	}
	for _, c := range cases {
		r, _ := http.NewRequest(c.method, "http://x"+c.path, nil)
		if got := linkAllows(c.scopes, r); got != c.want {
			t.Errorf("%s %s with %v: %v, want %v", c.method, c.path, c.scopes, got, c.want)
		}
	}
}
