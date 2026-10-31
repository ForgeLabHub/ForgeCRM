/**
 * Tests for the consent panel.
 *
 * The panel is the operator's view of the consent record the server keeps. The
 * assertions are about what it exposes and what it sends: the current decisions and
 * their states, a recorded decision carrying the right triple, a withdrawal going
 * to the withdraw endpoint rather than a record with state "withdrawn", and the
 * panel staying invisible without the read permission.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ConsentPanel } from './ConsentPanel'
import { AuthProvider } from '../hooks/useAuth'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

interface StubOptions {
  permissions?: string[]
  current?: unknown[]
  history?: unknown[]
  onWrite?: (url: string, method: string, body: Record<string, unknown>) => void
}

function stubFetch(options: StubOptions = {}) {
  const permissions = options.permissions ?? ['consent.read', 'consent.manage']
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    const method = init?.method ?? 'GET'
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
    if (url.includes('/api/v1/consent/history')) {
      return Promise.resolve(json({ items: options.history ?? [] }))
    }
    if (url.includes('/api/v1/consent/current')) {
      return Promise.resolve(json({ items: options.current ?? [] }))
    }
    if (url.includes('/api/v1/consent')) {
      options.onWrite?.(url, method, JSON.parse(String(init?.body ?? '{}')))
      return Promise.resolve(json({ id: 'new' }, 201))
    }
    return Promise.resolve(json({}))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderPanel() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <ConsentPanel contactId="c1" email="person@example.com" />
      </AuthProvider>
    </QueryClientProvider>,
  )
}

const GRANTED = {
  id: 'r1',
  organization_id: 'o1',
  contact_id: 'c1',
  purpose: 'marketing',
  channel: 'email',
  state: 'granted',
  revision: 1,
  decided_at: '2026-01-01T00:00:00Z',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  current: true,
}

beforeEach(() => {
  vi.unstubAllGlobals()
})

describe('ConsentPanel', () => {
  it('renders the current decision and its state', async () => {
    stubFetch({ current: [GRANTED] })
    renderPanel()

    const cell = await screen.findByRole('cell', { name: 'marketing' })
    expect(cell).toBeInTheDocument()
    // The row's state badge, not the option of the same name in the form select.
    expect(within(cell.closest('tr') as HTMLElement).getByText('Granted')).toBeInTheDocument()
  })

  it('explains that an empty record blocks marketing but allows transactional', async () => {
    stubFetch({ current: [] })
    renderPanel()

    expect(
      await screen.findByText(/Marketing and product messages are blocked/i),
    ).toBeInTheDocument()
  })

  it('records a decision against the contact with the chosen triple', async () => {
    const writes: { url: string; method: string; body: Record<string, unknown> }[] = []
    stubFetch({ current: [], onWrite: (url, method, body) => writes.push({ url, method, body }) })
    renderPanel()

    await userEvent.selectOptions(await screen.findByLabelText('Purpose'), 'product_updates')
    await userEvent.selectOptions(screen.getByLabelText('Channel'), 'telegram')
    await userEvent.type(screen.getByLabelText('Basis'), 'signup form')
    await userEvent.click(screen.getByRole('button', { name: 'Record decision' }))

    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].method).toBe('POST')
    expect(writes[0].url).toContain('/api/v1/consent')
    expect(writes[0].body).toMatchObject({
      contact_id: 'c1',
      purpose: 'product_updates',
      channel: 'telegram',
      state: 'granted',
      basis: 'signup form',
    })
  })

  it('withdraws through the withdraw endpoint, not a record with state withdrawn', async () => {
    const writes: { url: string; method: string; body: Record<string, unknown> }[] = []
    stubFetch({ current: [GRANTED], onWrite: (url, method, body) => writes.push({ url, method, body }) })
    renderPanel()

    await userEvent.selectOptions(await screen.findByLabelText('Decision'), 'withdrawn')
    await userEvent.click(screen.getByRole('button', { name: 'Withdraw consent' }))

    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].url).toContain('/api/v1/consent/withdraw')
    expect(writes[0].body).toMatchObject({ contact_id: 'c1', purpose: 'marketing', channel: 'email' })
  })

  it('is invisible without the read permission', async () => {
    stubFetch({ permissions: ['contacts.read'], current: [GRANTED] })
    renderPanel()

    await waitFor(() => expect(screen.queryByText('Consent')).not.toBeInTheDocument())
  })

  it('offers no way to change consent without the manage permission', async () => {
    stubFetch({ permissions: ['consent.read'], current: [GRANTED] })
    renderPanel()

    expect(await screen.findByRole('cell', { name: 'marketing' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Record decision' })).not.toBeInTheDocument()
  })
})
