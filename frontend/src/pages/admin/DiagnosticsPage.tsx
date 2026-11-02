import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { diagnosticsApi, systemApi } from '../../api/admin'
import { errorMessage } from '../../api/client'
import { useAuth } from '../../hooks/useAuth'
import { useT, type TranslationKey } from '../../i18n'
import { Alert, Badge, Card, ErrorState, PageHeader, TableSkeleton, Timestamp } from '../../components/ui'

/** Renders a duration in seconds as a short human-readable string. */
function formatDuration(seconds: number): string {
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const parts = []
  if (days) parts.push(`${days}d`)
  if (hours) parts.push(`${hours}h`)
  parts.push(`${minutes}m`)
  return parts.join(' ')
}

/**
 * Labels that differ from the raw capability key.
 *
 * A bare `oidc_configured: enabled` reads as "SSO works", which it does not: the
 * flag reports that deployment-level settings are present, not that a provider is
 * configured and enabled. The label says what the flag means so the page cannot
 * mislead. A key with no entry falls back to the key itself.
 */
const CAPABILITY_LABELS: Record<string, TranslationKey> = {
  oidc_configured: 'diagnostics.capOidc',
  tool_api: 'diagnostics.capToolApi',
}

export function DiagnosticsPage() {
  const t = useT()
  const info = useQuery({ queryKey: ['system', 'info'], queryFn: systemApi.info })
  const capabilities = useQuery({
    queryKey: ['system', 'capabilities'],
    queryFn: systemApi.capabilities,
  })
  const metrics = useQuery({ queryKey: ['admin', 'metrics'], queryFn: diagnosticsApi.metrics })
  const diagnostics = useQuery({
    queryKey: ['admin', 'diagnostics'],
    queryFn: diagnosticsApi.diagnostics,
  })

  return (
    <>
      <PageHeader title={t('diagnostics.title')} meta={t('diagnostics.meta')} />

      <div className="stack">
        <Card title={t('diagnostics.systemInfo')}>
          {info.isError ? (
            <Alert tone="error">{errorMessage(info.error)}</Alert>
          ) : info.isLoading ? (
            <TableSkeleton rows={3} columns={2} />
          ) : info.data ? (
            <dl className="dl">
              <dt>{t('diagnostics.version')}</dt>
              <dd>{info.data.version}</dd>
              <dt>{t('diagnostics.environment')}</dt>
              <dd>
                <Badge tone={info.data.environment === 'production' ? 'accent' : 'neutral'}>
                  {info.data.environment}
                </Badge>
              </dd>
              {info.data.commit ? (
                <>
                  <dt>{t('diagnostics.buildCommit')}</dt>
                  <dd className="mono">{info.data.commit}</dd>
                </>
              ) : null}
              {info.data.started_at ? (
                <>
                  <dt>{t('diagnostics.started')}</dt>
                  <dd>{new Date(info.data.started_at).toLocaleString()}</dd>
                </>
              ) : null}
              <dt>{t('diagnostics.database')}</dt>
              <dd>
                <Badge tone={info.data.database === 'ok' ? 'success' : 'danger'}>
                  {info.data.database}
                </Badge>
              </dd>
              <dt>{t('diagnostics.uptime')}</dt>
              <dd>{formatDuration(info.data.uptime_seconds)}</dd>
            </dl>
          ) : null}
        </Card>

        <Card title={t('diagnostics.capabilities')}>
          {capabilities.isError ? (
            <Alert tone="error">{errorMessage(capabilities.error)}</Alert>
          ) : capabilities.isLoading ? (
            <TableSkeleton rows={4} columns={2} />
          ) : capabilities.data ? (
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>{t('diagnostics.feature')}</th>
                    <th>{t('diagnostics.state')}</th>
                  </tr>
                </thead>
                <tbody>
                  {Object.entries(capabilities.data).map(([key, value]) => (
                    <tr key={key}>
                      <td className="table__primary mono">
                        {CAPABILITY_LABELS[key] ? t(CAPABILITY_LABELS[key]) : key}
                      </td>
                      <td>
                        {typeof value === 'boolean' ? (
                          value ? (
                            <Badge tone="success">{t('diagnostics.enabled')}</Badge>
                          ) : (
                            <Badge tone="neutral">{t('diagnostics.disabled')}</Badge>
                          )
                        ) : (
                          <span className="mono">{String(value)}</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
        </Card>

        <JsonCard
          title={t('diagnostics.metrics')}
          query={metrics}
          description={t('diagnostics.metricsHint')}
        />

        <SettingsCard />

        <JsonCard
          title={t('diagnostics.diagnosticsTitle')}
          query={diagnostics}
          description={t('diagnostics.diagnosticsHint')}
        />
      </div>
    </>
  )
}

function JsonCard({
  title,
  query,
  description,
}: {
  title: string
  query: { isError: boolean; isLoading: boolean; data?: unknown; error: unknown; refetch: () => void }
  description: string
}) {
  return (
    <Card title={title}>
      {query.isError ? (
        <ErrorState message={errorMessage(query.error)} onRetry={() => query.refetch()} />
      ) : query.isLoading ? (
        <TableSkeleton rows={4} columns={2} />
      ) : (
        <>
          <p className="text-muted text-sm" style={{ marginBottom: 'var(--space-3)' }}>
            {description}
          </p>
          <pre
            className="mono"
            style={{
              background: 'var(--color-bg)',
              padding: 'var(--space-3)',
              borderRadius: 'var(--radius-sm)',
              overflowX: 'auto',
              maxHeight: 360,
              margin: 0,
            }}
          >
            {JSON.stringify(query.data ?? {}, null, 2)}
          </pre>
        </>
      )}
    </Card>
  )
}

/**
 * Platform settings.
 *
 * `app_settings` is a key/value store the server writes to and reads from. The
 * endpoints existed with no consumer, and the client sent the key in the body
 * while the route requires it as a query parameter, so a save could never
 * succeed. Editing is offered only to a caller holding `system.settings`; a
 * reader without it still sees the current values.
 */
function SettingsCard() {
  const t = useT()
  const { can } = useAuth()
  const canEdit = can('system.settings')
  const queryClient = useQueryClient()
  const [error, setError] = useState<string | null>(null)
  const [editing, setEditing] = useState<string | null>(null)
  const [draft, setDraft] = useState('')

  const settings = useQuery({
    queryKey: ['admin', 'settings'],
    queryFn: diagnosticsApi.settings,
  })

  const save = useMutation({
    mutationFn: (args: { key: string; value: unknown }) =>
      diagnosticsApi.updateSetting(args.key, args.value),
    onSuccess: () => {
      setError(null)
      setEditing(null)
      void queryClient.invalidateQueries({ queryKey: ['admin', 'settings'] })
    },
    onError: (err) => setError(errorMessage(err)),
  })

  const items = settings.data?.items ?? {}

  function beginEdit(key: string, value: unknown) {
    setError(null)
    setEditing(key)
    setDraft(typeof value === 'string' ? value : JSON.stringify(value))
  }

  function commit(key: string) {
    // The value is a JSON document. A bare word is stored as a string; anything
    // that parses as JSON is stored as that value.
    let value: unknown = draft
    try {
      value = JSON.parse(draft)
    } catch {
      value = draft
    }
    save.mutate({ key, value })
  }

  return (
    <Card title={t('diagnostics.platformSettings')}>
      {error ? <Alert tone="error">{error}</Alert> : null}
      {settings.isError ? (
        <ErrorState message={errorMessage(settings.error)} onRetry={() => settings.refetch()} />
      ) : settings.isLoading ? (
        <TableSkeleton rows={3} columns={2} />
      ) : Object.keys(items).length === 0 ? (
        <p className="text-muted text-sm">
          {t('diagnostics.noSettings')}
          {canEdit ? t('diagnostics.noSettingsCanEdit') : ''}
        </p>
      ) : (
        <dl className="dl">
          {Object.entries(items).map(([key, entry]) => (
            <div key={key} className="dl-row">
              <dt className="mono">{key}</dt>
              <dd>
                {editing === key ? (
                  <div className="row" style={{ gap: 'var(--space-2)' }}>
                    <input
                      aria-label={t('diagnostics.valueFor', { key })}
                      value={draft}
                      onChange={(event) => setDraft(event.target.value)}
                    />
                    <button
                      type="button"
                      className="btn btn--primary btn--sm"
                      disabled={save.isPending}
                      onClick={() => commit(key)}
                    >
                      {t('diagnostics.save')}
                      </button>
                    <button
                      type="button"
                      className="btn btn--ghost btn--sm"
                      onClick={() => setEditing(null)}
                    >
                      {t('common.cancel')}
                      </button>
                  </div>
                ) : (
                  <span className="row" style={{ gap: 'var(--space-2)' }}>
                    <span className="mono truncate">{JSON.stringify(entry.value)}</span>
                    <span className="text-subtle text-sm">
                      <Timestamp value={entry.updated_at} />
                    </span>
                    {canEdit ? (
                      <button
                        type="button"
                        className="btn btn--ghost btn--sm"
                        onClick={() => beginEdit(key, entry.value)}
                      >
                        {t('diagnostics.edit')}
                        </button>
                    ) : null}
                  </span>
                )}
              </dd>
            </div>
          ))}
        </dl>
      )}
    </Card>
  )
}
