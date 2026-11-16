package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-jam/internal/store"
)

// The export is everything a person made: songs with their tracks and takes,
// mixes, ideas and the time spent. export.json has no audio; export.zip
// carries the audio files too (audio/<file>). Importing either twice changes
// nothing; takes, mixes and ideas come back only from the .zip.

type exportDoc struct {
	App        string          `json:"app"`
	Version    string          `json:"version"`
	ExportedAt string          `json:"exported_at"`
	Bands      []store.Band    `json:"bands"`
	Songs      []store.Song    `json:"songs"` // with tracks, takes and marks (notes on the timeline)
	Mixes      []exportMix     `json:"mixes"`
	Ideas      []exportIdea    `json:"ideas"`
	Sittings   []store.Sitting `json:"sittings"`
}

// exportMix and exportIdea carry the audio file's name inside the .zip.
type exportMix struct {
	store.Mix
	Audio string `json:"audio"`
}

type exportIdea struct {
	store.Idea
	Audio string `json:"audio"`
}

func audioName(rel string) string { return "audio/" + path.Base(rel) }

func (s *Server) export(ctx context.Context, u *store.User) (*exportDoc, error) {
	doc := &exportDoc{App: "pocket-jam", Version: Version, ExportedAt: store.Now(), Bands: []store.Band{}, Songs: []store.Song{}, Mixes: []exportMix{}, Ideas: []exportIdea{}}
	bands, err := s.store.ListBands(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	doc.Bands = bands
	list, _, err := s.store.ListSongs(ctx, u.ID, store.SongFilter{Sort: "added", Limit: 1000000})
	if err != nil {
		return nil, err
	}
	for i := len(list) - 1; i >= 0; i-- {
		so, err := s.store.SongWithTracks(ctx, u.ID, list[i].ID)
		if err != nil {
			return nil, err
		}
		doc.Songs = append(doc.Songs, *so)
	}
	mixes, err := s.store.ListMixes(ctx, u.ID, 0, 500)
	if err != nil {
		return nil, err
	}
	for _, m := range mixes {
		doc.Mixes = append(doc.Mixes, exportMix{Mix: m, Audio: audioName(m.File)})
	}
	ideas, err := s.store.ListIdeas(ctx, u.ID, 0, 100000)
	if err != nil {
		return nil, err
	}
	for _, x := range ideas {
		doc.Ideas = append(doc.Ideas, exportIdea{Idea: x, Audio: audioName(x.File)})
	}
	if doc.Sittings, err = s.store.ListSittings(ctx, u.ID, "0000-00-00", "9999-12-31"); err != nil {
		return nil, err
	}
	return doc, nil
}

// takeAudio is the name of each take's file, by take id (the JSON hides the path).
func (s *Server) takeAudio(ctx context.Context, uid int64) map[int64]string {
	out := map[int64]string{}
	songs, _, _ := s.store.ListSongs(ctx, uid, store.SongFilter{Limit: 1000000})
	for _, so := range songs {
		takes, _ := s.store.ListTakes(ctx, uid, so.ID)
		for _, k := range takes {
			out[k.ID] = k.File
		}
	}
	return out
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	doc, err := s.export(ctx, userFrom(ctx))
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	if queryBool(r, "audio") {
		s.exportZip(w, r, doc)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="pocket-jam-%s.json"`, time.Now().Format("2006-01-02")))
	_ = json.NewEncoder(w).Encode(doc)
}

// exportZip writes jam.json plus every audio file.
func (s *Server) exportZip(w http.ResponseWriter, r *http.Request, doc *exportDoc) {
	ctx := r.Context()
	files := s.takeAudio(ctx, actorID(ctx))
	takeFiles := map[string]string{} // take id -> audio name, inside jam.json
	for id, rel := range files {
		takeFiles[strconv.FormatInt(id, 10)] = audioName(rel)
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="pocket-jam-%s.zip"`, time.Now().Format("2006-01-02")))
	zw := zip.NewWriter(w)
	if f, err := zw.Create("jam.json"); err == nil {
		_ = json.NewEncoder(f).Encode(struct {
			*exportDoc
			TakeAudio map[string]string `json:"take_audio"`
		}{doc, takeFiles})
	}
	add := func(rel string) {
		in, err := os.Open(s.mediaPath(rel))
		if err != nil {
			return
		}
		defer in.Close()
		if fw, err := zw.CreateHeader(&zip.FileHeader{Name: audioName(rel), Method: zip.Store, Modified: time.Now()}); err == nil {
			_, _ = io.Copy(fw, in)
		}
	}
	for _, rel := range files {
		add(rel)
	}
	for _, m := range doc.Mixes {
		add(m.File)
	}
	for _, x := range doc.Ideas {
		add(x.File)
	}
	_ = zw.Close()
}

// handleExportCSV is one row per sitting (the time spent).
func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="pocket-jam-%s.csv"`, time.Now().Format("2006-01-02")))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"day", "started", "song", "minutes", "takes", "sent_to"})
	list, _ := s.store.ListSittings(ctx, actorID(ctx), "0000-00-00", "9999-12-31")
	for _, x := range list {
		t, _ := time.Parse(time.RFC3339Nano, x.StartedAt)
		_ = cw.Write([]string{x.Day, t.Local().Format("15:04"), x.Title, strconv.Itoa((x.Seconds + 30) / 60), strconv.Itoa(x.Takes), x.SentTo})
	}
	cw.Flush()
}

// handleImport takes a .json or a .zip export.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tmpDir := filepath.Join(s.cfg.DataDir, "tmp")
	_ = os.MkdirAll(tmpDir, 0o755)
	tf, err := os.CreateTemp(tmpDir, "import-*")
	if err != nil {
		s.fail(w, err, "import")
		return
	}
	defer func() { tf.Close(); _ = os.Remove(tf.Name()) }()
	size, err := io.Copy(tf, http.MaxBytesReader(w, r.Body, 16<<30))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "the file is too big or broke off")
		return
	}
	head := make([]byte, 2)
	_, _ = tf.ReadAt(head, 0)
	var doc struct {
		exportDoc
		TakeAudio map[string]string `json:"take_audio"`
	}
	var zr *zip.Reader
	if bytes.Equal(head, []byte("PK")) {
		zr, err = zip.NewReader(tf, size)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "that .zip cannot be opened")
			return
		}
		f, err := zr.Open("jam.json")
		if err != nil {
			writeErr(w, http.StatusBadRequest, "that .zip is not a Pocket Jam export (no jam.json)")
			return
		}
		err = json.NewDecoder(f).Decode(&doc)
		f.Close()
		if err != nil {
			writeErr(w, http.StatusBadRequest, "jam.json cannot be read")
			return
		}
	} else if _, err := tf.Seek(0, io.SeekStart); err != nil || json.NewDecoder(tf).Decode(&doc) != nil {
		writeErr(w, http.StatusBadRequest, "that is not a Pocket Jam export")
		return
	}
	if doc.App != "pocket-jam" {
		writeErr(w, http.StatusBadRequest, "that is not a Pocket Jam export")
		return
	}
	res, err := s.importDoc(ctx, userFrom(ctx), &doc.exportDoc, doc.TakeAudio, zr)
	if err != nil {
		s.userFail(w, err, "import")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func songKey(title, created string) string {
	return strings.ToLower(strings.TrimSpace(title)) + "|" + created
}

// importDoc adds what is not here yet: songs by title and when they were
// made, tracks by position, takes by number, mixes by name and time, ideas
// by client id (else name and time), sittings by start and song.
func (s *Server) importDoc(ctx context.Context, u *store.User, doc *exportDoc, takeAudio map[string]string, zr *zip.Reader) (map[string]any, error) {
	have, _, err := s.store.ListSongs(ctx, u.ID, store.SongFilter{Limit: 1000000})
	if err != nil {
		return nil, err
	}
	byKey := map[string]int64{}
	for _, so := range have {
		byKey[songKey(so.Title, so.CreatedAt)] = so.ID
	}
	added := map[string]int{"bands": 0, "songs": 0, "tracks": 0, "takes": 0, "notes": 0, "mixes": 0, "ideas": 0, "sittings": 0}
	for _, b := range doc.Bands {
		name := cleanText(b.Name, maxName)
		if name == "" || s.store.BandByName(ctx, u.ID, name) != nil {
			continue
		}
		nb := &store.Band{UserID: u.ID, Name: name, Notes: clip(b.Notes, maxBandNotes)}
		if reColor.MatchString(b.Color) {
			nb.Color = b.Color
		}
		if err := s.store.CreateBand(ctx, nb); err != nil {
			return nil, err
		}
		added["bands"]++
	}
	songMap := map[int64]int64{}
	readAudio := func(name, ext string) (string, int64, bool) {
		if zr == nil || name == "" {
			return "", 0, false
		}
		f, err := zr.Open(name)
		if err != nil {
			return "", 0, false
		}
		defer f.Close()
		rel, n, err := s.saveMedia(u.ID, firstNonEmpty(ext, filepath.Ext(name)), f, maxAudioBytes)
		return rel, n, err == nil
	}
	for _, in := range doc.Songs {
		id, ok := byKey[songKey(in.Title, in.CreatedAt)]
		if !ok {
			so := in
			so.ID, so.UserID, so.Source, so.BandID = 0, u.ID, "", nil
			if so.BandName != "" {
				name := so.BandName
				if band, _, m := s.bandFor(ctx, u.ID, nil, &name); m == "" {
					so.BandID = band
				}
			}
			if so.Title == "" {
				continue
			}
			if so.Tempo < 20 || so.Tempo > 400 {
				so.Tempo = 100
			}
			if so.Beats < 1 || so.Beats > 16 {
				so.Beats = 4
			}
			if so.BeatUnit == 0 {
				so.BeatUnit = 4
			}
			if err := s.store.CreateSong(ctx, &so); err != nil {
				return nil, err
			}
			id = so.ID
			byKey[songKey(in.Title, in.CreatedAt)] = id
			added["songs"]++
		}
		songMap[in.ID] = id
		cur, err := s.store.SongWithTracks(ctx, u.ID, id)
		if err != nil {
			return nil, err
		}
		trackMap := map[int64]int64{}
		for ti, t := range in.Tracks {
			var tr *store.Track
			if ti < len(cur.Tracks) {
				tr = &cur.Tracks[ti]
			} else {
				nt := t
				nt.ID, nt.UserID, nt.SongID, nt.TakeID = 0, u.ID, id, nil
				if !oneOf(nt.Instrument, instruments) {
					nt.Instrument = "other"
				}
				if err := s.store.CreateTrack(ctx, &nt); err != nil {
					return nil, err
				}
				nt.Takes = []store.Take{}
				tr = &nt
				added["tracks"]++
			}
			trackMap[t.ID] = tr.ID
			numbers := map[int]bool{}
			for _, k := range tr.Takes {
				numbers[k.Number] = true
			}
			var playing *int64
			for _, k := range t.Takes {
				if numbers[k.Number] {
					continue
				}
				rel, size, ok := readAudio(takeAudio[strconv.FormatInt(k.ID, 10)], "")
				if !ok {
					continue
				}
				nk := k
				nk.ID, nk.UserID, nk.SongID, nk.TrackID, nk.File, nk.Size = 0, u.ID, id, tr.ID, rel, size
				if err := s.store.CreateTake(ctx, &nk); err != nil {
					_ = os.Remove(s.mediaPath(rel))
					return nil, err
				}
				if t.TakeID != nil && *t.TakeID == k.ID {
					playing = &nk.ID
				}
				added["takes"]++
			}
			if playing != nil {
				if cur, err := s.store.GetTrack(ctx, u.ID, tr.ID); err == nil {
					cur.TakeID = playing
					_ = s.store.UpdateTrack(ctx, cur)
				}
			}
		}
		haveMark := map[string]bool{}
		markKey := func(m store.Mark) string {
			var t int64
			if m.TrackID != nil {
				t = *m.TrackID
			}
			return fmt.Sprintf("%d|%d|%s", t, m.AtMS, m.Text)
		}
		for _, m := range cur.Marks {
			haveMark[markKey(m)] = true
		}
		for _, m := range in.Marks {
			nm := m
			nm.ID, nm.UserID, nm.SongID, nm.TrackID = 0, u.ID, id, nil
			if m.TrackID != nil {
				t, ok := trackMap[*m.TrackID]
				if !ok {
					continue
				}
				nm.TrackID = &t
			}
			nm.Text = strings.TrimSpace(clip(nm.Text, maxMarkText))
			if !reColor.MatchString(nm.Color) {
				nm.Color = ""
			}
			if nm.AtMS < 0 || nm.AtMS > maxLengthMS || haveMark[markKey(nm)] || len(haveMark) >= maxMarks {
				continue
			}
			if nm.EndMS <= nm.AtMS || nm.EndMS > maxLengthMS {
				nm.EndMS = 0
			}
			if err := s.store.CreateMark(ctx, &nm); err != nil {
				return nil, err
			}
			haveMark[markKey(nm)] = true
			added["notes"]++
		}
	}
	mixes, _ := s.store.ListMixes(ctx, u.ID, 0, 100000)
	haveMix := map[string]bool{}
	for _, m := range mixes {
		haveMix[m.Name+"|"+m.CreatedAt] = true
	}
	for _, m := range doc.Mixes {
		if haveMix[m.Name+"|"+m.CreatedAt] {
			continue
		}
		rel, size, ok := readAudio(m.Audio, "")
		if !ok {
			continue
		}
		nm := m.Mix
		nm.ID, nm.UserID, nm.File, nm.Size, nm.FilesPath, nm.FilesURL = 0, u.ID, rel, size, "", ""
		if m.SongID != nil {
			if id, ok := songMap[*m.SongID]; ok {
				nm.SongID = &id
			} else {
				nm.SongID = nil
			}
		}
		if err := s.store.CreateMix(ctx, &nm); err != nil {
			return nil, err
		}
		added["mixes"]++
	}
	ideas, _ := s.store.ListIdeas(ctx, u.ID, 0, 100000)
	haveIdea := map[string]bool{}
	for _, x := range ideas {
		haveIdea[x.Name+"|"+x.CreatedAt] = true
		if x.ClientID != "" {
			haveIdea["client|"+x.ClientID] = true
		}
	}
	for _, x := range doc.Ideas {
		if haveIdea[x.Name+"|"+x.CreatedAt] || (x.ClientID != "" && haveIdea["client|"+x.ClientID]) {
			continue
		}
		rel, size, ok := readAudio(x.Audio, "")
		if !ok {
			continue
		}
		nx := x.Idea
		nx.ID, nx.UserID, nx.File, nx.Size, nx.App, nx.BandID = 0, u.ID, rel, size, "", nil
		if nx.BandName != "" {
			name := nx.BandName
			if band, _, m := s.bandFor(ctx, u.ID, nil, &name); m == "" {
				nx.BandID = band
			}
		}
		if !oneOf(nx.Source, []string{"recorded", "upload", "files", "music"}) {
			nx.Source = "recorded"
		}
		if err := s.store.CreateIdea(ctx, &nx); err != nil {
			_ = os.Remove(s.mediaPath(rel))
			return nil, err
		}
		added["ideas"]++
	}
	sits, _ := s.store.ListSittings(ctx, u.ID, "0000-00-00", "9999-12-31")
	haveSit := map[string]bool{}
	for _, x := range sits {
		haveSit[x.StartedAt+"|"+x.Title] = true
	}
	for _, x := range doc.Sittings {
		if haveSit[x.StartedAt+"|"+x.Title] || !validDate(x.Day) || x.Day == "" {
			continue
		}
		nx := x
		nx.ID, nx.UserID, nx.SentTo = 0, u.ID, firstNonEmpty(x.SentTo, "")
		if x.SongID != nil {
			if id, ok := songMap[*x.SongID]; ok {
				nx.SongID = &id
			} else {
				nx.SongID = nil
			}
		}
		if err := s.store.InsertSitting(ctx, &nx); err != nil {
			return nil, err
		}
		added["sittings"]++
	}
	parts := []string{}
	for _, k := range []string{"bands", "songs", "tracks", "takes", "notes", "mixes", "ideas", "sittings"} {
		if added[k] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", added[k], k))
		}
	}
	text := "Nothing new: everything in the file is here already"
	if len(parts) > 0 {
		text = "Added " + strings.Join(parts, ", ")
	}
	return map[string]any{"added": added, "text": text}, nil
}
