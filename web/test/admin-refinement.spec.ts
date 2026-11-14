import { expect, test, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'

const config = process.env.RANKING_TEST_ADMIN_CONFIG
const owner: { username: string; password: string } = config ? JSON.parse(readFileSync(config, 'utf8')) : { username: '', password: '' }
test.skip(!config, 'Requires the separate enabled test service')

async function login(page: Page) {
  await page.goto('/admin/login')
  await page.getByLabel('用户名', { exact: true }).fill(owner.username)
  await page.getByLabel('密码', { exact: true }).fill(owner.password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('navigation', { name: '控制台导航' })).toBeVisible()
}

test('keeps a shared-session document across tabs, public return and history; refresh and expiry still authenticate', async ({ page, context }, info) => {
  let documents = 0
  let sessionReads = 0
  page.on('request', (request) => {
    if (request.isNavigationRequest() && request.resourceType() === 'document') documents++
    if (request.method() === 'GET' && new URL(request.url()).pathname === '/v1/admin/session') sessionReads++
  })
  await login(page)
  const reads = sessionReads
  await page.evaluate(() => { document.documentElement.dataset.refinement = 'same-document' })
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: width === 1440 ? 1000 : 812 })
    await page.getByRole('link', { name: '用户管理', exact: true }).click()
    await expect(page.getByRole('heading', { name: '用户管理', exact: true })).toBeVisible()
    await page.getByRole('link', { name: '管理员管理', exact: true }).click()
    await expect(page.getByRole('heading', { name: '管理员管理', exact: true })).toBeVisible()
    await page.goBack()
    await expect(page.getByRole('heading', { name: '用户管理', exact: true })).toBeVisible()
    await page.goForward()
    await expect(page.getByRole('heading', { name: '管理员管理', exact: true })).toBeVisible()
    await expect(page.getByText('正在加载…', { exact: true })).toHaveCount(0)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: info.outputPath(`navigation-${width}.png`), fullPage: true })
    await page.getByRole('link', { name: '公开榜单', exact: true }).click()
    await expect(page.getByRole('link', { name: '控制台', exact: true })).toBeVisible()
    await page.getByRole('link', { name: '控制台', exact: true }).click()
  }
  expect(documents).toBe(1)
  expect(sessionReads).toBe(reads)
  expect(await page.evaluate(() => document.documentElement.dataset.refinement)).toBe('same-document')
  await page.getByRole('link', { name: '用户管理', exact: true }).click()
  await page.reload()
  await expect(page.getByRole('heading', { name: '用户管理', exact: true })).toBeVisible()
  expect(sessionReads).toBeGreaterThan(reads)
  await context.clearCookies()
  await page.getByRole('link', { name: '管理员管理', exact: true }).click()
  await expect(page.getByRole('heading', { name: '管理员登录', exact: true })).toBeVisible()
  await expect(page).toHaveURL(/\/admin\/login$/)
  await page.getByRole('link', { name: '公开榜单', exact: true }).click()
  await expect(page.getByRole('link', { name: '登录', exact: true })).toBeVisible()
})
