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
import { translateEn, useT, type TFunction } from '../i18n'
import { Badge } from './ui'

export interface StepValidation {
  ok: boolean
  errors: string[]
}

/**
 * Validates a definition the way the engine's parser does, for early feedback.
 *
 * It reports its findings through the same translator the page uses, so the
 * message a user reads here is localised; the default is the reference locale
 * for callers that have no translator (a unit test, or any future CLI).
 */
export function validateDefinition(steps: WorkflowStep[], t: TFunction = translateEn): StepValidation {
  const errors: string[] = []
  const ids = new Set<string>()

  const walk = (list: WorkflowStep[], inForeach: boolean) => {
    if (!Array.isArray(list) || list.length === 0) {
      errors.push(t('workflowEditor.errEmptyList'))
      return
    }
    for (const step of list) {
      if (!step.id?.trim()) errors.push(t('workflowEditor.errMissingId'))
      if (step.id && ids.has(step.id)) errors.push(t('workflowEditor.errDuplicateId', { id: step.id }))
      if (step.id) ids.add(step.id)
      if (!WORKFLOW_STEP_TYPES.includes(step.type as (typeof WORKFLOW_STEP_TYPES)[number])) {
        errors.push(t('workflowEditor.errUnknownType', { type: step.type }))
        continue
      }
      if (inForeach && (step.type === 'condition' || step.type === 'delay' || step.type === 'foreach' || step.type === 'branch')) {
        errors.push(t('workflowEditor.errForeachForbidden', { type: step.type }))
      }
      if (step.type === 'condition' || step.type === 'branch') {
        if (!step.condition?.field?.trim()) errors.push(t('workflowEditor.errNeedField', { id: step.id }))
        if (!step.condition?.op) errors.push(t('workflowEditor.errNeedOperator', { id: step.id }))
      }
      if (step.type === 'branch') {
        walk(step.then ?? [], inForeach)
        walk(step.else ?? [], inForeach)
      }
      if (step.type === 'foreach') {
        if (!step.do || step.do.length === 0) errors.push(t('workflowEditor.errNothingToRepeat', { id: step.id }))
        else walk(step.do, true)
      }
    }
  }

  if (!Array.isArray(steps) || steps.length === 0) errors.push(t('workflowEditor.errNeedSteps'))
  else walk(steps, false)

  return { ok: errors.length === 0, errors }
}

interface WorkflowEditorProps {
  steps: WorkflowStep[]
  eventTypes: string[]
  onChange: (steps: WorkflowStep[]) => void
}

export function WorkflowEditor({ steps, eventTypes, onChange }: WorkflowEditorProps) {
  const t = useT()
  return (
    <div className="stack" data-testid="workflow-editor">
      <StepList steps={steps} eventTypes={eventTypes} onChange={onChange} depth={0} />
      <button
        type="button"
        className="btn btn--ghost btn--sm"
        onClick={() => onChange([...steps, newStep('create_task')])}
      >
        <span aria-hidden="true">+</span> {t('workflowEditor.addStep')}
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
  const t = useT()
  const patch = (fields: Partial<WorkflowStep>) => onChange({ ...step, ...fields })
  const patchArgs = (fields: Record<string, unknown>) =>
    onChange({ ...step, args: { ...(step.args ?? {}), ...fields } })

  const isConditional = step.type === 'condition' || step.type === 'branch'

  return (
    <div className="card" style={{ marginBottom: 'var(--space-3)' }} data-testid={`step-${step.id}`}>
      <div className="card__body stack">
        <div className="row" style={{ gap: 'var(--space-2)', alignItems: 'flex-end', flexWrap: 'wrap' }}>
          <label className="field" style={{ flex: 1, minWidth: 140 }}>
            <span className="field__label">{t('workflowEditor.stepId')}</span>
            <input
              className="input mono"
              value={step.id}
              aria-label={t('workflowEditor.stepIdAria', { id: step.id })}
              onChange={(e) => patch({ id: e.target.value })}
            />
          </label>
          <label className="field" style={{ flex: 1, minWidth: 160 }}>
            <span className="field__label">{t('workflowEditor.type')}</span>
            <select
              className="input"
              value={step.type}
              aria-label={t('workflowEditor.stepTypeAria', { id: step.id })}
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
            {t('workflowEditor.remove')}
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
              <Badge tone="accent">{t('workflowEditor.then')}</Badge>
              <StepList
                steps={step.then ?? []}
                eventTypes={eventTypes}
                depth={depth + 1}
                onChange={(then) => patch({ then })}
              />
            </div>
            <div>
              <Badge tone="neutral">{t('workflowEditor.else')}</Badge>
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
            <Badge tone="accent">{t('workflowEditor.foreach')}</Badge>
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
  const t = useT()
  return (
    <div className="row" style={{ gap: 'var(--space-2)', flexWrap: 'wrap', alignItems: 'flex-end' }}>
      <label className="field" style={{ flex: 2, minWidth: 160 }}>
        <span className="field__label">{t('workflowEditor.field')}</span>
        <input
          className="input mono"
          value={condition.field}
          aria-label={t('workflowEditor.conditionFieldAria')}
          placeholder={t('workflowEditor.conditionFieldPlaceholder')}
          onChange={(e) => onChange({ ...condition, field: e.target.value })}
        />
      </label>
      <label className="field" style={{ flex: 1, minWidth: 100 }}>
        <span className="field__label">{t('workflowEditor.operator')}</span>
        <select
          className="input"
          value={condition.op}
          aria-label={t('workflowEditor.conditionOperatorAria')}
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
        <span className="field__label">{t('workflowEditor.value')}</span>
        <input
          className="input mono"
          value={condition.value === undefined || condition.value === null ? '' : String(condition.value)}
          aria-label={t('workflowEditor.conditionValueAria')}
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
  const t = useT()
  const args = step.args ?? {}
  const str = (key: string) => (typeof args[key] === 'string' ? (args[key] as string) : '')
  const num = (key: string) => (typeof args[key] === 'number' ? String(args[key]) : '')

  const fields: { key: string; label: string; kind: 'text' | 'number' | 'select'; options?: string[] }[] = []
  switch (step.type) {
    case 'create_task':
      fields.push(
        { key: 'title', label: t('workflowEditor.argTitle'), kind: 'text' },
        { key: 'description', label: t('workflowEditor.argDescription'), kind: 'text' },
        { key: 'priority', label: t('workflowEditor.argPriority'), kind: 'select', options: ['low', 'normal', 'high', 'urgent'] },
        { key: 'due_in_hours', label: t('workflowEditor.argDueInHours'), kind: 'number' },
        { key: 'deal_id', label: t('workflowEditor.argDealId'), kind: 'text' },
      )
      break
    case 'create_ticket':
      fields.push(
        { key: 'subject', label: t('workflowEditor.argSubject'), kind: 'text' },
        { key: 'priority', label: t('workflowEditor.argPriority'), kind: 'select', options: ['low', 'normal', 'high', 'urgent'] },
        { key: 'description', label: t('workflowEditor.argDescription'), kind: 'text' },
      )
      break
    case 'notify':
      fields.push(
        { key: 'user_id', label: t('workflowEditor.argRecipientUserId'), kind: 'text' },
        { key: 'title', label: t('workflowEditor.argTitle'), kind: 'text' },
        { key: 'body', label: t('workflowEditor.argBody'), kind: 'text' },
      )
      break
    case 'update_deal':
      fields.push(
        { key: 'deal_id', label: t('workflowEditor.argDealId'), kind: 'text' },
        { key: 'stage_id', label: t('workflowEditor.argNewStageId'), kind: 'text' },
        { key: 'status', label: t('workflowEditor.argStatus'), kind: 'select', options: ['open', 'won', 'lost'] },
      )
      break
    case 'delay':
      fields.push({ key: 'minutes', label: t('workflowEditor.argDelayMinutes'), kind: 'number' })
      break
    case 'find_deals':
      fields.push(
        { key: 'status', label: t('workflowEditor.argStatus'), kind: 'select', options: ['', 'open', 'won', 'lost'] },
        { key: 'close_date_within_days', label: t('workflowEditor.argCloseWithinDays'), kind: 'number' },
        { key: 'stale_days', label: t('workflowEditor.argStaleDays'), kind: 'number' },
        { key: 'limit', label: t('workflowEditor.argLimit'), kind: 'number' },
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
          {t('workflowEditor.argEventHint', { path: '{{payload.deal_id}}' })}
        </span>
      ) : null}
    </div>
  )
}

/** The JSON the editor would send, for the raw view to display and copy. */
export function definitionToJSON(steps: WorkflowStep[]): string {
  return JSON.stringify({ steps }, null, 2)
}
