import { randomBytes } from 'node:crypto'
import { expect, test } from '@playwright/test'

test('shows real submitted data across approved boards and periods', async ({ page, request }, info) => {
  const secret = randomBytes(32).toString('base64url')
  const headers = { Authorization: `Bearer ${secret}` }
  const registration = await request.post('/v1/clients/register', { headers, data: { display_name: '真实测试参与者', avatar_id: 2 } })
  expect(registration.status()).toBe(201)
  const registered = await registration.json() as { client_id: string }
  const day = new Date().toISOString().slice(0, 10)
  const report = await request.put(`/v1/reports/${day}`, { headers, data: {
    schema_version: 1, revision: '1', items: [{ model: 'claude-fable-5', total_tokens: '9007199254740993', input_tokens: '10', output_tokens: '0', cache_read_tokens: '5', cache_write_tokens: '0', reasoning_tokens: '0', request_count: '2', success_count: '1', ttft_avg_ms: 12, ttft_samples: '1', latency_avg_ms: null, latency_samples: '0' }],
  } })
  expect(report.status()).toBe(200)
  await expect.poll(async () => {
    const response = await request.get('/v1/leaderboards/usage/today')
    if (response.status() !== 200) return false
    const value = await response.json() as { entries: { display_name: string; metric: string }[] }
    return value.entries.some((entry) => entry.display_name === '真实测试参与者' && entry.metric === '9007199254740993')
  }).toBe(true)
  await page.route('**/avatars/images/002.webp', (route) => route.fulfill({ status: 404, body: '' }), { times: 1 })
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '今日 · 综合总榜' })).toBeVisible()
  await expect(page.getByText('真实测试参与者')).toBeVisible()
  await expect(page.getByRole('row', { name: /真实测试参与者/ }).locator('.avatar img')).toHaveAttribute('src', '/avatars/images/001.webp')
  await page.getByRole('button', { name: '查看真实测试参与者完整数值' }).click()
  const details = page.locator(`dl[aria-describedby="participant-${registered.client_id}"]`)
  await expect(details.locator('dt')).toHaveText(['Token', '请求数', '实际成功率', '缓存复用率', '平均首字延迟', '平均总延迟'])
  await expect(details.locator('dd')).toHaveText(['9,007,199,254,740,993', '2', '50.00%', '50.00%', '12.00 ms', '—'])
  await expect(page.getByRole('button', { name: '今日' })).toHaveAttribute('aria-pressed', 'true')
  await page.getByRole('button', { name: '用量榜' }).click()
  await expect(page.getByRole('heading', { name: '今日 · 用量榜' })).toBeVisible()
  await expect(page.getByText('9007199.25')).toBeVisible()
  await page.screenshot({ path: info.outputPath('real-desktop.png'), fullPage: true })
  await page.getByRole('button', { name: '缓存复用榜' }).click()
  await expect(page.getByRole('columnheader', { name: '缓存复用率' })).toBeVisible()
  await expect(page.getByText('50.00')).toBeVisible()
  await page.getByRole('button', { name: '成功率榜' }).click()
  await expect(page.getByRole('columnheader', { name: '成功率得分' })).toBeVisible()
  await page.getByRole('button', { name: '首字响应榜' }).click()
  await expect(page.getByRole('columnheader', { name: '平均首字' })).toBeVisible()
  await expect(page.locator('.metric-value').filter({ hasText: '12 ms' })).toBeVisible()
  await page.getByRole('button', { name: '昨日' }).click()
  await expect(page.getByRole('heading', { name: '昨日 · 首字响应榜' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '等待第一份真实数据' })).toBeVisible()
  for (const period of ['本周', '本月', '今日']) {
    await page.getByRole('button', { name: period }).click()
    await expect(page.getByRole('heading', { name: `${period} · 首字响应榜` })).toBeVisible()
    await expect(page.getByText('真实测试参与者')).toBeVisible()
  }
  await expect(page.getByRole('button', { name: /年度|累计/ })).toHaveCount(0)
  await expect(page.getByRole('textbox')).toHaveCount(0)
  await expect(page.getByRole('combobox')).toHaveCount(0)
  await page.setViewportSize({ width: 375, height: 812 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.getByRole('button', { name: '用量榜' }).click()
  await expect(page.getByText('9007199.25')).toBeVisible()
  await page.screenshot({ path: info.outputPath('real-mobile.png'), fullPage: true })
})

test('retains correctly labeled old data on races and retries', async ({ page, request }) => {
  await expect.poll(async () => (await request.get('/readyz')).status()).toBe(200)
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '今日 · 综合总榜' })).toBeVisible()
  let unblock!: () => void
  let intercepted!: () => void
  const blocked = new Promise<void>((resolve) => { unblock = resolve })
  const started = new Promise<void>((resolve) => { intercepted = resolve })
  await page.route('**/v1/leaderboards/usage/today', async (route) => {
    intercepted()
    await blocked
    await route.continue().catch(() => {})
  }, { times: 1 })
  await page.getByRole('button', { name: '用量榜' }).click()
  await started
  await expect(page.getByText(/正在加载今日 · 用量榜/)).toBeVisible()
  await expect(page.getByRole('heading', { name: /今日 · 综合总榜/ })).toContainText('上次结果')
  await expect(page.getByRole('heading', { name: /今日 · 用量榜/ })).toHaveCount(0)
  await page.getByRole('button', { name: '成功率榜' }).click()
  await expect(page.getByRole('heading', { name: '今日 · 成功率榜' })).toBeVisible()
  unblock()
  await expect(page.getByRole('columnheader', { name: '成功率得分' })).toBeVisible()
  await page.route('**/v1/leaderboards/cache/today', (route) => route.fulfill({ status: 503, contentType: 'application/json', body: '{}' }), { times: 1 })
  await page.getByRole('button', { name: '缓存复用榜' }).click()
  await expect(page.getByText(/榜单缓存尚未就绪/)).toBeVisible()
  await expect(page.getByRole('heading', { name: /今日 · 成功率榜/ })).toContainText('上次结果')
  await page.getByRole('button', { name: '重试榜单' }).click()
  await expect(page.getByRole('heading', { name: '今日 · 缓存复用榜' })).toBeVisible()
})
