import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { auditApi } from '../../api/admin'
import { sessionsApi } from '../../api/admin'
import { errorMessage } from '../../api/client'
import { ssoApi, type LinkedIdentity } from '../../api/auth'
import { useAuth } from '../../hooks/useAuth'
import { ResourceList, type Column } from '../../components/ResourceList'
import { Alert, Badge, Card, ConfirmDialog, PageHeader, TableSkeleton, Timestamp } from '../../components/ui'
import type { SecurityEvent, Session } from '../../types/api'
import { translationExists, useLocale, useT, type TFunction } from '../../i18n'

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
  const locale = useLocale()
  const [tab, setTab] = useState<'events' | 'sessions' | 'identities'>('events')
  const [params, setParams] = useSearchParams()

  // The callback lands back here with a query flag; read it and clear it so a
  // reload does not re-announce a link that already happened.
  const linked = params.get('identity_linked') === '1'
  const ssoError = params.get('sso_error')
  // The provider returns a short code, never a sentence. An unknown code falls back
  // to a real message rather than printing the raw key.
  const ssoKey = ssoError ? `login.errors.${ssoError}` : null
  const ssoMessage =
    ssoKey && translationExists(locale, ssoKey)
      ? t(ssoKey as 'login.errors.sign_in_failed')
      : ssoError
        ? t('security.identitiesErrorBanner')
        : null
  const dismissBanner = () => {
    const next = new URLSearchParams(params)
    next.delete('identity_linked')
    next.delete('sso_error')
    setParams(next, { replace: true })
  }

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
        <button
          type="button"
          className={tab === 'identities' ? 'btn btn--secondary btn--sm' : 'btn btn--ghost btn--sm'}
          onClick={() => setTab('identities')}
          aria-pressed={tab === 'identities'}
        >
          {t('security.tabIdentities')}
          </button>
      </div>

      {linked ? (
        <Alert tone="success" onDismiss={dismissBanner}>
          {t('security.identitiesLinkedBanner')}
        </Alert>
      ) : null}
      {ssoMessage ? (
        <Alert tone="error" onDismiss={dismissBanner}>
          {ssoMessage}
        </Alert>
      ) : null}

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
      ) : tab === 'sessions' ? (
        <MySessions />
      ) : (
        <MyIdentities />
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

/** The columns of the caller's own linked-identity table. */
function buildIdentityColumns(t: TFunction): Column<LinkedIdentity>[] {
  return [
    {
      key: 'provider',
      header: t('security.identitiesColumnProvider'),
      render: (row) => (
        <div>
          <div>{row.provider_name}</div>
          <div className="text-subtle text-sm mono">{row.provider_key}</div>
        </div>
      ),
    },
    {
      key: 'subject',
      header: t('security.identitiesColumnSubject'),
      render: (row) => (
        <div>
          <div className="mono text-sm" title={row.subject}>
            {row.subject}
          </div>
          {row.email ? <div className="text-subtle text-sm">{row.email}</div> : null}
        </div>
      ),
    },
    {
      key: 'linked',
      header: t('security.identitiesColumnLinked'),
      render: (row) => <Timestamp value={row.created_at} />,
    },
    {
      key: 'lastUsed',
      header: t('security.identitiesColumnLastUsed'),
      render: (row) => <Timestamp value={row.last_login_at} />,
    },
  ]
}

/**
 * The signed-in user's own provider links.
 *
 * The list belongs to the account owner, so no elevated permission is needed: a
 * link is a property of the account, not of the organization. Linking is always an
 * explicit act. The button asks the server for an authorization URL over a
 * CSRF-checked POST, then navigates to the provider; a verified email address is
 * never used to adopt an existing account, so nothing links without this click.
 */
function MyIdentities() {
  const t = useT()
  const queryClient = useQueryClient()
  const identities = useQuery({ queryKey: ['identities'], queryFn: () => ssoApi.identities() })
  const providers = useQuery({ queryKey: ['sso-providers'], queryFn: () => ssoApi.providers() })
  const [pending, setPending] = useState<LinkedIdentity | null>(null)

  const link = useMutation({ mutationFn: (providerKey: string) => ssoApi.link(providerKey) })
  const unlink = useMutation({
    mutationFn: (id: string) => ssoApi.unlink(id),
    onSuccess: () => {
      setPending(null)
      void queryClient.invalidateQueries({ queryKey: ['identities'] })
    },
  })

  const linkedKeys = new Set((identities.data?.items ?? []).map((identity) => identity.provider_key))
  const available = (providers.data?.providers ?? []).filter((p) => !linkedKeys.has(p.key))

  return (
    <Card title={t('security.identitiesTitle')} flush>
      <div className="card__body">
        <p className="text-muted">{t('security.identitiesMeta')}</p>
      </div>

      {link.isError ? (
        <div className="card__body">
          <Alert tone="error">{errorMessage(link.error)}</Alert>
        </div>
      ) : null}
      {unlink.isError ? (
        <div className="card__body">
          <Alert tone="error">{errorMessage(unlink.error)}</Alert>
        </div>
      ) : null}

      {identities.isError ? (
        <div className="card__body">
          <Alert tone="error">{errorMessage(identities.error)}</Alert>
        </div>
      ) : identities.isLoading ? (
        <TableSkeleton rows={2} columns={5} />
      ) : identities.data?.items.length ? (
        <div className="table-wrap">
          <table className="table--stack">
            <thead>
              <tr>
                {buildIdentityColumns(t).map((column) => (
                  <th key={column.key}>{column.header}</th>
                ))}
                <th />
              </tr>
            </thead>
            <tbody>
              {identities.data.items.map((identity) => (
                <tr key={identity.id}>
                  {buildIdentityColumns(t).map((column) => (
                    <td key={column.key} data-label={column.header || undefined}>
                      {column.render(identity)}
                    </td>
                  ))}
                  <td>
                    <button
                      type="button"
                      className="btn btn--ghost btn--sm"
                      onClick={() => setPending(identity)}
                      disabled={unlink.isPending}
                    >
                      {t('security.identitiesUnlink')}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="card__body">
          <p className="text-muted">{t('security.identitiesEmpty')}</p>
        </div>
      )}

      <div className="card__body">
        <h3 className="card__title">{t('security.identitiesLinkTitle')}</h3>
        <p className="text-muted">{t('security.identitiesLinkHint')}</p>
        {providers.isLoading ? (
          <TableSkeleton rows={1} columns={1} />
        ) : available.length ? (
          <div className="row">
            {available.map((provider) => (
              <button
                key={provider.key}
                type="button"
                className="btn btn--secondary btn--sm"
                onClick={() => link.mutate(provider.key)}
                disabled={link.isPending}
              >
                {t('security.identitiesLink')} {provider.display_name}
              </button>
            ))}
          </div>
        ) : (
          <p className="text-subtle">{t('security.identitiesNoProviders')}</p>
        )}
      </div>

      <ConfirmDialog
        open={pending !== null}
        title={t('security.identitiesUnlinkTitle')}
        text={t('security.identitiesUnlinkText', { provider: pending?.provider_name ?? '' })}
        confirmLabel={t('security.identitiesUnlinkConfirm')}
        danger
        busy={unlink.isPending}
        onConfirm={() => {
          if (pending) unlink.mutate(pending.id)
        }}
        onCancel={() => setPending(null)}
      />
    </Card>
  )
}
