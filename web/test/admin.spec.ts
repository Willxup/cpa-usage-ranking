import { expect, test, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'

const config = process.env.RANKING_TEST_ADMIN_CONFIG
const owner: { username: string; password: string } = config ? JSON.parse(readFileSync(config, 'utf8')) : { username: '', password: '' }
test.skip(!config, 'Uses a separate enabled, isolated console service')
test.setTimeout(100_000)

// Respect the real production login budget rather than weakening the limiter.
let attempts: number[] = []
async function loginSlot() {
  attempts = attempts.filter((time) => Date.now() - time < 61_000)
  if (attempts.length >= 5) {
    await new Promise((resolve) => setTimeout(resolve, 61_000 - (Date.now() - attempts[0])))
    attempts = attempts.filter((time) => Date.now() - time < 61_000)
  }
  attempts.push(Date.now())
}

async function login(page: Page) {
  await loginSlot()
  await page.goto('/admin/login')
  await page.getByLabel('用户名', { exact: true }).fill(owner.username)
  await page.getByLabel('密码', { exact: true }).fill(owner.password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('navigation', { name: '控制台导航' })).toBeVisible()
}

for (const width of [1440, 375, 320]) {
  test(`owner login, public entry, console and logout at ${width}px`, async ({ page, context }, info) => {
    await page.setViewportSize({ width, height: width === 1440 ? 1000 : 812 })
    await page.goto('/')
    const entry = page.getByRole('link', { name: '登录', exact: true })
    await expect(entry).toBeVisible()
    const brandBox = (await page.locator('.brand').boundingBox())!
    const entryBox = (await entry.boundingBox())!
    expect(brandBox.x + brandBox.width).toBeLessThanOrEqual(entryBox.x)
    expect(Math.abs(brandBox.y - entryBox.y)).toBeLessThan(30)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: info.outputPath(`public-login-${width}.png`), fullPage: true })
    await entry.click()
    await loginSlot()
    await page.getByLabel('用户名', { exact: true }).fill(owner.username)
    await page.getByLabel('密码', { exact: true }).fill(owner.password)
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await expect(page.getByRole('heading', { name: '榜单查看', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: '年度' })).toHaveCount(0)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: info.outputPath(`console-${width}.png`), fullPage: true })
    await page.getByRole('link', { name: '公开榜单' }).click()
    const consoleEntry = page.getByRole('link', { name: '控制台', exact: true })
    await expect(consoleEntry).toBeVisible()
    await page.screenshot({ path: info.outputPath(`public-console-${width}.png`), fullPage: true })
    await consoleEntry.click()
    await page.getByRole('button', { name: '退出', exact: true }).click()
    await expect(page.getByRole('link', { name: '登录', exact: true })).toBeVisible()
    expect((await context.cookies()).filter((cookie) => cookie.name === 'ranking_admin')).toHaveLength(0)
  })
}

test('login failure, keyboard submission and session expiry', async ({ page, context }) => {
  await page.goto('/admin/login')
  await page.getByLabel('用户名', { exact: true }).fill(owner.username)
  await page.getByLabel('密码', { exact: true }).fill('incorrect-password-for-test')
  await loginSlot()
  await page.getByLabel('密码', { exact: true }).press('Enter')
  await expect(page.getByRole('alert')).toContainText('用户名或密码错误')
  await page.getByLabel('密码', { exact: true }).fill(owner.password)
  await loginSlot()
  await page.getByLabel('密码', { exact: true }).press('Enter')
  await expect(page.getByRole('navigation', { name: '控制台导航' })).toBeVisible()
  await context.clearCookies()
  await page.reload()
  await expect(page).toHaveURL(/\/admin\/login$/)
  await page.goto('/')
  await expect(page.getByRole('link', { name: '登录', exact: true })).toBeVisible()
})

test('expired public session changes the top right entry without navigation', async ({ page, context }) => {
  await page.clock.install()
  await login(page)
  await page.goto('/')
  await expect(page.getByRole('link', { name: '控制台', exact: true })).toBeVisible()
  await context.clearCookies()
  await page.clock.fastForward(61_000)
  await expect(page.getByRole('link', { name: '登录', exact: true })).toBeVisible()
})
