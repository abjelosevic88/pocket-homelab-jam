package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-jam/internal/store"
)

// ---- undo, for any line of a song's history ----
//
// A line keeps what puts its change back: API calls (a field back to what it
// was, a thing made deleted again), takes back where they were (moves, picks,
// recording over a part), or deleted things back as they were (their files wait
// in the trash for trashDays). Undoing runs those as the person, as one job, so
// the undo is a line of its own — and undoing that line does it again (redo).

const trashDays = 30

type undoOp struct {
	M       string            `json:"m,omitempty"` // an API call: method, path, body
	P       string            `json:"p,omitempty"`
	B       map[string]any    `json:"b,omitempty"`
	Takes   []store.TakeState `json:"takes,omitempty"`   // takes back where they were
	Restore *store.Snapshot   `json:"restore,omitempty"` // deleted things back
}

func (o undoOp) empty() bool { return o.M == "" && len(o.Takes) == 0 && o.Restore == nil }

func encodeOps(ops []undoOp) string {
	if len(ops) == 0 {
		return ""
	}
	j, _ := json.Marshal(ops)
	return string(j)
}

func call(method, path string, body map[string]any) undoOp {
	return undoOp{M: method, P: "/api/v1" + path, B: body}
}

func takesBack(states []store.TakeState) undoOp { return undoOp{Takes: states} }

// statesOn are where the takes on these tracks are now (for putting back later).
func (s *Server) statesOn(ctx context.Context, uid int64, tracks ...int64) []store.TakeState {
	st, _ := s.store.TakeStates(ctx, uid, tracks...)
	return st
}

// trackSnapshot is a track with its takes and the notes pinned to it, as they are now.
func (s *Server) trackSnapshot(ctx context.Context, uid int64, t *store.Track) *store.Snapshot {
	x := &store.Snapshot{}
	tt := *t
	tt.Takes = nil
	x.Tracks = []store.Track{tt}
	if ks, err := s.store.ListTakes(ctx, uid, t.SongID); err == nil {
		for _, k := range ks {
			if k.TrackID == t.ID {
				x.Takes = append(x.Takes, store.SnapTake{Take: k, F: k.File})
			}
		}
	}
	x.Marks, _ = s.store.MarksOnTrack(ctx, uid, t.ID)
	return x
}

// trashMedia keeps the files of deleted takes and mixes for trashDays, so an undo can bring them back.
func (s *Server) trashMedia(ctx context.Context, uid int64, rels ...string) {
	for _, rel := range rels {
		if rel == "" || s.store.MediaInUse(ctx, rel) {
			continue
		}
		if err := s.store.Trash(ctx, uid, rel); err != nil {
			s.dropMedia(ctx, rel)
		}
	}
}

// emptyTrash removes trashed files older than trashDays that nothing uses again.
func (s *Server) emptyTrash(ctx context.Context) {
	files, err := s.store.TrashedBefore(ctx, time.Now().AddDate(0, 0, -trashDays))
	if err != nil {
		return
	}
	for _, f := range files {
		s.dropMedia(ctx, f)
		_ = s.store.Untrash(ctx, f)
	}
}

// handleUndo undoes one line of a song's history (an undo line: does it again).
func (s *Server) handleUndo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	so, err := s.store.GetSong(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "song")
		return
	}
	if !mayChange(ctx, so.Source) {
		writeErr(w, http.StatusForbidden, "this song was not added by "+linkApp(ctx))
		return
	}
	e, err := s.store.GetSongEvent(ctx, uid, so.ID, idParam(r, "eid"))
	if err != nil {
		s.fail(w, err, "change")
		return
	}
	if e.Undo == "" {
		writeErr(w, http.StatusBadRequest, "this change can't be undone")
		return
	}
	if e.UndoneBy != 0 {
		writeErr(w, http.StatusConflict, "that change is undone already")
		return
	}
	var ops []undoOp
	if err := json.Unmarshal([]byte(e.Undo), &ops); err != nil {
		writeErr(w, http.StatusBadRequest, "this change can't be undone")
		return
	}
	g := group{id: fmt.Sprintf("undo-%d-%d", e.ID, time.Now().UnixNano()), kind: "undo", ref: e.ID, label: "Undid: " + e.Text}
	if e.Kind == "undo" {
		g.kind, g.label = "redo", "Redid: "+strings.TrimPrefix(e.Text, "Undid: ")
	}
	runErr := s.runUndo(context.WithValue(ctx, groupKey{}, g), r, uid, so.ID, ops)
	u, _ := s.store.SongEventByKey(ctx, uid, so.ID, "group:"+g.id)
	if u == nil && runErr != nil {
		writeErr(w, http.StatusConflict, "couldn't undo it: "+runErr.Error())
		return
	}
	by := int64(-1) // nothing was left to change
	if u != nil {
		by = u.ID
	}
	_ = s.store.MarkUndone(ctx, uid, e.ID, by)
	if g.kind == "redo" {
		// the change stands again and can be undone again; the redo line itself can't
		_ = s.store.MarkUndone(ctx, uid, e.Ref, 0)
		if u != nil {
			_ = s.store.RegroupSongEvent(ctx, u.ID, u.Text, "")
		}
	}
	out := map[string]any{"ok": true}
	if u != nil {
		out["event"], _ = s.store.GetSongEvent(ctx, uid, so.ID, u.ID)
	} else {
		out["text"] = "It was like that already"
	}
	if runErr != nil {
		out["ok"], out["error"] = false, "undid only part of it: "+runErr.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

// runUndo does what puts a change back, in order, as the person (r's sign-in).
func (s *Server) runUndo(ctx context.Context, r *http.Request, uid, songID int64, ops []undoOp) error {
	for _, op := range ops {
		switch {
		case len(op.Takes) > 0:
			ids := make([]int64, len(op.Takes))
			for i, k := range op.Takes {
				ids[i] = k.ID
			}
			now, _ := s.store.TakeStatesOf(ctx, uid, ids)
			if err := s.store.SetTakeStates(ctx, uid, songID, op.Takes); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return errors.New("the track it was on is gone")
				}
				return err
			}
			s.note(ctx, uid, songID, change{kind: "part", text: "Takes put back"}.with(takesBack(now)))
		case op.Restore != nil:
			x := op.Restore
			for _, k := range x.Takes {
				if p := s.mediaPath(k.F); p == "" || !fileExists(p) {
					return errors.New("its recording is gone for good")
				}
			}
			for _, m := range x.Mixes {
				if p := s.mediaPath(m.F); p == "" || !fileExists(p) {
					return errors.New("the mix's file is gone for good")
				}
			}
			if err := s.store.Restore(ctx, uid, x); err != nil {
				if strings.Contains(err.Error(), "FOREIGN KEY") {
					return errors.New("the track or song it was on is gone")
				}
				if strings.Contains(err.Error(), "UNIQUE") {
					return errors.New("it is back already")
				}
				return err
			}
			// what deletes it again
			var back []undoOp
			onTrack := map[int64]bool{}
			for _, t := range x.Tracks {
				back = append(back, call("DELETE", fmt.Sprintf("/tracks/%d", t.ID), nil))
				onTrack[t.ID] = true
			}
			for _, k := range x.Takes {
				if !onTrack[k.TrackID] {
					back = append(back, call("DELETE", fmt.Sprintf("/takes/%d", k.ID), nil))
				}
			}
			for _, m := range x.Marks {
				if m.TrackID == nil || !onTrack[*m.TrackID] {
					back = append(back, call("DELETE", fmt.Sprintf("/marks/%d", m.ID), nil))
				}
			}
			for _, m := range x.Mixes {
				back = append(back, call("DELETE", fmt.Sprintf("/mixes/%d", m.ID), nil))
			}
			s.note(ctx, uid, songID, change{kind: "take", text: "Brought back"}.with(back...))
		case op.M != "":
			if err := s.replay(ctx, r, op); err != nil {
				return err
			}
		}
	}
	return nil
}

// replay sends an API call through the app as the person who asked for the undo.
func (s *Server) replay(ctx context.Context, r *http.Request, op undoOp) error {
	if s.router == nil || !strings.HasPrefix(op.P, "/api/v1/") {
		return errors.New("can't do that here")
	}
	var body io.Reader
	if op.B != nil {
		j, _ := json.Marshal(op.B)
		body = bytes.NewReader(j)
	}
	// a fresh route for chi (the undo request's own is on ctx)
	req := httptest.NewRequest(op.M, op.P, body).WithContext(context.WithValue(ctx, chi.RouteCtxKey, (*chi.Context)(nil)))
	req.RemoteAddr = r.RemoteAddr
	for _, h := range []string{"Cookie", "Authorization", "X-API-Key", "X-Forwarded-For", "X-Real-IP"} {
		if v := r.Header.Values(h); len(v) > 0 {
			req.Header[h] = v
		}
	}
	if s.cfg.AuthProxyHeader != "" {
		for _, h := range []string{s.cfg.AuthProxyHeader, s.cfg.AuthProxyEmailHeader} {
			if v := r.Header.Get(h); h != "" && v != "" {
				req.Header.Set(h, v)
			}
		}
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	if rec.Code < 300 {
		return nil
	}
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &e)
	if rec.Code == http.StatusNotFound {
		return errors.New("what it changed is gone")
	}
	if e.Error == "" {
		e.Error = http.StatusText(rec.Code)
	}
	return errors.New(e.Error)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
