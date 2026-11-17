import { readFileSync } from 'node:fs'
import { randomBytes } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'

const config = process.env.RANKING_TEST_ADMIN_CONFIG
const credentials: { username: string; password: string } = config ? JSON.parse(readFileSync(config, 'utf8')) : { username: '', password: '' }
test.skip(!config, 'Requires an isolated enabled console')

async function login(page: Page) {
  await page.goto('/admin/login')
  await page.getByLabel('用户名', { exact: true }).fill(credentials.username)
  await page.getByLabel('密码', { exact: true }).fill(credentials.password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('navigation', { name: '控制台导航' })).toBeVisible()
}

test('places hidden-user search immediately below its tab without a duplicate heading', async ({ page, request, baseURL }, info) => {
  const name = `hidden-list-${randomBytes(3).toString('hex')}`
  const registration = await request.post('/v1/clients/register', { headers: { Authorization: `Bearer ${randomBytes(32).toString('base64url')}` }, data: { display_name: name, avatar_id: 1 } })
  expect(registration.status()).toBe(201)
  const id = String((await registration.json()).client_id)
  await login(page)
  const session = await (await page.request.get('/v1/admin/session')).json()
  expect((await page.request.put(`/v1/admin/users/${id}/ranking-visibility`, { headers: { Origin: baseURL!, 'X-CSRF-Token': session.csrf_token }, data: { hidden: true } })).status()).toBe(200)
  await page.getByRole('button', { name: '已下榜用户', exact: true }).click()
  const region = page.getByRole('region', { name: '已下榜用户' })
  await expect(region.getByRole('row').filter({ hasText: name })).toBeVisible()
  await expect(region.getByRole('heading')).toHaveCount(0)
  await expect(region.getByText('EXCLUDED USERS')).toHaveCount(0)
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: width === 1440 ? 1000 : 812 })
    const tab = (await page.getByRole('button', { name: '已下榜用户', exact: true }).boundingBox())!
    const search = (await region.getByLabel('昵称或ID').boundingBox())!
    expect(search.y - tab.y - tab.height).toBeLessThan(35)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: info.outputPath(`hidden-list-${width}.png`), fullPage: true })
  }
  await page.getByRole('link', { name: '用户管理', exact: true }).click()
  await expect(page.getByRole('heading', { name: '用户管理' })).toBeVisible()
})

test('uses the same existing title typography in both administrator cards', async ({ page }, info) => {
  await login(page)
  await page.getByRole('link', { name: '管理员管理', exact: true }).click()
  const add = page.getByRole('region', { name: '添加管理员' })
  const list = page.getByRole('region', { name: '管理员列表' })
  const typography = (node: Element) => { const css = getComputedStyle(node); return [css.fontFamily, css.fontSize, css.fontWeight, css.lineHeight] }
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: width === 1440 ? 1000 : 812 })
    const left = await add.getByRole('heading').evaluate(typography)
    const right = await list.getByRole('heading').evaluate(typography)
    expect(left).toEqual(right)
    expect(left[1]).toBe('20px')
    expect(left[2]).toBe('400')
    const leftBox = (await add.boundingBox())!
    const rightBox = (await list.boundingBox())!
    if (width === 1440) expect(Math.abs(rightBox.width - 2 * leftBox.width)).toBeLessThan(2)
    else expect(leftBox.y + leftBox.height).toBeLessThan(rightBox.y)
    await page.screenshot({ path: info.outputPath(`account-titles-${width}.png`), fullPage: true })
  }
})
