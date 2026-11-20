package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-jam/internal/config"
)

// moveLinks starts Jam behind a fake Core whose pcx_<who>_<export|import|read>
// tokens are Pocket Move links acting for alice@ or bob@.
func moveLinks(t *testing.T) *app {
	t.Helper()
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		parts := strings.SplitN(strings.TrimPrefix(in.Token, "pcx_"), "_", 2)
		if len(parts) != 2 {
			io.WriteString(w, `{"active":false}`)
			return
		}
		scope := map[string]string{"export": core.ScopeMoveExport, "import": core.ScopeMoveImport, "read": "jam:read"}[parts[1]]
		email := parts[0] + "@example.com"
		fmt.Fprintf(w, `{"active":true,"kind":"delegated","scope":%q,"audience":"jam","user":{"id":1,"sub":%q,"email":%q,"name":%q,"role":"member"},"app":{"id":7,"slug":"move","name":"Pocket Move"}}`, scope, email, email, parts[0])
	}))
	t.Cleanup(coreSrv.Close)
	return newApp(t, func(c *config.Config) { c.CoreURL = coreSrv.URL })
}

func (a *app) moveRaw(tok, method, path, ctype string, body []byte) (int, []byte) {
	a.t.Helper()
	req, _ := http.NewRequest(method, a.url+path, bytes.NewReader(body))
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func TestMoveLinkScopes(t *testing.T) {
	a := moveLinks(t)
	exp, imp, read := "pcx_bob_export", "pcx_bob_import", "pcx_bob_read"
	for _, c := range []struct {
		tok, method, path string
		want              int
	}{
		{exp, "GET", core.PortablePath, 200},
		{exp, "GET", "/api/v1/export.zip", 200},
		{exp, "GET", "/api/v1/export.csv", 200},
		{exp, "GET", "/api/v1/export.json", 403},
		{exp, "POST", "/api/v1/import", 403},
		{exp, "GET", "/api/v1/songs", 403},
		{exp, "GET", "/api/v1/settings", 403},
		{exp, "GET", "/api/v1/backup", 403},
		{imp, "GET", core.PortablePath, 200},
		{imp, "GET", "/api/v1/export.zip", 403},
		{imp, "POST", "/api/v1/songs", 403},
		{imp, "POST", "/api/v1/ideas", 403},
		{imp, "PUT", "/api/v1/settings", 403},
		{read, "GET", core.PortablePath, 403},
		{read, "GET", "/api/v1/export.zip", 403},
	} {
		if code, body := a.moveRaw(c.tok, c.method, c.path, "application/json", []byte("{}")); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%s)", c.tok, c.method, c.path, code, c.want, body)
		}
	}
	_, body := a.moveRaw(exp, "GET", core.PortablePath, "", nil)
	var p core.Portable
	if err := json.Unmarshal(body, &p); err != nil || len(p.Exports) != 2 || len(p.Imports) != 1 || p.Exports[0].Kind != core.KindPocket {
		t.Fatalf("portable: %s", body)
	}
}

// jamJSON is the jam.json inside an export.zip.
func jamJSON(t *testing.T, z []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(z), int64(len(z)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	f, err := zr.Open("jam.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b, _ := io.ReadAll(f)
	return string(b)
}

// Move brings Alice's songs with their audio to Bob: the import adds once,
// and nothing of one person shows up for the other.
func TestMoveImportsAddOnce(t *testing.T) {
	a := moveLinks(t)
	sid := id(a.ok(a.alice, "POST", "/api/v1/songs", map[string]any{"title": "Alice's song"}))
	tid := id(a.ok(a.alice, "POST", "/api/v1/songs/"+sid+"/tracks", map[string]any{"instrument": "keys"}))
	a.upload(a.alice, "/api/v1/tracks/"+tid+"/takes", nil, tone(0.3), "a.wav")

	code, export := a.moveRaw("pcx_alice_export", "GET", "/api/v1/export.zip", "", nil)
	if code != 200 || !strings.Contains(jamJSON(t, export), "Alice's song") {
		t.Fatalf("alice's export: %d", code)
	}
	if _, bob := a.moveRaw("pcx_bob_export", "GET", "/api/v1/export.zip", "", nil); strings.Contains(jamJSON(t, bob), "Alice's song") {
		t.Fatal("bob's export holds alice's song")
	}
	imp := func() map[string]any {
		code, out := a.moveRaw("pcx_bob_import", "POST", "/api/v1/import", "application/zip", export)
		if code != 200 {
			t.Fatalf("import → %d %s", code, out)
		}
		var m map[string]any
		_ = json.Unmarshal(out, &m)
		return m
	}
	if first := imp(); num(first, "songs") != 1 || num(first, "tracks") != 1 || num(first, "takes") != 1 {
		t.Fatalf("first import: %v", first)
	}
	second := imp()
	for _, k := range []string{"bands", "songs", "tracks", "takes", "notes", "mixes", "ideas", "sittings"} {
		if num(second, k) != 0 {
			t.Fatalf("second import added %s: %v", k, second)
		}
	}
	if l := a.ok(a.bob, "GET", "/api/v1/songs", nil)["songs"].([]any); len(l) != 1 {
		t.Fatalf("bob's songs: %v", l)
	}
	if l := a.ok(a.alice, "GET", "/api/v1/songs", nil)["songs"].([]any); len(l) != 1 {
		t.Fatalf("alice's songs: %v", l)
	}
}
