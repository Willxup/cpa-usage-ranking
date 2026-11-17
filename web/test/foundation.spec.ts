import { expect, test } from '@playwright/test'

test('hides the login entry and admin pages when not configured', async ({ page, request }) => {
  test.skip(!!process.env.RANKING_TEST_ADMIN_CONFIG, 'Disabled-console check')
  expect(await (await request.get('/v1/admin/session')).json()).toEqual({ enabled: false, authenticated: false })
  await page.goto('/')
  await expect(page.getByRole('link', { name: /^(登录|控制台)$/ })).toHaveCount(0)
  expect((await request.get('/admin/login')).status()).toBe(404)
})

test('loads the real leaderboard without an introduction or health card', async ({ page, baseURL, request }, info) => {
  await expect.poll(async () => (await request.get('/readyz')).status()).toBe(200)
  const health = await request.get('/healthz')
  expect(health.status()).toBe(200)
  expect(await health.json()).toEqual({ status: 'ok' })
  const errors: string[] = []
  const externalRequests: string[] = []
  const healthRequests: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('console', (message) => { if (message.type() === 'error') errors.push(message.text()) })
  page.on('request', (request) => {
    if (new URL(request.url()).origin !== baseURL) externalRequests.push(request.url())
    if (new URL(request.url()).pathname === '/healthz') healthRequests.push(request.url())
  })
  await page.goto('/')
  await expect(page.getByRole('heading', { level: 1 })).toContainText('AI 用量排行榜')
  await expect(page.getByRole('heading', { name: '等待第一份真实数据' })).toBeVisible()
  await expect(page.getByText(/ABOUT THE RANKING|LIVE CONNECTION/)).toHaveCount(0)
  await expect(page.getByRole('button', { name: '检查服务连接' })).toHaveCount(0)
  const footer = page.locator('.brand-footer')
  await expect(footer).toHaveText('CPA Usage Ranking')
  await expect(footer.locator('img')).toHaveAttribute('src', '/assets/brand/project-icon.svg')
  const box = (await footer.locator('span').boundingBox())!
  const footerBox = (await footer.boundingBox())!
  expect(Math.abs(box.x + box.width / 2 - footerBox.x - footerBox.width / 2)).toBeLessThan(1)
  await page.screenshot({ path: info.outputPath('desktop.png'), fullPage: true })
  expect(healthRequests).toEqual([])
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
    const footer = page.locator('.brand-footer')
    await expect(footer).toHaveText('CPA Usage Ranking')
    await expect(footer.locator('img')).toHaveAttribute('src', '/assets/brand/project-icon.svg')
    const box = (await footer.locator('span').boundingBox())!
    const footerBox = (await footer.boundingBox())!
    expect(Math.abs(box.x + box.width / 2 - footerBox.x - footerBox.width / 2)).toBeLessThan(1)
    await expect(page.getByText(/ABOUT THE RANKING|LIVE CONNECTION/)).toHaveCount(0)
    if (width === 375) await page.screenshot({ path: info.outputPath('mobile.png'), fullPage: true })
  })
}

test('supports keyboard navigation of boards and periods', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('link', { name: '跳到主要内容' })).toHaveCount(0)
  for (const name of ['CPA Usage Ranking 首页', '排行榜']) {
    await page.keyboard.press('Tab')
    await expect(page.getByRole('link', { name, exact: true })).toBeFocused()
  }
  for (const name of ['综合总榜', '用量榜', '缓存复用榜', '成功率榜', '首字响应榜', '今日', '昨日', '本周', '本月']) {
    await page.keyboard.press('Tab')
    await expect(page.getByRole('button', { name, exact: true })).toBeFocused()
    if (name === '成功率榜' || name === '昨日') await page.keyboard.press('Space')
  }
  await expect(page.getByRole('button', { name: '成功率榜' })).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByRole('button', { name: '昨日' })).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByRole('heading', { name: '昨日 · 成功率榜' })).toBeVisible()
})

test('preserves API and missing-resource boundaries and ships source notices', async ({ request }) => {
  for (const path of ['/api/missing', '/assets/missing.js', '/missing-page']) {
    const response = await request.get(path, { headers: { Accept: 'text/html' } })
    expect(response.status()).toBe(404)
    expect(response.headers()['content-type']).toContain('application/json')
  }
  await expect.poll(async () => (await request.get('/readyz')).status()).toBe(200)
  const readiness = await request.get('/readyz')
  expect(await readiness.json()).toEqual({ status: 'ready' })
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
  test('can switch the board and period without hover', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('button', { name: '用量榜' }).tap()
    await page.getByRole('button', { name: '昨日' }).tap()
    await expect(page.getByRole('button', { name: '昨日' })).toHaveAttribute('aria-pressed', 'true')
    await expect(page.getByRole('heading', { name: '昨日 · 用量榜' })).toBeVisible()
  })
})
