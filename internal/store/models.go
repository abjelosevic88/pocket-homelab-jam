package store

import (
	"encoding/json"
)

// User is an account on this box.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Conn is one person's connection to a sibling Pocket app.
type Conn struct {
	Mode      string `json:"mode,omitempty"`
	URL       string `json:"url,omitempty"`
	Token     string `json:"token,omitempty"`
	PublicURL string `json:"public_url,omitempty"`
}

// Settings are the box-wide values plus one person's preferences and their
// own connections.
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`

	DateFormat string `json:"date_format"`
	TimeFormat string `json:"time_format"`
	WeekStart  int    `json:"week_start"` // 1 = Monday, 7 = Sunday

	// A new song starts with these.
	Tempo   int  `json:"tempo"`    // beats per minute
	Beats   int  `json:"beats"`    // beats in a bar
	CountIn int  `json:"count_in"` // bars of clicks before recording
	Click   bool `json:"click"`    // the click plays while recording

	// Recording.
	ClickSound  string `json:"click_sound"`  // click | wood | beep
	ClickVolume int    `json:"click_volume"` // 0..100
	Stereo      bool   `json:"stereo"`       // record two channels (a stereo interface)
	Monitor     bool   `json:"monitor"`      // hear yourself in the headphones while recording
	LevelTakes  bool   `json:"level_takes"`  // raise a quiet take to a normal level when it is kept
	KeepAwake   bool   `json:"keep_awake"`   // keep the screen on while a song is open

	// The tuner.
	A4          int    `json:"a4"`           // reference pitch, 415..466 Hz
	TunerPreset string `json:"tuner_preset"` // chromatic | guitar | …

	// Practice time goes on to: off | guitar | time.
	PracticeTo  string `json:"practice_to"`
	FilesFolder string `json:"files_folder"` // where mixes go in Pocket Files

	// Each sibling Pocket app, by slug.
	Conns map[string]Conn `json:"conns"`
}

// Song is a song (or an idea, an exercise) with its tracks.
type Song struct {
	ID          int64   `json:"id"`
	UserID      int64   `json:"-"`
	Title       string  `json:"title"`
	Artist      string  `json:"artist"`
	Notes       string  `json:"notes"`
	Tempo       float64 `json:"tempo"`
	Beats       int     `json:"beats"`
	BeatUnit    int     `json:"beat_unit"`
	KeyName     string  `json:"key_name"`
	CountIn     int     `json:"count_in"`
	Click       bool    `json:"click"`
	LoopStartMS int64   `json:"loop_start_ms"`
	LoopEndMS   int64   `json:"loop_end_ms"`
	Starred     bool    `json:"starred"`
	Color       string  `json:"color"`
	Source      string  `json:"source"`
	BandID      *int64  `json:"band_id"`
	BandName    string  `json:"band_name"`
	OpenedAt    *string `json:"opened_at"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`

	// Filled for lists and the song page.
	TrackCount int     `json:"track_count"`
	TakeCount  int     `json:"take_count"`
	LengthMS   int64   `json:"length_ms"`
	Minutes    int     `json:"minutes"` // time spent on it
	Tracks     []Track `json:"tracks,omitempty"`
	Marks      []Mark  `json:"marks,omitempty"`
}

// Band is a folder for songs.
type Band struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	Notes     string `json:"notes"`
	CreatedAt string `json:"created_at"`
	Songs     int    `json:"songs"`      // how many songs are in it
	UpdatedAt string `json:"updated_at"` // its newest song's change ('' when empty)
}

// Mark is a note pinned to a moment (or a stretch) of a song, on the whole
// song (TrackID nil) or on one track.
type Mark struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	SongID    int64  `json:"song_id"`
	TrackID   *int64 `json:"track_id"`
	AtMS      int64  `json:"at_ms"`
	EndMS     int64  `json:"end_ms"`
	Text      string `json:"text"`
	Color     string `json:"color"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// Track is one instrument or voice in a song.
type Track struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"-"`
	SongID     int64  `json:"song_id"`
	Name       string `json:"name"`
	Instrument string `json:"instrument"`
	Color      string `json:"color"`
	Volume     int    `json:"volume"`
	Pan        int    `json:"pan"`
	Muted      bool   `json:"muted"`
	Solo       bool   `json:"solo"`
	Position   int    `json:"position"`
	TakeID     *int64 `json:"take_id"`
	CreatedAt  string `json:"created_at"`
	Takes      []Take `json:"takes"`
}

// Take is one recording on a track.
type Take struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"-"`
	SongID     int64  `json:"song_id"`
	TrackID    int64  `json:"track_id"`
	Number     int    `json:"number"`
	File       string `json:"-"`
	Mime       string `json:"mime"`
	Size       int64  `json:"size"`
	DurationMS int64  `json:"duration_ms"`
	OffsetMS   int64  `json:"offset_ms"`
	LatencyMS  int64  `json:"latency_ms"`
	SampleRate int    `json:"sample_rate"`
	Channels   int    `json:"channels"`
	Peaks      string `json:"peaks"`
	Source     string `json:"source"`
	SourceRef  string `json:"source_ref"`
	Device     string `json:"device"`
	Note       string `json:"note"`
	Liked      bool   `json:"liked"`
	CreatedAt  string `json:"created_at"`
}

// Mix is a song's tracks mixed down to one file.
type Mix struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"-"`
	SongID     *int64 `json:"song_id"`
	SongTitle  string `json:"song_title"`
	Name       string `json:"name"`
	File       string `json:"-"`
	Mime       string `json:"mime"`
	Size       int64  `json:"size"`
	DurationMS int64  `json:"duration_ms"`
	Peaks      string `json:"peaks"`
	Tracks     int    `json:"tracks"`
	FilesPath  string `json:"files_path"`
	FilesURL   string `json:"files_url"`
	CreatedAt  string `json:"created_at"`
}

// Idea is a quick recording on its own, not in a song yet.
type Idea struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"-"`
	ClientID   string `json:"client_id"`
	Name       string `json:"name"`
	File       string `json:"-"`
	Mime       string `json:"mime"`
	Size       int64  `json:"size"`
	DurationMS int64  `json:"duration_ms"`
	SampleRate int    `json:"sample_rate"`
	Channels   int    `json:"channels"`
	Peaks      string `json:"peaks"`
	Source     string `json:"source"`
	Device     string `json:"device"`
	App        string `json:"-"` // the Pocket app that added it ("" = here)
	CreatedAt  string `json:"created_at"`
}

// Sitting is time spent on one song in one go.
type Sitting struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"-"`
	SongID      *int64 `json:"song_id"`
	Title       string `json:"title"`
	Day         string `json:"day"`
	StartedAt   string `json:"started_at"`
	LastAt      string `json:"last_at"`
	Seconds     int    `json:"seconds"`
	Takes       int    `json:"takes"`
	SentTo      string `json:"sent_to"`
	SentRef     string `json:"-"`
	SentSeconds int    `json:"-"`
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
