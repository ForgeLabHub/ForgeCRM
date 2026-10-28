import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { accountsApi, tasksApi } from '../api/crm'
import { errorMessage, fieldErrors } from '../api/client'
import type { Account, AccountInput } from '../types/api'
import { useAuth } from '../hooks/useAuth'
import { ResourceList, type Column } from '../components/ResourceList'
import { BulkArchiveButton } from '../components/BulkArchiveButton'
import {
  Alert,
  Card,
  ConfirmDialog,
  Field,
  PageHeader,
  StatusBadge,
  Timestamp,
  formatMoney,
} from '../components/ui'

const columns: Column<Account>[] = [
  {
    key: 'name',
    header: 'Name',
    render: (row) => (
      <div>
        <div className="table__primary">{row.name}</div>
        {row.website ? <div className="table__secondary truncate">{row.website}</div> : null}
      </div>
    ),
  },
  {
    key: 'industry',
    header: 'Industry',
    render: (row) => row.industry ?? <span className="text-subtle">—</span>,
  },
  {
    key: 'employees',
    header: 'Employees',
    numeric: true,
    render: (row) => row.employee_count ?? <span className="text-subtle">—</span>,
  },
  {
    key: 'revenue',
    header: 'Annual revenue',
    numeric: true,
    render: (row) =>
      row.annual_revenue !== undefined ? formatMoney(row.annual_revenue) : <span className="text-subtle">—</span>,
  },
  {
    key: 'updated',
    header: 'Updated',
    render: (row) => <Timestamp value={row.updated_at} />,
  },
]

export function AccountsPage() {
  const navigate = useNavigate()
  const { can } = useAuth()
  const [creating, setCreating] = useState(false)

  return (
    <>
      <PageHeader
        title="Accounts"
        meta="Companies you sell to, and the deals and contacts attached to them."
      />
      <ResourceList
        title="All accounts"
        queryKey={['accounts']}
        fetchPage={(params) => accountsApi.list(params)}
        columns={columns}
        rowKey={(row) => row.id}
        onRowClick={(row) => navigate(`/accounts/${row.id}`)}
        emptyTitle="No accounts yet"
        emptyText="Accounts are the companies you do business with. Create one to get started."
        searchable
        searchPlaceholder="Search accounts…"
        bulkActions={
          can('accounts.archive')
            ? (selected, clear) => (
                <BulkArchiveButton
                  ids={selected}
                  entityLabel="accounts"
                  archive={accountsApi.bulkArchive}
                  onDone={clear}
                />
              )
            : undefined
        }
        actions={
          can('accounts.create') ? (
            <button type="button" className="btn btn--primary btn--sm" onClick={() => setCreating(true)}>
              New account
            </button>
          ) : null
        }
      />
      <AccountFormDialog
        open={creating}
        onClose={() => setCreating(false)}
        onSaved={(account) => {
          setCreating(false)
          navigate(`/accounts/${account.id}`)
        }}
      />
    </>
  )
}

/** Create/edit form. Also used from the detail page for editing. */
export function AccountFormDialog({
  open,
  account,
  onClose,
  onSaved,
}: {
  open: boolean
  account?: Account
  onClose: () => void
  onSaved: (account: Account) => void
}) {
  const queryClient = useQueryClient()
  const editing = Boolean(account)

  const [form, setForm] = useState<AccountInput>(() => toInput(account))
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)

  // Reset the fields whenever the dialog opens for a different record.
  const formKey = account?.id ?? 'new'
  const [lastKey, setLastKey] = useState(formKey)
  if (lastKey !== formKey) {
    setLastKey(formKey)
    setForm(toInput(account))
    setErrors({})
    setFormError(null)
  }

  const mutation = useMutation({
    mutationFn: (input: AccountInput) =>
      editing ? accountsApi.update(account!.id, input) : accountsApi.create(input),
    onSuccess: (saved) => {
      void queryClient.invalidateQueries({ queryKey: ['accounts'] })
      onSaved(saved)
    },
    onError: (error) => {
      setErrors(fieldErrors(error))
      setFormError(errorMessage(error))
    },
  })

  function update<K extends keyof AccountInput>(key: K, value: AccountInput[K]) {
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
      aria-labelledby="account-form-title"
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
        style={{ maxWidth: 720, margin: '0 auto' }}
        onClick={(event) => event.stopPropagation()}
      >
        <div className="card__header">
          <h2 id="account-form-title">{editing ? 'Edit account' : 'New account'}</h2>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="card__body">
            {formError ? (
              <div style={{ marginBottom: 'var(--space-4)' }}>
                <Alert tone="error">{formError}</Alert>
              </div>
            ) : null}

            <div className="form-grid">
              <Field label="Name" htmlFor="account-name" required error={errors.name}>
                <input
                  id="account-name"
                  value={form.name}
                  onChange={(event) => update('name', event.target.value)}
                  className={errors.name ? 'input--invalid' : undefined}
                  required
                />
              </Field>
              <Field label="Legal name" htmlFor="account-legal">
                <input
                  id="account-legal"
                  value={form.legal_name ?? ''}
                  onChange={(event) => update('legal_name', event.target.value || null)}
                />
              </Field>
              <Field label="Industry" htmlFor="account-industry">
                <input
                  id="account-industry"
                  value={form.industry ?? ''}
                  onChange={(event) => update('industry', event.target.value || null)}
                />
              </Field>
              <Field label="Website" htmlFor="account-website">
                <input
                  id="account-website"
                  type="url"
                  placeholder="https://"
                  value={form.website ?? ''}
                  onChange={(event) => update('website', event.target.value || null)}
                />
              </Field>
              <Field label="Email" htmlFor="account-email" error={errors.email}>
                <input
                  id="account-email"
                  type="email"
                  value={form.email ?? ''}
                  onChange={(event) => update('email', event.target.value || null)}
                />
              </Field>
              <Field label="Phone" htmlFor="account-phone">
                <input
                  id="account-phone"
                  value={form.phone ?? ''}
                  onChange={(event) => update('phone', event.target.value || null)}
                />
              </Field>
              <Field label="Tax ID" htmlFor="account-tax">
                <input
                  id="account-tax"
                  value={form.tax_id ?? ''}
                  onChange={(event) => update('tax_id', event.target.value || null)}
                />
              </Field>
              <Field label="Employees" htmlFor="account-employees">
                <input
                  id="account-employees"
                  type="number"
                  min={0}
                  value={form.employee_count ?? ''}
                  onChange={(event) =>
                    update('employee_count', event.target.value === '' ? null : Number(event.target.value))
                  }
                />
              </Field>
              <Field label="Annual revenue" htmlFor="account-revenue">
                <input
                  id="account-revenue"
                  type="number"
                  min={0}
                  step="0.01"
                  value={form.annual_revenue ?? ''}
                  onChange={(event) =>
                    update('annual_revenue', event.target.value === '' ? null : Number(event.target.value))
                  }
                />
              </Field>
              <Field label="Country" htmlFor="account-country">
                <input
                  id="account-country"
                  value={form.country ?? ''}
                  onChange={(event) => update('country', event.target.value || null)}
                />
              </Field>
              <Field label="City" htmlFor="account-city">
                <input
                  id="account-city"
                  value={form.city ?? ''}
                  onChange={(event) => update('city', event.target.value || null)}
                />
              </Field>
              <Field label="Address" htmlFor="account-address" full>
                <input
                  id="account-address"
                  value={form.address_line1 ?? ''}
                  onChange={(event) => update('address_line1', event.target.value || null)}
                />
              </Field>
            </div>
          </div>
          <div className="form-actions">
            <button type="button" className="btn btn--secondary" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="btn btn--primary" disabled={mutation.isPending}>
              {mutation.isPending ? 'Saving…' : editing ? 'Save changes' : 'Create account'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

function toInput(account?: Account): AccountInput {
  if (!account) return { name: '' }
  return {
    name: account.name,
    legal_name: account.legal_name ?? null,
    industry: account.industry ?? null,
    website: account.website ?? null,
    phone: account.phone ?? null,
    email: account.email ?? null,
    tax_id: account.tax_id ?? null,
    address_line1: account.address_line1 ?? null,
    city: account.city ?? null,
    region: account.region ?? null,
    postal_code: account.postal_code ?? null,
    country: account.country ?? null,
    annual_revenue: account.annual_revenue ?? null,
    employee_count: account.employee_count ?? null,
    owner_user_id: account.owner_user_id ?? null,
    team_id: account.team_id ?? null,
    custom_fields: account.custom_fields,
  }
}

export function AccountDetailPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [archiving, setArchiving] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)

  const account = useQuery({
    queryKey: ['account', id],
    queryFn: () => accountsApi.get(id),
    enabled: Boolean(id),
  })

  const contacts = useQuery({
    queryKey: ['account', id, 'contacts'],
    queryFn: () => accountsApi.contacts(id, { limit: 10 }),
    enabled: Boolean(id) && can('contacts.read'),
  })

  const deals = useQuery({
    queryKey: ['account', id, 'deals'],
    queryFn: () => accountsApi.deals(id, { limit: 10 }),
    enabled: Boolean(id) && can('deals.read'),
  })

  // Open tasks for this account. The server applies the record-level rule: a
  // caller without tasks.assign sees only tasks assigned to them or their teams,
  // so this list is already narrowed and must not be widened on the client.
  const tasks = useQuery({
    queryKey: ['account', id, 'tasks'],
    queryFn: () => tasksApi.list({ account_id: id, limit: 10 }),
    enabled: Boolean(id) && can('tasks.read'),
  })

  const activities = useQuery({
    queryKey: ['account', id, 'activities'],
    queryFn: () => accountsApi.activities(id, { limit: 10 }),
    enabled: Boolean(id) && can('activities.read'),
  })

  const archive = useMutation({
    mutationFn: () => accountsApi.archive(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['accounts'] })
      navigate('/accounts')
    },
    onError: (error) => {
      setArchiving(false)
      setActionError(errorMessage(error))
    },
  })

  if (account.isError) {
    return (
      <Card title="Account">
        <Alert tone="error">{errorMessage(account.error)}</Alert>
      </Card>
    )
  }

  if (account.isLoading || !account.data) {
    return (
      <div aria-busy="true">
        <div className="skeleton" style={{ width: 240, height: 28, marginBottom: 16 }} />
        <div className="skeleton" style={{ width: '100%', height: 200 }} />
      </div>
    )
  }

  const data = account.data

  return (
    <>
      <PageHeader
        title={data.name}
        meta={
          <>
            {data.industry ? <>{data.industry} · </> : null}
            Updated <Timestamp value={data.updated_at} />
          </>
        }
        actions={
          <>
            <Link to="/accounts" className="btn btn--ghost btn--sm">
              Back
            </Link>
            {can('accounts.update') ? (
              <button type="button" className="btn btn--secondary btn--sm" onClick={() => setEditing(true)}>
                Edit
              </button>
            ) : null}
            {can('accounts.archive') ? (
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
            <dt>Legal name</dt>
            <dd>{data.legal_name ?? <span className="text-subtle">—</span>}</dd>
            <dt>Website</dt>
            <dd>
              {data.website ? (
                <a href={data.website} target="_blank" rel="noreferrer noopener">
                  {data.website}
                </a>
              ) : (
                <span className="text-subtle">—</span>
              )}
            </dd>
            <dt>Email</dt>
            <dd>{data.email ?? <span className="text-subtle">—</span>}</dd>
            <dt>Phone</dt>
            <dd>{data.phone ?? <span className="text-subtle">—</span>}</dd>
            <dt>Employees</dt>
            <dd>{data.employee_count ?? <span className="text-subtle">—</span>}</dd>
            <dt>Annual revenue</dt>
            <dd>
              {data.annual_revenue !== undefined ? (
                formatMoney(data.annual_revenue)
              ) : (
                <span className="text-subtle">—</span>
              )}
            </dd>
            <dt>Address</dt>
            <dd>
              {[data.address_line1, data.city, data.region, data.postal_code, data.country]
                .filter(Boolean)
                .join(', ') || <span className="text-subtle">—</span>}
            </dd>
            <dt>Created</dt>
            <dd>
              <Timestamp value={data.created_at} />
            </dd>
          </dl>
        </Card>

        <div className="stack">
          {can('contacts.read') ? (
            <Card title={`Contacts${contacts.data ? ` (${contacts.data.total})` : ''}`}>
              {contacts.data?.items.length ? (
                <div className="stack stack--sm">
                  {contacts.data.items.map((contact) => (
                    <Link
                      key={contact.id}
                      to={`/contacts/${contact.id}`}
                      className="row row--between"
                      style={{ color: 'inherit' }}
                    >
                      <span className="table__primary">
                        {[contact.first_name, contact.last_name].filter(Boolean).join(' ') || contact.email}
                      </span>
                      <span className="table__secondary">{contact.job_title ?? contact.email}</span>
                    </Link>
                  ))}
                </div>
              ) : (
                <p className="text-muted text-sm">No contacts linked to this account.</p>
              )}
            </Card>
          ) : null}

          {can('deals.read') ? (
            <Card title={`Deals${deals.data ? ` (${deals.data.total})` : ''}`}>
              {deals.data?.items.length ? (
                <div className="stack stack--sm">
                  {deals.data.items.map((deal) => (
                    <Link
                      key={deal.id}
                      to={`/deals/${deal.id}`}
                      className="row row--between"
                      style={{ color: 'inherit' }}
                    >
                      <span className="table__primary truncate">{deal.title}</span>
                      <span className="row">
                        <StatusBadge status={deal.status} />
                        <span className="table__secondary numeric">
                          {formatMoney(deal.amount, deal.currency)}
                        </span>
                      </span>
                    </Link>
                  ))}
                </div>
              ) : (
                <p className="text-muted text-sm">No deals for this account.</p>
              )}
            </Card>
          ) : null}

          {can('tasks.read') ? (
            <Card title={`Open tasks${tasks.data ? ` (${tasks.data.total})` : ''}`}>
              {tasks.isError ? (
                <Alert tone="error">{errorMessage(tasks.error)}</Alert>
              ) : tasks.data?.items.length ? (
                <div className="stack stack--sm">
                  {tasks.data.items.map((task) => (
                    <div key={task.id} className="row row--between">
                      <span className="table__primary truncate">{task.title}</span>
                      <span className="row">
                        <StatusBadge status={task.status} />
                        {task.due_at ? <Timestamp value={task.due_at} /> : null}
                      </span>
                    </div>
                  ))}
                </div>
              ) : (
                <p className="text-muted text-sm">No open tasks for this account.</p>
              )}
            </Card>
          ) : null}

          {can('activities.read') ? (
            <Card title={`Activity history${activities.data ? ` (${activities.data.total})` : ''}`}>
              {activities.isError ? (
                <Alert tone="error">{errorMessage(activities.error)}</Alert>
              ) : activities.data?.items.length ? (
                <div className="stack stack--sm">
                  {activities.data.items.map((activity) => (
                    <div key={activity.id} className="row row--between">
                      <span className="table__primary truncate">
                        {activity.subject || activity.type}
                      </span>
                      <span className="row">
                        <span className="text-subtle text-sm">{activity.type}</span>
                        <Timestamp value={activity.occurred_at} />
                      </span>
                    </div>
                  ))}
                </div>
              ) : (
                <p className="text-muted text-sm">No recorded interactions yet.</p>
              )}
            </Card>
          ) : null}
        </div>
      </div>

      <AccountFormDialog
        open={editing}
        account={data}
        onClose={() => setEditing(false)}
        onSaved={() => {
          setEditing(false)
          void queryClient.invalidateQueries({ queryKey: ['account', id] })
        }}
      />

      <ConfirmDialog
        open={archiving}
        title="Archive this account?"
        text="The account and its history are kept, but it disappears from lists. This can be reversed by an administrator."
        confirmLabel="Archive"
        danger
        busy={archive.isPending}
        onConfirm={() => archive.mutate()}
        onCancel={() => setArchiving(false)}
      />
    </>
  )
}
