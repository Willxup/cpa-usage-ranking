import { expect, test, type Locator } from '@playwright/test'

async function contrast(locator: Locator) {
  return locator.evaluate((element) => {
    const style = getComputedStyle(element)
    const luminance = (color: string) => {
      const values = color.match(/[\d.]+/g)?.map(Number)
      if (!values || values.length < 3 || (values.length > 3 && values[3] !== 1)) throw new Error('Expected an opaque computed RGB color')
      const linear = values.slice(0, 3).map((v) => { const c = v / 255; return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4 })
      return linear[0] * 0.2126 + linear[1] * 0.7152 + linear[2] * 0.0722
    }
    const a = luminance(style.color), b = luminance(style.backgroundColor)
    return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05)
  })
}

for (const width of [375, 1440]) {
  test(`selected navigation and board text meet AA contrast at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 })
    await page.goto('/')
    const nav = page.getByRole('navigation', { name: '页面导航' }).getByRole('link', { name: '排行榜', exact: true })
    await expect(nav).toBeVisible()
    expect(await contrast(nav)).toBeGreaterThanOrEqual(4.5)
    for (const name of ['综合总榜', '用量榜', '缓存复用榜', '成功率榜', '首字响应榜']) {
      const button = page.getByRole('button', { name, exact: true })
      await button.click()
      await expect(button).toHaveAttribute('aria-pressed', 'true')
      expect(await contrast(button)).toBeGreaterThanOrEqual(4.5)
    }
  })
}
