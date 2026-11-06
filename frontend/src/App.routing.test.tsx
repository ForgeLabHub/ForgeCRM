/**
 * The product is one application on one origin.
 *
 * These assertions pin the routing boundary: the root serves the workspace (and
 * asks a stranger to sign in), the application lives under `/app`, and no
 * marketing pages or a second "site" render on this origin. The regression they
 * guard against is a public site owning `/` and swallowing the product.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { App } from './App'
import { AuthProvider } from './hooks/useAuth'
import { I18nProvider } from './i18n'
import { ThemeProvider } from './theme'
import { WorkspaceProvider } from './workspace/WorkspaceProvider'

const SESSION = {
  user: {
    id: 'u-1',
    email: 'e2e@example.com',
    display_name: 'E2E',
    status: 'active',
    is_platform_owner: false,
    mfa_enabled: false,
  },
  organization: { id: 'o-1', name: 'Acme' },
  permissions: ['accounts.read'],
  teams: [],
  csrf_token: 'csrf',
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

/** No session: every API call answers 401, as it would for a stranger. */
function stubAnonymous() {
  vi.stubGlobal(
    'fetch',
    vi.fn(() =>
      Promise.resolve(json({ error: { code: 'unauthorized', message: 'no session' } }, 401)),
    ),
  )
}

function renderApp(path: string) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider>
        <ThemeProvider>
          <MemoryRouter initialEntries={[path]}>
            <AuthProvider>
              <WorkspaceProvider>
                <App />
              </WorkspaceProvider>
            </AuthProvider>
          </MemoryRouter>
        </ThemeProvider>
      </I18nProvider>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  window.localStorage.clear()
  stubAnonymous()
})

describe('one application on one origin', () => {
  it('sends an anonymous visitor from the root to the sign-in form', async () => {
    renderApp('/')
    await waitFor(() => expect(screen.getByLabelText(/email/i)).toBeInTheDocument())
    expect(screen.getByLabelText(/^password$/i)).toBeInTheDocument()
  })

  it('does not render marketing content at the root', async () => {
    renderApp('/')
    // The old public site rendered these; none may come back.
    await waitFor(() => expect(screen.getByLabelText(/email/i)).toBeInTheDocument())
    expect(screen.queryByText(/Pinscher/i)).toBeNull()
    expect(screen.queryByText(/amoCRM/i)).toBeNull()
    expect(screen.queryByText(/Битрикс24/i)).toBeNull()
  })

  it('keeps the application behind /app', async () => {
    renderApp('/app/deals')
    await waitFor(() => expect(screen.getByLabelText(/email/i)).toBeInTheDocument())
  })

  it('renders an application route once a session exists', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString()
        if (url.includes('/auth/me')) return Promise.resolve(json(SESSION))
        if (url.includes('/api/')) return Promise.resolve(json({ items: [], total: 0 }))
        return Promise.resolve(json({}))
      }),
    )
    renderApp('/app/accounts')
    expect(await screen.findByRole('heading', { level: 1, name: /accounts/i })).toBeInTheDocument()
  })

  it('signs in and lands on the overview', async () => {
    const user = userEvent.setup()
    let authed = false
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString()
        if (url.includes('/auth/login')) {
          authed = true
          return Promise.resolve(json(SESSION))
        }
        if (url.includes('/auth/me')) {
          return Promise.resolve(
            authed ? json(SESSION) : json({ error: { code: 'unauthorized' } }, 401),
          )
        }
        if (url.includes('/oidc/providers'))
          return Promise.resolve(json({ enabled: false, providers: [] }))
        return Promise.resolve(json({}))
      }),
    )

    renderApp('/login')
    await user.type(await screen.findByLabelText(/email/i), 'e2e@example.com')
    await user.type(screen.getByLabelText(/^password$/i), 'correct-horse-battery')
    await user.click(screen.getByRole('button', { name: /sign in/i }))

    expect(
      await screen.findByRole('heading', { level: 1, name: /welcome back/i }),
    ).toBeInTheDocument()
  })
})
