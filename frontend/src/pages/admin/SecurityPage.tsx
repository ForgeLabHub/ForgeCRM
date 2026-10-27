import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { auditApi } from '../../api/admin'
import { sessionsApi } from '../../api/admin'
import { errorMessage } from '../../api/client'
import { useAuth } from '../../hooks/useAuth'
import { ResourceList, type Column } from '../../components/ResourceList'
import { Alert, Badge, Card, PageHeader, TableSkeleton, Timestamp } from '../../components/ui'
import type { SecurityEvent, Session } from '../../types/api'

const eventColumns: Column<SecurityEvent>[] = [
  {
    key: 'type',
    header: 'Event',
    render: (row) => (
      <div>
        <div className="table__primary mono">{row.event_type}</div>
        {row.message ? <div className="table__secondary">{row.message}</div> : null}
      </div>
    ),
  },
  {
    key: 'severity',
    header: 'Severity',
    render: (row) => (
      <Badge
        tone={
          row.severity === 'critical' || row.severity === 'high'
            ? 'danger'
            : row.severity === 'medium'
              ? 'warning'
              : 'neutral'
        }
      >
        {row.severity}
      </Badge>
    ),
  },
  {
    key: 'actor',
    header: 'User',
    render: (row) =>
      row.user_id ? (
        <span className="mono text-sm">{row.user_id.slice(0, 8)}</span>
      ) : (
        <span className="text-subtle">—</span>
      ),
  },
  {
    key: 'ip',
    header: 'Source',
    render: (row) => row.ip ?? <span className="text-subtle">—</span>,
  },
  {
    key: 'when',
    header: 'When',
    render: (row) => <Timestamp value={row.created_at} />,
  },
]

export function SecurityPage() {
  const { can } = useAuth()
  const [tab, setTab] = useState<'events' | 'sessions'>('events')

  return (
    <>
      <PageHeader
        title="Security"
        meta="Authentication events and the sessions that are currently active."
      />

      <div className="row" style={{ marginBottom: 'var(--space-4)' }}>
        <button
          type="button"
          className={tab === 'events' ? 'btn btn--secondary btn--sm' : 'btn btn--ghost btn--sm'}
          onClick={() => setTab('events')}
          aria-pressed={tab === 'events'}
        >
          Security events
        </button>
        <button
          type="button"
          className={tab === 'sessions' ? 'btn btn--secondary btn--sm' : 'btn btn--ghost btn--sm'}
          onClick={() => setTab('sessions')}
          aria-pressed={tab === 'sessions'}
        >
          My sessions
        </button>
      </div>

      {tab === 'events' ? (
        can('security.read') ? (
          <ResourceList
            title="Security events"
            queryKey={['security-events']}
            fetchPage={(params) => auditApi.securityEvents(params)}
            columns={eventColumns}
            rowKey={(row) => row.id}
            emptyTitle="No security events"
            emptyText="Sign-ins, failures and account changes are recorded here."
            searchable
            searchPlaceholder="Search by event type…"
          />
        ) : (
          <Alert tone="warning">You do not have permission to read security events.</Alert>
        )
      ) : (
        <MySessions />
      )}
    </>
  )
}

const sessionColumns: Column<Session>[] = [
  {
    key: 'created',
    header: 'Started',
    render: (row) => <Timestamp value={row.created_at} />,
  },
  {
    key: 'lastSeen',
    header: 'Last used',
    render: (row) => <Timestamp value={row.last_seen_at} />,
  },
  {
    key: 'expires',
    header: 'Expires',
    render: (row) => <Timestamp value={row.expires_at} />,
  },
  {
    key: 'ip',
    header: 'Source',
    render: (row) => row.ip ?? <span className="text-subtle">—</span>,
  },
  {
    key: 'agent',
    header: 'Client',
    render: (row) => (
      <span className="truncate text-sm" style={{ maxWidth: 260 }} title={row.user_agent ?? ''}>
        {row.user_agent ?? <span className="text-subtle">—</span>}
      </span>
    ),
  },
  {
    key: 'current',
    header: '',
    render: (row) => (row.current ? <Badge tone="accent">this session</Badge> : null),
  },
]

function MySessions() {
  const sessions = useQuery({ queryKey: ['sessions'], queryFn: () => sessionsApi.list() })

  return (
    <Card title="Active sessions" flush>
      {sessions.isError ? (
        <div className="card__body">
          <Alert tone="error">{errorMessage(sessions.error)}</Alert>
        </div>
      ) : sessions.isLoading ? (
        <TableSkeleton rows={3} columns={5} />
      ) : sessions.data?.items.length ? (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                {sessionColumns.map((column) => (
                  <th key={column.key}>{column.header}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {sessions.data.items.map((session) => (
                <tr key={session.id}>
                  {sessionColumns.map((column) => (
                    <td key={column.key}>{column.render(session)}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="card__body">
          <p className="text-muted">No active sessions.</p>
        </div>
      )}
    </Card>
  )
}
