import { test, expect, signIn, E2E_EMAIL, E2E_PASSWORD } from './fixtures'

// Real API, real PostgreSQL and two independent browser contexts; no mock.
test('appearance preview, account persistence and role-default reset', async ({ page, browser }) => {
  await signIn(page)
  const me = await (await page.request.get('/api/v1/auth/me')).json()
  const set = async (data: object) => {
    const response = await page.request.put('/api/v1/auth/appearance', {
      headers: { 'X-CSRF-Token': me.csrf_token }, data,
    })
    expect(response.ok()).toBeTruthy()
  }
  const defaults = { style: null, mode: 'system', accent: 'theme', density: 'standard' }
  await set(defaults)
  try {
    await page.goto('/app/appearance')
    await expect(page.getByRole('heading', { name: 'Appearance', exact: true })).toBeVisible()
    const before = await (await page.request.get('/api/v1/auth/appearance')).json()
    await expect(page.locator('html')).toHaveAttribute('data-style', before.default_style)
    const glass = page.locator('.style-card').filter({ has: page.getByText('Glass', { exact: true }) })
    await glass.getByRole('button', { name: 'Preview', exact: true }).click()
    await expect(page.locator('html')).toHaveAttribute('data-style', 'glass')
    expect((await (await page.request.get('/api/v1/auth/appearance')).json()).style).toBeNull()
    await glass.getByRole('button', { name: 'Apply', exact: true }).click()
    await expect.poll(async () => (await (await page.request.get('/api/v1/auth/appearance')).json()).style).toBe('glass')
    await expect(page.locator('html')).toHaveAttribute('data-style', 'glass')

    const other = await browser.newContext({ baseURL: 'http://127.0.0.1:5173', viewport: { width: 390, height: 844 } })
    try {
      const second = await other.newPage()
      await signIn(second, E2E_EMAIL, E2E_PASSWORD)
      await expect(second.locator('html')).toHaveAttribute('data-style', 'glass')
      await second.goto('/app/appearance')
      await expect(second.getByRole('button', { name: 'Reset to defaults' })).toBeVisible()
      expect(await second.evaluate(() => document.documentElement.scrollWidth - innerWidth)).toBeLessThanOrEqual(1)
      await second.getByRole('button', { name: 'Reset to defaults' }).click()
      await expect(second.getByText('Appearance reset to defaults.')).toBeVisible()
      await expect(second.locator('html')).toHaveAttribute('data-style', before.default_style)
      expect((await (await second.request.get('/api/v1/auth/appearance')).json()).style).toBeNull()
    } finally {
      await other.close()
    }
  } finally {
    await set(defaults)
  }
})
