/**
 * Tests for the lead detail page.
 *
 * The page reads a single lead through `GET /api/v1/leads/{id}`, which the
 * interface only reaches by clicking a row. These tests hold the page to that
 * path: the row click navigates to the detail route, the page reads the record
 * the server returns rather than a list, and a failure is surfaced instead of
 * rendering an empty card. A lead that is already converted must not offer the
 * Convert control, because the server would reject a second conversion.
 */
import { describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { LeadDetailPage, LeadsPage } from './LeadsPage'
import { AuthProvider } from '../hooks/useAuth'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const lead = {
  id: 'lead-1',
  organization_id: 'org-1',
  first_name: 'Ada',
  last_name: 'Lovelace',
  email: 'ada@example.com',
  company: 'Analytical Engines',
  status: 'qualified',
  custom_fields: {},
  created_at: '2026-10-01T09:00:00Z',
  updated_at: '2026-10-02T09:00:00Z',
}

function stubFetch(
  handler: (url: string) => Response | undefined,
  permissions: string[] = ['leads.read', 'leads.update', 'leads.convert'],
) {
  const fetchMock = vi.fn((input: RequestInfo | URL, _init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    const path = url.replace(/^https?:\/\/[^/]+/, '')
    if (path.includes('/api/v1/auth/me')) {
      return Promise.resolve(
        json({
          user: { id: 'u1', email: 'a@example.com', display_name: 'A' },
          organization: { id: 'org-1', name: 'Org' },
          permissions,
          csrf_token: 't',
        }),
      )
    }
    const handled = handler(path)
    if (handled) return Promise.resolve(handled)
    return Promise.resolve(json({ items: [], total: 0, limit: 25, offset: 0 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderDetail() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/leads/lead-1']}>
        <AuthProvider>
          <Routes>
            <Route path="/leads/:id" element={<LeadDetailPage />} />
          </Routes>
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('LeadDetailPage', () => {
  it('renders the lead the server returns for the id in the path', async () => {
    const fetchMock = stubFetch((path) =>
      path === '/api/v1/leads/lead-1' ? json(lead) : undefined,
    )

    renderDetail()

    expect(await screen.findByRole('heading', { name: 'Ada Lovelace' })).toBeDefined()
    expect(screen.getByText('ada@example.com')).toBeDefined()
    expect(
      fetchMock.mock.calls.some(([input]) => String(input).includes('/api/v1/leads/lead-1')),
    ).toBe(true)
  })

  it('surfaces a load failure instead of an empty card', async () => {
    stubFetch((path) =>
      path === '/api/v1/leads/lead-1'
        ? json({ error: { code: 'not_found', message: 'lead not found' } }, 404)
        : undefined,
    )

    renderDetail()

    expect(await screen.findByText(/lead not found/i)).toBeDefined()
  })

  it('does not offer to convert a lead that is already converted', async () => {
    stubFetch((path) =>
      path === '/api/v1/leads/lead-1'
        ? json({ ...lead, status: 'converted', converted_at: '2026-10-03T09:00:00Z' })
        : undefined,
    )

    renderDetail()

    await screen.findByRole('heading', { name: 'Ada Lovelace' })
    expect(screen.queryByRole('button', { name: 'Convert' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Edit' })).toBeDefined()
  })

  it('sends the conversion the dialog confirms and shows the result', async () => {
    const fetchMock = stubFetch((path) => {
      if (path === '/api/v1/leads/lead-1') return json(lead)
      if (path === '/api/v1/leads/lead-1/convert') {
        return json({
          lead: { ...lead, status: 'converted', converted_at: '2026-10-04T09:00:00Z' },
          account: { id: 'acc-1', name: 'Analytical Engines' },
          contact: { id: 'con-1', first_name: 'Ada', last_name: 'Lovelace' },
        })
      }
      return undefined
    })

    renderDetail()

    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Convert' }))
    const dialog = screen.getByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Convert' }))

    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(([input]) => String(input).includes('/api/v1/leads/lead-1/convert')),
      ).toBe(true),
    )
    expect(await screen.findByText('Lead converted')).toBeDefined()
  })
})

describe('LeadsPage row navigation', () => {
  it('opens the detail route when a row is clicked', async () => {
    stubFetch((path) =>
      path.startsWith('/api/v1/leads?') || path === '/api/v1/leads'
        ? json({ items: [lead], total: 1, limit: 25, offset: 0 })
        : undefined,
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/leads']}>
          <AuthProvider>
            <Routes>
              <Route path="/leads" element={<LeadsPage />} />
              <Route path="/leads/:id" element={<div>Lead detail page</div>} />
            </Routes>
          </AuthProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const user = userEvent.setup()
    await user.click(await screen.findByText('ada@example.com'))
    await waitFor(() => expect(screen.getByText('Lead detail page')).toBeDefined())
  })
})

describe('LeadsPage create form', () => {
  it('marks the title required so an empty lead is never submitted', async () => {
    const fetchMock = stubFetch(() => undefined, ['leads.read', 'leads.create'])

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/leads']}>
          <AuthProvider>
            <Routes>
              <Route path="/leads" element={<LeadsPage />} />
            </Routes>
          </AuthProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'New lead' }))
    const dialog = screen.getByRole('dialog')
    const title = within(dialog).getByLabelText(/Title/)
    // The title is the one required field on a lead; the browser must block the
    // submit that previously created a blank row (the D-5 defect).
    expect(title).toBeRequired()

    await user.click(within(dialog).getByRole('button', { name: 'Create lead' }))
    expect(
      fetchMock.mock.calls.some(([, init]) => (init as RequestInit | undefined)?.method === 'POST'),
    ).toBe(false)
  })
})
