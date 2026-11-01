import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { leadsApi } from '../api/crm'
import { errorMessage, fieldErrors } from '../api/client'
import type { ViewFilters } from '../api/views'
import type { Lead } from '../types/api'
import { useAuth } from '../hooks/useAuth'
import { ResourceList, type Column } from '../components/ResourceList'
import { SavedViewsBar } from '../components/SavedViewsBar'
import { Alert, Field, PageHeader, StatusBadge, Timestamp } from '../components/ui'

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
  const [creating, setCreating] = useState(false)
  const [converting, setConverting] = useState<Lead | null>(null)
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
            {can('leads.convert') ? (
              <button
                type="button"
                className="btn btn--secondary btn--sm"
                onClick={() => {
                  const first = document.querySelector<HTMLInputElement>('#lead-convert-id')
                  if (first) first.focus()
                }}
                title="Open a lead, then use Convert"
              >
                Convert a lead
              </button>
            ) : null}
            {can('leads.create') ? (
              <button type="button" className="btn btn--primary btn--sm" onClick={() => setCreating(true)}>
                New lead
              </button>
            ) : null}
          </div>
        }
      />
      <LeadFormDialog open={creating} onClose={() => setCreating(false)} onSaved={() => setCreating(false)} />
      {converting ? <ConvertLeadDialog lead={converting} onClose={() => setConverting(null)} /> : null}
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
                  value={form.job_title ?? ''}
                  onChange={(event) => update('job_title', event.target.value)}
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
export function ConvertLeadDialog({ lead, onClose }: { lead: Lead; onClose: () => void }) {
  const queryClient = useQueryClient()
  const [createDeal, setCreateDeal] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const mutation = useMutation({
    mutationFn: () => leadsApi.convert(lead.id, { create_deal: createDeal }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['leads'] })
      void queryClient.invalidateQueries({ queryKey: ['accounts'] })
      void queryClient.invalidateQueries({ queryKey: ['contacts'] })
      void queryClient.invalidateQueries({ queryKey: ['deals'] })
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
          <input id="lead-convert-id" type="hidden" value={lead.id} readOnly />
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
