import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { auditApi, diagnosticsApi } from '../../api/admin'
import type { LogLine } from '../../api/admin'
import { errorMessage } from '../../api/client'
import { ResourceList, type Column } from '../../components/ResourceList'
import { Alert, Badge, Card, PageHeader, Timestamp } from '../../components/ui'
import type { AuditEntry } from '../../types/api'

const auditColumns: Column<AuditEntry>[] = [
  {
    key: 'action',
    header: 'Action',
    render: (row) => (
      <div>
        <div className="table__primary mono">{row.action}</div>
        {row.entity_type ? (
          <div className="table__secondary">
            {row.entity_type}
            {row.entity_id ? ` · ${row.entity_id.slice(0, 8)}` : ''}
          </div>
        ) : null}
      </div>
    ),
  },
  {
    key: 'actor',
    header: 'Actor',
    render: (row) =>
      row.actor_user_id ? (
        <span className="mono text-sm">{row.actor_user_id.slice(0, 8)}</span>
      ) : (
        <Badge tone="neutral">system</Badge>
      ),
  },
  {
    key: 'ip',
    header: 'Source',
    render: (row) => row.ip ?? <span className="text-subtle">—</span>,
  },
  {
    key: 'correlation',
    header: 'Correlation',
    render: (row) => (
      <span className="mono text-sm truncate" style={{ maxWidth: 180 }}>
        {row.correlation_id ?? '—'}
      </span>
    ),
  },
  {
    key: 'when',
    header: 'When',
    render: (row) => <Timestamp value={row.created_at} />,
  },
]

export function AuditPage() {
  const [tab, setTab] = useState<'audit' | 'logs'>('audit')

  return (
    <>
      <PageHeader
        title="Audit & logs"
        meta="Who changed what, and the application's own structured log."
      />

      <div className="row" style={{ marginBottom: 'var(--space-4)' }}>
        <button
          type="button"
          className={tab === 'audit' ? 'btn btn--secondary btn--sm' : 'btn btn--ghost btn--sm'}
          onClick={() => setTab('audit')}
          aria-pressed={tab === 'audit'}
        >
          Audit trail
        </button>
        <button
          type="button"
          className={tab === 'logs' ? 'btn btn--secondary btn--sm' : 'btn btn--ghost btn--sm'}
          onClick={() => setTab('logs')}
          aria-pressed={tab === 'logs'}
        >
          Application log
        </button>
      </div>

      {tab === 'audit' ? (
        <ResourceList
          title="Audit trail"
          queryKey={['audit']}
          fetchPage={(params) => auditApi.list(params)}
          columns={auditColumns}
          rowKey={(row) => row.id}
          emptyTitle="No audit entries"
          emptyText="Changes to records, roles and configuration are recorded here."
          searchable
          searchPlaceholder="Search by action…"
        />
      ) : (
        <ApplicationLog />
      )}
    </>
  )
}

const logColumns: Column<LogLine>[] = [
  {
    key: 'level',
    header: 'Level',
    render: (row) => (
      <Badge
        tone={
          row.level === 'error'
            ? 'danger'
            : row.level === 'warn'
              ? 'warning'
              : row.level === 'debug'
                ? 'neutral'
                : 'accent'
        }
      >
        {row.level}
      </Badge>
    ),
  },
  {
    key: 'message',
    header: 'Message',
    render: (row) => (
      <div>
        <div className="table__primary">{row.message}</div>
        {row.component ? <div className="table__secondary">{row.component}</div> : null}
      </div>
    ),
  },
  {
    key: 'correlation',
    header: 'Correlation',
    render: (row) => (
      <span className="mono text-sm truncate" style={{ maxWidth: 180 }}>
        {row.correlation_id ?? '—'}
      </span>
    ),
  },
  {
    key: 'time',
    header: 'When',
    render: (row) => <Timestamp value={row.time} />,
  },
]

function ApplicationLog() {
  const [level, setLevel] = useState('')

  // A single cheap request decides whether the log is reachable at all, so a
  // permission problem shows as a clear message rather than an empty table.
  const logs = useQuery({
    queryKey: ['logs', 'probe', level],
    queryFn: () => diagnosticsApi.logs({ limit: 1, level: level || undefined }),
  })

  return (
    <>
      <Alert tone="info">
        Logs are redacted at write time: passwords, tokens and full personal data never reach them.
      </Alert>

      <div style={{ marginTop: 'var(--space-4)' }}>
        {logs.isError ? (
          <Card title="Application log">
            <Alert tone="error">{errorMessage(logs.error)}</Alert>
          </Card>
        ) : (
          <ResourceList
            title="Application log"
            queryKey={['logs', level]}
            fetchPage={(params) => diagnosticsApi.logs({ ...params, level: level || undefined })}
            columns={logColumns}
            rowKey={(row) => `${row.time}-${row.message}`}
            emptyTitle="No log entries"
            emptyText="Nothing has been logged at this level."
            searchable
            searchPlaceholder="Search messages…"
            filters={
              <label className="field" style={{ maxWidth: 160 }}>
                <span className="field__label">Level</span>
                <select value={level} onChange={(event) => setLevel(event.target.value)}>
                  <option value="">All levels</option>
                  {['debug', 'info', 'warn', 'error'].map((item) => (
                    <option key={item} value={item}>
                      {item}
                    </option>
                  ))}
                </select>
              </label>
            }
          />
        )}
      </div>
    </>
  )
}
