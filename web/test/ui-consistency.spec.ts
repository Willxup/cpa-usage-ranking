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
    const selected = page.locator('.app-shell:visible .board-control button[aria-pressed="true"]')
    expect(await selected.evaluate((node) => { const css = getComputedStyle(node); return css.borderTopColor === css.backgroundColor })).toBe(true)
  }
  await page.getByRole('link', { name: '登录', exact: true }).click()
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: 900 })
    expect(await page.getByRole('navigation', { name: '页面导航' }).getByRole('link', { name: '排行榜' }).evaluate(style)).toEqual(fonts.get(width))
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  }
  expect(await page.getByRole('button', { name: '登录', exact: true }).evaluate((node) => getComputedStyle(node).fontSize)).toBe('14px')
  await page.getByLabel('用户名', { exact: true }).fill(credentials.username)
  await page.getByLabel('密码', { exact: true }).fill(credentials.password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: 900 })
    expect(await page.getByRole('navigation', { name: '页面导航' }).getByRole('link', { name: '排行榜' }).evaluate(style)).toEqual(fonts.get(width))
    expect(await page.getByRole('button', { name: '退出' }).evaluate(style)).toEqual(fonts.get(width))
    expect(await page.getByRole('link', { name: '用户管理' }).evaluate((node) => getComputedStyle(node).fontSize)).toBe('14px')
    const selected = page.locator('.admin-shell .board-control button[aria-pressed="true"]:visible')
    for (const node of await selected.all()) expect(await node.evaluate((item) => { const css = getComputedStyle(item); return css.borderTopColor === css.backgroundColor })).toBe(true)
    await page.screenshot({ path: info.outputPath(`navigation-${width}.png`), fullPage: true })
  }
  await page.keyboard.press('Tab')
  const boardButton = page.getByRole('button', { name: '用量榜', exact: true })
  await boardButton.focus()
  expect(await boardButton.evaluate((node) => getComputedStyle(node).outlineStyle)).not.toBe('none')
  await expect(page.getByRole('heading', { name: '今日 · 综合总榜' })).toBeVisible()
  let releaseBoard!: () => void
  let beginBoard!: () => void
  const boardBlocked = new Promise<void>((resolve) => { releaseBoard = resolve })
  const boardStarted = new Promise<void>((resolve) => { beginBoard = resolve })
  await page.route('**/v1/leaderboards/usage/today', async (route) => { beginBoard(); await boardBlocked; await route.continue().catch(() => {}) }, { times: 1 })
  try {
    await boardButton.click()
    await boardStarted
    for (const width of [1440, 375]) {
      await page.setViewportSize({ width, height: 900 })
      await expect(page.getByRole('heading', { name: '今日 · 综合总榜' })).toBeVisible()
      await expect(page.getByText(/正在加载|上次结果/)).toHaveCount(0)
      await expect(page.locator('.admin-progress')).toHaveCount(0)
      await page.screenshot({ path: info.outputPath(`board-pending-${width}.png`), fullPage: true })
    }
    releaseBoard()
    await expect(page.getByRole('heading', { name: '今日 · 用量榜' })).toBeVisible()
  } finally { releaseBoard() }
  const navigation = page.getByRole('navigation', { name: '控制台导航' })
  await navigation.getByRole('link', { name: '用户管理' }).click()
  await expect(page.getByText('未找到匹配的用户。')).toBeVisible()
  await navigation.getByRole('link', { name: '管理员管理' }).click()
  let release!: () => void
  let begin!: () => void
  const blocked = new Promise<void>((resolve) => { release = resolve })
  const started = new Promise<void>((resolve) => { begin = resolve })
  await page.route('**/v1/admin/users?*', async (route) => { begin(); await blocked; await route.continue().catch(() => {}) }, { times: 1 })
  try {
    await navigation.getByRole('link', { name: '用户管理' }).click()
    await started
    await expect(page.getByText('未找到匹配的用户。')).toBeVisible()
    await expect(page.getByRole('region', { name: '用户管理' })).toBeVisible()
    await expect(page.getByLabel('昵称或ID')).toBeVisible()
    await expect(page.locator('.admin-progress')).toHaveCount(0)
    await expect(page.getByRole('progressbar')).toHaveCount(0)
    for (const width of [1440, 375]) {
      await page.setViewportSize({ width, height: 900 })
      await page.screenshot({ path: info.outputPath(`tab-pending-${width}.png`), fullPage: true })
    }
    await navigation.getByRole('link', { name: '管理员管理' }).click()
    release()
    await navigation.getByRole('link', { name: '用户管理' }).click()
    await expect(page.getByText('未找到匹配的用户。')).toBeVisible()
  } finally { release() }
  await page.getByRole('button', { name: '退出' }).click()
  await expect(page.getByRole('link', { name: '登录', exact: true })).toBeVisible()
})
