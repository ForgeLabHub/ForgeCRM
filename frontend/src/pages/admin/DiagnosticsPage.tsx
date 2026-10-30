import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { diagnosticsApi, systemApi } from '../../api/admin'
import { errorMessage } from '../../api/client'
import { useAuth } from '../../hooks/useAuth'
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
 * setting is present but the sign-in flow is not implemented. The label says what
 * the flag means so the page cannot mislead. A key with no entry falls back to the
 * key itself.
 */
const CAPABILITY_LABELS: Record<string, string> = {
  oidc_configured: 'OIDC settings present (sign-in flow not implemented)',
  tool_api: 'Tool API over HTTP (not a full MCP server)',
}

export function DiagnosticsPage() {
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
      <PageHeader
        title="Diagnostics"
        meta="Runtime state of this instance. No secrets are exposed on this page."
      />

      <div className="stack">
        <Card title="System information">
          {info.isError ? (
            <Alert tone="error">{errorMessage(info.error)}</Alert>
          ) : info.isLoading ? (
            <TableSkeleton rows={3} columns={2} />
          ) : info.data ? (
            <dl className="dl">
              <dt>Version</dt>
              <dd>{info.data.version}</dd>
              <dt>Environment</dt>
              <dd>
                <Badge tone={info.data.environment === 'production' ? 'accent' : 'neutral'}>
                  {info.data.environment}
                </Badge>
              </dd>
              {info.data.commit ? (
                <>
                  <dt>Build commit</dt>
                  <dd className="mono">{info.data.commit}</dd>
                </>
              ) : null}
              {info.data.started_at ? (
                <>
                  <dt>Started</dt>
                  <dd>{new Date(info.data.started_at).toLocaleString()}</dd>
                </>
              ) : null}
              <dt>Database</dt>
              <dd>
                <Badge tone={info.data.database === 'ok' ? 'success' : 'danger'}>
                  {info.data.database}
                </Badge>
              </dd>
              <dt>Uptime</dt>
              <dd>{formatDuration(info.data.uptime_seconds)}</dd>
            </dl>
          ) : null}
        </Card>

        <Card title="Capabilities">
          {capabilities.isError ? (
            <Alert tone="error">{errorMessage(capabilities.error)}</Alert>
          ) : capabilities.isLoading ? (
            <TableSkeleton rows={4} columns={2} />
          ) : capabilities.data ? (
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Feature</th>
                    <th>State</th>
                  </tr>
                </thead>
                <tbody>
                  {Object.entries(capabilities.data).map(([key, value]) => (
                    <tr key={key}>
                      <td className="table__primary mono">
                        {CAPABILITY_LABELS[key] ?? key}
                      </td>
                      <td>
                        {typeof value === 'boolean' ? (
                          value ? (
                            <Badge tone="success">enabled</Badge>
                          ) : (
                            <Badge tone="neutral">disabled</Badge>
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
          title="Metrics"
          query={metrics}
          description="Counters and gauges collected since the process started."
        />

        <SettingsCard />

        <JsonCard
          title="Diagnostics"
          query={diagnostics}
          description="Dependency reachability and configuration summary, with sensitive values omitted."
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
    <Card title="Platform settings">
      {error ? <Alert tone="error">{error}</Alert> : null}
      {settings.isError ? (
        <ErrorState message={errorMessage(settings.error)} onRetry={() => settings.refetch()} />
      ) : settings.isLoading ? (
        <TableSkeleton rows={3} columns={2} />
      ) : Object.keys(items).length === 0 ? (
        <p className="text-muted text-sm">
          No platform settings have been written yet.
          {canEdit ? ' Saving one creates it.' : ''}
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
                      aria-label={`Value for ${key}`}
                      value={draft}
                      onChange={(event) => setDraft(event.target.value)}
                    />
                    <button
                      type="button"
                      className="btn btn--primary btn--sm"
                      disabled={save.isPending}
                      onClick={() => commit(key)}
                    >
                      Save
                    </button>
                    <button
                      type="button"
                      className="btn btn--ghost btn--sm"
                      onClick={() => setEditing(null)}
                    >
                      Cancel
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
                        Edit
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
