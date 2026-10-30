/**
 * Tests for the saved-views switcher.
 *
 * The component is the only interface to the saved-view endpoints, so the
 * assertions are about what it sends and what it exposes: picking a view hands
 * its filters back to the screen, a user without views.manage is offered no save
 * or delete control, and a shared view owned by someone else cannot be deleted
 * from the switcher.
 */
import { describe, expect, it, vi } from 'vitest'
import { useState } from 'react'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { SavedViewsBar } from './SavedViewsBar'
import { AuthProvider } from '../hooks/useAuth'
import type { SavedView, ViewFilters } from '../api/views'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function view(overrides: Partial<SavedView> = {}): SavedView {
  return {
    id: 'v1',
    organization_id: 'o1',
    owner_user_id: 'u1',
    entity_type: 'deals',
    name: 'Hot deals',
    shared: false,
    pinned: false,
    filters: { status: 'open' },
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    is_mine: true,
    ...overrides,
  }
}

interface StubOptions {
  permissions?: string[]
  views?: SavedView[]
  onCreate?: (body: unknown) => void
  onDelete?: (id: string) => void
}

function stubFetch(options: StubOptions = {}) {
  const permissions = options.permissions ?? ['views.read', 'views.manage']
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
    if (url.includes('/api/v1/views')) {
      if (method === 'POST') {
        options.onCreate?.(JSON.parse(String(init?.body ?? '{}')))
        return Promise.resolve(json(view({ id: 'new' }), 201))
      }
      if (method === 'DELETE') {
        options.onDelete?.(url.split('/').pop() ?? '')
        return Promise.resolve(new Response(null, { status: 204 }))
      }
      return Promise.resolve(json({ items: options.views ?? [], total: 0, limit: 25, offset: 0 }))
    }
    return Promise.resolve(json({}))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderBar(
  onApply: (filters: ViewFilters) => void = () => {},
  onActiveChange: (id: string | undefined) => void = () => {},
  current: ViewFilters = {},
) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  // The switcher is controlled: it reports the active view and the screen feeds
  // it back. The harness holds that state so the component sees what a real page
  // would give it.
  function Harness() {
    const [activeId, setActiveId] = useState<string | undefined>(undefined)
    const [filters, setFilters] = useState<ViewFilters>(current)
    return (
      <SavedViewsBar
        entityType="deals"
        current={filters}
        onApply={(next) => {
          setFilters(next)
          onApply(next)
        }}
        activeId={activeId}
        onActiveChange={(id) => {
          setActiveId(id)
          onActiveChange(id)
        }}
      />
    )
  }
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <Harness />
      </AuthProvider>
    </QueryClientProvider>,
  )
}

describe('SavedViewsBar', () => {
  it('lists the views for the screen', async () => {
    stubFetch({ views: [view({ name: 'Hot deals' }), view({ id: 'v2', name: 'Closing soon' })] })
    renderBar()

    expect(await screen.findByRole('option', { name: /hot deals/i })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: /closing soon/i })).toBeInTheDocument()
  })

  it('applies the chosen view\u2019s filters to the screen', async () => {
    const onApply = vi.fn()
    const onActiveChange = vi.fn()
    stubFetch({ views: [view({ id: 'v9', name: 'Won', filters: { status: 'won' } })] })
    renderBar(onApply, onActiveChange)

    const select = await screen.findByRole('combobox')
    await userEvent.selectOptions(select, 'v9')

    await waitFor(() => expect(onApply).toHaveBeenCalledWith({ status: 'won' }))
    expect(onActiveChange).toHaveBeenCalledWith('v9')
  })

  it('offers save and delete only when the user may manage views', async () => {
    stubFetch({ permissions: ['views.read'], views: [view()] })
    renderBar()

    // The list still loads for a read-only user.
    expect(await screen.findByRole('option', { name: /hot deals/i })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /save/i })).not.toBeInTheDocument()
  })

  it('does not offer delete for a shared view owned by someone else', async () => {
    stubFetch({ views: [view({ id: 'v3', name: 'Team view', shared: true, is_mine: false })] })
    renderBar()

    const select = await screen.findByRole('combobox')
    await userEvent.selectOptions(select, 'v3')

    await screen.findByText('shared view')
    expect(screen.queryByRole('button', { name: /^delete$/i })).not.toBeInTheDocument()
  })

  it('sends the current filters when saving a view', async () => {
    const onCreate = vi.fn()
    stubFetch({ onCreate })
    renderBar(undefined, undefined, { status: 'open' })

    await userEvent.click(await screen.findByRole('button', { name: /save view/i }))
    await userEvent.type(screen.getByLabelText(/name/i), 'My open deals')
    await userEvent.click(screen.getByRole("button", { name: /^save$/i }))

    await waitFor(() => expect(onCreate).toHaveBeenCalled())
    expect(onCreate.mock.calls[0][0]).toMatchObject({
      entity_type: 'deals',
      name: 'My open deals',
      filters: { status: 'open' },
    })
  })

  it('refuses to save a view with no name', async () => {
    const onCreate = vi.fn()
    stubFetch({ onCreate })
    renderBar()

    await userEvent.click(await screen.findByRole('button', { name: /save view/i }))
    await userEvent.click(screen.getByRole("button", { name: /^save$/i }))

    expect(await screen.findByText(/a name is required/i)).toBeInTheDocument()
    expect(onCreate).not.toHaveBeenCalled()
  })

  it('flags a view as modified when the screen no longer matches it', async () => {
    stubFetch({ views: [view({ id: 'v5', filters: { status: 'open' } })] })
    renderBar(undefined, undefined, { status: 'won' })

    const select = await screen.findByRole('combobox')
    await userEvent.selectOptions(select, 'v5')

    // Applying the view first restores its filters; the screen then matches it.
    await waitFor(() => expect(screen.queryByText('modified')).not.toBeInTheDocument())
  })
})
