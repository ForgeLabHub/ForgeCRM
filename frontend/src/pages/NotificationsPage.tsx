import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router-dom'
import { notificationsApi } from '../api/crm'
import { errorMessage } from '../api/client'
import type { Notification } from '../types/api'
import { ResourceList, type Column } from '../components/ResourceList'
import { Alert, Badge, PageHeader, Timestamp } from '../components/ui'
import { useT } from '../i18n'

/**
 * The notification inbox.
 *
 * Notifications are written by workflow `notify` steps, but nothing in the
 * product listed them: the server implemented list and mark-read and the API
 * client wrapped both, yet no page consumed them, so a notification a workflow
 * raised could not be seen or cleared from the interface.
 */
export function NotificationsPage() {
  const t = useT()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [unreadOnly, setUnreadOnly] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const list = useQuery({
    queryKey: ['notifications', 'unread-count'],
    queryFn: () => notificationsApi.list({ unread_only: true, limit: 1 }),
  })
  const unread = list.data?.total ?? 0

  const markRead = useMutation({
    mutationFn: (id: string) => notificationsApi.markRead(id),
    onSuccess: () => {
      setError(null)
      void queryClient.invalidateQueries({ queryKey: ['notifications'] })
    },
    onError: (err) => setError(errorMessage(err)),
  })

  const columns: Column<Notification>[] = [
    {
      key: 'status',
      header: '',
      width: '90px',
      render: (row) =>
        row.read_at ? (
          <Badge tone="neutral">{t('notifications.read')}</Badge>
        ) : (
          <Badge tone="accent">{t('notifications.unread')}</Badge>
        ),
    },
    {
      key: 'title',
      header: t('notifications.columnTitle'),
      render: (row) => (
        <span style={{ fontWeight: row.read_at ? 400 : 600 }}>{row.title}</span>
      ),
    },
    {
      key: 'type',
      header: t('notifications.columnType'),
      render: (row) => <span className="mono text-sm">{row.type}</span>,
    },
    {
      key: 'created',
      header: t('notifications.columnReceived'),
      render: (row) => <Timestamp value={row.created_at} />,
    },
    {
      key: 'actions',
      header: '',
      width: '110px',
      render: (row) =>
        row.read_at ? null : (
          <button
            type="button"
            className="btn btn--ghost btn--sm"
            disabled={markRead.isPending}
            onClick={(event) => {
              event.stopPropagation()
              markRead.mutate(row.id)
            }}
          >
            {t('notifications.markRead')}
          </button>
        ),
    },
  ]

  return (
    <>
      <PageHeader title={t('notifications.title')} meta={t('notifications.meta')} />
      {error ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="error">{error}</Alert>
        </div>
      ) : null}
      <ResourceList
        title={
          unread > 0 ? t('notifications.inboxUnread', { count: unread }) : t('notifications.inbox')
        }
        queryKey={['notifications', { unreadOnly }]}
        fetchPage={(params) =>
          notificationsApi.list({ ...params, unread_only: unreadOnly || undefined })
        }
        columns={columns}
        rowKey={(row) => row.id}
        emptyTitle={unreadOnly ? t('notifications.emptyUnread') : t('notifications.emptyTitle')}
        emptyText={t('notifications.emptyText')}
        filters={
          <label className="row" style={{ gap: 'var(--space-2)' }}>
            <input
              type="checkbox"
              checked={unreadOnly}
              onChange={(event) => setUnreadOnly(event.target.checked)}
            />
            <span className="text-sm">{t('notifications.unreadOnly')}</span>
          </label>
        }
        onRowClick={(row) => {
          if (!row.entity_type || !row.entity_id) return
          const path = entityPath(row.entity_type, row.entity_id)
          if (path) navigate(path)
        }}
      />
      <p className="text-subtle text-sm" style={{ marginTop: 'var(--space-3)' }}>
        {t('notifications.approvalsPrompt')}
        <Link to="/approvals">{t('notifications.approvalsLink')}</Link>.
      </p>
    </>
  )
}

function entityPath(entityType: string, entityId: string): string | null {
  switch (entityType) {
    case 'account':
      return `/accounts/${entityId}`
    case 'contact':
      return `/contacts/${entityId}`
    case 'deal':
      return `/deals/${entityId}`
    case 'task':
      return `/tasks/${entityId}`
    case 'ticket':
      return `/tickets/${entityId}`
    default:
      return null
  }
}
