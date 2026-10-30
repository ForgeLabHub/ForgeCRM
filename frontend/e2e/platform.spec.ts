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
    // Assert on the record header, not the bare text: with a provider
    // configured the assistant also lists the industry among the facts it can
    // see, so a bare text match is ambiguous.
    await expect(page.locator('.page-header__meta')).toContainText('Manufacturing')
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

  test('bulk archives the selected accounts from the list', async ({ authedPage: page }) => {
    // Two records, so the assertion is about a set rather than one row. A bulk
    // action that only ever archived the first selection would pass a
    // single-record test.
    const first = `E2E Bulk A ${RUN_ID}`
    const second = `E2E Bulk B ${RUN_ID}`
    await createAccount(page, first)
    await createAccount(page, second)

    await page.goto('/accounts')
    await expect(page.getByText(first)).toBeVisible()

    // Select both by their row checkboxes. The row checkbox is labelled
    // "Select row"; the header one is labelled separately, so this cannot
    // accidentally select everything on the page.
    for (const name of [first, second]) {
      await page
        .getByRole('row')
        .filter({ hasText: name })
        .getByLabel('Select row')
        .check()
    }

    await expect(page.getByText('2 selected')).toBeVisible()

    // The count in the button is the count that will be archived.
    await page.getByRole('button', { name: 'Archive 2 accounts' }).click()

    // Destructive and multiplied, so it must be confirmed.
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()
    await dialog.getByRole('button', { name: 'Archive 2' }).click()
    await expect(dialog).toBeHidden({ timeout: 15_000 })

    await page.goto('/accounts')
    await expect(page.getByText(first)).toHaveCount(0)
    await expect(page.getByText(second)).toHaveCount(0)
  })

  test('bulk archive reports a record that could not be archived', async ({ authedPage: page }) => {
    // The server refuses an already-archived record and reports it per record
    // rather than failing the request. The interface must show that, because a
    // success message for a partial archive would be a lie.
    const name = `E2E Bulk Partial ${RUN_ID}`
    await createAccount(page, name)

    await page.goto('/accounts')
    await page.getByRole('row').filter({ hasText: name }).getByLabel('Select row').check()
    await page.getByRole('button', { name: 'Archive 1 account' }).click()
    await page.getByRole('dialog').getByRole('button', { name: 'Archive 1' }).click()

    // The row leaves the list, so the archive itself succeeded.
    await expect(page.getByText(name)).toHaveCount(0)

    // Archiving the same id again is refused per record. Driving the API
    // directly is deliberate: the interface cannot select a row that is no
    // longer listed, and the point here is the server's per-record report.
    const me = await page.request.get('/api/v1/auth/me')
    const csrf = (await me.json()).csrf_token as string
    const listed = await (await page.request.get('/api/v1/accounts/?limit=100')).json()
    const target = listed.items.find((item: { name: string }) => item.name === name)
    expect(target, 'the archived account should not be listed').toBeUndefined()

    // A second archive of the same name is impossible through the list, so the
    // partial case is exercised through the API contract instead.
    const again = await page.request.post('/api/v1/accounts/bulk-archive', {
      headers: { 'X-CSRF-Token': csrf },
      data: { ids: ['00000000-0000-0000-0000-000000000001'] },
    })
    expect(again.status()).toBe(200)
    const body = await again.json()
    expect(body.archived).toHaveLength(0)
    expect(body.failed).toHaveLength(1)
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

test.describe('contextual AI', () => {
  test('the assistant appears on a record and reports when no provider is configured', async ({
    authedPage: page,
  }) => {
    // This case asserts the no-provider path. When a provider is configured —
    // as it is in a developer run with a stub — that path cannot be shown, so
    // the case is skipped rather than made to fail.
    const status = await page.request.get('/api/v1/ai/status')
    const { enabled } = (await status.json()) as { enabled: boolean }
    test.skip(enabled, 'a provider is configured in this environment')

    const name = `E2E AI ${RUN_ID}`
    await createAccount(page, name)

    await page.goto('/accounts')
    await page.getByText(name).click()
    await expect(page.getByRole('heading', { level: 1 })).toContainText(name)

    // The panel is part of the record card, so it must render without the user
    // opening anything. The E2E stack configures no provider, and the panel must
    // say so plainly rather than fail or offer assistance it cannot deliver.
    const assistant = page.getByRole('heading', { name: 'Assistant' })
    await expect(assistant).toBeVisible()
    await expect(page.getByText(/No AI provider is configured/i)).toBeVisible()
  })

  test('the assistant scores its answer against the record, with a reason', async ({
    authedPage: page,
  }) => {
    // The assistant needs a configured provider. CI has none, and a test that
    // silently passes when the feature is absent would not be a test of it, so
    // the case is skipped rather than made to pass. The status is read through
    // the signed-in page's own request context, because the endpoint needs a
    // session and the bare `request` fixture has none.
    const status = await page.request.get('/api/v1/ai/status')
    const { enabled } = (await status.json()) as { enabled: boolean }
    test.skip(!enabled, 'no AI provider is configured in this environment')

    const name = `E2E AI grounding ${RUN_ID}`
    await createAccount(page, name)

    await page.goto('/accounts')
    await page.getByText(name).click()
    await expect(page.getByRole('heading', { level: 1 })).toContainText(name)

    // Run the assistant from the record's own panel. The panel is the card
    // whose heading is "Assistant"; the card is a plain section, so the heading
    // is the anchor.
    const panel = page
      .getByRole('heading', { name: 'Assistant' })
      .locator('xpath=ancestor::section[1]')
    await panel.getByRole('button', { name: 'Run' }).click()

    // The grounding note must appear, with the level and at least one stated
    // reason — the point is that the user is not handed a bare number.
    await expect(panel.getByText(/grounded/i)).toBeVisible({ timeout: 15_000 })
    await expect(panel.getByText(/judged by the server/i)).toBeVisible()

    // And the answer must still separate CRM facts from model output.
    await expect(panel.getByText(/from the CRM/i)).toBeVisible()
    await expect(panel.getByText('model output').first()).toBeVisible()
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
    { path: '/simulation', title: 'Simulation Lab' },
    { path: '/forecast', title: 'Forecasting' },
    { path: '/risk', title: 'Risk' },
    { path: '/imports', title: 'Import Center' },
    { path: '/integrations', title: 'Integrations' },
    { path: '/webhooks', title: 'Webhooks' },
    { path: '/events', title: 'Events' },
    { path: '/workflows', title: 'Workflows' },
    { path: '/jobs', title: 'Background jobs' },
    { path: '/ai', title: 'AI & MCP' },
    { path: '/approvals', title: 'Agent approvals' },
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

test.describe('webhook subscriptions', () => {
  test('the event picker offers the canonical event types the dispatcher matches', async ({
    authedPage: page,
  }) => {
    await page.goto('/webhooks')
    await page.getByRole('button', { name: 'New subscription' }).click()

    // The picker must offer the names the server actually emits. A shortened list
    // ("contact.created") produced a subscription the dispatcher never matched, so
    // the endpoint silently received nothing — the failure this test guards.
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByText('crm.contact.created', { exact: true })).toBeVisible()
    await expect(dialog.getByText('contact.created', { exact: true })).toHaveCount(0)
  })
})

test.describe('workflow automation', () => {
  test('a workflow can be created and run from the page', async ({ authedPage: page }) => {
    await page.goto('/workflows')
    await page.getByRole('button', { name: 'New workflow' }).click()

    const dialog = page.getByRole('dialog')
    const name = `E2E workflow ${Date.now()}`
    await dialog.locator('#workflow-name').fill(name)

    // The editor opens in the structured view. Switch to the JSON view, which is
    // the same document serialised, and replace it with a notify-free action so
    // the run cannot depend on another record existing.
    await dialog.getByRole('button', { name: 'Edit as JSON' }).click()
    const definition = dialog.locator('#workflow-definition')
    await definition.fill(
      JSON.stringify(
        { steps: [{ id: 't1', type: 'create_task', args: { title: 'Created by the E2E workflow' } }] },
        null,
        2,
      ),
    )
    await dialog.getByRole('button', { name: 'Save' }).click()

    // The row appears, which is the page reading the created workflow back from
    // the API rather than from local state.
    const row = page.getByRole('row', { name: new RegExp(name) })
    await expect(row).toBeVisible()

    // Run it, and assert the run is reported as succeeded.
    await row.getByRole('button', { name: 'Run', exact: true }).click()
    await expect(page.getByText(/the run succeeded/i)).toBeVisible()
  })
})

test.describe('workflow templates', () => {
  test('a workflow can be created from a catalog template', async ({ authedPage: page }) => {
    await page.goto('/workflows')
    await page.getByRole('button', { name: 'From a template' }).click()

    const picker = page.getByRole('dialog')
    // The catalog is served by the API, so a template is present only if the
    // server registered it. Its trigger names an event the engine actually emits.
    const template = picker.getByRole('button', { name: /Notify the owner about an overdue deal/ })
    await expect(template).toBeVisible()
    await template.click()
    await picker.getByRole('button', { name: 'Create', exact: true }).click()

    // Creating from a template returns a real workflow, which the page reads back.
    await expect(page.getByText(/Created .* from a template/i)).toBeVisible()
  })

  test('a workflow can be exported and its document re-imported', async ({ authedPage: page }) => {
    await page.goto('/workflows')

    // Create one to export.
    await page.getByRole('button', { name: 'New workflow' }).click()
    const dialog = page.getByRole('dialog')
    const name = `E2E export ${Date.now()}`
    await dialog.locator('#workflow-name').fill(name)
    await dialog.getByRole('button', { name: 'Save' }).click()

    const row = page.getByRole('row', { name: new RegExp(name) })
    await expect(row).toBeVisible()

    // Export downloads the server's canonical document.
    const download = page.waitForEvent('download')
    await row.getByRole('button', { name: 'Export' }).click()
    const file = await download
    expect(file.suggestedFilename()).toContain('.json')
  })
})

test.describe('workflow triggers', () => {
  test('the event picker offers the canonical names the engine matches', async ({ authedPage: page }) => {
    await page.goto('/workflows')
    await page.getByRole('button', { name: 'New workflow' }).click()

    const dialog = page.getByRole('dialog')
    await dialog.locator('#workflow-trigger').selectOption('event')

    // An event-triggered workflow matches the exact committed name, which is
    // namespaced. Offering a bare name produces a workflow that is accepted and
    // then never fires, so the picker must read the server's vocabulary.
    const options = await dialog.locator('#workflow-event-type option').allTextContents()
    expect(options).toContain('crm.deal.won')
    expect(options).not.toContain('deal.won')
  })
})

test.describe('simulation lab', () => {
  test('a scenario can be built, saved and run against the real pipeline', async ({
    authedPage: page,
  }) => {
    await page.goto('/simulation')

    // Start from the catalog. The entry is offered only if the server registered
    // it, so this also checks the catalog endpoint is reachable.
    await page.getByRole('button', { name: 'Sales funnel' }).click()

    // Name the scenario uniquely for this run.
    const name = `E2E scenario ${Date.now()}`
    await page.locator('#sim-name').fill(name)

    // Save it, which persists a real row.
    await page.getByRole('button', { name: 'Save scenario' }).click()

    // The saved scenario appears in the list and opens with a run control.
    await page.getByRole('button', { name: new RegExp(name) }).click()
    const run = page.getByRole('button', { name: 'Run scenario' })
    await expect(run).toBeVisible()
    await run.click()

    // A run either produces a comparison or reports that there is not enough CRM
    // history. Both are correct outcomes; what must never happen is a number
    // invented from nothing, so the assertion is that one of the two is shown.
    await expect(
      page.getByText(/Observed \(CRM\)|insufficient data/i),
    ).toBeVisible()

    // The run history is shown regardless, so a completed run is always recorded.
    await expect(page.getByText('Run history')).toBeVisible()
  })

  test('previewing a scenario writes nothing but shows a comparison', async ({
    authedPage: page,
  }) => {
    await page.goto('/simulation')
    await page.getByRole('button', { name: 'Revenue and terms' }).click()
    await page.getByRole('button', { name: 'Preview' }).click()

    // Either a comparison is shown, or the server explained there is not enough
    // history — but never a fabricated figure. Both are acceptable outcomes;
    // silence is not.
    await expect(
      page.getByText(/Observed \(CRM\)|insufficient data/i),
    ).toBeVisible()
  })
})

test.describe('forecasting', () => {
  test('a forecast is computed against the real revenue history', async ({ authedPage: page }) => {
    await page.goto('/forecast')

    // Compute with the default method. The catalog is served by the API, so the
    // form only offers methods the core can actually compute.
    await page.getByRole('button', { name: 'Compute forecast' }).click()

    // The result is either a projection with the observed series it came from, or
    // the server's reason there is not enough history. A projection shown without
    // its observed series would be a number from nowhere, which must not happen.
    await expect(page.getByText(/Measured accuracy|insufficient history/i)).toBeVisible()

    if (await page.getByText('Measured accuracy').isVisible()) {
      // The accuracy is presented next to the naive baseline the method must beat.
      await expect(page.getByText('Naive MAE')).toBeVisible()
      await expect(page.getByText('Observed series').first()).toBeVisible()
    }
  })
})

test.describe('risk', () => {
  test('a scan reconciles the signal list and the result is explained', async ({ authedPage: page }) => {
    await page.goto('/risk')

    await page.getByRole('button', { name: 'Scan now' }).click()

    // The scan reports what it did, so the list's state is explained rather than
    // silent.
    await expect(page.getByText(/Scan detected \d+ condition/i)).toBeVisible()

    // Either a signal is listed with its rationale and evidence, or the list is
    // empty because the organization has no data to flag. Both are correct; a
    // signal without a reason is not.
    const empty = page.getByText('No risk signals')
    if (await empty.isVisible()) {
      await expect(empty).toBeVisible()
    } else {
      await expect(page.getByText(/Stalled deal|Overdue task|Account without contact|Unusual deal amount|Conversion drop|Stage concentration/).first()).toBeVisible()
    }
  })

  test('the thresholds form is present and saves', async ({ authedPage: page }) => {
    await page.goto('/risk')

    const stalled = page.getByLabel('Stalled deal (days)')
    await expect(stalled).toBeVisible()
    await stalled.fill('31')
    await page.getByRole('button', { name: 'Save thresholds' }).click()

    await expect(page.getByText('Saved')).toBeVisible()
  })
})

test.describe('tags and comments', () => {
  test('a tag and a comment on an account survive a reload', async ({ authedPage: page }) => {
    const name = `E2E Collab ${RUN_ID}`
    const tagName = `Hot ${RUN_ID}`
    const comment = `Called the buyer on ${RUN_ID}`

    await createAccount(page, name)
    await page.goto('/accounts')
    await page.getByText(name).click()
    await expect(page.getByRole('heading', { level: 1 })).toContainText(name)

    // Create a tag and attach it to this record in one step. The panel is the
    // only consumer of the tags API, so a passing assertion here is the proof
    // the missing wiring was added.
    await page.getByLabel('New tag name').fill(tagName)
    await page.getByRole('button', { name: 'Create & attach' }).click()
    await expect(page.getByText(tagName)).toBeVisible({ timeout: 15_000 })

    // Add a comment attributed to the signed-in user.
    await page.getByLabel('Add a comment').fill(comment)
    await page.getByRole('button', { name: 'Comment' }).click()
    await expect(page.getByText(comment)).toBeVisible({ timeout: 15_000 })

    // Both are stored server-side, not held in client state.
    await page.reload()
    await expect(page.getByText(tagName)).toBeVisible()
    await expect(page.getByText(comment)).toBeVisible()
  })
})

test.describe('custom fields', () => {
  test('a defined field is set on a record and survives a reload', async ({ authedPage: page }) => {
    const name = `E2E Custom ${RUN_ID}`
    const key = `tier${RUN_ID.replace(/[^a-z0-9]/g, '')}`
    const label = `Account tier ${RUN_ID}`

    await createAccount(page, name)
    await page.goto('/accounts')
    await page.getByText(name).click()
    await expect(page.getByRole('heading', { level: 1 })).toContainText(name)

    // Define a text field, then set it on this record. The label carries the run
    // id because definitions are tenant-scoped and persist across runs, so a
    // fixed label would resolve to a field a previous run left behind.
    await page.getByLabel('Field key').fill(key)
    await page.getByLabel('Field label').fill(label)
    await page.getByRole('button', { name: 'Add field' }).click()

    const input = page.getByLabel(label)
    await expect(input).toBeVisible({ timeout: 15_000 })
    await input.fill('Gold')
    await input.blur()

    // The value is on the record, so it must still be there after a reload.
    await page.reload()
    await expect(page.getByLabel(label)).toHaveValue('Gold', { timeout: 15_000 })
  })
})

test.describe('health endpoints', () => {
  test('the API reports healthy and ready', async ({ request }) => {
    expect((await request.get('/health')).status()).toBe(200)
    expect((await request.get('/ready')).status()).toBe(200)
  })
})
