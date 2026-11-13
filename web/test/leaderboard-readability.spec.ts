import { expect, test } from '@playwright/test'

for (const width of [320, 375, 390, 768, 1200, 1440]) {
  test(`keeps compact usage and its exact int64 value readable at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await page.route('**/v1/leaderboards/usage/today', (route) => route.fulfill({
      status: 200, contentType: 'application/json', body: JSON.stringify({
        board: 'usage', period: 'today', start: '2026-11-16', end: '2026-11-17', generated_at: '2026-11-16T12:00:00Z',
        entries: [{ rank: 1, client_id: '1', display_name: 'ABCDEFGHIJKLMNOPQRSTUVWXYZ012345', avatar_id: '1', metric: '9223372036854775807' }],
      }),
    }))
    await page.goto('/')
    await page.getByRole('button', { name: '用量榜', exact: true }).click()
    await expect(page.locator('.metric-value')).toHaveText('9223372036.85B')
    await expect(page.locator('#participant-1')).toHaveCSS('font-weight', '750')
    const toggle = page.getByRole('button', { name: '查看ABCDEFGHIJKLMNOPQRSTUVWXYZ012345完整数值' })
    await toggle.focus()
    await page.keyboard.press('Enter')
    await expect(page.locator('.exact-toggle').first()).toHaveAttribute('aria-expanded', 'true')
    const value = page.locator('.exact-values dd')
    await expect(value).toHaveText('9,223,372,036,854,775,807')
    const geometry = await value.evaluate((cell) => {
      const range = document.createRange()
      range.selectNodeContents(cell)
      const lines = [...range.getClientRects()]
      return { lines: lines.length, right: Math.max(...lines.map((r) => r.right)), left: Math.min(...lines.map((r) => r.left)), viewport: innerWidth, scrollWidth: document.documentElement.scrollWidth }
    })
    expect(geometry.lines).toBe(1)
    expect(geometry.left).toBeGreaterThanOrEqual(0)
    expect(geometry.right).toBeLessThanOrEqual(geometry.viewport)
    expect(geometry.scrollWidth).toBeLessThanOrEqual(geometry.viewport)
    await expect(page.getByRole('columnheader', { name: 'Token 用量' })).toHaveCount(1)
    const close = page.getByRole('button', { name: '收起ABCDEFGHIJKLMNOPQRSTUVWXYZ012345完整数值' })
    await expect(close).toBeFocused()
    await page.keyboard.press('Space')
    await expect(page.locator('.exact-values')).toHaveCount(0)
  })
}
