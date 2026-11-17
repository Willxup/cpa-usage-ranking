import { readFileSync, writeFileSync } from 'node:fs'
import { expect, test, type Page } from '@playwright/test'

const config = process.env.RANKING_TEST_ADMIN_CONFIG
const credentials: { username: string; password: string } = config ? JSON.parse(readFileSync(config, 'utf8')) : { username: '', password: '' }
test.skip(!config, 'Requires an isolated enabled console')

type Position = { header: number[]; main: number[]; footer: number[]; icon: number[]; text: number[]; scroll: boolean; width: number; clientWidth: number }
async function position(page: Page): Promise<Position> {
  return page.evaluate(() => {
    const shell = document.querySelector('.route-view:not([hidden]) .app-shell')!
    const rect = (selector: string) => { const box = shell.querySelector(selector)!.getBoundingClientRect(); return [box.left, box.right] }
    return { header: rect('.site-header'), main: rect('main'), footer: rect('footer'), icon: rect('.brand .project-icon'), text: rect('.brand span'), scroll: document.documentElement.scrollHeight > innerHeight + 1, width: innerWidth, clientWidth: document.documentElement.clientWidth }
  })
}
function sameHorizontal(left: Position, right: Position) {
  for (const key of ['header', 'main', 'footer', 'icon', 'text'] as const) {
    for (let i = 0; i < 2; i++) expect(Math.abs(left[key][i] - right[key][i]), `${key}[${i}]`).toBeLessThan(1)
  }
}

test('keeps both levels of console tabs aligned when content height changes', async ({ page }, info) => {
  await page.goto('/admin/login')
  await page.getByLabel('用户名', { exact: true }).fill(credentials.username)
  await page.getByLabel('密码', { exact: true }).fill(credentials.password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('navigation', { name: '控制台导航' })).toBeVisible()
  const logout = page.getByRole('button', { name: '退出', exact: true })
  expect(await logout.evaluate((node) => [getComputedStyle(node).color, getComputedStyle(node).borderWidth])).toEqual(['rgb(190, 52, 52)', '0px'])
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: 1000 })
    await page.getByRole('link', { name: '榜单管理', exact: true }).click()
    await page.getByRole('button', { name: '当前榜单', exact: true }).click()
    const current = page.locator('.admin-shell section[aria-label="榜单管理"] > .tab-view').first()
    await current.evaluate((node) => { (node as HTMLElement).style.minHeight = '2000px' })
    const longSub = await position(page)
    await page.getByRole('button', { name: '已下榜用户', exact: true }).click()
    await expect(page.getByRole('heading', { name: '已下榜用户' })).toBeVisible()
    const shortSub = await position(page)
    sameHorizontal(longSub, shortSub)
    await page.getByRole('button', { name: '当前榜单', exact: true }).click()
    const longMain = await position(page)
    await page.getByRole('link', { name: '用户管理', exact: true }).click()
    await expect(page.getByRole('heading', { name: '用户管理' })).toBeVisible()
    const users = await position(page)
    await page.getByRole('link', { name: '管理员管理', exact: true }).click()
    await expect(page.getByRole('heading', { name: '管理员管理', exact: true })).toBeVisible()
    const accounts = await position(page)
    const footer = page.locator('.admin-shell .brand-footer')
    await expect(footer).toHaveText('CPA Usage Ranking')
    await expect(footer.locator('img')).toHaveAttribute('src', '/assets/brand/project-icon.svg')
    const footerBox = (await footer.boundingBox())!
    const footerBrand = (await footer.locator('span').boundingBox())!
    expect(Math.abs(footerBrand.x + footerBrand.width / 2 - footerBox.x - footerBox.width / 2)).toBeLessThan(1)
    sameHorizontal(longMain, users); sameHorizontal(users, accounts)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    if (width === 1440) { expect(longSub.scroll).toBe(true); expect(shortSub.scroll).toBe(false); expect(users.scroll).toBe(false) }
    writeFileSync(info.outputPath(`positions-${width}.json`), JSON.stringify({ longSub, shortSub, longMain, users, accounts }, null, 2))
    await page.screenshot({ path: info.outputPath(`tabs-${width}.png`), fullPage: true })
  }
})
