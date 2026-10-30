import { useMemo, useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { accountsApi, dealsApi, pipelinesApi } from '../api/crm'
import { errorMessage, fieldErrors } from '../api/client'
import type { ViewFilters } from '../api/views'
import type { Deal, DealInput, Pipeline } from '../types/api'
import { useAuth } from '../hooks/useAuth'
import { ResourceList, type Column } from '../components/ResourceList'
import { SavedViewsBar } from '../components/SavedViewsBar'
import { AssistPanel } from '../components/AssistPanel'
import { AttachmentPanel } from '../components/AttachmentPanel'
import { RecordCollabPanel } from '../components/RecordCollabPanel'
import { CustomFieldsPanel } from '../components/CustomFieldsPanel'
import { BulkArchiveButton } from '../components/BulkArchiveButton'
import {
  Alert,
  Badge,
  Card,
  ConfirmDialog,
  Field,
  PageHeader,
  StatusBadge,
  Timestamp,
  formatMoney,
} from '../components/ui'

function amountCell(deal: Deal) {
  return formatMoney(deal.amount, deal.currency)
}

const columns: Column<Deal>[] = [
  {
    key: 'title',
    header: 'Deal',
    render: (row) => (
      <div>
        <div className="table__primary">{row.title}</div>
        {row.expected_close_date ? (
          <div className="table__secondary">Close {row.expected_close_date}</div>
        ) : null}
      </div>
    ),
  },
  {
    key: 'amount',
    header: 'Amount',
    numeric: true,
    render: (row) => amountCell(row),
  },
  {
    key: 'discount',
    header: 'Discount',
    numeric: true,
    render: (row) =>
      row.discount_percent > 0 ? (
        <span className="row" style={{ justifyContent: 'flex-end' }}>
          {row.discount_percent}%
          {row.discount_approved_at ? <Badge tone="success">approved</Badge> : <Badge tone="warning">pending</Badge>}
        </span>
      ) : (
        <span className="text-subtle">—</span>
      ),
  },
  {
    key: 'status',
    header: 'Status',
    render: (row) => <StatusBadge status={row.status} />,
  },
  {
    key: 'updated',
    header: 'Updated',
    render: (row) => <Timestamp value={row.updated_at} />,
  },
]

export function DealsPage() {
  const navigate = useNavigate()
  const { can } = useAuth()
  const [creating, setCreating] = useState(false)
  const [view, setView] = useState<'table' | 'board'>('table')
  const [statusFilter, setStatusFilter] = useState('')
  const [activeViewId, setActiveViewId] = useState<string | undefined>(undefined)

  return (
    <>
      <PageHeader
        title="Deals"
        meta="Every opportunity in the pipeline, with its stage and value."
        actions={
          <div className="row">
            <button
              type="button"
              className={view === 'table' ? 'btn btn--secondary btn--sm' : 'btn btn--ghost btn--sm'}
              onClick={() => setView('table')}
              aria-pressed={view === 'table'}
            >
              Table
            </button>
            <button
              type="button"
              className={view === 'board' ? 'btn btn--secondary btn--sm' : 'btn btn--ghost btn--sm'}
              onClick={() => setView('board')}
              aria-pressed={view === 'board'}
            >
              Board
            </button>
            {can('deals.create') ? (
              <button type="button" className="btn btn--primary btn--sm" onClick={() => setCreating(true)}>
                New deal
              </button>
            ) : null}
          </div>
        }
      />

      {view === 'table' ? (
        <ResourceList
          title="All deals"
          queryKey={['deals', statusFilter]}
          fetchPage={(params) => dealsApi.list({ ...params, status: statusFilter || undefined })}
          columns={columns}
          rowKey={(row) => row.id}
          onRowClick={(row) => navigate(`/deals/${row.id}`)}
          emptyTitle="No deals yet"
          emptyText="Create a deal to track an opportunity through your pipeline."
          searchable
          searchPlaceholder="Search deals…"
          filters={
            <>
              <SavedViewsBar
                entityType="deals"
                current={statusFilter ? { status: statusFilter } : {}}
                onApply={(filters: ViewFilters) => setStatusFilter(filters.status ?? '')}
                activeId={activeViewId}
                onActiveChange={setActiveViewId}
              />
              <label className="field" style={{ maxWidth: 180 }}>
                <span className="field__label">Status</span>
                <select
                  value={statusFilter}
                  onChange={(event) => setStatusFilter(event.target.value)}
                >
                  <option value="">All statuses</option>
                  {['open', 'won', 'lost'].map((status) => (
                    <option key={status} value={status}>
                      {status}
                    </option>
                  ))}
                </select>
              </label>
            </>
          }
          bulkActions={
            can('deals.archive')
              ? (selected, clear) => (
                  <BulkArchiveButton
                    ids={selected}
                    entityLabel="deals"
                    archive={dealsApi.bulkArchive}
                    onDone={clear}
                  />
                )
              : undefined
          }
        />
      ) : (
        <PipelineBoard onOpen={(deal) => navigate(`/deals/${deal.id}`)} />
      )}

      <DealFormDialog
        open={creating}
        onClose={() => setCreating(false)}
        onSaved={(deal) => {
          setCreating(false)
          navigate(`/deals/${deal.id}`)
        }}
      />
    </>
  )
}

/** Kanban view. Moving a card calls the same stage endpoint the table would. */
function PipelineBoard({ onOpen }: { onOpen: (deal: Deal) => void }) {
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [pipelineId, setPipelineId] = useState<string>('')
  const [moveError, setMoveError] = useState<string | null>(null)

  const pipelines = useQuery({ queryKey: ['pipelines'], queryFn: () => pipelinesApi.list() })

  const active: Pipeline | undefined = useMemo(() => {
    const items = pipelines.data?.items ?? []
    if (pipelineId) return items.find((item) => item.id === pipelineId)
    return items.find((item) => item.is_default) ?? items[0]
  }, [pipelines.data, pipelineId])

  const deals = useQuery({
    queryKey: ['deals', 'board', active?.id],
    queryFn: () => dealsApi.list({ limit: 200, pipeline_id: active!.id }),
    enabled: Boolean(active?.id),
  })

  const move = useMutation({
    mutationFn: ({ dealId, stageId }: { dealId: string; stageId: string }) =>
      dealsApi.moveStage(dealId, stageId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['deals'] })
      setMoveError(null)
    },
    onError: (error) => setMoveError(errorMessage(error)),
  })

  if (pipelines.isLoading) {
    return <div className="skeleton" style={{ width: '100%', height: 300 }} aria-busy="true" />
  }

  if (pipelines.isError) {
    return (
      <Card title="Pipeline">
        <Alert tone="error">{errorMessage(pipelines.error)}</Alert>
      </Card>
    )
  }

  if (!active) {
    return (
      <Card title="Pipeline">
        <p className="text-muted">
          No pipeline is configured yet. Create one to see deals on a board.
        </p>
      </Card>
    )
  }

  const stages = active.stages ?? []
  const items = deals.data?.items ?? []

  return (
    <div className="stack">
      <div className="row row--wrap">
        <label className="field" style={{ maxWidth: 280 }}>
          <span className="field__label">Pipeline</span>
          <select value={active.id} onChange={(event) => setPipelineId(event.target.value)}>
            {(pipelines.data?.items ?? []).map((pipeline) => (
              <option key={pipeline.id} value={pipeline.id}>
                {pipeline.name}
              </option>
            ))}
          </select>
        </label>
        <div className="spacer" />
        {deals.data ? (
          <Badge tone="neutral">{deals.data.total} deals</Badge>
        ) : null}
      </div>

      {moveError ? <Alert tone="error">{moveError}</Alert> : null}

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: `repeat(${Math.max(stages.length, 1)}, minmax(220px, 1fr))`,
          gap: 'var(--space-3)',
          overflowX: 'auto',
        }}
      >
        {stages.map((stage) => {
          const stageDeals = items.filter((deal) => deal.stage_id === stage.id)
          const total = stageDeals.reduce((sum, deal) => sum + deal.amount, 0)
          return (
            <div key={stage.id} className="card" style={{ minWidth: 220 }}>
              <div className="card__header" style={{ padding: 'var(--space-3)' }}>
                <div>
                  <div style={{ fontWeight: 600, fontSize: 13.5 }}>{stage.name}</div>
                  <div className="text-subtle" style={{ fontSize: 12 }}>
                    {stageDeals.length} · {formatMoney(total, active.currency)}
                  </div>
                </div>
                <Badge tone="neutral">{stage.probability}%</Badge>
              </div>
              <div className="card__body stack stack--sm" style={{ padding: 'var(--space-3)' }}>
                {stageDeals.length === 0 ? (
                  <p className="text-subtle text-sm">Empty</p>
                ) : (
                  stageDeals.map((deal) => (
                    <div
                      key={deal.id}
                      className="card"
                      style={{ padding: 'var(--space-3)', cursor: 'pointer' }}
                    >
                      <button
                        type="button"
                        onClick={() => onOpen(deal)}
                        style={{
                          all: 'unset',
                          display: 'block',
                          cursor: 'pointer',
                          fontWeight: 500,
                        }}
                      >
                        {deal.title}
                      </button>
                      <div className="row row--between" style={{ marginTop: 'var(--space-2)' }}>
                        <span className="text-sm text-muted">
                          {formatMoney(deal.amount, deal.currency)}
                        </span>
                        {can('deals.change_stage') && stages.length > 1 ? (
                          <select
                            aria-label={`Move ${deal.title} to another stage`}
                            value={deal.stage_id}
                            style={{ width: 'auto', fontSize: 12, padding: '2px 4px' }}
                            onClick={(event) => event.stopPropagation()}
                            onChange={(event) => {
                              event.stopPropagation()
                              move.mutate({ dealId: deal.id, stageId: event.target.value })
                            }}
                          >
                            {stages.map((option) => (
                              <option key={option.id} value={option.id}>
                                {option.name}
                              </option>
                            ))}
                          </select>
                        ) : null}
                      </div>
                    </div>
                  ))
                )}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

export function DealFormDialog({
  open,
  deal,
  onClose,
  onSaved,
}: {
  open: boolean
  deal?: Deal
  onClose: () => void
  onSaved: (deal: Deal) => void
}) {
  const queryClient = useQueryClient()
  const editing = Boolean(deal)
  const [form, setForm] = useState<DealInput>(() => toInput(deal))
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)

  const formKey = deal?.id ?? 'new'
  const [lastKey, setLastKey] = useState(formKey)

  const pipelines = useQuery({
    queryKey: ['pipelines'],
    queryFn: () => pipelinesApi.list(),
    enabled: open,
  })

  const accounts = useQuery({
    queryKey: ['accounts', 'for-select'],
    queryFn: () => accountsApi.list({ limit: 200 }),
    enabled: open,
  })

  const pipelineList = pipelines.data?.items ?? []
  const selectedPipeline = pipelineList.find((pipeline) => pipeline.id === form.pipeline_id)
  const stages = selectedPipeline?.stages ?? []

  // Default to the first pipeline and its first stage when creating, so the
  // form is submittable without the user choosing values that have one obvious
  // answer.
  if (open && !editing && lastKey !== formKey && pipelineList.length > 0) {
    setLastKey(formKey)
    const first = pipelineList.find((p) => p.is_default) ?? pipelineList[0]
    setForm({
      ...toInput(deal),
      pipeline_id: first.id,
      stage_id: first.stages?.[0]?.id ?? '',
      currency: first.currency || 'USD',
    })
  } else if (lastKey !== formKey) {
    setLastKey(formKey)
    setForm(toInput(deal))
  }

  const mutation = useMutation({
    mutationFn: (input: DealInput) =>
      editing ? dealsApi.update(deal!.id, input) : dealsApi.create(input),
    onSuccess: (saved) => {
      void queryClient.invalidateQueries({ queryKey: ['deals'] })
      onSaved(saved)
    },
    onError: (error) => {
      setErrors(fieldErrors(error))
      setFormError(errorMessage(error))
    },
  })

  function update<K extends keyof DealInput>(key: K, value: DealInput[K]) {
    setForm((previous) => ({ ...previous, [key]: value }))
  }

  function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setFormError(null)
    setErrors({})
    mutation.mutate(form)
  }

  if (!open) return null

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="deal-form-title"
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
        style={{ maxWidth: 680, margin: '0 auto' }}
        onClick={(event) => event.stopPropagation()}
      >
        <div className="card__header">
          <h2 id="deal-form-title">{editing ? 'Edit deal' : 'New deal'}</h2>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="card__body">
            {formError ? (
              <div style={{ marginBottom: 'var(--space-4)' }}>
                <Alert tone="error">{formError}</Alert>
              </div>
            ) : null}
            <div className="form-grid">
              <Field label="Title" htmlFor="deal-title" required error={errors.title} full>
                <input
                  id="deal-title"
                  value={form.title}
                  onChange={(event) => update('title', event.target.value)}
                  className={errors.title ? 'input--invalid' : undefined}
                  required
                />
              </Field>
              <Field label="Pipeline" htmlFor="deal-pipeline" required error={errors.pipeline_id}>
                <select
                  id="deal-pipeline"
                  value={form.pipeline_id}
                  onChange={(event) => {
                    const next = pipelineList.find((p) => p.id === event.target.value)
                    setForm((previous) => ({
                      ...previous,
                      pipeline_id: event.target.value,
                      stage_id: next?.stages?.[0]?.id ?? '',
                    }))
                  }}
                  required
                >
                  <option value="">Select a pipeline</option>
                  {pipelineList.map((pipeline) => (
                    <option key={pipeline.id} value={pipeline.id}>
                      {pipeline.name}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="Stage" htmlFor="deal-stage" required error={errors.stage_id}>
                <select
                  id="deal-stage"
                  value={form.stage_id}
                  onChange={(event) => update('stage_id', event.target.value)}
                  disabled={stages.length === 0}
                  required
                >
                  <option value="">Select a stage</option>
                  {stages.map((stage) => (
                    <option key={stage.id} value={stage.id}>
                      {stage.name}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="Amount" htmlFor="deal-amount" error={errors.amount}>
                <input
                  id="deal-amount"
                  type="number"
                  min={0}
                  step="0.01"
                  value={form.amount ?? 0}
                  onChange={(event) => update('amount', Number(event.target.value))}
                />
              </Field>
              <Field label="Currency" htmlFor="deal-currency">
                <input
                  id="deal-currency"
                  maxLength={3}
                  value={form.currency ?? 'USD'}
                  onChange={(event) => update('currency', event.target.value.toUpperCase())}
                />
              </Field>
              <Field
                label="Discount %"
                htmlFor="deal-discount"
                error={errors.discount_percent}
                hint="Above the approval threshold this needs a manager's approval."
              >
                <input
                  id="deal-discount"
                  type="number"
                  min={0}
                  max={100}
                  step="0.5"
                  value={form.discount_percent ?? 0}
                  onChange={(event) => update('discount_percent', Number(event.target.value))}
                />
              </Field>
              <Field label="Expected close" htmlFor="deal-close">
                <input
                  id="deal-close"
                  type="date"
                  value={form.expected_close_date ?? ''}
                  onChange={(event) => update('expected_close_date', event.target.value || null)}
                />
              </Field>
              <Field label="Account" htmlFor="deal-account">
                <select
                  id="deal-account"
                  value={form.account_id ?? ''}
                  onChange={(event) => update('account_id', event.target.value || null)}
                >
                  <option value="">No account</option>
                  {(accounts.data?.items ?? []).map((account) => (
                    <option key={account.id} value={account.id}>
                      {account.name}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="Description" htmlFor="deal-description" full>
                <textarea
                  id="deal-description"
                  value={form.description ?? ''}
                  onChange={(event) => update('description', event.target.value || null)}
                />
              </Field>
            </div>
          </div>
          <div className="form-actions">
            <button type="button" className="btn btn--secondary" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="btn btn--primary" disabled={mutation.isPending}>
              {mutation.isPending ? 'Saving…' : editing ? 'Save changes' : 'Create deal'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

function toInput(deal?: Deal): DealInput {
  if (!deal) return { pipeline_id: '', stage_id: '', title: '', amount: 0, currency: 'USD', discount_percent: 0 }
  return {
    pipeline_id: deal.pipeline_id,
    stage_id: deal.stage_id,
    account_id: deal.account_id ?? null,
    primary_contact_id: deal.primary_contact_id ?? null,
    title: deal.title,
    description: deal.description ?? null,
    amount: deal.amount,
    currency: deal.currency,
    discount_percent: deal.discount_percent,
    expected_close_date: deal.expected_close_date ?? null,
    owner_user_id: deal.owner_user_id ?? null,
    team_id: deal.team_id ?? null,
    custom_fields: deal.custom_fields,
  }
}

export function DealDetailPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [archiving, setArchiving] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)

  const deal = useQuery({
    queryKey: ['deal', id],
    queryFn: () => dealsApi.get(id),
    enabled: Boolean(id),
  })

  const history = useQuery({
    queryKey: ['deal', id, 'history'],
    queryFn: () => dealsApi.history(id),
    enabled: Boolean(id),
  })

  const archive = useMutation({
    mutationFn: () => dealsApi.archive(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['deals'] })
      navigate('/deals')
    },
    onError: (error) => {
      setArchiving(false)
      setActionError(errorMessage(error))
    },
  })

  const saveCustomFields = useMutation({
    mutationFn: (next: Deal['custom_fields']) =>
      dealsApi.update(id, { ...toInput(data), custom_fields: next }),
    onSuccess: (saved) => {
      queryClient.setQueryData(['deal', id], saved)
      void queryClient.invalidateQueries({ queryKey: ['deals'] })
    },
  })

  if (deal.isError) {
    return (
      <Card title="Deal">
        <Alert tone="error">{errorMessage(deal.error)}</Alert>
      </Card>
    )
  }

  if (deal.isLoading || !deal.data) {
    return <div className="skeleton" style={{ width: '100%', height: 260 }} aria-busy="true" />
  }

  const data = deal.data

  return (
    <>
      <PageHeader
        title={data.title}
        meta={
          <>
            {formatMoney(data.amount, data.currency)} · Updated{' '}
            <Timestamp value={data.updated_at} />
          </>
        }
        actions={
          <>
            <Link to="/deals" className="btn btn--ghost btn--sm">
              Back
            </Link>
            {can('deals.update') ? (
              <button type="button" className="btn btn--secondary btn--sm" onClick={() => setEditing(true)}>
                Edit
              </button>
            ) : null}
            {can('deals.archive') ? (
              <button type="button" className="btn btn--danger btn--sm" onClick={() => setArchiving(true)}>
                Archive
              </button>
            ) : null}
          </>
        }
      />

      {actionError ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="error">{actionError}</Alert>
        </div>
      ) : null}

      <div className="grid grid--split">
        <Card title="Details">
          <dl className="dl">
            <dt>Status</dt>
            <dd>
              <StatusBadge status={data.status} />
            </dd>
            <dt>Amount</dt>
            <dd className="numeric">{formatMoney(data.amount, data.currency)}</dd>
            <dt>Discount</dt>
            <dd>
              {data.discount_percent}%{' '}
              {data.discount_approved_at ? (
                <Badge tone="success">approved</Badge>
              ) : data.discount_percent > 0 ? (
                <Badge tone="warning">awaiting approval</Badge>
              ) : null}
            </dd>
            <dt>Expected close</dt>
            <dd>{data.expected_close_date ?? <span className="text-subtle">—</span>}</dd>
            <dt>Closed</dt>
            <dd>
              <Timestamp value={data.closed_at} />
            </dd>
            {data.lost_reason ? (
              <>
                <dt>Lost reason</dt>
                <dd>{data.lost_reason}</dd>
              </>
            ) : null}
            <dt>Account</dt>
            <dd>
              {data.account_id ? (
                <Link to={`/accounts/${data.account_id}`}>View account</Link>
              ) : (
                <span className="text-subtle">Not linked</span>
              )}
            </dd>
            <dt>Description</dt>
            <dd>{data.description ?? <span className="text-subtle">—</span>}</dd>
          </dl>
        </Card>

        <Card title="Stage history">
          {history.data?.items.length ? (
            <div className="stack stack--sm">
              {history.data.items.map((entry) => (
                <div key={entry.id} className="row row--between">
                  <span className="text-sm">Stage changed</span>
                  <Timestamp value={entry.changed_at} />
                </div>
              ))}
            </div>
          ) : (
            <p className="text-muted text-sm">The deal has not changed stage yet.</p>
          )}
        </Card>
      </div>

      <AssistPanel entityType="deal" entityId={id} />

      <RecordCollabPanel entityType="deal" entityId={id} />

      <CustomFieldsPanel
        entityType="deal"
        values={data.custom_fields ?? {}}
        canEdit={can('deals.update')}
        saving={saveCustomFields.isPending}
        onSave={(next) => saveCustomFields.mutateAsync(next)}
      />

      <AttachmentPanel entityType="deal" entityId={id} />

      <DealFormDialog
        open={editing}
        deal={data}
        onClose={() => setEditing(false)}
        onSaved={() => {
          setEditing(false)
          void queryClient.invalidateQueries({ queryKey: ['deal', id] })
        }}
      />

      <ConfirmDialog
        open={archiving}
        title="Archive this deal?"
        text="The deal leaves the pipeline but its history is kept."
        confirmLabel="Archive"
        danger
        busy={archive.isPending}
        onConfirm={() => archive.mutate()}
        onCancel={() => setArchiving(false)}
      />
    </>
  )
}
