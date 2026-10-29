/**
 * A structured editor for a workflow definition.
 *
 * The engine's contract is a JSON document; the editor is a view over that same
 * document, not a second format. It never invents a step type or an operator: the
 * lists come from `WORKFLOW_STEP_TYPES` and `CONDITION_OPERATORS`, which mirror the
 * engine, and the event list comes from the server. The JSON textarea remains
 * available for anything the structured controls do not cover, and the two views are
 * kept in step through one `onChange`.
 *
 * Validation here is a courtesy, not the contract. The server validates again, and a
 * definition the server rejects is reported as the server reported it — a client
 * check that disagreed with the engine would be worse than none.
 */
import {
  CONDITION_OPERATORS,
  WORKFLOW_STEP_TYPES,
  type WorkflowCondition,
  type WorkflowStep,
} from '../api/workflows'
import { Badge } from './ui'

export interface StepValidation {
  ok: boolean
  errors: string[]
}

/** Validates a definition the way the engine's parser does, for early feedback. */
export function validateDefinition(steps: WorkflowStep[]): StepValidation {
  const errors: string[] = []
  const ids = new Set<string>()

  const walk = (list: WorkflowStep[], inForeach: boolean) => {
    if (!Array.isArray(list) || list.length === 0) {
      errors.push('A step list cannot be empty.')
      return
    }
    for (const step of list) {
      if (!step.id?.trim()) errors.push('Every step needs an id.')
      if (step.id && ids.has(step.id)) errors.push(`Duplicate step id "${step.id}".`)
      if (step.id) ids.add(step.id)
      if (!WORKFLOW_STEP_TYPES.includes(step.type as (typeof WORKFLOW_STEP_TYPES)[number])) {
        errors.push(`Unknown step type "${step.type}".`)
        continue
      }
      if (inForeach && (step.type === 'condition' || step.type === 'delay' || step.type === 'foreach' || step.type === 'branch')) {
        errors.push(`A foreach body cannot contain a "${step.type}" step.`)
      }
      if (step.type === 'condition' || step.type === 'branch') {
        if (!step.condition?.field?.trim()) errors.push(`Step "${step.id}" needs a condition field.`)
        if (!step.condition?.op) errors.push(`Step "${step.id}" needs an operator.`)
      }
      if (step.type === 'branch') {
        walk(step.then ?? [], inForeach)
        walk(step.else ?? [], inForeach)
      }
      if (step.type === 'foreach') {
        if (!step.do || step.do.length === 0) errors.push(`Step "${step.id}" has nothing to repeat.`)
        else walk(step.do, true)
      }
    }
  }

  if (!Array.isArray(steps) || steps.length === 0) errors.push('The definition needs a non-empty "steps" array.')
  else walk(steps, false)

  return { ok: errors.length === 0, errors }
}

interface WorkflowEditorProps {
  steps: WorkflowStep[]
  eventTypes: string[]
  onChange: (steps: WorkflowStep[]) => void
}

export function WorkflowEditor({ steps, eventTypes, onChange }: WorkflowEditorProps) {
  return (
    <div className="stack" data-testid="workflow-editor">
      <StepList steps={steps} eventTypes={eventTypes} onChange={onChange} depth={0} />
      <button
        type="button"
        className="btn btn--ghost btn--sm"
        onClick={() => onChange([...steps, newStep('create_task')])}
      >
        + Add step
      </button>
    </div>
  )
}

function newStep(type: string): WorkflowStep {
  const id = `${type}_${Math.random().toString(36).slice(2, 7)}`
  switch (type) {
    case 'condition':
      return { id, type, condition: { field: 'payload.status', op: 'eq', value: '' } }
    case 'branch':
      return {
        id,
        type,
        condition: { field: 'payload.status', op: 'eq', value: '' },
        then: [newStep('create_task')],
        else: [],
      }
    case 'find_deals':
      return { id, type, args: { status: 'open', close_date_within_days: 0, limit: 100 } }
    case 'foreach':
      return { id, type, do: [newStep('notify')] }
    case 'notify':
      return { id, type, args: { user_id: '{{payload.owner_user_id}}', title: 'Notification', body: '' } }
    case 'delay':
      return { id, type, args: { minutes: 30 } }
    case 'update_deal':
      return { id, type, args: { deal_id: '{{payload.deal_id}}', stage_id: '' } }
    case 'create_ticket':
      return { id, type, args: { subject: 'Ticket', priority: 'normal' } }
    default:
      return { id, type, args: { title: 'Follow up', priority: 'normal' } }
  }
}

interface StepListProps {
  steps: WorkflowStep[]
  eventTypes: string[]
  onChange: (steps: WorkflowStep[]) => void
  depth: number
}

function StepList({ steps, eventTypes, onChange, depth }: StepListProps) {
  const update = (index: number, next: WorkflowStep) => {
    const copy = steps.slice()
    copy[index] = next
    onChange(copy)
  }
  const remove = (index: number) => onChange(steps.filter((_, i) => i !== index))

  return (
    <ol className="stack" style={{ listStyle: 'none', padding: 0, margin: 0 }} data-depth={depth}>
      {steps.map((step, index) => (
        <li key={step.id}>
          <StepCard
            step={step}
            eventTypes={eventTypes}
            onChange={(next) => update(index, next)}
            onRemove={() => remove(index)}
            depth={depth}
          />
        </li>
      ))}
    </ol>
  )
}

interface StepCardProps {
  step: WorkflowStep
  eventTypes: string[]
  onChange: (step: WorkflowStep) => void
  onRemove: () => void
  depth: number
}

function StepCard({ step, eventTypes, onChange, onRemove, depth }: StepCardProps) {
  const patch = (fields: Partial<WorkflowStep>) => onChange({ ...step, ...fields })
  const patchArgs = (fields: Record<string, unknown>) =>
    onChange({ ...step, args: { ...(step.args ?? {}), ...fields } })

  const isConditional = step.type === 'condition' || step.type === 'branch'

  return (
    <div className="card" style={{ marginBottom: 'var(--space-3)' }} data-testid={`step-${step.id}`}>
      <div className="card__body stack">
        <div className="row" style={{ gap: 'var(--space-2)', alignItems: 'flex-end', flexWrap: 'wrap' }}>
          <label className="field" style={{ flex: 1, minWidth: 140 }}>
            <span className="field__label">Step id</span>
            <input
              className="input mono"
              value={step.id}
              aria-label={`step id ${step.id}`}
              onChange={(e) => patch({ id: e.target.value })}
            />
          </label>
          <label className="field" style={{ flex: 1, minWidth: 160 }}>
            <span className="field__label">Type</span>
            <select
              className="input"
              value={step.type}
              aria-label={`step type ${step.id}`}
              onChange={(e) => onChange(newStep(e.target.value))}
            >
              {WORKFLOW_STEP_TYPES.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </label>
          <button type="button" className="btn btn--ghost btn--sm" onClick={onRemove}>
            Remove
          </button>
        </div>

        {isConditional ? (
          <ConditionEditor
            condition={step.condition ?? { field: '', op: 'eq' }}
            onChange={(condition) => patch({ condition })}
          />
        ) : null}

        {step.type === 'branch' ? (
          <div className="stack">
            <div>
              <Badge tone="accent">then</Badge>
              <StepList
                steps={step.then ?? []}
                eventTypes={eventTypes}
                depth={depth + 1}
                onChange={(then) => patch({ then })}
              />
            </div>
            <div>
              <Badge tone="neutral">else</Badge>
              <StepList
                steps={step.else ?? []}
                eventTypes={eventTypes}
                depth={depth + 1}
                onChange={(elseSteps) => patch({ else: elseSteps })}
              />
            </div>
          </div>
        ) : null}

        {step.type === 'foreach' ? (
          <div>
            <Badge tone="accent">repeat for each item</Badge>
            <StepList
              steps={step.do ?? []}
              eventTypes={eventTypes}
              depth={depth + 1}
              onChange={(doSteps) => patch({ do: doSteps })}
            />
          </div>
        ) : null}

        {!isConditional && step.type !== 'branch' && step.type !== 'foreach' ? (
          <ArgsEditor step={step} eventTypes={eventTypes} onChange={patchArgs} />
        ) : null}
      </div>
    </div>
  )
}

function ConditionEditor({
  condition,
  onChange,
}: {
  condition: WorkflowCondition
  onChange: (condition: WorkflowCondition) => void
}) {
  return (
    <div className="row" style={{ gap: 'var(--space-2)', flexWrap: 'wrap', alignItems: 'flex-end' }}>
      <label className="field" style={{ flex: 2, minWidth: 160 }}>
        <span className="field__label">Field</span>
        <input
          className="input mono"
          value={condition.field}
          aria-label="condition field"
          placeholder="payload.priority"
          onChange={(e) => onChange({ ...condition, field: e.target.value })}
        />
      </label>
      <label className="field" style={{ flex: 1, minWidth: 100 }}>
        <span className="field__label">Operator</span>
        <select
          className="input"
          value={condition.op}
          aria-label="condition operator"
          onChange={(e) => onChange({ ...condition, op: e.target.value })}
        >
          {CONDITION_OPERATORS.map((op) => (
            <option key={op} value={op}>
              {op}
            </option>
          ))}
        </select>
      </label>
      <label className="field" style={{ flex: 2, minWidth: 140 }}>
        <span className="field__label">Value</span>
        <input
          className="input mono"
          value={condition.value === undefined || condition.value === null ? '' : String(condition.value)}
          aria-label="condition value"
          onChange={(e) => onChange({ ...condition, value: e.target.value })}
        />
      </label>
    </div>
  )
}

/** The argument fields a step type uses. Only the fields the engine reads are shown. */
function ArgsEditor({
  step,
  eventTypes,
  onChange,
}: {
  step: WorkflowStep
  eventTypes: string[]
  onChange: (fields: Record<string, unknown>) => void
}) {
  const args = step.args ?? {}
  const str = (key: string) => (typeof args[key] === 'string' ? (args[key] as string) : '')
  const num = (key: string) => (typeof args[key] === 'number' ? String(args[key]) : '')

  const fields: { key: string; label: string; kind: 'text' | 'number' | 'select'; options?: string[] }[] = []
  switch (step.type) {
    case 'create_task':
      fields.push(
        { key: 'title', label: 'Title', kind: 'text' },
        { key: 'description', label: 'Description', kind: 'text' },
        { key: 'priority', label: 'Priority', kind: 'select', options: ['low', 'normal', 'high', 'urgent'] },
        { key: 'due_in_hours', label: 'Due in (hours)', kind: 'number' },
        { key: 'deal_id', label: 'Deal id', kind: 'text' },
      )
      break
    case 'create_ticket':
      fields.push(
        { key: 'subject', label: 'Subject', kind: 'text' },
        { key: 'priority', label: 'Priority', kind: 'select', options: ['low', 'normal', 'high', 'urgent'] },
        { key: 'description', label: 'Description', kind: 'text' },
      )
      break
    case 'notify':
      fields.push(
        { key: 'user_id', label: 'Recipient user id', kind: 'text' },
        { key: 'title', label: 'Title', kind: 'text' },
        { key: 'body', label: 'Body', kind: 'text' },
      )
      break
    case 'update_deal':
      fields.push(
        { key: 'deal_id', label: 'Deal id', kind: 'text' },
        { key: 'stage_id', label: 'New stage id', kind: 'text' },
        { key: 'status', label: 'Status', kind: 'select', options: ['open', 'won', 'lost'] },
      )
      break
    case 'delay':
      fields.push({ key: 'minutes', label: 'Delay (minutes)', kind: 'number' })
      break
    case 'find_deals':
      fields.push(
        { key: 'status', label: 'Status', kind: 'select', options: ['', 'open', 'won', 'lost'] },
        { key: 'close_date_within_days', label: 'Close date within (days)', kind: 'number' },
        { key: 'stale_days', label: 'No activity for (days)', kind: 'number' },
        { key: 'limit', label: 'Limit', kind: 'number' },
      )
      break
    default:
      break
  }

  if (fields.length === 0) return null

  return (
    <div className="row" style={{ gap: 'var(--space-2)', flexWrap: 'wrap', alignItems: 'flex-end' }}>
      {fields.map((f) => (
        <label key={f.key} className="field" style={{ flex: 1, minWidth: 140 }}>
          <span className="field__label">{f.label}</span>
          {f.kind === 'select' ? (
            <select
              className="input"
              aria-label={`${step.id} ${f.key}`}
              value={str(f.key)}
              onChange={(e) => onChange({ [f.key]: e.target.value })}
            >
              {(f.options ?? []).map((o) => (
                <option key={o} value={o}>
                  {o || '—'}
                </option>
              ))}
            </select>
          ) : (
            <input
              className="input mono"
              type={f.kind === 'number' ? 'number' : 'text'}
              aria-label={`${step.id} ${f.key}`}
              value={f.kind === 'number' ? num(f.key) : str(f.key)}
              onChange={(e) =>
                onChange({ [f.key]: f.kind === 'number' ? Number(e.target.value) : e.target.value })
              }
            />
          )}
        </label>
      ))}
      {step.type === 'create_task' && eventTypes.length > 0 ? (
        <span className="field__hint">
          Use <code>{'{{payload.deal_id}}'}</code> to reference the triggering event.
        </span>
      ) : null}
    </div>
  )
}

/** The JSON the editor would send, for the raw view to display and copy. */
export function definitionToJSON(steps: WorkflowStep[]): string {
  return JSON.stringify({ steps }, null, 2)
}
