import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { jobsApi } from '../api/admin'
import { errorMessage } from '../api/client'
import type { Job } from '../types/api'
import { useAuth } from '../hooks/useAuth'
import { ResourceList, type Column } from '../components/ResourceList'
import { Alert, PageHeader, Stat, StatusBadge, Timestamp } from '../components/ui'

export function JobsPage() {
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
      header: 'Job',
      render: (row) => (
        <div>
          <div className="table__primary">{row.kind}</div>
          <div className="table__secondary mono truncate" style={{ maxWidth: 260 }}>
            {row.id}
          </div>
        </div>
      ),
    },
    { key: 'status', header: 'Status', render: (row) => <StatusBadge status={row.status} /> },
    {
      key: 'attempts',
      header: 'Attempts',
      numeric: true,
      render: (row) => `${row.attempts}/${row.max_attempts}`,
    },
    {
      key: 'runAt',
      header: 'Scheduled',
      render: (row) => <Timestamp value={row.run_at} />,
    },
    {
      key: 'finished',
      header: 'Finished',
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
              Cancel
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
              Retry
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
            Details
          </button>
        </div>
      ),
    },
  ]

  return (
    <>
      <PageHeader
        title="Background jobs"
        meta="Queued work: imports, exports, integration syncs and notification delivery."
      />

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
        title="Job queue"
        queryKey={['jobs', statusFilter]}
        fetchPage={(params) => jobsApi.list({ ...params, status: statusFilter || undefined })}
        columns={columns}
        rowKey={(row) => row.id}
        emptyTitle="No jobs"
        emptyText="Background work appears here when it is queued."
        searchable
        searchPlaceholder="Search jobs…"
        filters={
          <label className="field" style={{ maxWidth: 180 }}>
            <span className="field__label">Status</span>
            <select value={statusFilter} onChange={(event) => setStatusFilter(event.target.value)}>
              <option value="">All statuses</option>
              {['queued', 'running', 'succeeded', 'failed', 'cancelled'].map((status) => (
                <option key={status} value={status}>
                  {status}
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
            background: 'rgba(16, 24, 40, 0.45)',
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
                <dt>Job ID</dt>
                <dd className="mono truncate">{detail.id}</dd>
                <dt>Attempts</dt>
                <dd>
                  {detail.attempts} of {detail.max_attempts}
                </dd>
                <dt>Scheduled for</dt>
                <dd>
                  <Timestamp value={detail.run_at} />
                </dd>
                <dt>Created</dt>
                <dd>
                  <Timestamp value={detail.created_at} />
                </dd>
                <dt>Finished</dt>
                <dd>
                  <Timestamp value={detail.finished_at} />
                </dd>
                {detail.last_error ? (
                  <>
                    <dt>Last error</dt>
                    <dd className="text-danger">{detail.last_error}</dd>
                  </>
                ) : null}
              </dl>
            </div>
            <div className="form-actions" style={{ margin: 0, padding: 'var(--space-4)' }}>
              <button type="button" className="btn btn--secondary" onClick={() => setDetail(null)}>
                Close
              </button>
            </div>
          </div>
        </div>
      ) : null}

      {stats.isError ? (
        <div style={{ marginTop: 'var(--space-4)' }}>
          <Alert tone="warning">
            Could not load job statistics: {errorMessage(stats.error)}
          </Alert>
        </div>
      ) : null}
    </>
  )
}
