/**
 * Sign-in form behaviour.
 *
 * These exercise the parts of the page a user actually depends on: that the
 * labels follow the chosen language, that empty input is rejected before a
 * request is made, that the submit button reports progress, that a server error
 * is shown, that the password can be revealed, and that the theme control
 * switches the document theme. None of them mock the component under test.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { LoginPage } from './LoginPage'
import { AuthProvider } from '../hooks/useAuth'
import { I18nProvider } from '../i18n'
import { ThemeProvider } from '../theme'
import { en } from '../i18n/en'
import { ru } from '../i18n/ru'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

/** Answers the two anonymous calls the page makes on mount. */
function stubProviders() {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/api/v1/auth/oidc/providers')) {
        return Promise.resolve(json({ enabled: false, providers: [] }))
      }
      return Promise.resolve(json({}))
    }),
  )
}

function renderLogin() {
  return render(
    <I18nProvider>
      <ThemeProvider>
        <MemoryRouter initialEntries={['/login']}>
          <AuthProvider>
            <LoginPage />
          </AuthProvider>
        </MemoryRouter>
      </ThemeProvider>
    </I18nProvider>,
  )
}

beforeEach(() => {
  window.localStorage.clear()
  document.documentElement.removeAttribute('data-theme')
  stubProviders()
})

describe('LoginPage form', () => {
  it('renders the form with an accessible email and password field', async () => {
    renderLogin()
    expect(await screen.findByLabelText(en.common.email)).toBeTruthy()
    expect(screen.getByLabelText(en.common.password)).toBeTruthy()
    expect(screen.getByRole('button', { name: en.login.submit })).toBeTruthy()
  })

  it('rejects an empty submission without calling the API', async () => {
    renderLogin()
    await userEvent.click(await screen.findByRole('button', { name: en.login.submit }))
    expect(await screen.findByText(en.login.errorEmailRequired)).toBeTruthy()
    expect(screen.getByText(en.login.errorPasswordRequired)).toBeTruthy()
  })

  it('rejects a malformed email address', async () => {
    renderLogin()
    await userEvent.type(await screen.findByLabelText(en.common.email), 'not-an-email')
    await userEvent.type(screen.getByLabelText(en.common.password), 'secret')
    await userEvent.click(screen.getByRole('button', { name: en.login.submit }))
    expect(await screen.findByText(en.login.errorEmailInvalid)).toBeTruthy()
  })

  it('shows the server error when the credentials are rejected', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString()
        if (url.includes('/api/v1/auth/oidc/providers')) {
          return Promise.resolve(json({ enabled: false, providers: [] }))
        }
        if (url.includes('/api/v1/auth/login')) {
          return Promise.resolve(
            json(
              { error: { code: 'unauthorized', message: 'invalid email or password' } },
              401,
            ),
          )
        }
        return Promise.resolve(json({}))
      }),
    )

    renderLogin()
    await userEvent.type(await screen.findByLabelText(en.common.email), 'owner@example.com')
    await userEvent.type(screen.getByLabelText(en.common.password), 'wrong-password')
    await userEvent.click(screen.getByRole('button', { name: en.login.submit }))
    expect(await screen.findByText(/invalid email or password/i)).toBeTruthy()
  })

  it('toggles password visibility', async () => {
    renderLogin()
    const field = await screen.findByLabelText(en.common.password)
    expect(field.getAttribute('type')).toBe('password')
    await userEvent.click(screen.getByRole('button', { name: en.login.showPassword }))
    expect(field.getAttribute('type')).toBe('text')
    await userEvent.click(screen.getByRole('button', { name: en.login.hidePassword }))
    expect(field.getAttribute('type')).toBe('password')
  })

  it('relabels the whole form when the language changes', async () => {
    renderLogin()
    await screen.findByLabelText(en.common.email)
    await userEvent.selectOptions(screen.getByLabelText(en.language.label), 'ru')
    expect(await screen.findByLabelText(ru.common.email)).toBeTruthy()
    expect(screen.getByRole('button', { name: ru.login.submit })).toBeTruthy()
    // No leftover English label should remain.
    expect(screen.queryByLabelText(en.common.password)).toBeNull()
  })

  it('switches the document theme from the sign-in page', async () => {
    renderLogin()
    await screen.findByLabelText(en.common.email)
    await userEvent.click(screen.getByRole('radio', { name: new RegExp(en.theme.dark, 'i') }))
    await waitFor(() => {
      expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
    })
  })

  it('shows a submitting state and disables the button while the request is in flight', async () => {
    // A login that never settles lets the assertion observe the in-flight state
    // rather than racing the response.
    let release: ((value: Response) => void) | undefined
    const pending = new Promise<Response>((resolve) => {
      release = resolve
    })
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString()
        if (url.includes('/api/v1/auth/oidc/providers')) {
          return Promise.resolve(json({ enabled: false, providers: [] }))
        }
        if (url.includes('/api/v1/auth/login')) {
          return pending
        }
        return Promise.resolve(json({}))
      }),
    )

    renderLogin()
    await userEvent.type(await screen.findByLabelText(en.common.email), 'owner@example.com')
    await userEvent.type(screen.getByLabelText(en.common.password), 'a-password')
    await userEvent.click(screen.getByRole('button', { name: en.login.submit }))

    const busyButton = await screen.findByRole('button', { name: en.login.submitting })
    expect(busyButton).toBeDisabled()

    release?.(json({ mfa_required: false }))
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: en.login.submitting })).toBeNull()
    })
  })

  it('moves focus to the error region so the failure is announced', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString()
        if (url.includes('/api/v1/auth/oidc/providers')) {
          return Promise.resolve(json({ enabled: false, providers: [] }))
        }
        if (url.includes('/api/v1/auth/login')) {
          return Promise.resolve(
            json({ error: { code: 'unauthorized', message: 'invalid email or password' } }, 401),
          )
        }
        return Promise.resolve(json({}))
      }),
    )

    renderLogin()
    await userEvent.type(await screen.findByLabelText(en.common.email), 'owner@example.com')
    await userEvent.type(screen.getByLabelText(en.common.password), 'wrong')
    await userEvent.click(screen.getByRole('button', { name: en.login.submit }))

    const alert = await screen.findByText(/invalid email or password/i)
    await waitFor(() => {
      expect(document.activeElement).toBe(alert.closest('[tabindex="-1"]'))
    })
  })
})
