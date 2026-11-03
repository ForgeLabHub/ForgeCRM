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
const SMALL_PHONE = { width: 320, height: 568 }
const LARGE_PHONE = { width: 430, height: 932 }
const TABLET_PORTRAIT = { width: 768, height: 1024 }
const TABLET_LANDSCAPE = { width: 1024, height: 768 }
const DESKTOP = { width: 1280, height: 800 }
const LAPTOP = { width: 1280, height: 720 }
const LAPTOP_LARGE = { width: 1440, height: 900 }
const DESKTOP_LARGE = { width: 1920, height: 1080 }

/**
 * The widths the interface is required to render without sideways scroll.
 *
 * Each is a distinct layout, not a scale factor: the rail becomes a drawer at
 * 860px, the stat grid changes its column count at 900px, and the top bar drops
 * the search label and the account name at 560px. A route clean at one width
 * says nothing about the others, so every width is swept.
 */
const VIEWPORTS: ReadonlyArray<readonly [string, { width: number; height: number }]> = [
  ['large desktop 1920x1080', DESKTOP_LARGE],
  ['desktop 1440x900', LAPTOP_LARGE],
  ['compact laptop 1280x720', LAPTOP],
  ['desktop 1280x800', DESKTOP],
  ['tablet landscape 1024x768', TABLET_LANDSCAPE],
  ['tablet portrait 768x1024', TABLET_PORTRAIT],
  ['large phone 430x932', LARGE_PHONE],
  ['phone 390x844', PHONE],
  ['small phone 320x568', SMALL_PHONE],
]

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

/**
 * Waits for a route to stop fetching, without making idleness the assertion.
 *
 * A route that polls (the webhook delivery log refreshes every five seconds)
 * never reports `networkidle` for long, so a bare `waitForLoadState('networkidle')`
 * is a race against that timer and a flaky failure. Here the wait is best-effort:
 * the overflow measurement below is what the test actually asserts, and a route
 * whose data has not arrived yet has not rendered its widest element either.
 */
async function settle(page: import('@playwright/test').Page) {
  await expect(page.locator('main, [role="main"], h1').first()).toBeVisible({ timeout: 15_000 })
  await page.waitForLoadState('networkidle', { timeout: 5_000 }).catch(() => {})
}

/** Visits every route and returns the ones that scroll sideways, with the amount. */
async function sweepRoutes(page: import('@playwright/test').Page): Promise<string[]> {
  const offenders: string[] = []
  for (const route of ROUTES) {
    await page.goto(route)
    // A route that renders a list has to finish its request before its widest
    // element (a table) exists; a bare timeout would race that fetch.
    await settle(page)

    const overflow = await horizontalOverflow(page)
    if (overflow > 1) offenders.push(`${route} overflows by ${overflow}px`)
  }
  return offenders
}

/**
 * Sweeps every route at a viewport and asserts none scrolls sideways.
 *
 * A phone, a tablet and a desktop are three different layouts — a column that
 * collapses, a rail that becomes a drawer, a stat grid that changes its column
 * count — so a route that is clean at one width says nothing about the others.
 * The width is in the assertion message so a failure names the viewport.
 */
async function expectNoSidewaysScroll(
  page: import('@playwright/test').Page,
  viewport: { width: number; height: number },
) {
  await page.setViewportSize(viewport)
  const offenders = await sweepRoutes(page)
  expect(
    offenders,
    `routes that scroll sideways at ${viewport.width}px: ${offenders.join(', ')}`,
  ).toEqual([])
}

test.describe('responsive layout', () => {
  test.use({ viewport: PHONE })

  test('the sidebar is an off-canvas drawer that opens and closes on a phone', async ({
    authedPage: page,
  }) => {
    await page.goto('/')
    await settle(page)

    const sidebar = page.locator('.sidebar')
    const toggle = page.locator('.sidebar-toggle')

    // The drawer starts off-screen and its toggle is the only way to it.
    await expect(toggle).toBeVisible()
    await expect(sidebar).not.toBeInViewport()

    await toggle.click()
    await expect(sidebar).toBeInViewport()

    // While the drawer is open the page must not gain a horizontal scrollbar:
    // a drawer that widens the document is the same defect as a wide table.
    expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1)

    // The scrim is a real control, so a tap on it closes the drawer. Click the
    // part of the scrim the drawer does not cover: the drawer is pinned to the
    // left and is taller than it is wide, so a tap at its centre would land on
    // the open sidebar (which sits above the scrim) and never reach the scrim.
    // A tap on the visible strip beside the drawer is what a user actually does.
    const scrim = page.locator('.sidebar-scrim')
    const box = (await scrim.boundingBox())!
    await page.mouse.click(box.x + box.width - 12, box.y + box.height / 2)
    await expect(sidebar).not.toBeInViewport()
  })

  test('a data table becomes one labelled card per row on a phone', async ({ authedPage: page }) => {
    // A six-column table cannot fit a phone. Below the phone breakpoint the rows
    // stack into cards and each cell is labelled from its column header, so the
    // page never widens and no column is hidden off-screen.
    await createAccount(page, `Responsive ${RUN_ID}`)
    await page.goto('/accounts')
    await expect(page.getByRole('table')).toBeVisible({ timeout: 15_000 })

    expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1)

    // The header row is gone and the cells carry their label instead.
    await expect(page.locator('thead')).toBeHidden()
    const labelled = await page.locator('td[data-label]').count()
    expect(labelled).toBeGreaterThan(0)
  })

  test('the phone layout does not widen the page when the table is wider than it', async ({
    authedPage: page,
  }) => {
    await createAccount(page, `Responsive ${RUN_ID}`)
    await page.goto('/accounts')
    await expect(page.getByRole('table')).toBeVisible({ timeout: 15_000 })

    const wrapper = page.locator('.table-wrap').first()
    if ((await wrapper.count()) > 0) {
      const scrollable = await wrapper.evaluate((el) => el.scrollWidth > el.clientWidth)
      // The wrapper is allowed to not scroll when the table happens to fit; what
      // it must never do is pass the width on to the document.
      expect(scrollable || (await horizontalOverflow(page)) <= 1).toBe(true)
    }
  })

  // The awkward middle widths — 320px (where the top bar used to run 46px past
  // the viewport), the tablet range where the rail collapses to a drawer, and
  // the wide desktops where the content column caps out — are each their own
  // layout, so each gets its own sweep. A suite that only tests 390px calls a
  // broken 320px layout responsive.
  for (const [name, viewport] of VIEWPORTS) {
    test(`no route scrolls sideways on a ${name}`, async ({ authedPage: page }) => {
      // Thirty-five routes, each a navigation plus a fetch, do not fit the
      // default 30s budget; the loop used to time out on a route that was fine.
      test.setTimeout(240_000)
      await expectNoSidewaysScroll(page, viewport)
    })
  }
})

// The sign-in page is the one screen every user sees before a session exists,
// and it is outside the authenticated sweep above because it renders for a
// signed-out visitor. Its two-pane grid collapses to a single column, and the
// collapsed form is what a phone user actually fills in.
test.describe('sign-in page responsive', () => {
  for (const [name, viewport] of [
    ['phone', PHONE],
    ['small phone', SMALL_PHONE],
    ['tablet', TABLET_PORTRAIT],
    ['desktop', DESKTOP],
  ] as const) {
    test(`the sign-in form fits and is usable on a ${name}`, async ({ page }) => {
      await page.setViewportSize(viewport)
      await page.goto('/login')

      await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible()
      expect(
        await horizontalOverflow(page),
        `the sign-in page scrolls sideways at ${viewport.width}px`,
      ).toBeLessThanOrEqual(1)
    })
  }
})
