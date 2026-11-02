import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { organizationsApi } from '../../api/admin'
import { errorMessage, fieldErrors } from '../../api/client'
import { useAuth } from '../../hooks/useAuth'
import { useT } from '../../i18n'
import {
  Alert,
  Card,
  ErrorState,
  Field,
  Modal,
  PageHeader,
  StatusBadge,
  TableSkeleton,
  Timestamp,
} from '../../components/ui'

export function OrganizationsPage() {
  const { can } = useAuth()
  const t = useT()
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)

  const organizations = useQuery({
    queryKey: ['organizations'],
    queryFn: () => organizationsApi.list(),
  })

  if (organizations.isError) {
    return (
      <Card title={t('organizations.title')}>
        <ErrorState
          message={errorMessage(organizations.error)}
          onRetry={() => void organizations.refetch()}
        />
      </Card>
    )
  }

  return (
    <>
      <PageHeader title={t('organizations.title')} meta={t('organizations.meta')} />

      <Card
        title={t('organizations.all')}
        flush
        actions={
          can('organizations.manage') ? (
            <button type="button" className="btn btn--primary btn--sm" onClick={() => setCreating(true)}>
              {t('organizations.newOrganization')}
            </button>
          ) : null
        }
      >
        {organizations.isLoading ? (
          <TableSkeleton rows={4} columns={4} />
        ) : organizations.data?.items.length ? (
          <div className="table-wrap">
            <table className="table--stack">
              <thead>
                <tr>
                  <th>{t('organizations.columnName')}</th>
                  <th>{t('organizations.columnSlug')}</th>
                  <th>{t('organizations.columnStatus')}</th>
                  <th>{t('organizations.columnCreated')}</th>
                </tr>
              </thead>
              <tbody>
                {organizations.data.items.map((organization) => (
                  <tr key={organization.id}>
                    <td className="table__primary" data-label={t('organizations.columnName')}>
                      {organization.name}
                    </td>
                    <td className="mono" data-label={t('organizations.columnSlug')}>
                      {organization.slug}
                    </td>
                    <td data-label={t('organizations.columnStatus')}>
                      <StatusBadge status={organization.status} />
                    </td>
                    <td data-label={t('organizations.columnCreated')}>
                      <Timestamp value={organization.created_at} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="card__body">
            <p className="text-muted">{t('organizations.empty')}</p>
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
    const t = useT()
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
    <Modal
      open
      onClose={onClose}
      title={t('organizations.newOrganizationTitle')}
      size="sm"
      footer={<>
        <button type="button" className="btn btn--secondary" onClick={onClose}>
          {t('common.cancel')}
        </button>
        <button type="submit" className="btn btn--primary" disabled={mutation.isPending}>
          {mutation.isPending ? t('organizations.creating') : t('common.create')}
        </button>
      </>}
      formProps={{ onSubmit: handleSubmit }}
    >
      <div className="stack">
        {formError ? <Alert tone="error">{formError}</Alert> : null}
        <Field label={t('organizations.name')} htmlFor="org-name" required error={errors.name}>
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
          label={t('organizations.slug')}
          htmlFor="org-slug"
          required
          error={errors.slug}
          hint={t('organizations.slugHint')}
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
    </Modal>
  )
}
