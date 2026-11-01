import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ticketsApi } from '../api/crm'
import { errorMessage, fieldErrors } from '../api/client'
import type { ViewFilters } from '../api/views'
import type { Ticket, TicketInput } from '../types/api'
import { useAuth } from '../hooks/useAuth'
import { ResourceList, type Column } from '../components/ResourceList'
import { SavedViewsBar } from '../components/SavedViewsBar'
import { AssistPanel } from '../components/AssistPanel'
import { AttachmentPanel } from '../components/AttachmentPanel'
import { RecordCollabPanel } from '../components/RecordCollabPanel'
import { Alert, Badge, Card, Field, PageHeader, StatusBadge, Timestamp } from '../components/ui'

const STATUSES = ['new', 'open', 'pending', 'resolved', 'closed']
const PRIORITIES = ['low', 'normal', 'high', 'urgent']

function slaState(ticket: Ticket): 'breached' | 'due-soon' | 'ok' | 'none' {
  if (!ticket.sla_due_at || ticket.resolved_at || ticket.closed_at) return 'none'
  const due = new Date(ticket.sla_due_at).getTime()
  const now = Date.now()
  if (due < now) return 'breached'
  if (due - now < 60 * 60 * 1000) return 'due-soon'
  return 'ok'
}

export function TicketsPage() {
  const { can } = useAuth()
  const navigate = useNavigate()
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<Ticket | null>(null)
  const [statusFilter, setStatusFilter] = useState('')
  const [activeViewId, setActiveViewId] = useState<string | undefined>(undefined)

  const columns: Column<Ticket>[] = [
    {
      key: 'subject',
      header: 'Ticket',
      render: (row) => (
        <div>
          <div className="table__primary">{row.subject}</div>
          {row.channel ? <div className="table__secondary">via {row.channel}</div> : null}
        </div>
      ),
    },
    {
      key: 'priority',
      header: 'Priority',
      render: (row) => (
        <Badge tone={row.priority === 'urgent' ? 'danger' : row.priority === 'high' ? 'warning' : 'neutral'}>
          {row.priority}
        </Badge>
      ),
    },
    { key: 'status', header: 'Status', render: (row) => <StatusBadge status={row.status} /> },
    {
      key: 'sla',
      header: 'SLA',
      render: (row) => {
        const state = slaState(row)
        if (state === 'none') return <span className="text-subtle">—</span>
        if (state === 'breached') return <Badge tone="danger">breached</Badge>
        if (state === 'due-soon') return <Badge tone="warning">due soon</Badge>
        return <Timestamp value={row.sla_due_at} />
      },
    },
    {
      key: 'actions',
      header: '',
      width: '90px',
      render: (row) =>
        can('tickets.update') ? (
          <button
            type="button"
            className="btn btn--ghost btn--sm"
            onClick={(event) => {
              event.stopPropagation()
              setEditing(row)
            }}
          >
            Edit
          </button>
        ) : null,
    },
  ]

  return (
    <>
      <PageHeader title="Support" meta="Customer requests, their priority and SLA position." />
      <ResourceList
        title="All tickets"
        queryKey={['tickets', statusFilter]}
        fetchPage={(params) => ticketsApi.list({ ...params, status: statusFilter || undefined })}
        columns={columns}
        rowKey={(row) => row.id}
        onRowClick={(row) => navigate(`/tickets/${row.id}`)}
        emptyTitle="No tickets"
        emptyText="Tickets raised by customers or staff appear here."
        searchable
        searchPlaceholder="Search tickets…"
        filters={
          <>
            <SavedViewsBar
              entityType="tickets"
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
                {STATUSES.map((status) => (
                  <option key={status} value={status}>
                    {status}
                  </option>
                ))}
              </select>
            </label>
          </>
        }
        actions={
          can('tickets.create') ? (
            <button type="button" className="btn btn--primary btn--sm" onClick={() => setCreating(true)}>
              New ticket
            </button>
          ) : null
        }
      />
      <TicketFormDialog
        open={creating || editing !== null}
        ticket={editing ?? undefined}
        onClose={() => {
          setCreating(false)
          setEditing(null)
        }}
        onSaved={() => {
          setCreating(false)
          setEditing(null)
        }}
      />
    </>
  )
}

export function TicketFormDialog({
  open,
  ticket,
  onClose,
  onSaved,
}: {
  open: boolean
  ticket?: Ticket
  onClose: () => void
  onSaved: (ticket: Ticket) => void
}) {
  const queryClient = useQueryClient()
  const editing = Boolean(ticket)
  const [form, setForm] = useState<TicketInput>(() => toInput(ticket))
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)

  const formKey = ticket?.id ?? 'new'
  const [lastKey, setLastKey] = useState(formKey)
  if (lastKey !== formKey) {
    setLastKey(formKey)
    setForm(toInput(ticket))
    setErrors({})
    setFormError(null)
  }

  const mutation = useMutation({
    mutationFn: (input: TicketInput) =>
      editing ? ticketsApi.update(ticket!.id, input) : ticketsApi.create(input),
    onSuccess: (saved) => {
      void queryClient.invalidateQueries({ queryKey: ['tickets'] })
      onSaved(saved)
    },
    onError: (error) => {
      setErrors(fieldErrors(error))
      setFormError(errorMessage(error))
    },
  })

  function update<K extends keyof TicketInput>(key: K, value: TicketInput[K]) {
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
      aria-labelledby="ticket-form-title"
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
      <div
        className="card"
        style={{ maxWidth: 620, margin: '0 auto' }}
        onClick={(event) => event.stopPropagation()}
      >
        <div className="card__header">
          <h2 id="ticket-form-title">{editing ? 'Update ticket' : 'New ticket'}</h2>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="card__body">
            {formError ? (
              <div style={{ marginBottom: 'var(--space-4)' }}>
                <Alert tone="error">{formError}</Alert>
              </div>
            ) : null}
            {editing && ticket ? (
              <div style={{ marginBottom: 'var(--space-4)' }} className="stack">
                <AssistPanel entityType="ticket" entityId={ticket.id} />
                <RecordCollabPanel entityType="ticket" entityId={ticket.id} />
                <AttachmentPanel entityType="ticket" entityId={ticket.id} />
              </div>
            ) : null}
            <div className="form-grid">
              <Field label="Subject" htmlFor="ticket-subject" required error={errors.subject} full>
                <input
                  id="ticket-subject"
                  value={form.subject}
                  onChange={(event) => update('subject', event.target.value)}
                  className={errors.subject ? 'input--invalid' : undefined}
                  required
                />
              </Field>
              <Field label="Status" htmlFor="ticket-status">
                <select
                  id="ticket-status"
                  value={form.status ?? 'new'}
                  onChange={(event) => update('status', event.target.value)}
                >
                  {STATUSES.map((status) => (
                    <option key={status} value={status}>
                      {status}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="Priority" htmlFor="ticket-priority">
                <select
                  id="ticket-priority"
                  value={form.priority ?? 'normal'}
                  onChange={(event) => update('priority', event.target.value)}
                >
                  {PRIORITIES.map((priority) => (
                    <option key={priority} value={priority}>
                      {priority}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="Channel" htmlFor="ticket-channel">
                <select
                  id="ticket-channel"
                  value={form.channel ?? ''}
                  onChange={(event) => update('channel', event.target.value || null)}
                >
                  <option value="">Not set</option>
                  {['email', 'phone', 'web', 'chat', 'telegram'].map((channel) => (
                    <option key={channel} value={channel}>
                      {channel}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="Description" htmlFor="ticket-description" full>
                <textarea
                  id="ticket-description"
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
              {mutation.isPending ? 'Saving…' : editing ? 'Save changes' : 'Create ticket'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

function toInput(ticket?: Ticket): TicketInput {
  if (!ticket) return { subject: '', status: 'new', priority: 'normal' }
  return {
    subject: ticket.subject,
    description: ticket.description ?? null,
    status: ticket.status,
    priority: ticket.priority,
    channel: ticket.channel ?? null,
    account_id: ticket.account_id ?? null,
    contact_id: ticket.contact_id ?? null,
    assignee_user_id: ticket.assignee_user_id ?? null,
    team_id: ticket.team_id ?? null,
  }
}

export function TicketDetailPage() {
  const { id = '' } = useParams()
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)

  const ticket = useQuery({
    queryKey: ['ticket', id],
    queryFn: () => ticketsApi.get(id),
    enabled: Boolean(id),
  })

  const resolve = useMutation({
    mutationFn: () => {
      const current = ticket.data
      if (!current) throw new Error('The ticket is not loaded')
      return ticketsApi.update(id, { ...toInput(current), status: 'resolved' })
    },
    onSuccess: (saved) => {
      queryClient.setQueryData(['ticket', id], saved)
      void queryClient.invalidateQueries({ queryKey: ['tickets'] })
      setActionError(null)
    },
    onError: (error) => setActionError(errorMessage(error)),
  })

  if (ticket.isError) {
    return (
      <Card title="Ticket">
        <Alert tone="error">{errorMessage(ticket.error)}</Alert>
      </Card>
    )
  }

  if (ticket.isLoading || !ticket.data) {
    return <div className="skeleton" style={{ width: '100%', height: 260 }} aria-busy="true" />
  }

  const data = ticket.data
  const closed = data.status === 'resolved' || data.status === 'closed'
  const sla = slaState(data)

  return (
    <>
      <PageHeader
        title={data.subject}
        meta={
          <>
            <StatusBadge status={data.status} /> · Updated <Timestamp value={data.updated_at} />
          </>
        }
        actions={
          <>
            <Link to="/tickets" className="btn btn--ghost btn--sm">
              Back
            </Link>
            {can('tickets.update') && !closed ? (
              <button
                type="button"
                className="btn btn--secondary btn--sm"
                onClick={() => resolve.mutate()}
                disabled={resolve.isPending}
              >
                Resolve
              </button>
            ) : null}
            {can('tickets.update') ? (
              <button type="button" className="btn btn--secondary btn--sm" onClick={() => setEditing(true)}>
                Edit
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
            <dt>Priority</dt>
            <dd>
              <Badge
                tone={
                  data.priority === 'urgent' ? 'danger' : data.priority === 'high' ? 'warning' : 'neutral'
                }
              >
                {data.priority}
              </Badge>
            </dd>
            <dt>Channel</dt>
            <dd>{data.channel ?? <span className="text-subtle">—</span>}</dd>
            <dt>SLA due</dt>
            <dd className={sla === 'breached' ? 'text-danger' : undefined}>
              {sla === 'none' ? (
                <span className="text-subtle">—</span>
              ) : (
                <>
                  <Timestamp value={data.sla_due_at} />
                  {sla === 'breached' ? ' · breached' : sla === 'due-soon' ? ' · due soon' : ''}
                </>
              )}
            </dd>
            <dt>First response</dt>
            <dd>
              <Timestamp value={data.first_response_at} />
            </dd>
            <dt>Resolved</dt>
            <dd>
              <Timestamp value={data.resolved_at} />
            </dd>
            <dt>Closed</dt>
            <dd>
              <Timestamp value={data.closed_at} />
            </dd>
            <dt>Resolution</dt>
            <dd>{data.resolution ?? <span className="text-subtle">—</span>}</dd>
            <dt>Description</dt>
            <dd>{data.description ?? <span className="text-subtle">—</span>}</dd>
          </dl>
        </Card>

        <Card title="Related records">
          <dl className="dl">
            <dt>Account</dt>
            <dd>
              {data.account_id ? (
                <Link to={`/accounts/${data.account_id}`}>View account</Link>
              ) : (
                <span className="text-subtle">Not linked</span>
              )}
            </dd>
            <dt>Contact</dt>
            <dd>
              {data.contact_id ? (
                <Link to={`/contacts/${data.contact_id}`}>View contact</Link>
              ) : (
                <span className="text-subtle">Not linked</span>
              )}
            </dd>
            <dt>External source</dt>
            <dd>
              {data.external_source ? (
                <>
                  {data.external_source}
                  {data.external_id ? ` · ${data.external_id}` : ''}
                </>
              ) : (
                <span className="text-subtle">—</span>
              )}
            </dd>
            <dt>Created</dt>
            <dd>
              <Timestamp value={data.created_at} />
            </dd>
          </dl>
        </Card>
      </div>

      <AssistPanel entityType="ticket" entityId={id} />

      <RecordCollabPanel entityType="ticket" entityId={id} />

      <AttachmentPanel entityType="ticket" entityId={id} />

      <TicketFormDialog
        open={editing}
        ticket={data}
        onClose={() => setEditing(false)}
        onSaved={() => {
          setEditing(false)
          void queryClient.invalidateQueries({ queryKey: ['ticket', id] })
          void queryClient.invalidateQueries({ queryKey: ['tickets'] })
        }}
      />
    </>
  )
}
