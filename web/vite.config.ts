import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    host: '127.0.0.1',
    strictPort: true,
    proxy: {
      '/healthz': { target: process.env.RANKING_DEV_PROXY_TARGET ?? 'http://127.0.0.1:8080' },
      '/readyz': { target: process.env.RANKING_DEV_PROXY_TARGET ?? 'http://127.0.0.1:8080' },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/test/**/*.test.{ts,tsx}'],
    restoreMocks: true,
    unstubGlobals: true,
    unstubEnvs: true,
  },
})
