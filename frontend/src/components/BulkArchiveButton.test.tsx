/**
 * Tests for bulk archive, from the checkbox down to the API call.
 *
 * Bulk archive is the one place in the product where a single click changes many
 * records, so the assertions here are about the two ways that goes wrong: acting
 * without confirming, and reporting a partial archive as a complete one.
 */
import { describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ResourceList, type Column } from './ResourceList'
import { BulkArchiveButton } from './BulkArchiveButton'
import type { ListResponse } from '../api/client'
import type { BulkArchiveResult } from '../types/api'

interface Row {
  id: string
  name: string
}

const columns: Column<Row>[] = [{ key: 'name', header: 'Name', render: (row) => row.name }]

function page(items: Row[]): ListResponse<Row> {
  return { items, total: items.length, limit: 25, offset: 0 }
}

const rows: Row[] = [
  { id: '11111111-1111-1111-1111-111111111111', name: 'Acme' },
  { id: '22222222-2222-2222-2222-222222222222', name: 'Globex' },
]

/**
 * Renders a list with a bulk archive control, mirroring how the pages wire the
 * two together.
 */
function renderWithBulk(archive: (ids: string[]) => Promise<BulkArchiveResult>) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <ResourceList
        title="Rows"
        queryKey={['rows']}
        fetchPage={() => Promise.resolve(page(rows))}
        columns={columns}
        rowKey={(row) => row.id}
        emptyTitle="Nothing here"
        searchable
        searchPlaceholder="Search rows…"
        bulkActions={(selected, clear) => (
          <BulkArchiveButton
            ids={selected}
            entityLabel="accounts"
            archive={archive}
            onDone={clear}
          />
        )}
      />
    </QueryClientProvider>,
  )
}

describe('bulk archive', () => {
  it('does not archive anything until the confirmation is accepted', async () => {
    const user = userEvent.setup()
    const archive = vi.fn().mockResolvedValue({ archived: rows.map((r) => r.id), failed: [] })
    renderWithBulk(archive)

    await screen.findByText('Acme')

    // Selecting a row must not itself call the API: the checkbox builds a
    // selection, the confirmation is what acts on it.
    await user.click(screen.getAllByLabelText('Select row')[0])
    expect(archive).not.toHaveBeenCalled()
    expect(screen.getByText('1 selected')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Archive 1 account' }))
    expect(archive).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: 'Archive 1' }))

    await waitFor(() => expect(archive).toHaveBeenCalledTimes(1))
    expect(archive).toHaveBeenCalledWith([rows[0].id])
  })

  it('selects every row on the page from the header checkbox', async () => {
    const user = userEvent.setup()
    const archive = vi.fn().mockResolvedValue({ archived: rows.map((r) => r.id), failed: [] })
    renderWithBulk(archive)

    await screen.findByText('Acme')
    await user.click(screen.getByLabelText('Select all rows on this page'))

    expect(screen.getByText('2 selected')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Archive 2 accounts' }))
    await user.click(screen.getByRole('button', { name: 'Archive 2' }))

    await waitFor(() => expect(archive).toHaveBeenCalledTimes(1))
    expect(archive).toHaveBeenCalledWith([rows[0].id, rows[1].id])
  })

  it('reports a partial result per record instead of claiming success', async () => {
    const user = userEvent.setup()
    const archive = vi.fn().mockResolvedValue({
      archived: [rows[0].id],
      failed: [{ id: rows[1].id, reason: 'not found or already archived' }],
    })
    renderWithBulk(archive)

    await screen.findByText('Acme')
    await user.click(screen.getByLabelText('Select all rows on this page'))
    await user.click(screen.getByRole('button', { name: 'Archive 2 accounts' }))
    await user.click(screen.getByRole('button', { name: 'Archive 2' }))

    // The report must say one of two was archived and name the reason. A plain
    // "archived" message would leave the user believing both were archived.
    const report = await screen.findByText(/Archived 1 of 2/)
    expect(report).toHaveTextContent('not found or already archived')

    // The selection survives, because the record that failed is still the one the
    // user needs to act on. Clearing it would hide it.
    expect(screen.getByText('2 selected')).toBeInTheDocument()
  })

  it('clears the selection when every record was archived', async () => {
    const user = userEvent.setup()
    const archive = vi.fn().mockResolvedValue({ archived: rows.map((r) => r.id), failed: [] })
    renderWithBulk(archive)

    await screen.findByText('Acme')
    await user.click(screen.getByLabelText('Select all rows on this page'))
    await user.click(screen.getByRole('button', { name: 'Archive 2 accounts' }))
    await user.click(screen.getByRole('button', { name: 'Archive 2' }))

    // Clearing the selection removes the bulk bar, and with it the success report
    // that was rendered inside it. That is the intended end state for a fully
    // successful archive: there is nothing left for the user to act on. The
    // partial case above is where the report has to stay visible, because there
    // the records that failed still need attention.
    await waitFor(() => expect(screen.queryByText('2 selected')).not.toBeInTheDocument())
    expect(screen.queryByRole('button', { name: 'Archive 2 accounts' })).not.toBeInTheDocument()
  })

  it('shows the failure when the request itself is refused', async () => {
    const user = userEvent.setup()
    const archive = vi.fn().mockRejectedValue(new Error('permission denied'))
    renderWithBulk(archive)

    await screen.findByText('Acme')
    await user.click(screen.getByLabelText('Select all rows on this page'))
    await user.click(screen.getByRole('button', { name: 'Archive 2 accounts' }))
    await user.click(screen.getByRole('button', { name: 'Archive 2' }))

    expect(await screen.findByText(/permission denied/i)).toBeInTheDocument()
  })

  it('drops the selection when the search changes', async () => {
    const user = userEvent.setup()
    const archive = vi.fn().mockResolvedValue({ archived: [], failed: [] })
    renderWithBulk(archive)

    await screen.findByText('Acme')
    await user.click(screen.getAllByLabelText('Select row')[0])
    expect(screen.getByText('1 selected')).toBeInTheDocument()

    // A selection refers to rows on screen. Once the search changes, those rows
    // are gone, so a selection that survived would let a bulk action apply to
    // records the user can no longer see or deselect.
    await user.type(screen.getByRole('searchbox'), 'acme')

    await waitFor(() => expect(screen.queryByText('1 selected')).not.toBeInTheDocument())
  })
})
