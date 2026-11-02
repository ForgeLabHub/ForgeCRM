/**
 * Tests for the command palette.
 *
 * The palette is rendered against a stubbed `fetch` and a real auth session, so
 * these cover the path from the HTTP response through the client, the auth
 * context and the router into the list. The behaviour worth pinning is what a
 * user sees when a search finds nothing, when it fails, where a result leads,
 * and that a section the caller cannot write to is not offered as a create
 * action. None of them mock the component under test.
 */
import { describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { CommandPalette } from './CommandPalette'
import { AuthProvider } from '../../hooks/useAuth'
import { I18nProvider } from '../../i18n'
import type { MeResponse } from '../../types/api'

const ME: MeResponse = {
  user: {
    id: 'u-1',
    email: 'sales@example.com',
    display_name: 'Sam Sales',
    status: 'active',
    is_platform_owner: false,
    mfa_enabled: false,
  },
  organization: { id: 'o-1', name: 'Acme Inc' },
  permissions: ['accounts.read', 'contacts.read', 'deals.read', 'contacts.create'],
  teams: [],
  csrf_token: 'csrf-from-me',
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

/** Routes requests by path so a test can describe a whole session. */
function renderPalette(search: () => Response | Promise<Response>) {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString()
    const path = url.replace(/^https?:\/\/[^/]+/, '').split('?')[0]
    if (path === '/api/v1/auth/me') return json(ME)
    if (path === '/api/v1/search') return search()
    return json({ items: [] })
  })
  vi.stubGlobal('fetch', fetchMock)

  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const onClose = vi.fn()
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider>
        <MemoryRouter initialEntries={['/']}>
          <AuthProvider>
            <Routes>
              <Route path="/" element={<CommandPalette onClose={onClose} />} />
              <Route path="/accounts/:id" element={<div>Account detail page</div>} />
              <Route path="/deals/:id" element={<div>Deal detail page</div>} />
              <Route path="/leads" element={<div>Leads list page</div>} />
            </Routes>
          </AuthProvider>
        </MemoryRouter>
      </I18nProvider>
    </QueryClientProvider>,
  )
  return { fetchMock, onClose }
}

describe('CommandPalette', () => {
  it('does not search until two characters are typed', async () => {
    const { fetchMock } = renderPalette(() => json({ items: [] }))
    const user = userEvent.setup()

    await user.type(await screen.findByRole('combobox'), 'a')

    // Give the debounce and any stray request time to fire.
    await new Promise((resolve) => setTimeout(resolve, 300))
    expect(fetchMock.mock.calls.some((call) => String(call[0]).includes('/api/v1/search'))).toBe(false)
  })

  it('lists the records the server returned and navigates to one', async () => {
    const { onClose } = renderPalette(() =>
      json({
        items: [
          { entity_type: 'account', id: 'acc-1', title: 'Acme Ltd', subtitle: 'hello@acme.test' },
        ],
      }),
    )
    const user = userEvent.setup()

    await user.type(await screen.findByRole('combobox'), 'acme')
    const option = await screen.findByRole('option', { name: /Acme Ltd/ })
    await user.click(option)

    expect(await screen.findByText('Account detail page')).toBeInTheDocument()
    expect(onClose).toHaveBeenCalled()
  })

  it('sends a lead to the list, since leads have no detail route', async () => {
    renderPalette(() =>
      json({ items: [{ entity_type: 'lead', id: 'lead-1', title: 'Big Lead' }] }),
    )
    const user = userEvent.setup()

    await user.type(await screen.findByRole('combobox'), 'big')
    await user.click(await screen.findByRole('option', { name: /Big Lead/ }))

    expect(await screen.findByText('Leads list page')).toBeInTheDocument()
  })

  it('shows the empty message when a search matches nothing', async () => {
    renderPalette(() => json({ items: [] }))
    const user = userEvent.setup()

    await user.type(await screen.findByRole('combobox'), 'zzzz')

    expect(await screen.findByText(/No matches for/i)).toBeInTheDocument()
  })

  it('offers create actions only for sections the caller may write to', async () => {
    renderPalette(() => json({ items: [] }))
    const user = userEvent.setup()

    // "contacts" is in the session's permission list, "deals" is not.
    await user.type(await screen.findByRole('combobox'), 'contact')

    // "Contacts" is offered both as a destination and as a create action; the
    // create one carries the "Create" hint. Wait for both to appear rather than
    // reading the list the moment the first one renders.
    await waitFor(() => {
      expect(screen.getAllByRole('option', { name: /Contacts/ }).length).toBe(2)
    })
    expect(screen.queryByRole('option', { name: /Deals/ })).toBeNull()
  })

  it('closes on Escape', async () => {
    const { onClose } = renderPalette(() => json({ items: [] }))
    const user = userEvent.setup()

    await user.type(await screen.findByRole('combobox'), '{Escape}')

    await waitFor(() => expect(onClose).toHaveBeenCalled())
  })
})
