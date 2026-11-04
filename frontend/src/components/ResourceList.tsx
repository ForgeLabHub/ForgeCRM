import type { ReactNode } from 'react'
import type { ListResponse, PaginationParams } from '../api/client'
import { ListLayout } from './ds/ListLayout'
import type { DataColumn } from './ds/DataTable'

/**
 * A column in a resource list. Kept as an alias of the design-system column so
 * the many pages that already declare `Column<T>` arrays keep compiling, while
 * the table underneath is the single shared implementation.
 */
export type Column<T> = DataColumn<T>

interface ResourceListProps<T> {
  /** Stable key for the query cache. */
  queryKey: readonly unknown[]
  /** Fetches one page. Must accept limit, offset, the search term and sort. */
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
  /** Rendered in the toolbar, typically a create button. */
  actions?: ReactNode
  title?: string
  pageSize?: number
  /**
   * Adds a checkbox column and renders the bulk bar when rows are selected.
   * Receives the selected row keys and a function to clear the selection.
   */
  bulkActions?: (selected: string[], clear: () => void) => ReactNode
  /**
   * Stacks each row into a labelled card on a phone instead of scrolling the
   * table sideways. On by default: a list is the common case and a sideways
   * table hides exactly the column a phone user came to read. Turn it off for a
   * two-column key/value table where a card per row would be noise.
   */
  stackOnPhone?: boolean
}

/**
 * The collection table every CRM page uses.
 *
 * It is a thin adapter over ListLayout: the behaviour (search, sort, paging,
 * selection, column picker, loading and empty states) lives in one place, so
 * every list in the product behaves the same and a fix applies everywhere. A
 * page supplies only its columns and how to fetch a page.
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
  searchPlaceholder,
  filters,
  actions,
  title,
  pageSize = 25,
  bulkActions,
  stackOnPhone = true,
}: ResourceListProps<T>) {
  return (
    <ListLayout
      queryKey={queryKey}
      fetchPage={fetchPage}
      columns={columns}
      rowKey={(row) => String(rowKey(row))}
      onRowClick={onRowClick}
      title={title}
      filters={filters}
      searchable={searchable}
      searchPlaceholder={searchPlaceholder}
      actions={actions}
      emptyTitle={emptyTitle}
      emptyText={emptyText}
      emptyAction={emptyAction}
      pageSize={pageSize}
      bulkActions={bulkActions}
      stackOnPhone={stackOnPhone}
    />
  )
}
