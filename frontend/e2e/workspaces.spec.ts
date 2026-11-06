import { type Page } from '@playwright/test'
import { test, expect } from './fixtures'

// Workspaces: the switcher and the workspace-shaped navigation.
//
// A workspace is a lens on the same application, not a second one: it decides
// which of the existing routes lead the sidebar and in what order, and it grants
// nothing. `workspace.test.tsx` proves the model in jsdom; this spec proves the
// real shell renders it against the real API — the switcher lists every
// workspace, choosing one reorders and narrows the sidebar, the up/down controls
// rearrange it for the session, and switching away and back restores the
// intended order.
//
// There was no end-to-end coverage of any of this before this spec.

const SALES_ORDER = [
  'Overview',
  'Deals',
  'Leads',
  'Contacts',
  'Accounts',
  'Tasks',
  'Forecasting',
  'Reports',
  'Simulation Lab',
]

/** Opens the workspace menu and picks a workspace by its visible name. */
async function chooseWorkspace(page: Page, name: string) {
  await page.locator('.workspace-switcher').click()
  await page.getByRole('button', { name: new RegExp(`^${name}`) }).first().click()
}

test.describe('workspaces', () => {
  test('the switcher offers every workspace and names the active one', async ({ authedPage: page }) => {
    await page.goto('/app')
    // The default workspace is the ungrouped product view.
    await expect(page.locator('html')).toHaveAttribute('data-workspace', 'all')

    await page.locator('.workspace-switcher').click()
    const options = page.locator('.workspace-option__name')
    await expect(options).toHaveCount(8)
    // The switcher names the whole set, not a hardcoded subset.
    expect(await options.allTextContents()).toEqual([
      'Sales',
      'Executive',
      'Support',
      'Marketing',
      'Operations',
      'Analyst',
      'Admin',
      'Everything',
    ])
  })

  test('choosing a workspace reorders and narrows the sidebar', async ({ authedPage: page }) => {
    await page.goto('/app')
    await chooseWorkspace(page, 'Sales')

    // The choice is reflected on the document and persisted, so a reload keeps it.
    await expect(page.locator('html')).toHaveAttribute('data-workspace', 'sales')
    await page.reload()
    await expect(page.locator('html')).toHaveAttribute('data-workspace', 'sales')

    // A role workspace is one flat, ordered list under a single group label —
    // not the grouped product navigation.
    await expect(page.locator('.sidebar__group-label')).toHaveText(['Sales'])
    expect(await page.locator('.sidebar__nav a.nav-item').allTextContents()).toEqual(SALES_ORDER)
  })

  test('the reorder controls rearrange the sidebar for the session only', async ({ authedPage: page }) => {
    await page.goto('/app')
    await chooseWorkspace(page, 'Sales')
    await expect(page.locator('.sidebar__nav a.nav-item')).toHaveText(SALES_ORDER)

    // The up/down controls appear only inside a reorderable workspace.
    await page.locator('.nav-row').filter({ hasText: 'Deals' }).hover()
    await page.locator('.nav-row__move[aria-label="Move Deals up"]').click()
    expect(await page.locator('.sidebar__nav a.nav-item').allTextContents()).toEqual([
      'Deals',
      'Overview',
      'Leads',
      'Contacts',
      'Accounts',
      'Tasks',
      'Forecasting',
      'Reports',
      'Simulation Lab',
    ])

    // Switching away and back re-seeds the order, so the session tweak does not
    // silently become the workspace's new shape.
    await chooseWorkspace(page, 'Everything')
    await expect(page.locator('html')).toHaveAttribute('data-workspace', 'all')
    await chooseWorkspace(page, 'Sales')
    expect(await page.locator('.sidebar__nav a.nav-item').allTextContents()).toEqual(SALES_ORDER)
  })

  test('the product view keeps the full grouped navigation', async ({ authedPage: page }) => {
    await page.goto('/app')
    // "Everything" is the way back to the grouped product navigation.
    await expect(page.locator('.sidebar__group-label')).toHaveText([
      'Workspace',
      'Data',
      'Platform',
      'Administration',
    ])
    // The grouped view is not reorderable, so it offers no move controls.
    await expect(page.locator('.nav-row__move')).toHaveCount(0)
  })
})
