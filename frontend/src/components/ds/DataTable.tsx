/**
 * DataTable — the shared table for every collection in ForgeCRM.
 *
 * It is presentational: it renders rows it is given and reports sort and
 * selection back. Fetching, filtering and paging live in ListLayout, so a page
 * can compose either the whole list experience or just the table.
 *
 * Accessibility: it is a real <table> with a <th scope="col"> per column, so a
 * screen reader announces the column a cell belongs to. Sorting is exposed
 * through aria-sort rather than only an arrow glyph.
 */
import type { ReactNode } from 'react'
import { useT } from '../../i18n'
import {
  IconArrowDown,
  IconArrowUp,
  IconChevronUpDown,
  IconInbox,
} from '../icons'
import { ErrorState } from '../ui'
import { Checkbox } from './controls'

export interface DataColumn<T> {
  key: string
  header: string
  render: (row: T) => ReactNode
  /** Whether the header is a sort control. The page decides how to sort. */
  sortable?: boolean
  numeric?: boolean
  width?: string
  /** Hidden by default but selectable from the column picker. */
  defaultHidden?: boolean
  /** Never offered for hiding (e.g. the identity column). */
  alwaysVisible?: boolean
}

export type SortDirection = 'asc' | 'desc'

export interface SortState {
  key: string
  direction: SortDirection
}

interface DataTableProps<T> {
  columns: DataColumn<T>[]
  rows: T[]
  rowKey: (row: T) => string
  loading?: boolean
  error?: ReactNode
  onRetry?: () => void
  empty?: ReactNode
  onRowClick?: (row: T) => void
  sort?: SortState | null
  onSortChange?: (sort: SortState) => void
  /** Enables the selection column and the header "select all" control. */
  selectable?: boolean
  selected?: ReadonlySet<string>
  onToggleRow?: (key: string) => void
  onToggleAll?: () => void
  allSelected?: boolean
  stackOnPhone?: boolean
}

function SortGlyph({ state }: { state: SortState | null }) {
  if (!state) return <IconChevronUpDown size={13} />
  return state.direction === 'asc' ? <IconArrowUp size={13} /> : <IconArrowDown size={13} />
}

export function DataTable<T>({
  columns,
  rows,
  rowKey,
  loading = false,
  error,
  onRetry,
  empty,
  onRowClick,
  sort = null,
  onSortChange,
  selectable = false,
  selected,
  onToggleRow,
  onToggleAll,
  allSelected = false,
  stackOnPhone = true,
}: DataTableProps<T>) {
  const t = useT()
  if (error) return <ErrorState message={typeof error === 'string' ? error : ''} onRetry={onRetry} />

  if (loading) {
    return (
      <div className="data-table-wrap" aria-busy="true" aria-label={t('table.loading')}>
        <table className="data-table">
          <thead>
            <tr>
              {selectable ? <th style={{ width: '2.25rem' }} /> : null}
              {columns.map((column) => (
                <th key={column.key} style={column.width ? { width: column.width } : undefined}>
                  {column.header}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {Array.from({ length: 6 }, (_, rowIndex) => (
              <tr key={rowIndex}>
                {selectable ? (
                  <td>
                    <span className="skeleton" style={{ width: 16, height: 16 }} />
                  </td>
                ) : null}
                {columns.map((column, colIndex) => (
                  <td key={column.key}>
                    <span
                      className="skeleton"
                      style={{ width: colIndex === 0 ? '60%' : '40%', height: 12 }}
                    />
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    )
  }

  if (rows.length === 0) {
    return <>{empty ?? <EmptyRows label={t('table.emptyRecords')} />}</>
  }

  return (
    <div className="table-wrap data-table-wrap">
      <table className={stackOnPhone ? 'data-table table--stack' : 'data-table'}>
        <thead>
          <tr>
            {selectable ? (
              <th style={{ width: '2.25rem' }}>
                <Checkbox
                  checked={allSelected}
                  onChange={() => onToggleAll?.()}
                  aria-label={t('table.selectAll')}
                />
              </th>
            ) : null}
            {columns.map((column) => {
              const active = sort?.key === column.key
              return (
                <th
                  key={column.key}
                  scope="col"
                  className={[column.numeric ? 'numeric' : '', column.sortable ? 'is-sortable' : '']
                    .filter(Boolean)
                    .join(' ')}
                  style={column.width ? { width: column.width } : undefined}
                  aria-sort={
                    active ? (sort?.direction === 'asc' ? 'ascending' : 'descending') : undefined
                  }
                  onClick={
                    column.sortable && onSortChange
                      ? () =>
                          onSortChange({
                            key: column.key,
                            direction: active && sort?.direction === 'asc' ? 'desc' : 'asc',
                          })
                      : undefined
                  }
                >
                  {column.sortable ? (
                    <span
                      className={active ? 'data-table__sort data-table__sort--active' : 'data-table__sort'}
                    >
                      {column.header}
                      <SortGlyph state={active ? sort : null} />
                    </span>
                  ) : (
                    column.header
                  )}
                </th>
              )
            })}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => {
            const key = rowKey(row)
            const isSelected = selected?.has(key) ?? false
            return (
              <tr
                key={key}
                className={[onRowClick ? 'is-clickable' : '', isSelected ? 'is-selected' : '']
                  .filter(Boolean)
                  .join(' ')}
                onClick={onRowClick ? () => onRowClick(row) : undefined}
              >
                {selectable ? (
                  <td onClick={(event) => event.stopPropagation()}>
                    <Checkbox
                      checked={isSelected}
                      onChange={() => onToggleRow?.(key)}
                      aria-label={t('table.selectRow')}
                    />
                  </td>
                ) : null}
                {columns.map((column) => (
                  <td
                    key={column.key}
                    className={column.numeric ? 'numeric' : undefined}
                    data-label={stackOnPhone ? column.header : undefined}
                  >
                    {column.render(row)}
                  </td>
                ))}
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function EmptyRows({ label }: { label: string }) {
  return (
    <div className="empty-state">
      <span className="empty-state__icon">
        <IconInbox size={22} />
      </span>
      <div className="empty-state__title">{label}</div>
    </div>
  )
}
