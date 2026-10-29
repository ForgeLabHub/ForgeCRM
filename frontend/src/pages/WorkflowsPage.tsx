import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { errorMessage } from '../api/client'
import {
  workflowsApi,
  type Workflow,
  type WorkflowInput,
  type WorkflowRun,
} from '../api/workflows'
import { ResourceList, type Column } from '../components/ResourceList'
import { Badge, PageHeader, StatusBadge, Timestamp } from '../components/ui'
import { useAuth } from '../hooks/useAuth'

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

export function WorkflowsPage() {
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<Workflow | null>(null)
  const [creating, setCreating] = useState(false)
  const [historyFor, setHistoryFor] = useState<Workflow | null>(null)
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
        emptyText="A workflow reacts to an event, runs on a schedule, or runs when you ask it to."
        searchable
        searchPlaceholder="Search workflows…"
        actions={
          can('workflows.manage') ? (
            <button type="button" className="btn btn--primary btn--sm" onClick={() => setCreating(true)}>
              New workflow
            </button>
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
  const [definition, setDefinition] = useState(() =>
    JSON.stringify(
      workflow?.definition ?? {
        steps: [{ id: 'notify', type: 'create_task', args: { title: 'Follow up', priority: 'normal' } }],
      },
      null,
      2,
    ),
  )
  const [error, setError] = useState<string | null>(null)

  const saveMutation = useMutation({
    mutationFn: (body: WorkflowInput) =>
      isEdit ? workflowsApi.update(workflow.id, body) : workflowsApi.create(body),
    onSuccess: onSaved,
    onError: (err) => setError(errorMessage(err)),
  })

  function submit() {
    setError(null)
    let parsed: { steps: unknown[] }
    try {
      parsed = JSON.parse(definition)
    } catch {
      setError('The definition is not valid JSON.')
      return
    }
    if (!parsed || !Array.isArray(parsed.steps) || parsed.steps.length === 0) {
      setError('The definition needs a non-empty "steps" array.')
      return
    }
    const body: WorkflowInput = {
      name: name.trim(),
      definition: parsed as WorkflowInput['definition'],
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
        background: 'rgba(16, 24, 40, 0.45)',
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

          <label className="field">
            <span className="field__label">Name</span>
            <input
              className="input"
              value={name}
              maxLength={200}
              onChange={(e) => setName(e.target.value)}
              placeholder="Notify the owner when a deal is won"
            />
          </label>

          <label className="field">
            <span className="field__label">Description (optional)</span>
            <input className="input" value={description} onChange={(e) => setDescription(e.target.value)} />
          </label>

          <label className="field">
            <span className="field__label">Trigger</span>
            <select
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
            <label className="field">
              <span className="field__label">Event type</span>
              <input
                className="input mono"
                value={eventType}
                onChange={(e) => setEventType(e.target.value)}
                placeholder="deal.won"
              />
              <span className="field__hint">The event type as it appears in the event log.</span>
            </label>
          ) : null}

          {triggerType === 'schedule' ? (
            <div className="row" style={{ gap: 'var(--space-3)', alignItems: 'flex-start' }}>
              <label className="field" style={{ flex: 1 }}>
                <span className="field__label">Cron expression</span>
                <input className="input mono" value={cron} onChange={(e) => setCron(e.target.value)} />
                <span className="field__hint">Five fields: minute hour day month weekday.</span>
              </label>
              <label className="field" style={{ flex: 1 }}>
                <span className="field__label">Timezone</span>
                <input className="input mono" value={timezone} onChange={(e) => setTimezone(e.target.value)} />
              </label>
            </div>
          ) : null}

          <label className="field">
            <span className="field__label">Definition (JSON)</span>
            <textarea
              className="input mono"
              rows={14}
              value={definition}
              onChange={(e) => setDefinition(e.target.value)}
              spellCheck={false}
            />
            <span className="field__hint">
              Steps: condition, branch, create_task, update_deal, create_ticket, notify, delay. Use{' '}
              <code>{'{{path}}'}</code> to reference the triggering event.
            </span>
          </label>

          <label className="field" style={{ maxWidth: 200 }}>
            <span className="field__label">Max chain depth</span>
            <input
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
        background: 'rgba(16, 24, 40, 0.45)',
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
