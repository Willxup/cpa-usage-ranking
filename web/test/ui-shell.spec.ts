import { readFileSync, writeFileSync } from 'node:fs'
import { expect, test, type Page } from '@playwright/test'

const config = process.env.RANKING_TEST_ADMIN_CONFIG
const baselineURL = process.env.RANKING_BASELINE_URL
const credentials: { username: string; password: string } = config ? JSON.parse(readFileSync(config, 'utf8')) : { username: '', password: '' }
test.skip(!config, 'Requires an isolated enabled console')
test.use({ launchOptions: { ignoreDefaultArgs: ['--hide-scrollbars'], ...(process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH } : {}) } })

type Position = { header: number[]; main: number[]; footer: number[]; icon: number[]; text: number[]; scroll: boolean; width: number; clientWidth: number }
async function position(page: Page): Promise<Position> {
  return page.evaluate(() => {
    const shell = document.querySelector('.route-view:not([hidden]) .app-shell')!
    const rect = (selector: string) => { const box = shell.querySelector(selector)!.getBoundingClientRect(); return [box.left, box.right] }
    return { header: rect('.site-header'), main: rect('main'), footer: rect('footer'), icon: rect('.brand .project-icon'), text: rect('.brand span'), scroll: document.documentElement.scrollHeight > innerHeight + 1, width: innerWidth, clientWidth: document.documentElement.clientWidth }
  })
}
function sameHorizontal(left: Position, right: Position) {
  for (const key of ['header', 'main', 'footer', 'icon', 'text'] as const) {
    for (let i = 0; i < 2; i++) expect(Math.abs(left[key][i] - right[key][i]), `${key}[${i}]`).toBeLessThan(0.1)
  }
}

test('keeps console tabs and public-to-console navigation horizontally aligned', async ({ page }, info) => {
  let documents = 0
  page.on('request', (request) => { if (request.isNavigationRequest() && request.resourceType() === 'document') documents++ })
  await page.goto('/admin/login')
  const header = page.getByRole('navigation', { name: 'Page navigation' })
  await expect(header.getByRole('link')).toHaveText(['Leaderboards', 'Sign in'])
  await expect(header.getByRole('link', { name: 'Sign in' })).toHaveAttribute('aria-current', 'page')
  await page.getByLabel('Username', { exact: true }).fill(credentials.username)
  await page.getByLabel('Password', { exact: true }).fill(credentials.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  const consoleNav = page.getByRole('navigation', { name: 'Console navigation' })
  await expect(consoleNav).toBeVisible()
  await expect(header.getByRole('link')).toHaveText(['Leaderboards', 'Console'])
  await expect(header.getByRole('link', { name: 'Console' })).toHaveAttribute('aria-current', 'page')
  const logout = page.getByRole('button', { name: 'Sign out', exact: true })
  expect(await logout.evaluate((node) => [getComputedStyle(node).color, getComputedStyle(node).borderWidth])).toEqual(['rgb(190, 52, 52)', '3px'])
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: 1000 })
    await consoleNav.getByRole('link', { name: 'Leaderboards', exact: true }).click()
    await page.getByRole('button', { name: 'Active Board', exact: true }).click()
    const current = page.locator('.admin-shell section[aria-label="Leaderboard moderation"] > .tab-view').first()
    await current.evaluate((node) => { (node as HTMLElement).style.minHeight = '2000px' })
    const longSub = await position(page)
    await page.getByRole('button', { name: 'Excluded Users', exact: true }).click()
    await expect(page.getByRole('region', { name: 'Excluded Users' })).toBeVisible()
    const shortSub = await position(page)
    sameHorizontal(longSub, shortSub)
    await page.getByRole('button', { name: 'Active Board', exact: true }).click()
    const longMain = await position(page)
    await consoleNav.getByRole('link', { name: 'Users', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Users' })).toBeVisible()
    const users = await position(page)
    await consoleNav.getByRole('link', { name: 'Administrators', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Administrators', exact: true })).toBeVisible()
    const accounts = await position(page)
    const footer = page.locator('.admin-shell .brand-footer')
    await expect(footer).toHaveText('CPA Usage Ranking')
    await expect(footer.locator('img')).toHaveAttribute('src', '/assets/brand/logo.svg')
    const footerBox = (await footer.boundingBox())!
    const footerBrand = (await footer.locator('span').boundingBox())!
    expect(Math.abs(footerBrand.x + footerBrand.width / 2 - footerBox.x - footerBox.width / 2)).toBeLessThan(1)
    sameHorizontal(longMain, users); sameHorizontal(users, accounts)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    expect(longSub.scroll).toBe(true)
    expect(shortSub.scroll).toBe(false)
    expect(users.scroll).toBe(false)
    await page.screenshot({ path: info.outputPath(`tabs-${width}.png`), fullPage: true })
    await header.getByRole('link', { name: 'Leaderboards' }).click()
    await expect(header.getByRole('link', { name: 'Leaderboards' })).toHaveAttribute('aria-current', 'page')
    await page.locator('.route-view:not([hidden]) main').evaluate((node) => { (node as HTMLElement).style.minHeight = '2000px' })
    const publicLong = await position(page)
    sameHorizontal(accounts, publicLong)
    await page.screenshot({ path: info.outputPath(`public-${width}.png`), fullPage: true })
    await header.getByRole('link', { name: 'Console' }).click()
    await expect(header.getByRole('link', { name: 'Console' })).toHaveAttribute('aria-current', 'page')
    const adminAgain = await position(page)
    sameHorizontal(publicLong, adminAgain)
    await page.goBack()
    await expect(header.getByRole('link', { name: 'Leaderboards' })).toHaveAttribute('aria-current', 'page')
    await page.goForward()
    await expect(header.getByRole('link', { name: 'Console' })).toHaveAttribute('aria-current', 'page')
    writeFileSync(info.outputPath(`positions-${width}.json`), JSON.stringify({ longSub, shortSub, longMain, users, accounts, publicLong, adminAgain }, null, 2))
  }
  expect(documents).toBe(1)
  const previousURL = page.url()
  const popupPromise = page.context().waitForEvent('page')
  await header.getByRole('link', { name: 'Leaderboards' }).click({ modifiers: ['Control'] })
  const popup = await popupPromise
  await popup.waitForLoadState('domcontentloaded')
  expect(new URL(popup.url()).pathname).toBe('/')
  expect(page.url()).toBe(previousURL)
  await popup.close()
})

test('records the earlier tab and shell geometry for comparison', async ({ browser }, info) => {
  test.skip(!baselineURL, 'Requires an isolated baseline service')
  const context = await browser.newContext({ baseURL: baselineURL, viewport: { width: 1440, height: 1000 } })
  try {
    const page = await context.newPage()
    await page.goto('/admin/login')
    await page.getByLabel('Username', { exact: true }).fill(credentials.username)
    await page.getByLabel('Password', { exact: true }).fill(credentials.password)
    await page.getByRole('button', { name: 'Sign in', exact: true }).click()
    const consoleNav = page.getByRole('navigation', { name: 'Console navigation' })
    await expect(consoleNav).toBeVisible()
    for (const width of [1440, 375]) {
      await page.setViewportSize({ width, height: 1000 })
      await consoleNav.getByRole('link', { name: 'Leaderboards', exact: true }).click()
      await page.getByRole('button', { name: 'Active Board', exact: true }).click()
      await page.locator('.admin-shell section[aria-label="Leaderboard moderation"] > .tab-view').first().evaluate((node) => { (node as HTMLElement).style.minHeight = '2000px' })
      const long = await position(page)
      await page.getByRole('button', { name: 'Excluded Users', exact: true }).click()
      await expect(page.getByRole('heading', { name: 'Excluded Users' })).toBeVisible()
      const short = await position(page)
      await consoleNav.getByRole('link', { name: 'Users', exact: true }).click()
      await expect(page.getByRole('heading', { name: 'Users' })).toBeVisible()
      const admin = await position(page)
      await page.getByRole('navigation', { name: 'Page navigation' }).getByRole('link', { name: 'Leaderboards' }).click()
      await expect(page.getByRole('heading', { name: 'CPA Usage' })).toBeVisible()
      const publicPage = await position(page)
      writeFileSync(info.outputPath(`baseline-positions-${width}.json`), JSON.stringify({ long, short, admin, publicPage }, null, 2))
      await page.getByRole('link', { name: 'Console', exact: true }).click()
    }
  } finally { await context.close() }
})
