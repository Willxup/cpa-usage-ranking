import { readFileSync } from 'node:fs'
import { expect, test } from '@playwright/test'

const config = process.env.RANKING_TEST_ADMIN_CONFIG
const credentials: { username: string; password: string } = config ? JSON.parse(readFileSync(config, 'utf8')) : { username: '', password: '' }
test.skip(!config, 'Requires an isolated enabled console')

test('uses the public text-navigation style for return and logout at both sizes', async ({ page }, info) => {
  await page.goto('/')
  const fonts = new Map<number, string[]>()
  const style = (node: Element) => {
    const css = getComputedStyle(node)
    return [css.fontFamily, css.fontSize, css.fontWeight, css.borderWidth]
  }
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: 900 })
    await expect(page.getByRole('link', { name: '登录', exact: true })).toBeVisible()
    fonts.set(width, await page.getByRole('link', { name: '排行榜', exact: true }).evaluate(style))
  }
  await page.getByRole('link', { name: '登录', exact: true }).click()
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: 900 })
    expect(await page.getByRole('link', { name: '公开榜单' }).evaluate(style)).toEqual(fonts.get(width))
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  }
  await page.getByLabel('用户名', { exact: true }).fill(credentials.username)
  await page.getByLabel('密码', { exact: true }).fill(credentials.password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: 900 })
    expect(await page.getByRole('link', { name: '公开榜单' }).evaluate(style)).toEqual(fonts.get(width))
    expect(await page.getByRole('button', { name: '退出' }).evaluate(style)).toEqual(fonts.get(width))
    await page.screenshot({ path: info.outputPath(`navigation-${width}.png`), fullPage: true })
  }
  await page.getByRole('button', { name: '退出' }).click()
  await expect(page.getByRole('link', { name: '登录', exact: true })).toBeVisible()
})
