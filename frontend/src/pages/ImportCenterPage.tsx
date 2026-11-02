import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { exportsApi, importsApi } from '../api/imports'
import { errorMessage, fieldErrors } from '../api/client'
import { IMPORT_ENTITY_TYPES } from '../types/api'
import type { ImportJob } from '../types/api'
import { useAuth } from '../hooks/useAuth'
import { useT, type TFunction } from '../i18n'
import { ResourceList, type Column } from '../components/ResourceList'
import {
  Alert,
  Field,
  Modal,
  PageHeader,
  StatusBadge,
  Timestamp,
  formatBytes,
} from '../components/ui'

const columns = (t: TFunction): Column<ImportJob>[] => [
  {
    key: 'name',
        header: t('imports.columnImport'),
    render: (row) => (
      <div>
        <div className="table__primary">{row.name || row.original_filename || t('imports.untitled')}</div>
        <div className="table__secondary">
          {row.entity_type ?? '—'}
          {row.format ? ` · ${row.format}` : ''}
          {row.original_filename ? ` · ${row.original_filename}` : ''}
        </div>
      </div>
    ),
  },
    {
      key: 'status',
      header: t('imports.columnStatus'),
      render: (row) => <StatusBadge status={row.status} />,
    },
  {
    key: 'rows',
        header: t('imports.columnRows'),
    numeric: true,
    render: (row) =>
      row.status === 'pending' || row.total_rows === 0 ? (
        <span className="text-subtle">—</span>
      ) : (
        <span
                  title={t('imports.rowsTitle', {
                    imported: row.imported_rows,
                    skipped: row.skipped_rows,
                    failed: row.failed_rows,
                  })}
                >
          {row.imported_rows}/{row.total_rows}
        </span>
      ),
  },
  {
    key: 'size',
        header: t('imports.columnSize'),
    numeric: true,
    render: (row) => formatBytes(row.size_bytes),
  },
  {
    key: 'created',
        header: t('imports.columnCreated'),
    render: (row) => <Timestamp value={row.created_at} />,
  },
]

export function ImportCenterPage() {
    const navigate = useNavigate()
    const { can } = useAuth()
    const t = useT()
  const [creating, setCreating] = useState(false)
  const [exporting, setExporting] = useState(false)

  return (
    <>
      <PageHeader
                title={t('imports.title')}
                meta={t('imports.meta')}
              />
      <ResourceList
                title={t('imports.jobsTitle')}
        queryKey={['imports']}
        fetchPage={(params) => importsApi.list(params)}
                columns={columns(t)}
        rowKey={(row) => row.id}
        onRowClick={(row) => navigate(`/imports/${row.id}`)}
                emptyTitle={t('imports.emptyTitle')}
                emptyText={t('imports.emptyText')}
        searchable
                searchPlaceholder={t('imports.searchPlaceholder')}
        actions={
          <div className="row" style={{ gap: 'var(--space-2)' }}>
            {can('exports.execute') ? (
              <button type="button" className="btn btn--secondary btn--sm" onClick={() => setExporting(true)}>
                {t('imports.export')}
                </button>
            ) : null}
            {can('imports.create') ? (
              <button type="button" className="btn btn--primary btn--sm" onClick={() => setCreating(true)}>
                {t('imports.newImport')}
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
  const t = useT()
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
    <Modal
      open
      onClose={onClose}
      title={t('imports.exportTitle')}
      footer={<>
        <button type="button" className="btn btn--secondary" onClick={onClose}>
          {t('common.cancel')}
        </button>
        <button type="submit" className="btn btn--primary" disabled={mutation.isPending}>
          {mutation.isPending ? t('imports.preparing') : t('imports.download')}
        </button>
      </>}
      formProps={{ onSubmit: (event: FormEvent) => {
            event.preventDefault()
            setError(null)
            mutation.mutate()
          } }}
    >
      {error ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="error">{error}</Alert>
        </div>
      ) : null}
      <div className="form-grid">
        <Field label={t('imports.entity')} htmlFor="export-entity">
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
        <Field label={t('imports.format')} htmlFor="export-format">
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
    </Modal>
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
  const t = useT()
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
      setErrors({ file: t('imports.chooseFile') })
      return
    }
    mutation.mutate()
  }

  if (!open) return null

  return (
    <Modal
      open
      onClose={onClose}
      title={t('imports.newTitle')}
      footer={<>
        <button type="button" className="btn btn--secondary" onClick={onClose} disabled={mutation.isPending}>
          {t('common.cancel')}
        </button>
        <button type="submit" className="btn btn--primary" disabled={mutation.isPending}>
          {mutation.isPending ? t('common.uploading') : t('imports.createAndUpload')}
        </button>
      </>}
      formProps={{ onSubmit: handleSubmit }}
    >
      <div className="stack">
        {formError ? <Alert tone="error">{formError}</Alert> : null}

        <Field label={t('imports.name')} htmlFor="import-name" hint={t('imports.nameHint')}>
          <input
            id="import-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder={t('imports.namePlaceholder')}
          />
        </Field>

        <Field
                      label={t('imports.targetEntity')}
                      htmlFor="import-entity"
                      required
                      error={errors.entity_type}
                    >
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
                        label={t('imports.sourceFile')}
                        htmlFor="import-file"
                        required
                        error={errors.file}
                        hint={t('imports.sourceFileHint')}
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

        <Alert tone="info">{t('imports.nothingWritten')}</Alert>
      </div>
    </Modal>
  )
}
