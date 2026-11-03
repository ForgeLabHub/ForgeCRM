import { defineConfig, devices } from '@playwright/test'

// End-to-end tests run against the real stack: a real PostgreSQL database, the
// real Go API and the real frontend bundle. They deliberately do not stub the
// network. A test that mocks the API proves the components render; it does not
// prove a button performs the operation it claims to, which is the thing most
// likely to be broken and the thing a reviewer will try first.
//
// The suite needs three things to be running, which CI starts before invoking
// Playwright (see .github/workflows/ci.yml):
//   1. PostgreSQL with migrations applied
//   2. forgecrm-api on FORGECRM_API_URL (default http://127.0.0.1:8080)
//   3. the Vite dev server, started below
const API_URL = process.env.FORGECRM_API_URL ?? 'http://127.0.0.1:8080'

export default defineConfig({
  testDir: './e2e',
  // A failed test should not leave a stray account behind that breaks a later
  // run, and the tests touch shared state, so they run one at a time.
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  timeout: 30_000,
  expect: { timeout: 10_000 },

  use: {
    baseURL: 'http://127.0.0.1:5173',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
      // The visual suite has its own project (below) with a pinned context and
      // committed baselines; keep it out of the behavioural run.
      testIgnore: /visual\.spec\.ts/,
    },
    {
      // Opt-in: this is the only project that writes and compares PNG baselines.
      // It pins its own deterministic context — fixed viewport, DPR 1, one
      // locale, one timezone, reduced motion — because a screenshot is only
      // meaningful when everything except the thing under test is held still.
      // Baselines are Linux-only and are generated inside the same pinned
      // Playwright image CI uses (see .github/workflows/ci.yml); a snapshot
      // taken on another platform would diff on font rasterisation alone.
      name: 'visual',
      testMatch: /visual\.spec\.ts/,
      // A retry would paper over a real pixel regression. The suite is
      // already serial through the global `workers: 1` / `fullyParallel: false`.
      retries: 0,
      use: {
        ...devices['Desktop Chrome'],
        viewport: { width: 1280, height: 800 },
        deviceScaleFactor: 1,
        locale: 'en-US',
        timezoneId: 'UTC',
        colorScheme: 'light',
        // Not a top-level `use` option in this Playwright version; it is a
        // browser-context option. The stylesheet already honours
        // `prefers-reduced-motion: reduce`, so this freezes the skeleton
        // shimmer and modal transitions before a screenshot is taken.
        contextOptions: { reducedMotion: 'reduce' },
        trace: 'off',
        screenshot: 'off',
      },
      expect: {
        timeout: 10_000,
        toHaveScreenshot: {
          // Absorbs sub-pixel antialiasing noise without hiding a real layout or
          // colour change.
          maxDiffPixelRatio: 0.01,
          threshold: 0.2,
          animations: 'disabled',
          caret: 'hide',
        },
      },
    },
  ],

  webServer: {
    // --host pins Vite to IPv4. Its default is `localhost`, which resolves to
    // ::1 on some runners while Playwright polls 127.0.0.1 below; the server
    // then starts fine and the wait still times out.
    command: 'npm run dev -- --host 127.0.0.1 --port 5173 --strictPort',
    url: 'http://127.0.0.1:5173',
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
    env: { FORGECRM_API_URL: API_URL },
  },
})
