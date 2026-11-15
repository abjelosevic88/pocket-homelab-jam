package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-jam/internal/store"
)

// The other Pocket apps. None of it is needed: when an app is not there,
// the page greys the button and says what to do.

func linkTo(sib *siblingConn, path string) string {
	if sib == nil || sib.public == "" {
		return ""
	}
	return strings.TrimRight(sib.public, "/") + path
}

func plainErr(a pocketApp, err error) string {
	if isNotConnected(err) {
		return err.Error()
	}
	if ue, ok := err.(*userErr); ok {
		return ue.msg
	}
	return a.name + ": " + err.Error()
}

func (s *Server) appFail(w http.ResponseWriter, a pocketApp, err error) {
	if isNotConnected(err) {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if ue, ok := err.(*userErr); ok {
		writeErr(w, http.StatusBadRequest, ue.msg)
		return
	}
	writeErr(w, http.StatusBadGateway, plainErr(a, err))
}

// handleApps lists how the person reaches each app, in words they can act on.
func (s *Server) handleApps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := s.clients(ctx, actorID(ctx))
	out := []appState{}
	for _, a := range pocketApps {
		out = append(out, c.status(ctx, a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": out, "core": s.core.Configured()})
}

func (s *Server) sib(w http.ResponseWriter, r *http.Request, a pocketApp, scopes ...string) (*siblingConn, bool) {
	ctx := r.Context()
	sib, err := s.clients(ctx, actorID(ctx)).conn(ctx, a, scopes...)
	if err != nil {
		s.appFail(w, a, err)
		return nil, false
	}
	return sib, true
}

func (s *Server) actorName(u *store.User) string {
	if u == nil {
		return "Someone"
	}
	if n := strings.TrimSpace(u.Name); n != "" {
		return n
	}
	if i := strings.Index(u.Email, "@"); i > 0 {
		return u.Email[:i]
	}
	return "Someone"
}

// ---- Pocket Music: backing tracks from your library ----

var reMusicID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// handleMusicSearch finds songs in the person's music library.
func (s *Server) handleMusicSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sib, ok := s.sib(w, r, appMusic, scopeMusicPlay)
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"songs": []any{}, "url": linkTo(sib, "/")})
		return
	}
	v, err := getJSONFrom(ctx, sib, "/api/v1/search", url.Values{"q": {q}, "limit": {"40"}})
	if err != nil {
		s.appFail(w, appMusic, err)
		return
	}
	m, _ := v.(map[string]any)
	out := []map[string]any{}
	for _, so := range rows(m["songs"]) {
		id := str(so, "id")
		if !reMusicID.MatchString(id) {
			continue
		}
		out = append(out, map[string]any{"id": id, "title": str(so, "title"), "artist": str(so, "artist"), "album": str(so, "album"), "duration_ms": int64(num(so, "duration") * 1000)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"songs": out, "url": linkTo(sib, "/")})
}

// handleMusicStream passes a Pocket Music song through (the browser decodes
// it and keeps it as a take).
func (s *Server) handleMusicStream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chiParam(r, "sid")
	if !reMusicID.MatchString(id) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	sib, ok := s.sib(w, r, appMusic, scopeMusicPlay)
	if !ok {
		return
	}
	res, err := sib.client.Open(ctx, "/api/v1/stream/"+url.PathEscape(id), nil, r.Header.Get("Range"))
	if err != nil {
		if ctx.Err() == nil {
			s.appFail(w, appMusic, err)
		}
		return
	}
	defer res.Body.Close()
	passThrough(w, res)
}

func passThrough(w http.ResponseWriter, res *http.Response) {
	for _, k := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag"} {
		if v := res.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, res.Body)
}

// ---- Pocket Files ----

// handleFilesList lists a folder: sub-folders and audio files.
func (s *Server) handleFilesList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sib, ok := s.sib(w, r, appFiles, scopeFilesRead)
	if !ok {
		return
	}
	space := firstNonEmpty(r.URL.Query().Get("space"), "home")
	path := firstNonEmpty(r.URL.Query().Get("path"), "/")
	v, err := getJSONFrom(ctx, sib, "/api/v1/fs/list", url.Values{"space": {space}, "path": {path}})
	if err != nil {
		s.appFail(w, appFiles, err)
		return
	}
	out := []map[string]any{}
	for _, e := range rows(v, "entries") {
		name := str(e, "name")
		dir := e["is_dir"] == true
		ext := strings.ToLower(filepath.Ext(name))
		kind := ""
		switch {
		case dir:
			kind = "folder"
		case isAudioExt(ext):
			kind = "audio"
		case isVideoExt(ext):
			kind = "video"
		}
		if kind == "" || strings.HasPrefix(name, ".") {
			continue
		}
		out = append(out, map[string]any{"name": name, "path": str(e, "path"), "kind": kind, "size": num(e, "size"), "modified": str(e, "modified")})
	}
	spaces := []map[string]any{}
	if sv, err := getJSONFrom(ctx, sib, "/api/v1/spaces", nil); err == nil {
		for _, sp := range rows(sv, "spaces") {
			spaces = append(spaces, map[string]any{"id": str(sp, "id"), "name": str(sp, "name")})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out, "space": space, "path": path, "spaces": spaces, "url": linkTo(sib, "/")})
}

// handleFilesRaw passes one audio file from Pocket Files through.
func (s *Server) handleFilesRaw(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	space := firstNonEmpty(r.URL.Query().Get("space"), "home")
	path := r.URL.Query().Get("path")
	ext := strings.ToLower(filepath.Ext(path))
	if path == "" || (!isAudioExt(ext) && !isVideoExt(ext)) {
		writeErr(w, http.StatusBadRequest, "pick an audio file")
		return
	}
	sib, ok := s.sib(w, r, appFiles, scopeFilesRead)
	if !ok {
		return
	}
	res, err := sib.client.Open(ctx, "/api/v1/fs/raw", url.Values{"space": {space}, "path": {path}}, r.Header.Get("Range"))
	if err != nil {
		if ctx.Err() == nil {
			s.appFail(w, appFiles, err)
		}
		return
	}
	defer res.Body.Close()
	if mt := mimeByExt(path); mt != "" {
		res.Header.Set("Content-Type", mt)
	}
	passThrough(w, res)
}

// handleMixToFiles keeps a mix in the person's Pocket Files.
func (s *Server) handleMixToFiles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	m, err := s.store.GetMix(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "mix")
		return
	}
	sib, ok := s.sib(w, r, appFiles, scopeFilesWrite)
	if !ok {
		return
	}
	data, err := os.ReadFile(s.mediaPath(m.File))
	if err != nil {
		writeErr(w, http.StatusNotFound, "the mix's file is gone")
		return
	}
	at, _ := time.Parse(time.RFC3339Nano, m.CreatedAt)
	name := fileSafe(fmt.Sprintf("%s %s", at.In(time.Local).Format("2006-01-02"), m.Name)) + filepath.Ext(m.File)
	dir := strings.Trim(firstNonEmpty(s.settingsOf(ctx, uid).FilesFolder, "Jam"), "/")
	var res map[string]any
	q := url.Values{"space": {"home"}, "path": {"/"}, "name": {dir + "/" + name}, "conflict": {"rename"}, "mtime": {strconv.FormatInt(time.Now().UnixMilli(), 10)}}
	if err := sib.client.PutRaw(ctx, "/api/v1/fs/upload", q, data, firstNonEmpty(m.Mime, "audio/wav"), &res); err != nil {
		s.appFail(w, appFiles, err)
		return
	}
	path := firstNonEmpty(str(res, "path"), "/"+dir+"/"+name)
	folder := path
	if i := strings.LastIndex(path, "/"); i > 0 {
		folder = path[:i]
	}
	m.FilesPath, m.FilesURL = path, linkTo(sib, "/f/home"+folder)
	_ = s.store.UpdateMix(ctx, m)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "text": "Saved in Pocket Files as " + path, "url": m.FilesURL, "mix": m})
}

// ---- practice time: Pocket Guitar or Pocket Time ----

// sendPractice sends a sitting on as practice; the same sitting updates the
// same entry over there as it grows.
func (s *Server) sendPractice(ctx context.Context, uid int64, x store.Sitting) {
	st := s.settingsOf(ctx, uid)
	if x.Seconds < 60 || (st.PracticeTo != "guitar" && st.PracticeTo != "time") {
		return
	}
	if x.SentTo != "" && x.SentTo != st.PracticeTo {
		return // it went to the other app before the setting changed
	}
	c := s.clients(ctx, uid)
	title := firstNonEmpty(x.Title, "Practice")
	songURL := s.link("/")
	if x.SongID != nil {
		songURL = s.link(fmt.Sprintf("/songs/%d", *x.SongID))
	}
	note := "Played and recorded in Pocket Jam"
	if x.Takes > 0 {
		note += fmt.Sprintf(" (%d %s)", x.Takes, plural(x.Takes, "take", "takes"))
	}
	switch st.PracticeTo {
	case "guitar":
		sib, err := c.conn(ctx, appGuitar, scopeGuitarWrite)
		if err != nil {
			return
		}
		res, err := postJSONTo(ctx, sib, "/api/v1/practice", map[string]any{"title": title, "started_at": x.StartedAt, "duration_s": x.Seconds, "note": note, "external_id": fmt.Sprintf("jam-%d", x.ID)})
		if err != nil {
			s.log.Debug("pocket guitar: practice not logged", "err", err)
			return
		}
		_ = s.store.MarkSent(ctx, uid, x.ID, "guitar", firstNonEmpty(idOf(res), x.SentRef), x.Seconds)
	case "time":
		sib, err := c.conn(ctx, appTime, scopeTimeWrite)
		if err != nil {
			return
		}
		if x.SentRef != "" {
			var res map[string]any
			if err := sib.client.Do(ctx, http.MethodPut, "/api/v1/entries/"+url.PathEscape(x.SentRef), nil, map[string]any{"duration_s": x.Seconds, "note": note}, &res); err != nil {
				s.log.Debug("pocket time: entry not updated", "err", err)
				return
			}
			_ = s.store.MarkSent(ctx, uid, x.ID, "time", x.SentRef, x.Seconds)
			return
		}
		res, err := postJSONTo(ctx, sib, "/api/v1/entries", map[string]any{"title": title, "started_at": x.StartedAt, "duration_s": x.Seconds, "note": note, "tags": "music",
			"app_ref": fmt.Sprint(firstNonNil(x.SongID)), "app_label": title, "app_url": songURL, "external_id": fmt.Sprintf("sitting-%d", x.ID)})
		if err != nil {
			s.log.Debug("pocket time: entry not logged", "err", err)
			return
		}
		_ = s.store.MarkSent(ctx, uid, x.ID, "time", idOf(res), x.Seconds)
	}
}

func firstNonNil(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
