package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Takes and mixes are files in DATA_DIR/media/<user id>/ — never in the
// database — so a long recording does not slow every query and a backup of
// the folder is a plain copy. The database keeps the relative path.

func (s *Server) mediaRoot() string { return filepath.Join(s.cfg.DataDir, "media") }

// mediaPath is the full path of a relative media name; "" when it would leave the folder.
func (s *Server) mediaPath(rel string) string {
	if rel == "" || strings.Contains(rel, "..") || filepath.IsAbs(rel) {
		return ""
	}
	return filepath.Join(s.mediaRoot(), filepath.FromSlash(rel))
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// newMediaName is a fresh relative name for one person's file.
func newMediaName(uid int64, ext string) string {
	ext = strings.ToLower(ext)
	if !regexp.MustCompile(`^\.[a-z0-9]{1,5}$`).MatchString(ext) {
		ext = ".bin"
	}
	return fmt.Sprintf("%d/%s%s", uid, randomHex(10), ext)
}

// saveMedia copies r into a new file of the person's; it returns the
// relative name and the size. At most max bytes.
func (s *Server) saveMedia(uid int64, ext string, r io.Reader, max int64) (string, int64, error) {
	rel := newMediaName(uid, ext)
	p := s.mediaPath(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", 0, err
	}
	f, err := os.OpenFile(p+".part", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", 0, err
	}
	n, err := io.Copy(f, io.LimitReader(r, max+1))
	cerr := f.Close()
	if err == nil {
		err = cerr
	}
	if err == nil && n > max {
		err = &userErr{fmt.Sprintf("a file can be up to %d MB", max>>20)}
	}
	if err == nil && n == 0 {
		err = &userErr{"the file is empty"}
	}
	if err != nil {
		_ = os.Remove(p + ".part")
		return "", 0, err
	}
	return rel, n, os.Rename(p+".part", p)
}

// moveIntoMedia moves a finished file (a download) into the person's folder.
func (s *Server) moveIntoMedia(uid int64, src string) (string, int64, error) {
	rel := newMediaName(uid, filepath.Ext(src))
	p := s.mediaPath(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", 0, err
	}
	if err := os.Rename(src, p); err != nil {
		in, err := os.Open(src)
		if err != nil {
			return "", 0, err
		}
		defer in.Close()
		r, n, err := s.saveMedia(uid, filepath.Ext(src), in, 8<<30)
		_ = os.Remove(src)
		return r, n, err
	}
	fi, _ := os.Stat(p)
	var n int64
	if fi != nil {
		n = fi.Size()
	}
	return rel, n, nil
}

// dropMedia removes files nothing points at any more.
func (s *Server) dropMedia(ctx context.Context, rels ...string) {
	for _, rel := range rels {
		if rel == "" || s.store.MediaInUse(ctx, rel) {
			continue
		}
		if p := s.mediaPath(rel); p != "" {
			_ = os.Remove(p)
		}
	}
}

// serveMediaFile sends a media file with ranges (seeking in a song).
func (s *Server) serveMediaFile(w http.ResponseWriter, r *http.Request, rel, mime, name string) {
	p := s.mediaPath(rel)
	f, err := os.Open(p)
	if p == "" || err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if mime == "" {
		mime = mimeByExt(rel)
	}
	if mime != "" {
		w.Header().Set("Content-Type", mime)
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if name != "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, strings.ReplaceAll(safeName(name), `"`, "")))
	}
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

var mediaExts = map[string]string{
	".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".aac": "audio/aac", ".ogg": "audio/ogg", ".oga": "audio/ogg", ".opus": "audio/ogg", ".flac": "audio/flac",
	".wav": "audio/wav", ".weba": "audio/webm", ".caf": "audio/x-caf", ".aif": "audio/aiff", ".aiff": "audio/aiff",
	".mp4": "video/mp4", ".m4v": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime",
}

func mimeByExt(name string) string { return mediaExts[strings.ToLower(filepath.Ext(name))] }

func isAudioExt(ext string) bool {
	return strings.HasPrefix(mediaExts[strings.ToLower(ext)], "audio/")
}

func isVideoExt(ext string) bool {
	return strings.HasPrefix(mediaExts[strings.ToLower(ext)], "video/")
}

// extForMime is the file extension for an audio type ("" when unknown).
func extForMime(mime string) string {
	base := strings.ToLower(strings.TrimSpace(strings.SplitN(mime, ";", 2)[0]))
	switch base {
	case "audio/wav", "audio/wave", "audio/x-wav", "audio/vnd.wave":
		return ".wav"
	case "audio/x-m4a", "audio/m4a":
		return ".m4a"
	case "audio/mp3":
		return ".mp3"
	case "audio/webm":
		return ".weba"
	}
	for e, m := range mediaExts {
		if m == base && e != ".oga" && e != ".opus" && e != ".m4v" {
			return e
		}
	}
	return ""
}

// fileSafe makes a name fit for a file in Pocket Files.
func fileSafe(n string) string {
	n = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '-'
		}
		return r
	}, n)
	return firstNonEmpty(clip(strings.TrimSpace(n), 120), "Jam")
}

func safeName(n string) string {
	n = filepath.Base(strings.ReplaceAll(n, "\\", "/"))
	n = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, n)
	return firstNonEmpty(clip(strings.TrimSpace(n), 160), "file")
}
