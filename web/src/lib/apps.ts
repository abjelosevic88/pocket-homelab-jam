// The other Pocket apps Jam works with.
import type { AppSlug, AppStateName } from './types'

export const APP_INFO: Record<AppSlug, { name: string; icon: string }> = {
  files: { name: 'Pocket Files', icon: 'archive' }, music: { name: 'Pocket Music', icon: 'music' },
  guitar: { name: 'Pocket Guitar', icon: 'key-music' }, time: { name: 'Pocket Time', icon: 'timer' },
}

export const STATE_WORDS: Record<AppStateName, string> = {
  on: 'Connected', partly: 'Partly connected', off: 'Off', link_off: 'Link is off in Pocket Core', missing: 'Not on this box', old: 'Needs an update',
  no_core: 'No Pocket Core', not_set_up: 'Not set up', error: 'Not working',
}
