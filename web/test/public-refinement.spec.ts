import { readFileSync } from 'node:fs'
import { expect, test } from '@playwright/test'

const config = process.env.RANKING_TEST_ADMIN_CONFIG
const credentials: { username: string; password: string } = config ? JSON.parse(readFileSync(config, 'utf8')) : { username: '', password: '' }

test('keeps public navigation consistent for anonymous and authenticated sessions', async ({ page }, info) => {
  test.skip(!config, 'Requires an isolated enabled console')
  let documents = 0
  page.on('request', (request) => { if (request.isNavigationRequest() && request.resourceType() === 'document') documents++ })
  await page.goto('/')
  const navigation = page.getByRole('navigation', { name: '页面导航' })
  const check = async (entry: '登录' | '控制台') => {
    await expect(navigation.getByRole('link')).toHaveText(['排行榜', entry])
    const styles = await navigation.getByRole('link').evaluateAll((links) => links.map((link) => {
      const css = getComputedStyle(link)
      return [css.fontFamily, css.fontSize, css.fontWeight, css.borderWidth, css.padding]
    }))
    expect(styles[0]).toEqual(styles[1])
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  }
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: width === 1440 ? 1000 : 812 })
    await check('登录')
    await page.screenshot({ path: info.outputPath(`anonymous-${width}.png`), fullPage: true })
  }
  await navigation.getByRole('link', { name: '登录' }).click()
  await page.getByLabel('用户名', { exact: true }).fill(credentials.username)
  await page.getByLabel('密码', { exact: true }).fill(credentials.password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('navigation', { name: '控制台导航' })).toBeVisible()
  await page.getByRole('link', { name: '公开榜单' }).click()
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: width === 1440 ? 1000 : 812 })
    await check('控制台')
    await page.screenshot({ path: info.outputPath(`authenticated-${width}.png`), fullPage: true })
  }
  await navigation.getByRole('link', { name: '控制台' }).click()
  await expect(page.getByRole('navigation', { name: '控制台导航' })).toBeVisible()
  expect(documents).toBe(1)
})
