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
function triggerLabel(wf: Workflow): string {
  switch (wf.trigger_type) {
    case 'event':
      return `on ${wf.event_type ?? 'an event'}`
    case 'schedule':
      return `every ${wf.schedule_cron ?? '—'} (${wf.schedule_timezone ?? 'UTC'})`
    default:
      return 'manual'
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
            ? `The run failed: ${result.summary ?? 'no summary'}`
            : `The run ${result.status}.`,
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
      header: 'Workflow',
      render: (row) => (
        <div className="stack" style={{ gap: 2 }}>
          <strong>{row.name}</strong>
          {row.description ? <span className="text-subtle text-sm">{row.description}</span> : null}
        </div>
      ),
    },
    {
      key: 'trigger',
      header: 'Trigger',
      render: (row) => <span className="mono text-sm">{triggerLabel(row)}</span>,
    },
    {
      key: 'steps',
      header: 'Steps',
      numeric: true,
      render: (row) => row.definition?.steps?.length ?? 0,
    },
    {
      key: 'enabled',
      header: 'State',
      render: (row) =>
        row.enabled ? <Badge tone="success">enabled</Badge> : <Badge tone="neutral">disabled</Badge>,
    },
    {
      key: 'updated',
      header: 'Updated',
      render: (row) => <Timestamp value={row.updated_at} />,
    },
    {
      key: 'actions',
      header: '',
      width: '260px',
      render: (row) => (
        <div className="row" style={{ gap: 6, justifyContent: 'flex-end' }}>
          <button type="button" className="btn btn--ghost btn--sm" onClick={() => setHistoryFor(row)}>
            Runs
          </button>
          <button type="button" className="btn btn--ghost btn--sm" onClick={() => downloadExport(row)}>
            Export
          </button>
          {can('workflows.run') ? (
            <button
              type="button"
              className="btn btn--ghost btn--sm"
              disabled={!row.enabled || runMutation.isPending}
              title={row.enabled ? 'Run now' : 'Enable the workflow first'}
              onClick={() => runMutation.mutate(row.id)}
            >
              Run
            </button>
          ) : null}
          {can('workflows.manage') ? (
            <>
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                onClick={() => toggleMutation.mutate({ id: row.id, enabled: !row.enabled })}
              >
                {row.enabled ? 'Disable' : 'Enable'}
              </button>
              <button type="button" className="btn btn--ghost btn--sm" onClick={() => setEditing(row)}>
                Edit
              </button>
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                onClick={() => {
                  if (window.confirm(`Archive "${row.name}"? It will stop running.`)) {
                    archiveMutation.mutate(row.id)
                  }
                }}
              >
                Archive
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
        title="Workflows"
        meta="Automate what happens when a record changes, on a schedule, or on demand. A workflow runs as its author, with that author's permissions checked on every run."
      />

      {notice ? (
        <div className={`alert alert--${notice.tone}`} role="status" style={{ marginBottom: 'var(--space-4)' }}>
          <span>{notice.text}</span>
          <button type="button" className="btn btn--ghost btn--sm" onClick={() => setNotice(null)}>
            Dismiss
          </button>
        </div>
      ) : null}

      <ResourceList
        title="Workflow definitions"
        queryKey={['workflows']}
        fetchPage={(params) => workflowsApi.list(params)}
        columns={columns}
        rowKey={(row) => row.id}
        emptyTitle="No workflows yet"
        emptyText="A workflow reacts to an event, runs on a schedule, or runs when you ask it to. Start from a template or write one."
        searchable
        searchPlaceholder="Search workflows…"
        actions={
          can('workflows.manage') ? (
            <div className="row" style={{ gap: 6 }}>
              <button type="button" className="btn btn--secondary btn--sm" onClick={() => setBrowsingTemplates(true)}>
                From a template
              </button>
              <button type="button" className="btn btn--secondary btn--sm" onClick={() => setImporting(true)}>
                Import
              </button>
              <button type="button" className="btn btn--primary btn--sm" onClick={() => setCreating(true)}>
                New workflow
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
            setNotice({ tone: 'success', text: 'Saved.' })
            invalidate()
          }}
        />
      ) : null}

      {browsingTemplates ? (
        <TemplatePicker
          onClose={() => setBrowsingTemplates(false)}
          onCreated={(name) => {
            setBrowsingTemplates(false)
            setNotice({ tone: 'success', text: `Created "${name}" from a template.` })
            invalidate()
          }}
        />
      ) : null}

      {importing ? (
        <ImportDialog
          onClose={() => setImporting(false)}
          onImported={(name) => {
            setImporting(false)
            setNotice({ tone: 'success', text: `Imported "${name}".` })
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

  const validation = validateDefinition(steps)

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
        setError('The definition needs a "steps" array.')
        return
      }
      setSteps(parsed.steps as WorkflowStep[])
      setMode('structured')
    } catch {
      setError('The definition is not valid JSON.')
    }
  }

  function submit() {
    setError(null)
    let finalSteps = steps
    if (mode === 'json') {
      try {
        const parsed = JSON.parse(rawJSON)
        if (!parsed || !Array.isArray(parsed.steps) || parsed.steps.length === 0) {
          setError('The definition needs a non-empty "steps" array.')
          return
        }
        finalSteps = parsed.steps as WorkflowStep[]
        setSteps(finalSteps)
      } catch {
        setError('The definition is not valid JSON.')
        return
      }
    }
    if (!name.trim()) {
      setError('Give the workflow a name.')
      return
    }
    if (triggerType === 'event' && !eventType) {
      setError('Choose the event this workflow reacts to.')
      return
    }
    if (triggerType === 'schedule' && !cron.trim()) {
      setError('A schedule trigger needs a cron expression.')
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
          <h2 id="workflow-dialog-title">{isEdit ? 'Edit workflow' : 'New workflow'}</h2>
        </div>
        <div className="card__body stack">
          {error ? <div className="alert alert--danger" role="alert">{error}</div> : null}

          <label className="field" htmlFor="workflow-name">
            <span className="field__label">Name</span>
            <input
              id="workflow-name"
              className="input"
              value={name}
              maxLength={200}
              onChange={(e) => setName(e.target.value)}
              placeholder="Notify the owner when a deal is won"
            />
          </label>

          <label className="field" htmlFor="workflow-description">
            <span className="field__label">Description (optional)</span>
            <input
              id="workflow-description"
              className="input"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </label>

          <label className="field" htmlFor="workflow-trigger">
            <span className="field__label">Trigger</span>
            <select
              id="workflow-trigger"
              className="input"
              value={triggerType}
              onChange={(e) => setTriggerType(e.target.value as WorkflowInput['trigger_type'])}
            >
              <option value="manual">Manual — run when asked</option>
              <option value="event">Event — react to a committed event</option>
              <option value="schedule">Schedule — run on a cron</option>
            </select>
          </label>

          {triggerType === 'event' ? (
            <label className="field" htmlFor="workflow-event-type">
              <span className="field__label">Event type</span>
              <select
                id="workflow-event-type"
                className="input mono"
                value={eventType}
                onChange={(e) => setEventType(e.target.value)}
                disabled={eventsLoading}
              >
                <option value="">Choose an event…</option>
                {eventTypes.map((type) => (
                  <option key={type} value={type}>
                    {type}
                  </option>
                ))}
              </select>
              <span className="field__hint">
                The canonical event names the server emits. A workflow matches one of these
                exactly; a name that is not in this list never fires.
              </span>
            </label>
          ) : null}

          {triggerType === 'schedule' ? (
            <div className="row" style={{ gap: 'var(--space-3)', alignItems: 'flex-start' }}>
              <label className="field" htmlFor="workflow-cron" style={{ flex: 1 }}>
                <span className="field__label">Cron expression</span>
                <input
                  id="workflow-cron"
                  className="input mono"
                  value={cron}
                  onChange={(e) => setCron(e.target.value)}
                />
                <span className="field__hint">Five fields: minute hour day month weekday.</span>
              </label>
              <label className="field" htmlFor="workflow-timezone" style={{ flex: 1 }}>
                <span className="field__label">Timezone</span>
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
              <span className="field__label">Definition</span>
              <div className="row" style={{ gap: 6 }}>
                {mode === 'structured' ? (
                  <button type="button" className="btn btn--ghost btn--sm" onClick={switchToJSON}>
                    Edit as JSON
                  </button>
                ) : (
                  <button type="button" className="btn btn--ghost btn--sm" onClick={switchToStructured}>
                    Edit as form
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
                aria-label="workflow definition JSON"
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
              Steps: condition, branch, create_task, update_deal, create_ticket, notify, delay, find_deals,
              foreach. Use <code>{'{{path}}'}</code> to reference the triggering event; inside a foreach,{' '}
              <code>{'{{item.field}}'}</code> reads the current item.
            </span>
          </div>

          <label className="field" htmlFor="workflow-max-depth" style={{ maxWidth: 200 }}>
            <span className="field__label">Max chain depth</span>
            <input
              id="workflow-max-depth"
              className="input"
              type="number"
              min={1}
              max={20}
              value={maxDepth}
              onChange={(e) => setMaxDepth(Number(e.target.value))}
            />
            <span className="field__hint">Stops workflows that trigger each other.</span>
          </label>
        </div>
        <div className="form-actions" style={{ margin: 0, padding: 'var(--space-4)' }}>
          <button type="button" className="btn btn--secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="button" className="btn btn--primary" disabled={saveMutation.isPending} onClick={submit}>
            {saveMutation.isPending ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  )
}

function RunHistoryDrawer({ workflow, onClose }: { workflow: Workflow; onClose: () => void }) {
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
          <h2 id="run-history-title">Runs — {workflow.name}</h2>
        </div>
        <div className="card__body">
          {runsQuery.isLoading ? <p className="text-subtle">Loading…</p> : null}
          {runsQuery.isError ? (
            <div className="alert alert--danger" role="alert">{errorMessage(runsQuery.error)}</div>
          ) : null}
          {!runsQuery.isLoading && runs.length === 0 ? (
            <p className="text-subtle">This workflow has not run yet.</p>
          ) : null}

          {runs.length > 0 ? (
            <table className="table">
              <thead>
                <tr>
                  <th>Started</th>
                  <th>Trigger</th>
                  <th>Status</th>
                  <th>Steps</th>
                  <th>Depth</th>
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
                      {run.steps_failed > 0 ? <span className="text-danger"> ({run.steps_failed} failed)</span> : null}
                    </td>
                    <td className="numeric">{run.run_depth}</td>
                    <td>
                      <button type="button" className="btn btn--ghost btn--sm" onClick={() => setSelected(run.id)}>
                        Steps
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : null}

          {selected ? (
            <div style={{ marginTop: 'var(--space-4)' }}>
              <h3>Steps</h3>
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
    <Modal title="Start from a template" onClose={onClose}>
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
                        ? `on ${t.event_type}`
                        : t.trigger_type === 'schedule'
                          ? `every ${t.schedule_cron} (${t.schedule_timezone ?? 'UTC'})`
                          : 'manual'}{' '}
                      · {t.definition.steps.length} step(s)
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
          <span className="field__label">Name for the new workflow</span>
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
          {createMutation.isPending ? 'Creating…' : 'Create'}
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
      setError('That is not valid JSON.')
      return
    }
    importMutation.mutate(doc)
  }

  return (
    <Modal title="Import a workflow" onClose={onClose}>
      {error ? <div className="alert alert--danger" role="alert">{error}</div> : null}
      <label className="field" htmlFor="import-document">
        <span className="field__label">Portable document</span>
        <textarea
          id="import-document"
          className="input mono"
          rows={14}
          value={text}
          spellCheck={false}
          placeholder='{"format":"forgecrm.workflow","format_version":1,…}'
          onChange={(e) => setText(e.target.value)}
        />
        <span className="field__hint">
          Paste a document produced by Export. The server rejects a document whose format, version or event
          it does not recognise, and says which.
        </span>
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
          {importMutation.isPending ? 'Importing…' : 'Import'}
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
