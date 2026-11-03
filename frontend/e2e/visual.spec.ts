import { test, expect, type Locator, type Page } from '@playwright/test'
import { createAccount, RUN_ID, signIn } from './fixtures'

// Visual regression for the surfaces where a layout or token regression is
// most visible. The set is deliberately small — one view per distinct visual
// primitive (shell, form, table, stacked table, dialog, stat tiles, static
// page), not one per route — because every image is a maintenance cost and a
// new source of flakiness.
//
// Baselines are compared on Linux only. They are generated inside the pinned
// Playwright image CI uses (see the `visual` job in .github/workflows/ci.yml),
// so the browser, fonts and rasterisation are identical on both sides. A run on
// another platform will diff on font hinting alone and must not be trusted.
//
// The golden rule for masks: mask the value, keep the chrome. Every mask below
// targets the smallest element that carries run-specific text (a number, a
// date, an id), never a whole card or table, so the layout under test stays
// visible in the comparison.

/** Waits until the app has rendered real content, never a loading skeleton. */
async function ready(page: Page, marker: Locator) {
  await expect(marker).toBeVisible({ timeout: 15_000 })
  // The skeletons are neutralised by reducedMotion in the project, but a
  // lingering `aria-busy="true"` region means data has not landed yet.
  await expect(page.locator('[aria-busy="true"]')).toHaveCount(0)
}

/** Dynamic content that must never be part of a comparison. */
function dynamic(page: Page): Locator[] {
  return [
    // Metric and stat values/hints carry live numbers.
    page.locator('.metric__value, .metric__hint'),
    page.locator('.stat__value, .stat__hint'),
    // Version/environment/commit badges.
    page.locator('.hero__chips'),
    // Platform card shows the API process start time.
    page.locator('.dl'),
    // Every rendered timestamp is data-dependent.
    page.locator('time'),
    // Initials derive from run-specific names.
    page.locator('.avatar'),
    // "showing X–Y of Z" depends on how much data the run created.
    page.locator('.pagination'),
    // Defaults to today-30…today.
    page.locator('input[type="date"]'),
  ]
}

/** Sets the app's own persisted theme/locale before the first paint. */
async function prime(page: Page, opts: { theme?: 'light' | 'dark'; locale?: 'en' | 'ru' }) {
  await page.addInitScript(
    ([theme, locale]) => {
      try {
        if (theme) localStorage.setItem('forgecrm.theme', theme)
        if (locale) localStorage.setItem('forgecrm.locale', locale)
      } catch {
        // A blocked localStorage only means the defaults are used.
      }
    },
    [opts.theme ?? null, opts.locale ?? null],
  )
}

test.describe('visual: sign-in', () => {
  test('the two-pane sign-in page', async ({ page }) => {
    await page.goto('/login')
    await ready(page, page.getByRole('heading', { level: 1 }))
    await expect(page).toHaveScreenshot('login-desktop-light-en.png', {
      mask: dynamic(page),
    })
  })

  test('the sign-in page in Russian', async ({ page }) => {
    // RU is a distinct signal here: AGENTS.md records RU/EN mixed-copy bugs.
    await prime(page, { locale: 'ru' })
    await page.goto('/login')
    await ready(page, page.getByRole('heading', { level: 1 }))
    await expect(page).toHaveScreenshot('login-desktop-light-ru.png', {
      mask: dynamic(page),
    })
  })

  test('the sign-in page collapsed to one column on a phone', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/login')
    await ready(page, page.getByRole('heading', { level: 1 }))
    // The aside is hidden at this width; the snapshot proves the collapse.
    await expect(page.locator('.login__aside')).toBeHidden()
    await expect(page).toHaveScreenshot('login-mobile-light-en.png', {
      mask: dynamic(page),
    })
  })
})

test.describe('visual: workspace', () => {
  test('the app shell and dashboard in dark mode', async ({ page }) => {
    await prime(page, { theme: 'dark' })
    await signIn(page)
    await ready(page, page.getByRole('heading', { level: 1 }))
    // Dark mode is where the contrast tokens are most visible.
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
    await expect(page).toHaveScreenshot('dashboard-desktop-dark-en.png', {
      mask: dynamic(page),
    })
  })

  test('the accounts data table', async ({ page }) => {
    await signIn(page)
    const name = `Visual Corp ${RUN_ID}`
    await createAccount(page, name)
    await page.goto('/accounts')
    // Filter to the one row this run created so the table body is deterministic
    // regardless of how much data earlier runs left behind.
    await page.getByPlaceholder(/search/i).fill(name)
    await ready(page, page.getByRole('table'))
    await expect(page.getByRole('cell', { name })).toBeVisible()
    await expect(page).toHaveScreenshot('accounts-table-desktop-light-en.png', {
      // Mask the run-specific name and the row's timestamp, not the table.
      mask: [...dynamic(page), page.getByRole('cell', { name })],
    })
  })

  test('the accounts table stacked into cards on a phone', async ({ page }) => {
    await signIn(page)
    const name = `Visual Corp ${RUN_ID}`
    await createAccount(page, name)
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/accounts')
    await page.getByPlaceholder(/search/i).fill(name)
    await ready(page, page.getByRole('table'))
    // The responsive spec proves no horizontal overflow numerically; this
    // proves the labelled-card layout renders in the right shape.
    await expect(page).toHaveScreenshot('accounts-cards-mobile-light-en.png', {
      mask: [...dynamic(page), page.getByRole('cell', { name })],
    })
  })

  test('the create-account dialog', async ({ page }) => {
    await signIn(page)
    await page.goto('/accounts')
    await page.getByRole('button', { name: 'New account' }).click()
    const dialog = page.getByRole('dialog')
    await ready(page, dialog)
    // Covers the Modal primitive: scrim, panel, header/footer and focus ring.
    await expect(page).toHaveScreenshot('account-dialog-light-en.png', {
      mask: dynamic(page),
    })
  })

  test('the reports stat tiles and pipeline table', async ({ page }) => {
    await signIn(page)
    await page.goto('/reports')
    await ready(page, page.getByRole('heading', { name: 'Reports' }))
    await expect(page).toHaveScreenshot('reports-desktop-light-en.png', {
      mask: dynamic(page),
    })
  })
})
