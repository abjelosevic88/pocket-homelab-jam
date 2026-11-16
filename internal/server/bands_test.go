package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestBandsAndMarks(t *testing.T) {
	a := newApp(t, nil)
	band := a.ok(a.alice, "POST", "/api/v1/bands", map[string]any{"name": "The Trio", "color": "#336699"})
	bid := id(band)
	// the same name (any case) is the same band
	if again := a.ok(a.alice, "POST", "/api/v1/bands", map[string]any{"name": "the trio"}); id(again) != bid {
		t.Fatalf("same band twice: %v", again)
	}
	a.expect(400, a.alice, "POST", "/api/v1/bands", map[string]any{"name": " "})

	so := a.ok(a.alice, "POST", "/api/v1/songs", map[string]any{"title": "Opener", "band_id": n(bid)})
	sid := id(so)
	if so["band_name"] != "The Trio" {
		t.Fatalf("song in a band: %v", so)
	}
	// by name: a new band is made
	so2 := a.ok(a.alice, "POST", "/api/v1/songs", map[string]any{"title": "Solo thing", "band_name": "Me alone"})
	if so2["band_name"] != "Me alone" {
		t.Fatalf("band by name: %v", so2)
	}
	a.ok(a.alice, "POST", "/api/v1/songs", map[string]any{"title": "Loose"})
	a.expect(400, a.bob, "POST", "/api/v1/songs", map[string]any{"title": "x", "band_id": n(bid)}) // not Bob's band

	count := func(q string) int { return len(a.ok(a.alice, "GET", "/api/v1/songs?"+q, nil)["songs"].([]any)) }
	if count("band="+bid) != 1 || count("band=none") != 1 || count("") != 3 {
		t.Fatalf("band filter: %d %d %d", count("band="+bid), count("band=none"), count(""))
	}
	bands := a.ok(a.alice, "GET", "/api/v1/bands", nil)["bands"].([]any)
	if len(bands) != 2 || num(bands[1].(map[string]any), "songs") != 1 {
		t.Fatalf("bands: %v", bands)
	}

	// marks: on the whole song and on a track, a moment and a stretch
	tid := id(a.ok(a.alice, "POST", "/api/v1/songs/"+sid+"/tracks", map[string]any{"instrument": "voice"}))
	m1 := a.ok(a.alice, "POST", "/api/v1/songs/"+sid+"/marks", map[string]any{"at_ms": 12000, "text": "Chorus"})
	m2 := a.ok(a.alice, "POST", "/api/v1/songs/"+sid+"/marks", map[string]any{"at_ms": 3000, "end_ms": 5000, "text": "too loud", "track_id": n(tid)})
	if m1["track_id"] != nil || num(m2, "end_ms") != 5000 {
		t.Fatalf("marks: %v %v", m1, m2)
	}
	a.expect(400, a.alice, "POST", "/api/v1/songs/"+sid+"/marks", map[string]any{"at_ms": -1})
	a.expect(400, a.alice, "POST", "/api/v1/songs/"+sid+"/marks", map[string]any{"at_ms": 1, "track_id": 99999})
	a.expect(404, a.bob, "PUT", "/api/v1/marks/"+id(m1), map[string]any{"text": "mine"})
	up := a.ok(a.alice, "PUT", "/api/v1/marks/"+id(m1), map[string]any{"text": "Chorus 1", "end_ms": 100}) // an end before the start is a moment
	if up["text"] != "Chorus 1" || num(up, "end_ms") != 0 {
		t.Fatalf("mark update: %v", up)
	}
	full := a.ok(a.alice, "GET", "/api/v1/songs/"+sid, nil)
	marks := full["marks"].([]any)
	if len(marks) != 2 || marks[0].(map[string]any)["text"] != "too loud" {
		t.Fatalf("marks in time order: %v", marks)
	}
	if count("q=chorus") != 1 || count("q=trio") != 1 {
		t.Fatal("search finds notes on the timeline and band names")
	}

	// export into Bob: bands, songs in them, marks on the right track
	req, _ := http.NewRequest("GET", a.url+"/api/v1/export.json", nil)
	req.Header.Set("Authorization", "Bearer "+a.alice)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := io.ReadAll(res.Body)
	res.Body.Close()
	imp := func() map[string]any {
		req, _ := http.NewRequest("POST", a.url+"/api/v1/import", bytes.NewReader(doc))
		req.Header.Set("Authorization", "Bearer "+a.bob)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var m map[string]any
		_ = json.NewDecoder(res.Body).Decode(&m)
		return m["added"].(map[string]any)
	}
	if got := imp(); num(got, "bands") != 2 || num(got, "notes") != 2 {
		t.Fatalf("import: %v", got)
	}
	if got := imp(); num(got, "notes") != 0 || num(got, "bands") != 0 {
		t.Fatalf("import twice: %v", got)
	}
	bb := a.ok(a.bob, "GET", "/api/v1/bands", nil)["bands"].([]any)
	var trio string
	for _, b := range bb {
		if b.(map[string]any)["name"] == "The Trio" {
			trio = id(b.(map[string]any))
		}
	}
	bs := a.ok(a.bob, "GET", "/api/v1/songs?band="+trio, nil)["songs"].([]any)
	if len(bs) != 1 {
		t.Fatalf("Bob's trio songs: %v", bs)
	}
	bf := a.ok(a.bob, "GET", "/api/v1/songs/"+id(bs[0].(map[string]any)), nil)
	btid := bf["tracks"].([]any)[0].(map[string]any)["id"]
	if bm := bf["marks"].([]any)[0].(map[string]any); bm["track_id"] != btid {
		t.Fatalf("Bob's mark on his track: %v vs %v", bm, btid)
	}

	// deleting the band keeps its song
	a.ok(a.alice, "DELETE", "/api/v1/bands/"+bid, nil)
	if s := a.ok(a.alice, "GET", "/api/v1/songs/"+sid, nil); s["band_id"] != nil {
		t.Fatalf("song after its band went: %v", s)
	}
	a.ok(a.alice, "DELETE", "/api/v1/marks/"+id(m2), nil)
}
