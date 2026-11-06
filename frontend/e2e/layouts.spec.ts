import { type Page } from '@playwright/test'
import { test, expect, signIn } from './fixtures'

// Interface experiences (the five shells / "layouts").
//
// The appearance page offers five complete frames — classic, stratus, horizon,
// pulse and slate — each a different anatomy of the same application. The
// appearance spec covers the *style* preview/apply/reset flow; the shells are
// otherwise only proved in jsdom by `shells.test.tsx`, which asserts the markup
// of each shell but never mounts it against real data in a real browser.
//
// This spec closes that gap: the picker applies an experience live, the chosen
// frame actually renders, the choice is remembered on the device, and the frame
// survives a reload with the session intact.

// Each experience, its `data-layout` value, and the signature element that
// proves its frame mounted (not an attribute the picker could set by itself).
const EXPERIENCES = [
  { name: 'Classic', layout: 'classic', signature: '.app-shell' },
  { name: 'Stratus', layout: 'stratus', signature: '.stratus-header' },
  { name: 'Horizon', layout: 'horizon', signature: '.horizon-subbar' },
  { name: 'Pulse', layout: 'pulse', signature: '.pulse-tile' },
  { name: 'Slate', layout: 'slate', signature: '.slate-side__nav' },
] as const

/** Reads the layout the document reports, so an assertion cannot pass on a stale frame. */
async function currentLayout(page: Page): Promise<string | null> {
  return page.locator('html').getAttribute('data-layout')
}

test.describe('interface experiences', () => {
  test('the appearance page offers every experience', async ({ authedPage: page }) => {
    await page.goto('/app/appearance')
    await expect(page.getByRole('heading', { name: 'Appearance', exact: true })).toBeVisible()
    // Exactly the five experiences, each named once. The active card also
    // carries a "Current" badge, so match on the card rather than the bare name.
    await expect(page.locator('.layout-card')).toHaveCount(EXPERIENCES.length)
    for (const { name } of EXPERIENCES) {
      await expect(page.locator('.layout-card').filter({ hasText: name })).toHaveCount(1)
    }
  })

  test('picking an experience applies its frame and marks it current', async ({ authedPage: page }) => {
    await page.goto('/app/appearance')

    for (const { name, layout, signature } of EXPERIENCES) {
      await page.locator('.layout-card').filter({ hasText: name }).first().click()
      await expect(page.locator('html')).toHaveAttribute('data-layout', layout)
      await expect(page.locator(signature).first()).toBeVisible()
      // The card the document agrees with is the one marked active.
      await expect(page.locator('.layout-card--active')).toContainText(name)
    }
  })

  test('the chosen experience is remembered on the device across a reload', async ({ authedPage: page }) => {
    await page.goto('/app/appearance')
    await page.locator('.layout-card').filter({ hasText: 'Slate' }).first().click()
    await expect(page.locator('html')).toHaveAttribute('data-layout', 'slate')

    // The experience is per-device, so it must survive a reload without the
    // server being involved.
    await page.reload()
    expect(await currentLayout(page)).toBe('slate')
    await expect(page.locator('.shell.slate')).toBeVisible()

    // And it is a frame for the whole app, not just the appearance page.
    await page.goto('/app/accounts')
    await expect(page.locator('html')).toHaveAttribute('data-layout', 'slate')
    await expect(page.locator('.shell.slate')).toBeVisible()
  })

  test('a non-classic frame keeps the workspace switcher and permission-filtered navigation', async ({ authedPage: page }) => {
    await page.addInitScript(() => localStorage.setItem('forgecrm.layout', 'stratus'))
    await signIn(page)
    await expect(page.locator('.shell.stratus')).toBeVisible()

    // The shared chrome is the same in every frame: the switcher still works
    // and the launcher lists the sections the user may open.
    await page.locator('.stratus-launcher__trigger').click()
    const launcher = page.locator('.stratus-launcher__panel')
    await expect(launcher).toBeVisible()
    await expect(launcher.getByRole('link', { name: 'Accounts' })).toBeVisible()

    await page.locator('.workspace-switcher').click()
    await page.getByRole('button', { name: /^Sales/ }).first().click()
    await expect(page.locator('html')).toHaveAttribute('data-workspace', 'sales')
  })
})
