import { useState } from 'react'
import { eventsApi } from '../api/integrations'
import type { EventRow } from '../api/integrations'
import { ResourceList, type Column } from '../components/ResourceList'
import { Badge, PageHeader, Timestamp } from '../components/ui'
import { useT } from '../i18n'

export function EventsPage() {
  const t = useT()
  const [detail, setDetail] = useState<EventRow | null>(null)

  const columns: Column<EventRow>[] = [
    {
      key: 'type',
      header: t('events.columnType'),
      render: (row) => <span className="mono">{row.event_type}</span>,
    },
    {
      key: 'source',
      header: t('events.columnSource'),
      render: (row) => (row.source ? <Badge tone="neutral">{row.source}</Badge> : <span className="text-subtle">—</span>),
    },
    {
      key: 'correlation',
      header: t('events.columnCorrelation'),
      render: (row) => (
        <span className="mono text-sm truncate" style={{ maxWidth: 220 }}>
          {row.correlation_id ?? '—'}
        </span>
      ),
    },
    {
      key: 'version',
      header: t('events.columnSchema'),
      render: (row) => (row.schema_version !== undefined ? `v${row.schema_version}` : <span className="text-subtle">—</span>),
    },
    {
      key: 'occurred',
      header: t('events.columnOccurred'),
      render: (row) => <Timestamp value={row.occurred_at} />,
    },
    {
      key: 'actions',
      header: '',
      width: '90px',
      render: (row) => (
        <button
          type="button"
          className="btn btn--ghost btn--sm"
          onClick={(event) => {
            event.stopPropagation()
            setDetail(row)
          }}
        >
          {t('events.payload')}
        </button>
      ),
    },
  ]

  return (
    <>
      <PageHeader title={t('events.title')} meta={t('events.meta')} />
      <ResourceList
        title={t('events.logTitle')}
        queryKey={['events']}
        fetchPage={(params) => eventsApi.list(params)}
        columns={columns}
        rowKey={(row) => row.id}
        emptyTitle={t('events.emptyTitle')}
        emptyText={t('events.emptyText')}
        searchable
        searchPlaceholder={t('events.searchPlaceholder')}
      />

      {detail ? (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="event-payload-title"
          style={{
            position: 'fixed',
            inset: 0,
            background: 'var(--color-overlay)',
            zIndex: 60,
            overflowY: 'auto',
            padding: 'var(--space-5)',
          }}
          onClick={() => setDetail(null)}
        >
          <div className="card" style={{ maxWidth: 720, margin: '0 auto' }} onClick={(e) => e.stopPropagation()}>
            <div className="card__header">
              <h2 id="event-payload-title" className="mono">
                {detail.event_type}
              </h2>
              <Badge tone="neutral">{detail.source ?? t('events.internal')}</Badge>
            </div>
            <div className="card__body stack">
              <dl className="dl">
                <dt>{t('events.eventId')}</dt>
                <dd className="mono truncate">{detail.id}</dd>
                <dt>{t('events.columnOccurred')}</dt>
                <dd>
                  <Timestamp value={detail.occurred_at} />
                </dd>
                <dt>{t('events.correlationId')}</dt>
                <dd className="mono truncate">{detail.correlation_id ?? '—'}</dd>
                <dt>{t('events.schemaVersion')}</dt>
                <dd>{detail.schema_version !== undefined ? `v${detail.schema_version}` : '—'}</dd>
              </dl>
              <div>
                <h3 style={{ marginBottom: 'var(--space-2)' }}>{t('events.payload')}</h3>
                <pre
                  className="mono"
                  style={{
                    background: 'var(--color-bg)',
                    padding: 'var(--space-3)',
                    borderRadius: 'var(--radius-sm)',
                    overflowX: 'auto',
                    maxHeight: 420,
                    margin: 0,
                  }}
                >
                  {JSON.stringify(detail.payload ?? {}, null, 2)}
                </pre>
              </div>
            </div>
            <div className="form-actions" style={{ margin: 0, padding: 'var(--space-4)' }}>
              <button type="button" className="btn btn--secondary" onClick={() => setDetail(null)}>
                {t('common.close')}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </>
  )
}
