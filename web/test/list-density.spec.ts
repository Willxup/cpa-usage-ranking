import { expect, test } from '@playwright/test'

for (const width of [320, 1440]) {
  test(`keeps all 100 ranked entries usable at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const entries = Array.from({ length: 100 }, (_, index) => ({
      rank: index + 1, client_id: String(index + 1), avatar_id: '1', display_name: `演示参与者-${index + 1}`, metric: '51.16',
      details: { total_tokens: '123456789', request_count: '12345', success_rate: '99.98', cache_read_rate: '50.00', ttft_mean_ms: '1234.00', latency_mean_ms: '61000.00' },
    }))
    await page.route('**/v1/leaderboards/overall/today', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ board: 'overall', period: 'today', start: '2026-11-16', end: '2026-11-17', generated_at: '2026-11-16T12:00:00Z', entries }) }))
    await page.goto('/')
    await expect(page.locator('.leaderboard-group')).toHaveCount(100)
    await expect(page.locator('.rank-leading')).toHaveCount(3)
    await expect(page.locator('.detail-cell')).toHaveCount(600)
    await expect(page.locator('.leaderboard-group').first().locator('.detail-cell')).toHaveText(['123.46M', '12.35K', '99.98%', '50.00%', '1.23 s', '1 m 01 s'])
    const last = page.getByRole('button', { name: '查看演示参与者-100完整数值' })
    await last.scrollIntoViewIfNeeded()
    await last.click()
    const row = page.locator('.leaderboard-group').last()
    await expect(row.locator('.overall-details dd')).toHaveText(['123,456,789', '12,345', '99.98%', '50.00%', '1234.00 ms', '61000.00 ms'])
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await expect(page.getByText('已显示 100 位 · 最多 100 位')).toBeVisible()
  })
}
