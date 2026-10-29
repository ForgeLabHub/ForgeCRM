import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ticketsApi } from '../api/crm'
import { errorMessage, fieldErrors } from '../api/client'
import type { Ticket, TicketInput } from '../types/api'
import { useAuth } from '../hooks/useAuth'
import { ResourceList, type Column } from '../components/ResourceList'
import { AssistPanel } from '../components/AssistPanel'
import { AttachmentPanel } from '../components/AttachmentPanel'
import { Alert, Badge, Field, PageHeader, StatusBadge, Timestamp } from '../components/ui'

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
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<Ticket | null>(null)

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
        queryKey={['tickets']}
        fetchPage={(params) => ticketsApi.list(params)}
        columns={columns}
        rowKey={(row) => row.id}
        emptyTitle="No tickets"
        emptyText="Tickets raised by customers or staff appear here."
        searchable
        searchPlaceholder="Search tickets…"
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
        background: 'rgba(16, 24, 40, 0.45)',
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
