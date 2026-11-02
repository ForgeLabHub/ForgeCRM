import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { auditApi } from '../../api/admin'
import { sessionsApi } from '../../api/admin'
import { errorMessage } from '../../api/client'
import { useAuth } from '../../hooks/useAuth'
import { ResourceList, type Column } from '../../components/ResourceList'
import { Alert, Badge, Card, ConfirmDialog, PageHeader, TableSkeleton, Timestamp } from '../../components/ui'
import type { SecurityEvent, Session } from '../../types/api'
import { useT, type TFunction } from '../../i18n'

function buildEventColumns(t: TFunction): Column<SecurityEvent>[] {
  return [
  {
    key: 'type',
    header: t('security.columnEvent'),
    render: (row) => (
      <div>
        <div className="table__primary mono">{row.event_type}</div>
        {row.message ? <div className="table__secondary">{row.message}</div> : null}
      </div>
    ),
  },
  {
    key: 'severity',
    header: t('security.columnSeverity'),
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
    header: t('security.columnUser'),
    render: (row) =>
      row.user_id ? (
        <span className="mono text-sm">{row.user_id.slice(0, 8)}</span>
      ) : (
        <span className="text-subtle">—</span>
      ),
  },
  {
    key: 'ip',
    header: t('security.columnSource'),
    render: (row) => row.ip ?? <span className="text-subtle">—</span>,
    },
    {
    key: 'when',
    header: t('security.columnWhen'),
    render: (row) => <Timestamp value={row.created_at} />,
    },
    ]
    }

export function SecurityPage() {
  const { can } = useAuth()
  const t = useT()
  const [tab, setTab] = useState<'events' | 'sessions'>('events')

  return (
    <>
      <PageHeader title={t('security.title')} meta={t('security.meta')} />

      <div className="row" style={{ marginBottom: 'var(--space-4)' }}>
        <button
          type="button"
          className={tab === 'events' ? 'btn btn--secondary btn--sm' : 'btn btn--ghost btn--sm'}
          onClick={() => setTab('events')}
          aria-pressed={tab === 'events'}
        >
          {t('security.tabEvents')}
          </button>
        <button
          type="button"
          className={tab === 'sessions' ? 'btn btn--secondary btn--sm' : 'btn btn--ghost btn--sm'}
          onClick={() => setTab('sessions')}
          aria-pressed={tab === 'sessions'}
        >
          {t('security.tabSessions')}
          </button>
      </div>

      {tab === 'events' ? (
        can('security.read') ? (
          <ResourceList
            title={t('security.eventsTitle')}
            queryKey={['security-events']}
            fetchPage={(params) => auditApi.securityEvents(params)}
            columns={buildEventColumns(t)}
            rowKey={(row) => row.id}
            emptyTitle={t('security.emptyEventsTitle')}
            emptyText={t('security.emptyEventsText')}
            searchable
            searchPlaceholder={t('security.searchEvents')}
          />
        ) : (
          <Alert tone="warning">{t('security.noPermission')}</Alert>
        )
      ) : (
        <MySessions />
      )}
    </>
  )
}

function buildSessionColumns(t: TFunction): Column<Session>[] {
  return [
  {
    key: 'created',
    header: t('security.columnStarted'),
    render: (row) => <Timestamp value={row.created_at} />,
  },
  {
    key: 'lastSeen',
    header: t('security.columnLastUsed'),
    render: (row) => <Timestamp value={row.last_seen_at} />,
  },
  {
    key: 'expires',
    header: t('security.columnExpires'),
    render: (row) => <Timestamp value={row.expires_at} />,
  },
  {
    key: 'ip',
    header: t('security.columnSource'),
    render: (row) => row.ip ?? <span className="text-subtle">—</span>,
  },
  {
    key: 'agent',
    header: t('security.columnClient'),
    render: (row) => (
      <span className="truncate text-sm" style={{ maxWidth: 260 }} title={row.user_agent ?? ''}>
        {row.user_agent ?? <span className="text-subtle">—</span>}
      </span>
    ),
  },
  {
    key: 'current',
    header: '',
    render: (row) =>
      row.current ? <Badge tone="accent">{t('security.thisSession')}</Badge> : null,
    },
    ]
    }

function MySessions() {
  const t = useT()
  const queryClient = useQueryClient()
  const sessions = useQuery({ queryKey: ['sessions'], queryFn: () => sessionsApi.list() })
  const [pending, setPending] = useState<Session | null>(null)

  const revoke = useMutation({
    mutationFn: (id: string) => sessionsApi.revoke(id),
    onSuccess: () => {
      setPending(null)
      void queryClient.invalidateQueries({ queryKey: ['sessions'] })
    },
  })

  return (
    <Card title={t('security.activeSessions')} flush>
      {revoke.isError ? (
        <div className="card__body">
          <Alert tone="error">{errorMessage(revoke.error)}</Alert>
        </div>
      ) : null}
      {sessions.isError ? (
        <div className="card__body">
          <Alert tone="error">{errorMessage(sessions.error)}</Alert>
        </div>
      ) : sessions.isLoading ? (
        <TableSkeleton rows={3} columns={6} />
      ) : sessions.data?.items.length ? (
        <div className="table-wrap">
          <table className="table--stack">
            <thead>
              <tr>
                {buildSessionColumns(t).map((column) => (
                  <th key={column.key}>{column.header}</th>
                ))}
                <th />
              </tr>
            </thead>
            <tbody>
              {sessions.data.items.map((session) => (
                <tr key={session.id}>
                  {buildSessionColumns(t).map((column) => (
                    /* The header row is hidden once the rows stack into cards on
                       a phone, so each cell carries the header it belongs to. */
                    <td key={column.key} data-label={column.header || undefined}>
                      {column.render(session)}
                    </td>
                  ))}
                  <td>
                    {session.current ? (
                      <span className="text-subtle text-sm">{t('security.current')}</span>
                    ) : (
                      <button
                        type="button"
                        className="btn btn--ghost btn--sm"
                        onClick={() => setPending(session)}
                        disabled={revoke.isPending}
                      >
                        {t('security.revoke')}
                        </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="card__body">
          <p className="text-muted">{t('security.emptySessions')}</p>
        </div>
      )}

      <ConfirmDialog
        open={pending !== null}
        title={t('security.revokeTitle')}
        text={t('security.revokeText')}
        confirmLabel={t('security.revokeConfirm')}
        danger
        busy={revoke.isPending}
        onConfirm={() => {
          if (pending) revoke.mutate(pending.id)
        }}
        onCancel={() => setPending(null)}
      />
    </Card>
  )
}
