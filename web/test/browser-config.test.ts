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

  it('rejects ambiguous path and channel settings', async () => {
    await expect(config('/shared/chromium', 'chrome')).rejects.toThrow('not both')
  })
})
