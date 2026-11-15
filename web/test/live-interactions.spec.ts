import { randomBytes } from 'node:crypto'
import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

let participant = ''
const usage = '123.46 M'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

// 仅分类本用例明确注入的 503；其他错误均须暴露。
function observePage(page: Page, baseURL: string) {
  const pageErrors: string[] = []
  const consoleErrors: string[] = []
  const failedRequests: string[] = []
  const errorResponses: string[] = []
  const externalRequests: string[] = []
  page.on('pageerror', (error) => pageErrors.push(error.message))
  page.on('console', (message) => { if (message.type() === 'error') consoleErrors.push(message.text()) })
  page.on('request', (request) => {
    if (new URL(request.url()).origin !== new URL(baseURL).origin) externalRequests.push(request.url())
  })
  page.on('requestfailed', (request) => failedRequests.push(`${new URL(request.url()).pathname} ${request.failure()?.errorText}`))
  page.on('response', (response) => {
    if (response.status() >= 400) errorResponses.push(`${new URL(response.url()).pathname} ${response.status()}`)
  })
  return { pageErrors, consoleErrors, failedRequests, errorResponses, externalRequests }
}

async function expectObservedClean(page: Page, observed: ReturnType<typeof observePage>, options?: { initial503?: boolean }) {
  await page.waitForLoadState('networkidle')
  expect(observed.pageErrors).toEqual([])
  expect(observed.externalRequests).toEqual([])
  expect(observed.errorResponses).toEqual(options?.initial503 ? ['/v1/leaderboards/overall/today 503'] : [])
  const unexpectedConsole = observed.consoleErrors.filter((message) =>
    !(options?.initial503 && /^Failed to load resource: the server responded with a status of 503\b/.test(message)))
  expect(unexpectedConsole).toEqual([])
  expect(observed.consoleErrors.length).toBeLessThanOrEqual(options?.initial503 ? 1 : 0)
  expect(observed.failedRequests).toEqual([])
}

test.beforeAll(async ({ playwright }, workerInfo) => {
  const api = await playwright.request.newContext({ baseURL: workerInfo.project.use.baseURL })
  try {
    // 一个独立真实身份供本文件复用，避免超过默认每来源的注册创建限制。
    const secret = randomBytes(32).toString('base64url')
    participant = `交互验收-${randomBytes(4).toString('hex')}`
    const headers = { Authorization: `Bearer ${secret}` }
    expect((await api.post('/v1/clients/register', { headers, data: { display_name: participant, avatar_id: 2 } })).status()).toBe(201)
    const day = new Date().toISOString().slice(0, 10)
    expect((await api.put(`/v1/reports/${day}`, { headers, data: {
      schema_version: 1, revision: '1', items: [{ model: 'claude-fable-5', total_tokens: '123456789', input_tokens: '10', output_tokens: '0', cache_read_tokens: '5', cache_write_tokens: '0', reasoning_tokens: '0', request_count: '2', success_count: '1', ttft_avg_ms: 12, ttft_samples: '1', latency_avg_ms: null, latency_samples: '0' }],
    } })).status()).toBe(200)
    await expect.poll(async () => {
      const response = await api.get('/v1/leaderboards/usage/today')
      if (response.status() !== 200) return false
      const data = await response.json() as { entries: { display_name: string; metric: string }[] }
      return data.entries.some((entry) => entry.display_name === participant && entry.metric === '123456789')
    }).toBe(true)
  } finally {
    await api.dispose()
  }
})

test('keeps initial loading quiet and reports a simulated first failure, then retries the real API', async ({ page, baseURL }) => {
  const observed = observePage(page, baseURL!)
  const started = deferred<void>()
  const release = deferred<void>()
  // 只把首次榜单响应替换成 503；重试不拦截，返回实际服务。
  await page.route('**/v1/leaderboards/overall/today', async (route) => {
    started.resolve()
    await release.promise
    await route.fulfill({ status: 503, contentType: 'application/json', body: '{}' })
  }, { times: 1 })
  try {
    await page.goto('/', { waitUntil: 'domcontentloaded' })
    await started.promise
    await expect(page.locator('.board-content')).toHaveAttribute('aria-busy', 'true')
    await expect(page.getByText(/正在加载|^LOADING$/)).toHaveCount(0)
    await expect(page.getByRole('heading', { name: '等待第一份真实数据' })).toHaveCount(0)
    release.resolve()
    await expect(page.getByText(/榜单缓存尚未就绪/)).toBeVisible()
    await expect(page.getByRole('heading', { name: '暂时无法加载榜单' })).toBeVisible()
    await expect(page.getByRole('heading', { name: '等待第一份真实数据' })).toHaveCount(0)
    const response = page.waitForResponse((value) => new URL(value.url()).pathname === '/v1/leaderboards/overall/today')
    await page.getByRole('button', { name: '重试榜单' }).click()
    expect((await response).status()).toBe(200)
    await expect(page.getByRole('heading', { name: '今日 · 综合总榜' })).toBeVisible()
    await expect(page.getByRole('row', { name: new RegExp(participant) })).toBeVisible()
    await expect(page.locator('.board-content')).toHaveAttribute('aria-busy', 'false')
    await expectObservedClean(page, observed, { initial503: true })
  } finally {
    release.resolve()
  }
})

test('uses keyboard navigation to change the real board and period', async ({ page, baseURL }, info) => {
  const observed = observePage(page, baseURL!)
  await page.goto('/')
  await expect(page.getByRole('row', { name: new RegExp(participant) })).toBeVisible()
  for (const name of ['CPA Usage Ranking 首页', '排行榜']) {
    await page.keyboard.press('Tab')
    await expect(page.getByRole('link', { name, exact: true })).toBeFocused()
  }
  for (const name of ['综合总榜', '用量榜']) {
    await page.keyboard.press('Tab')
    await expect(page.getByRole('button', { name, exact: true })).toBeFocused()
  }
  await page.keyboard.press('Enter')
  await expect(page.getByRole('button', { name: '用量榜' })).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByRole('heading', { name: '今日 · 用量榜' })).toBeVisible()
  await expect(page.getByRole('row', { name: new RegExp(participant) })).toContainText(usage)
  for (const name of ['缓存复用榜', '成功率榜', '首字响应榜', '今日', '昨日', '本周']) {
    await page.keyboard.press('Tab')
    await expect(page.getByRole('button', { name, exact: true })).toBeFocused()
  }
  await page.keyboard.press('Space')
  await expect(page.getByRole('button', { name: '本周' })).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByRole('heading', { name: '本周 · 用量榜' })).toBeVisible()
  await expect(page.getByRole('row', { name: new RegExp(participant) })).toContainText(usage)
  await page.screenshot({ path: info.outputPath('keyboard-desktop.png'), fullPage: true })
  await expectObservedClean(page, observed)
})

test('keeps the existing result while switching back and ignores a delayed superseded response', async ({ page, baseURL }, info) => {
  const observed = observePage(page, baseURL!)
  // 模拟底层网络不响应取消，让真实响应在切换后仍能到达 fetch。
  await page.addInitScript(() => {
    const nativeFetch = window.fetch.bind(window)
    window.fetch = (input, init) => {
      if (input === '/v1/leaderboards/usage/today') {
        return nativeFetch(input, { ...init, signal: undefined }).then((response) => {
          document.documentElement.dataset.c53UsageDelivered = 'true'
          return response
        })
      }
      return nativeFetch(input, init)
    }
  })
  await page.goto('/')
  await expect(page.getByRole('row', { name: new RegExp(participant) })).toBeVisible()
  const usageStarted = deferred<number>()
  const releaseUsage = deferred<void>()
  const usageFinished = deferred<string>()
  const revisitStarted = deferred<void>()
  const releaseRevisit = deferred<void>()
  // 先从真实服务取得用量响应，再延后转交浏览器；回访总榜也暂缓以检查内容保持。
  // 保持拦截器注册至测试结束，避免移除最后一个拦截器时自动放行仍在等待的请求。
  await page.route('**/v1/leaderboards/usage/today', async (route) => {
    try {
      const response = await route.fetch()
      usageStarted.resolve(response.status())
      await releaseUsage.promise
      await route.fulfill({ response })
      usageFinished.resolve('fulfilled')
    } catch (error) {
      usageStarted.resolve(0)
      usageFinished.resolve(error instanceof Error ? error.message : String(error))
    }
  })
  await page.route('**/v1/leaderboards/overall/today', async (route) => {
    revisitStarted.resolve()
    await releaseRevisit.promise
    await route.continue()
  })
  try {
    await page.getByRole('button', { name: '用量榜' }).click()
    expect(await usageStarted.promise).toBe(200)
    await expect(page.getByText(/正在加载|上次结果/)).toHaveCount(0)
    await expect(page.getByRole('heading', { name: '今日 · 综合总榜', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: '综合总榜' })).toHaveAttribute('aria-pressed', 'true')
    await expect(page.getByRole('row', { name: new RegExp(participant) })).toBeVisible()
    await page.screenshot({ path: info.outputPath('switch-pending-desktop.png'), fullPage: true })
    await page.getByRole('button', { name: '综合总榜' }).click()
    await revisitStarted.promise
    await expect(page.getByText(/正在加载|上次结果/)).toHaveCount(0)
    await expect(page.getByRole('heading', { name: '今日 · 综合总榜', exact: true })).toBeVisible()
    await expect(page.getByRole('row', { name: new RegExp(participant) })).toBeVisible()
    const response = page.waitForResponse((value) => new URL(value.url()).pathname === '/v1/leaderboards/overall/today')
    releaseRevisit.resolve()
    expect((await response).status()).toBe(200)
    await expect(page.getByRole('heading', { name: '今日 · 综合总榜', exact: true })).toBeVisible()
    releaseUsage.resolve()
    expect(await usageFinished.promise).toBe('fulfilled')
    await expect(page.locator('html')).toHaveAttribute('data-c53-usage-delivered', 'true')
    await expectObservedClean(page, observed)
    await expect(page.getByRole('heading', { name: '今日 · 综合总榜', exact: true })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: '总分' })).toBeVisible()
    await expect(page.getByRole('row', { name: new RegExp(participant) })).toBeVisible()
  } finally {
    releaseRevisit.resolve()
    releaseUsage.resolve()
  }
})

test('keeps a real mobile result until a rapid board and period choice is ready', async ({ page, baseURL }, info) => {
  const observed = observePage(page, baseURL!)
  await page.setViewportSize({ width: 375, height: 812 })
  await page.addInitScript(() => {
    const nativeFetch = window.fetch.bind(window)
    window.fetch = (input, init) => input === '/v1/leaderboards/usage/today'
      ? nativeFetch(input, { ...init, signal: undefined }).then((response) => { document.documentElement.dataset.usageDelivered = 'true'; return response })
      : nativeFetch(input, init)
  })
  await page.goto('/')
  await expect(page.getByRole('row', { name: new RegExp(participant) })).toBeVisible()
  const started = deferred<void>()
  const release = deferred<void>()
  const delivered = deferred<void>()
  await page.route('**/v1/leaderboards/usage/today', async (route) => {
    const response = await route.fetch()
    started.resolve()
    await release.promise
    await route.fulfill({ response })
    delivered.resolve()
  })
  try {
    await page.getByRole('button', { name: '用量榜' }).click()
    await started.promise
    await expect(page.getByText(/正在加载|上次结果/)).toHaveCount(0)
    await expect(page.getByRole('heading', { name: '今日 · 综合总榜' })).toBeVisible()
    await expect(page.getByRole('button', { name: '综合总榜' })).toHaveAttribute('aria-pressed', 'true')
    await expect(page.getByRole('row', { name: new RegExp(participant) })).toBeVisible()
    await page.screenshot({ path: info.outputPath('switch-pending-375.png'), fullPage: true })
    await page.getByRole('button', { name: '本周' }).click()
    await expect(page.getByRole('heading', { name: '本周 · 用量榜' })).toBeVisible()
    await expect(page.getByRole('button', { name: '用量榜' })).toHaveAttribute('aria-pressed', 'true')
    await expect(page.getByRole('button', { name: '本周' })).toHaveAttribute('aria-pressed', 'true')
    await expect(page.getByRole('row', { name: new RegExp(participant) })).toContainText(usage)
    await expect(page.locator('.metric-value').first()).toHaveAttribute('data-label', 'Token 用量')
    await page.screenshot({ path: info.outputPath('switch-latest-375.png'), fullPage: true })
    release.resolve()
    await delivered.promise
    await expect(page.locator('html')).toHaveAttribute('data-usage-delivered', 'true')
    await expect(page.getByRole('heading', { name: '本周 · 用量榜' })).toBeVisible()
    await expect(page.getByRole('row', { name: new RegExp(participant) })).toContainText(usage)
    await expectObservedClean(page, observed)
  } finally { release.resolve() }
})

test.describe('touch leaderboard', () => {
  test.use({ viewport: { width: 375, height: 812 }, isMobile: true, hasTouch: true })
  test('changes the real board and period without hover', async ({ page, baseURL }, info) => {
    const observed = observePage(page, baseURL!)
    await page.goto('/')
    await expect(page.getByRole('row', { name: new RegExp(participant) })).toBeVisible()
    await page.getByRole('button', { name: '用量榜' }).tap()
    await expect(page.getByRole('heading', { name: '今日 · 用量榜' })).toBeVisible()
    await page.getByRole('button', { name: '本周' }).tap()
    await expect(page.getByRole('button', { name: '本周' })).toHaveAttribute('aria-pressed', 'true')
    await expect(page.getByRole('heading', { name: '本周 · 用量榜' })).toBeVisible()
    await expect(page.getByRole('row', { name: new RegExp(participant) })).toContainText(usage)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: info.outputPath('touch-mobile.png'), fullPage: true })
    await expectObservedClean(page, observed)
  })
})
