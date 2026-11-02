import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ticketsApi } from '../api/crm'
import { errorMessage, fieldErrors } from '../api/client'
import type { ViewFilters } from '../api/views'
import type { Ticket, TicketInput } from '../types/api'
import { useAuth } from '../hooks/useAuth'
import { useCreateParam } from '../hooks/useCreateParam'
import { ResourceList, type Column } from '../components/ResourceList'
import { SavedViewsBar } from '../components/SavedViewsBar'
import { AssistPanel } from '../components/AssistPanel'
import { AttachmentPanel } from '../components/AttachmentPanel'
import { RecordCollabPanel } from '../components/RecordCollabPanel'
import {
  Alert,
  Badge,
  Card,
  Field,
  Modal,
  PageHeader,
  StatusBadge,
  Timestamp,
} from '../components/ui'
import { useT, type TFunction } from '../i18n'

const STATUSES = ['new', 'open', 'pending', 'resolved', 'closed']
const PRIORITIES = ['low', 'normal', 'high', 'urgent']

function priorityLabel(t: TFunction, priority: string): string {
  return t(`priority.${priority}` as Parameters<TFunction>[0])
}

function slaState(ticket: Ticket): 'breached' | 'due-soon' | 'ok' | 'none' {
  if (!ticket.resolution_due_at || ticket.resolved_at) return 'none'
  if (ticket.sla_breached) return 'breached'
  const due = new Date(ticket.resolution_due_at).getTime()
  const now = Date.now()
  if (due - now < 60 * 60 * 1000) return 'due-soon'
  return 'ok'
}

export function TicketsPage() {
  const { can } = useAuth()
  const navigate = useNavigate()
  const t = useT()
  const [creating, setCreating] = useCreateParam(can('tickets.create'))
  const [editing, setEditing] = useState<Ticket | null>(null)
  const [statusFilter, setStatusFilter] = useState('')
  const [activeViewId, setActiveViewId] = useState<string | undefined>(undefined)

  const columns: Column<Ticket>[] = [
    {
      key: 'subject',
      header: t('tickets.columnTicket'),
      render: (row) => (
        <div>
          <div className="table__primary">{row.subject}</div>
          {row.channel ? (
            <div className="table__secondary">{t('tickets.viaChannel', { channel: row.channel })}</div>
          ) : null}
        </div>
      ),
    },
    {
      key: 'priority',
      header: t('tickets.columnPriority'),
      render: (row) => (
        <Badge tone={row.priority === 'urgent' ? 'danger' : row.priority === 'high' ? 'warning' : 'neutral'}>
          {priorityLabel(t, row.priority)}
        </Badge>
      ),
    },
    {
      key: 'status',
      header: t('tickets.columnStatus'),
      render: (row) => <StatusBadge status={row.status} />,
    },
    {
      key: 'sla',
      header: t('tickets.columnSla'),
      render: (row) => {
        const state = slaState(row)
        if (state === 'none') return <span className="text-subtle">—</span>
        if (state === 'breached') return <Badge tone="danger">{t('tickets.slaBreached')}</Badge>
        if (state === 'due-soon') return <Badge tone="warning">{t('tickets.slaDueSoon')}</Badge>
        return <Timestamp value={row.resolution_due_at} />
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
            {t('common.edit')}
          </button>
        ) : null,
    },
  ]

  return (
    <>
      <PageHeader title={t('tickets.title')} meta={t('tickets.meta')} />
      <ResourceList
        title={t('tickets.all')}
        queryKey={['tickets', statusFilter]}
        fetchPage={(params) => ticketsApi.list({ ...params, status: statusFilter || undefined })}
        columns={columns}
        rowKey={(row) => row.id}
        onRowClick={(row) => navigate(`/tickets/${row.id}`)}
        emptyTitle={t('tickets.emptyTitle')}
        emptyText={t('tickets.emptyText')}
        searchable
        searchPlaceholder={t('tickets.searchPlaceholder')}
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
              <span className="field__label">{t('tickets.statusFilter')}</span>
              <select
                value={statusFilter}
                onChange={(event) => setStatusFilter(event.target.value)}
              >
                <option value="">{t('common.allStatuses')}</option>
                {STATUSES.map((status) => (
                  <option key={status} value={status}>
                    {t(`status.${status}` as Parameters<TFunction>[0])}
                  </option>
                ))}
              </select>
            </label>
          </>
        }
        actions={
          can('tickets.create') ? (
            <button type="button" className="btn btn--primary btn--sm" onClick={() => setCreating(true)}>
              {t('tickets.newTicket')}
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
  const t = useT()
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
    <Modal
      open
      onClose={onClose}
      title={editing ? t('tickets.editTicket') : t('tickets.newTicket')}
      footer={<>
        <button type="button" className="btn btn--secondary" onClick={onClose}>
          {t('common.cancel')}
        </button>
        <button type="submit" className="btn btn--primary" disabled={mutation.isPending}>
          {mutation.isPending
            ? t('common.saving')
            : editing
              ? t('common.saveChanges')
              : t('tickets.createTicket')}
        </button>
      </>}
      formProps={{ onSubmit: handleSubmit }}
    >
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
        <Field label={t('tickets.subjectField')} htmlFor="ticket-subject" required error={errors.subject} full>
          <input
            id="ticket-subject"
            value={form.subject}
            onChange={(event) => update('subject', event.target.value)}
            className={errors.subject ? 'input--invalid' : undefined}
            required
          />
        </Field>
        <Field label={t('tickets.statusField')} htmlFor="ticket-status">
          <select
            id="ticket-status"
            value={form.status ?? 'new'}
            onChange={(event) => update('status', event.target.value)}
          >
            {STATUSES.map((status) => (
              <option key={status} value={status}>
                {t(`status.${status}` as Parameters<TFunction>[0])}
              </option>
            ))}
          </select>
        </Field>
        <Field label={t('tickets.priorityField')} htmlFor="ticket-priority">
          <select
            id="ticket-priority"
            value={form.priority ?? 'normal'}
            onChange={(event) => update('priority', event.target.value)}
          >
            {PRIORITIES.map((priority) => (
              <option key={priority} value={priority}>
                {priorityLabel(t, priority)}
              </option>
            ))}
          </select>
        </Field>
        <Field label={t('tickets.channelField')} htmlFor="ticket-channel">
          <select
            id="ticket-channel"
            value={form.channel ?? ''}
            onChange={(event) => update('channel', event.target.value || null)}
          >
            <option value="">{t('tickets.channelNotSet')}</option>
            {['email', 'phone', 'web', 'chat', 'telegram'].map((channel) => (
              <option key={channel} value={channel}>
                {t(`channel.${channel}` as Parameters<TFunction>[0])}
              </option>
            ))}
          </select>
        </Field>
        <Field label={t('tickets.descriptionField')} htmlFor="ticket-description" full>
          <textarea
            id="ticket-description"
            value={form.description ?? ''}
            onChange={(event) => update('description', event.target.value || null)}
          />
        </Field>
      </div>
    </Modal>
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
  const t = useT()
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
      if (!current) throw new Error(t('tickets.notLoaded'))
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
      <Card title={t('tickets.detailTitle')}>
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
        breadcrumb={[{ label: t('nav.support'), to: '/tickets' }]}
        meta={
          <>
            <StatusBadge status={data.status} /> · {t('common.updated')}{' '}
            <Timestamp value={data.updated_at} />
          </>
        }
        actions={
          <>
            {can('tickets.update') && !closed ? (
              <button
                type="button"
                className="btn btn--secondary btn--sm"
                onClick={() => resolve.mutate()}
                disabled={resolve.isPending}
              >
                {t('tickets.resolve')}
              </button>
            ) : null}
            {can('tickets.update') ? (
              <button type="button" className="btn btn--secondary btn--sm" onClick={() => setEditing(true)}>
                {t('common.edit')}
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
        <Card title={t('tickets.details')}>
          <dl className="dl">
            <dt>{t('tickets.statusField')}</dt>
            <dd>
              <StatusBadge status={data.status} />
            </dd>
            <dt>{t('tickets.priorityField')}</dt>
            <dd>
              <Badge
                tone={
                  data.priority === 'urgent' ? 'danger' : data.priority === 'high' ? 'warning' : 'neutral'
                }
              >
                {priorityLabel(t, data.priority)}
              </Badge>
            </dd>
            <dt>{t('tickets.channelField')}</dt>
            <dd>
              {data.channel
                ? t(`channel.${data.channel}` as Parameters<TFunction>[0])
                : <span className="text-subtle">—</span>}
            </dd>
            <dt>{t('tickets.slaDue')}</dt>
            <dd className={sla === 'breached' ? 'text-danger' : undefined}>
              {sla === 'none' ? (
                <span className="text-subtle">—</span>
              ) : (
                <>
                  <Timestamp value={data.resolution_due_at} />
                  {sla === 'breached'
                    ? ` · ${t('tickets.slaBreached')}`
                    : sla === 'due-soon'
                      ? ` · ${t('tickets.slaDueSoon')}`
                      : ''}
                </>
              )}
            </dd>
            <dt>{t('tickets.firstResponse')}</dt>
            <dd>
              <Timestamp value={data.first_responded_at} />
            </dd>
            <dt>{t('tickets.resolved')}</dt>
            <dd>
              <Timestamp value={data.resolved_at} />
            </dd>
            <dt>{t('tickets.descriptionField')}</dt>
            <dd>{data.description ?? <span className="text-subtle">—</span>}</dd>
          </dl>
        </Card>

        <Card title={t('tickets.relatedRecords')}>
          <dl className="dl">
            <dt>{t('contacts.accountField')}</dt>
            <dd>
              {data.account_id ? (
                <Link to={`/accounts/${data.account_id}`}>{t('tickets.viewAccount')}</Link>
              ) : (
                <span className="text-subtle">{t('common.notLinked')}</span>
              )}
            </dd>
            <dt>{t('tickets.relatedContact')}</dt>
            <dd>
              {data.contact_id ? (
                <Link to={`/contacts/${data.contact_id}`}>{t('tickets.viewContact')}</Link>
              ) : (
                <span className="text-subtle">{t('common.notLinked')}</span>
              )}
            </dd>
            <dt>{t('common.created')}</dt>
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
