// @vitest-environment node
import { describe, expect, it, vi } from 'vitest'

async function config(executablePath = '', channel = '') {
  vi.stubEnv('PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH', executablePath)
  vi.stubEnv('PLAYWRIGHT_CHROMIUM_CHANNEL', channel)
  vi.resetModules()
  return (await import('./playwright.config')).default
}

describe('reusable browser configuration', () => {
  it('does not impose an executable path or channel by default', async () => {
    const { use } = await config()
    expect(use?.channel).toBeUndefined()
    expect(use?.launchOptions).toBeUndefined()
  })

  it('passes an existing browser path through without a version requirement', async () => {
    const { use } = await config('/shared browser/chromium')
    expect(use?.launchOptions?.executablePath).toBe('/shared browser/chromium')
    expect(use?.channel).toBeUndefined()
  })

  it('can select an installed browser channel instead', async () => {
    const { use } = await config('', 'chrome')
    expect(use?.channel).toBe('chrome')
    expect(use?.launchOptions).toBeUndefined()
  })

  it('supports an isolated built web directory while keeping the existing default', async () => {
    vi.stubEnv('RANKING_TEST_WEB_DIR', '/isolated/web-dist')
    const isolated = await config()
    expect((isolated.webServer as { env: Record<string, string> }).env.RANKING_WEB_DIR).toBe('/isolated/web-dist')
    vi.unstubAllEnvs()
    const defaults = await config()
    expect((defaults.webServer as { env: Record<string, string> }).env.RANKING_WEB_DIR).toMatch(/\/web\/dist$/)
  })

  it('tests console mutations with the real five-minute timer rather than a shortened interval', async () => {
    vi.stubEnv('RANKING_TEST_ADMIN_CONFIG', '/isolated/admin.json')
    const enabled = await config()
    expect((enabled.webServer as { env: Record<string, string> }).env.RANKING_CACHE_REFRESH_INTERVAL).toBe('5m')
  })

  it('rejects ambiguous path and channel settings', async () => {
    await expect(config('/shared/chromium', 'chrome')).rejects.toThrow('not both')
  })
})
