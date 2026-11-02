import { test, expect, RUN_ID, createAccount } from './fixtures'

// A page that scrolls sideways on a phone is broken in a way a desktop browser
// never shows: the submit button, the last table column or the card's action
// menu simply sits off-screen. The failure is easy to reintroduce — one wide
// table or one non-wrapping flex row is enough — so it is asserted rather than
// eyeballed.
//
// The check is `documentElement.scrollWidth > clientWidth`, the property a user
// feels, not a pixel snapshot of any particular layout.

const PHONE = { width: 390, height: 844 }

/** Every route that renders real content for the bootstrap owner. */
const ROUTES = [
  '/',
  '/accounts',
  '/contacts',
  '/leads',
  '/deals',
  '/tasks',
  '/tickets',
  '/reports',
  '/imports',
  '/migrations',
  '/integrations',
  '/webhooks',
  '/events',
  '/workflows',
  '/simulation',
  '/forecast',
  '/risk',
  '/jobs',
  '/ai',
  '/approvals',
  '/notifications',
  '/admin/users',
  '/admin/roles',
  '/admin/organizations',
  '/admin/teams',
  '/admin/audit',
  '/admin/consent',
  '/admin/security',
  '/admin/diagnostics',
  '/admin/email',
  '/admin/telegram',
  '/admin/sso',
  '/admin/mcp-credentials',
  '/admin/configuration',
  '/account',
] as const

async function horizontalOverflow(page: import('@playwright/test').Page): Promise<number> {
  return page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  )
}

test.describe('responsive layout', () => {
  test.use({ viewport: PHONE })

  test('no route scrolls sideways on a phone viewport', async ({ authedPage: page }) => {
    const offenders: string[] = []

    for (const route of ROUTES) {
      await page.goto(route)
      // A route that renders a list has to finish its request before its widest
      // element (a table) exists; a bare timeout would race that fetch.
      await expect(page.locator('main, [role="main"], h1').first()).toBeVisible({ timeout: 15_000 })
      await page.waitForLoadState('networkidle')

      const overflow = await horizontalOverflow(page)
      if (overflow > 1) offenders.push(`${route} overflows by ${overflow}px`)
    }

    expect(offenders, `routes that scroll sideways: ${offenders.join(', ')}`).toEqual([])
  })

  test('a data table scrolls inside its wrapper instead of widening the page', async ({ authedPage: page }) => {
    // A table is wider than a phone by nature. It must scroll in place, which
    // means the page itself stays put and the wrapper is the scroll container.
    await createAccount(page, `Responsive ${RUN_ID}`)
    await page.goto('/accounts')
    await expect(page.getByRole('table')).toBeVisible({ timeout: 15_000 })

    expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1)

    const wrapper = page.locator('.table-wrap').first()
    if ((await wrapper.count()) > 0) {
      const scrollable = await wrapper.evaluate((el) => el.scrollWidth > el.clientWidth)
      // The wrapper is allowed to not scroll when the table happens to fit; what
      // it must never do is pass the width on to the document.
      expect(scrollable || (await horizontalOverflow(page)) <= 1).toBe(true)
    }
  })
})
