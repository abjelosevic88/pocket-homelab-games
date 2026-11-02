// README screenshots and a browser smoke test, against a demo instance (DEMO_DATA=true seeds placeholder
// games and months of play; METADATA=false keeps every cover a generated placeholder — no real box art):
//
//   DATA_DIR=/tmp/games-demo DEMO_DATA=true METADATA=false AUTH_MODE=none PORT=18227 ./pocket-games
//   CDP=http://127.0.0.1:19291 BASE=http://127.0.0.1:18227 node scripts/screenshots.mjs   # any Chrome with remote debugging
//   SHOTS=0 … node scripts/screenshots.mjs                                                  # checks only
//   OUT=/tmp/shots … node scripts/screenshots.mjs                                           # somewhere else than docs/screenshots
// playwright-core comes from web/node_modules, or set PW_FROM to a package.json next to an install of it.
import { createRequire } from 'node:module'
import assert from 'node:assert/strict'

const require = createRequire(process.env.PW_FROM || new URL('../web/package.json', import.meta.url))
let pw
try { pw = require('playwright') } catch { pw = require('playwright-core') }
const { chromium } = pw
const base = process.env.BASE || 'http://127.0.0.1:18227'
const shots = process.env.SHOTS !== '0'
const dir = process.env.OUT ? process.env.OUT.replace(/\/?$/, '/') : new URL('../docs/screenshots/', import.meta.url).pathname
const browser = process.env.CDP ? await chromium.connectOverCDP(process.env.CDP) : await chromium.launch()
const errors = []

async function session({ phone = false, dark = false } = {}) {
  const ctx = await browser.newContext({
    viewport: phone ? { width: 390, height: 844 } : { width: 1440, height: 900 },
    deviceScaleFactor: phone ? 3 : 2, isMobile: phone, hasTouch: phone, colorScheme: dark ? 'dark' : 'light',
    userAgent: phone ? 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1' : undefined,
  })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e}`))
  page.on('console', m => { if (m.type() === 'error' && !/Failed to load resource/.test(m.text())) errors.push(m.text()) })
  await page.goto(base + '/login')
  return { ctx, page }
}
async function go(page, path, wait = 1500) {
  await page.goto(base + path, { waitUntil: 'networkidle' })
  // headless Chrome freezes CSS fade-ins: show everything at once
  await page.addStyleTag({ content: '*, *::before, *::after { transition: none !important; animation: none !important; }' })
  await page.evaluate(() => document.fonts.ready)
  await page.waitForTimeout(wait)
}
async function shot(page, name) { if (shots) await page.screenshot({ path: dir + name + '.png' }) }
const api = (page, p) => page.evaluate(u => fetch(u).then(r => r.json()), p)

const { ctx: c0, page: p0 } = await session()
// with AUTH_MODE=none the demo is seeded on the first request: wait for it
let list = await api(p0, '/api/v1/games?limit=200')
for (let i = 0; i < 20 && list.total < 30; i++) { await p0.waitForTimeout(500); list = await api(p0, '/api/v1/games?limit=200') }
assert.ok(list.total >= 30, 'demo games')
const byName = n => list.items.find(g => g.name === n)
const sotn = byName('Castlevania: Symphony of the Night'), aria = byName('Castlevania: Aria of Sorrow')
await c0.close()

// ---- desktop ----
{
  const { ctx, page } = await session()
  await go(page, '/')
  assert.ok(await page.getByText('Continue playing').first().isVisible(), 'home renders')
  await shot(page, 'desktop-home')
  await go(page, '/library')
  await shot(page, 'desktop-library')
  await go(page, `/games/${(aria || sotn).id}`)
  assert.ok(await page.getByRole('link', { name: /Play|Continue/ }).first().isVisible(), 'game page has Play')
  await shot(page, 'desktop-game')
  await go(page, '/platforms/psx')
  await shot(page, 'desktop-system')
  await go(page, '/stats')
  await shot(page, 'desktop-stats')
  await go(page, '/upload')
  await shot(page, 'desktop-upload')
  await go(page, '/settings/devices')
  await shot(page, 'desktop-devices')
  await ctx.close()
}

// ---- phone ----
{
  const { ctx, page } = await session({ phone: true })
  await go(page, '/')
  await shot(page, 'phone-home')
  await go(page, `/games/${(aria || sotn).id}`)
  await shot(page, 'phone-game')
  await ctx.close()
}
{
  const { ctx, page } = await session({ phone: true, dark: true })
  await go(page, '/library')
  await shot(page, 'phone-library-dark')
  await ctx.close()
}

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('ok')
