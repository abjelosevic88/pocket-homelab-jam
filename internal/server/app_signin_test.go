package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The iPhone app signs in through /app-signin: sign in, Allow, a one-time
// code to pocketjam://, traded with the PKCE verifier for its own token.
func TestAppSignin(t *testing.T) {
	a := newApp(t, nil)
	sid, err := a.st.CreateSession(context.Background(), a.ids["Alice"], time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	verifier := strings.Repeat("v", 50)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	q := url.Values{"challenge": {challenge}, "device": {"Ana's iPhone"}}.Encode()

	res, _ := noFollow.Get(a.url + "/app-signin?" + q)
	if loc := res.Header.Get("Location"); res.StatusCode != 302 || !strings.HasPrefix(loc, "/login?next=%2Fapp-signin%3F") {
		t.Fatalf("not signed in → %d %s", res.StatusCode, loc)
	}
	allow := func() string {
		req, _ := http.NewRequest("GET", a.url+"/app-signin?"+q, nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
		res, _ := noFollow.Do(req)
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != 200 || !strings.Contains(string(body), "Ana&#39;s iPhone") {
			t.Fatalf("allow page %d", res.StatusCode)
		}
		req, _ = http.NewRequest("POST", a.url+"/app-signin", strings.NewReader(url.Values{"challenge": {challenge}, "device": {"Ana's iPhone"}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
		res, _ = noFollow.Do(req)
		loc := res.Header.Get("Location")
		if !strings.HasPrefix(loc, "pocketjam://signed-in?code=") {
			t.Fatalf("allow → %s", loc)
		}
		return strings.TrimPrefix(loc, "pocketjam://signed-in?code=")
	}
	exchange := func(c, v string) (int, map[string]any) {
		res, _ := http.Post(a.url+"/api/v1/auth/app/exchange", "application/json", strings.NewReader(`{"code":"`+c+`","verifier":"`+v+`"}`))
		var m map[string]any
		_ = json.NewDecoder(res.Body).Decode(&m)
		return res.StatusCode, m
	}
	code := allow()
	if st, _ := exchange(code, strings.Repeat("x", 50)); st != 401 {
		t.Fatalf("a wrong verifier got %d", st)
	}
	if st, _ := exchange(code, verifier); st != 401 {
		t.Fatalf("a used code got %d", st)
	}
	st, m := exchange(allow(), verifier)
	tok, _ := m["token"].(string)
	if st != 200 || !strings.HasPrefix(tok, "pjam_") {
		t.Fatalf("exchange → %d %v", st, m)
	}
	if me := a.ok(tok, "GET", "/api/v1/auth/me", nil); me["email"] != "alice@example.com" {
		t.Fatalf("the app's token acts as Alice: %v", me)
	}
}
