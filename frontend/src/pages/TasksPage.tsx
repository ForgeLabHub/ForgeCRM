import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { accountsApi, contactsApi, dealsApi, tasksApi } from '../api/crm'
import { errorMessage, fieldErrors } from '../api/client'
import type { Task, TaskInput } from '../types/api'
import { useAuth } from '../hooks/useAuth'
import { AssistPanel } from '../components/AssistPanel'
import { ResourceList, type Column } from '../components/ResourceList'
import { Alert, Badge, Field, PageHeader, StatusBadge, Timestamp } from '../components/ui'

const PRIORITIES = ['low', 'normal', 'high', 'urgent']
const STATUSES = ['open', 'in_progress', 'completed', 'cancelled']

function isOverdue(task: Task): boolean {
  if (!task.due_at || task.status === 'completed' || task.status === 'cancelled') return false
  return new Date(task.due_at).getTime() < Date.now()
}

export function TasksPage() {
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<Task | null>(null)
  const [statusFilter, setStatusFilter] = useState('')
  const [actionError, setActionError] = useState<string | null>(null)

  const complete = useMutation({
    mutationFn: (task: Task) =>
      tasksApi.update(task.id, {
        title: task.title,
        status: 'completed',
        priority: task.priority,
        description: task.description ?? null,
        due_at: task.due_at ?? null,
        assignee_user_id: task.assignee_user_id ?? null,
        team_id: task.team_id ?? null,
        account_id: task.account_id ?? null,
        contact_id: task.contact_id ?? null,
        deal_id: task.deal_id ?? null,
        ticket_id: task.ticket_id ?? null,
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['tasks'] })
      setActionError(null)
    },
    onError: (error) => setActionError(errorMessage(error)),
  })

  const columns: Column<Task>[] = [
    {
      key: 'title',
      header: 'Task',
      render: (row) => (
        <div>
          <div className="table__primary">{row.title}</div>
          {row.description ? (
            <div className="table__secondary truncate" style={{ maxWidth: 380 }}>
              {row.description}
            </div>
          ) : null}
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
    {
      key: 'status',
      header: 'Status',
      render: (row) => <StatusBadge status={row.status} />,
    },
    {
      key: 'due',
      header: 'Due',
      render: (row) =>
        row.due_at ? (
          <span className={isOverdue(row) ? 'text-danger' : undefined}>
            <Timestamp value={row.due_at} />
            {isOverdue(row) ? ' · overdue' : ''}
          </span>
        ) : (
          <span className="text-subtle">—</span>
        ),
    },
    {
      key: 'actions',
      header: '',
      width: '140px',
      render: (row) => (
        <div className="row">
          {can('tasks.update') && row.status !== 'completed' ? (
            <button
              type="button"
              className="btn btn--secondary btn--sm"
              onClick={(event) => {
                event.stopPropagation()
                complete.mutate(row)
              }}
              disabled={complete.isPending}
            >
              Complete
            </button>
          ) : null}
          {can('tasks.update') ? (
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
          ) : null}
        </div>
      ),
    },
  ]

  return (
    <>
      <PageHeader title="Tasks" meta="Work assigned to you and your team, with due dates and priority." />

      {actionError ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="error">{actionError}</Alert>
        </div>
      ) : null}

      <ResourceList
        title="All tasks"
        queryKey={['tasks', statusFilter]}
        fetchPage={(params) => tasksApi.list({ ...params, status: statusFilter || undefined })}
        columns={columns}
        rowKey={(row) => row.id}
        emptyTitle="No tasks"
        emptyText="Create a task to track something that needs doing."
        searchable
        searchPlaceholder="Search tasks…"
        filters={
          <label className="field" style={{ maxWidth: 180 }}>
            <span className="field__label">Status</span>
            <select
              value={statusFilter}
              onChange={(event) => {
                setStatusFilter(event.target.value)
              }}
            >
              <option value="">All statuses</option>
              {STATUSES.map((status) => (
                <option key={status} value={status}>
                  {status.replace('_', ' ')}
                </option>
              ))}
            </select>
          </label>
        }
        actions={
          can('tasks.create') ? (
            <button type="button" className="btn btn--primary btn--sm" onClick={() => setCreating(true)}>
              New task
            </button>
          ) : null
        }
      />

      <TaskFormDialog
        open={creating || editing !== null}
        task={editing ?? undefined}
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

export function TaskFormDialog({
  open,
  task,
  onClose,
  onSaved,
}: {
  open: boolean
  task?: Task
  onClose: () => void
  onSaved: (task: Task) => void
}) {
  const queryClient = useQueryClient()
  const editing = Boolean(task)
  const [form, setForm] = useState<TaskInput>(() => toInput(task))
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)

  const formKey = task?.id ?? 'new'
  const [lastKey, setLastKey] = useState(formKey)
  if (lastKey !== formKey) {
    setLastKey(formKey)
    setForm(toInput(task))
    setErrors({})
    setFormError(null)
  }

  const accounts = useQuery({
    queryKey: ['accounts', 'for-select'],
    queryFn: () => accountsApi.list({ limit: 200 }),
    enabled: open,
  })
  const contacts = useQuery({
    queryKey: ['contacts', 'for-select'],
    queryFn: () => contactsApi.list({ limit: 200 }),
    enabled: open,
  })
  const deals = useQuery({
    queryKey: ['deals', 'for-select'],
    queryFn: () => dealsApi.list({ limit: 200 }),
    enabled: open,
  })

  const mutation = useMutation({
    mutationFn: (input: TaskInput) =>
      editing ? tasksApi.update(task!.id, input) : tasksApi.create(input),
    onSuccess: (saved) => {
      void queryClient.invalidateQueries({ queryKey: ['tasks'] })
      onSaved(saved)
    },
    onError: (error) => {
      setErrors(fieldErrors(error))
      setFormError(errorMessage(error))
    },
  })

  function update<K extends keyof TaskInput>(key: K, value: TaskInput[K]) {
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
      aria-labelledby="task-form-title"
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
        style={{ maxWidth: 640, margin: '0 auto' }}
        onClick={(event) => event.stopPropagation()}
      >
        <div className="card__header">
          <h2 id="task-form-title">{editing ? 'Edit task' : 'New task'}</h2>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="card__body">
            {formError ? (
              <div style={{ marginBottom: 'var(--space-4)' }}>
                <Alert tone="error">{formError}</Alert>
              </div>
            ) : null}
            {editing && task ? (
              <div style={{ marginBottom: 'var(--space-4)' }}>
                <AssistPanel entityType="task" entityId={task.id} />
              </div>
            ) : null}
            <div className="form-grid">
              <Field label="Title" htmlFor="task-title" required error={errors.title} full>
                <input
                  id="task-title"
                  value={form.title}
                  onChange={(event) => update('title', event.target.value)}
                  className={errors.title ? 'input--invalid' : undefined}
                  required
                />
              </Field>
              <Field label="Status" htmlFor="task-status">
                <select
                  id="task-status"
                  value={form.status ?? 'open'}
                  onChange={(event) => update('status', event.target.value)}
                >
                  {STATUSES.map((status) => (
                    <option key={status} value={status}>
                      {status.replace('_', ' ')}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="Priority" htmlFor="task-priority">
                <select
                  id="task-priority"
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
              <Field label="Due" htmlFor="task-due">
                <input
                  id="task-due"
                  type="datetime-local"
                  value={toLocalInput(form.due_at ?? null)}
                  onChange={(event) =>
                    update('due_at', event.target.value ? new Date(event.target.value).toISOString() : null)
                  }
                />
              </Field>
              <Field label="Related account" htmlFor="task-account">
                <select
                  id="task-account"
                  value={form.account_id ?? ''}
                  onChange={(event) => update('account_id', event.target.value || null)}
                >
                  <option value="">None</option>
                  {(accounts.data?.items ?? []).map((account) => (
                    <option key={account.id} value={account.id}>
                      {account.name}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="Related contact" htmlFor="task-contact">
                <select
                  id="task-contact"
                  value={form.contact_id ?? ''}
                  onChange={(event) => update('contact_id', event.target.value || null)}
                >
                  <option value="">None</option>
                  {(contacts.data?.items ?? []).map((contact) => (
                    <option key={contact.id} value={contact.id}>
                      {[contact.first_name, contact.last_name].filter(Boolean).join(' ') ||
                        contact.email ||
                        contact.id}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="Related deal" htmlFor="task-deal">
                <select
                  id="task-deal"
                  value={form.deal_id ?? ''}
                  onChange={(event) => update('deal_id', event.target.value || null)}
                >
                  <option value="">None</option>
                  {(deals.data?.items ?? []).map((deal) => (
                    <option key={deal.id} value={deal.id}>
                      {deal.title}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="Description" htmlFor="task-description" full>
                <textarea
                  id="task-description"
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
              {mutation.isPending ? 'Saving…' : editing ? 'Save changes' : 'Create task'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

function toInput(task?: Task): TaskInput {
  if (!task) return { title: '', status: 'open', priority: 'normal' }
  return {
    title: task.title,
    description: task.description ?? null,
    status: task.status,
    priority: task.priority,
    due_at: task.due_at ?? null,
    assignee_user_id: task.assignee_user_id ?? null,
    team_id: task.team_id ?? null,
    account_id: task.account_id ?? null,
    contact_id: task.contact_id ?? null,
    deal_id: task.deal_id ?? null,
    ticket_id: task.ticket_id ?? null,
  }
}

/** Converts an ISO timestamp to the value a datetime-local input expects. */
function toLocalInput(iso: string | null): string {
  if (!iso) return ''
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return ''
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(
    date.getHours(),
  )}:${pad(date.getMinutes())}`
}
