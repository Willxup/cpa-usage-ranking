import { expect, test } from '@playwright/test'

test('loads the real production build and checks the live Go process', async ({ page, baseURL }, info) => {
  const errors: string[] = []
  const externalRequests: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('console', (message) => { if (message.type() === 'error') errors.push(message.text()) })
  page.on('request', (request) => { if (new URL(request.url()).origin !== baseURL) externalRequests.push(request.url()) })
  await page.goto('/')
  await expect(page.getByRole('heading', { level: 1 })).toContainText('AI 用量排行榜')
  const response = page.waitForResponse((value) => new URL(value.url()).pathname === '/healthz')
  await page.getByRole('button', { name: '检查服务连接' }).click()
  expect((await response).status()).toBe(200)
  expect(await (await response).json()).toEqual({ status: 'ok' })
  await expect(page.getByRole('status')).toContainText('服务在线')
  await expect(page.getByText('尚未就绪')).toBeVisible()
  await expect(page.getByRole('heading', { name: '等待第一份真实数据' })).toBeVisible()
  await page.screenshot({ path: info.outputPath('desktop.png'), fullPage: true })
  expect(errors).toEqual([])
  expect(externalRequests).toEqual([])
})

test('uses compact desktop sizing without scaling down mobile or applying page zoom', async ({ page }) => {
  await page.goto('/')
  const heading = page.getByRole('heading', { level: 1 })
  expect(await heading.evaluate((node) => parseFloat(getComputedStyle(node).fontSize))).toBeLessThan(34)
  expect((await page.locator('.site-header').boundingBox())!.height).toBeLessThanOrEqual(72)
  expect(await page.locator('body').evaluate((node) => getComputedStyle(node).zoom)).toBe('1')
  expect((await page.locator('.brand .project-icon').boundingBox())!.width).toBe(62)
  expect(await page.locator('.brand').evaluate((node) => getComputedStyle(node).columnGap)).toBe('0px')
  await page.setViewportSize({ width: 375, height: 812 })
  expect(await heading.evaluate((node) => parseFloat(getComputedStyle(node).fontSize))).toBe(34)
  expect((await page.locator('.site-header').boundingBox())!.height).toBeGreaterThanOrEqual(116)
  expect((await page.locator('.brand .project-icon').boundingBox())!.width).toBe(52)
  expect(await page.locator('.brand').evaluate((node) => getComputedStyle(node).columnGap)).toBe('0px')
})

for (const width of [320, 375, 768]) {
  test(`keeps the page readable without horizontal overflow at ${width}px`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 812 })
    await page.goto('/')
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
    await expect(page.getByRole('navigation')).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    await page.getByRole('button', { name: '检查服务连接' }).click()
    await expect(page.getByRole('status')).toContainText('服务在线')
    if (width === 375) await page.screenshot({ path: info.outputPath('mobile.png'), fullPage: true })
  })
}

test('supports normal tab navigation and keyboard-only health checks', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('link', { name: '跳到主要内容' })).toHaveCount(0)
  for (const name of ['CPA Usage Ranking 首页', '排行榜', '模型洞察', '关于', '查看服务状态']) {
    await page.keyboard.press('Tab')
    await expect(page.getByRole('link', { name, exact: true })).toBeFocused()
  }
  await page.keyboard.press('Tab')
  await expect(page.getByRole('button', { name: '检查服务连接' })).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('status')).toContainText('服务在线')
})

test('respects reduced motion and stops decorative animation offscreen', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 500 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/')
  const button = page.getByRole('button', { name: '检查服务连接' })
  await button.scrollIntoViewIfNeeded()
  await expect(button).toHaveAttribute('data-motion', 'off')
  expect(await button.locator('.star-button__trail').first().evaluate((node) => getComputedStyle(node).animationName)).toBe('none')
  await button.click()
  await expect(page.getByRole('status')).toContainText('服务在线')
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await expect(button).toHaveAttribute('data-motion', 'on')
  await page.evaluate(() => window.scrollTo({ top: 0, behavior: 'instant' }))
  await expect(button).toHaveAttribute('data-motion', 'off')
})

test('shows a simulated service failure and retries against the real backend', async ({ page }) => {
  await page.route('**/healthz', (route) => route.fulfill({ status: 503, contentType: 'application/json', body: '{"error":"unavailable"}' }), { times: 1 })
  await page.goto('/')
  await page.getByRole('button', { name: '检查服务连接' }).click()
  await expect(page.getByRole('status')).toContainText('服务返回 503')
  await page.getByRole('button', { name: '检查服务连接' }).click()
  await expect(page.getByRole('status')).toContainText('服务在线')
})

test('preserves API and missing-resource boundaries and ships source notices', async ({ request }) => {
  for (const path of ['/api/missing', '/assets/missing.js', '/missing-page']) {
    const response = await request.get(path, { headers: { Accept: 'text/html' } })
    expect(response.status()).toBe(404)
    expect(response.headers()['content-type']).toContain('application/json')
  }
  const readiness = await request.get('/readyz')
  expect(readiness.status()).toBe(503)
  expect(await readiness.json()).toEqual({ status: 'not_ready', reason: 'business_not_implemented' })
  const notices = await request.get('/assets/third-party-notices.txt')
  expect(notices.status()).toBe(200)
  expect(await notices.text()).toContain('MIT + Commons Clause')
  expect(await notices.text()).toContain('Copyright (c) 2025 David Haz')
})

test('serves decodable project icons and browser favicon variants', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('.brand .project-icon')).toBeVisible()
  await expect(page.locator('link[rel="icon"][type="image/svg+xml"]')).toHaveAttribute('href', '/favicon.svg?v=2')
  const files = ['/favicon.svg', '/assets/brand/project-icon.svg', '/assets/brand/favicon.ico',
    ...[16, 32, 48].map((size) => `/assets/brand/favicon-${size}.png`),
    ...[180, 192, 512].map((size) => `/assets/brand/project-icon-${size}.png`)]
  for (const file of files) {
    const size = await page.evaluate(async (src) => {
      const image = new Image()
      image.src = src
      await image.decode()
      return { width: image.naturalWidth, height: image.naturalHeight }
    }, file)
    expect(size.width).toBeGreaterThan(0)
    expect(size.width).toBe(size.height)
    const match = file.match(/-(\d+)\.png$/)
    if (match) expect(size.width).toBe(Number(match[1]))
  }
})

test.describe('touch interaction', () => {
  test.use({ viewport: { width: 375, height: 812 }, isMobile: true, hasTouch: true })
  test('can check health without hover', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('button', { name: '检查服务连接' }).tap()
    await expect(page.getByRole('status')).toContainText('服务在线')
  })
})
