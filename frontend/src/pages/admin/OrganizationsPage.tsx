import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { organizationsApi } from '../../api/admin'
import { errorMessage, fieldErrors } from '../../api/client'
import { useAuth } from '../../hooks/useAuth'
import {
  Alert,
  Card,
  ErrorState,
  Field,
  PageHeader,
  StatusBadge,
  TableSkeleton,
  Timestamp,
} from '../../components/ui'

export function OrganizationsPage() {
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)

  const organizations = useQuery({
    queryKey: ['organizations'],
    queryFn: () => organizationsApi.list(),
  })

  if (organizations.isError) {
    return (
      <Card title="Organizations">
        <ErrorState
          message={errorMessage(organizations.error)}
          onRetry={() => void organizations.refetch()}
        />
      </Card>
    )
  }

  return (
    <>
      <PageHeader
        title="Organizations"
        meta="Tenants on this instance. Every record belongs to exactly one organization."
      />

      <Card
        title="All organizations"
        flush
        actions={
          can('organizations.manage') ? (
            <button type="button" className="btn btn--primary btn--sm" onClick={() => setCreating(true)}>
              New organization
            </button>
          ) : null
        }
      >
        {organizations.isLoading ? (
          <TableSkeleton rows={4} columns={4} />
        ) : organizations.data?.items.length ? (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Slug</th>
                  <th>Status</th>
                  <th>Created</th>
                </tr>
              </thead>
              <tbody>
                {organizations.data.items.map((organization) => (
                  <tr key={organization.id}>
                    <td className="table__primary">{organization.name}</td>
                    <td className="mono">{organization.slug}</td>
                    <td>
                      <StatusBadge status={organization.status} />
                    </td>
                    <td>
                      <Timestamp value={organization.created_at} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="card__body">
            <p className="text-muted">No organizations.</p>
          </div>
        )}
      </Card>

      <NewOrganizationDialog
        open={creating}
        onClose={() => setCreating(false)}
        onCreated={() => {
          setCreating(false)
          void queryClient.invalidateQueries({ queryKey: ['organizations'] })
        }}
      />
    </>
  )
}

function NewOrganizationDialog({
  open,
  onClose,
  onCreated,
}: {
  open: boolean
  onClose: () => void
  onCreated: () => void
}) {
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)

  const mutation = useMutation({
    mutationFn: () => organizationsApi.create({ name, slug }),
    onSuccess: onCreated,
    onError: (error) => {
      setErrors(fieldErrors(error))
      setFormError(errorMessage(error))
    },
  })

  function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setFormError(null)
    setErrors({})
    mutation.mutate()
  }

  if (!open) return null

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="new-org-title"
      style={{
        position: 'fixed',
        inset: 0,
        display: 'grid',
        placeItems: 'center',
        background: 'rgba(16, 24, 40, 0.45)',
        zIndex: 60,
        padding: 'var(--space-4)',
      }}
      onClick={onClose}
    >
      <div className="card" style={{ maxWidth: 460, width: '100%' }} onClick={(e) => e.stopPropagation()}>
        <div className="card__header">
          <h2 id="new-org-title">New organization</h2>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="card__body stack">
            {formError ? <Alert tone="error">{formError}</Alert> : null}
            <Field label="Name" htmlFor="org-name" required error={errors.name}>
              <input
                id="org-name"
                value={name}
                onChange={(event) => {
                  setName(event.target.value)
                  if (!slug) {
                    setSlug(
                      event.target.value
                        .toLowerCase()
                        .replace(/[^a-z0-9]+/g, '-')
                        .replace(/^-|-$/g, ''),
                    )
                  }
                }}
                required
              />
            </Field>
            <Field
              label="Slug"
              htmlFor="org-slug"
              required
              error={errors.slug}
              hint="Used in URLs and must be unique across the instance."
            >
              <input
                id="org-slug"
                className="mono"
                value={slug}
                onChange={(event) => setSlug(event.target.value)}
                required
              />
            </Field>
          </div>
          <div className="form-actions">
            <button type="button" className="btn btn--secondary" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="btn btn--primary" disabled={mutation.isPending}>
              {mutation.isPending ? 'Creating…' : 'Create'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
