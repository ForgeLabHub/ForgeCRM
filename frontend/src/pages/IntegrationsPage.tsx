import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { integrationsApi } from '../api/integrations'
import { errorMessage, fieldErrors } from '../api/client'
import type { Integration } from '../types/api'
import { useAuth } from '../hooks/useAuth'
import { ResourceList, type Column } from '../components/ResourceList'
import {
  Alert,
  Badge,
  ConfirmDialog,
  Field,
  PageHeader,
  StatusBadge,
  Timestamp,
} from '../components/ui'

export function IntegrationsPage() {
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [detail, setDetail] = useState<Integration | null>(null)
  const [testResult, setTestResult] = useState<Record<string, { ok: boolean; message?: string }>>({})
  const [actionError, setActionError] = useState<string | null>(null)

  const test = useMutation({
    mutationFn: (id: string) => integrationsApi.test(id),
    onSuccess: (result, id) => {
      setTestResult((previous) => ({ ...previous, [id]: result }))
      void queryClient.invalidateQueries({ queryKey: ['integrations'] })
    },
    onError: (error, id) => {
      setTestResult((previous) => ({ ...previous, [id]: { ok: false, message: errorMessage(error) } }))
    },
  })

  const setStatus = useMutation({
    mutationFn: ({ id, status }: { id: string; status: 'active' | 'paused' | 'revoked' }) =>
      integrationsApi.setStatus(id, status),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['integrations'] })
      setActionError(null)
    },
    onError: (error) => setActionError(errorMessage(error)),
  })

  const columns: Column<Integration>[] = [
    {
      key: 'name',
      header: 'Integration',
      render: (row) => (
        <div>
          <div className="table__primary">{row.name}</div>
          <div className="table__secondary">
            {row.kind}
            {row.direction ? ` · ${row.direction}` : ''}
          </div>
        </div>
      ),
    },
    { key: 'status', header: 'Status', render: (row) => <StatusBadge status={row.status} /> },
    {
      key: 'lastChecked',
      header: 'Last checked',
      render: (row) => <Timestamp value={row.last_test_at} />,
    },
    {
      key: 'lastSync',
      header: 'Last sync',
      render: (row) => <Timestamp value={row.last_sync_at} />,
    },
    {
      key: 'connection',
      header: 'Connection',
      render: (row) => {
        const result = testResult[row.id]
        if (!result) return <span className="text-subtle">Not tested</span>
        return result.ok ? (
          <Badge tone="success">reachable</Badge>
        ) : (
          <span title={result.message}>
            <Badge tone="danger">failed</Badge>
          </span>
        )
      },
    },
    {
      key: 'actions',
      header: '',
      width: '210px',
      render: (row) => (
        <div className="row">
          {can('integrations.configure') ? (
            <>
              <button
                type="button"
                className="btn btn--secondary btn--sm"
                onClick={(event) => {
                  event.stopPropagation()
                  test.mutate(row.id)
                }}
                disabled={test.isPending}
              >
                Test
              </button>
              {row.status === 'active' ? (
                <button
                  type="button"
                  className="btn btn--ghost btn--sm"
                  onClick={(event) => {
                    event.stopPropagation()
                    setStatus.mutate({ id: row.id, status: 'paused' })
                  }}
                >
                  Pause
                </button>
              ) : (
                <button
                  type="button"
                  className="btn btn--ghost btn--sm"
                  onClick={(event) => {
                    event.stopPropagation()
                    setStatus.mutate({ id: row.id, status: 'active' })
                  }}
                >
                  Activate
                </button>
              )}
            </>
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
        title="Integrations"
        meta="Connections to external systems, their health and their recent activity."
      />

      {actionError ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="error">{actionError}</Alert>
        </div>
      ) : null}

      <ResourceList
        title="All integrations"
        queryKey={['integrations']}
        fetchPage={(params) => integrationsApi.list(params)}
        columns={columns}
        rowKey={(row) => row.id}
        emptyTitle="No integrations configured"
        emptyText="Add a connection to exchange data with an external system."
        searchable
        searchPlaceholder="Search integrations…"
        actions={
          can('integrations.configure') ? (
            <button type="button" className="btn btn--primary btn--sm" onClick={() => setCreating(true)}>
              New integration
            </button>
          ) : null
        }
      />

      <NewIntegrationDialog
        open={creating}
        onClose={() => setCreating(false)}
        onCreated={() => {
          setCreating(false)
          void queryClient.invalidateQueries({ queryKey: ['integrations'] })
        }}
      />

      {detail ? (
        <IntegrationDetailDialog
          integration={detail}
          onClose={() => setDetail(null)}
          canConfigure={can('integrations.configure')}
        />
      ) : null}
    </>
  )
}

function NewIntegrationDialog({
  open,
  onClose,
  onCreated,
}: {
  open: boolean
  onClose: () => void
  onCreated: () => void
}) {
  const [key, setKey] = useState('')
  const [name, setName] = useState('')
  const [configText, setConfigText] = useState('{\n  \n}')
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)

  const kinds = useQuery({
    queryKey: ['integration-kinds'],
    queryFn: () => integrationsApi.kinds(),
    enabled: open,
  })

  const mutation = useMutation({
    mutationFn: () => {
      let config: Record<string, unknown>
      try {
        config = JSON.parse(configText) as Record<string, unknown>
      } catch {
        throw new Error('Configuration must be valid JSON.')
      }
      return integrationsApi.create({ key, name, config })
    },
    onSuccess: onCreated,
    onError: (error) => {
      setErrors(fieldErrors(error))
      setFormError(errorMessage(error))
    },
  })

  function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setFormError(null)
    setErrors({})
    mutation.mutate()
  }

  if (!open) return null

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="new-integration-title"
      style={{
        position: 'fixed',
        inset: 0,
        background: 'var(--color-overlay)',
        zIndex: 60,
        overflowY: 'auto',
        padding: 'var(--space-5)',
      }}
      onClick={onClose}
    >
      <div
        className="card"
        style={{ maxWidth: 620, margin: '0 auto' }}
        onClick={(event) => event.stopPropagation()}
      >
        <div className="card__header">
          <h2 id="new-integration-title">New integration</h2>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="card__body stack">
            {formError ? <Alert tone="error">{formError}</Alert> : null}

            <Field label="Kind" htmlFor="integration-kind" required error={errors.key}>
              <select
                id="integration-kind"
                value={key}
                onChange={(event) => {
                  setKey(event.target.value)
                  const kind = kinds.data?.items.find((item) => item.key === event.target.value)
                  if (kind && !name) setName(kind.name)
                }}
                required
              >
                <option value="">Select a kind</option>
                {(kinds.data?.items ?? []).map((kind) => (
                  <option key={kind.key} value={kind.key}>
                    {kind.name}
                  </option>
                ))}
              </select>
              {kinds.isError ? (
                <span className="field__hint text-danger">
                  Could not load the list of integration kinds.
                </span>
              ) : null}
            </Field>

            <Field label="Name" htmlFor="integration-name" required error={errors.name}>
              <input
                id="integration-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                required
              />
            </Field>

            <Field
              label="Configuration (JSON)"
              htmlFor="integration-config"
              hint="Secrets are stored encrypted and never returned to the browser."
            >
              <textarea
                id="integration-config"
                className="mono"
                rows={8}
                value={configText}
                onChange={(event) => setConfigText(event.target.value)}
              />
            </Field>
          </div>
          <div className="form-actions">
            <button type="button" className="btn btn--secondary" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="btn btn--primary" disabled={mutation.isPending}>
              {mutation.isPending ? 'Creating…' : 'Create'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

function IntegrationDetailDialog({
  integration,
  onClose,
  canConfigure,
}: {
  integration: Integration
  onClose: () => void
  canConfigure: boolean
}) {
  const queryClient = useQueryClient()
  const [revoking, setRevoking] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const runs = useQuery({
    queryKey: ['integration', integration.id, 'runs'],
    queryFn: () => integrationsApi.runs(integration.id, { limit: 20 }),
  })

  const status = useQuery({
    queryKey: ['integration', integration.id, 'status'],
    queryFn: () => integrationsApi.status(integration.id),
  })

  const revoke = useMutation({
    mutationFn: () => integrationsApi.revoke(integration.id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['integrations'] })
      onClose()
    },
    onError: (err) => {
      setRevoking(false)
      setError(errorMessage(err))
    },
  })

  return (
    <>
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="integration-detail-title"
        style={{
          position: 'fixed',
          inset: 0,
          background: 'var(--color-overlay)',
          zIndex: 60,
          overflowY: 'auto',
          padding: 'var(--space-5)',
        }}
        onClick={onClose}
      >
        <div
          className="card"
          style={{ maxWidth: 760, margin: '0 auto' }}
          onClick={(event) => event.stopPropagation()}
        >
          <div className="card__header">
            <h2 id="integration-detail-title">{integration.name}</h2>
            <StatusBadge status={integration.status} />
          </div>
          <div className="card__body stack">
            {error ? <Alert tone="error">{error}</Alert> : null}

            <dl className="dl">
              <dt>Kind</dt>
              <dd>{integration.kind}</dd>
              <dt>Direction</dt>
              <dd>{integration.direction ?? '—'}</dd>
              <dt>Last checked</dt>
              <dd>
                <Timestamp value={integration.last_test_at} />
              </dd>
              <dt>Last sync</dt>
              <dd>
                <Timestamp value={integration.last_sync_at} />
              </dd>
              {integration.last_test_error ? (
                <>
                  <dt>Last error</dt>
                  <dd className="text-danger">{integration.last_test_error}</dd>
                </>
              ) : null}
            </dl>

            {status.data ? (
              <div>
                <h3 style={{ marginBottom: 'var(--space-2)' }}>Status</h3>
                <pre
                  className="mono"
                  style={{
                    background: 'var(--color-bg)',
                    padding: 'var(--space-3)',
                    borderRadius: 'var(--radius-sm)',
                    overflowX: 'auto',
                    margin: 0,
                  }}
                >
                  {JSON.stringify(status.data, null, 2)}
                </pre>
              </div>
            ) : null}

            <div>
              <h3 style={{ marginBottom: 'var(--space-2)' }}>Recent runs</h3>
              {runs.isLoading ? (
                <p className="text-muted text-sm">Loading…</p>
              ) : runs.data?.items.length ? (
                <div className="table-wrap">
                  <table>
                    <thead>
                      <tr>
                        <th>Status</th>
                        <th>Operation</th>
                        <th className="numeric">Read</th>
                        <th className="numeric">Written</th>
                        <th>Started</th>
                      </tr>
                    </thead>
                    <tbody>
                      {runs.data.items.map((run) => (
                        <tr key={run.id}>
                          <td>
                            <StatusBadge status={run.status} />
                          </td>
                          <td>{run.operation ?? '—'}</td>
                          <td className="numeric">{run.records_read ?? '—'}</td>
                          <td className="numeric">{run.records_written ?? '—'}</td>
                          <td>
                            <Timestamp value={run.started_at} />
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              ) : (
                <p className="text-muted text-sm">This integration has not run yet.</p>
              )}
            </div>
          </div>
          <div className="form-actions" style={{ margin: 0, padding: 'var(--space-4)' }}>
            <button type="button" className="btn btn--secondary" onClick={onClose}>
              Close
            </button>
            {canConfigure ? (
              <button type="button" className="btn btn--danger" onClick={() => setRevoking(true)}>
                Revoke credentials
              </button>
            ) : null}
          </div>
        </div>
      </div>

      <ConfirmDialog
        open={revoking}
        title="Revoke this integration?"
        text="Its stored credentials are discarded and it stops running. Historical runs are kept."
        confirmLabel="Revoke"
        danger
        busy={revoke.isPending}
        onConfirm={() => revoke.mutate()}
        onCancel={() => setRevoking(false)}
      />
    </>
  )
}
