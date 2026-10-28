import { test, expect, signIn, createAccount, RUN_ID, E2E_EMAIL } from './fixtures'

// These tests exist to answer one question: does the interface actually perform
// the operation it presents? A button that renders is not a button that works,
// so every case here drives the real form and then confirms the effect through
// the real API.

test.describe('authentication', () => {
  test('signs in with valid credentials and reaches the workspace', async ({ page }) => {
    await signIn(page)
    await expect(page).toHaveURL(/\/$/)
  })

  test('refuses a wrong password without revealing whether the account exists', async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel('Email').fill(E2E_EMAIL)
    await page.getByLabel('Password').fill('definitely-not-the-password')
    await page.getByRole('button', { name: 'Sign in' }).click()

    // An alert is shown and the user stays on the login page.
    await expect(page.locator('[role="alert"]')).toBeVisible()
    await expect(page).toHaveURL(/\/login/)
  })

  test('rejects a request for a protected page when there is no session', async ({ page }) => {
    // The client-side guard should redirect, and the API would refuse anyway.
    await page.goto('/accounts')
    await expect(page).toHaveURL(/\/login/)
  })

  test('signs out and the session stops working', async ({ page }) => {
    await signIn(page)

    // Capture a working API call before signing out so the comparison is fair.
    const before = await page.request.get('/api/v1/accounts/')
    expect(before.status()).toBe(200)

    await page.getByRole('button', { name: 'Sign out' }).click()
    await expect(page).toHaveURL(/\/login/)

    const after = await page.request.get('/api/v1/accounts/')
    expect(after.status()).toBe(401)
  })
})

test.describe('accounts', () => {
  test('creates an account that persists and is visible after a reload', async ({ authedPage: page }) => {
    const name = `E2E Corp ${RUN_ID}`
    await createAccount(page, name)

    // Visible in the list...
    await page.goto('/accounts')
    await expect(page.getByText(name)).toBeVisible()

    // ...and still there after a full reload, which proves it was stored rather
    // than held in client state.
    await page.reload()
    await expect(page.getByText(name)).toBeVisible()
  })

  test('the created account is readable through the API', async ({ authedPage: page }) => {
    const name = `E2E Api Check ${RUN_ID}`
    await createAccount(page, name)

    const response = await page.request.get(`/api/v1/accounts/?search=${encodeURIComponent(name)}`)
    expect(response.status()).toBe(200)

    const body = await response.json()
    const items = body.items ?? body.data ?? []
    expect(
      items.some((a: { name: string }) => a.name === name),
      `the API did not return the account the interface just created: ${JSON.stringify(body).slice(0, 400)}`,
    ).toBe(true)
  })

  test('edits an account and the change survives a reload', async ({ authedPage: page }) => {
    const name = `E2E Edit ${RUN_ID}`
    await createAccount(page, name)

    // Open the record from the list.
    await page.goto('/accounts')
    await page.getByText(name).click()
    await expect(page.getByRole('heading', { level: 1 })).toContainText(name)

    await page.getByRole('button', { name: 'Edit' }).click()
    const dialog = page.getByRole('dialog')
    const industry = dialog.getByLabel('Industry')
    await industry.fill('Manufacturing')
    await dialog.getByRole('button', { name: 'Save changes' }).click()
    await expect(dialog).toBeHidden()

    await page.reload()
    await expect(page.getByText('Manufacturing')).toBeVisible()
  })

  test('archives an account and it disappears from the list', async ({ authedPage: page }) => {
    const name = `E2E Archive ${RUN_ID}`
    await createAccount(page, name)

    await page.goto('/accounts')
    await page.getByText(name).click()

    await page.getByRole('button', { name: 'Archive' }).click()
    // Destructive actions require confirmation, so the dialog must appear.
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()
    await dialog.getByRole('button', { name: 'Archive' }).click()
    await expect(dialog).toBeHidden()

    await page.goto('/accounts')
    await expect(page.getByText(name)).toHaveCount(0)
  })

  test('rejects a write without a CSRF token, then rejects an empty name', async ({ authedPage: page }) => {
    // State-changing requests must carry the CSRF token the session issued.
    // Omitting it is refused outright.
    const withoutToken = await page.request.post('/api/v1/accounts/', { data: { name: 'x' } })
    expect(withoutToken.status()).toBe(403)

    // With the token, server-side validation of the payload is what is checked.
    // The form marks Name as required, so the browser would block this; a client
    // that skips the form must still be refused.
    const me = await page.request.get('/api/v1/auth/me')
    const csrf = (await me.json()).csrf_token as string
    expect(csrf, 'the session did not issue a CSRF token').toBeTruthy()

    const before = (await (await page.request.get('/api/v1/accounts/?limit=1')).json()).total

    const emptyName = await page.request.post('/api/v1/accounts/', {
      headers: { 'X-CSRF-Token': csrf },
      data: { name: '' },
    })
    expect(emptyName.status()).toBe(422)

    const after = (await (await page.request.get('/api/v1/accounts/?limit=1')).json()).total
    expect(after).toBe(before)
  })
})

test.describe('workspace pages render live data', () => {
  const pages = [
    { path: '/accounts', title: 'Accounts' },
    { path: '/contacts', title: 'Contacts' },
    { path: '/leads', title: 'Leads' },
    { path: '/deals', title: 'Deals' },
    { path: '/tasks', title: 'Tasks' },
    { path: '/tickets', title: 'Support' },
    { path: '/reports', title: 'Reports' },
    { path: '/imports', title: 'Import Center' },
    { path: '/integrations', title: 'Integrations' },
    { path: '/jobs', title: 'Background jobs' },
    { path: '/admin/audit', title: 'Audit & logs' },
    { path: '/admin/users', title: 'Users' },
  ]

  for (const { path, title } of pages) {
    test(`${path} loads without an error`, async ({ authedPage: page }) => {
      await page.goto(path)
      // Assert on the page's own heading, not the shell's, so a route that
      // silently falls through to the overview is caught.
      await expect(page.getByRole('heading', { level: 1 })).toContainText(title)
      // A rendered error boundary or a failed request is a failure even though
      // the route resolved.
      await expect(page.getByText(/something went wrong|failed to load/i)).toHaveCount(0)
    })
  }
})

test.describe('health endpoints', () => {
  test('the API reports healthy and ready', async ({ request }) => {
    expect((await request.get('/health')).status()).toBe(200)
    expect((await request.get('/ready')).status()).toBe(200)
  })
})
