import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { errorMessage } from '../api/client'
import { systemApi } from '../api/admin'
import {
  workflowsApi,
  type Workflow,
  type WorkflowExportDocument,
  type WorkflowInput,
  type WorkflowRun,
  type WorkflowStep,
  type WorkflowTemplate,
} from '../api/workflows'
import { ResourceList, type Column } from '../components/ResourceList'
import { Badge, PageHeader, StatusBadge, Timestamp } from '../components/ui'
import { WorkflowEditor, definitionToJSON, validateDefinition } from '../components/WorkflowEditor'
import { useAuth } from '../hooks/useAuth'
import { useT, type TFunction } from '../i18n'

/**
 * The event vocabulary is read from the server, never hard-coded.
 *
 * An event-triggered workflow matches on the exact committed event name, which is
 * namespaced ("crm.deal.won"). A form that offers or suggests a bare name
 * ("deal.won") produces a workflow that is accepted, enabled and then never fires —
 * the same silent-no-match failure the webhook form once had. The canonical list is
 * `event_types` on `/system/capabilities`, which is `outbox.EventTypes()`.
 */
function useEventTypes() {
  const query = useQuery({
    queryKey: ['system', 'capabilities'],
    queryFn: systemApi.capabilities,
    staleTime: 5 * 60 * 1000,
  })
  return { eventTypes: query.data?.event_types ?? [], isLoading: query.isLoading }
}

/** The trigger of a workflow, rendered as one line. */
function triggerLabel(t: TFunction, wf: Workflow): string {
  switch (wf.trigger_type) {
    case 'event':
      return t('workflows.triggerOnEvent', { event: wf.event_type ?? t('workflows.anEvent') })
    case 'schedule':
      return t('workflows.triggerEvery', {
        cron: wf.schedule_cron ?? '—',
        timezone: wf.schedule_timezone ?? 'UTC',
      })
    default:
      return t('workflows.triggerManual')
  }
}

/**
 * Fetches a workflow's portable document and hands it to the browser as a download.
 *
 * The document is the server's export, not a client-built JSON: a client that
 * assembled one could drift from the import contract, and the whole point of a
 * portable document is that the server produces and consumes the same shape.
 */
async function downloadExport(wf: Workflow) {
  const doc = await workflowsApi.export(wf.id)
  const blob = new Blob([JSON.stringify(doc, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${wf.name.replace(/[^a-z0-9._-]+/gi, '-').toLowerCase()}.workflow.json`
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

export function WorkflowsPage() {
  const { can } = useAuth()
  const t = useT()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<Workflow | null>(null)
  const [creating, setCreating] = useState(false)
  const [historyFor, setHistoryFor] = useState<Workflow | null>(null)
  const [browsingTemplates, setBrowsingTemplates] = useState(false)
  const [importing, setImporting] = useState(false)
  const [notice, setNotice] = useState<{ tone: 'success' | 'danger'; text: string } | null>(null)

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['workflows'] })

  const runMutation = useMutation({
    mutationFn: (id: string) => workflowsApi.run(id),
    onSuccess: (result) => {
      setNotice({
        tone: result.status === 'failed' ? 'danger' : 'success',
        text:
          result.status === 'failed'
            ? t('workflows.runFailed', { summary: result.summary ?? t('workflows.noSummary') })
                        : t('workflows.runStatus', { status: result.status }),
      })
      invalidate()
    },
    onError: (error) => setNotice({ tone: 'danger', text: errorMessage(error) }),
  })

  const toggleMutation = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => workflowsApi.setEnabled(id, enabled),
    onSuccess: invalidate,
    onError: (error) => setNotice({ tone: 'danger', text: errorMessage(error) }),
  })

  const archiveMutation = useMutation({
    mutationFn: (id: string) => workflowsApi.archive(id),
    onSuccess: invalidate,
    onError: (error) => setNotice({ tone: 'danger', text: errorMessage(error) }),
  })

  const columns: Column<Workflow>[] = [
    {
            key: 'name',
            header: t('workflows.columnWorkflow'),
      render: (row) => (
        <div className="stack" style={{ gap: 2 }}>
          <strong>{row.name}</strong>
          {row.description ? <span className="text-subtle text-sm">{row.description}</span> : null}
        </div>
      ),
    },
    {
      key: 'trigger',
            header: t('workflows.columnTrigger'),
            render: (row) => <span className="mono text-sm">{triggerLabel(t, row)}</span>,
    },
    {
      key: 'steps',
            header: t('workflows.columnSteps'),
      numeric: true,
      render: (row) => row.definition?.steps?.length ?? 0,
    },
    {
      key: 'enabled',
            header: t('workflows.columnState'),
      render: (row) =>
        row.enabled ? (
                  <Badge tone="success">{t('workflows.enabled')}</Badge>
                ) : (
                  <Badge tone="neutral">{t('workflows.disabled')}</Badge>
                ),
    },
    {
      key: 'updated',
            header: t('common.updated'),
      render: (row) => <Timestamp value={row.updated_at} />,
    },
    {
      key: 'actions',
      header: '',
      width: '260px',
      render: (row) => (
        <div className="row" style={{ gap: 6, justifyContent: 'flex-end' }}>
          <button type="button" className="btn btn--ghost btn--sm" onClick={() => setHistoryFor(row)}>
            {t('workflows.runs')}
                      </button>
          <button type="button" className="btn btn--ghost btn--sm" onClick={() => downloadExport(row)}>
            {t('workflows.export')}
                      </button>
          {can('workflows.run') ? (
            <button
              type="button"
              className="btn btn--ghost btn--sm"
              disabled={!row.enabled || runMutation.isPending}
              title={row.enabled ? t('workflows.runNow') : t('workflows.enableFirst')}
              onClick={() => runMutation.mutate(row.id)}
            >
              {t('workflows.run')}
                          </button>
          ) : null}
          {can('workflows.manage') ? (
            <>
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                onClick={() => toggleMutation.mutate({ id: row.id, enabled: !row.enabled })}
              >
                {row.enabled ? t('workflows.disable') : t('workflows.enable')}
              </button>
              <button type="button" className="btn btn--ghost btn--sm" onClick={() => setEditing(row)}>
                {t('workflows.edit')}
                              </button>
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                onClick={() => {
                  if (window.confirm(t('workflows.archiveConfirm', { name: row.name }))) {
                    archiveMutation.mutate(row.id)
                  }
                }}
              >
                {t('workflows.archive')}
                              </button>
            </>
          ) : null}
        </div>
      ),
    },
  ]

  return (
    <>
      <PageHeader
                title={t('workflows.title')}
                meta={t('workflows.meta')}
              />

      {notice ? (
        <div className={`alert alert--${notice.tone}`} role="status" style={{ marginBottom: 'var(--space-4)' }}>
          <span>{notice.text}</span>
          <button type="button" className="btn btn--ghost btn--sm" onClick={() => setNotice(null)}>
            {t('common.close')}
                      </button>
        </div>
      ) : null}

      <ResourceList
                title={t('workflows.definitions')}
        queryKey={['workflows']}
        fetchPage={(params) => workflowsApi.list(params)}
        columns={columns}
        rowKey={(row) => row.id}
                emptyTitle={t('workflows.emptyTitle')}
                emptyText={t('workflows.emptyText')}
        searchable
                searchPlaceholder={t('workflows.searchPlaceholder')}
        actions={
          can('workflows.manage') ? (
            <div className="row" style={{ gap: 6 }}>
              <button type="button" className="btn btn--secondary btn--sm" onClick={() => setBrowsingTemplates(true)}>
                {t('workflows.fromTemplate')}
                              </button>
              <button type="button" className="btn btn--secondary btn--sm" onClick={() => setImporting(true)}>
                {t('workflows.import')}
                              </button>
              <button type="button" className="btn btn--primary btn--sm" onClick={() => setCreating(true)}>
                {t('workflows.newWorkflow')}
                              </button>
            </div>
          ) : null
        }
      />

      {creating || editing ? (
        <WorkflowDialog
          workflow={editing}
          onClose={() => {
            setCreating(false)
            setEditing(null)
          }}
          onSaved={() => {
            setCreating(false)
            setEditing(null)
            setNotice({ tone: 'success', text: t('workflows.saved') })
            invalidate()
          }}
        />
      ) : null}

      {browsingTemplates ? (
        <TemplatePicker
          onClose={() => setBrowsingTemplates(false)}
          onCreated={(name) => {
            setBrowsingTemplates(false)
            setNotice({ tone: 'success', text: t('workflows.createdFromTemplate', { name }) })
            invalidate()
          }}
        />
      ) : null}

      {importing ? (
        <ImportDialog
          onClose={() => setImporting(false)}
          onImported={(name) => {
            setImporting(false)
            setNotice({ tone: 'success', text: t('workflows.importedNotice', { name }) })
            invalidate()
          }}
        />
      ) : null}

      {historyFor ? <RunHistoryDrawer workflow={historyFor} onClose={() => setHistoryFor(null)} /> : null}
    </>
  )
}

interface WorkflowDialogProps {
  workflow: Workflow | null
  onClose: () => void
  onSaved: () => void
}

/**
 * The editor. The definition is edited as JSON, which is the contract the API
 * publishes; a form per step type would have to be kept in step with the engine and
 * would hide the shape a user needs to understand.
 */
function WorkflowDialog({ workflow, onClose, onSaved }: WorkflowDialogProps) {
  const t = useT()
  const isEdit = workflow !== null
  const [name, setName] = useState(workflow?.name ?? '')
  const [description, setDescription] = useState(workflow?.description ?? '')
  const [triggerType, setTriggerType] = useState<WorkflowInput['trigger_type']>(
    workflow?.trigger_type ?? 'manual',
  )
  const [eventType, setEventType] = useState(workflow?.event_type ?? '')
  const [cron, setCron] = useState(workflow?.schedule_cron ?? '0 9 * * 1-5')
  const [timezone, setTimezone] = useState(workflow?.schedule_timezone ?? 'UTC')
  const [maxDepth, setMaxDepth] = useState(workflow?.max_depth ?? 5)
  const { eventTypes, isLoading: eventsLoading } = useEventTypes()
  const [steps, setSteps] = useState<WorkflowStep[]>(
    () =>
      workflow?.definition?.steps ?? [
        { id: 'notify', type: 'create_task', args: { title: 'Follow up', priority: 'normal' } },
      ],
  )
  // The raw JSON view and the structured view are two renderings of the same steps.
  // Switching to raw serialises the current steps; switching back parses what is in
  // the textarea, so an edit made in either view is what gets saved.
  const [mode, setMode] = useState<'structured' | 'json'>('structured')
  const [rawJSON, setRawJSON] = useState(() => definitionToJSON(steps))
  const [error, setError] = useState<string | null>(null)

  const validation = validateDefinition(steps, t)

  const saveMutation = useMutation({
    mutationFn: (body: WorkflowInput) =>
      isEdit ? workflowsApi.update(workflow.id, body) : workflowsApi.create(body),
    onSuccess: onSaved,
    onError: (err) => setError(errorMessage(err)),
  })

  function switchToJSON() {
    setRawJSON(definitionToJSON(steps))
    setMode('json')
  }

  function switchToStructured() {
    setError(null)
    try {
      const parsed = JSON.parse(rawJSON)
            if (!parsed || !Array.isArray(parsed.steps)) {
              setError(t('workflows.definitionNeedsSteps'))
              return
            }
            setSteps(parsed.steps as WorkflowStep[])
            setMode('structured')
          } catch {
            setError(t('workflows.definitionNotJSON'))
          }
        }

        function submit() {
          setError(null)
          let finalSteps = steps
          if (mode === 'json') {
            try {
              const parsed = JSON.parse(rawJSON)
              if (!parsed || !Array.isArray(parsed.steps) || parsed.steps.length === 0) {
                setError(t('workflows.definitionNeedsStepsNonEmpty'))
                return
              }
              finalSteps = parsed.steps as WorkflowStep[]
              setSteps(finalSteps)
            } catch {
              setError(t('workflows.definitionNotJSON'))
              return
            }
    }
    if (!name.trim()) {
      setError(t('workflows.nameRequired'))
      return
    }
    if (triggerType === 'event' && !eventType) {
      setError(t('workflows.chooseEvent'))
      return
    }
    if (triggerType === 'schedule' && !cron.trim()) {
      setError(t('workflows.cronRequired'))
      return
    }
    const body: WorkflowInput = {
      name: name.trim(),
      definition: { steps: finalSteps },
      trigger_type: triggerType,
      max_depth: maxDepth,
    }
    if (description.trim()) body.description = description.trim()
    if (triggerType === 'event') body.event_type = eventType.trim()
    if (triggerType === 'schedule') {
      body.schedule_cron = cron.trim()
      body.schedule_timezone = timezone.trim()
    }
    saveMutation.mutate(body)
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="workflow-dialog-title"
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
          <h2 id="workflow-dialog-title">
                      {isEdit ? t('workflows.editTitle') : t('workflows.newTitle')}
                    </h2>
        </div>
        <div className="card__body stack">
          {error ? <div className="alert alert--danger" role="alert">{error}</div> : null}

          <label className="field" htmlFor="workflow-name">
                        <span className="field__label">{t('common.name')}</span>
            <input
              id="workflow-name"
              className="input"
              value={name}
              maxLength={200}
              onChange={(e) => setName(e.target.value)}
              placeholder={t('workflows.namePlaceholder')}
            />
          </label>

          <label className="field" htmlFor="workflow-description">
                        <span className="field__label">{t('workflows.descriptionOptional')}</span>
            <input
              id="workflow-description"
              className="input"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </label>

          <label className="field" htmlFor="workflow-trigger">
                        <span className="field__label">{t('workflows.trigger')}</span>
            <select
              id="workflow-trigger"
              className="input"
              value={triggerType}
              onChange={(e) => setTriggerType(e.target.value as WorkflowInput['trigger_type'])}
            >
              <option value="manual">{t('workflows.triggerManualOption')}</option>
                            <option value="event">{t('workflows.triggerEventOption')}</option>
                            <option value="schedule">{t('workflows.triggerScheduleOption')}</option>
            </select>
          </label>

          {triggerType === 'event' ? (
            <label className="field" htmlFor="workflow-event-type">
                            <span className="field__label">{t('workflows.eventType')}</span>
              <select
                id="workflow-event-type"
                className="input mono"
                value={eventType}
                onChange={(e) => setEventType(e.target.value)}
                disabled={eventsLoading}
              >
                                <option value="">{t('workflows.chooseEventOption')}</option>
                {eventTypes.map((type) => (
                  <option key={type} value={type}>
                    {type}
                  </option>
                ))}
              </select>
                            <span className="field__hint">{t('workflows.eventHint')}</span>
            </label>
          ) : null}

          {triggerType === 'schedule' ? (
            <div className="row" style={{ gap: 'var(--space-3)', alignItems: 'flex-start' }}>
              <label className="field" htmlFor="workflow-cron" style={{ flex: 1 }}>
                                <span className="field__label">{t('workflows.cronExpression')}</span>
                <input
                  id="workflow-cron"
                  className="input mono"
                  value={cron}
                  onChange={(e) => setCron(e.target.value)}
                />
                                <span className="field__hint">{t('workflows.cronHint')}</span>
              </label>
              <label className="field" htmlFor="workflow-timezone" style={{ flex: 1 }}>
                                <span className="field__label">{t('workflows.timezone')}</span>
                <input
                  id="workflow-timezone"
                  className="input mono"
                  value={timezone}
                  onChange={(e) => setTimezone(e.target.value)}
                />
              </label>
            </div>
          ) : null}

          <div className="field">
            <div className="row" style={{ justifyContent: 'space-between', alignItems: 'center' }}>
                            <span className="field__label">{t('workflows.definition')}</span>
              <div className="row" style={{ gap: 6 }}>
                {mode === 'structured' ? (
                  <button type="button" className="btn btn--ghost btn--sm" onClick={switchToJSON}>
                    {t('workflows.editAsJson')}
                                      </button>
                ) : (
                  <button type="button" className="btn btn--ghost btn--sm" onClick={switchToStructured}>
                    {t('workflows.editAsForm')}
                                      </button>
                )}
              </div>
            </div>

            {mode === 'structured' ? (
              <WorkflowEditor steps={steps} eventTypes={eventTypes} onChange={setSteps} />
            ) : (
              <textarea
                id="workflow-definition"
                className="input mono"
                rows={14}
                value={rawJSON}
                                aria-label={t('workflows.definitionAria')}
                onChange={(e) => setRawJSON(e.target.value)}
                spellCheck={false}
              />
            )}

            {mode === 'structured' && !validation.ok ? (
              <ul className="text-danger text-sm" style={{ margin: 0, paddingLeft: 18 }} role="alert">
                {validation.errors.map((msg) => (
                  <li key={msg}>{msg}</li>
                ))}
              </ul>
            ) : null}

                        <span className="field__hint">
                          {t('workflows.stepsHint', {
                            open: '{{path}}',
                            item: '{{item.field}}',
                          })}
                        </span>
          </div>

          <label className="field" htmlFor="workflow-max-depth" style={{ maxWidth: 200 }}>
                        <span className="field__label">{t('workflows.maxDepth')}</span>
            <input
              id="workflow-max-depth"
              className="input"
              type="number"
              min={1}
              max={20}
              value={maxDepth}
              onChange={(e) => setMaxDepth(Number(e.target.value))}
            />
                        <span className="field__hint">{t('workflows.maxDepthHint')}</span>
          </label>
        </div>
        <div className="form-actions" style={{ margin: 0, padding: 'var(--space-4)' }}>
          <button type="button" className="btn btn--secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="button" className="btn btn--primary" disabled={saveMutation.isPending} onClick={submit}>
            {saveMutation.isPending ? t('common.saving') : t('common.save')}
          </button>
        </div>
      </div>
    </div>
  )
}

function RunHistoryDrawer({ workflow, onClose }: { workflow: Workflow; onClose: () => void }) {
  const t = useT()
  const [selected, setSelected] = useState<string | null>(null)

  const runsQuery = useQuery({
    queryKey: ['workflows', workflow.id, 'runs'],
    queryFn: () => workflowsApi.runs(workflow.id, { limit: 50 }),
  })

  const stepsQuery = useQuery({
    queryKey: ['workflow-runs', selected, 'steps'],
    queryFn: () => workflowsApi.runSteps(selected as string),
    enabled: selected !== null,
  })

  const runs: WorkflowRun[] = runsQuery.data?.items ?? []

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="run-history-title"
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
      <div className="card" style={{ maxWidth: 900, margin: '0 auto' }} onClick={(e) => e.stopPropagation()}>
        <div className="card__header">
          <h2 id="run-history-title">{t('workflows.runsTitle', { name: workflow.name })}</h2>
        </div>
        <div className="card__body">
          {runsQuery.isLoading ? <p className="text-subtle">Loading…</p> : null}
          {runsQuery.isError ? (
            <div className="alert alert--danger" role="alert">{errorMessage(runsQuery.error)}</div>
          ) : null}
          {!runsQuery.isLoading && runs.length === 0 ? (
            <p className="text-subtle">{t('workflows.notRunYet')}</p>
          ) : null}

          {runs.length > 0 ? (
            <div className="table-wrap">
              <table className="table">
                <thead>
                  <tr>
                                        <th>{t('workflows.columnStarted')}</th>
                                        <th>{t('workflows.columnTrigger')}</th>
                                        <th>{t('common.status')}</th>
                                        <th>{t('workflows.columnSteps')}</th>
                                        <th>{t('workflows.columnDepth')}</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {runs.map((run) => (
                    <tr key={run.id}>
                      <td>
                        <Timestamp value={run.started_at} />
                      </td>
                      <td className="mono text-sm">{run.trigger_type}</td>
                      <td>
                        <StatusBadge status={run.status} />
                      </td>
                      <td className="numeric">
                        {run.steps_succeeded}/{run.steps_total}
                        {run.steps_failed > 0 ? (
                                                  <span className="text-danger">
                                                    {t('workflows.failedSuffix', { count: run.steps_failed })}
                                                  </span>
                                                ) : null}
                      </td>
                      <td className="numeric">{run.run_depth}</td>
                      <td>
                        <button type="button" className="btn btn--ghost btn--sm" onClick={() => setSelected(run.id)}>
                          {t('workflows.stepsCount')}
                                                  </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}

          {selected ? (
            <div style={{ marginTop: 'var(--space-4)' }}>
                            <h3>{t('workflows.steps')}</h3>
              {stepsQuery.isLoading ? <p className="text-subtle">Loading…</p> : null}
              {stepsQuery.isError ? (
                <div className="alert alert--danger" role="alert">{errorMessage(stepsQuery.error)}</div>
              ) : null}
              <ul className="stack" style={{ listStyle: 'none', padding: 0 }}>
                {(stepsQuery.data?.items ?? []).map((step) => (
                  <li key={step.id} className="row" style={{ gap: 'var(--space-3)', alignItems: 'baseline' }}>
                    <span className="mono text-sm" style={{ minWidth: 28 }}>{step.step_index}</span>
                    <Badge tone="neutral">{step.step_type}</Badge>
                    <StatusBadge status={step.status} />
                    <span className="text-subtle text-sm">{step.error ?? ''}</span>
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
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

/**
 * The template picker.
 *
 * The catalog comes from the server, so a template cannot be offered that the
 * server would reject: each entry is created through `/workflow-templates/{id}/create`,
 * which re-validates the template's definition on the server before storing it. The
 * event each template triggers on is shown, because a template that names an event
 * the platform does not emit is the failure this avoids — it would be created,
 * enabled and never fire.
 */
function TemplatePicker({ onClose, onCreated }: { onClose: () => void; onCreated: (name: string) => void }) {
  const tr = useT()
  const [selected, setSelected] = useState<WorkflowTemplate | null>(null)
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)

  const templatesQuery = useQuery({
    queryKey: ['workflow-templates'],
    queryFn: () => workflowsApi.templates(),
    staleTime: 5 * 60 * 1000,
  })

  const createMutation = useMutation({
    mutationFn: () => workflowsApi.createFromTemplate(selected!.id, name.trim() || undefined),
    onSuccess: (wf) => onCreated(wf.name),
    onError: (err) => setError(errorMessage(err)),
  })

  const templates = templatesQuery.data?.items ?? []
  const byCategory = templates.reduce<Record<string, WorkflowTemplate[]>>((acc, t) => {
    ;(acc[t.category] ??= []).push(t)
    return acc
  }, {})

  return (
    <Modal title={tr('workflows.templateTitle')} onClose={onClose}>
      {error ? <div className="alert alert--danger" role="alert">{error}</div> : null}
      {templatesQuery.isLoading ? <p className="text-subtle">Loading…</p> : null}
      {templatesQuery.isError ? (
        <div className="alert alert--danger" role="alert">{errorMessage(templatesQuery.error)}</div>
      ) : null}

      {Object.entries(byCategory).map(([category, items]) => (
        <div key={category} className="stack" style={{ gap: 6 }}>
          <h3 style={{ textTransform: 'capitalize' }}>{category}</h3>
          <ul className="stack" style={{ listStyle: 'none', padding: 0 }}>
            {items.map((t) => (
              <li key={t.id}>
                <button
                  type="button"
                  className="card"
                  style={{
                    width: '100%',
                    textAlign: 'left',
                    cursor: 'pointer',
                    borderColor: selected?.id === t.id ? 'var(--color-accent)' : undefined,
                  }}
                  onClick={() => {
                    setSelected(t)
                    setName(t.name)
                    setError(null)
                  }}
                >
                  <div className="card__body stack" style={{ gap: 4 }}>
                    <strong>{t.name}</strong>
                    <span className="text-subtle text-sm">{t.description}</span>
                    <span className="mono text-sm">
                      {t.trigger_type === 'event'
                        ? tr('workflows.triggerOnEvent', { event: t.event_type ?? tr('workflows.anEvent') })
                        : t.trigger_type === 'schedule'
                          ? tr('workflows.triggerEvery', {
                              cron: t.schedule_cron ?? '—',
                              timezone: t.schedule_timezone ?? 'UTC',
                            })
                          : tr('workflows.triggerManual')}{' '}
                      · {t.definition.steps.length}
                    </span>
                  </div>
                </button>
              </li>
            ))}
          </ul>
        </div>
      ))}

      {selected ? (
        <label className="field" htmlFor="template-name">
                    <span className="field__label">{tr('workflows.templateNameLabel')}</span>
          <input id="template-name" className="input" value={name} onChange={(e) => setName(e.target.value)} />
        </label>
      ) : null}

      <div className="form-actions" style={{ margin: 0 }}>
        <button type="button" className="btn btn--secondary" onClick={onClose}>
          Cancel
        </button>
        <button
          type="button"
          className="btn btn--primary"
          disabled={!selected || createMutation.isPending}
          onClick={() => createMutation.mutate()}
        >
          {createMutation.isPending ? tr('workflows.creating') : tr('common.create')}
        </button>
      </div>
    </Modal>
  )
}

/**
 * The import dialog. The document is parsed on the client only to catch an obvious
 * mistake early; the server is what validates the format, the version, the
 * definition and the event, and its message is shown verbatim so a refused import
 * says why.
 */
function ImportDialog({ onClose, onImported }: { onClose: () => void; onImported: (name: string) => void }) {
  const t = useT()
  const [text, setText] = useState('')
  const [error, setError] = useState<string | null>(null)

  const importMutation = useMutation({
    mutationFn: (doc: WorkflowExportDocument) => workflowsApi.import(doc),
    onSuccess: (wf) => onImported(wf.name),
    onError: (err) => setError(errorMessage(err)),
  })

  function submit() {
    setError(null)
    let doc: WorkflowExportDocument
    try {
      doc = JSON.parse(text) as WorkflowExportDocument
    } catch {
      setError(t('workflows.notValidJSON'))
      return
    }
    importMutation.mutate(doc)
  }

  return (
    <Modal title={t('workflows.importTitle')} onClose={onClose}>
      {error ? <div className="alert alert--danger" role="alert">{error}</div> : null}
      <label className="field" htmlFor="import-document">
                <span className="field__label">{t('workflows.importLabel')}</span>
        <textarea
          id="import-document"
          className="input mono"
          rows={14}
          value={text}
          spellCheck={false}
          placeholder='{"format":"forgecrm.workflow","format_version":1,…}'
          onChange={(e) => setText(e.target.value)}
        />
                <span className="field__hint">{t('workflows.importHint')}</span>
      </label>
      <div className="form-actions" style={{ margin: 0 }}>
        <button type="button" className="btn btn--secondary" onClick={onClose}>
          Cancel
        </button>
        <button
          type="button"
          className="btn btn--primary"
          disabled={!text.trim() || importMutation.isPending}
          onClick={submit}
        >
          {importMutation.isPending ? t('workflows.importing') : t('workflows.import')}
        </button>
      </div>
    </Modal>
  )
}

/** A shared modal shell, so every dialog has the same focus and dismissal behaviour. */
function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: React.ReactNode }) {
  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={title}
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
          <h2>{title}</h2>
        </div>
        <div className="card__body stack">{children}</div>
      </div>
    </div>
  )
}
