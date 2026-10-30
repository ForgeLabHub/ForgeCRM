/**
 * Tests for the single sign-on controls on the sign-in page.
 *
 * The provider list is anonymous, so it must render without a session, and the
 * start control must be a full navigation to the server route rather than a fetch
 * — the browser has to leave the application for the provider's own page. When no
 * provider is configured the button must not appear, so a deployment without SSO
 * shows only the password form.
 */
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { LoginPage } from './LoginPage'
import { AuthProvider } from '../hooks/useAuth'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function renderLogin(initialEntry = '/login') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <AuthProvider>
        <LoginPage />
      </AuthProvider>
    </MemoryRouter>,
  )
}

describe('LoginPage single sign-on', () => {
  const originalLocation = window.location

  beforeEach(() => {
    vi.restoreAllMocks()
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', {
      configurable: true,
      writable: true,
      value: originalLocation,
    })
  })

  it('offers a button for each configured provider', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString()
        if (url.includes('/api/v1/auth/oidc/providers')) {
          return Promise.resolve(
            json({
              enabled: true,
              providers: [
                { id: 'p1', key: 'acme', display_name: 'Acme Identity', jit_provisioning: true },
              ],
            }),
          )
        }
        return Promise.resolve(json({}))
      }),
    )

    renderLogin()
    expect(await screen.findByRole('button', { name: /Continue with Acme Identity/i })).toBeTruthy()
  })

  it('navigates to the start route when the provider button is chosen', async () => {
    const assign = vi.fn()
    Object.defineProperty(window, 'location', {
      configurable: true,
      writable: true,
      value: { ...originalLocation, assign },
    })

    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString()
        if (url.includes('/api/v1/auth/oidc/providers')) {
          return Promise.resolve(
            json({
              enabled: true,
              providers: [
                { id: 'p1', key: 'acme', display_name: 'Acme Identity', jit_provisioning: true },
              ],
            }),
          )
        }
        return Promise.resolve(json({}))
      }),
    )

    renderLogin()
    const button = await screen.findByRole('button', { name: /Continue with Acme Identity/i })
    await userEvent.click(button)
    expect(assign).toHaveBeenCalledWith('/api/v1/auth/oidc/start?provider=acme')
  })

  it('shows no SSO control when no provider is configured', async () => {
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

    renderLogin()
    // Wait for the providers call to settle, then assert nothing appeared.
    await waitFor(() => {
      expect(screen.queryByText(/Continue with/i)).toBeNull()
    })
    expect(screen.getByRole('button', { name: /^Sign in$/i })).toBeTruthy()
  })

  it('explains a failed sign-in returned by the callback', async () => {
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

    renderLogin('/login?sso_error=no_account')
    expect(await screen.findByText(/no ForgeCRM account is linked/i)).toBeTruthy()
  })
})
