import { readFileSync } from 'node:fs'
import { expect, test } from '@playwright/test'

const config = process.env.RANKING_TEST_ADMIN_CONFIG
const credentials: { username: string; password: string } = config ? JSON.parse(readFileSync(config, 'utf8')) : { username: '', password: '' }

test('keeps public navigation consistent for anonymous and authenticated sessions', async ({ page }, info) => {
  test.skip(!config, 'Requires an isolated enabled console')
  let documents = 0
  page.on('request', (request) => { if (request.isNavigationRequest() && request.resourceType() === 'document') documents++ })
  await page.goto('/')
  const navigation = page.getByRole('navigation', { name: 'Page navigation' })
  const publicRoute = page.locator('.route-view').first()
  const publicMain = publicRoute.locator('main')
  await publicMain.evaluate((node) => {
    const element = node as HTMLElement
    element.dataset.enters = '0'
    element.addEventListener('animationstart', (event) => { if (event.target === element) element.dataset.enters = String(Number(element.dataset.enters) + 1) })
  })
  const check = async (entry: 'Sign in' | 'Console') => {
    await expect(navigation.getByRole('link')).toHaveText(['Leaderboards', entry])
    const styles = await navigation.getByRole('link').evaluateAll((links) => links.map((link) => {
      const css = getComputedStyle(link)
      return [css.fontFamily, css.fontSize, css.fontWeight, css.borderWidth, css.padding]
    }))
    expect(styles[0]).toEqual(styles[1])
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  }
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: width === 1440 ? 1000 : 812 })
    await check('Sign in')
    await page.screenshot({ path: info.outputPath(`anonymous-${width}.png`), fullPage: true })
  }
  await navigation.getByRole('link', { name: 'Sign in' }).click()
  await page.getByLabel('Username', { exact: true }).fill(credentials.username)
  await page.getByLabel('Password', { exact: true }).fill(credentials.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('navigation', { name: 'Console navigation' })).toBeVisible()
  const adminBoard = page.getByRole('region', { name: 'Leaderboard moderation' })
  for (const period of ['Weekly', 'Monthly']) {
    await adminBoard.getByRole('button', { name: period, exact: true }).click()
    await expect(adminBoard.getByRole('heading', { name: `${period} · Overall` })).toBeVisible()
  }
  await page.getByRole('navigation', { name: 'Page navigation' }).getByRole('link', { name: 'Leaderboards' }).click()
  await expect.poll(async () => Number(await publicMain.getAttribute('data-enters'))).toBeGreaterThan(0)
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: width === 1440 ? 1000 : 812 })
    await check('Console')
    await page.screenshot({ path: info.outputPath(`authenticated-${width}.png`), fullPage: true })
  }
  await navigation.getByRole('link', { name: 'Console' }).click()
  await expect(page.getByRole('navigation', { name: 'Console navigation' })).toBeVisible()
  expect(documents).toBe(1)
})

test('keeps route, tab and result transitions light, interruptible and optional', async ({ page }, info) => {
  test.skip(!config, 'Requires an isolated enabled console')
  let documents = 0
  page.on('request', (request) => { if (request.isNavigationRequest() && request.resourceType() === 'document') documents++ })
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Today · Overall' })).toBeVisible()
  const publicRoute = page.locator('.route-view').first()
  await publicRoute.evaluate((node) => { (node as HTMLElement).dataset.preserved = 'yes' })
  expect(await publicRoute.evaluate((node) => getComputedStyle(node).animationName)).toBe('none')
  expect(await publicRoute.locator('main').evaluate((node) => getComputedStyle(node).animationName)).toBe('view-enter')
  expect(await publicRoute.locator('header').evaluate((node) => getComputedStyle(node).animationName)).toBe('none')
  const firstResult = page.locator('.leaderboard-result')
  await firstResult.evaluate((node) => { (node as HTMLElement).dataset.old = 'yes' })
  await page.getByRole('button', { name: 'Token Usage' }).click()
  await expect(page.getByRole('heading', { name: 'Today · Token Usage' })).toBeVisible()
  await expect(page.locator('.leaderboard-result')).not.toHaveAttribute('data-old', 'yes')
  expect(await page.locator('.leaderboard-result').evaluate((node) => getComputedStyle(node).animationDuration)).toBe('0.16s')
  const boardTime = await page.evaluate(() => {
    const start = performance.now()
    for (const name of ['Cache Reuse', 'Reliability', 'Yesterday']) {
      const button = Array.from(document.querySelectorAll('button')).find((item) => item.textContent === name)!
      button.click()
    }
    return performance.now() - start
  })
  expect(boardTime).toBeLessThan(160)
  await expect(page.getByRole('heading', { name: 'Yesterday · Reliability' })).toBeVisible()
  await page.getByRole('link', { name: 'Sign in', exact: true }).click()
  await page.getByLabel('Username', { exact: true }).fill(credentials.username)
  await page.getByLabel('Password', { exact: true }).fill(credentials.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  const navigation = page.getByRole('navigation', { name: 'Console navigation' })
  await expect(navigation).toBeVisible()
  await navigation.evaluate((node) => { (node as HTMLElement).dataset.preserved = 'yes' })
  const tabTime = await navigation.evaluate((node) => {
    const start = performance.now()
    for (const name of ['Users', 'Administrators', 'Users']) {
      const link = Array.from(node.querySelectorAll('a')).find((item) => item.textContent === name)!
      link.click()
    }
    return performance.now() - start
  })
  expect(tabTime).toBeLessThan(160)
  await expect(page.getByRole('heading', { name: 'Users' })).toBeVisible()
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: width === 1440 ? 1000 : 812 })
    await navigation.getByRole('link', { name: 'Users' }).click()
    await expect(page.getByRole('heading', { name: 'Users' })).toBeVisible()
    await navigation.getByRole('link', { name: 'Administrators' }).click()
    await expect(page.getByRole('heading', { name: 'Administrators', exact: true })).toBeVisible()
    await page.goBack()
    await expect(page.getByRole('heading', { name: 'Users' })).toBeVisible()
    await page.goForward()
    await expect(page.getByRole('heading', { name: 'Administrators', exact: true })).toBeVisible()
    await expect(navigation).toHaveAttribute('data-preserved', 'yes')
    expect(await page.locator('.admin-main > .tab-view:not([hidden])').evaluate((node) => getComputedStyle(node).animationName)).toBe('view-enter')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: info.outputPath(`admin-tab-${width}.png`), fullPage: true })
  }
  await navigation.getByRole('link', { name: 'Leaderboards' }).click()
  await page.getByRole('button', { name: 'Excluded Users' }).click()
  await expect(page.getByRole('region', { name: 'Excluded Users' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Excluded Users' })).toHaveCount(0)
  expect(await page.locator('.tab-view:not([hidden]) .tab-view:not([hidden])').evaluate((node) => getComputedStyle(node).animationName)).toBe('view-enter')
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await navigation.getByRole('link', { name: 'Users' }).click()
  expect(await page.locator('.admin-main > .tab-view:not([hidden])').evaluate((node) => getComputedStyle(node).animationName)).toBe('none')
  await page.getByRole('navigation', { name: 'Page navigation' }).getByRole('link', { name: 'Leaderboards' }).click()
  await expect(publicRoute).toHaveAttribute('data-preserved', 'yes')
  expect(await publicRoute.evaluate((node) => getComputedStyle(node).animationName)).toBe('none')
  expect(await publicRoute.locator('main').evaluate((node) => getComputedStyle(node).animationName)).toBe('none')
  await page.getByRole('button', { name: 'Weekly' }).click()
  await expect(page.getByRole('heading', { name: 'Weekly · Reliability' })).toBeVisible()
  expect(await page.locator('.leaderboard-result').evaluate((node) => getComputedStyle(node).animationName)).toBe('none')
  expect(await page.getByRole('button', { name: 'Weekly' }).evaluate((node) => getComputedStyle(node).transitionDuration)).toBe('0s')
  await page.screenshot({ path: info.outputPath('public-reduced-375.png'), fullPage: true })
  expect(documents).toBe(1)
})
