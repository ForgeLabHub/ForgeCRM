/**
 * Tests for the single sign-on administration page.
 *
 * The provider CRUD endpoints existed on the server but nothing in the interface
 * called them, so an operator could not register a provider without a hand-built
 * request. These tests pin the two behaviours that matter: the list reports
 * whether a secret is stored without ever showing it, and an edit that leaves the
 * secret field blank sends no secret, so it cannot clear the stored one.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { SSOPage } from './SSOPage'
import { AuthProvider } from '../../hooks/useAuth'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const provider = {
  id: 'p1',
  key: 'acme',
  display_name: 'Acme SSO',
  issuer: 'https://idp.example.com/realms/acme',
  client_id: 'forgecrm',
  scopes: ['openid', 'profile', 'email'],
  allowed_email_domains: ['example.com'],
  jit_provisioning: false,
  enabled: true,
  has_secret: true,
}

function stubFetch(permissions: string[] = ['sso.read', 'sso.manage']) {
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    const method = init?.method ?? 'GET'
    if (url.includes('/api/v1/admin/sso/providers') && method === 'PUT') {
      return Promise.resolve(json(provider))
    }
    if (url.includes('/api/v1/admin/sso/providers')) {
      return Promise.resolve(
        json({ items: [provider], redirect_url: 'https://crm.example.com/api/v1/auth/oidc/callback' }),
      )
    }
    if (url.includes('/api/v1/roles')) {
      return Promise.resolve(json({ items: [{ id: 'r1', key: 'employee', name: 'Employee' }] }))
    }
    if (url.includes('/api/v1/auth/me')) {
      return Promise.resolve(
        json({
          user: { id: 'u1', email: 'a@example.com', display_name: 'A' },
          organization: { id: 'o1', name: 'Org' },
          permissions,
          csrf_token: 't',
        }),
      )
    }
    return Promise.resolve(json({ items: [] }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <SSOPage />
      </AuthProvider>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.unstubAllGlobals()
})

describe('SSOPage', () => {
  it('shows whether a secret is stored without showing the secret', async () => {
    stubFetch()
    renderPage()

    expect(await screen.findByText('Acme SSO')).toBeInTheDocument()
    // The secret is never returned by the API, so the page can only report its
    // presence. It must not offer an input pre-filled with a secret it does not have.
    expect(screen.getByText('stored')).toBeInTheDocument()
    expect(screen.getByText(/https:\/\/crm\.example\.com\/api\/v1\/auth\/oidc\/callback/)).toBeInTheDocument()
  })

  it('sends no secret when an edit leaves the field blank', async () => {
    const fetchMock = stubFetch()
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Edit' }))
    // Change only the display name; leave the secret blank.
    const name = screen.getByLabelText(/Display name/)
    await userEvent.clear(name)
    await userEvent.type(name, 'Acme SSO (renamed)')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      const put = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/api/v1/admin/sso/providers/p1') &&
          (init as RequestInit | undefined)?.method === 'PUT',
      )
      expect(put).toBeTruthy()
      const body = JSON.parse(String((put![1] as RequestInit).body))
      expect(body.display_name).toBe('Acme SSO (renamed)')
      // A blank secret must not be sent: sending "" would be indistinguishable
      // from clearing it, and the server keeps the stored one only when the field
      // is absent.
      expect('client_secret' in body).toBe(false)
    })
  })

  it('hides the management controls from a read-only operator', async () => {
    stubFetch(['sso.read'])
    renderPage()

    expect(await screen.findByText('Acme SSO')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'New provider' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull()
  })
})
