import { expect, test } from '@playwright/test'

for (const width of [320, 375, 768, 1200, 1280, 1440]) {
  test(`shows spaced metric units without duplicate detail rows at ${width}px`, async ({ page }, info) => {
    const errors: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    await page.setViewportSize({ width, height: 1100 })
    await page.route('**/v1/leaderboards/overall/today', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
      board: 'overall', period: 'today', start: '2026-11-16', end: '2026-11-17', generated_at: '2026-11-16T12:00:00Z', entries: [
        { rank: 1, client_id: '1', display_name: 'ABCDEFGHIJKLMNOPQRSTUVWXYZ012345', avatar_id: '1', metric: '51.16', details: { total_tokens: '9223372036854775807', request_count: '9223372036854775807', success_rate: '75.00', cache_read_rate: '25.13', ttft_mean_ms: '0.00', latency_mean_ms: '1350.00' } },
        { rank: 2, client_id: '2', display_name: '缺失样本', avatar_id: '2', metric: '0.00', details: { total_tokens: '0', request_count: '1', success_rate: '0.00', cache_read_rate: null, ttft_mean_ms: null, latency_mean_ms: null } },
      ],
    }) }))
    await page.goto('/')
    const first = page.locator('.leaderboard-group').first()
    await expect(first.locator('.detail-cell')).toHaveText(['9223372036.85 B', '9223372036.85 B', '75.00 %', '25.13 %', '0 ms', '1.35 s'])
    await expect(page.locator('#participant-1')).toHaveCSS('font-weight', '750')
    await expect(first.locator('.detail-cell').first()).toHaveCSS('font-weight', '500')
    await expect(first.locator('.metric-unit').first()).toHaveCSS('font-weight', '400')
    await expect(page.locator('.leaderboard-group').nth(1).locator('.detail-cell')).toHaveText(['0', '1', '0.00 %', '—', '—', '—'])
    await expect(page.getByRole('columnheader')).toHaveCount(9)
    await expect(page.getByRole('table').getByRole('button')).toHaveCount(0)
    await expect(page.locator('.exact-values-row, .overall-details')).toHaveCount(0)
    for (const amount of await first.locator('.metric-number').all()) {
      expect(await amount.evaluate((element) => {
        const value = element.getBoundingClientRect()
        const cell = element.closest('td')!.getBoundingClientRect()
        return value.left >= cell.left && value.right <= cell.right && value.right <= innerWidth
      })).toBe(true)
    }
    for (const unit of await first.locator('.metric-unit').all()) {
      const gap = await unit.evaluate((element) => {
        const previous = element.previousElementSibling!
        const text = previous.firstChild!
        const range = document.createRange()
        range.setStart(text, 0)
        range.setEnd(text, text.textContent!.trimEnd().length)
        return element.getBoundingClientRect().left - range.getBoundingClientRect().right
      })
      expect(gap).toBeGreaterThan(1)
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    expect(errors).toEqual([])
    await page.screenshot({ path: info.outputPath(`overall-${width}.png`) })
  })
}
