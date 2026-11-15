package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-jam/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-jam/internal/store"
)

// ---- settings ----

func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "••••"
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

// publicSettings is what one person sees: their settings with the secrets
// masked and, per Pocket app, the mode in effect (<app>_mode), whether it
// works and how (<app>_via: core | direct | "") and why not (<app>_detail).
func (s *Server) publicSettings(ctx context.Context, u *store.User, st store.Settings) map[string]any {
	out := map[string]any{
		"core_configured": s.core.Configured(), "core_registered": s.coreStatus()["registered"],
		"title": st.Title, "date_format": st.DateFormat, "time_format": st.TimeFormat, "week_start": st.WeekStart,
		"tempo": st.Tempo, "beats": st.Beats, "count_in": st.CountIn, "click": st.Click,
		"click_sound": st.ClickSound, "click_volume": st.ClickVolume, "stereo": st.Stereo, "monitor": st.Monitor, "level_takes": st.LevelTakes, "keep_awake": st.KeepAwake,
		"a4": st.A4, "tuner_preset": st.TunerPreset, "practice_to": st.PracticeTo, "files_folder": st.FilesFolder,
		"setup_complete": st.SetupComplete, "base_url": s.cfg.BaseURL, "demo": s.cfg.DemoData,
	}
	for _, a := range pocketApps {
		mode, via, detail := s.connStatus(ctx, u, st, a)
		out[a.slug+"_mode"], out[a.slug+"_via"], out[a.slug+"_detail"] = mode, via, detail
		out[a.slug+"_configured"] = via != ""
		_, url, token, public := connSettings(st, a)
		out[a.slug+"_url"], out[a.slug+"_token"], out[a.slug+"_public_url"] = url, mask(token), public
		if public == "" && via != "" {
			public = s.resolvePublic(ctx, a.slug)
		}
		out[a.slug+"_open_url"] = public
	}
	return out
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), userFrom(r.Context()), st))
}

// handlePutSettings saves the caller's personal settings. The box-wide title
// is only taken from an admin.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	me := userFrom(r.Context())
	isAdmin := me.Role == "admin"
	cur, err := s.store.GetSettings(r.Context(), me.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	raw := map[string]any{}
	var in store.Settings
	keys, err := decodeKeys(r, &raw)
	if err != nil || remarshal(raw, &in) != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	bad := func(msg string) { writeErr(w, http.StatusBadRequest, msg) }
	if keys["title"] && isAdmin {
		cur.Title = strings.TrimSpace(in.Title)
		if cur.Title == "" {
			cur.Title = store.DefaultTitle
		}
	}
	if keys["date_format"] {
		cur.DateFormat = in.DateFormat
	}
	if keys["time_format"] {
		cur.TimeFormat = in.TimeFormat
	}
	if keys["week_start"] && (in.WeekStart == 1 || in.WeekStart == 7 || in.WeekStart == 6) {
		cur.WeekStart = in.WeekStart
	}
	secret := func(k string, dst *string, v string) {
		if keys[k] && !strings.Contains(v, "••") {
			*dst = strings.TrimSpace(v)
		}
	}
	url := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimRight(strings.TrimSpace(v), "/")
		}
	}
	if keys["tempo"] {
		if in.Tempo < 20 || in.Tempo > 400 {
			bad("the tempo is 20 to 400 beats a minute")
			return
		}
		cur.Tempo = in.Tempo
	}
	if keys["beats"] {
		cur.Beats = clampInt(in.Beats, 1, 16)
	}
	if keys["count_in"] {
		cur.CountIn = clampInt(in.CountIn, 0, 4)
	}
	if keys["click_sound"] && oneOf(in.ClickSound, []string{"click", "wood", "beep"}) {
		cur.ClickSound = in.ClickSound
	}
	if keys["click_volume"] {
		cur.ClickVolume = clampInt(in.ClickVolume, 0, 100)
	}
	if keys["a4"] {
		if in.A4 < 415 || in.A4 > 466 {
			bad("the reference A is 415 to 466 Hz")
			return
		}
		cur.A4 = in.A4
	}
	if keys["tuner_preset"] && oneOf(in.TunerPreset, tunerPresets) {
		cur.TunerPreset = in.TunerPreset
	}
	if keys["practice_to"] {
		if !oneOf(in.PracticeTo, []string{"off", "guitar", "time"}) {
			bad("practice time goes to Pocket Guitar, Pocket Time or nowhere")
			return
		}
		cur.PracticeTo = in.PracticeTo
	}
	if keys["files_folder"] {
		cur.FilesFolder = clip(strings.Trim(strings.TrimSpace(in.FilesFolder), "/"), 80)
		if cur.FilesFolder == "" {
			cur.FilesFolder = "Jam"
		}
	}
	for k, pair := range map[string][2]*bool{"click": {&cur.Click, &in.Click}, "stereo": {&cur.Stereo, &in.Stereo}, "monitor": {&cur.Monitor, &in.Monitor}, "keep_awake": {&cur.KeepAwake, &in.KeepAwake}, "level_takes": {&cur.LevelTakes, &in.LevelTakes}} {
		if keys[k] {
			*pair[0] = *pair[1]
		}
	}
	// each Pocket app: mode, URL, token, public URL
	inMap := map[string]string{}
	for k, v := range raw {
		if sv, ok := v.(string); ok {
			inMap[k] = sv
		}
	}
	for _, a := range pocketApps {
		c := cur.Conns[a.slug]
		if v := inMap[a.slug+"_mode"]; keys[a.slug+"_mode"] && (v == modeCore || v == modeDirect || v == modeOff) {
			c.Mode = v
		}
		url(a.slug+"_url", &c.URL, inMap[a.slug+"_url"])
		secret(a.slug+"_token", &c.Token, inMap[a.slug+"_token"])
		url(a.slug+"_public_url", &c.PublicURL, inMap[a.slug+"_public_url"])
		if c != (store.Conn{}) {
			cur.Conns[a.slug] = c
		}
	}
	if err := s.store.SaveUserSettings(r.Context(), me.ID, cur); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	if isAdmin && keys["title"] {
		if err := s.store.SaveBoxSettings(r.Context(), cur); err != nil {
			s.fail(w, err, "save settings")
			return
		}
	}
	s.forgetStatus(me.ID)
	for k := range keys {
		if isModeKey(k) {
			s.reportModesSoon()
			break
		}
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), me, cur))
}

// handleIntegrationTest checks a connection with the caller's own saved
// settings (or the URL/key typed into the form).
func (s *Server) handleIntegrationTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Which  string `json:"which"`
		URL    string `json:"url"`
		Key    string `json:"key"`
		Format string `json:"format"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	uid := actorID(r.Context())
	st, _ := s.store.GetSettings(r.Context(), uid)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	keyOr := func(saved string) string {
		if strings.Contains(in.Key, "••") || in.Key == "" {
			return saved
		}
		return in.Key
	}
	urlOr := func(saved string) string {
		if strings.TrimSpace(in.URL) == "" {
			return saved
		}
		return strings.TrimRight(strings.TrimSpace(in.URL), "/")
	}
	var msg string
	var err error
	if a, ok := appBySlug(in.Which); ok {
		mode, savedURL, savedTok, _ := connSettings(st, a)
		typed := in.Key != "" && !strings.Contains(in.Key, "••")
		var conn *siblingConn
		if tok := keyOr(savedTok); tok != "" && (typed || s.connMode(mode, savedTok) == modeDirect) {
			base := urlOr(savedURL)
			if base == "" {
				base = s.resolveSibling(ctx, a.slug)
			}
			conn = &siblingConn{client: pocket.New(base, tok, 15*time.Second), via: modeDirect}
		} else {
			conn, err = s.clients(ctx, uid).conn(ctx, a, a.scopes...)
		}
		if err == nil {
			how := "direct connection"
			if conn.via == modeCore {
				how = "through Pocket Core"
			}
			var v string
			if v, err = conn.client.Health(ctx); err == nil {
				msg = fmt.Sprintf("%s %s, %s", a.name, v, how)
			}
		}
	} else {
		writeErr(w, http.StatusBadRequest, "unknown integration")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

// ---- system / backup / export ----

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var size int64
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		size = fi.Size()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"core":      s.coreStatus(),
		"auth_mode": s.cfg.AuthMode, "data_dir": s.cfg.DataDir, "db_path": s.cfg.DBPath, "db_bytes": size,
		"uptime": time.Since(s.started).Round(time.Second).String(), "timezone": time.Now().Location().String(),
		"counts": s.store.CountsFor(r.Context(), actorID(r.Context())),
	})
}

// handleBackup downloads the whole database, an admin-only box operation.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "backups")
	name := fmt.Sprintf("pocket-jam-%s.db", time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := s.store.Backup(r.Context(), dst); err != nil {
		s.fail(w, err, "backup")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, dst)
}

// tunerPresets are the tuner's string sets (the web app has the notes).
var tunerPresets = []string{"chromatic", "guitar", "guitar_drop_d", "guitar_dadgad", "guitar_open_g", "bass", "bass5", "ukulele", "violin", "viola", "cello", "mandolin", "banjo"}
