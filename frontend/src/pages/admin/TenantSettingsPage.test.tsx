/**
 * Tests for the organization configuration page.
 *
 * The tenant configuration endpoints existed on the server but nothing in the
 * interface reached them, so an organization could not choose its own defaults
 * without a hand-built request. These tests pin the behaviours that make the page
 * honest about what it does: the editor is built from the server's catalog, an
 * override is distinguished from a default by the flag rather than by comparing
 * values, a write shows the value the server resolved, and reset is confirmed and
 * unavailable to a read-only viewer.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TenantSettingsPage } from './TenantSettingsPage'
import { AuthProvider } from '../../hooks/useAuth'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

interface StubSetting {
  key: string
  title: string
  description: string
  kind: string
  enum?: string[]
  min?: number
  max?: number
  max_length?: number
  default: unknown
  override?: unknown
  has_override: boolean
  effective: unknown
  schema_version: number
}

const currencySetting: StubSetting = {
  key: 'crm.default_currency',
  title: 'Default currency',
  description: 'The currency a new deal is denominated in when none is given.',
  kind: 'string',
  max_length: 3,
  default: 'USD',
  override: 'EUR',
  has_override: true,
  effective: 'EUR',
  schema_version: 1,
}

const prioritySetting: StubSetting = {
  key: 'tasks.default_priority',
  title: 'Default task priority',
  description: 'The priority a new task gets when none is given.',
  kind: 'enum',
  enum: ['low', 'normal', 'high', 'urgent'],
  default: 'normal',
  has_override: false,
  effective: 'normal',
  schema_version: 1,
}

function stubFetch(permissions: string[] = ['tenant.settings.read', 'tenant.settings.manage']) {
  // The server's state, so a refetch after a write reflects the write rather than
  // the state before it. Without this the page would appear to revert on refetch.
  let currency = { ...currencySetting }
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    const method = init?.method ?? 'GET'
    if (url.includes('/api/v1/settings/crm.default_currency') && method === 'PUT') {
      const body = JSON.parse(String((init as RequestInit).body))
      currency = { ...currency, override: body.value, has_override: true, effective: body.value }
      return Promise.resolve(json(currency))
    }
    if (url.includes('/api/v1/settings/crm.default_currency') && method === 'DELETE') {
      currency = { ...currency, override: undefined, has_override: false, effective: 'USD' }
      return Promise.resolve(json(currency))
    }
    if (url.includes('/api/v1/settings')) {
      return Promise.resolve(json({ items: [currency, prioritySetting] }))
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
        <TenantSettingsPage />
      </AuthProvider>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.unstubAllGlobals()
})

describe('TenantSettingsPage', () => {
  it('renders one editor per catalog setting', async () => {
    stubFetch()
    renderPage()

    expect(await screen.findByText('Default currency')).toBeInTheDocument()
    expect(screen.getByText('Default task priority')).toBeInTheDocument()
    expect(screen.getByText('crm.default_currency')).toBeInTheDocument()
  })

  it('marks an override as the organization’s own and a value as the platform default', async () => {
    stubFetch()
    renderPage()

    // The distinction is read from the flag, not inferred from the value.
    expect(await screen.findByText('organization override')).toBeInTheDocument()
    expect(screen.getByText('platform default')).toBeInTheDocument()
  })

  it('sends the chosen value and shows the value the server resolved', async () => {
    const fetchMock = stubFetch()
    renderPage()

    const input = await screen.findByLabelText('Value', {
      selector: '#setting-crm\\.default_currency',
    })
    // The card for the setting under test, so the page's other Save button is not matched.
    const card = input.closest('.card') as HTMLElement
    await userEvent.clear(input)
    await userEvent.type(input, 'GBP')
    await userEvent.click(within(card).getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      const put = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/api/v1/settings/crm.default_currency') &&
          (init as RequestInit | undefined)?.method === 'PUT',
      )
      expect(put).toBeTruthy()
      const body = JSON.parse(String((put![1] as RequestInit).body))
      expect(body.value).toBe('GBP')
    })

    // The response's effective value is what the page shows, not the request's.
    await waitFor(() => expect(within(card).getByText(/Effective now:/)).toHaveTextContent('GBP'))
  })

  it('confirms before resetting an override and then shows the default in force', async () => {
    stubFetch()
    renderPage()

    const input = await screen.findByLabelText('Value', {
      selector: '#setting-crm\\.default_currency',
    })
    const card = input.closest('.card') as HTMLElement
    await userEvent.click(within(card).getByRole('button', { name: 'Reset to default' }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Reset' }))

    await waitFor(() => expect(within(card).getByText(/Effective now:/)).toHaveTextContent('USD'))
  })

  it('hides the management controls from a read-only viewer', async () => {
    stubFetch(['tenant.settings.read'])
    renderPage()

    await screen.findByText('Default currency')
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Reset to default' })).toBeNull()
    expect(screen.getByLabelText('Value', { selector: '#setting-crm\\.default_currency' })).toBeDisabled()
  })
})
