package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"math"
	"math/rand"
	"time"

	"github.com/abjelosevic88/pocket-homelab-jam/internal/store"
)

// seedDemo fills the owner's account with songs that really play — drums,
// bass, plucked guitar chords, keys and a sung-like melody, all made right
// here — with a few takes each, a mix, two ideas not in a song yet, and two
// months of time spent (DEMO_DATA=true): for trying it out, CI and the
// screenshots.
func (s *Server) seedDemo(ctx context.Context, uid int64) error {
	ctx = context.WithoutCancel(ctx)
	if _, n, _ := s.store.ListSongs(ctx, uid, store.SongFilter{Limit: 1}); n > 0 {
		return nil
	}
	rnd := rand.New(rand.NewSource(42))
	now := time.Now()
	ago := func(d, h int) string {
		return now.AddDate(0, 0, -d).Add(-time.Duration(h) * time.Hour).UTC().Format(time.RFC3339Nano)
	}

	type demoTrack struct {
		name, instrument string
		volume, pan      int
		takes            int // how many takes (the last one plays)
		make             func(sy *synth, take int) []float64
		muted            bool
	}
	type demoSong struct {
		title, artist, key, notes string
		tempo                     float64
		beats, bars               int
		starred                   bool
		color                     string
		daysAgo                   int
		chords                    []chord
		tracks                    []demoTrack
		mix                       bool
	}
	am := []chord{{57, []int{0, 3, 7}}, {53, []int{0, 4, 7}}, {48, []int{0, 4, 7}}, {55, []int{0, 4, 7}}}
	blues := []chord{{52, []int{0, 4, 7, 10}}, {52, []int{0, 4, 7, 10}}, {57, []int{0, 4, 7, 10}}, {52, []int{0, 4, 7, 10}}, {59, []int{0, 4, 7, 10}}, {57, []int{0, 4, 7, 10}}, {52, []int{0, 4, 7, 10}}, {59, []int{0, 4, 7, 10}}}
	lull := []chord{{60, []int{0, 4, 7}}, {57, []int{0, 3, 7}}, {53, []int{0, 4, 7}}, {55, []int{0, 4, 7}}}
	songs := []demoSong{
		{title: "Sunday Riff", artist: "", key: "A minor", notes: "Verse riff from Sunday.\nTry the bass an octave lower in the second half.\nVoice: hum the melody first, words later.", tempo: 96, beats: 4, bars: 8, starred: true, color: "#e8590c", daysAgo: 0, chords: am, mix: true,
			tracks: []demoTrack{
				{"Drums", "drums", 90, 0, 1, func(sy *synth, _ int) []float64 { return sy.drums(false) }, false},
				{"Bass", "bass", 95, -10, 2, func(sy *synth, _ int) []float64 { return sy.bass() }, false},
				{"Guitar", "guitar", 85, 30, 3, func(sy *synth, t int) []float64 { return sy.strum(t) }, false},
				{"Voice", "voice", 100, 0, 2, func(sy *synth, t int) []float64 { return sy.melody([]int{69, 72, 74, 72, 69, 67, 69, -1}, t) }, false},
			}},
		{title: "Blues in E", artist: "Jam with Marko", key: "E", notes: "Shuffle feel. Marko on bass next time.", tempo: 112, beats: 4, bars: 8, daysAgo: 3, chords: blues, mix: true,
			tracks: []demoTrack{
				{"Backing", "backing", 80, 0, 1, func(sy *synth, _ int) []float64 { return mixDown(sy.drums(true), sy.bass()) }, false},
				{"Lead guitar", "guitar", 100, 20, 4, func(sy *synth, t int) []float64 { return sy.melody([]int{76, 79, 81, 79, 76, 74, 76, -1}, t+2) }, false},
				{"Rhythm guitar", "guitar", 70, -35, 1, func(sy *synth, t int) []float64 { return sy.strum(t) }, true},
			}},
		{title: "Lullaby", artist: "", key: "C", notes: "For Mila. Slow, 3/4.", tempo: 72, beats: 3, bars: 8, starred: true, color: "#1c7ed6", daysAgo: 6, chords: lull,
			tracks: []demoTrack{
				{"Piano", "keys", 90, -15, 2, func(sy *synth, _ int) []float64 { return sy.keys() }, false},
				{"Voice", "voice", 100, 10, 3, func(sy *synth, t int) []float64 { return sy.melody([]int{72, 74, 76, 74, 72, 69, 67, 72}, t) }, false},
			}},
		{title: "Scale warm-up", artist: "", key: "G major", notes: "Two octaves, up and down. Up the tempo by 4 when it is clean.", tempo: 80, beats: 4, bars: 4, daysAgo: 9, chords: []chord{{55, []int{0, 4, 7}}},
			tracks: []demoTrack{
				{"Guitar", "guitar", 100, 0, 2, func(sy *synth, t int) []float64 {
					return sy.melody([]int{67, 69, 71, 72, 74, 76, 78, 79}, t)
				}, false},
			}},
		{title: "Night drive", artist: "", key: "D minor", notes: "Idea: drums + keys first, then a bass line.", tempo: 88, beats: 4, bars: 4, daysAgo: 14, chords: []chord{{50, []int{0, 3, 7}}, {58, []int{0, 4, 7}}},
			tracks: []demoTrack{
				{"Drums", "drums", 85, 0, 1, func(sy *synth, _ int) []float64 { return sy.drums(false) }, false},
				{"Keys", "keys", 80, 0, 1, func(sy *synth, _ int) []float64 { return sy.keys() }, false},
				{"Bass", "bass", 100, 0, 0, nil, false},
			}},
		{title: "Hallelujah", artist: "Leonard Cohen (cover)", key: "C", notes: "Capo 5? Try it lower for the voice.", tempo: 60, beats: 3, bars: 0, daysAgo: 20,
			tracks: []demoTrack{{"Guitar", "guitar", 100, 0, 0, nil, false}, {"Voice", "voice", 100, 0, 0, nil, false}}},
	}

	songIDs := []int64{}
	for si, d := range songs {
		so := &store.Song{UserID: uid, Title: d.title, Artist: d.artist, KeyName: d.key, Notes: d.notes, Tempo: d.tempo, Beats: d.beats, BeatUnit: 4, CountIn: 1, Click: true,
			Starred: d.starred, Color: d.color, CreatedAt: ago(d.daysAgo+20+si*3, 2)}
		if err := s.store.CreateSong(ctx, so); err != nil {
			return err
		}
		songIDs = append(songIDs, so.ID)
		var mixParts [][]float64
		var pans, vols []int
		for _, dt := range d.tracks {
			t := &store.Track{UserID: uid, SongID: so.ID, Name: dt.name, Instrument: dt.instrument, Volume: dt.volume, Pan: dt.pan, Muted: dt.muted}
			if err := s.store.CreateTrack(ctx, t); err != nil {
				return err
			}
			if dt.make == nil || d.bars == 0 {
				continue
			}
			var last []float64
			for n := 1; n <= dt.takes; n++ {
				sy := newSynth(d.tempo, d.beats, d.bars, d.chords, int64(si*100+n))
				pcm := dt.make(sy, n)
				// later takes are a little tighter; the first one starts a hair late
				offset := int64(0)
				if n == 1 && dt.takes > 1 {
					offset = int64(rnd.Intn(40) + 10)
				}
				rel, size, err := s.saveMedia(uid, ".wav", bytes.NewReader(wav(pcm, demoRate, 1)), maxAudioBytes)
				if err != nil {
					return err
				}
				k := &store.Take{UserID: uid, SongID: so.ID, TrackID: t.ID, File: rel, Mime: "audio/wav", Size: size, DurationMS: int64(len(pcm)) * 1000 / demoRate,
					OffsetMS: offset, LatencyMS: 42, SampleRate: demoRate, Channels: 1, Peaks: peaks(pcm, demoRate), Source: "recorded", Device: "iPhone microphone",
					Liked: n == dt.takes && dt.takes > 2, CreatedAt: ago(d.daysAgo+dt.takes-n, 3-n%3)}
				if err := s.store.CreateTake(ctx, k); err != nil {
					return err
				}
				last = pcm
			}
			if !dt.muted {
				mixParts = append(mixParts, last)
				pans, vols = append(pans, dt.pan), append(vols, dt.volume)
			}
		}
		if d.mix && len(mixParts) > 0 {
			st := stereoMix(mixParts, pans, vols)
			rel, size, err := s.saveMedia(uid, ".wav", bytes.NewReader(wav(st, demoRate, 2)), maxAudioBytes)
			if err != nil {
				return err
			}
			mono := make([]float64, len(st)/2)
			for i := range mono {
				mono[i] = math.Max(math.Abs(st[2*i]), math.Abs(st[2*i+1]))
			}
			id := so.ID
			m := &store.Mix{UserID: uid, SongID: &id, Name: d.title + " — mix 1", File: rel, Mime: "audio/wav", Size: size, DurationMS: int64(len(mono)) * 1000 / demoRate,
				Peaks: peaks(mono, demoRate), Tracks: len(mixParts), CreatedAt: ago(d.daysAgo, 1)}
			if err := s.store.CreateMix(ctx, m); err != nil {
				return err
			}
		}
		if d.daysAgo < 7 {
			s.store.TouchSong(ctx, uid, so.ID, true)
		}
	}

	// two ideas not in a song yet: a hummed hook and a riff
	ideas := []struct {
		name    string
		daysAgo int
		make    func() []float64
	}{
		{"Hook for the chorus", 1, func() []float64 {
			return newSynth(84, 4, 2, am, 7).melody([]int{72, 74, 76, 79, 76, 74, 72, -1}, 1)
		}},
		{"Riff in D", 4, func() []float64 {
			return newSynth(100, 4, 2, []chord{{50, []int{0, 3, 7}}, {53, []int{0, 4, 7}}}, 8).strum(2)
		}},
	}
	for i, d := range ideas {
		pcm := d.make()
		rel, size, err := s.saveMedia(uid, ".wav", bytes.NewReader(wav(pcm, demoRate, 1)), maxAudioBytes)
		if err != nil {
			return err
		}
		x := &store.Idea{UserID: uid, Name: d.name, File: rel, Mime: "audio/wav", Size: size, DurationMS: int64(len(pcm)) * 1000 / demoRate, SampleRate: demoRate, Channels: 1,
			Peaks: peaks(pcm, demoRate), Source: "recorded", Device: "iPhone microphone", CreatedAt: isoUTC(now.AddDate(0, 0, -d.daysAgo).Add(-time.Duration(2+i) * time.Hour))}
		if err := s.store.CreateIdea(ctx, x); err != nil {
			return err
		}
	}

	// two months of playing: most days, 10 to 70 minutes, one or two songs
	for d := 60; d >= 0; d-- {
		if rnd.Float64() < 0.3 && d != 0 && d != 1 {
			continue
		}
		n := 1 + rnd.Intn(2)
		for j := 0; j < n; j++ {
			i := rnd.Intn(len(songs) - 1)
			if d < 7 {
				i = rnd.Intn(3)
			}
			id := songIDs[i]
			start := time.Date(now.Year(), now.Month(), now.Day(), 18+j*2, rnd.Intn(50), 0, 0, time.Local).AddDate(0, 0, -d)
			sec := (10 + rnd.Intn(55)) * 60
			x := &store.Sitting{UserID: uid, SongID: &id, Title: songs[i].title, Day: start.Format("2006-01-02"), StartedAt: start.UTC().Format(time.RFC3339Nano),
				LastAt: start.Add(time.Duration(sec) * time.Second).UTC().Format(time.RFC3339Nano), Seconds: sec, Takes: rnd.Intn(5)}
			if err := s.store.InsertSitting(ctx, x); err != nil {
				return err
			}
		}
	}
	s.log.Info("demo data added", "songs", len(songs))
	return nil
}

// ---- a tiny synthesiser for the demo songs ----

const demoRate = 22050

type chord struct {
	root  int // MIDI note
	notes []int
}

type synth struct {
	tempo     float64
	beats     int
	bars      int
	chords    []chord
	rnd       *rand.Rand
	beatLen   int
	total     int
	humanizeS float64
}

func newSynth(tempo float64, beats, bars int, chords []chord, seed int64) *synth {
	bl := int(float64(demoRate) * 60 / tempo)
	return &synth{tempo: tempo, beats: beats, bars: bars, chords: chords, rnd: rand.New(rand.NewSource(seed)), beatLen: bl, total: bl*beats*bars + demoRate/2}
}

func (sy *synth) chordAt(bar int) chord { return sy.chords[bar%len(sy.chords)] }

func midiHz(n float64) float64 { return 440 * math.Pow(2, (n-69)/12) }

// pluck adds a Karplus–Strong plucked string.
func (sy *synth) pluck(out []float64, at int, hz, amp, decay float64, length int) {
	p := int(float64(demoRate) / hz)
	if p < 2 {
		return
	}
	buf := make([]float64, p)
	for i := range buf {
		buf[i] = sy.rnd.Float64()*2 - 1
	}
	for i := 0; i < length && at+i < len(out); i++ {
		j := i % p
		v := buf[j]
		buf[j] = decay * 0.5 * (buf[j] + buf[(j+1)%p])
		if at+i >= 0 {
			out[at+i] += v * amp
		}
	}
}

// strum plays the chords as down/up strums on the beats.
func (sy *synth) strum(take int) []float64 {
	out := make([]float64, sy.total)
	for bar := 0; bar < sy.bars; bar++ {
		c := sy.chordAt(bar)
		for b := 0; b < sy.beats; b++ {
			at := (bar*sy.beats+b)*sy.beatLen + sy.rnd.Intn(300/take+40)
			for i, n := range c.notes {
				note := float64(c.root + n)
				if note < 52 {
					note += 12
				}
				sy.pluck(out, at+i*90, midiHz(note), 0.22, 0.996, sy.beatLen*2)
			}
			if b%2 == 1 {
				for i := len(c.notes) - 1; i >= 0; i-- {
					sy.pluck(out, at+sy.beatLen/2+(len(c.notes)-1-i)*70, midiHz(float64(c.root+c.notes[i]+12)), 0.12, 0.994, sy.beatLen)
				}
			}
		}
	}
	return normalise(out, 0.8)
}

// bass plays the root on 1 and 3 (and the fifth before the next bar).
func (sy *synth) bass() []float64 {
	out := make([]float64, sy.total)
	for bar := 0; bar < sy.bars; bar++ {
		c := sy.chordAt(bar)
		root := float64(c.root)
		for root >= 45 {
			root -= 12
		}
		for b := 0; b < sy.beats; b++ {
			at := (bar*sy.beats + b) * sy.beatLen
			n := root
			if b == sy.beats-1 {
				n += 7
			}
			if b%2 == 1 && b != sy.beats-1 {
				continue
			}
			hz := midiHz(n)
			l := sy.beatLen * 2
			for i := 0; i < l && at+i < len(out); i++ {
				t := float64(i) / demoRate
				env := math.Exp(-t*3) * math.Min(1, t*200)
				out[at+i] += env * (0.7*math.Sin(2*math.Pi*hz*t) + 0.25*math.Sin(4*math.Pi*hz*t) + 0.1*math.Sin(6*math.Pi*hz*t))
			}
		}
	}
	return normalise(out, 0.8)
}

// keys plays soft chords once a bar, with a broken-chord arpeggio.
func (sy *synth) keys() []float64 {
	out := make([]float64, sy.total)
	barLen := sy.beatLen * sy.beats
	for bar := 0; bar < sy.bars; bar++ {
		c := sy.chordAt(bar)
		for i, n := range c.notes {
			hz := midiHz(float64(c.root + n))
			at := bar*barLen + i*sy.beatLen/2
			for k := 0; k < barLen && at+k < len(out); k++ {
				t := float64(k) / demoRate
				env := math.Exp(-t*1.6) * math.Min(1, t*300)
				out[at+k] += 0.3 * env * (math.Sin(2*math.Pi*hz*t) + 0.3*math.Sin(4*math.Pi*hz*t) + 0.08*math.Sin(6*math.Pi*hz*t))
			}
		}
	}
	return normalise(out, 0.75)
}

// melody sings (or plays) one note a beat-pair, with vibrato; -1 is a rest.
func (sy *synth) melody(notes []int, take int) []float64 {
	out := make([]float64, sy.total)
	step := sy.beatLen * 2
	if sy.beats == 3 {
		step = sy.beatLen * 3
	}
	steps := sy.beatLen * sy.beats * sy.bars / step
	for i := 0; i < steps; i++ {
		n := notes[i%len(notes)]
		if n < 0 {
			continue
		}
		hz := midiHz(float64(n))
		at := i*step + sy.rnd.Intn(500/take+50)
		l := step - step/8
		phase := 0.0
		for k := 0; k < l && at+k < len(out); k++ {
			t := float64(k) / demoRate
			env := math.Min(1, t*12) * math.Min(1, float64(l-k)/(demoRate*0.08))
			vib := 1 + 0.006*math.Sin(2*math.Pi*5.5*t)*math.Min(1, t*2)
			phase += 2 * math.Pi * hz * vib / demoRate
			out[at+k] += 0.5 * env * (math.Sin(phase) + 0.45*math.Sin(2*phase) + 0.2*math.Sin(3*phase) + 0.08*math.Sin(5*phase))
		}
	}
	return normalise(out, 0.7)
}

// drums plays kick, snare and hi-hats (shuffle swings the off-beats).
func (sy *synth) drums(shuffle bool) []float64 {
	out := make([]float64, sy.total)
	hit := func(at int, kind string, amp float64) {
		switch kind {
		case "kick":
			ph := 0.0
			for k := 0; k < demoRate/4 && at+k < len(out); k++ {
				t := float64(k) / demoRate
				ph += 2 * math.Pi * (50 + 90*math.Exp(-t*30)) / demoRate
				out[at+k] += amp * math.Exp(-t*14) * math.Sin(ph)
			}
		case "snare":
			for k := 0; k < demoRate/6 && at+k < len(out); k++ {
				t := float64(k) / demoRate
				out[at+k] += amp * math.Exp(-t*22) * (0.7*(sy.rnd.Float64()*2-1) + 0.4*math.Sin(2*math.Pi*190*t))
			}
		case "hat":
			prev := 0.0
			for k := 0; k < demoRate/25 && at+k < len(out); k++ {
				t := float64(k) / demoRate
				n := sy.rnd.Float64()*2 - 1
				out[at+k] += amp * math.Exp(-t*90) * (n - prev)
				prev = n
			}
		}
	}
	for bar := 0; bar < sy.bars; bar++ {
		for b := 0; b < sy.beats; b++ {
			at := (bar*sy.beats + b) * sy.beatLen
			if b == 0 || (sy.beats == 4 && b == 2) {
				hit(at, "kick", 0.9)
			}
			if (sy.beats == 4 && (b == 1 || b == 3)) || (sy.beats == 3 && b > 0) {
				hit(at, "snare", 0.55)
			}
			hit(at, "hat", 0.25)
			off := sy.beatLen / 2
			if shuffle {
				off = sy.beatLen * 2 / 3
			}
			hit(at+off, "hat", 0.15)
		}
	}
	return normalise(out, 0.85)
}

func normalise(x []float64, to float64) []float64 {
	m := 0.0
	for _, v := range x {
		m = math.Max(m, math.Abs(v))
	}
	if m == 0 {
		return x
	}
	for i := range x {
		x[i] = x[i] / m * to
	}
	return x
}

func mixDown(parts ...[]float64) []float64 {
	n := 0
	for _, p := range parts {
		n = max(n, len(p))
	}
	out := make([]float64, n)
	for _, p := range parts {
		for i, v := range p {
			out[i] += v * 0.6
		}
	}
	return normalise(out, 0.85)
}

// stereoMix pans and mixes mono tracks into interleaved stereo.
func stereoMix(parts [][]float64, pans, vols []int) []float64 {
	n := 0
	for _, p := range parts {
		n = max(n, len(p))
	}
	out := make([]float64, 2*n)
	for i, p := range parts {
		pan := float64(pans[i]) / 100
		l, r := math.Cos((pan+1)*math.Pi/4), math.Sin((pan+1)*math.Pi/4)
		g := float64(vols[i]) / 100 * 0.5
		for j, v := range p {
			out[2*j] += v * g * l
			out[2*j+1] += v * g * r
		}
	}
	return normalise(out, 0.9)
}

// wav encodes samples (-1..1, interleaved when stereo) as 16-bit PCM WAV.
func wav(pcm []float64, rate, channels int) []byte {
	var b bytes.Buffer
	data := len(pcm) * 2
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+data))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, uint32(16))
	_ = binary.Write(&b, binary.LittleEndian, uint16(1))
	_ = binary.Write(&b, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&b, binary.LittleEndian, uint32(rate))
	_ = binary.Write(&b, binary.LittleEndian, uint32(rate*channels*2))
	_ = binary.Write(&b, binary.LittleEndian, uint16(channels*2))
	_ = binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(data))
	for _, v := range pcm {
		v = math.Max(-1, math.Min(1, v))
		_ = binary.Write(&b, binary.LittleEndian, int16(v*32767))
	}
	return b.Bytes()
}

// peaks is the waveform the app draws: the loudest sample in each 1/50 s,
// 0..255, base64 — the same as the browser makes for a recording.
func peaks(pcm []float64, rate int) string {
	step := rate / 50
	out := make([]byte, 0, len(pcm)/step+1)
	for i := 0; i < len(pcm); i += step {
		m := 0.0
		for j := i; j < i+step && j < len(pcm); j++ {
			m = math.Max(m, math.Abs(pcm[j]))
		}
		out = append(out, byte(math.Min(255, math.Round(m*255))))
	}
	return base64.StdEncoding.EncodeToString(out)
}
