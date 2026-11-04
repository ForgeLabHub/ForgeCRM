/**
 * ListLayout — the shared collection experience.
 *
 * Every CRM list (contacts, accounts, deals, tasks, tickets) is the same shape:
 * a searchable, sortable, paginated table with optional filters, a bulk-action
 * bar and a column picker. Centralising it means the pages behave identically,
 * so a user does not relearn the table when they move between sections.
 *
 * Sorting is sent to the server (`sort` / `-sort`), which owns the data and the
 * allowlist of sortable columns; the table never sorts a page of rows locally,
 * because that would order a slice rather than the collection.
 */
import { useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import type { ListResponse, PaginationParams } from '../../api/client'
import { errorMessage } from '../../api/client'
import { useT } from '../../i18n'
import { IconColumns, IconFilter, IconInbox } from '../icons'
import { Button, IconButton, SearchInput } from './controls'
import { DataTable, type DataColumn, type SortState } from './DataTable'
import { DropdownMenu } from './overlays'

export interface ListLayoutProps<T> {
  queryKey: readonly unknown[]
  fetchPage: (params: PaginationParams) => Promise<ListResponse<T>>
  columns: DataColumn<T>[]
  rowKey: (row: T) => string
  onRowClick?: (row: T) => void
  title?: string
  /** Extra filters rendered inline in the filter bar. */
  filters?: ReactNode
  searchable?: boolean
  searchPlaceholder?: string
  actions?: ReactNode
  emptyTitle?: string
  emptyText?: string
  emptyAction?: ReactNode
  pageSize?: number
  /** Enables selection; receives the selected keys and a clear callback. */
  bulkActions?: (selected: string[], clear: () => void) => ReactNode
  /** Adds a "Columns" picker for the hideable columns. */
  columnPicker?: boolean
  stackOnPhone?: boolean
}

export function ListLayout<T>({
  queryKey,
  fetchPage,
  columns,
  rowKey,
  onRowClick,
  title,
  filters,
  searchable = true,
  searchPlaceholder,
  actions,
  emptyTitle,
  emptyText,
  emptyAction,
  pageSize = 25,
  bulkActions,
  columnPicker = true,
  stackOnPhone = true,
}: ListLayoutProps<T>) {
  const t = useT()
  const placeholder = searchPlaceholder ?? t('table.search')
  const emptyHeading = emptyTitle ?? t('table.emptyTitle')
  const [offset, setOffset] = useState(0)
  const [search, setSearch] = useState('')
  const [sort, setSort] = useState<SortState | null>(null)
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const [hidden, setHidden] = useState<ReadonlySet<string>>(
    () => new Set(columns.filter((column) => column.defaultHidden).map((column) => column.key)),
  )

  const selectable = Boolean(bulkActions)

  // A selection is confined to the page it was made on. A new page or search
  // term drops it, because the rows it referred to are no longer on screen and
  // a bulk action must never apply to records the user cannot see.
  useEffect(() => {
    setSelected(new Set())
  }, [offset, search])

  const params = useMemo<PaginationParams>(() => {
    const next: PaginationParams = { limit: pageSize, offset }
    if (search.trim()) next.q = search.trim()
    if (sort) next.sort = sort.direction === 'desc' ? `-${sort.key}` : sort.key
    return next
  }, [pageSize, offset, search, sort])

  const query = useQuery({
    queryKey: [...queryKey, params],
    queryFn: () => fetchPage(params),
    placeholderData: keepPreviousData,
  })

  const data = query.data
  const visibleColumns = columns.filter(
    (column) => column.alwaysVisible || !hidden.has(column.key),
  )

  const pageKeys = data ? data.items.map(rowKey) : []
  const allSelected = pageKeys.length > 0 && pageKeys.every((key) => selected.has(key))

  function toggleRow(key: string) {
    setSelected((current) => {
      const next = new Set(current)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  function toggleAll() {
    setSelected((current) => {
      const next = new Set(current)
      if (allSelected) pageKeys.forEach((key) => next.delete(key))
      else pageKeys.forEach((key) => next.add(key))
      return next
    })
  }

  function toggleColumn(key: string) {
    setHidden((current) => {
      const next = new Set(current)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  const hideable = columns.filter((column) => !column.alwaysVisible)
  const hasToolbar = searchable || Boolean(filters) || columnPicker || (Boolean(actions) && !title)
  const isFiltered = search.trim().length > 0

  return (
    <section className="card">
      {title ? (
        <div className="card__header">
          <h2>{title}</h2>
          {actions}
        </div>
      ) : null}
      {hasToolbar ? (
        <div className="filter-bar">
          {searchable ? (
            <SearchInput
              value={search}
              placeholder={placeholder}
              aria-label={placeholder}
              className="toolbar__search"
              onChange={(event) => {
                setSearch(event.target.value)
                setOffset(0)
              }}
              onClear={() => {
                setSearch('')
                setOffset(0)
              }}
            />
          ) : null}
          {filters}
          <div className="filter-bar__spacer" />
          {data && !query.isLoading ? (
            <span className="filter-bar__count tabular">
              {data.total} {data.total === 1 ? t('table.record') : t('table.records')}
            </span>
          ) : null}
          {columnPicker && hideable.length > 0 ? (
            <DropdownMenu
              items={hideable.map((column) => ({
                label: t(hidden.has(column.key) ? 'table.showColumn' : 'table.hideColumn', {
                  column: column.header,
                }),
                icon: <IconColumns size={15} />,
                onSelect: () => toggleColumn(column.key),
              }))}
              trigger={({ toggle }) => (
                <IconButton label={t('table.columns')} onClick={toggle}>
                  <IconColumns size={16} />
                </IconButton>
              )}
            />
          ) : null}
          {actions && !title ? actions : null}
        </div>
      ) : null}

      {selectable && selected.size > 0 ? (
        <div className="filter-bar" style={{ background: 'var(--color-accent-soft)' }}>
          <span className="text-sm" style={{ fontWeight: 520 }}>
            {t('table.selected', { count: selected.size })}
          </span>
          <div className="filter-bar__spacer" />
          {bulkActions ? bulkActions([...selected], () => setSelected(new Set())) : null}
        </div>
      ) : null}

      <DataTable
        columns={visibleColumns}
        rows={data?.items ?? []}
        rowKey={rowKey}
        loading={query.isLoading}
        error={query.isError ? errorMessage(query.error) : undefined}
        onRetry={() => void query.refetch()}
        onRowClick={onRowClick}
        sort={sort}
        onSortChange={(next) => {
          setSort(next)
          setOffset(0)
        }}
        selectable={selectable}
        selected={selected}
        onToggleRow={toggleRow}
        onToggleAll={toggleAll}
        allSelected={allSelected}
        stackOnPhone={stackOnPhone}
        empty={
          <div className="empty-state">
            <span className="empty-state__icon">
              <IconInbox size={22} />
            </span>
            <div className="empty-state__title">
              {isFiltered ? t('table.noMatches') : emptyHeading}
            </div>
            <p className="empty-state__text">
              {isFiltered ? t('table.noMatchesText') : emptyText}
            </p>
            {!isFiltered && emptyAction ? emptyAction : null}
          </div>
        }
      />

      {data && data.total > 0 ? (
        <div className="pagination">
          <span className="tabular">
            {t('table.range', {
              from: offset + 1,
              to: Math.min(offset + pageSize, data.total),
              total: data.total,
            })}
          </span>
          <div className="pagination__controls">
            <Button
              size="sm"
              disabled={offset === 0}
              onClick={() => setOffset(Math.max(0, offset - pageSize))}
            >
              {t('table.previous')}
            </Button>
            <Button
              size="sm"
              disabled={offset + pageSize >= data.total}
              onClick={() => setOffset(offset + pageSize)}
            >
              {t('table.next')}
            </Button>
          </div>
        </div>
      ) : null}
    </section>
  )
}

/** Re-export so pages import the filter glyph from one place. */
export { IconFilter }
