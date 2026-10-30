import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { exportsApi, importsApi } from '../api/imports'
import { errorMessage, fieldErrors } from '../api/client'
import { IMPORT_ENTITY_TYPES } from '../types/api'
import type { ImportJob } from '../types/api'
import { useAuth } from '../hooks/useAuth'
import { ResourceList, type Column } from '../components/ResourceList'
import { Alert, Field, PageHeader, StatusBadge, Timestamp, formatBytes } from '../components/ui'

const columns: Column<ImportJob>[] = [
  {
    key: 'name',
    header: 'Import',
    render: (row) => (
      <div>
        <div className="table__primary">{row.name || row.original_filename || 'Untitled import'}</div>
        <div className="table__secondary">
          {row.entity_type ?? '—'}
          {row.format ? ` · ${row.format}` : ''}
          {row.original_filename ? ` · ${row.original_filename}` : ''}
        </div>
      </div>
    ),
  },
  { key: 'status', header: 'Status', render: (row) => <StatusBadge status={row.status} /> },
  {
    key: 'rows',
    header: 'Rows',
    numeric: true,
    render: (row) =>
      row.status === 'pending' || row.total_rows === 0 ? (
        <span className="text-subtle">—</span>
      ) : (
        <span title={`${row.imported_rows} imported, ${row.skipped_rows} skipped, ${row.failed_rows} failed`}>
          {row.imported_rows}/{row.total_rows}
        </span>
      ),
  },
  {
    key: 'size',
    header: 'Size',
    numeric: true,
    render: (row) => formatBytes(row.size_bytes),
  },
  {
    key: 'created',
    header: 'Created',
    render: (row) => <Timestamp value={row.created_at} />,
  },
]

export function ImportCenterPage() {
  const navigate = useNavigate()
  const { can } = useAuth()
  const [creating, setCreating] = useState(false)
  const [exporting, setExporting] = useState(false)

  return (
    <>
      <PageHeader
        title="Import Center"
        meta="Load data from CSV, JSON, XLSX, XML and archives, preview it, and check the result before committing."
      />
      <ResourceList
        title="Import jobs"
        queryKey={['imports']}
        fetchPage={(params) => importsApi.list(params)}
        columns={columns}
        rowKey={(row) => row.id}
        onRowClick={(row) => navigate(`/imports/${row.id}`)}
        emptyTitle="No imports yet"
        emptyText="Start an import to load contacts, accounts, deals or any other supported entity."
        searchable
        searchPlaceholder="Search imports…"
        actions={
          <div className="row" style={{ gap: 'var(--space-2)' }}>
            {can('exports.execute') ? (
              <button type="button" className="btn btn--secondary btn--sm" onClick={() => setExporting(true)}>
                Export
              </button>
            ) : null}
            {can('imports.create') ? (
              <button type="button" className="btn btn--primary btn--sm" onClick={() => setCreating(true)}>
                New import
              </button>
            ) : null}
          </div>
        }
      />
      <NewImportDialog
        open={creating}
        onClose={() => setCreating(false)}
        onCreated={(job) => {
          setCreating(false)
          navigate(`/imports/${job.id}`)
        }}
      />
      <ExportDialog open={exporting} onClose={() => setExporting(false)} />
    </>
  )
}

/**
 * Downloads an export of one entity type.
 *
 * The server streams the file in the response, so the result is a blob the page
 * hands to the browser as a download. The export permission is checked here for
 * the button and again on the server; read access to the chosen entity is a
 * second server-side check, so an entity the user cannot read is refused rather
 * than silently exported.
 */
function ExportDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [entityType, setEntityType] = useState<string>(IMPORT_ENTITY_TYPES[0])
  const [format, setFormat] = useState<'csv' | 'jsonl'>('csv')
  const [error, setError] = useState<string | null>(null)

  const mutation = useMutation({
    mutationFn: () => exportsApi.download({ entity_type: entityType, format }),
    onSuccess: ({ blob, filename }) => {
      setError(null)
      const url = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = filename ?? `${entityType}.${format}`
      document.body.appendChild(anchor)
      anchor.click()
      anchor.remove()
      URL.revokeObjectURL(url)
      onClose()
    },
    onError: (err) => setError(errorMessage(err)),
  })

  if (!open) return null

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="export-title"
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
      <div
        className="card"
        style={{ maxWidth: 480, margin: '0 auto' }}
        onClick={(event) => event.stopPropagation()}
      >
        <div className="card__header">
          <h2 id="export-title">Export records</h2>
        </div>
        <form
          onSubmit={(event: FormEvent) => {
            event.preventDefault()
            setError(null)
            mutation.mutate()
          }}
        >
          <div className="card__body">
            {error ? (
              <div style={{ marginBottom: 'var(--space-4)' }}>
                <Alert tone="error">{error}</Alert>
              </div>
            ) : null}
            <div className="form-grid">
              <Field label="Entity" htmlFor="export-entity">
                <select
                  id="export-entity"
                  value={entityType}
                  onChange={(event) => setEntityType(event.target.value)}
                >
                  {IMPORT_ENTITY_TYPES.map((type) => (
                    <option key={type} value={type}>
                      {type}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="Format" htmlFor="export-format">
                <select
                  id="export-format"
                  value={format}
                  onChange={(event) => setFormat(event.target.value as 'csv' | 'jsonl')}
                >
                  <option value="csv">CSV</option>
                  <option value="jsonl">JSONL</option>
                </select>
              </Field>
            </div>
          </div>
          <div className="form-actions">
            <button type="button" className="btn btn--secondary" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="btn btn--primary" disabled={mutation.isPending}>
              {mutation.isPending ? 'Preparing…' : 'Download'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

/**
 * Creates the job and uploads the file.
 *
 * The two steps are separate on the server so that neither half is wasted when
 * the other fails; here they are sequenced so the user sees one action.
 */
function NewImportDialog({
  open,
  onClose,
  onCreated,
}: {
  open: boolean
  onClose: () => void
  onCreated: (job: ImportJob) => void
}) {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [entityType, setEntityType] = useState<string>('contact')
  const [file, setFile] = useState<File | null>(null)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)

  const mutation = useMutation({
    mutationFn: async () => {
      const job = await importsApi.create({
        name: name.trim(),
        entity_type: entityType,
        original_filename: file?.name,
      })
      if (file) await importsApi.upload(job.id, file)
      return job
    },
    onSuccess: (job) => {
      void queryClient.invalidateQueries({ queryKey: ['imports'] })
      onCreated(job)
    },
    onError: (error) => {
      setErrors(fieldErrors(error))
      setFormError(errorMessage(error))
    },
  })

  function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setFormError(null)
    setErrors({})
    if (!file) {
      setErrors({ file: 'Choose a file to import.' })
      return
    }
    mutation.mutate()
  }

  if (!open) return null

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="new-import-title"
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
      <div
        className="card"
        style={{ maxWidth: 560, margin: '0 auto' }}
        onClick={(event) => event.stopPropagation()}
      >
        <div className="card__header">
          <h2 id="new-import-title">New import</h2>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="card__body stack">
            {formError ? <Alert tone="error">{formError}</Alert> : null}

            <Field label="Name" htmlFor="import-name" hint="For your own reference.">
              <input
                id="import-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder="March contact upload"
              />
            </Field>

            <Field label="Target entity" htmlFor="import-entity" required error={errors.entity_type}>
              <select
                id="import-entity"
                value={entityType}
                onChange={(event) => setEntityType(event.target.value)}
              >
                {IMPORT_ENTITY_TYPES.map((type) => (
                  <option key={type} value={type}>
                    {type}
                  </option>
                ))}
              </select>
            </Field>

            <Field
              label="Source file"
              htmlFor="import-file"
              required
              error={errors.file}
              hint="CSV, TSV, XLSX, JSON, JSONL, XML, or an archive containing them."
            >
              <input
                id="import-file"
                type="file"
                accept=".csv,.tsv,.txt,.xlsx,.json,.jsonl,.ndjson,.xml,.zip,.tar,.gz,.tgz,.bz2,.xz,.7z"
                onChange={(event) => setFile(event.target.files?.[0] ?? null)}
                className={errors.file ? 'input--invalid' : undefined}
              />
            </Field>

            {file ? (
              <p className="text-sm text-muted">
                {file.name} · {formatBytes(file.size)}
              </p>
            ) : null}

            <Alert tone="info">
              Nothing is written until you review the mapping and confirm. You can dry-run the
              import first and see exactly what would change.
            </Alert>
          </div>
          <div className="form-actions">
            <button type="button" className="btn btn--secondary" onClick={onClose} disabled={mutation.isPending}>
              Cancel
            </button>
            <button type="submit" className="btn btn--primary" disabled={mutation.isPending}>
              {mutation.isPending ? 'Uploading…' : 'Create and upload'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
