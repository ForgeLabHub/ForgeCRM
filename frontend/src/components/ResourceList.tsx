import { useState } from 'react'
import type { ReactNode } from 'react'
import { useQuery, keepPreviousData } from '@tanstack/react-query'
import type { ListResponse, PaginationParams } from '../api/client'
import { errorMessage } from '../api/client'
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
}: ResourceListProps<T>) {
  const [offset, setOffset] = useState(0)
  const [search, setSearch] = useState('')

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

      {query.isError ? (
        <ErrorState message={errorMessage(query.error)} onRetry={() => void query.refetch()} />
      ) : query.isLoading ? (
        <TableSkeleton rows={6} columns={columns.length} />
      ) : !data || data.items.length === 0 ? (
        <EmptyState
          title={search.trim() ? 'No matches' : emptyTitle}
          text={search.trim() ? 'Try a different search term.' : emptyText}
          action={search.trim() ? undefined : emptyAction}
        />
      ) : (
        <>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
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
