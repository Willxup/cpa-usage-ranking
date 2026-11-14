import { expect, test, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { randomBytes } from 'node:crypto'
import { seedUsers } from './admin-fixtures'

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

async function login(page: Page, credentials = owner) {
  await loginSlot()
  await page.goto('/admin/login')
  await page.getByLabel('用户名', { exact: true }).fill(credentials.username)
  await page.getByLabel('密码', { exact: true }).fill(credentials.password)
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

test('owner manages ordinary accounts and deletion revokes the live session', async ({ page, browser, baseURL }, info) => {
  await page.setViewportSize({ width: 375, height: 812 })
  await login(page)
  await page.getByRole('link', { name: '管理员管理', exact: true }).click()
  const credentials = { username: `staff-${Date.now()}`, password: randomBytes(24).toString('base64url') }
  await page.getByLabel('用户名', { exact: true }).fill(credentials.username)
  await page.getByLabel('初始密码', { exact: true }).fill(credentials.password)
  await page.getByRole('button', { name: '添加管理员', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '管理员已添加' })).toBeVisible()
  const row = page.getByRole('row').filter({ hasText: credentials.username })
  await expect(row).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: info.outputPath('accounts-375.png'), fullPage: true })
  const ordinaryContext = await browser.newContext({ baseURL })
  try {
    const ordinary = await ordinaryContext.newPage()
    await login(ordinary, credentials)
    await expect(ordinary.getByRole('link', { name: '管理员管理', exact: true })).toHaveCount(0)
    expect((await ordinaryContext.request.get('/v1/admin/accounts')).status()).toBe(403)
    await ordinary.goto('/admin/accounts')
    await expect(ordinary.getByRole('heading', { name: '仅主管理员可管理账号' })).toBeVisible()
    page.once('dialog', (dialog) => dialog.accept())
    await row.getByRole('button', { name: '删除', exact: true }).click()
    await expect(page.getByRole('status').filter({ hasText: '管理员已删除' })).toBeVisible()
    await ordinary.goto('/admin/leaderboards')
    await expect(ordinary).toHaveURL(/\/admin\/login$/)
  } finally { await ordinaryContext.close() }
})

test('lists real registered users with bounded paging, literal names and filters', async ({ page }, info) => {
  const prefix = `adm3-${randomBytes(3).toString('hex')}`
  const users = seedUsers(prefix, 51)
  await page.setViewportSize({ width: 320, height: 812 })
  await login(page)
  await page.getByRole('link', { name: '用户管理', exact: true }).click()
  await page.getByLabel('查询内容', { exact: true }).fill(prefix)
  await page.getByRole('button', { name: '查询', exact: true }).click()
  await expect(page.getByText('第 1 页 · 50 位用户')).toBeVisible()
  await expect(page.getByText(users[0].name, { exact: true })).toBeVisible()
  expect(await page.locator('.admin-users img').count()).toBeGreaterThan(0)
  expect(await page.locator('.admin-users img[src="x"]').count()).toBe(0)
  await page.getByRole('button', { name: '下一页' }).focus()
  await page.keyboard.press('Enter')
  await expect(page.getByText('第 2 页 · 1 位用户')).toBeVisible()
  await page.getByLabel('封禁状态', { exact: true }).selectOption('true')
  await page.getByRole('button', { name: '查询', exact: true }).click()
  await expect(page.getByText('第 1 页 · 1 位用户')).toBeVisible()
  await expect(page.getByText(users[0].name, { exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: info.outputPath('users-320.png'), fullPage: true })
  await page.getByLabel('查询内容', { exact: true }).fill(`${prefix}-missing`)
  await page.getByRole('button', { name: '查询', exact: true }).click()
  await expect(page.getByText('未找到匹配的用户。')).toBeVisible()
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
