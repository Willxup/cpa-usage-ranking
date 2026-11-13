import { expect, test } from '@playwright/test'

for (const width of [320, 375, 390, 768, 1440]) {
  test(`keeps the full int64 usage value readable at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await page.route('**/v1/leaderboards/usage/today', (route) => route.fulfill({
      status: 200, contentType: 'application/json', body: JSON.stringify({
        board: 'usage', period: 'today', start: '2026-11-16', end: '2026-11-17', generated_at: '2026-11-16T12:00:00Z',
        entries: [{ rank: 1, client_id: '1', display_name: 'ABCDEFGHIJKLMNOPQRSTUVWXYZ012345', avatar_id: '1', metric: '9223372036854775807' }],
      }),
    }))
    await page.goto('/')
    await page.getByRole('button', { name: '用量榜', exact: true }).click()
    const value = page.getByRole('cell').filter({ hasText: '9,223,372,036,854,775,807' })
    await expect(value).toBeVisible()
    const geometry = await value.evaluate((cell) => {
      const walker = document.createTreeWalker(cell, NodeFilter.SHOW_TEXT)
      let number: Text | null = null
      while (walker.nextNode()) {
        if (walker.currentNode.textContent?.includes('9,223,372,036,854,775,807')) { number = walker.currentNode as Text; break }
      }
      if (!number) throw new Error('formatted number missing')
      const range = document.createRange()
      const start = number.data.indexOf('9,223,372,036,854,775,807')
      range.setStart(number, start)
      range.setEnd(number, start + '9,223,372,036,854,775,807'.length)
      const lines = [...range.getClientRects()]
      return { lines: lines.length, right: Math.max(...lines.map((r) => r.right)), left: Math.min(...lines.map((r) => r.left)), viewport: innerWidth, scrollWidth: document.documentElement.scrollWidth }
    })
    expect(geometry.lines).toBe(1)
    expect(geometry.left).toBeGreaterThanOrEqual(0)
    expect(geometry.right).toBeLessThanOrEqual(geometry.viewport)
    expect(geometry.scrollWidth).toBeLessThanOrEqual(geometry.viewport)
    await expect(page.getByRole('columnheader', { name: 'Token 用量' })).toHaveCount(1)
    await expect(page.getByRole('cell', { name: 'ABCDEFGHIJKLMNOPQRSTUVWXYZ012345' })).toBeVisible()
  })
}
