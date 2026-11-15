// README screenshots and a browser smoke test against a demo instance:
//
//   make build && DATA_DIR=/tmp/jam-demo DEMO_DATA=true AUTH_MODE=none HOST=127.0.0.1 PORT=18278 ./pocket-jam &
//   CDP=http://127.0.0.1:9351 BASE=http://127.0.0.1:18278 node scripts/screenshots.mjs   # any Chrome with remote debugging
//   SHOTS=0 … node scripts/screenshots.mjs                                                # checks only
// playwright-core comes from web/node_modules, or set PW_FROM to a package.json next to an install of it.
import { createRequire } from 'node:module'
import assert from 'node:assert/strict'

const require = createRequire(process.env.PW_FROM || new URL('../web/package.json', import.meta.url))
let pw
try { pw = require('playwright') } catch { pw = require('playwright-core') }
const { chromium } = pw
const base = process.env.BASE || 'http://127.0.0.1:18278'
const shots = process.env.SHOTS !== '0'
const only = process.env.ONLY || ''
const dir = process.env.OUT || new URL('../docs/screenshots/', import.meta.url).pathname
const browser = process.env.CDP ? await chromium.connectOverCDP(process.env.CDP) : await chromium.launch()
const errors = []

async function session({ phone = false, dark = false, w = 1440, h = 900 } = {}) {
  const ctx = await browser.newContext({
    viewport: phone ? { width: 390, height: 844 } : { width: w, height: h },
    deviceScaleFactor: 2, isMobile: phone, hasTouch: phone, colorScheme: dark ? 'dark' : 'light',
  })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e}`))
  page.on('console', m => { if (m.type() === 'error' && !/Failed to load resource|ERR_/.test(m.text())) errors.push(m.text()) })
  return { ctx, page }
}
async function go(page, path, wait = 1400) {
  await page.goto(base + path, { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(wait)
}
async function shot(page, name) {
  if (!shots || (only && !only.split(',').includes(name))) return
  for (let i = 0; ; i++) {
    try { await page.bringToFront(); await page.screenshot({ path: dir + name + '.png', animations: 'disabled' }); return } catch (e) { if (i >= 3) throw e; await page.waitForTimeout(700) }
  }
}
const api = (page, path) => page.evaluate(p => fetch('/api/v1' + p).then(r => r.json()), path)

const { page } = await session()
await go(page, '/', 2000)
assert.ok((await page.content()).includes('Sunday Riff'), 'the home page lists the songs')
await shot(page, 'desktop-home')

await go(page, '/songs')
assert.ok((await page.content()).includes('Lullaby'), 'the songs list')
await shot(page, 'desktop-songs')

const songs = (await api(page, '/songs')).songs
const riff = songs.find(s => s.title === 'Sunday Riff')
await go(page, `/songs/${riff.id}`, 2600)
assert.equal(await page.locator('.tl-row .take').count(), 4, 'four takes on the timeline')
assert.ok(await page.locator('.tp-btn.tp-play:not([disabled])').count() === 1, 'the takes load and play is ready')
await shot(page, 'desktop-song')
await page.locator('.trk-name').nth(2).click()
await page.waitForTimeout(700)
assert.ok((await page.content()).includes('Take 3'), 'the track sheet lists the takes')
await shot(page, 'desktop-track')
await page.keyboard.press('Escape')

await go(page, '/mixes')
assert.ok((await page.content()).includes('Sunday Riff — mix 1'), 'the mixes')
await shot(page, 'desktop-mixes')
await go(page, '/tuner')
await shot(page, 'desktop-tuner')
await go(page, '/metronome')
await shot(page, 'desktop-metronome')
await go(page, '/time')
await shot(page, 'desktop-time')
await go(page, '/settings/recording')
assert.ok((await page.content()).includes('Measure the delay'), 'the recording settings')
await shot(page, 'desktop-settings')

const dark = await session({ dark: true })
await go(dark.page, `/songs/${riff.id}`, 2600)
await shot(dark.page, 'desktop-song-dark')

const phone = await session({ phone: true })
await go(phone.page, '/', 2000)
await shot(phone.page, 'phone-home')
await go(phone.page, `/songs/${riff.id}`, 2600)
await shot(phone.page, 'phone-song')
await go(phone.page, '/tuner')
await shot(phone.page, 'phone-tuner')
await go(phone.page, '/metronome')
await shot(phone.page, 'phone-metronome')
await go(phone.page, '/mixes')
await shot(phone.page, 'phone-mixes')

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('ok')
