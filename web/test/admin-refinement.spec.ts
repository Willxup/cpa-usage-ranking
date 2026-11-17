import { expect, test, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { randomBytes } from 'node:crypto'
import { seedUsers } from './admin-fixtures'

const config = process.env.RANKING_TEST_ADMIN_CONFIG
const owner: { username: string; password: string } = config ? JSON.parse(readFileSync(config, 'utf8')) : { username: '', password: '' }
test.skip(!config, 'Requires the separate enabled test service')
test.setTimeout(100_000)
let attempts: number[] = []

async function login(page: Page, credentials = owner) {
  await page.goto('/admin/login')
  await page.getByLabel('用户名', { exact: true }).fill(credentials.username)
  await page.getByLabel('密码', { exact: true }).fill(credentials.password)
  attempts = attempts.filter((time) => Date.now() - time < 61_000)
  if (attempts.length >= 5) {
    await new Promise((resolve) => setTimeout(resolve, 61_000 - (Date.now() - attempts[0])))
    attempts = attempts.filter((time) => Date.now() - time < 61_000)
  }
  attempts.push(Date.now())
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('navigation', { name: '控制台导航' })).toBeVisible()
}

test('places add administrator on the left and a live simulated list on the right, stacking on phones', async ({ page }, info) => {
  await login(page)
  await page.getByRole('link', { name: '管理员管理', exact: true }).click()
  const suffix = randomBytes(2).toString('hex')
  const names = ['demo-iris', 'demo-mira', 'demo-river'].map((name) => `${name}-${suffix}`)
  const add = async (name: string) => {
    await page.getByLabel('用户名', { exact: true }).fill(name)
    await page.getByLabel('初始密码', { exact: true }).fill(randomBytes(24).toString('base64url'))
    await page.getByRole('button', { name: '添加管理员', exact: true }).click()
  }
  await add(names[0])
  await expect(page.getByRole('row').filter({ hasText: names[0] })).toBeVisible()
  await add(names[1])
  await expect(page.getByRole('row').filter({ hasText: names[1] })).toBeVisible()
  await add(names[0])
  await expect(page.getByRole('alert')).toContainText('用户名已存在')
  await add(names[2])
  await expect(page.getByRole('row').filter({ hasText: names[2] })).toBeVisible()
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: width === 1440 ? 1000 : 812 })
    const form = (await page.getByRole('region', { name: '添加管理员', exact: true }).boundingBox())!
    const list = (await page.getByRole('region', { name: '管理员列表', exact: true }).boundingBox())!
    if (width === 1440) { expect(form.x + form.width).toBeLessThan(list.x); expect(Math.abs(form.y - list.y)).toBeLessThan(2); expect(Math.abs(list.width - 2 * form.width)).toBeLessThan(2) }
    else { expect(form.y + form.height).toBeLessThan(list.y); expect(Math.abs(form.x - list.x)).toBeLessThan(2) }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    const remove = page.getByRole('row').filter({ hasText: names[0] }).getByRole('button', { name: '删除' })
    expect(await remove.evaluate((node) => [getComputedStyle(node).fontSize, getComputedStyle(node).fontWeight])).toEqual(['13px', '700'])
    expect(await page.getByRole('button', { name: '添加管理员' }).evaluate((node) => getComputedStyle(node).fontSize)).toBe('14px')
    await page.screenshot({ path: info.outputPath(`administrators-layout-${width}.png`), fullPage: true })
  }
})

test('uses the same administrator presentation, with only the account tab differing', async ({ page, browser, baseURL }, info) => {
  await page.setViewportSize({ width: 375, height: 812 })
  await login(page)
  const forbidden = /主管理员|普通管理员|配置文件|只读|直接读取公开榜单|所有已注册客户端|用户名 3–64/
  for (const tab of ['用户管理', '管理员管理', '榜单管理']) {
    await page.getByRole('link', { name: tab, exact: true }).click()
    await expect(page.getByRole('heading', { name: tab, exact: true })).toBeVisible()
    expect(await page.locator('.admin-shell').innerText()).not.toMatch(forbidden)
  }
  await page.getByRole('link', { name: '管理员管理', exact: true }).click()
  await expect(page.getByRole('heading', { name: '添加管理员', exact: true })).toBeVisible()
  await page.screenshot({ path: info.outputPath('administrator-owner-375.png'), fullPage: true })
  const session = await (await page.request.get('/v1/admin/session')).json()
  const headers = { Origin: baseURL!, 'X-CSRF-Token': session.csrf_token }
  const credentials = { username: `display-${randomBytes(3).toString('hex')}`, password: randomBytes(24).toString('base64url') }
  const response = await page.request.post('/v1/admin/accounts', { headers, data: credentials })
  expect(response.status()).toBe(201)
  const id = (await response.json()).id
  const context = await browser.newContext({ baseURL, viewport: { width: 375, height: 812 } })
  try {
    const staff = await context.newPage()
    await login(staff, credentials)
    await expect(staff.getByRole('navigation', { name: '控制台导航' }).getByRole('link')).toHaveCount(2)
    expect(await staff.locator('.admin-shell').innerText()).not.toMatch(forbidden)
    await staff.getByRole('link', { name: '用户管理', exact: true }).click()
    await expect(staff.getByRole('heading', { name: '用户管理', exact: true })).toBeVisible()
    await staff.screenshot({ path: info.outputPath('administrator-staff-375.png'), fullPage: true })
    expect((await context.request.get('/v1/admin/accounts')).status()).toBe(403)
    await staff.goto('/admin/accounts')
    await expect(staff.getByRole('heading', { name: '无访问权限', exact: true })).toBeVisible()
  } finally {
    await context.close()
    expect((await page.request.delete(`/v1/admin/accounts/${id}`, { headers, data: {} })).status()).toBe(204)
  }
})

test('aligns a 13px date-style refresh button above the cache timestamp, never on the public page', async ({ page }, info) => {
  await login(page)
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: width === 1440 ? 1000 : 812 })
    const button = page.getByRole('button', { name: '刷新榜单', exact: true })
    const timestamp = page.locator('.admin-shell .board-heading').getByText(/^缓存生成于/)
    await expect(timestamp).toBeVisible()
    const box = (await button.boundingBox())!
    const time = (await timestamp.boundingBox())!
    expect(box.y + box.height).toBeLessThanOrEqual(time.y)
    expect(width === 1440 ? Math.abs(box.x + box.width - time.x - time.width) : Math.abs(box.x - time.x)).toBeLessThan(2)
    expect(box.height).toBeGreaterThanOrEqual(30)
    expect(box.height).toBeLessThanOrEqual(36)
    const style = await button.evaluate((node) => { const css = getComputedStyle(node); return [css.fontSize, css.color, css.backgroundColor, css.borderColor] })
    expect(style).toEqual(['13px', 'rgb(28, 28, 39)', 'rgb(255, 255, 255)', 'rgb(240, 240, 244)'])
    await page.screenshot({ path: info.outputPath(`refresh-${width}.png`), fullPage: true })
  }
  const refresh = page.getByRole('button', { name: '刷新榜单', exact: true })
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await page.mouse.move(0, 0)
  const baseShadow = await refresh.evaluate((node) => getComputedStyle(node).boxShadow)
  await refresh.hover()
  await expect.poll(() => refresh.evaluate((node) => getComputedStyle(node).boxShadow)).not.toBe(baseShadow)
  const response = page.waitForResponse((value) => new URL(value.url()).pathname === '/v1/leaderboards/overall/today')
  await page.mouse.down()
  await expect.poll(() => refresh.evaluate((node) => getComputedStyle(node).transform)).not.toBe('none')
  await page.mouse.up()
  expect((await response).status()).toBe(200)
  await page.emulateMedia({ reducedMotion: 'reduce' })
  expect(await refresh.evaluate((node) => getComputedStyle(node).transitionDuration)).toBe('0s')
  await refresh.hover()
  await page.mouse.down()
  expect(await refresh.evaluate((node) => getComputedStyle(node).transform)).toBe('none')
  await page.mouse.up()
  await page.getByRole('navigation', { name: '页面导航' }).getByRole('link', { name: '排行榜', exact: true }).click()
  await expect(page.getByRole('button', { name: '刷新榜单', exact: true })).toHaveCount(0)
})

test('uses one compact server-side nickname-or-ID search in both user lists', async ({ page, request, baseURL }, info) => {
  const prefix = `compact-${randomBytes(3).toString('hex')}`
  seedUsers(prefix, 51)
  const numericName = '1234567890123456'
  const response = await request.post('/v1/clients/register', { headers: { Authorization: `Bearer ${randomBytes(32).toString('base64url')}` }, data: { display_name: numericName, avatar_id: 1 } })
  expect(response.status()).toBe(201)
  const id = String((await response.json()).client_id)
  await login(page)
  await page.getByRole('link', { name: '用户管理', exact: true }).click()
  const search = async (term: string) => { await page.getByLabel('昵称或ID', { exact: true }).fill(term); await page.getByRole('button', { name: '查询', exact: true }).click() }
  await search(prefix)
  await expect(page.getByText('第 1 页 · 50 位用户')).toBeVisible()
  await page.getByRole('button', { name: '下一页' }).click()
  await expect(page.getByText('第 2 页 · 1 位用户')).toBeVisible()
  await search(numericName)
  await expect(page.getByText('第 1 页 · 1 位用户')).toBeVisible()
  await expect(page.getByRole('row').filter({ hasText: numericName })).toBeVisible()
  await search(id)
  await expect(page.getByRole('row').filter({ hasText: numericName })).toBeVisible()
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: width === 1440 ? 1000 : 812 })
    await expect(page.getByRole('combobox')).toHaveCount(0)
    const input = (await page.getByLabel('昵称或ID', { exact: true }).boundingBox())!
    const button = (await page.getByRole('button', { name: '查询', exact: true }).boundingBox())!
    expect(Math.abs(input.y - button.y)).toBeLessThan(4)
    expect(input.x + input.width).toBeLessThanOrEqual(button.x)
    expect(await page.getByRole('button', { name: '查询', exact: true }).evaluate((node) => getComputedStyle(node).fontSize)).toBe('12px')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: info.outputPath(`compact-users-${width}.png`), fullPage: true })
  }
  const session = await (await page.request.get('/v1/admin/session')).json()
  expect((await page.request.put(`/v1/admin/users/${id}/ranking-visibility`, { headers: { Origin: baseURL!, 'X-CSRF-Token': session.csrf_token }, data: { hidden: true } })).status()).toBe(200)
  await page.getByRole('link', { name: '榜单管理', exact: true }).click()
  await page.getByRole('button', { name: '已下榜用户', exact: true }).click()
  await search(numericName)
  await expect(page.getByRole('row').filter({ hasText: numericName })).toBeVisible()
  await expect(page.getByRole('combobox')).toHaveCount(0)
  await page.screenshot({ path: info.outputPath('compact-hidden-375.png'), fullPage: true })
  await search('no-matching-identity')
  await expect(page.getByText('未找到匹配的用户。')).toBeVisible()
})

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
    await page.getByRole('navigation', { name: '页面导航' }).getByRole('link', { name: '排行榜', exact: true }).click()
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
  await page.getByRole('navigation', { name: '页面导航' }).getByRole('link', { name: '排行榜', exact: true }).click()
  await expect(page.getByRole('link', { name: '登录', exact: true })).toBeVisible()
})
