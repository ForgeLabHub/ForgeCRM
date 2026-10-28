/**
 * Tests for the global search palette.
 *
 * The palette is rendered against a stubbed `fetch`, so these cover the path
 * from the HTTP response through the client into the list, rather than asserting
 * against a mock of the component's own logic. The behaviour worth pinning is
 * what a user sees when a search finds nothing, when it fails, and where a
 * result leads — including the case where the server returns a record the
 * interface has no detail page for.
 */
import { describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { GlobalSearch } from './GlobalSearch'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function renderSearch(handler: (url: string) => Response) {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString()
    return handler(url.replace(/^https?:\/\/[^/]+/, ''))
  })
  vi.stubGlobal('fetch', fetchMock)

  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route path="/" element={<GlobalSearch />} />
          <Route path="/accounts/:id" element={<div>Account detail page</div>} />
          <Route path="/deals/:id" element={<div>Deal detail page</div>} />
          <Route path="/leads" element={<div>Leads list page</div>} />
          <Route path="/tickets" element={<div>Tickets list page</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return fetchMock
}

describe('GlobalSearch', () => {
  it('does not search until two characters are typed', async () => {
    const fetchMock = renderSearch(() => json({ items: [] }))
    const user = userEvent.setup()

    await user.type(screen.getByRole('combobox'), 'a')

    // Give the debounce and any stray request time to fire.
    await new Promise((resolve) => setTimeout(resolve, 350))
    expect(fetchMock).not.toHaveBeenCalled()
    expect(screen.getByText(/at least two characters/i)).toBeDefined()
  })

  it('lists what the server returned', async () => {
    renderSearch(() =>
      json({
        items: [
          { entity_type: 'account', id: 'acc-1', title: 'Acme Ltd', subtitle: 'hello@acme.test' },
          { entity_type: 'deal', id: 'deal-1', title: 'Acme renewal' },
        ],
      }),
    )
    const user = userEvent.setup()

    await user.type(screen.getByRole('combobox'), 'ac')
    expect(await screen.findByText('Acme Ltd')).toBeDefined()
    expect(screen.getByText('Acme renewal')).toBeDefined()
    expect(screen.getByText('hello@acme.test')).toBeDefined()
  })

  it('says plainly that an empty result may be a permission limit, not a missing record', async () => {
    renderSearch(() => json({ items: [] }))
    const user = userEvent.setup()

    await user.type(screen.getByRole('combobox'), 'zz')
    expect(await screen.findByText(/none of the matches is available to you/i)).toBeDefined()
  })

  it('surfaces a server error instead of showing an empty list', async () => {
    renderSearch(() =>
      json({ error: { code: 'internal', message: 'search is unavailable' } }, 500),
    )
    const user = userEvent.setup()

    await user.type(screen.getByRole('combobox'), 'ac')
    expect(await screen.findByText(/search is unavailable/i)).toBeDefined()
  })

  it('opens the account detail page for an account result', async () => {
    renderSearch(() =>
      json({ items: [{ entity_type: 'account', id: 'acc-1', title: 'Acme Ltd' }] }),
    )
    const user = userEvent.setup()

    await user.type(screen.getByRole('combobox'), 'ac')
    await user.click(await screen.findByText('Acme Ltd'))

    await waitFor(() => expect(screen.getByText('Account detail page')).toBeDefined())
  })

  it('sends a record with no detail page to its list rather than a broken link', async () => {
    renderSearch(() => json({ items: [{ entity_type: 'lead', id: 'lead-1', title: 'Inbound' }] }))
    const user = userEvent.setup()

    await user.type(screen.getByRole('combobox'), 'in')
    await user.click(await screen.findByText('Inbound'))

    await waitFor(() => expect(screen.getByText('Leads list page')).toBeDefined())
  })

  it('moves the active result with the arrow keys and opens it with Enter', async () => {
    renderSearch(() =>
      json({
        items: [
          { entity_type: 'account', id: 'acc-1', title: 'First Account' },
          { entity_type: 'account', id: 'acc-2', title: 'Second Account' },
        ],
      }),
    )
    const user = userEvent.setup()

    await user.type(screen.getByRole('combobox'), 'ac')
    await screen.findByText('Second Account')

    // The first result is active initially, so one ArrowDown selects the second.
    await user.keyboard('{ArrowDown}{Enter}')

    await waitFor(() => expect(screen.getByText('Account detail page')).toBeDefined())
  })
})
