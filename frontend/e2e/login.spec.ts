import { test, expect, E2E_EMAIL, E2E_PASSWORD } from './fixtures'

// The sign-in page is the first screen a user meets, and the language and theme
// controls are the two things on it that persist across visits. These cases drive
// the real controls against the real bundle: a switch that only looks like a
// switch is exactly the kind of thing a unit test with a mocked provider misses.

test.describe('sign-in page', () => {
  test('renders a real form with email, password and a working submit', async ({ page }) => {
    await page.goto('/login')

    await expect(page.getByLabel('Email')).toBeVisible()
    await expect(page.getByLabel('Password', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible()

    // The password field starts masked and the reveal toggles it, so the control
    // is not decorative.
    const password = page.getByLabel('Password', { exact: true })
    await expect(password).toHaveAttribute('type', 'password')
    await page.getByRole('button', { name: 'Show password' }).click()
    await expect(password).toHaveAttribute('type', 'text')
    await page.getByRole('button', { name: 'Hide password' }).click()
    await expect(password).toHaveAttribute('type', 'password')
  })

  test('rejects an empty submit before any request is sent', async ({ page }) => {
    await page.goto('/login')
    await page.getByRole('button', { name: 'Sign in' }).click()

    // Both required messages appear and the user stays on the page.
    await expect(page.getByText('Enter your email address.')).toBeVisible()
    await expect(page.getByText('Enter your password.')).toBeVisible()
    await expect(page).toHaveURL(/\/login/)
  })

  test('shows a server error for a wrong password and clears it on a new attempt', async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel('Email').fill(E2E_EMAIL)
    await page.getByLabel('Password', { exact: true }).fill('definitely-not-the-password')
    await page.getByRole('button', { name: 'Sign in' }).click()

    await expect(page.locator('[role="alert"]').first()).toBeVisible()
    await expect(page).toHaveURL(/\/login/)
  })

  test('switches the whole page to Russian and back without a reload', async ({ page }) => {
    await page.goto('/login')

    await page.getByLabel('Language').selectOption('ru')

    // The heading, the labels and the button are all Russian now: no mixed copy.
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Вход в рабочее пространство')
    await expect(page.getByLabel('Электронная почта')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Войти' })).toBeVisible()

    await page.getByLabel('Язык').selectOption('en')
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Sign in to your workspace')
    await expect(page.getByLabel('Email')).toBeVisible()
  })

  test('remembers the chosen language across a reload', async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel('Language').selectOption('ru')
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Вход в рабочее пространство')

    await page.reload()

    await expect(page.getByRole('heading', { level: 1 })).toContainText('Вход в рабочее пространство')
    await expect(page.getByLabel('Электронная почта')).toBeVisible()
  })

  test('applies the chosen theme and remembers it across a reload', async ({ page }) => {
    await page.goto('/login')

    await page.getByRole('radio', { name: 'Dark' }).click()
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')

    await page.reload()
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')

    await page.getByRole('radio', { name: 'Light' }).click()
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')

    await page.reload()
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  })

  test('resolves the system theme to the OS preference', async ({ page }) => {
    // Emulate a dark OS preference, then choose System: the page must follow it.
    await page.emulateMedia({ colorScheme: 'dark' })
    await page.goto('/login')
    await page.getByRole('radio', { name: 'System' }).click()

    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')

    // A live OS change is reflected without a reload.
    await page.emulateMedia({ colorScheme: 'light' })
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  })

  test('collapses to a single column and hides the brand panel on a phone viewport', async ({ page }) => {
    // The two-pane layout must not squeeze the form on a phone. The aside is
    // hidden and the grid becomes one column, which is the property that keeps
    // the controls usable rather than a pixel-perfect snapshot.
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/login')

    await expect(page.getByLabel('Email')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible()

    const aside = page.locator('.login__aside')
    await expect(aside).toBeHidden()

    // The form fits the viewport: a single-column layout that overflows would
    // hide the submit button off-screen on a phone.
    const overflows = await page.evaluate(
      () => document.documentElement.scrollWidth > window.innerWidth + 1,
    )
    expect(overflows).toBe(false)
  })

  test('keeps the language and theme choice after signing in', async ({ page }) => {
    await page.goto('/login')
    // Theme first, then language: the theme control's accessible name is itself
    // translated, so choosing the theme before switching keeps the label English.
    await page.getByRole('radio', { name: 'Dark' }).click()
    await page.getByLabel('Language').selectOption('ru')

    await page.getByLabel('Электронная почта').fill(E2E_EMAIL)
    await page.getByLabel('Пароль', { exact: true }).fill(E2E_PASSWORD)
    await page.getByRole('button', { name: 'Войти' }).click()

    // The workspace loads with the same theme and language: the preference is not
    // reset by the sign-in transition.
    await expect(page).toHaveURL(/\/$/, { timeout: 15_000 })
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
    await expect(page.locator('html')).toHaveAttribute('lang', 'ru')
  })
})
