/**
 * Tests for the MCP client credentials page.
 *
 * The credential endpoints existed on the server but nothing in the interface
 * called them, so an operator could not connect an external MCP client without a
 * hand-built request. These tests pin the behaviours that matter for a secret: the
 * list shows only a non-secret prefix, the plaintext token is shown exactly once
 * after creation, and the management controls are hidden from a read-only viewer.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MCPCredentialsPage } from './MCPCredentialsPage'
import { AuthProvider } from '../../hooks/useAuth'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const credential = {
  id: 'c1',
  organization_id: 'o1',
  owner_user_id: 'u1',
  name: 'Laptop agent',
  token_prefix: 'fcmcp_ab12cd34',
  permissions: ['accounts.read'],
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  status: 'active',
}

const fullToken = 'fcmcp_ab12cd34_the_whole_secret_value'

function stubFetch(permissions: string[] = ['mcp.credentials.read', 'mcp.credentials.manage']) {
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    const method = init?.method ?? 'GET'
    if (url.includes('/api/v1/mcp/credentials') && method === 'POST') {
      return Promise.resolve(json({ credential, token: fullToken }, 201))
    }
    if (url.includes('/api/v1/mcp/credentials') && method === 'DELETE') {
      return Promise.resolve(new Response(null, { status: 204 }))
    }
    if (url.includes('/api/v1/mcp/credentials')) {
      return Promise.resolve(json({ items: [credential] }))
    }
    if (url.includes('/api/v1/roles/permissions')) {
      return Promise.resolve(
        json({
          items: [
            { key: 'accounts.read', category: 'accounts', description: 'View accounts' },
            { key: 'tasks.create', category: 'tasks', description: 'Create tasks' },
          ],
        }),
      )
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
        <MCPCredentialsPage />
      </AuthProvider>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.unstubAllGlobals()
})

describe('MCPCredentialsPage', () => {
  it('lists credentials showing only the non-secret prefix', async () => {
    stubFetch()
    renderPage()

    expect(await screen.findByText('Laptop agent')).toBeInTheDocument()
    expect(screen.getByText('fcmcp_ab12cd34…')).toBeInTheDocument()
    // The full token is never in the list response, so it must not appear.
    expect(screen.queryByText(fullToken)).toBeNull()
  })

  it('reveals the plaintext token exactly once after creating a credential', async () => {
    stubFetch()
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'New credential' }))
    await userEvent.type(screen.getByLabelText(/Name/), 'CI agent')
    await userEvent.click(screen.getByLabelText('accounts.read'))
    await userEvent.click(screen.getByRole('button', { name: 'Create credential' }))

    // The one-time dialog shows the token and says it cannot be recovered.
    expect(await screen.findByText(fullToken)).toBeInTheDocument()
    expect(screen.getByText(/only time the token is shown/i)).toBeInTheDocument()
  })

  it('sends the selected permissions and the name', async () => {
    const fetchMock = stubFetch()
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'New credential' }))
    await userEvent.type(screen.getByLabelText(/Name/), 'CI agent')
    await userEvent.click(screen.getByLabelText('tasks.create'))
    await userEvent.click(screen.getByRole('button', { name: 'Create credential' }))

    await waitFor(() => {
      const post = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/api/v1/mcp/credentials') &&
          (init as RequestInit | undefined)?.method === 'POST',
      )
      expect(post).toBeTruthy()
      const body = JSON.parse(String((post![1] as RequestInit).body))
      expect(body.name).toBe('CI agent')
      expect(body.permissions).toEqual(['tasks.create'])
      // No expiry selected means the field is omitted, not sent as zero.
      expect('expires_in_hours' in body).toBe(false)
    })
  })

  it('hides the management controls from a read-only operator', async () => {
    stubFetch(['mcp.credentials.read'])
    renderPage()

    expect(await screen.findByText('Laptop agent')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'New credential' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Rotate' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Revoke' })).toBeNull()
  })
})
