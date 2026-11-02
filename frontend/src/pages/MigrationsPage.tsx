import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { migrationsApi } from '../api/integrations'
import { systemApi } from '../api/admin'
import { errorMessage, fieldErrors } from '../api/client'
import type { MigrationDiscovery, MigrationJob, MigrationReconciliation } from '../api/integrations'
import { useAuth } from '../hooks/useAuth'
import { ResourceList, type Column } from '../components/ResourceList'
import { Alert, Field, PageHeader, StatusBadge, Timestamp } from '../components/ui'

/**
 * The source vocabulary is read from the server, never hard-coded.
 *
 * `migration_sources` on `/system/capabilities` is `migratex.ImplementedSources()`,
 * so a kind appears exactly when the server can build a source for it. A local
 * list once offered `sqlserver` (recognised but refused with "not implemented") and
 * spelled the REST source `rest` while the server calls it `rest_api`, so a
 * migration created from the interface would have been rejected at submit — the
 * same silent-mismatch failure the webhook and workflow pickers once had.
 */
function useMigrationSources() {
  const query = useQuery({
    queryKey: ['system', 'capabilities'],
    queryFn: systemApi.capabilities,
    staleTime: 5 * 60 * 1000,
  })
  return { sourceTypes: query.data?.migration_sources ?? [], isLoading: query.isLoading }
}

const STRATEGIES = ['one_time', 'periodic', 'incremental', 'cdc']

const columns: Column<MigrationJob>[] = [
  {
    key: 'name',
    header: 'Migration',
    render: (row) => (
      <div>
        <div className="table__primary">{row.name || row.source_kind}</div>
        <div className="table__secondary">{row.source_kind}</div>
      </div>
    ),
  },
  { key: 'status', header: 'Status', render: (row) => <StatusBadge status={row.status} /> },
  {
    key: 'strategy',
    header: 'Strategy',
    render: (row) => row.strategy?.replace('_', ' ') ?? <span className="text-subtle">—</span>,
  },
  {
    key: 'created',
    header: 'Created',
    render: (row) => <Timestamp value={row.created_at} />,
  },
]

export function MigrationsPage() {
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [details, setDetails] = useState<MigrationJob | null>(null)
  const [schema, setSchema] = useState<{ job: MigrationJob; result: MigrationDiscovery } | null>(null)
  const [reconciling, setReconciling] = useState<MigrationJob | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const discover = useMutation({
    mutationFn: (job: MigrationJob) => migrationsApi.discover(job.id),
    onSuccess: (result, job) => {
      void queryClient.invalidateQueries({ queryKey: ['migrations'] })
      setActionError(null)
      setSchema({ job, result })
    },
    onError: (error) => setActionError(errorMessage(error)),
  })

  const withActions: Column<MigrationJob>[] = [
    ...columns,
    {
      key: 'actions',
      header: '',
      width: '220px',
      render: (row) => (
        <div className="row">
          <button
            type="button"
            className="btn btn--secondary btn--sm"
            onClick={(event) => {
              event.stopPropagation()
              discover.mutate(row)
            }}
            disabled={discover.isPending}
          >
            Discover schema
          </button>
          <button
            type="button"
            className="btn btn--ghost btn--sm"
            onClick={(event) => {
              event.stopPropagation()
              setReconciling(row)
            }}
          >
            Reconcile
          </button>
          <button
            type="button"
            className="btn btn--ghost btn--sm"
            onClick={(event) => {
              event.stopPropagation()
              setDetails(row)
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
        title="Migrations"
        meta="Read from an existing database or API, map it onto ForgeCRM, and check that the counts agree."
      />

      <div style={{ marginBottom: 'var(--space-4)' }}>
        <Alert tone="info">
          Migrations connect with read-only credentials by default. ForgeCRM never writes to the
          source system, and passwords are stored encrypted rather than in the configuration text.
        </Alert>
      </div>

      {actionError ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="error">{actionError}</Alert>
        </div>
      ) : null}

      <ResourceList
        title="Migration jobs"
        queryKey={['migrations']}
        fetchPage={(params) => migrationsApi.list(params)}
        columns={withActions}
        rowKey={(row) => row.id}
        emptyTitle="No migrations"
        emptyText="Set up a migration to bring existing data across from another system."
        searchable
        searchPlaceholder="Search migrations…"
        actions={
          can('migrations.manage') ? (
            <button type="button" className="btn btn--primary btn--sm" onClick={() => setCreating(true)}>
              New migration
            </button>
          ) : null
        }
      />

      <NewMigrationDialog
        open={creating}
        onClose={() => setCreating(false)}
        onCreated={() => {
          setCreating(false)
          void queryClient.invalidateQueries({ queryKey: ['migrations'] })
        }}
      />

      {details ? (
        <MigrationDetailDialog migration={details} onClose={() => setDetails(null)} />
      ) : null}

      {schema ? (
        <SchemaDialog job={schema.job} result={schema.result} onClose={() => setSchema(null)} />
      ) : null}

      {reconciling ? (
        <ReconcileDialog job={reconciling} onClose={() => setReconciling(null)} />
      ) : null}
    </>
  )
}

/**
 * Renders what `discover` read from the source. The list comes from the server, so
 * an empty result means the account can see no tables — not that the feature is
 * missing.
 */
function SchemaDialog({
  job,
  result,
  onClose,
}: {
  job: MigrationJob
  result: MigrationDiscovery
  onClose: () => void
}) {
  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="schema-title"
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
      <div className="card" style={{ maxWidth: 760, margin: '0 auto' }} onClick={(e) => e.stopPropagation()}>
        <div className="card__header">
          <h2 id="schema-title">Source schema — {job.name || job.source_kind}</h2>
          <span className="badge">{result.read_only ? 'read-only' : 'read-write'}</span>
        </div>
        <div className="card__body stack">
          {result.tables.length === 0 ? (
            <p className="text-muted text-sm">
              The account returned no tables. Check that it can read the source schema.
            </p>
          ) : (
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Table</th>
                    <th className="numeric">Rows</th>
                    <th className="numeric">Columns</th>
                  </tr>
                </thead>
                <tbody>
                  {result.tables.map((table) => (
                    <tr key={table.name}>
                      <td className="mono">{table.name}</td>
                      <td className="numeric">{table.rows}</td>
                      <td className="numeric">{table.columns.length}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
        <div className="form-actions">
          <button type="button" className="btn btn--secondary" onClick={onClose}>
            Close
          </button>
        </div>
      </div>
    </div>
  )
}

/**
 * Reconcile compares a source table with the target entity and records the counts.
 * Both fields are required by the handler; leaving them empty is what made the old
 * one-click button fail with a 400.
 */
function ReconcileDialog({
  job,
  onClose,
}: {
  job: MigrationJob
  onClose: () => void
}) {
  const [sourceTable, setSourceTable] = useState(job.source_tables?.[0] ?? '')
  const [targetEntity, setTargetEntity] = useState('')
  const [result, setResult] = useState<MigrationReconciliation | null>(null)

  const mutation = useMutation({
    mutationFn: () =>
      migrationsApi.reconcile(job.id, { source_table: sourceTable, target_entity: targetEntity }),
    onSuccess: (rec) => setResult(rec),
  })

  function handleSubmit(event: FormEvent) {
    event.preventDefault()
    mutation.mutate()
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="reconcile-title"
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
      <div className="card" style={{ maxWidth: 560, margin: '0 auto' }} onClick={(e) => e.stopPropagation()}>
        <div className="card__header">
          <h2 id="reconcile-title">Reconcile {job.name || job.source_kind}</h2>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="card__body stack">
            {mutation.error ? <Alert tone="error">{errorMessage(mutation.error)}</Alert> : null}

            <Field label="Source table" htmlFor="reconcile-source" required>
              <input
                id="reconcile-source"
                value={sourceTable}
                onChange={(event) => setSourceTable(event.target.value)}
                placeholder="customers"
              />
            </Field>
            <Field label="Target entity" htmlFor="reconcile-target" required>
              <input
                id="reconcile-target"
                value={targetEntity}
                onChange={(event) => setTargetEntity(event.target.value)}
                placeholder="customers"
              />
            </Field>

            {result ? (
              <dl className="dl">
                <dt>Status</dt>
                <dd>
                  <StatusBadge status={result.status} />
                </dd>
                <dt>Source rows</dt>
                <dd>{result.source_count}</dd>
                <dt>Target rows</dt>
                <dd>{result.target_count}</dd>
                <dt>Matched</dt>
                <dd>{result.matched_count}</dd>
                <dt>Mismatched</dt>
                <dd>{result.mismatched_count}</dd>
              </dl>
            ) : null}
          </div>
          <div className="form-actions">
            <button type="button" className="btn btn--secondary" onClick={onClose}>
              Close
            </button>
            <button
              type="submit"
              className="btn btn--primary"
              disabled={mutation.isPending || !sourceTable || !targetEntity}
            >
              {mutation.isPending ? 'Reconciling…' : 'Reconcile'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

function NewMigrationDialog({
  open,
  onClose,
  onCreated,
}: {
  open: boolean
  onClose: () => void
  onCreated: () => void
}) {
  const [name, setName] = useState('')
  const { sourceTypes, isLoading: sourcesLoading } = useMigrationSources()
  const [sourceType, setSourceType] = useState('')
  const [strategy, setStrategy] = useState('one_time')
  const [password, setPassword] = useState('')
  const [configText, setConfigText] = useState('{\n  "host": "",\n  "port": 5432,\n  "database": "",\n  "username": ""\n}')
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)

  const mutation = useMutation({
    mutationFn: () => {
      let config: Record<string, unknown>
      try {
        config = JSON.parse(configText) as Record<string, unknown>
      } catch {
        throw new Error('Connection configuration must be valid JSON.')
      }
      // The picker is the single source of truth for the source kind: the server
      // reads it from `config.kind`, so injecting it here keeps the two from
      // disagreeing if the connection text omits or contradicts it.
      const kind = sourceType || sourceTypes[0] || ''
      return migrationsApi.create({
        name,
        config: { ...config, kind },
        strategy,
        password: password || undefined,
      })
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
      aria-labelledby="new-migration-title"
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
      <div className="card" style={{ maxWidth: 640, margin: '0 auto' }} onClick={(e) => e.stopPropagation()}>
        <div className="card__header">
          <h2 id="new-migration-title">New migration</h2>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="card__body stack">
            {formError ? <Alert tone="error">{formError}</Alert> : null}

            <Field label="Name" htmlFor="migration-name" error={errors.name}>
              <input
                id="migration-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder="Legacy CRM cutover"
              />
            </Field>

            <div className="form-grid">
              <Field label="Source type" htmlFor="migration-source" required>
                <select
                  id="migration-source"
                  value={sourceType || sourceTypes[0] || ''}
                  onChange={(event) => setSourceType(event.target.value)}
                  disabled={sourcesLoading || sourceTypes.length === 0}
                >
                  {sourceTypes.map((type) => (
                    <option key={type} value={type}>
                      {type}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="Strategy" htmlFor="migration-strategy">
                <select
                  id="migration-strategy"
                  value={strategy}
                  onChange={(event) => setStrategy(event.target.value)}
                >
                  {STRATEGIES.map((item) => (
                    <option key={item} value={item}>
                      {item.replace('_', ' ')}
                    </option>
                  ))}
                </select>
              </Field>
            </div>

            <Field
              label="Connection (JSON)"
              htmlFor="migration-config"
              hint="Host, port, database and username. The source kind comes from the picker above."
            >
              <textarea
                id="migration-config"
                className="mono"
                rows={8}
                value={configText}
                onChange={(event) => setConfigText(event.target.value)}
              />
            </Field>

            <Field
              label="Password"
              htmlFor="migration-password"
              hint="Encrypted at rest and never returned to the browser."
            >
              <input
                id="migration-password"
                type="password"
                autoComplete="new-password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
              />
            </Field>

            <Alert tone="warning">
              Grant the account read-only access. ForgeCRM will not modify the source database.
            </Alert>
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

function MigrationDetailDialog({
  migration,
  onClose,
}: {
  migration: MigrationJob
  onClose: () => void
}) {
  const reconciliations = useQuery({
    queryKey: ['migration', migration.id, 'reconciliation'],
    queryFn: () => migrationsApi.reconciliations(migration.id, { limit: 20 }),
  })

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="migration-detail-title"
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
      <div className="card" style={{ maxWidth: 760, margin: '0 auto' }} onClick={(e) => e.stopPropagation()}>
        <div className="card__header">
          <h2 id="migration-detail-title">{migration.name || migration.source_kind}</h2>
          <StatusBadge status={migration.status} />
        </div>
        <div className="card__body stack">
          <dl className="dl">
            <dt>Source</dt>
            <dd>{migration.source_kind}</dd>
            <dt>Strategy</dt>
            <dd>{migration.strategy?.replace('_', ' ') ?? '—'}</dd>
            <dt>Created</dt>
            <dd>
              <Timestamp value={migration.created_at} />
            </dd>
            {migration.error ? (
              <>
                <dt>Last error</dt>
                <dd className="text-danger">{migration.error}</dd>
              </>
            ) : null}
          </dl>

          <div>
            <h3 style={{ marginBottom: 'var(--space-2)' }}>Reconciliations</h3>
            {reconciliations.isLoading ? (
              <p className="text-muted text-sm">Loading…</p>
            ) : reconciliations.data?.items.length ? (
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>Status</th>
                      <th>Table</th>
                      <th className="numeric">Source</th>
                      <th className="numeric">Target</th>
                      <th className="numeric">Mismatches</th>
                      <th>When</th>
                    </tr>
                  </thead>
                  <tbody>
                    {reconciliations.data.items.map((row) => (
                      <tr key={row.id}>
                        <td>
                          <StatusBadge status={row.status} />
                        </td>
                        <td>{row.source_table}</td>
                        <td className="numeric">{row.source_count}</td>
                        <td className="numeric">{row.target_count}</td>
                        <td className="numeric">{row.mismatched_count}</td>
                        <td>
                          <Timestamp value={row.created_at} />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <p className="text-muted text-sm">No reconciliation has been run yet.</p>
            )}
          </div>
        </div>
        <div className="form-actions" style={{ margin: 0, padding: 'var(--space-4)' }}>
          <button type="button" className="btn btn--secondary" onClick={onClose}>
            Close
          </button>
        </div>
      </div>
    </div>
  )
}
