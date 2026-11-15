// Pocket Jam's recorder: copies the microphone's samples out of the audio
// thread with the frame they were captured at, so a take can be placed on the
// song's timeline to the sample. Batches ~4096 frames per message.
class JamRecorder extends AudioWorkletProcessor {
  constructor() {
    super()
    this.on = false
    this.chunks = []
    this.frames = 0
    this.first = -1
    this.port.onmessage = e => {
      if (e.data === 'start') { this.on = true; this.chunks = []; this.frames = 0; this.first = -1 }
      if (e.data === 'stop') { this.flush(); this.on = false; this.port.postMessage({ done: true }) }
    }
  }
  flush() {
    if (!this.chunks.length) return
    const chans = this.chunks[0].length
    const out = []
    for (let c = 0; c < chans; c++) {
      const buf = new Float32Array(this.frames)
      let at = 0
      for (const ch of this.chunks) { buf.set(ch[c], at); at += ch[c].length }
      out.push(buf)
    }
    this.port.postMessage({ first: this.first, data: out }, out.map(b => b.buffer))
    this.chunks = []
    this.frames = 0
    this.first = -1
  }
  process(inputs) {
    const input = inputs[0]
    if (this.on && input && input.length) {
      if (this.first < 0) this.first = currentFrame
      this.chunks.push(input.map(ch => ch.slice(0)))
      this.frames += input[0].length
      if (this.frames >= 4096) this.flush()
    }
    return true
  }
}
registerProcessor('jam-recorder', JamRecorder)
