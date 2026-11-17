package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-jam/internal/config"
)

// upload sends a take or a mix as multipart (meta + file).
func (a *app) upload(auth, path string, meta map[string]any, audio []byte, name string) (int, map[string]any) {
	a.t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	mj, _ := json.Marshal(meta)
	_ = mw.WriteField("meta", string(mj))
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, name))
	h.Set("Content-Type", "audio/wav")
	fw, _ := mw.CreatePart(h)
	_, _ = fw.Write(audio)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", a.url+path, &b)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+auth)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return res.StatusCode, m
}

func tone(seconds float64) []byte {
	sy := newSynth(120, 4, 1, []chord{{57, []int{0, 3, 7}}}, 1)
	pcm := sy.melody([]int{69}, 1)
	return wav(pcm[:int(seconds*demoRate)], demoRate, 1)
}

func TestSongsTracksTakes(t *testing.T) {
	a := newApp(t, nil)
	so := a.ok(a.alice, "POST", "/api/v1/songs", map[string]any{"title": "Riff", "tempo": 92})
	sid := id(so)
	if num(so, "tempo") != 92 || num(so, "beats") != 4 || so["click"] != true {
		t.Fatalf("song defaults: %v", so)
	}
	a.expect(400, a.alice, "POST", "/api/v1/songs", map[string]any{"title": "x", "tempo": 999})
	a.expect(400, a.alice, "POST", "/api/v1/songs", map[string]any{"title": ""})

	tr := a.ok(a.alice, "POST", "/api/v1/songs/"+sid+"/tracks", map[string]any{"instrument": "guitar"})
	tid := id(tr)
	if tr["name"] != "Guitar" {
		t.Fatalf("track name from instrument: %v", tr)
	}
	a.expect(400, a.alice, "POST", "/api/v1/songs/"+sid+"/tracks", map[string]any{"instrument": "kazoo"})

	code, k1 := a.upload(a.alice, "/api/v1/tracks/"+tid+"/takes", map[string]any{"offset_ms": -40, "latency_ms": 40, "peaks": "AAEC"}, tone(0.5), "take.wav")
	if code != 201 || num(k1, "number") != 1 || num(k1, "duration_ms") != 500 || num(k1, "sample_rate") != demoRate {
		t.Fatalf("take 1: %d %v", code, k1)
	}
	code, k2 := a.upload(a.alice, "/api/v1/tracks/"+tid+"/takes", map[string]any{"duration_ms": 400}, tone(0.4), "take.wav")
	if code != 201 || num(k2, "number") != 2 {
		t.Fatalf("take 2: %d %v", code, k2)
	}
	code, bad := a.upload(a.alice, "/api/v1/tracks/"+tid+"/takes", map[string]any{"duration_ms": 400}, []byte("hello"), "notes.txt")
	if code != 400 {
		t.Fatalf("a text file is not a take: %d %v", code, bad)
	}

	full := a.ok(a.alice, "GET", "/api/v1/songs/"+sid+"?open=1", nil)
	tracks := full["tracks"].([]any)
	t0 := tracks[0].(map[string]any)
	if num(t0, "take_id") != num(k2, "id") || len(t0["takes"].([]any)) != 2 {
		t.Fatalf("the newest take plays: %v", t0)
	}
	if num(full, "take_count") != 2 || num(full, "length_ms") != 400 {
		t.Fatalf("counts: %v", full)
	}
	// pick take 1, then delete it: take 2 plays again
	a.ok(a.alice, "PUT", "/api/v1/tracks/"+tid, map[string]any{"take_id": num(k1, "id"), "volume": 80, "pan": -20, "solo": true})
	a.ok(a.alice, "DELETE", "/api/v1/takes/"+id(k1), nil)
	tr2 := a.ok(a.alice, "GET", "/api/v1/songs/"+sid, nil)["tracks"].([]any)[0].(map[string]any)
	if num(tr2, "take_id") != num(k2, "id") || num(tr2, "volume") != 80 || tr2["solo"] != true {
		t.Fatalf("after delete: %v", tr2)
	}
	// audio comes back
	res, err := http.Get(a.url + "/api/v1/takes/" + id(k2) + "/audio")
	if err == nil {
		res.Body.Close()
		if res.StatusCode != 401 {
			t.Fatalf("audio without signing in: %d", res.StatusCode)
		}
	}
	req, _ := http.NewRequest("GET", a.url+"/api/v1/takes/"+id(k2)+"/audio", nil)
	req.Header.Set("Authorization", "Bearer "+a.alice)
	req.Header.Set("Range", "bytes=0-3")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	head, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 206 || string(head) != "RIFF" {
		t.Fatalf("range: %d %q", res.StatusCode, head)
	}

	// a mix
	code, mix := a.upload(a.alice, "/api/v1/songs/"+sid+"/mixes", map[string]any{"tracks": 1}, tone(0.4), "mix.wav")
	if code != 201 || mix["name"] != "Riff — mix 1" {
		t.Fatalf("mix: %d %v", code, mix)
	}
	if l := a.ok(a.alice, "GET", "/api/v1/mixes", nil)["mixes"].([]any); len(l) != 1 {
		t.Fatalf("mixes: %v", l)
	}
	// stems
	req, _ = http.NewRequest("GET", a.url+"/api/v1/songs/"+sid+"/stems.zip", nil)
	req.Header.Set("Authorization", "Bearer "+a.alice)
	res, _ = http.DefaultClient.Do(req)
	zb, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !bytes.HasPrefix(zb, []byte("PK")) {
		t.Fatalf("stems: %d", res.StatusCode)
	}

	// time spent
	a.ok(a.alice, "POST", "/api/v1/time", map[string]any{"song_id": n(sid), "seconds": 60})
	a.ok(a.alice, "POST", "/api/v1/time", map[string]any{"song_id": n(sid), "seconds": 60})
	a.expect(400, a.alice, "POST", "/api/v1/time", map[string]any{"song_id": n(sid), "seconds": 4000})
	tm := a.ok(a.alice, "GET", "/api/v1/time", nil)
	if num(tm, "minutes") != 2 || num(tm, "streak") != 1 || len(tm["sittings"].([]any)) != 1 {
		t.Fatalf("time: %v", tm)
	}
	home := a.ok(a.alice, "GET", "/api/v1/home", nil)
	if num(home, "today_minutes") != 2 || num(home, "songs") != 1 {
		t.Fatalf("home: %v", home)
	}
	if hits := a.ok(a.alice, "GET", "/api/v1/search?q=riff", nil)["hits"].([]any); len(hits) != 2 {
		t.Fatalf("search: %v", hits)
	}

	a.ok(a.alice, "DELETE", "/api/v1/songs/"+sid, nil)
	a.expect(404, a.alice, "GET", "/api/v1/songs/"+sid, nil)
}

func TestPerPerson(t *testing.T) {
	a := newApp(t, nil)
	sid := id(a.ok(a.alice, "POST", "/api/v1/songs", map[string]any{"title": "Mine"}))
	tid := id(a.ok(a.alice, "POST", "/api/v1/songs/"+sid+"/tracks", map[string]any{"instrument": "voice"}))
	_, k := a.upload(a.alice, "/api/v1/tracks/"+tid+"/takes", nil, tone(0.3), "a.wav")
	_, m := a.upload(a.alice, "/api/v1/songs/"+sid+"/mixes", nil, tone(0.3), "m.wav")

	// Bob (a member) sees none of it — and neither would an admin
	for _, p := range []string{"/api/v1/songs/" + sid, "/api/v1/takes/" + id(k) + "/audio", "/api/v1/mixes/" + id(m) + "/audio", "/api/v1/songs/" + sid + "/stems.zip"} {
		a.expect(404, a.bob, "GET", p, nil)
	}
	a.expect(404, a.bob, "PUT", "/api/v1/songs/"+sid, map[string]any{"title": "Ha"})
	a.expect(404, a.bob, "DELETE", "/api/v1/songs/"+sid, nil)
	a.expect(404, a.bob, "POST", "/api/v1/songs/"+sid+"/tracks", map[string]any{"instrument": "voice"})
	a.expect(404, a.bob, "PUT", "/api/v1/tracks/"+tid, map[string]any{"volume": 0})
	a.expect(404, a.bob, "DELETE", "/api/v1/tracks/"+tid, nil)
	a.expect(404, a.bob, "PUT", "/api/v1/takes/"+id(k), map[string]any{"liked": true})
	a.expect(404, a.bob, "DELETE", "/api/v1/takes/"+id(k), nil)
	a.expect(404, a.bob, "DELETE", "/api/v1/mixes/"+id(m), nil)
	a.expect(404, a.bob, "POST", "/api/v1/time", map[string]any{"song_id": n(sid), "seconds": 30})
	if code, _ := a.upload(a.bob, "/api/v1/tracks/"+tid+"/takes", nil, tone(0.3), "b.wav"); code != 404 {
		t.Fatalf("Bob uploads to Alice's track: %d", code)
	}
	// Bob cannot point his track at Alice's take
	bs := id(a.ok(a.bob, "POST", "/api/v1/songs", map[string]any{"title": "His"}))
	bt := id(a.ok(a.bob, "POST", "/api/v1/songs/"+bs+"/tracks", map[string]any{"instrument": "bass"}))
	a.expect(400, a.bob, "PUT", "/api/v1/tracks/"+bt, map[string]any{"take_id": num(k, "id")})
	if l := a.ok(a.bob, "GET", "/api/v1/songs", nil)["songs"].([]any); len(l) != 1 {
		t.Fatalf("Bob's songs: %v", l)
	}
	if l := a.ok(a.bob, "GET", "/api/v1/mixes", nil)["mixes"].([]any); len(l) != 0 {
		t.Fatalf("Bob's mixes: %v", l)
	}
	if hits := a.ok(a.bob, "GET", "/api/v1/search?q=mine", nil)["hits"].([]any); len(hits) != 0 {
		t.Fatalf("Bob finds Alice's song: %v", hits)
	}
}

func TestExportImport(t *testing.T) {
	a := newApp(t, nil)
	sid := id(a.ok(a.alice, "POST", "/api/v1/songs", map[string]any{"title": "Keep me", "key_name": "D"}))
	tid := id(a.ok(a.alice, "POST", "/api/v1/songs/"+sid+"/tracks", map[string]any{"instrument": "keys", "volume": 70}))
	a.upload(a.alice, "/api/v1/tracks/"+tid+"/takes", nil, tone(0.3), "a.wav")
	a.upload(a.alice, "/api/v1/tracks/"+tid+"/takes", nil, tone(0.2), "b.wav")
	a.upload(a.alice, "/api/v1/songs/"+sid+"/mixes", nil, tone(0.3), "m.wav")
	a.ok(a.alice, "POST", "/api/v1/time", map[string]any{"song_id": n(sid), "seconds": 120})

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
	plain := get("/api/v1/export.json")
	post := func(auth string, body []byte) map[string]any {
		req, _ := http.NewRequest("POST", a.url+"/api/v1/import", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+auth)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var m map[string]any
		_ = json.NewDecoder(res.Body).Decode(&m)
		if res.StatusCode != 200 {
			t.Fatalf("import: %d %v", res.StatusCode, m)
		}
		return m
	}
	// into the same account: nothing new
	if r := post(a.alice, zipped); r["text"] != "Nothing new: everything in the file is here already" {
		t.Fatalf("import twice: %v", r)
	}
	// into Bob's: everything, with the audio
	r := post(a.bob, zipped)
	added := r["added"].(map[string]any)
	if num(added, "songs") != 1 || num(added, "tracks") != 1 || num(added, "takes") != 2 || num(added, "mixes") != 1 || num(added, "sittings") != 1 {
		t.Fatalf("import into Bob: %v", r)
	}
	if r := post(a.bob, zipped); num(r["added"].(map[string]any), "takes") != 0 {
		t.Fatalf("import twice into Bob: %v", r)
	}
	songs := a.ok(a.bob, "GET", "/api/v1/songs", nil)["songs"].([]any)
	bs := id(songs[0].(map[string]any))
	full := a.ok(a.bob, "GET", "/api/v1/songs/"+bs, nil)
	tr := full["tracks"].([]any)[0].(map[string]any)
	if num(tr, "volume") != 70 || len(tr["takes"].([]any)) != 2 {
		t.Fatalf("Bob's imported track: %v", tr)
	}
	// the JSON alone brings no takes, and nothing twice
	if r := post(a.bob, plain); r["text"] != "Nothing new: everything in the file is here already" {
		t.Fatalf("json import: %v", r)
	}
}

func TestLinkScopes(t *testing.T) {
	read := []string{"jam:read"}
	write := []string{"jam:write"}
	cases := []struct {
		method, path string
		scopes       []string
		want         bool
	}{
		{"GET", "/api/v1/auth/me", nil, true},
		{"GET", "/api/v1/songs", read, true},
		{"GET", "/api/v1/songs/3", read, true},
		{"GET", "/api/v1/takes/3/audio", read, true},
		{"GET", "/api/v1/day/2026-11-15", read, true},
		{"GET", "/api/v1/songs", write, false},
		{"POST", "/api/v1/songs", write, true},
		{"POST", "/api/v1/songs", read, false},
		{"PUT", "/api/v1/songs/3", write, true},
		{"POST", "/api/v1/tracks/3/takes", write, false},
		{"DELETE", "/api/v1/takes/3", write, false},
		{"GET", "/api/v1/settings", read, false},
		{"GET", "/api/v1/export.json", read, false},
	}
	for _, c := range cases {
		r, _ := http.NewRequest(c.method, "http://x"+c.path, nil)
		if got := linkAllows(c.scopes, r); got != c.want {
			t.Errorf("%s %s with %v: %v, want %v", c.method, c.path, c.scopes, got, c.want)
		}
	}
}

func TestSettingsPerPerson(t *testing.T) {
	a := newApp(t, nil)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"tempo": 132, "a4": 442, "practice_to": "guitar", "monitor": true})
	a.expect(400, a.alice, "PUT", "/api/v1/settings", map[string]any{"a4": 500})
	a.expect(400, a.alice, "PUT", "/api/v1/settings", map[string]any{"practice_to": "somewhere"})
	al := a.ok(a.alice, "GET", "/api/v1/settings", nil)
	bo := a.ok(a.bob, "GET", "/api/v1/settings", nil)
	if num(al, "tempo") != 132 || num(al, "a4") != 442 || al["practice_to"] != "guitar" || al["monitor"] != true {
		t.Fatalf("alice: %v", al)
	}
	if num(bo, "tempo") != 100 || num(bo, "a4") != 440 || bo["practice_to"] != "off" || bo["monitor"] != false {
		t.Fatalf("bob: %v", bo)
	}
	so := a.ok(a.alice, "POST", "/api/v1/songs", map[string]any{"title": "Fast"})
	if num(so, "tempo") != 132 {
		t.Fatalf("a new song takes the person's tempo: %v", so)
	}
}

func TestDemoData(t *testing.T) {
	a := newApp(t, func(c *config.Config) { c.DemoData = true })
	songs := a.ok(a.alice, "GET", "/api/v1/songs", nil)
	if num(songs, "total") != 6 {
		t.Fatalf("demo songs: %v", songs["total"])
	}
	home := a.ok(a.alice, "GET", "/api/v1/home", nil)
	if len(home["mixes"].([]any)) != 2 || num(home, "takes") < 15 || num(home, "month_minutes") == 0 {
		t.Fatalf("demo home: mixes %d takes %v minutes %v", len(home["mixes"].([]any)), home["takes"], home["month_minutes"])
	}
	if l := a.ok(a.bob, "GET", "/api/v1/songs", nil)["songs"].([]any); len(l) != 0 {
		t.Fatal("demo data lands only in the owner's account")
	}
}

func TestParts(t *testing.T) {
	a := newApp(t, nil)
	sid := id(a.ok(a.alice, "POST", "/api/v1/songs", map[string]any{"title": "Parts"}))
	gt := a.ok(a.alice, "POST", "/api/v1/songs/"+sid+"/tracks", map[string]any{"instrument": "guitar"})
	bt := a.ok(a.alice, "POST", "/api/v1/songs/"+sid+"/tracks", map[string]any{"instrument": "bass"})
	g, b := id(gt), id(bt)
	takes := func(track string) map[float64]map[string]any {
		out := map[float64]map[string]any{}
		for _, x := range a.ok(a.alice, "GET", "/api/v1/songs/"+sid, nil)["tracks"].([]any) {
			tr := x.(map[string]any)
			if id(tr) != track {
				continue
			}
			for _, k := range tr["takes"].([]any) {
				km := k.(map[string]any)
				out[num(km, "id")] = km
			}
		}
		return out
	}
	// a verse at 0, then a chorus at 10 s: two parts, both play
	_, v1 := a.upload(a.alice, "/api/v1/tracks/"+g+"/takes", map[string]any{"duration_ms": 500, "offset_ms": 0}, tone(0.5), "take.wav")
	_, c1 := a.upload(a.alice, "/api/v1/tracks/"+g+"/takes", map[string]any{"duration_ms": 500, "offset_ms": 10000}, tone(0.5), "take.wav")
	if num(v1, "part") == num(c1, "part") || v1["plays"] != true || c1["plays"] != true {
		t.Fatalf("two parts: %v %v", v1, c1)
	}
	// the verse again, over the first: another attempt at it — it plays, the first waits
	_, v2 := a.upload(a.alice, "/api/v1/tracks/"+g+"/takes", map[string]any{"duration_ms": 500, "offset_ms": 30}, tone(0.5), "take.wav")
	tk := takes(g)
	if num(v2, "part") != num(v1, "part") || tk[num(v1, "id")]["plays"] != false || tk[num(v2, "id")]["plays"] != true || tk[num(c1, "id")]["plays"] != true {
		t.Fatalf("attempt: %v", tk)
	}
	// a file brought in over the verse is a part of its own
	_, f1 := a.upload(a.alice, "/api/v1/tracks/"+g+"/takes", map[string]any{"duration_ms": 500, "source": "upload"}, tone(0.5), "take.wav")
	if num(f1, "part") == num(v1, "part") {
		t.Fatalf("an upload joined a part: %v", f1)
	}
	a.ok(a.alice, "DELETE", "/api/v1/takes/"+id(f1), nil)
	// pick the first verse back
	a.ok(a.alice, "PUT", "/api/v1/takes/"+id(v1), map[string]any{"plays": true})
	tk = takes(g)
	if tk[num(v1, "id")]["plays"] != true || tk[num(v2, "id")]["plays"] != false {
		t.Fatalf("pick: %v", tk)
	}
	// song length: the chorus ends last
	if l := num(a.ok(a.alice, "GET", "/api/v1/songs/"+sid, nil), "length_ms"); l != 10500 {
		t.Fatalf("length %v", l)
	}
	// the verse part (both attempts) goes to the bass, 2 s later, on top
	_, kb := a.upload(a.alice, "/api/v1/tracks/"+b+"/takes", map[string]any{"duration_ms": 500, "offset_ms": 2000}, tone(0.5), "take.wav")
	mv := a.ok(a.alice, "PUT", "/api/v1/parts/"+itoa(int64(num(v1, "part"))), map[string]any{"track_id": num(bt, "id"), "shift_ms": 2000})
	if len(mv["takes"].([]any)) != 2 {
		t.Fatalf("moved: %v", mv)
	}
	tb := takes(b)
	if len(tb) != 3 || num(tb[num(v1, "id")], "offset_ms") != 2000 || num(tb[num(v2, "id")], "offset_ms") != 2030 || num(tb[num(v1, "id")], "layer") <= num(tb[num(kb, "id")], "layer") {
		t.Fatalf("bass after the move: %v", tb)
	}
	if tb[num(kb, "id")]["plays"] != true || tb[num(v1, "id")]["plays"] != true {
		t.Fatalf("both parts play on the bass: %v", tb)
	}
	if tg := takes(g); len(tg) != 1 {
		t.Fatalf("guitar keeps the chorus only: %v", tg)
	}
	// deleting the playing attempt: the other one plays; deleting that too: the part is gone
	a.ok(a.alice, "DELETE", "/api/v1/takes/"+id(v1), nil)
	if takes(b)[num(v2, "id")]["plays"] != true {
		t.Fatalf("the other attempt plays")
	}
	// older clients: PUT /takes track_id moves the part
	a.ok(a.alice, "PUT", "/api/v1/takes/"+id(c1), map[string]any{"track_id": num(bt, "id")})
	if len(takes(g)) != 0 || len(takes(b)) != 3 {
		t.Fatalf("old move")
	}
	// not to another song's track, nor someone else's part
	other := id(a.ok(a.alice, "POST", "/api/v1/songs", map[string]any{"title": "Other"}))
	ot := a.ok(a.alice, "POST", "/api/v1/songs/"+other+"/tracks", map[string]any{"instrument": "voice"})
	a.expect(404, a.alice, "PUT", "/api/v1/parts/"+itoa(int64(num(kb, "part"))), map[string]any{"track_id": num(ot, "id")})
	a.expect(404, a.bob, "PUT", "/api/v1/parts/"+itoa(int64(num(kb, "part"))), map[string]any{"shift_ms": 10})
}
