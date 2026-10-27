import { useQuery } from '@tanstack/react-query'
import { diagnosticsApi, systemApi } from '../../api/admin'
import { errorMessage } from '../../api/client'
import { Alert, Badge, Card, ErrorState, PageHeader, TableSkeleton } from '../../components/ui'

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
                      <td className="table__primary mono">{key}</td>
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
