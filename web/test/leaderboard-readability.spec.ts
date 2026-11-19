import { expect, test } from '@playwright/test'

for (const width of [320, 375, 390, 768, 1200, 1440]) {
  test(`keeps compact usage readable without a duplicate value column at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await page.route('**/v1/leaderboards/usage/today', (route) => route.fulfill({
      status: 200, contentType: 'application/json', body: JSON.stringify({
        board: 'usage', period: 'today', start: '2026-11-16', end: '2026-11-17', generated_at: '2026-11-16T12:00:00Z',
        entries: [{ rank: 1, client_id: '1', display_name: 'ABCDEFGHIJKLMNOPQRSTUVWXYZ012345', avatar_id: '1', metric: '9223372036854775807' }],
      }),
    }))
    await page.goto('/')
    await page.getByRole('button', { name: 'Token Usage', exact: true }).click()
    await expect(page.locator('.metric-value')).toHaveText('9223372036.85 B')
    await expect(page.locator('#participant-1')).toHaveCSS('font-weight', '750')
    await expect(page.getByRole('columnheader')).toHaveCount(3)
    await expect(page.getByRole('table').getByRole('button')).toHaveCount(0)
    await expect(page.locator('.metric-number')).toHaveAttribute('title', '9,223,372,036,854,775,807')
    const geometry = await page.locator('.metric-number').evaluate((element) => {
      const rect = element.getBoundingClientRect()
      return { left: rect.left, right: rect.right, viewport: innerWidth, scrollWidth: document.documentElement.scrollWidth }
    })
    expect(geometry.left).toBeGreaterThanOrEqual(0)
    expect(geometry.right).toBeLessThanOrEqual(geometry.viewport)
    expect(geometry.scrollWidth).toBeLessThanOrEqual(geometry.viewport)
  })
}
