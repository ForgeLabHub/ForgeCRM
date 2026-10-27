/**
 * Tests for the paginated list component.
 *
 * The component owns pagination, search and the empty/error states, so these
 * assertions cover behaviour every collection in the application inherits.
 */
import { describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ResourceList, type Column } from './ResourceList'
import type { ListResponse } from '../api/client'

interface Row {
  id: string
  name: string
}

const columns: Column<Row>[] = [
  { key: 'name', header: 'Name', render: (row) => row.name },
]

function page(items: Row[], total: number, limit = 25, offset = 0): ListResponse<Row> {
  return { items, total, limit, offset }
}

function renderList(fetchPage: (params: { limit?: number; offset?: number; q?: string }) => Promise<ListResponse<Row>>) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <ResourceList
        title="Rows"
        queryKey={['rows']}
        fetchPage={fetchPage}
        columns={columns}
        rowKey={(row) => row.id}
        emptyTitle="Nothing here"
        emptyText="Create one to get started."
        searchable
        searchPlaceholder="Search rows…"
        pageSize={2}
      />
    </QueryClientProvider>,
  )
}

describe('ResourceList', () => {
  it('renders the rows it is given', async () => {
    renderList(async () => page([{ id: '1', name: 'Alpha' }], 1))

    expect(await screen.findByText('Alpha')).toBeInTheDocument()
  })

  it('shows the empty state when there are no rows', async () => {
    renderList(async () => page([], 0))

    expect(await screen.findByText('Nothing here')).toBeInTheDocument()
    expect(screen.getByText('Create one to get started.')).toBeInTheDocument()
  })

  it('shows the error message and offers a retry when the request fails', async () => {
    const fetchPage = vi.fn().mockRejectedValue(new Error('Database is down'))
    renderList(fetchPage)

    expect(await screen.findByText('Database is down')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /try again/i })).toBeInTheDocument()
  })

  it('disables Previous on the first page and Next on the last', async () => {
    renderList(async () => page([{ id: '1', name: 'Alpha' }], 1))

    const previous = await screen.findByRole('button', { name: /previous/i })
    const next = screen.getByRole('button', { name: /next/i })
    expect(previous).toBeDisabled()
    expect(next).toBeDisabled()
    expect(screen.getByText('1–1 of 1')).toBeInTheDocument()
  })

  it('requests the next page with the right offset', async () => {
    const user = userEvent.setup()
    const fetchPage = vi.fn(async (params: { limit?: number; offset?: number }) =>
      params.offset === 2
        ? page([{ id: '3', name: 'Gamma' }], 3, 2, 2)
        : page([{ id: '1', name: 'Alpha' }, { id: '2', name: 'Beta' }], 3, 2, 0),
    )
    renderList(fetchPage)

    await user.click(await screen.findByRole('button', { name: /next/i }))

    expect(await screen.findByText('Gamma')).toBeInTheDocument()
    expect(fetchPage).toHaveBeenCalledWith(expect.objectContaining({ offset: 2, limit: 2 }))
  })

  it('sends the search term and resets to the first page', async () => {
    const user = userEvent.setup()
    const fetchPage = vi.fn(async (params: { limit?: number; offset?: number; q?: string }) =>
      params.q ? page([{ id: '9', name: 'Matching' }], 1, 2, 0) : page([], 0, 2, 0),
    )
    renderList(fetchPage)

    await user.type(await screen.findByPlaceholderText('Search rows…'), 'acme')

    await waitFor(() =>
      expect(fetchPage).toHaveBeenCalledWith(expect.objectContaining({ q: 'acme', offset: 0 })),
    )
    expect(await screen.findByText('Matching')).toBeInTheDocument()
  })

  it('reports no matches rather than an empty collection while searching', async () => {
    const user = userEvent.setup()
    renderList(async (params) => (params.q ? page([], 0, 2, 0) : page([{ id: '1', name: 'Alpha' }], 1, 2, 0)))

    await user.type(await screen.findByPlaceholderText('Search rows…'), 'zzz')

    expect(await screen.findByText('No matches')).toBeInTheDocument()
    expect(screen.getByText('Try a different search term.')).toBeInTheDocument()
  })
})
