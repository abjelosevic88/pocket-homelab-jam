package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestHistory(t *testing.T) {
	a := newApp(t, nil)
	sid := id(a.ok(a.alice, "POST", "/api/v1/songs", map[string]any{"title": "Story"}))
	g := id(a.ok(a.alice, "POST", "/api/v1/songs/"+sid+"/tracks", map[string]any{"instrument": "guitar"}))
	b := id(a.ok(a.alice, "POST", "/api/v1/songs/"+sid+"/tracks", map[string]any{"instrument": "bass"}))
	lines := func() []string {
		var out []string
		for _, e := range a.ok(a.alice, "GET", "/api/v1/songs/"+sid+"/history", nil)["events"].([]any) {
			out = append(out, e.(map[string]any)["text"].(string))
		}
		return out // newest first
	}
	_, k := a.upload(a.alice, "/api/v1/tracks/"+g+"/takes", map[string]any{"duration_ms": 500, "offset_ms": 2000, "source": "recorded"}, tone(0.5), "take.wav")
	// a fader moved three times: one line, from where it started
	for _, v := range []int{90, 70, 82} {
		a.ok(a.alice, "PUT", "/api/v1/tracks/"+g, map[string]any{"volume": v})
	}
	// muted and unmuted: no line
	a.ok(a.alice, "PUT", "/api/v1/tracks/"+b, map[string]any{"muted": true})
	a.ok(a.alice, "PUT", "/api/v1/tracks/"+b, map[string]any{"muted": false})
	a.ok(a.alice, "PUT", "/api/v1/parts/"+id(map[string]any{"id": k["part"]}), map[string]any{"shift_ms": 1000, "track_id": num(map[string]any{"id": 0}, "id") + float64(atoiT(b))})
	a.ok(a.alice, "PUT", "/api/v1/songs/"+sid, map[string]any{"tempo": 132})
	l := lines()
	want := []string{
		"Tempo 100 BPM → 132 BPM",
		"Moved a part from Guitar to Bass (0:02.0 → 0:03.0)",
		"Guitar volume 100 % → 82 %",
		"Recorded take 1 on Guitar at 0:02.0 (0:00.5 long)",
		"Added the track “Bass”",
		"Added the track “Guitar”",
		"Made the song “Story”",
	}
	if strings.Join(l, "\n") != strings.Join(want, "\n") {
		t.Fatalf("history:\n%s\nwant:\n%s", strings.Join(l, "\n"), strings.Join(want, "\n"))
	}
	// a job of many calls with X-Jam-Group is one line
	for _, v := range []int{60, 50} {
		j, _ := json.Marshal(map[string]any{"volume": v})
		req, _ := http.NewRequest("PUT", a.url+"/api/v1/tracks/"+b, bytes.NewReader(j))
		req.Header.Set("Authorization", "Bearer "+a.alice)
		req.Header.Set("X-Jam-Group", "cut-1")
		req.Header.Set("X-Jam-Group-Label", url.QueryEscape("Cut the song to 0:01–0:05"))
		res, err := http.DefaultClient.Do(req)
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("grouped put: %v %v", err, res)
		}
		res.Body.Close()
	}
	if l := lines(); l[0] != "Cut the song to 0:01–0:05" || l[1] != want[0] {
		t.Fatalf("group: %v", l)
	}
	// someone else's song: not found
	a.expect(404, a.bob, "GET", "/api/v1/songs/"+sid+"/history", nil)
}

func atoiT(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

func TestUndo(t *testing.T) {
	a := newApp(t, nil)
	sid := id(a.ok(a.alice, "POST", "/api/v1/songs", map[string]any{"title": "Undo me", "tempo": 90}))
	g := id(a.ok(a.alice, "POST", "/api/v1/songs/"+sid+"/tracks", map[string]any{"instrument": "guitar"}))
	b := id(a.ok(a.alice, "POST", "/api/v1/songs/"+sid+"/tracks", map[string]any{"instrument": "bass"}))
	events := func() []map[string]any {
		var out []map[string]any
		for _, e := range a.ok(a.alice, "GET", "/api/v1/songs/"+sid+"/history", nil)["events"].([]any) {
			out = append(out, e.(map[string]any))
		}
		return out
	}
	newest := func() map[string]any { return events()[0] }
	undo := func(e map[string]any) map[string]any {
		t.Helper()
		return a.ok(a.alice, "POST", "/api/v1/songs/"+sid+"/history/"+id(e)+"/undo", nil)
	}
	track := func(tid string) map[string]any {
		for _, x := range a.ok(a.alice, "GET", "/api/v1/songs/"+sid, nil)["tracks"].([]any) {
			if id(x.(map[string]any)) == tid {
				return x.(map[string]any)
			}
		}
		return nil
	}

	// a fader run: undo puts it back, redo does it again, and the change can be undone again
	for _, v := range []int{90, 70} {
		a.ok(a.alice, "PUT", "/api/v1/tracks/"+g, map[string]any{"volume": v})
	}
	vol := newest()
	undo(vol)
	if num(track(g), "volume") != 100 {
		t.Fatalf("undo volume: %v", track(g)["volume"])
	}
	u := newest()
	if u["kind"] != "undo" || u["text"] != "Undid: Guitar volume 100 % → 70 %" || u["can_undo"] != true {
		t.Fatalf("undo line: %v", u)
	}
	a.expect(409, a.alice, "POST", "/api/v1/songs/"+sid+"/history/"+id(vol)+"/undo", nil)
	undo(u) // redo
	if num(track(g), "volume") != 70 || newest()["text"] != "Redid: Guitar volume 100 % → 70 %" || newest()["can_undo"] == true {
		t.Fatalf("redo: %v %v", track(g)["volume"], newest())
	}
	undo(vol) // the change stands again, so it can be undone again
	if num(track(g), "volume") != 100 {
		t.Fatalf("undo again: %v", track(g)["volume"])
	}

	// a take recorded, then moved to Bass: undo the move, then the recording
	_, k1 := a.upload(a.alice, "/api/v1/tracks/"+g+"/takes", map[string]any{"duration_ms": 500, "offset_ms": 2000, "source": "recorded"}, tone(0.5), "take.wav")
	rec := newest()
	a.ok(a.alice, "PUT", "/api/v1/parts/"+itoa(int64(num(k1, "part"))), map[string]any{"shift_ms": 1000, "track_id": atoiT(b)})
	undo(newest())
	if tk := track(g)["takes"].([]any); len(tk) != 1 || num(tk[0].(map[string]any), "offset_ms") != 2000 || len(track(b)["takes"].([]any)) != 0 {
		t.Fatalf("undo move: %v / %v", track(g)["takes"], track(b)["takes"])
	}
	undo(rec)
	if len(track(g)["takes"].([]any)) != 0 {
		t.Fatalf("undo recording: %v", track(g)["takes"])
	}
	// redo the recording: the take is back, with its sound
	var undoRec map[string]any
	for _, e := range events() {
		if e["text"] == "Undid: "+rec["text"].(string) {
			undoRec = e
		}
	}
	undo(undoRec)
	if len(track(g)["takes"].([]any)) != 1 {
		t.Fatalf("redo recording: %v", track(g)["takes"])
	}
	if code, _, _ := a.call(a.alice, "GET", "/api/v1/takes/"+id(k1)+"/audio", nil); code != 200 {
		t.Fatalf("redone take's audio: %d", code)
	}

	// a whole track deleted, with its take and a note pinned to it: undo brings all back
	a.ok(a.alice, "POST", "/api/v1/songs/"+sid+"/marks", map[string]any{"text": "riff", "at_ms": 1000, "track_id": atoiT(g)})
	a.ok(a.alice, "DELETE", "/api/v1/tracks/"+g, nil)
	undo(newest())
	so := a.ok(a.alice, "GET", "/api/v1/songs/"+sid, nil)
	if track(g) == nil || len(track(g)["takes"].([]any)) != 1 || len(so["marks"].([]any)) != 1 {
		t.Fatalf("undo delete track: %v", so)
	}
	if code, _, _ := a.call(a.alice, "GET", "/api/v1/takes/"+id(k1)+"/audio", nil); code != 200 {
		t.Fatalf("restored take's audio: %d", code)
	}

	// song details
	a.ok(a.alice, "PUT", "/api/v1/songs/"+sid, map[string]any{"title": "Renamed"})
	undo(newest())
	if a.ok(a.alice, "GET", "/api/v1/songs/"+sid, nil)["title"] != "Undo me" {
		t.Fatal("undo rename")
	}
	// lines that can't be undone, and other people
	for _, e := range events() {
		if strings.HasPrefix(e["text"].(string), "Made the song") && e["can_undo"] == true {
			t.Fatal("making the song can't be undone")
		}
	}
	a.expect(404, a.bob, "POST", "/api/v1/songs/"+sid+"/history/"+id(vol)+"/undo", nil)
}
