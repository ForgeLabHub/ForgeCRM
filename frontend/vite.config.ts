import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// The API is same-origin in production (Nginx serves both), so the default
// proxy target only matters for `npm run dev`. Keeping requests same-origin is
// what makes the session cookie and the CSRF double-submit work without CORS.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: process.env.FORGECRM_API_URL ?? 'http://127.0.0.1:8080',
        changeOrigin: false,
      },
      '/health': {
        target: process.env.FORGECRM_API_URL ?? 'http://127.0.0.1:8080',
        changeOrigin: false,
      },
    },
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
    css: false,
  },
})
