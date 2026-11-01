import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { useQuery, keepPreviousData } from '@tanstack/react-query'
import type { ListResponse, PaginationParams } from '../api/client'
import { errorMessage } from '../api/client'
import { useT } from '../i18n'
import { Card, EmptyState, ErrorState, Pagination, TableSkeleton } from './ui'

export interface Column<T> {
  key: string
  header: string
  /** Renders the cell. Receives the row. */
  render: (row: T) => ReactNode
  numeric?: boolean
  width?: string
}

interface ResourceListProps<T> {
  /** Stable key for the query cache. */
  queryKey: readonly unknown[]
  /** Fetches one page. Must accept limit, offset and the search term. */
  fetchPage: (params: PaginationParams) => Promise<ListResponse<T>>
  columns: Column<T>[]
  /** Extract a stable key for a row. */
  rowKey: (row: T) => string | number
  onRowClick?: (row: T) => void
  emptyTitle: string
  emptyText?: string
  emptyAction?: ReactNode
  /** Enables the search box, which is sent as `q`. */
  searchable?: boolean
  searchPlaceholder?: string
  /** Filters rendered next to the search box. */
  filters?: ReactNode
  /** Rendered in the card header, typically a create button. */
  actions?: ReactNode
  title?: string
  pageSize?: number
  /**
   * Adds a checkbox column and renders the bulk bar when rows are selected.
   * Receives the selected row keys and a function to clear the selection.
   */
  bulkActions?: (selected: string[], clear: () => void) => ReactNode
}

/**
 * A paginated, searchable table driven entirely by one fetch function.
 *
 * Centralising this means every collection behaves the same way: the same
 * loading state, the same empty state, the same error handling, and the same
 * pagination semantics. A page only supplies its columns.
 */
export function ResourceList<T>({
  queryKey,
  fetchPage,
  columns,
  rowKey,
  onRowClick,
  emptyTitle,
  emptyText,
  emptyAction,
  searchable = false,
  searchPlaceholder = 'Search…',
  filters,
  actions,
  title,
  pageSize = 25,
  bulkActions,
}: ResourceListProps<T>) {
  const [offset, setOffset] = useState(0)
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const t = useT()

  const selectable = Boolean(bulkActions)

  // A selection is confined to the page it was made on. Changing the page or the
  // search term clears it, because the rows it referred to are no longer on
  // screen: a selection that outlives its rows would let a bulk action apply to
  // records the user can no longer see and cannot deselect.
  useEffect(() => {
    setSelected(new Set())
  }, [offset, search])

  const params: PaginationParams = { limit: pageSize, offset }
  if (searchable && search.trim()) params.q = search.trim()

  const query = useQuery({
    queryKey: [...queryKey, params],
    queryFn: () => fetchPage(params),
    // Keeps the previous page visible while the next one loads, so the table
    // does not collapse to a skeleton on every page change.
    placeholderData: keepPreviousData,
  })

  const data = query.data
  const pageKeys = data ? data.items.map(rowKey).map(String) : []
  const selectedOnPage = pageKeys.filter((key) => selected.has(key))
  const allOnPageSelected = pageKeys.length > 0 && selectedOnPage.length === pageKeys.length

  function toggle(key: string) {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  function toggleAllOnPage() {
    setSelected((prev) => {
      const next = new Set(prev)
      if (allOnPageSelected) pageKeys.forEach((key) => next.delete(key))
      else pageKeys.forEach((key) => next.add(key))
      return next
    })
  }

  return (
    <Card title={title} actions={actions} flush>
      {searchable || filters ? (
        <div className="toolbar" style={{ padding: 'var(--space-4) var(--space-4) 0' }}>
          {searchable ? (
            <div className="toolbar__search">
              <label className="sr-only" htmlFor="resource-search">
                {searchPlaceholder}
              </label>
              <input
                id="resource-search"
                type="search"
                placeholder={searchPlaceholder}
                value={search}
                onChange={(event) => {
                  setSearch(event.target.value)
                  // A new search invalidates the current page number.
                  setOffset(0)
                }}
              />
            </div>
          ) : null}
          {filters}
        </div>
      ) : null}

      {selectable && selected.size > 0 ? (
        <div className="toolbar" style={{ padding: 'var(--space-3) var(--space-4) 0' }}>
          <span className="text-sm">{t('list.selectedCount', { count: selected.size })}</span>
          {bulkActions ? bulkActions([...selected], () => setSelected(new Set())) : null}
        </div>
      ) : null}

      {query.isError ? (
        <ErrorState message={errorMessage(query.error)} onRetry={() => void query.refetch()} />
      ) : query.isLoading ? (
        <TableSkeleton rows={6} columns={columns.length} />
      ) : !data || data.items.length === 0 ? (
        <EmptyState
          title={search.trim() ? t('list.noMatches') : emptyTitle}
          text={search.trim() ? t('list.noMatchesText') : emptyText}
          action={search.trim() ? undefined : emptyAction}
        />
      ) : (
        <>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  {selectable ? (
                    <th style={{ width: '2.5rem' }}>
                      <input
                        type="checkbox"
                        aria-label={t('list.selectAll')}
                        checked={allOnPageSelected}
                        onChange={toggleAllOnPage}
                      />
                    </th>
                  ) : null}
                  {columns.map((column) => (
                    <th
                      key={column.key}
                      className={column.numeric ? 'numeric' : undefined}
                      style={column.width ? { width: column.width } : undefined}
                    >
                      {column.header}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {data.items.map((row) => (
                  <tr
                    key={rowKey(row)}
                    onClick={onRowClick ? () => onRowClick(row) : undefined}
                    style={onRowClick ? { cursor: 'pointer' } : undefined}
                  >
                    {selectable ? (
                      <td>
                        {/* The checkbox must not also open the row: a click meant to
                            select would navigate away from the list being built. */}
                        <input
                          type="checkbox"
                          aria-label={t('list.selectRow')}
                          checked={selected.has(String(rowKey(row)))}
                          onClick={(event) => event.stopPropagation()}
                          onChange={() => toggle(String(rowKey(row)))}
                        />
                      </td>
                    ) : null}
                    {columns.map((column) => (
                      <td key={column.key} className={column.numeric ? 'numeric' : undefined}>
                        {column.render(row)}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Pagination
            total={data.total}
            limit={data.limit}
            offset={data.offset}
            onChange={setOffset}
          />
        </>
      )}
    </Card>
  )
}
