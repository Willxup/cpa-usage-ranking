import { readFileSync, writeFileSync } from 'node:fs'
import { expect, test, type Locator, type Page } from '@playwright/test'

const config = process.env.RANKING_TEST_ADMIN_CONFIG
const baselineURL = process.env.RANKING_BASELINE_URL
const credentials: { username: string; password: string } = config ? JSON.parse(readFileSync(config, 'utf8')) : { username: '', password: '' }
test.skip(!config, 'Requires an isolated enabled console')

type Frame = { time: number; path: string; header: number | null; icon: number | null; text: number | null; scroll: number; focus: string }
type Capture = { frames: Frame[]; done: boolean }

async function settleAtTop(page: Page) {
  await page.evaluate(async () => {
    window.scrollTo({ top: 0, behavior: 'instant' })
    await Promise.all(document.getAnimations().map((animation) => animation.finished.catch(() => {})))
    await new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())))
  })
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(0)
}

async function sampleTransition(page: Page, link: Locator): Promise<Frame[]> {
  await settleAtTop(page)
  await page.evaluate(() => {
    const state: Capture = { frames: [], done: false }
    ;(window as unknown as { headerCapture: Capture }).headerCapture = state
    const started = performance.now()
    const sample = () => {
      const shell = document.querySelector('.route-view:not([hidden]) .app-shell')
      const top = (selector: string) => shell?.querySelector(selector)?.getBoundingClientRect().top ?? null
      state.frames.push({ time: performance.now() - started, path: location.pathname, header: top('.site-header'), icon: top('.brand .project-icon'), text: top('.brand span'), scroll: window.scrollY, focus: document.activeElement?.tagName ?? '' })
      if (performance.now() - started < 400) requestAnimationFrame(sample)
      else state.done = true
    }
    sample()
  })
  await link.click({ force: true })
  await page.waitForFunction(() => (window as unknown as { headerCapture: Capture }).headerCapture.done)
  return page.evaluate(() => (window as unknown as { headerCapture: Capture }).headerCapture.frames)
}

function range(frames: Frame[], key: 'header' | 'icon' | 'text') {
  const values = frames.map((frame) => frame[key])
  expect(values.every((value) => value !== null), `${key} was missing in a frame`).toBe(true)
  const numbers = values as number[]
  return Math.max(...numbers) - Math.min(...numbers)
}

async function logIn(page: Page, origin: string) {
  await page.goto(`${origin}/admin/login`)
  await page.getByLabel('Username', { exact: true }).fill(credentials.username)
  await page.getByLabel('Password', { exact: true }).fill(credentials.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('navigation', { name: 'Console navigation' })).toBeVisible()
}

test('keeps the header and brand stable in every frame across both routes', async ({ page, baseURL }, info) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await logIn(page, baseURL!)
  const header = page.getByRole('navigation', { name: 'Page navigation' })
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: width === 1440 ? 1000 : 812 })
    if (new URL(page.url()).pathname !== '/') await header.getByRole('link', { name: 'Leaderboards' }).click()
    await expect(header.getByRole('link', { name: 'Console' })).toBeVisible()
    const toAdmin = await sampleTransition(page, header.getByRole('link', { name: 'Console' }))
    const toPublic = await sampleTransition(page, header.getByRole('link', { name: 'Leaderboards' }))
    writeFileSync(info.outputPath(`frames-${width}.json`), JSON.stringify({ toAdmin, toPublic }, null, 2))
    for (const frames of [toAdmin, toPublic]) {
      expect(frames.length).toBeGreaterThan(10)
      expect(new Set(frames.map((frame) => frame.path)).size).toBe(2)
      expect(frames.every((frame) => frame.scroll === 0)).toBe(true)
      for (const key of ['header', 'icon', 'text'] as const) expect(range(frames, key), key).toBeLessThan(0.25)
    }
    await page.screenshot({ path: info.outputPath(`public-${width}.png`), fullPage: true })
  }
})

test('records the previous full-shell animation across the same transitions', async ({ browser }, info) => {
  test.skip(!baselineURL, 'Requires an isolated baseline service')
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, reducedMotion: 'no-preference' })
  try {
    const page = await context.newPage()
    await logIn(page, baselineURL!)
    const header = page.getByRole('navigation', { name: 'Page navigation' })
    for (const width of [1440, 375]) {
      await page.setViewportSize({ width, height: width === 1440 ? 1000 : 812 })
      if (new URL(page.url()).pathname !== '/') await header.getByRole('link', { name: 'Leaderboards' }).click()
      const toAdmin = await sampleTransition(page, header.getByRole('link', { name: 'Console' }))
      const toPublic = await sampleTransition(page, header.getByRole('link', { name: 'Leaderboards' }))
      expect(range(toAdmin, 'header')).toBeGreaterThan(1)
      expect(range(toPublic, 'header')).toBeGreaterThan(1)
      writeFileSync(info.outputPath(`baseline-frames-${width}.json`), JSON.stringify({ toAdmin, toPublic }, null, 2))
    }
  } finally { await context.close() }
})
