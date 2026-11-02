import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { jobsApi } from '../api/admin'
import { errorMessage } from '../api/client'
import type { Job } from '../types/api'
import { useAuth } from '../hooks/useAuth'
import { ResourceList, type Column } from '../components/ResourceList'
import { Alert, PageHeader, Stat, StatusBadge, Timestamp } from '../components/ui'
import { useT } from '../i18n'

export function JobsPage() {
  const t = useT()
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [statusFilter, setStatusFilter] = useState('')
  const [detail, setDetail] = useState<Job | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const stats = useQuery({ queryKey: ['jobs', 'stats'], queryFn: () => jobsApi.stats() })

  const cancel = useMutation({
    mutationFn: (id: string) => jobsApi.cancel(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['jobs'] })
      setActionError(null)
    },
    onError: (error) => setActionError(errorMessage(error)),
  })

  const retry = useMutation({
    mutationFn: (id: string) => jobsApi.retry(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['jobs'] })
      setActionError(null)
    },
    onError: (error) => setActionError(errorMessage(error)),
  })

  const columns: Column<Job>[] = [
    {
      key: 'kind',
      header: t('jobs.columnJob'),
      render: (row) => (
        <div>
          <div className="table__primary">{row.kind}</div>
          <div className="table__secondary mono truncate" style={{ maxWidth: 260 }}>
            {row.id}
          </div>
        </div>
      ),
    },
    { key: 'status', header: t('common.status'), render: (row) => <StatusBadge status={row.status} /> },
    {
      key: 'attempts',
      header: t('jobs.columnAttempts'),
      numeric: true,
      render: (row) => `${row.attempts}/${row.max_attempts}`,
    },
    {
      key: 'runAt',
      header: t('jobs.columnScheduled'),
      render: (row) => <Timestamp value={row.run_after} />,
    },
    {
      key: 'finished',
      header: t('jobs.columnFinished'),
      render: (row) => <Timestamp value={row.finished_at} />,
    },
    {
      key: 'actions',
      header: '',
      width: '200px',
      render: (row) => (
        <div className="row">
          {can('jobs.manage') && (row.status === 'queued' || row.status === 'running') ? (
            <button
              type="button"
              className="btn btn--secondary btn--sm"
              onClick={(event) => {
                event.stopPropagation()
                cancel.mutate(row.id)
              }}
              disabled={cancel.isPending}
            >
              {t('jobs.cancel')}
            </button>
          ) : null}
          {can('jobs.manage') && row.status === 'failed' ? (
            <button
              type="button"
              className="btn btn--secondary btn--sm"
              onClick={(event) => {
                event.stopPropagation()
                retry.mutate(row.id)
              }}
              disabled={retry.isPending}
            >
              {t('jobs.retry')}
            </button>
          ) : null}
          <button
            type="button"
            className="btn btn--ghost btn--sm"
            onClick={(event) => {
              event.stopPropagation()
              setDetail(row)
            }}
          >
            {t('jobs.details')}
          </button>
        </div>
      ),
    },
  ]

  return (
    <>
      <PageHeader title={t('jobs.title')} meta={t('jobs.meta')} />

      {actionError ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="error">{actionError}</Alert>
        </div>
      ) : null}

      {stats.data ? (
        <div className="grid grid--stats" style={{ marginBottom: 'var(--space-5)' }}>
          {Object.entries(stats.data.by_status)
            .slice(0, 6)
            .map(([key, value]) => (
              <Stat key={key} label={key.replace(/_/g, ' ')} value={value} />
            ))}
        </div>
      ) : null}

      <ResourceList
        title={t('jobs.queueTitle')}
        queryKey={['jobs', statusFilter]}
        fetchPage={(params) => jobsApi.list({ ...params, status: statusFilter || undefined })}
        columns={columns}
        rowKey={(row) => row.id}
        emptyTitle={t('jobs.emptyTitle')}
        emptyText={t('jobs.emptyText')}
        searchable
        searchPlaceholder={t('jobs.searchPlaceholder')}
        filters={
          <label className="field" style={{ maxWidth: 180 }}>
            <span className="field__label">{t('jobs.statusField')}</span>
            <select value={statusFilter} onChange={(event) => setStatusFilter(event.target.value)}>
              <option value="">{t('common.allStatuses')}</option>
              {['queued', 'running', 'succeeded', 'failed', 'cancelled'].map((status) => (
                <option key={status} value={status}>
                  {t(`status.${status}` as Parameters<typeof t>[0])}
                </option>
              ))}
            </select>
          </label>
        }
      />

      {detail ? (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="job-detail-title"
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
          <div className="card" style={{ maxWidth: 620, margin: '0 auto' }} onClick={(e) => e.stopPropagation()}>
            <div className="card__header">
              <h2 id="job-detail-title">{detail.kind}</h2>
              <StatusBadge status={detail.status} />
            </div>
            <div className="card__body">
              <dl className="dl">
                <dt>{t('jobs.jobId')}</dt>
                <dd className="mono truncate">{detail.id}</dd>
                <dt>{t('jobs.columnAttempts')}</dt>
                <dd>{t('jobs.attemptsOf', { done: detail.attempts, total: detail.max_attempts })}</dd>
                <dt>{t('jobs.scheduledFor')}</dt>
                <dd>
                  <Timestamp value={detail.run_after} />
                </dd>
                <dt>{t('common.created')}</dt>
                <dd>
                  <Timestamp value={detail.created_at} />
                </dd>
                <dt>{t('jobs.columnFinished')}</dt>
                <dd>
                  <Timestamp value={detail.finished_at} />
                </dd>
                {detail.last_error ? (
                  <>
                    <dt>{t('jobs.lastError')}</dt>
                    <dd className="text-danger">{detail.last_error}</dd>
                  </>
                ) : null}
              </dl>
            </div>
            <div className="form-actions" style={{ margin: 0, padding: 'var(--space-4)' }}>
              <button type="button" className="btn btn--secondary" onClick={() => setDetail(null)}>
                {t('common.close')}
              </button>
            </div>
          </div>
        </div>
      ) : null}

      {stats.isError ? (
        <div style={{ marginTop: 'var(--space-4)' }}>
          <Alert tone="warning">{t('jobs.statsError', { message: errorMessage(stats.error) })}</Alert>
        </div>
      ) : null}
    </>
  )
}
