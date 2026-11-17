import { Modal } from './ui'

const GROUPS: { title: string; keys: [string[], string][] }[] = [
  { title: 'Go to', keys: [[['h'], 'Home'], [['s'], 'Songs'], [['i'], 'Ideas'], [['m'], 'Mixes'], [['t'], 'Tuner'], [['k'], 'Metronome'], [[','], 'Settings']] },
  { title: 'Make', keys: [[['n'], 'New song']] },
  { title: 'In a song', keys: [[['space'], 'Play or stop'], [['r'], 'Record or stop recording'], [['0'], 'To the start'], [['c'], 'Click on or off'], [['m'], 'A note on the timeline, where the song is'], [['x'], 'Show or hide the mixer'], [['⌘', 'Z'], 'Undo moving a part']] },
  { title: 'Metronome', keys: [[['space'], 'Start or stop'], [['↑ ↓'], 'Faster or slower'], [['t'], 'Tap the tempo']] },
  { title: 'Find', keys: [[['/'], 'Search & commands'], [['⌘', 'K'], 'Search & commands']] },
  { title: 'Window', keys: [[['['], 'Show or hide the sidebar'], [['esc'], 'Close']] },
  { title: 'Help', keys: [[['?'], 'This sheet']] },
]

export function ShortcutsSheet({ onClose }: { onClose: () => void }) {
  return (
    <Modal title="Keyboard shortcuts" onClose={onClose} size="lg">
      <div className="shortcuts">
        {GROUPS.map(g => (
          <section key={g.title}>
            <h4 className="sc-h">{g.title}</h4>
            {g.keys.map(([keys, label]) => (
              <div key={label + keys.join('')} className="sc-row"><span>{label}</span><span className="sc-keys">{keys.map(k => <kbd key={k}>{k}</kbd>)}</span></div>
            ))}
          </section>
        ))}
      </div>
    </Modal>
  )
}
