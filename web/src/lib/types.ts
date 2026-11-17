// The shapes the API sends.

export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }
export interface APIToken { id: number; user_id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }

export type ConnMode = 'core' | 'direct' | 'off'
export type ConnVia = 'core' | 'direct' | ''
export type AppSlug = 'files' | 'music' | 'guitar' | 'time'
type ConnFields = { [K in AppSlug as `${K}_mode`]: ConnMode } & { [K in AppSlug as `${K}_via`]: ConnVia } & { [K in AppSlug as `${K}_detail` | `${K}_url` | `${K}_token` | `${K}_public_url` | `${K}_open_url`]: string } & { [K in AppSlug as `${K}_configured`]: boolean }

export type Settings = ConnFields & {
  core_configured: boolean
  core_registered?: boolean
  title: string; date_format: string; time_format: string; week_start: number
  tempo: number; beats: number; count_in: number; click: boolean
  click_sound: 'click' | 'wood' | 'beep'; click_volume: number; stereo: boolean; monitor: boolean; keep_awake: boolean; level_takes: boolean
  a4: number; tuner_preset: string; practice_to: 'off' | 'guitar' | 'time'; files_folder: string
  setup_complete: boolean; base_url: string; demo: boolean
}

export type AppStateName = 'on' | 'partly' | 'off' | 'link_off' | 'missing' | 'old' | 'no_core' | 'not_set_up' | 'error'
export interface AppState { slug: AppSlug; name: string; reason: string; mode: ConnMode; via: ConnVia; state: AppStateName; detail: string; fix: string; on: string[]; open_url: string }

export type Instrument = 'voice' | 'guitar' | 'bass' | 'keys' | 'drums' | 'strings' | 'wind' | 'backing' | 'other'

export interface Take {
  id: number; song_id: number; track_id: number; number: number; mime: string; size: number; duration_ms: number; offset_ms: number; latency_ms: number
  /** the part it is an attempt at (the id of the part's first take); `plays` = the attempt heard; where parts overlap, the higher layer is heard */
  part: number; plays: boolean; layer: number
  sample_rate: number; channels: number; peaks: string; source: 'recorded' | 'upload' | 'files' | 'music'; source_ref: string; device: string; note: string; liked: boolean; created_at: string
}

export interface Track {
  id: number; song_id: number; name: string; instrument: Instrument; color: string; volume: number; pan: number; muted: boolean; solo: boolean; position: number
  take_id: number | null; created_at: string; takes: Take[]
}

export interface Song {
  id: number; title: string; artist: string; notes: string; tempo: number; beats: number; beat_unit: number; key_name: string; count_in: number; click: boolean
  loop_start_ms: number; loop_end_ms: number; starred: boolean; color: string; source: string; opened_at: string | null; created_at: string; updated_at: string
  band_id: number | null; band_name: string
  track_count: number; take_count: number; length_ms: number; minutes: number; tracks?: Track[]; marks?: Mark[]
}

/** A folder for songs and ideas (the band a song is for). */
export interface Band { id: number; name: string; color: string; notes: string; created_at: string; songs: number; ideas: number; updated_at: string }

/** A note pinned to a moment of a song (or a stretch, when end_ms > at_ms); on one track, or the whole song when track_id is null. */
export interface Mark { id: number; song_id: number; track_id: number | null; at_ms: number; end_ms: number; text: string; color: string; created_at: string; updated_at: string }

export interface Mix {
  id: number; song_id: number | null; song_title: string; name: string; mime: string; size: number; duration_ms: number; peaks: string; tracks: number
  files_path: string; files_url: string; created_at: string
}

export interface Idea {
  id: number; client_id: string; name: string; mime: string; size: number; duration_ms: number; sample_rate: number; channels: number; peaks: string
  source: string; device: string; band_id: number | null; band_name: string; created_at: string
}

export interface Sitting { id: number; song_id: number | null; title: string; day: string; started_at: string; last_at: string; seconds: number; takes: number; sent_to: string }
export interface DayMinutes { day: string; minutes: number; takes: number }

export interface Home {
  recent: Song[]; starred: Song[]; songs: number; takes: number; mix_count: number; mixes: Mix[]; idea_count: number; days: DayMinutes[]
  today_minutes: number; week_minutes: number; week_takes: number; month_minutes: number; streak: number
}

export interface TimeReport { from: string; to: string; days: DayMinutes[]; sittings: Sitting[]; minutes: number; streak: number }
