import { test as base, expect, type Page } from '@playwright/test'

// Credentials for the bootstrap platform owner. CI provisions this account
// before starting the suite; see scripts/e2e-bootstrap.sh. There is no default
// password in the product, so the suite cannot invent one.
export const E2E_EMAIL = process.env.FORGECRM_E2E_EMAIL ?? 'e2e-owner@forgecrm.test'
export const E2E_PASSWORD = process.env.FORGECRM_E2E_PASSWORD ?? 'E2E-Owner-Passw0rd-2026!'

// Unique per run so a leftover row from a previous run cannot make an assertion
// pass or fail by accident.
export const RUN_ID = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`

/** Signs in through the real login form and waits for the workspace to load. */
export async function signIn(page: Page, email = E2E_EMAIL, password = E2E_PASSWORD) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Password', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Sign in' }).click()

  // The login page also renders an <h1>, so waiting on a heading alone can
  // return before the session exists. Wait for the route change and then for
  // the overview heading, which only appears once /auth/me has succeeded.
  await expect(page).toHaveURL(/\/$/, { timeout: 15_000 })
  await expect(page.getByRole('heading', { level: 1 })).toContainText('Welcome back', {
    timeout: 15_000,
  })
}

/** Creates an account through the interface and returns the name used. */
export async function createAccount(page: Page, name: string) {
  await page.goto('/accounts')
  await page.getByRole('button', { name: 'New account' }).click()

  const dialog = page.getByRole('dialog')
  // The required marker makes the visible label 'Name *', so the stable
  // input id is used rather than the label text.
  await dialog.locator('#account-name').fill(name)
  await dialog.locator('#account-industry').fill('Software')
  await dialog.getByRole('button', { name: 'Create account' }).click()

  // The dialog closing is the signal the write succeeded; a failed request
  // leaves it open with an error.
  await expect(dialog).toBeHidden({ timeout: 15_000 })
  return name
}

/**
 * Creates a contact through the interface and returns its name and id.
 *
 * The id is read back from the API rather than the interface, because the consent
 * tests address the contact by id and need the real one the server assigned.
 */
export async function createContact(page: Page, firstName: string) {
  await page.goto('/contacts')
  await page.getByRole('button', { name: 'New contact' }).click()

  const dialog = page.getByRole('dialog')
  await dialog.locator('#contact-first').fill(firstName)
  // The display name carries spaces and case, which is not a valid local part;
  // slug it so the server's email check passes.
  const slug = firstName.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '')
  await dialog.locator('#contact-email').fill(`${slug}@e2e.test`)
  await dialog.getByRole('button', { name: 'Create contact' }).click()
  await expect(dialog).toBeHidden({ timeout: 15_000 })

  const response = await page.request.get(`/api/v1/contacts/?search=${encodeURIComponent(firstName)}`)
  const body = await response.json()
  const items = (body.items ?? body.data ?? []) as { id: string; first_name?: string }[]
  const match = items.find((c) => c.first_name === firstName)
  if (!match) throw new Error(`contact ${firstName} was not readable through the API`)
  return { name: firstName, id: match.id }
}

export const test = base.extend<{ authedPage: Page }>({
  // A page that is already signed in, for tests that are not about login.
  // The second parameter is named `provide` rather than Playwright's usual
  // `use`, which lint reads as React's `use` hook.
  authedPage: async ({ page }, provide) => {
    await signIn(page)
    await provide(page)
  },
})

export { expect }
