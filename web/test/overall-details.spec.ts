import { expect, test } from '@playwright/test'

for (const width of [320, 375, 768, 1280, 1400, 1440]) {
  test(`shows six overall metrics without losing integer precision at ${width}px`, async ({ page }, info) => {
    const errors: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    await page.setViewportSize({ width, height: 1100 })
    await page.route('**/v1/leaderboards/overall/today', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
      board: 'overall', period: 'today', start: '2026-11-16', end: '2026-11-17', generated_at: '2026-11-16T12:00:00Z', entries: [
        { rank: 1, client_id: '1', display_name: 'ABCDEFGHIJKLMNOPQRSTUVWXYZ012345', avatar_id: '1', metric: '51.16', details: { total_tokens: '9223372036854775807', request_count: '9223372036854775807', success_rate: '75.00', cache_read_rate: '25.13', ttft_mean_ms: '0.00', latency_mean_ms: '200.50' } },
        { rank: 2, client_id: '2', display_name: '缺失样本', avatar_id: '2', metric: '0.00', details: { total_tokens: '0', request_count: '1', success_rate: '0.00', cache_read_rate: null, ttft_mean_ms: null, latency_mean_ms: null } },
      ],
    }) }))
    await page.goto('/')
    const details = page.locator('dl[aria-describedby="participant-1"]')
    await expect(details.locator('dt')).toHaveText(['Token', '请求数', '实际成功率', '缓存复用率', '平均首字延迟', '平均总延迟'])
    await expect(details.locator('dd')).toHaveText(['9,223,372,036,854,775,807', '9,223,372,036,854,775,807', '75.00%', '25.13%', '0.00 ms', '200.50 ms'])
    for (const index of [0, 1]) {
      const value = details.locator('dd').nth(index)
      const geometry = await value.evaluate((element) => {
        const range = document.createRange()
        range.selectNodeContents(element)
        const rects = [...range.getClientRects()]
        const box = element.getBoundingClientRect()
        return { lines: rects.length, right: Math.max(...rects.map((r) => r.right)), boxRight: box.right, viewport: innerWidth }
      })
      expect(geometry.lines).toBe(1)
      expect(geometry.right).toBeLessThanOrEqual(geometry.boxRight + 1)
      expect(geometry.right).toBeLessThanOrEqual(geometry.viewport)
    }
    await expect(page.locator('dl[aria-describedby="participant-2"] dd')).toHaveText(['0', '1', '0.00%', '—', '—', '—'])
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    expect(errors).toEqual([])
    await page.screenshot({ path: info.outputPath(`overall-${width}.png`) })
  })
}
