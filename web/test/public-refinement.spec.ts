import { readFileSync } from 'node:fs'
import { expect, test } from '@playwright/test'

const config = process.env.RANKING_TEST_ADMIN_CONFIG
const credentials: { username: string; password: string } = config ? JSON.parse(readFileSync(config, 'utf8')) : { username: '', password: '' }

test('keeps public navigation consistent for anonymous and authenticated sessions', async ({ page }, info) => {
  test.skip(!config, 'Requires an isolated enabled console')
  let documents = 0
  page.on('request', (request) => { if (request.isNavigationRequest() && request.resourceType() === 'document') documents++ })
  await page.goto('/')
  const navigation = page.getByRole('navigation', { name: '页面导航' })
  const publicRoute = page.locator('.route-view').first()
  await publicRoute.evaluate((node) => {
    const element = node as HTMLElement
    element.dataset.enters = '0'
    element.addEventListener('animationstart', (event) => { if (event.target === element) element.dataset.enters = String(Number(element.dataset.enters) + 1) })
  })
  const check = async (entry: '登录' | '控制台') => {
    await expect(navigation.getByRole('link')).toHaveText(['排行榜', entry])
    const styles = await navigation.getByRole('link').evaluateAll((links) => links.map((link) => {
      const css = getComputedStyle(link)
      return [css.fontFamily, css.fontSize, css.fontWeight, css.borderWidth, css.padding]
    }))
    expect(styles[0]).toEqual(styles[1])
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  }
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: width === 1440 ? 1000 : 812 })
    await check('登录')
    await page.screenshot({ path: info.outputPath(`anonymous-${width}.png`), fullPage: true })
  }
  await navigation.getByRole('link', { name: '登录' }).click()
  await page.getByLabel('用户名', { exact: true }).fill(credentials.username)
  await page.getByLabel('密码', { exact: true }).fill(credentials.password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('navigation', { name: '控制台导航' })).toBeVisible()
  await page.getByRole('navigation', { name: '页面导航' }).getByRole('link', { name: '排行榜' }).click()
  await expect.poll(async () => Number(await publicRoute.getAttribute('data-enters'))).toBeGreaterThan(0)
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: width === 1440 ? 1000 : 812 })
    await check('控制台')
    await page.screenshot({ path: info.outputPath(`authenticated-${width}.png`), fullPage: true })
  }
  await navigation.getByRole('link', { name: '控制台' }).click()
  await expect(page.getByRole('navigation', { name: '控制台导航' })).toBeVisible()
  expect(documents).toBe(1)
})

test('keeps route, tab and result transitions light, interruptible and optional', async ({ page }, info) => {
  test.skip(!config, 'Requires an isolated enabled console')
  let documents = 0
  page.on('request', (request) => { if (request.isNavigationRequest() && request.resourceType() === 'document') documents++ })
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '今日 · 综合总榜' })).toBeVisible()
  const publicRoute = page.locator('.route-view').first()
  await publicRoute.evaluate((node) => { (node as HTMLElement).dataset.preserved = 'yes' })
  expect(await publicRoute.evaluate((node) => getComputedStyle(node).animationName)).toBe('view-enter')
  const firstResult = page.locator('.leaderboard-result')
  await firstResult.evaluate((node) => { (node as HTMLElement).dataset.old = 'yes' })
  await page.getByRole('button', { name: '用量榜' }).click()
  await expect(page.getByRole('heading', { name: '今日 · 用量榜' })).toBeVisible()
  await expect(page.locator('.leaderboard-result')).not.toHaveAttribute('data-old', 'yes')
  expect(await page.locator('.leaderboard-result').evaluate((node) => getComputedStyle(node).animationDuration)).toBe('0.16s')
  const boardTime = await page.evaluate(() => {
    const start = performance.now()
    for (const name of ['缓存复用榜', '成功率榜', '昨日']) {
      const button = Array.from(document.querySelectorAll('button')).find((item) => item.textContent === name)!
      button.click()
    }
    return performance.now() - start
  })
  expect(boardTime).toBeLessThan(160)
  await expect(page.getByRole('heading', { name: '昨日 · 成功率榜' })).toBeVisible()
  await page.getByRole('link', { name: '登录', exact: true }).click()
  await page.getByLabel('用户名', { exact: true }).fill(credentials.username)
  await page.getByLabel('密码', { exact: true }).fill(credentials.password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  const navigation = page.getByRole('navigation', { name: '控制台导航' })
  await expect(navigation).toBeVisible()
  await navigation.evaluate((node) => { (node as HTMLElement).dataset.preserved = 'yes' })
  const tabTime = await navigation.evaluate((node) => {
    const start = performance.now()
    for (const name of ['用户管理', '管理员管理', '用户管理']) {
      const link = Array.from(node.querySelectorAll('a')).find((item) => item.textContent === name)!
      link.click()
    }
    return performance.now() - start
  })
  expect(tabTime).toBeLessThan(160)
  await expect(page.getByRole('heading', { name: '用户管理' })).toBeVisible()
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: width === 1440 ? 1000 : 812 })
    await navigation.getByRole('link', { name: '用户管理' }).click()
    await expect(page.getByRole('heading', { name: '用户管理' })).toBeVisible()
    await navigation.getByRole('link', { name: '管理员管理' }).click()
    await expect(page.getByRole('heading', { name: '管理员管理', exact: true })).toBeVisible()
    await page.goBack()
    await expect(page.getByRole('heading', { name: '用户管理' })).toBeVisible()
    await page.goForward()
    await expect(page.getByRole('heading', { name: '管理员管理', exact: true })).toBeVisible()
    await expect(navigation).toHaveAttribute('data-preserved', 'yes')
    expect(await page.locator('.admin-main > .tab-view:not([hidden])').evaluate((node) => getComputedStyle(node).animationName)).toBe('view-enter')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: info.outputPath(`admin-tab-${width}.png`), fullPage: true })
  }
  await navigation.getByRole('link', { name: '榜单管理' }).click()
  await page.getByRole('button', { name: '已下榜用户' }).click()
  await expect(page.getByRole('heading', { name: '已下榜用户' })).toBeVisible()
  expect(await page.locator('.tab-view:not([hidden]) .tab-view:not([hidden])').evaluate((node) => getComputedStyle(node).animationName)).toBe('view-enter')
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await navigation.getByRole('link', { name: '用户管理' }).click()
  expect(await page.locator('.admin-main > .tab-view:not([hidden])').evaluate((node) => getComputedStyle(node).animationName)).toBe('none')
  await page.getByRole('navigation', { name: '页面导航' }).getByRole('link', { name: '排行榜' }).click()
  await expect(publicRoute).toHaveAttribute('data-preserved', 'yes')
  expect(await publicRoute.evaluate((node) => getComputedStyle(node).animationName)).toBe('none')
  await page.getByRole('button', { name: '本周' }).click()
  await expect(page.getByRole('heading', { name: '本周 · 成功率榜' })).toBeVisible()
  expect(await page.locator('.leaderboard-result').evaluate((node) => getComputedStyle(node).animationName)).toBe('none')
  expect(await page.getByRole('button', { name: '本周' }).evaluate((node) => getComputedStyle(node).transitionDuration)).toBe('0s')
  await page.screenshot({ path: info.outputPath('public-reduced-375.png'), fullPage: true })
  expect(documents).toBe(1)
})
