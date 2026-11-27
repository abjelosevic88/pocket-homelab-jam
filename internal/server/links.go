package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-jam/internal/store"
)

// Pocket Core app links (docs/links.md in pocket-homelab-core), Jam as
// the target: another Pocket app the person allowed in Core may act here for
// them with a pcx_ token, only within these scopes. Every route not listed
// in linkRoutes, portable or capabilities stays closed to it (403).

var linkOffers = []core.Offer{
	{Scope: "jam:read", Name: "See your music recordings", Description: "Read your songs (titles, tempo, key, notes), their tracks and takes, your ideas, your mixes and the time you spent playing and recording, and listen to them: for your journal, search and Pocket AI."},
	{Scope: "jam:write", Name: "Add songs to record", Description: "Add a song or an idea to record (title, artist, tempo, key, notes), or an idea already recorded. Changes or removes only the songs and ideas that app added; never your other recordings."},
}

// portable is what Pocket Move may copy out and bring in for a person
// (docs/move.md in pocket-homelab-core); Register adds move:export/import.
var portable = &core.Portable{
	Exports: []core.PortableExport{
		{Path: "/api/v1/export.zip", File: "jam.zip", Kind: core.KindPocket, Title: "Every song with its tracks, takes, notes and mixes, your ideas and the time you spent, with all the audio"},
		{Path: "/api/v1/export.csv", File: "jam-time.csv", Kind: core.KindCSV, Title: "The time you spent playing, one row per sitting"},
	},
	Imports: []core.PortableImport{
		{Path: "/api/v1/import", Kind: core.KindPocket, ContentType: "application/zip", MaxBytes: 16 << 30, Title: "Adds the songs, takes, mixes and ideas from another box with their audio; what you already have is left alone"},
	},
}

// capabilities are what Pocket Search, Pocket Day and Pocket Hub may ask
// Jam (docs/capabilities.md in pocket-homelab-core); Register adds the
// pocket:* offers. The routes are in capabilities.go. Nothing is kept from a
// quick line: a song needs its page (tempo, tracks) and an idea is a recording.
var capabilities = core.Capabilities{
	Search: &core.Searchable{What: "Your songs, by name, artist and notes", Kinds: []string{"song"}, Bangs: []string{"jam", "song"}},
	Day: &core.DayFeed{Label: "Jam", One: "song", Many: "songs", Unit: "count",
		What: "The songs you made, changed, played or recorded that day, with the minutes played and the takes, and the ideas you recorded"},
	Widget: &core.Widget{Title: "Jam", What: "The songs you worked on last"},
}

// linkRoute is one request a Pocket Core link token may make.
type linkRoute struct {
	methods string
	path    *regexp.Regexp
	scope   string // "" = any linked app
}

var linkRoutes = []linkRoute{
	{"GET", regexp.MustCompile(`^/api/v1/auth/me$`), ""},
	{"GET", regexp.MustCompile(`^/api/v1/(home|summary|search|songs|mixes|time|day/\d{4}-\d{2}-\d{2})$`), "jam:read"},
	{"GET", regexp.MustCompile(`^/api/v1/songs/\d+(/history)?$`), "jam:read"},
	{"GET", regexp.MustCompile(`^/api/v1/(takes|mixes|ideas)/\d+/audio$`), "jam:read"},
	{"GET", regexp.MustCompile(`^/api/v1/ideas(/\d+)?$`), "jam:read"},
	{"POST", regexp.MustCompile(`^/api/v1/songs$`), "jam:write"},
	{"PUT DELETE", regexp.MustCompile(`^/api/v1/songs/\d+$`), "jam:write"},
	{"POST", regexp.MustCompile(`^/api/v1/ideas(/\d+/song)?$`), "jam:write"},
	{"PUT DELETE", regexp.MustCompile(`^/api/v1/ideas/\d+$`), "jam:write"},
}

var errLinkScope = errors.New("outside the link's scope")

// linkAllows reports whether a link token with these scopes may make this request.
func linkAllows(scopes []string, r *http.Request) bool {
	if portable.LinkAllows(scopes, r) || capabilities.LinkAllows(scopes, r) {
		return true
	}
	for _, lr := range linkRoutes {
		if !strings.Contains(" "+lr.methods+" ", " "+r.Method+" ") || !lr.path.MatchString(r.URL.Path) {
			continue
		}
		if lr.scope == "" {
			return true
		}
		for _, sc := range scopes {
			if sc == lr.scope {
				return true
			}
		}
	}
	return false
}

// linkUser validates a pcx_ token, checks the route against its scopes and
// maps it to the person it acts for. The second value is the acting app's slug.
func (s *Server) linkUser(ctx context.Context, tok string, r *http.Request) (*store.User, string, error) {
	in, err := s.core.Introspect(ctx, tok)
	if err != nil {
		return nil, "", err
	}
	if !in.Active || !in.Delegated() || in.Audience != "jam" || in.User == nil || in.User.Email == "" || in.App == nil || in.App.Slug == "" {
		return nil, "", http.ErrNoCookie
	}
	if !linkAllows(strings.Fields(in.Scope), r) {
		return nil, "", errLinkScope
	}
	u, err := s.coreIdentityUser(ctx, in)
	return u, in.App.Slug, err
}

const linkAppKey ctxKey = 2

// linkApp is the acting app's slug when the request came in on a link token.
func linkApp(ctx context.Context) string {
	a, _ := ctx.Value(linkAppKey).(string)
	return a
}

// withLinkApp marks a context as acting for another app (tests, and the auth middleware).
func withLinkApp(ctx context.Context, slug string) context.Context {
	return context.WithValue(ctx, linkAppKey, slug)
}
