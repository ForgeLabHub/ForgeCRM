import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import { importsApi } from '../api/imports'
import { errorMessage } from '../api/client'
import type { ImportReport, MappingEntry } from '../types/api'
import { useAuth } from '../hooks/useAuth'
import { useT } from '../i18n'
import {
  Alert,
  Badge,
  Card,
  ConfirmDialog,
  PageHeader,
  Stat,
  StatusBadge,
  Timestamp,
  formatBytes,
} from '../components/ui'


export function ImportDetailPage() {
  const { id = '' } = useParams()
  const queryClient = useQueryClient()
    const { can } = useAuth()
    const t = useT()

    const steps = [
      t('imports.stepUpload'),
      t('imports.stepAnalyse'),
      t('imports.stepMap'),
      t('imports.stepValidate'),
      t('imports.stepDryRun'),
      t('imports.stepImport'),
      t('imports.stepReport'),
    ]

  const [mapping, setMapping] = useState<MappingEntry[]>([])
  const [duplicates, setDuplicates] = useState<'skip' | 'update' | 'create' | 'fail'>('skip')
  const [externalSource, setExternalSource] = useState('')
  const [sheetName, setSheetName] = useState('')
  const [archiveEntry, setArchiveEntry] = useState('')
  const [validateResult, setValidateResult] = useState<ImportReport | null>(null)
  const [dryRunResult, setDryRunResult] = useState<ImportReport | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [confirmExecute, setConfirmExecute] = useState(false)

  const job = useQuery({
    queryKey: ['import', id],
    queryFn: () => importsApi.get(id),
    enabled: Boolean(id),
    // Poll while the import is running so the report fills in without a reload.
    refetchInterval: (query) => {
      const status = query.state.data?.status
      return status === 'running' || status === 'queued' ? 2000 : false
    },
  })

  const fields = useQuery({
    queryKey: ['import', 'fields', job.data?.entity_type],
    queryFn: () => importsApi.fields(job.data!.entity_type!),
    enabled: Boolean(job.data?.entity_type),
  })

  const analyze = useMutation({
    mutationFn: () =>
      importsApi.analyze(id, {
        options: {
          sheet_name: sheetName || undefined,
          archive_entry: archiveEntry || undefined,
        },
      }),
    onSuccess: (result) => {
      setActionError(null)
      void queryClient.invalidateQueries({ queryKey: ['import', id] })
      // Pre-fill a mapping from columns whose name matches a target field, so
      // the common case needs no manual work.
      const columns = result.schema?.columns ?? []
      const available = fields.data?.fields ?? []
      const guessed = columns
        .filter((column) => available.includes(normalize(column)))
        .map((column) => ({ source_column: column, target_field: normalize(column) }))
      setMapping(guessed)
    },
    onError: (error) => setActionError(errorMessage(error)),
  })

  const validate = useMutation({
    mutationFn: () => importsApi.validate(id, { mapping, options: buildOptions() }),
    onSuccess: (report) => {
      setValidateResult(report)
      setActionError(null)
    },
    onError: (error) => setActionError(errorMessage(error)),
  })

  const dryRun = useMutation({
    mutationFn: () => importsApi.dryRun(id, { mapping, options: buildOptions() }),
    onSuccess: (report) => {
      setDryRunResult(report)
      setActionError(null)
    },
    onError: (error) => setActionError(errorMessage(error)),
  })

  const execute = useMutation({
    mutationFn: () => importsApi.execute(id, { mapping, options: buildOptions() }),
    onSuccess: () => {
      setConfirmExecute(false)
      setActionError(null)
      void queryClient.invalidateQueries({ queryKey: ['import', id] })
      void queryClient.invalidateQueries({ queryKey: ['imports'] })
    },
    onError: (error) => {
      setConfirmExecute(false)
      setActionError(errorMessage(error))
    },
  })

  const cancel = useMutation({
    mutationFn: () => importsApi.cancel(id),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['import', id] }),
    onError: (error) => setActionError(errorMessage(error)),
  })

  function buildOptions() {
    return {
      duplicates,
      external_source: externalSource || undefined,
      sheet_name: sheetName || undefined,
      archive_entry: archiveEntry || undefined,
    }
  }

  const errors = useQuery({
    queryKey: ['import', id, 'errors'],
    queryFn: () => importsApi.errors(id, { limit: 100 }),
    enabled: Boolean(id) && job.data?.status === 'completed',
  })

  const currentStep = useMemo(() => {
    const status = job.data?.status
    if (!status) return 0
    if (status === 'pending') return 0
    if (status === 'analyzed') return 2
    if (status === 'completed') return 6
    if (status === 'failed') return 5
    if (status === 'running' || status === 'queued') return 5
    return 1
  }, [job.data?.status])

  if (job.isError) {
    return (
            <Card title={t('imports.detailImport')}>
        <Alert tone="error">{errorMessage(job.error)}</Alert>
      </Card>
    )
  }

  if (job.isLoading || !job.data) {
    return <div className="skeleton" style={{ width: '100%', height: 320 }} aria-busy="true" />
  }

  const data = job.data
  const hasFile = Boolean(data.original_filename)
  const columns = data.schema_info?.columns ?? []
  const availableFields = fields.data?.fields ?? []
  const canExecute = can('imports.execute')
  const isBusy = execute.isPending || data.status === 'running'

  return (
    <>
      <PageHeader
        title={data.name || data.original_filename || 'Import'}
        meta={
          <>
            {data.entity_type ?? t('imports.unknownEntity')}
            {data.format ? ` · ${data.format}` : ''}
            {data.original_filename ? ` · ${data.original_filename}` : ''}
            {data.size_bytes ? ` · ${formatBytes(data.size_bytes)}` : ''}
          </>
        }
        actions={
          <>
            <Link to="/imports" className="btn btn--ghost btn--sm">
              {t('imports.back')}
                          </Link>
            {isBusy && canExecute ? (
              <button
                type="button"
                className="btn btn--danger btn--sm"
                onClick={() => cancel.mutate()}
                disabled={cancel.isPending}
              >
                {t('imports.cancelImport')}
                              </button>
            ) : null}
          </>
        }
      />

      <StepIndicator steps={steps} current={currentStep} />

      {actionError ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="error">{actionError}</Alert>
        </div>
      ) : null}

      {data.error ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="error">{t('imports.importFailed', { error: data.error })}</Alert>
        </div>
      ) : null}

      <div className="stack">
        <Card title={t('imports.source')}>
          <div className="grid grid--stats">
            <Stat label={t('common.status')} value={<StatusBadge status={data.status} />} />
            <Stat label={t('imports.entity')} value={data.entity_type ?? '—'} />
            <Stat label={t('imports.format')} value={data.format ?? '—'} />
            <Stat label={t('imports.columnSize')} value={formatBytes(data.size_bytes)} />
          </div>
          <dl className="dl" style={{ marginTop: 'var(--space-4)' }}>
            <dt>{t('imports.checksum')}</dt>
            <dd className="mono truncate">{data.checksum_sha256 ?? '—'}</dd>
            <dt>{t('imports.uploaded')}</dt>
            <dd>
              <Timestamp value={data.created_at} />
            </dd>
            {data.schema_info?.archive ? (
              <>
                                <dt>{t('imports.archive')}</dt>
                <dd>
                                    {t('imports.filesCount', { count: data.schema_info.files ?? 0 })}
                                    {data.schema_info.selected_file
                                      ? ` · ${t('imports.selectedFile', { name: data.schema_info.selected_file })}`
                                      : ''}
                </dd>
              </>
            ) : null}
          </dl>
        </Card>

        <Card
                    title={t('imports.analyseTitle')}
          actions={
            can('imports.read') ? (
              <button
                type="button"
                className="btn btn--secondary btn--sm"
                onClick={() => analyze.mutate()}
                disabled={!hasFile || analyze.isPending}
              >
                {analyze.isPending
                                  ? t('imports.analysing')
                                  : data.schema_info
                                    ? t('imports.reanalyse')
                                    : t('imports.analyse')}
              </button>
            ) : null
          }
        >
          {!hasFile ? (
            <Alert tone="warning">{t('imports.noFile')}</Alert>
          ) : data.schema_info ? (
            <div className="stack">
              <div className="grid grid--stats">
                <Stat label={t('imports.columnsFound')} value={columns.length} />
                <Stat label={t('imports.rowsDetected')} value={data.schema_info.total_rows ?? 0} />
                {data.schema_info.archive ? (
                  <Stat label={t('imports.filesInArchive')} value={data.schema_info.files ?? 0} />
                ) : null}
              </div>

              {data.schema_info.selected_file ? (
                <div className="row row--wrap">
                  <span className="text-muted text-sm">{t('imports.selectedInArchive')}</span>
                  <Badge tone="accent">{data.schema_info.selected_file}</Badge>
                </div>
              ) : null}

              {data.schema_info.warnings?.length ? (
                <Alert tone="warning">
                  <div>
                                        <strong>{t('imports.warnings')}</strong>
                    <ul style={{ margin: 'var(--space-1) 0 0', paddingLeft: 18 }}>
                      {data.schema_info.warnings.map((warning) => (
                        <li key={warning}>{warning}</li>
                      ))}
                    </ul>
                  </div>
                </Alert>
              ) : null}

              {columns.length > 0 ? (
                <div>
                  <h3 style={{ marginBottom: 'var(--space-2)' }}>{t('imports.detectedColumns')}</h3>
                  <div className="row row--wrap">
                    {columns.map((column) => (
                      <Badge key={column} tone="neutral">
                        {column}
                        {data.schema_info?.types?.[column] ? (
                          <span className="text-subtle">: {data.schema_info.types[column]}</span>
                        ) : null}
                      </Badge>
                    ))}
                  </div>
                </div>
              ) : null}

              {data.preview?.length ? (
                <div>
                  <h3 style={{ marginBottom: 'var(--space-2)' }}>{t('imports.preview')}</h3>
                  <div className="table-wrap">
                    <table className="table--stack">
                      <thead>
                        <tr>
                          {columns.map((column) => (
                            <th key={column}>{column}</th>
                          ))}
                        </tr>
                      </thead>
                      <tbody>
                        {data.preview.slice(0, 10).map((row, index) => (
                          <tr key={index}>
                            {columns.map((column) => (
                              <td
                                key={column}
                                className="truncate"
                                style={{ maxWidth: 220 }}
                                data-label={column}
                              >
                                {formatCell(row[column])}
                              </td>
                            ))}
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              ) : null}

              {data.schema_info.sheets?.length ? (
                <label className="field" style={{ maxWidth: 280 }}>
                  <span className="field__label">{t('imports.sheet')}</span>
                  <select
                    value={sheetName}
                    onChange={(event) => setSheetName(event.target.value)}
                  >
                    <option value="">{t('imports.firstSheet')}</option>
                    {data.schema_info.sheets.map((sheet) => (
                      <option key={sheet} value={sheet}>
                        {sheet}
                      </option>
                    ))}
                  </select>
                </label>
              ) : null}

              {data.schema_info.archive ? (
                <label className="field" style={{ maxWidth: 380 }}>
                  <span className="field__label">{t('imports.archiveEntry')}</span>
                  <input
                    value={archiveEntry}
                    onChange={(event) => setArchiveEntry(event.target.value)}
                    placeholder={data.schema_info.selected_file ?? 'data/contacts.csv'}
                  />
                                    <span className="field__hint">{t('imports.archiveEntryHint')}</span>
                </label>
              ) : null}
            </div>
          ) : (
            <p className="text-muted">{t('imports.analysePrompt')}</p>
          )}
        </Card>

        {columns.length > 0 && availableFields.length > 0 ? (
          <Card title={t('imports.mapFields')}>
            <p className="text-muted text-sm" style={{ marginBottom: 'var(--space-3)' }}>
                          {t('imports.mapFieldsHint')}
                        </p>
            <div className="table-wrap">
              <table className="table--stack">
                <thead>
                  <tr>
                    <th>{t('imports.targetField')}</th>
                    <th>{t('imports.sourceColumn')}</th>
                  </tr>
                </thead>
                <tbody>
                  {availableFields.map((field) => {
                    const selected = mapping.find((entry) => entry.target_field === field)
                    return (
                      <tr key={field}>
                        <td className="table__primary" data-label={t('imports.targetField')}>
                          {field}
                        </td>
                        <td data-label={t('imports.sourceColumn')}>
                          <select
                            aria-label={t('imports.sourceColumnFor', { field })}
                            value={selected?.source_column ?? ''}
                            onChange={(event) => {
                              const source = event.target.value
                              setMapping((previous) => [
                                ...previous.filter(
                                  (entry) =>
                                    entry.target_field !== field && entry.source_column !== source,
                                ),
                                ...(source ? [{ source_column: source, target_field: field }] : []),
                              ])
                            }}
                          >
                            <option value="">{t('imports.notMapped')}</option>
                            {columns.map((column) => (
                              <option key={column} value={column}>
                                {column}
                              </option>
                            ))}
                          </select>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
            <p className="text-sm text-muted" style={{ marginTop: 'var(--space-3)' }}>
                            {t('imports.mappedCount', {
                              mapped: mapping.length,
                              total: availableFields.length,
                            })}
            </p>
          </Card>
        ) : null}

        <Card title={t('imports.options')}>
          <div className="form-grid">
            <label className="field">
              <span className="field__label">{t('imports.onDuplicate')}</span>
              <select
                value={duplicates}
                onChange={(event) => setDuplicates(event.target.value as typeof duplicates)}
              >
                <option value="skip">{t('imports.duplicateSkip')}</option>
                                <option value="update">{t('imports.duplicateUpdate')}</option>
                                <option value="create">{t('imports.duplicateCreate')}</option>
                                <option value="fail">{t('imports.duplicateFail')}</option>
              </select>
                            <span className="field__hint">{t('imports.duplicatesHint')}</span>
            </label>
            <label className="field">
              <span className="field__label">{t('imports.externalSource')}</span>
              <input
                value={externalSource}
                onChange={(event) => setExternalSource(event.target.value)}
                placeholder="legacy-crm"
              />
                            <span className="field__hint">{t('imports.externalSourceHint')}</span>
            </label>
          </div>
        </Card>

        <Card
                    title={t('imports.runTitle')}
          actions={
            canExecute ? (
              <div className="row">
                <button
                  type="button"
                  className="btn btn--secondary btn--sm"
                  onClick={() => validate.mutate()}
                  disabled={validate.isPending || mapping.length === 0}
                >
                  {validate.isPending ? t('imports.checking') : t('imports.validate')}
                </button>
                <button
                  type="button"
                  className="btn btn--secondary btn--sm"
                  onClick={() => dryRun.mutate()}
                  disabled={dryRun.isPending || mapping.length === 0}
                >
                  {dryRun.isPending ? t('imports.running') : t('imports.dryRun')}
                </button>
                <button
                  type="button"
                  className="btn btn--primary btn--sm"
                  onClick={() => setConfirmExecute(true)}
                  disabled={isBusy || mapping.length === 0 || data.status === 'completed'}
                >
                  {data.status === 'completed' ? t('imports.imported') : t('imports.runImport')}
                </button>
              </div>
            ) : null
          }
        >
          {mapping.length === 0 ? (
            <p className="text-muted">{t('imports.mapFirst')}</p>
          ) : (
            <div className="stack">
              <p className="text-muted text-sm">{t('imports.runHint')}</p>
              {validateResult ? <ReportSummary title={t('imports.validation')} report={validateResult} /> : null}
              {dryRunResult ? <ReportSummary title={t('imports.dryRun')} report={dryRunResult} /> : null}
            </div>
          )}
        </Card>

        {data.status === 'completed' || data.imported_rows > 0 ? (
          <Card title={t('imports.result')}>
            <div className="grid grid--stats">
              <Stat label={t('imports.totalRows')} value={data.total_rows} />
              <Stat label={t('imports.imported')} value={data.imported_rows} />
              <Stat label={t('imports.skipped')} value={data.skipped_rows} />
              <Stat
                                label={t('imports.failed')}
                                value={data.failed_rows}
                                hint={data.failed_rows > 0 ? t('imports.failedHint') : undefined}
              />
            </div>
            <dl className="dl" style={{ marginTop: 'var(--space-4)' }}>
                            <dt>{t('imports.started')}</dt>
              <dd>
                <Timestamp value={data.started_at} />
              </dd>
                            <dt>{t('imports.finished')}</dt>
              <dd>
                <Timestamp value={data.finished_at} />
              </dd>
            </dl>
          </Card>
        ) : null}

        {errors.data && errors.data.total > 0 ? (
          <Card title={t('imports.rejectedRows', { count: errors.data.total })} flush>
            <div className="table-wrap">
              <table className="table--stack">
                <thead>
                  <tr>
                    <th className="numeric">{t('imports.columnRow')}</th>
                    <th>{t('imports.columnField')}</th>
                    <th>{t('imports.columnProblem')}</th>
                    <th>{t('imports.columnValue')}</th>
                  </tr>
                </thead>
                <tbody>
                  {errors.data.items.map((row, index) => (
                    <tr key={`${row.row_number}-${index}`}>
                      <td className="numeric" data-label={t('imports.columnRow')}>
                        {row.row_number}
                      </td>
                      <td data-label={t('imports.columnField')}>
                        {row.field ?? <span className="text-subtle">—</span>}
                      </td>
                      <td data-label={t('imports.columnProblem')}>{row.message}</td>
                      <td
                        className="truncate"
                        style={{ maxWidth: 260 }}
                        data-label={t('imports.columnValue')}
                      >
                        {row.value ?? <span className="text-subtle">—</span>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Card>
        ) : null}
      </div>

      <ConfirmDialog
        open={confirmExecute}
                title={t('imports.confirmTitle')}
                text={t('imports.confirmText', {
                  count: mapping.length,
                  entity: data.entity_type ?? t('imports.unknownEntity'),
                })}
                confirmLabel={t('imports.runImport')}
        busy={execute.isPending}
        onConfirm={() => execute.mutate()}
        onCancel={() => setConfirmExecute(false)}
      />
    </>
  )
}

function StepIndicator({ steps, current }: { steps: readonly string[]; current: number }) {
  return (
    <ol
      style={{
        display: 'flex',
        gap: 'var(--space-2)',
        listStyle: 'none',
        padding: 0,
        margin: '0 0 var(--space-4)',
        flexWrap: 'wrap',
      }}
    >
      {steps.map((step, index) => {
        const state = index < current ? 'done' : index === current ? 'active' : 'todo'
        return (
          <li key={step} className="row" style={{ gap: 'var(--space-1)' }}>
            <Badge tone={state === 'done' ? 'success' : state === 'active' ? 'accent' : 'neutral'}>
              {index + 1}
            </Badge>
            <span
              className={state === 'todo' ? 'text-subtle text-sm' : 'text-sm'}
              style={{ fontWeight: state === 'active' ? 600 : 400 }}
            >
              {step}
            </span>
          </li>
        )
      })}
    </ol>
  )
}

function ReportSummary({ title, report }: { title: string; report: ImportReport }) {
  const t = useT()
  return (
    <div className="card" style={{ background: 'var(--color-bg)' }}>
      <div className="card__header" style={{ padding: 'var(--space-3)' }}>
        <h3>{title}</h3>
        <StatusBadge status={report.status} />
      </div>
      <div className="card__body" style={{ padding: 'var(--space-3)' }}>
        <div className="row row--wrap">
          <Badge tone="neutral">
            {t('imports.rowsBadge', { count: report.total_rows })}
          </Badge>
          <Badge tone="success">
            {t('imports.imported')}: {report.imported_rows}
          </Badge>
          <Badge tone="warning">
            {t('imports.skipped')}: {report.skipped_rows}
          </Badge>
          <Badge tone={report.failed_rows > 0 ? 'danger' : 'neutral'}>
            {t('imports.failed')}: {report.failed_rows}
          </Badge>
        </div>
        {report.error ? (
          <div style={{ marginTop: 'var(--space-2)' }}>
            <Alert tone="error">{report.error}</Alert>
          </div>
        ) : null}
      </div>
    </div>
  )
}

/** Maps a source column name to a likely target field. */
function normalize(column: string): string {
  return column
    .trim()
    .toLowerCase()
    .replace(/[\s-]+/g, '_')
    .replace(/[^a-z0-9_]/g, '')
}

function formatCell(value: unknown): string {
  if (value === null || value === undefined) return ''
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}
