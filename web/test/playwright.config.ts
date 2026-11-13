import { defineConfig } from '@playwright/test'
import { fileURLToPath } from 'node:url'
import { resolve } from 'node:path'

const port = Number(process.env.RANKING_TEST_PORT ?? 4187)
if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error('RANKING_TEST_PORT must be 1–65535')
const baseURL = `http://127.0.0.1:${port}`
const outputDir = process.env.PLAYWRIGHT_OUTPUT_DIR ?? fileURLToPath(new URL('./results', import.meta.url))
const binary = resolve(process.env.RANKING_TEST_BINARY ?? fileURLToPath(new URL('../../bin/ranking', import.meta.url)))
const executablePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || undefined
const channel = process.env.PLAYWRIGHT_CHROMIUM_CHANNEL || undefined
if (executablePath && channel) throw new Error('Choose PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH or PLAYWRIGHT_CHROMIUM_CHANNEL, not both')

export default defineConfig({
  testDir: '.',
  testMatch: '**/*.spec.ts',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 30_000,
  outputDir,
  reporter: 'list',
  use: {
    baseURL,
    browserName: 'chromium',
    channel,
    launchOptions: executablePath ? { executablePath } : undefined,
    viewport: { width: 1440, height: 1000 },
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  webServer: {
    command: JSON.stringify(binary),
    url: `${baseURL}/healthz`,
    cwd: fileURLToPath(new URL('../..', import.meta.url)),
    env: {
      RANKING_HTTP_ADDR: `127.0.0.1:${port}`,
      RANKING_WEB_DIR: resolve(process.env.RANKING_TEST_WEB_DIR ?? fileURLToPath(new URL('../dist', import.meta.url))),
      RANKING_MODELS_PATH: fileURLToPath(new URL('../../config/models.json', import.meta.url)),
      RANKING_SHUTDOWN_TIMEOUT: '5s',
      RANKING_CACHE_REFRESH_INTERVAL: '200ms',
      RANKING_SQLITE_PATH: resolve(outputDir, '..', 'databases', String(port), 'ranking.db'),
    },
    reuseExistingServer: false,
    timeout: 15_000,
    gracefulShutdown: { signal: 'SIGTERM', timeout: 8_000 },
  },
})
