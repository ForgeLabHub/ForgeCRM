import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { migrationsApi } from '../api/integrations'
import { errorMessage, fieldErrors } from '../api/client'
import type { MigrationJob } from '../api/integrations'
import { useAuth } from '../hooks/useAuth'
import { ResourceList, type Column } from '../components/ResourceList'
import { Alert, Field, PageHeader, StatusBadge, Timestamp } from '../components/ui'

const SOURCE_TYPES = ['postgres', 'mysql', 'sqlite', 'sqlserver', 'rest']
const STRATEGIES = ['one_time', 'periodic', 'incremental', 'cdc']

const columns: Column<MigrationJob>[] = [
  {
    key: 'name',
    header: 'Migration',
    render: (row) => (
      <div>
        <div className="table__primary">{row.name || row.source_type}</div>
        <div className="table__secondary">{row.source_type}</div>
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
    key: 'updated',
    header: 'Updated',
    render: (row) => <Timestamp value={row.updated_at} />,
  },
]

export function MigrationsPage() {
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [discovering, setDiscovering] = useState<MigrationJob | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const discover = useMutation({
    mutationFn: (id: string) => migrationsApi.discover(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['migrations'] })
      setActionError(null)
    },
    onError: (error) => setActionError(errorMessage(error)),
  })

  const reconcile = useMutation({
    mutationFn: (id: string) => migrationsApi.reconcile(id),
    onSuccess: () => setActionError(null),
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
              discover.mutate(row.id)
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
              reconcile.mutate(row.id)
            }}
            disabled={reconcile.isPending}
          >
            Reconcile
          </button>
          <button
            type="button"
            className="btn btn--ghost btn--sm"
            onClick={(event) => {
              event.stopPropagation()
              setDiscovering(row)
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

      {discovering ? (
        <MigrationDetailDialog migration={discovering} onClose={() => setDiscovering(null)} />
      ) : null}
    </>
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
  const [sourceType, setSourceType] = useState('postgres')
  const [strategy, setStrategy] = useState('one_time')
  const [configText, setConfigText] = useState('{\n  "host": "",\n  "port": 5432,\n  "database": "",\n  "user": "",\n  "password": ""\n}')
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
      return migrationsApi.create({ name, source_type: sourceType, config, strategy })
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
        background: 'rgba(16, 24, 40, 0.45)',
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
                  value={sourceType}
                  onChange={(event) => setSourceType(event.target.value)}
                >
                  {SOURCE_TYPES.map((type) => (
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
              hint="The password is encrypted at rest and never returned to the browser."
            >
              <textarea
                id="migration-config"
                className="mono"
                rows={9}
                value={configText}
                onChange={(event) => setConfigText(event.target.value)}
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
        background: 'rgba(16, 24, 40, 0.45)',
        zIndex: 60,
        overflowY: 'auto',
        padding: 'var(--space-5)',
      }}
      onClick={onClose}
    >
      <div className="card" style={{ maxWidth: 760, margin: '0 auto' }} onClick={(e) => e.stopPropagation()}>
        <div className="card__header">
          <h2 id="migration-detail-title">{migration.name || migration.source_type}</h2>
          <StatusBadge status={migration.status} />
        </div>
        <div className="card__body stack">
          <dl className="dl">
            <dt>Source</dt>
            <dd>{migration.source_type}</dd>
            <dt>Strategy</dt>
            <dd>{migration.strategy?.replace('_', ' ') ?? '—'}</dd>
            <dt>Created</dt>
            <dd>
              <Timestamp value={migration.created_at} />
            </dd>
            {migration.last_error ? (
              <>
                <dt>Last error</dt>
                <dd className="text-danger">{migration.last_error}</dd>
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
                        <td className="numeric">{row.source_count ?? '—'}</td>
                        <td className="numeric">{row.target_count ?? '—'}</td>
                        <td className="numeric">{row.mismatches ?? '—'}</td>
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
