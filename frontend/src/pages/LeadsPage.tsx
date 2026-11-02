import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { leadsApi } from '../api/crm'
import { errorMessage, fieldErrors } from '../api/client'
import type { ViewFilters } from '../api/views'
import type { Lead, LeadConversionResult } from '../types/api'
import { useAuth } from '../hooks/useAuth'
import { ResourceList, type Column } from '../components/ResourceList'
import { SavedViewsBar } from '../components/SavedViewsBar'
import { AttachmentPanel } from '../components/AttachmentPanel'
import { RecordCollabPanel } from '../components/RecordCollabPanel'
import { CustomFieldsPanel } from '../components/CustomFieldsPanel'
import {
  Alert,
  Card,
  ConfirmDialog,
  Field,
  PageHeader,
  StatusBadge,
  Timestamp,
} from '../components/ui'

const LEAD_STATUSES = ['new', 'contacted', 'qualified', 'unqualified', 'converted']

const columns: Column<Lead>[] = [
  {
    key: 'name',
    header: 'Name',
    render: (row) => (
      <div>
        <div className="table__primary">
          {[row.first_name, row.last_name].filter(Boolean).join(' ') || row.email || '—'}
        </div>
        {row.company ? <div className="table__secondary">{row.company}</div> : null}
      </div>
    ),
  },
  { key: 'email', header: 'Email', render: (row) => row.email ?? <span className="text-subtle">—</span> },
  { key: 'source', header: 'Source', render: (row) => row.source ?? <span className="text-subtle">—</span> },
  {
    key: 'status',
    header: 'Status',
    render: (row) => <StatusBadge status={row.status} />,
  },
  {
    key: 'created',
    header: 'Created',
    render: (row) => <Timestamp value={row.created_at} />,
  },
]

export function LeadsPage() {
  const { can } = useAuth()
  const navigate = useNavigate()
  const [creating, setCreating] = useState(false)
  const [statusFilter, setStatusFilter] = useState('')
  const [activeViewId, setActiveViewId] = useState<string | undefined>(undefined)

  return (
    <>
      <PageHeader title="Leads" meta="Unqualified interest, before it becomes an account and a deal." />
      <ResourceList
        title="All leads"
        queryKey={['leads', statusFilter]}
        fetchPage={(params) => leadsApi.list({ ...params, status: statusFilter || undefined })}
        columns={columns}
        rowKey={(row) => row.id}
        onRowClick={(row) => navigate(`/leads/${row.id}`)}
        emptyTitle="No leads yet"
        emptyText="Capture interest here, then qualify and convert it into a deal."
        searchable
        searchPlaceholder="Search leads…"
        filters={
          <>
            <SavedViewsBar
              entityType="leads"
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
                {LEAD_STATUSES.map((status) => (
                  <option key={status} value={status}>
                    {status}
                  </option>
                ))}
              </select>
            </label>
          </>
        }
        actions={
          <div className="row">
            {can('leads.create') ? (
              <button type="button" className="btn btn--primary btn--sm" onClick={() => setCreating(true)}>
                New lead
              </button>
            ) : null}
          </div>
        }
      />
      <LeadFormDialog open={creating} onClose={() => setCreating(false)} onSaved={() => setCreating(false)} />
    </>
  )
}

export function LeadFormDialog({
  open,
  lead,
  onClose,
  onSaved,
}: {
  open: boolean
  lead?: Lead
  onClose: () => void
  onSaved: (lead: Lead) => void
}) {
  const queryClient = useQueryClient()
  const editing = Boolean(lead)
  const [form, setForm] = useState<Partial<Lead>>(() => ({ ...lead, status: lead?.status ?? 'new' }))
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)

  const mutation = useMutation({
    mutationFn: (input: Partial<Lead>) =>
      editing ? leadsApi.update(lead!.id, input) : leadsApi.create(input),
    onSuccess: (saved) => {
      void queryClient.invalidateQueries({ queryKey: ['leads'] })
      onSaved(saved)
    },
    onError: (error) => {
      setErrors(fieldErrors(error))
      setFormError(errorMessage(error))
    },
  })

  function update<K extends keyof Lead>(key: K, value: Lead[K]) {
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
      aria-labelledby="lead-form-title"
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
        style={{ maxWidth: 640, margin: '0 auto' }}
        onClick={(event) => event.stopPropagation()}
      >
        <div className="card__header">
          <h2 id="lead-form-title">{editing ? 'Edit lead' : 'New lead'}</h2>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="card__body">
            {formError ? (
              <div style={{ marginBottom: 'var(--space-4)' }}>
                <Alert tone="error">{formError}</Alert>
              </div>
            ) : null}
            <div className="form-grid">
              <Field label="First name" htmlFor="lead-first">
                <input
                  id="lead-first"
                  value={form.first_name ?? ''}
                  onChange={(event) => update('first_name', event.target.value)}
                />
              </Field>
              <Field label="Last name" htmlFor="lead-last">
                <input
                  id="lead-last"
                  value={form.last_name ?? ''}
                  onChange={(event) => update('last_name', event.target.value)}
                />
              </Field>
              <Field label="Email" htmlFor="lead-email" error={errors.email}>
                <input
                  id="lead-email"
                  type="email"
                  value={form.email ?? ''}
                  onChange={(event) => update('email', event.target.value)}
                />
              </Field>
              <Field label="Phone" htmlFor="lead-phone">
                <input
                  id="lead-phone"
                  value={form.phone ?? ''}
                  onChange={(event) => update('phone', event.target.value)}
                />
              </Field>
              <Field label="Company" htmlFor="lead-company">
                <input
                  id="lead-company"
                  value={form.company ?? ''}
                  onChange={(event) => update('company', event.target.value)}
                />
              </Field>
              <Field label="Job title" htmlFor="lead-title">
                <input
                  id="lead-title"
                  value={form.title ?? ''}
                  onChange={(event) => update('title', event.target.value)}
                />
              </Field>
              <Field label="Source" htmlFor="lead-source" hint="Where the lead came from.">
                <input
                  id="lead-source"
                  value={form.source ?? ''}
                  onChange={(event) => update('source', event.target.value)}
                />
              </Field>
              <Field label="Status" htmlFor="lead-status">
                <select
                  id="lead-status"
                  value={form.status ?? 'new'}
                  onChange={(event) => update('status', event.target.value)}
                >
                  {LEAD_STATUSES.map((status) => (
                    <option key={status} value={status}>
                      {status}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="Notes" htmlFor="lead-notes" full>
                <textarea
                  id="lead-notes"
                  value={form.notes ?? ''}
                  onChange={(event) => update('notes', event.target.value)}
                />
              </Field>
            </div>
          </div>
          <div className="form-actions">
            <button type="button" className="btn btn--secondary" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="btn btn--primary" disabled={mutation.isPending}>
              {mutation.isPending ? 'Saving…' : editing ? 'Save changes' : 'Create lead'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

/** Converting a lead creates an account, a contact and optionally a deal. */
export function ConvertLeadDialog({
  lead,
  onClose,
  onConverted,
}: {
  lead: Lead
  onClose: () => void
  onConverted?: (result: LeadConversionResult) => void
}) {
  const queryClient = useQueryClient()
  const [createDeal, setCreateDeal] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const mutation = useMutation({
    mutationFn: () => leadsApi.convert(lead.id, { create_deal: createDeal }),
    onSuccess: (result) => {
      void queryClient.invalidateQueries({ queryKey: ['leads'] })
      void queryClient.invalidateQueries({ queryKey: ['accounts'] })
      void queryClient.invalidateQueries({ queryKey: ['contacts'] })
      void queryClient.invalidateQueries({ queryKey: ['deals'] })
      onConverted?.(result)
      onClose()
    },
    onError: (err) => setError(errorMessage(err)),
  })

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="convert-lead-title"
      style={{
        position: 'fixed',
        inset: 0,
        display: 'grid',
        placeItems: 'center',
        background: 'var(--color-overlay)',
        zIndex: 60,
        padding: 'var(--space-4)',
      }}
      onClick={onClose}
    >
      <div className="card" style={{ maxWidth: 460, width: '100%' }} onClick={(e) => e.stopPropagation()}>
        <div className="card__header">
          <h2 id="convert-lead-title">Convert lead</h2>
        </div>
        <div className="card__body stack">
          {error ? <Alert tone="error">{error}</Alert> : null}
          <p className="text-muted">
            This creates an account and a contact from the lead's details, and marks the lead as
            converted. The original lead is kept for reporting.
          </p>
          <label className="checkbox">
            <input
              type="checkbox"
              checked={createDeal}
              onChange={(event) => setCreateDeal(event.target.checked)}
            />
            Also open a deal in the default pipeline
          </label>
        </div>
        <div className="form-actions" style={{ margin: 0, padding: 'var(--space-4)' }}>
          <button type="button" className="btn btn--secondary" onClick={onClose}>
            Cancel
          </button>
          <button
            type="button"
            className="btn btn--primary"
            disabled={mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            {mutation.isPending ? 'Converting…' : 'Convert'}
          </button>
        </div>
      </div>
    </div>
  )
}

function leadName(lead: Lead): string {
  const name = [lead.first_name, lead.last_name].filter(Boolean).join(' ')
  return name || lead.company || lead.email || 'Unnamed lead'
}

/** The editable subset of a lead. Server-computed fields (conversion, score) are not sent back. */
function toLeadInput(lead?: Lead): Partial<Lead> {
  if (!lead) return {}
  return {
    first_name: lead.first_name ?? null,
    last_name: lead.last_name ?? null,
    email: lead.email ?? null,
    phone: lead.phone ?? null,
    company: lead.company ?? null,
    title: lead.title ?? null,
    source: lead.source ?? null,
    status: lead.status,
    notes: lead.notes ?? null,
    owner_user_id: lead.owner_user_id ?? null,
    team_id: lead.team_id ?? null,
    custom_fields: lead.custom_fields,
  } as Partial<Lead>
}

/** A lead is already converted once the server has stamped `converted_at`. */
export function isConverted(lead: Lead): boolean {
  return Boolean(lead.converted_at) || lead.status === 'converted'
}

export function LeadDetailPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [converting, setConverting] = useState(false)
  const [converted, setConverted] = useState<{ account: string; contact: string } | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const lead = useQuery({
    queryKey: ['lead', id],
    queryFn: () => leadsApi.get(id),
    enabled: Boolean(id),
  })

  const saveCustomFields = useMutation({
    mutationFn: (next: Lead['custom_fields']) =>
      leadsApi.update(id, { ...toLeadInput(lead.data), custom_fields: next }),
    onSuccess: (saved) => {
      queryClient.setQueryData(['lead', id], saved)
      void queryClient.invalidateQueries({ queryKey: ['leads'] })
    },
    onError: (err) => setActionError(errorMessage(err)),
  })

  if (lead.isError) {
    return (
      <Card title="Lead">
        <Alert tone="error">{errorMessage(lead.error)}</Alert>
      </Card>
    )
  }

  if (lead.isLoading || !lead.data) {
    return <div className="skeleton" style={{ width: '100%', height: 240 }} aria-busy="true" />
  }

  const data = lead.data
  const done = isConverted(data)

  return (
    <>
      <PageHeader
        title={leadName(data)}
        meta={
          <>
            {data.company ? <>{data.company} · </> : null}
            Updated <Timestamp value={data.updated_at} />
          </>
        }
        actions={
          <>
            <Link to="/leads" className="btn btn--ghost btn--sm">
              Back
            </Link>
            {can('leads.update') ? (
              <button type="button" className="btn btn--secondary btn--sm" onClick={() => setEditing(true)}>
                Edit
              </button>
            ) : null}
            {can('leads.convert') && !done ? (
              <button type="button" className="btn btn--primary btn--sm" onClick={() => setConverting(true)}>
                Convert
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

      {done ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="success">
            This lead is converted.{' '}
            {data.converted_account_id ? (
              <Link to={`/accounts/${data.converted_account_id}`}>View the account</Link>
            ) : null}
            {data.converted_contact_id ? (
              <>
                {' '}
                <Link to={`/contacts/${data.converted_contact_id}`}>View the contact</Link>
              </>
            ) : null}
          </Alert>
        </div>
      ) : null}

      <Card title="Details">
        <dl className="dl">
          <dt>Email</dt>
          <dd>{data.email ?? <span className="text-subtle">—</span>}</dd>
          <dt>Phone</dt>
          <dd>{data.phone ?? <span className="text-subtle">—</span>}</dd>
          <dt>Company</dt>
          <dd>{data.company ?? <span className="text-subtle">—</span>}</dd>
          <dt>Job title</dt>
          <dd>{data.title ?? <span className="text-subtle">—</span>}</dd>
          <dt>Source</dt>
          <dd>{data.source ?? <span className="text-subtle">—</span>}</dd>
          <dt>Status</dt>
          <dd>
            <StatusBadge status={data.status} />
          </dd>
          <dt>Score</dt>
          <dd>{data.score ?? <span className="text-subtle">—</span>}</dd>
          <dt>Notes</dt>
          <dd>{data.notes ?? <span className="text-subtle">—</span>}</dd>
          <dt>Created</dt>
          <dd>
            <Timestamp value={data.created_at} />
          </dd>
        </dl>
      </Card>

      <RecordCollabPanel entityType="lead" entityId={id} />

      <CustomFieldsPanel
        entityType="lead"
        values={data.custom_fields ?? {}}
        canEdit={can('leads.update')}
        saving={saveCustomFields.isPending}
        onSave={(next) => saveCustomFields.mutateAsync(next)}
      />

      <AttachmentPanel entityType="lead" entityId={id} />

      <LeadFormDialog
        open={editing}
        lead={data}
        onClose={() => setEditing(false)}
        onSaved={() => {
          setEditing(false)
          void queryClient.invalidateQueries({ queryKey: ['lead', id] })
        }}
      />

      {converting ? (
        <ConvertLeadDialog
          lead={data}
          onClose={() => setConverting(false)}
          onConverted={(result) => {
            setConverting(false)
            setConverted({
              account: result.account.id,
              contact: result.contact.id,
            })
            queryClient.setQueryData(['lead', id], result.lead)
            void queryClient.invalidateQueries({ queryKey: ['leads'] })
            void queryClient.invalidateQueries({ queryKey: ['accounts'] })
            void queryClient.invalidateQueries({ queryKey: ['contacts'] })
          }}
        />
      ) : null}

      {converted ? (
        <ConfirmDialog
          open
          title="Lead converted"
          text="An account and a contact were created from this lead. Open the account to keep working on it."
          confirmLabel="Open account"
          onConfirm={() => {
            navigate(`/accounts/${converted.account}`)
          }}
          onCancel={() => setConverted(null)}
        />
      ) : null}
    </>
  )
}
