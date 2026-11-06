import { test, expect } from '@playwright/test'
import { createLimitedUser, signIn } from './fixtures'

// Role-based access, from the user's side.
//
// The server enforces every permission on every endpoint; the interface hides
// what the caller cannot do. This spec proves the second half against a real
// limited principal — an account holding only `readonly_auditor` — because the
// existing suite only ever signs in as the platform owner. Two surfaces matter:
// the permission-filtered navigation, and the forbidden page a direct URL lands
// on. The server half is asserted alongside, so a UI that hid a link while the
// API still served the data could not pass.

test.describe('role-based access', () => {
  test('a limited user sees only the navigation their permissions allow', async ({ page }) => {
    await signIn(page)
    const { email, password } = await createLimitedUser(page)
    await signIn(page, email, password)

    // The ungrouped product view is still grouped, but only with the groups that
    // contain at least one readable route. `readonly_auditor` can read audit,
    // SSO, consent and tenant settings, so Administration survives — but the
    // Users and Roles links inside it do not.
    const links = page.locator('.sidebar a[href]')
    await expect(links.filter({ hasText: 'Accounts' })).toHaveCount(1)
    await expect(page.locator('.sidebar a[href="/app/admin/users"]')).toHaveCount(0)
    await expect(page.locator('.sidebar a[href="/app/admin/roles"]')).toHaveCount(0)
    // A route the role *can* read is still offered.
    await expect(page.locator('.sidebar a[href="/app/admin/audit"]')).toHaveCount(1)
  })

  test('a direct URL to a forbidden route renders the forbidden page, and the API refuses it', async ({ page }) => {
    await signIn(page)
    const { email, password } = await createLimitedUser(page)
    await signIn(page, email, password)

    await page.goto('/app/admin/users')
    // The guard names the permission that is missing rather than failing blank.
    await expect(page.getByText('You do not have access to this area')).toBeVisible()
    await expect(page.getByText('users.read')).toBeVisible()
    await expect(page.getByRole('link', { name: /Back to overview/i })).toBeVisible()
    // The page's own heading never renders, so no data leaked behind the guard.
    await expect(page.getByRole('heading', { level: 1, name: 'Users' })).toHaveCount(0)

    // The same request is refused by the server: the guard is convenience, not
    // the boundary.
    expect((await page.request.get('/api/v1/users')).status()).toBe(403)
    expect((await page.request.get('/api/v1/roles')).status()).toBe(403)
    // A permission the role does hold is served.
    expect((await page.request.get('/api/v1/accounts/')).status()).toBe(200)
  })

  test('a role workspace never widens access beyond the role', async ({ page }) => {
    await signIn(page)
    const { email, password } = await createLimitedUser(page)
    await signIn(page, email, password)

    // The Admin workspace leads with every administration route. A workspace
    // narrows and reorders; it never grants, so the links the role cannot read
    // are dropped from it just as they are from the product view.
    await page.goto('/app')
    await page.locator('.workspace-switcher').click()
    await page.getByRole('button', { name: /^Admin/ }).first().click()
    await expect(page.locator('html')).toHaveAttribute('data-workspace', 'admin')

    await expect(page.locator('.sidebar a[href="/app/admin/users"]')).toHaveCount(0)
    await expect(page.locator('.sidebar a[href="/app/admin/audit"]')).toHaveCount(1)
    expect(await page.locator('.sidebar__nav a.nav-item').allTextContents()).toEqual([
      'Overview',
      'Audit log',
      'Configuration',
      'Single sign-on',
      'Consent',
    ])
  })
})
