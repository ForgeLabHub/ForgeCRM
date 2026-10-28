import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { accountsApi, contactsApi } from '../api/crm'
import { errorMessage, fieldErrors } from '../api/client'
import type { Contact, ContactInput } from '../types/api'
import { useAuth } from '../hooks/useAuth'
import { ResourceList, type Column } from '../components/ResourceList'
import { BulkArchiveButton } from '../components/BulkArchiveButton'
import {
  Alert,
  Card,
  ConfirmDialog,
  Field,
  PageHeader,
  Timestamp,
} from '../components/ui'

function fullName(contact: Contact): string {
  const name = [contact.first_name, contact.last_name].filter(Boolean).join(' ')
  return name || contact.email || 'Unnamed contact'
}

const columns: Column<Contact>[] = [
  {
    key: 'name',
    header: 'Name',
    render: (row) => (
      <div>
        <div className="table__primary">{fullName(row)}</div>
        {row.job_title ? <div className="table__secondary">{row.job_title}</div> : null}
      </div>
    ),
  },
  {
    key: 'email',
    header: 'Email',
    render: (row) => row.email ?? <span className="text-subtle">—</span>,
  },
  {
    key: 'phone',
    header: 'Phone',
    render: (row) => row.phone ?? row.mobile ?? <span className="text-subtle">—</span>,
  },
  {
    key: 'department',
    header: 'Department',
    render: (row) => row.department ?? <span className="text-subtle">—</span>,
  },
  {
    key: 'updated',
    header: 'Updated',
    render: (row) => <Timestamp value={row.updated_at} />,
  },
]

export function ContactsPage() {
  const navigate = useNavigate()
  const { can } = useAuth()
  const [creating, setCreating] = useState(false)

  return (
    <>
      <PageHeader title="Contacts" meta="The people you work with at each account." />
      <ResourceList
        title="All contacts"
        queryKey={['contacts']}
        fetchPage={(params) => contactsApi.list(params)}
        columns={columns}
        rowKey={(row) => row.id}
        onRowClick={(row) => navigate(`/contacts/${row.id}`)}
        emptyTitle="No contacts yet"
        emptyText="Add a contact to start recording interactions against a person."
        searchable
        searchPlaceholder="Search by name or email…"
        bulkActions={
          can('contacts.archive')
            ? (selected, clear) => (
                <BulkArchiveButton
                  ids={selected}
                  entityLabel="contacts"
                  archive={contactsApi.bulkArchive}
                  onDone={clear}
                />
              )
            : undefined
        }
        actions={
          can('contacts.create') ? (
            <button type="button" className="btn btn--primary btn--sm" onClick={() => setCreating(true)}>
              New contact
            </button>
          ) : null
        }
      />
      <ContactFormDialog
        open={creating}
        onClose={() => setCreating(false)}
        onSaved={(contact) => {
          setCreating(false)
          navigate(`/contacts/${contact.id}`)
        }}
      />
    </>
  )
}

export function ContactFormDialog({
  open,
  contact,
  onClose,
  onSaved,
}: {
  open: boolean
  contact?: Contact
  onClose: () => void
  onSaved: (contact: Contact) => void
}) {
  const queryClient = useQueryClient()
  const editing = Boolean(contact)
  const [form, setForm] = useState<ContactInput>(() => toInput(contact))
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)

  const formKey = contact?.id ?? 'new'
  const [lastKey, setLastKey] = useState(formKey)
  if (lastKey !== formKey) {
    setLastKey(formKey)
    setForm(toInput(contact))
    setErrors({})
    setFormError(null)
  }

  // Accounts populate the parent selector. A user who cannot read accounts
  // simply gets no options rather than an error.
  const accounts = useQuery({
    queryKey: ['accounts', 'for-select'],
    queryFn: () => accountsApi.list({ limit: 200 }),
    enabled: open,
  })

  const mutation = useMutation({
    mutationFn: (input: ContactInput) =>
      editing ? contactsApi.update(contact!.id, input) : contactsApi.create(input),
    onSuccess: (saved) => {
      void queryClient.invalidateQueries({ queryKey: ['contacts'] })
      onSaved(saved)
    },
    onError: (error) => {
      setErrors(fieldErrors(error))
      setFormError(errorMessage(error))
    },
  })

  function update<K extends keyof ContactInput>(key: K, value: ContactInput[K]) {
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
      aria-labelledby="contact-form-title"
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
          <h2 id="contact-form-title">{editing ? 'Edit contact' : 'New contact'}</h2>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="card__body">
            {formError ? (
              <div style={{ marginBottom: 'var(--space-4)' }}>
                <Alert tone="error">{formError}</Alert>
              </div>
            ) : null}

            <div className="form-grid">
              <Field label="First name" htmlFor="contact-first" error={errors.name}>
                <input
                  id="contact-first"
                  value={form.first_name ?? ''}
                  onChange={(event) => update('first_name', event.target.value || null)}
                />
              </Field>
              <Field label="Last name" htmlFor="contact-last">
                <input
                  id="contact-last"
                  value={form.last_name ?? ''}
                  onChange={(event) => update('last_name', event.target.value || null)}
                />
              </Field>
              <Field
                label="Email"
                htmlFor="contact-email"
                error={errors.email}
                hint={errors.name ? undefined : 'A name or an email is required.'}
              >
                <input
                  id="contact-email"
                  type="email"
                  value={form.email ?? ''}
                  onChange={(event) => update('email', event.target.value || null)}
                />
              </Field>
              <Field label="Phone" htmlFor="contact-phone">
                <input
                  id="contact-phone"
                  value={form.phone ?? ''}
                  onChange={(event) => update('phone', event.target.value || null)}
                />
              </Field>
              <Field label="Mobile" htmlFor="contact-mobile">
                <input
                  id="contact-mobile"
                  value={form.mobile ?? ''}
                  onChange={(event) => update('mobile', event.target.value || null)}
                />
              </Field>
              <Field label="Job title" htmlFor="contact-title">
                <input
                  id="contact-title"
                  value={form.job_title ?? ''}
                  onChange={(event) => update('job_title', event.target.value || null)}
                />
              </Field>
              <Field label="Department" htmlFor="contact-department">
                <input
                  id="contact-department"
                  value={form.department ?? ''}
                  onChange={(event) => update('department', event.target.value || null)}
                />
              </Field>
              <Field label="Account" htmlFor="contact-account">
                <select
                  id="contact-account"
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
              <Field label="Primary contact" htmlFor="contact-primary" full>
                <label className="checkbox">
                  <input
                    id="contact-primary"
                    type="checkbox"
                    checked={form.is_primary ?? false}
                    onChange={(event) => update('is_primary', event.target.checked)}
                  />
                  This is the main contact for the account
                </label>
              </Field>
            </div>
          </div>
          <div className="form-actions">
            <button type="button" className="btn btn--secondary" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="btn btn--primary" disabled={mutation.isPending}>
              {mutation.isPending ? 'Saving…' : editing ? 'Save changes' : 'Create contact'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

function toInput(contact?: Contact): ContactInput {
  if (!contact) return {}
  return {
    account_id: contact.account_id ?? null,
    first_name: contact.first_name ?? null,
    last_name: contact.last_name ?? null,
    email: contact.email ?? null,
    phone: contact.phone ?? null,
    mobile: contact.mobile ?? null,
    job_title: contact.job_title ?? null,
    department: contact.department ?? null,
    is_primary: contact.is_primary,
    owner_user_id: contact.owner_user_id ?? null,
    team_id: contact.team_id ?? null,
    custom_fields: contact.custom_fields,
  }
}

export function ContactDetailPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [archiving, setArchiving] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)

  const contact = useQuery({
    queryKey: ['contact', id],
    queryFn: () => contactsApi.get(id),
    enabled: Boolean(id),
  })

  const archive = useMutation({
    mutationFn: () => contactsApi.archive(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['contacts'] })
      navigate('/contacts')
    },
    onError: (error) => {
      setArchiving(false)
      setActionError(errorMessage(error))
    },
  })

  if (contact.isError) {
    return (
      <Card title="Contact">
        <Alert tone="error">{errorMessage(contact.error)}</Alert>
      </Card>
    )
  }

  if (contact.isLoading || !contact.data) {
    return <div className="skeleton" style={{ width: '100%', height: 240 }} aria-busy="true" />
  }

  const data = contact.data

  return (
    <>
      <PageHeader
        title={fullName(data)}
        meta={
          <>
            {data.job_title ? <>{data.job_title} · </> : null}
            Updated <Timestamp value={data.updated_at} />
          </>
        }
        actions={
          <>
            <Link to="/contacts" className="btn btn--ghost btn--sm">
              Back
            </Link>
            {can('contacts.update') ? (
              <button type="button" className="btn btn--secondary btn--sm" onClick={() => setEditing(true)}>
                Edit
              </button>
            ) : null}
            {can('contacts.archive') ? (
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

      <Card title="Details">
        <dl className="dl">
          <dt>Email</dt>
          <dd>{data.email ?? <span className="text-subtle">—</span>}</dd>
          <dt>Phone</dt>
          <dd>{data.phone ?? <span className="text-subtle">—</span>}</dd>
          <dt>Mobile</dt>
          <dd>{data.mobile ?? <span className="text-subtle">—</span>}</dd>
          <dt>Department</dt>
          <dd>{data.department ?? <span className="text-subtle">—</span>}</dd>
          <dt>Account</dt>
          <dd>
            {data.account_id ? (
              <Link to={`/accounts/${data.account_id}`}>View account</Link>
            ) : (
              <span className="text-subtle">Not linked</span>
            )}
          </dd>
          <dt>Primary contact</dt>
          <dd>{data.is_primary ? 'Yes' : 'No'}</dd>
          <dt>Created</dt>
          <dd>
            <Timestamp value={data.created_at} />
          </dd>
        </dl>
      </Card>

      <ContactFormDialog
        open={editing}
        contact={data}
        onClose={() => setEditing(false)}
        onSaved={() => {
          setEditing(false)
          void queryClient.invalidateQueries({ queryKey: ['contact', id] })
        }}
      />

      <ConfirmDialog
        open={archiving}
        title="Archive this contact?"
        text="The contact is kept but hidden from lists. Interaction history is preserved."
        confirmLabel="Archive"
        danger
        busy={archive.isPending}
        onConfirm={() => archive.mutate()}
        onCancel={() => setArchiving(false)}
      />
    </>
  )
}
