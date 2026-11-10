import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { readFileSync } from 'node:fs'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [react(), tailwindcss(), {
    name: 'application-source-notices',
    generateBundle() {
      const notices = readFileSync(new URL('../THIRD_PARTY_NOTICES.md', import.meta.url), 'utf8')
      const license = readFileSync(new URL('../third_party/react-bits/LICENSE.md', import.meta.url), 'utf8')
      this.emitFile({ type: 'asset', fileName: 'assets/third-party-notices.txt', source: `${notices}\n\n${license}` })
    },
  }],
  server: {
    host: '127.0.0.1',
    strictPort: true,
    proxy: {
      '/healthz': { target: process.env.RANKING_DEV_PROXY_TARGET ?? 'http://127.0.0.1:8080' },
      '/readyz': { target: process.env.RANKING_DEV_PROXY_TARGET ?? 'http://127.0.0.1:8080' },
      '/v1/leaderboards': { target: process.env.RANKING_DEV_PROXY_TARGET ?? 'http://127.0.0.1:8080' },
      '/avatars': { target: process.env.RANKING_DEV_PROXY_TARGET ?? 'http://127.0.0.1:8080' },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/test/**/*.test.{ts,tsx}', 'test/browser-config.test.ts'],
    restoreMocks: true,
    unstubGlobals: true,
    unstubEnvs: true,
  },
})
