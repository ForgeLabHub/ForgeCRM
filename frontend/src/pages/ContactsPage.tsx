import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { accountsApi, contactsApi } from '../api/crm'
import { errorMessage, fieldErrors } from '../api/client'
import type { Contact, ContactInput } from '../types/api'
import { useAuth } from '../hooks/useAuth'
import { useCreateParam } from '../hooks/useCreateParam'
import { ResourceList, type Column } from '../components/ResourceList'
import { AssistPanel } from '../components/AssistPanel'
import { AttachmentPanel } from '../components/AttachmentPanel'
import { RecordCollabPanel } from '../components/RecordCollabPanel'
import { CustomFieldsPanel } from '../components/CustomFieldsPanel'
import { ConsentPanel } from '../components/ConsentPanel'
import { BulkArchiveButton } from '../components/BulkArchiveButton'
import {
  Alert,
  Card,
  ConfirmDialog,
  Field,
  Modal,
  PageHeader,
  Timestamp,
} from '../components/ui'
import { useT, type TFunction } from '../i18n'

function fullName(contact: Contact, fallback: string): string {
  const name = [contact.first_name, contact.last_name].filter(Boolean).join(' ')
  return name || contact.email || fallback
}

function columns(t: TFunction): Column<Contact>[] {
  return [
    {
      key: 'name',
      header: t('contacts.columnName'),
      render: (row) => (
        <div>
          <div className="table__primary">{fullName(row, t('contacts.unnamed'))}</div>
          {row.job_title ? <div className="table__secondary">{row.job_title}</div> : null}
        </div>
      ),
    },
    {
      key: 'email',
      header: t('contacts.columnEmail'),
      render: (row) => row.email ?? <span className="text-subtle">—</span>,
    },
    {
      key: 'phone',
      header: t('contacts.columnPhone'),
      render: (row) => row.phone ?? row.mobile ?? <span className="text-subtle">—</span>,
    },
    {
      key: 'department',
      header: t('contacts.columnDepartment'),
      render: (row) => row.department ?? <span className="text-subtle">—</span>,
    },
    {
      key: 'updated',
      header: t('contacts.columnUpdated'),
      render: (row) => <Timestamp value={row.updated_at} />,
    },
  ]
}

export function ContactsPage() {
  const navigate = useNavigate()
  const { can } = useAuth()
  const t = useT()
  const [creating, setCreating] = useCreateParam(can('contacts.create'))

  return (
    <>
      <PageHeader title={t('contacts.title')} meta={t('contacts.meta')} />
      <ResourceList
        title={t('contacts.all')}
        queryKey={['contacts']}
        fetchPage={(params) => contactsApi.list(params)}
        columns={columns(t)}
        rowKey={(row) => row.id}
        onRowClick={(row) => navigate(`/app/contacts/${row.id}`)}
        emptyTitle={t('contacts.emptyTitle')}
        emptyText={t('contacts.emptyText')}
        searchable
        searchPlaceholder={t('contacts.searchPlaceholder')}
        bulkActions={
          can('contacts.archive')
            ? (selected, clear) => (
                <BulkArchiveButton
                  ids={selected}
                  entityLabel={t('contacts.bulkNoun')}
                  archive={contactsApi.bulkArchive}
                  onDone={clear}
                />
              )
            : undefined
        }
        actions={
          can('contacts.create') ? (
            <button type="button" className="btn btn--primary btn--sm" onClick={() => setCreating(true)}>
              {t('contacts.newContact')}
            </button>
          ) : null
        }
      />
      <ContactFormDialog
        open={creating}
        onClose={() => setCreating(false)}
        onSaved={(contact) => {
          setCreating(false)
          navigate(`/app/contacts/${contact.id}`)
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
  const t = useT()
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
    <Modal
      open
      onClose={onClose}
      title={editing ? t('contacts.editContact') : t('contacts.newContact')}
      size="lg"
      footer={<>
        <button type="button" className="btn btn--secondary" onClick={onClose}>
          {t('common.cancel')}
        </button>
        <button type="submit" className="btn btn--primary" disabled={mutation.isPending}>
          {mutation.isPending
            ? t('common.saving')
            : editing
              ? t('common.saveChanges')
              : t('contacts.createContact')}
        </button>
      </>}
      formProps={{ onSubmit: handleSubmit }}
    >
      {formError ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="error">{formError}</Alert>
        </div>
      ) : null}

      <div className="form-grid">
        <Field label={t('contacts.firstNameField')} htmlFor="contact-first" error={errors.name}>
          <input
            id="contact-first"
            value={form.first_name ?? ''}
            onChange={(event) => update('first_name', event.target.value || null)}
          />
        </Field>
        <Field label={t('contacts.lastNameField')} htmlFor="contact-last">
          <input
            id="contact-last"
            value={form.last_name ?? ''}
            onChange={(event) => update('last_name', event.target.value || null)}
          />
        </Field>
        <Field
          label={t('contacts.emailField')}
          htmlFor="contact-email"
          error={errors.email}
          hint={errors.name ? undefined : t('contacts.nameOrEmailHint')}
        >
          <input
            id="contact-email"
            type="email"
            value={form.email ?? ''}
            onChange={(event) => update('email', event.target.value || null)}
          />
        </Field>
        <Field label={t('contacts.phoneField')} htmlFor="contact-phone">
          <input
            id="contact-phone"
            value={form.phone ?? ''}
            onChange={(event) => update('phone', event.target.value || null)}
          />
        </Field>
        <Field label={t('contacts.mobileField')} htmlFor="contact-mobile">
          <input
            id="contact-mobile"
            value={form.mobile ?? ''}
            onChange={(event) => update('mobile', event.target.value || null)}
          />
        </Field>
        <Field label={t('contacts.jobTitleField')} htmlFor="contact-title">
          <input
            id="contact-title"
            value={form.job_title ?? ''}
            onChange={(event) => update('job_title', event.target.value || null)}
          />
        </Field>
        <Field label={t('contacts.departmentField')} htmlFor="contact-department">
          <input
            id="contact-department"
            value={form.department ?? ''}
            onChange={(event) => update('department', event.target.value || null)}
          />
        </Field>
        <Field label={t('contacts.accountField')} htmlFor="contact-account">
          <select
            id="contact-account"
            value={form.account_id ?? ''}
            onChange={(event) => update('account_id', event.target.value || null)}
          >
            <option value="">{t('contacts.noAccount')}</option>
            {(accounts.data?.items ?? []).map((account) => (
              <option key={account.id} value={account.id}>
                {account.name}
              </option>
            ))}
          </select>
        </Field>
        <Field label={t('contacts.primaryField')} htmlFor="contact-primary" full>
          <label className="checkbox">
            <input
              id="contact-primary"
              type="checkbox"
              checked={form.is_primary ?? false}
              onChange={(event) => update('is_primary', event.target.checked)}
            />
            {t('contacts.primaryHint')}
          </label>
        </Field>
      </div>
    </Modal>
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
  const t = useT()
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
      navigate('/app/contacts')
    },
    onError: (error) => {
      setArchiving(false)
      setActionError(errorMessage(error))
    },
  })

  const saveCustomFields = useMutation({
    mutationFn: (next: Contact['custom_fields']) =>
      contactsApi.update(id, { ...toInput(data), custom_fields: next }),
    onSuccess: (saved) => {
      queryClient.setQueryData(['contact', id], saved)
      void queryClient.invalidateQueries({ queryKey: ['contacts'] })
    },
  })

  if (contact.isError) {
    return (
      <Card title={t('contacts.detailTitle')}>
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
        title={fullName(data, t('contacts.unnamed'))}
        breadcrumb={[{ label: t('nav.contacts'), to: '/app/contacts' }]}
        meta={
          <>
            {data.job_title ? <>{data.job_title} · </> : null}
            {t('common.updated')} <Timestamp value={data.updated_at} />
          </>
        }
        actions={
          <>
            {can('contacts.update') ? (
              <button type="button" className="btn btn--secondary btn--sm" onClick={() => setEditing(true)}>
                {t('common.edit')}
              </button>
            ) : null}
            {can('contacts.archive') ? (
              <button type="button" className="btn btn--danger btn--sm" onClick={() => setArchiving(true)}>
                {t('common.archive')}
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

      <Card title={t('contacts.details')}>
        <dl className="dl">
          <dt>{t('contacts.emailField')}</dt>
          <dd>{data.email ?? <span className="text-subtle">—</span>}</dd>
          <dt>{t('contacts.phoneField')}</dt>
          <dd>{data.phone ?? <span className="text-subtle">—</span>}</dd>
          <dt>{t('contacts.mobileField')}</dt>
          <dd>{data.mobile ?? <span className="text-subtle">—</span>}</dd>
          <dt>{t('contacts.departmentField')}</dt>
          <dd>{data.department ?? <span className="text-subtle">—</span>}</dd>
          <dt>{t('contacts.accountField')}</dt>
          <dd>
            {data.account_id ? (
              <Link to={`/app/accounts/${data.account_id}`}>{t('contacts.viewAccount')}</Link>
            ) : (
              <span className="text-subtle">{t('common.notLinked')}</span>
            )}
          </dd>
          <dt>{t('contacts.primaryField')}</dt>
          <dd>{data.is_primary ? t('contacts.yes') : t('contacts.no')}</dd>
          <dt>{t('common.created')}</dt>
          <dd>
            <Timestamp value={data.created_at} />
          </dd>
        </dl>
      </Card>

      <AssistPanel entityType="contact" entityId={id} />

      <RecordCollabPanel entityType="contact" entityId={id} />

      <CustomFieldsPanel
        entityType="contact"
        values={data.custom_fields ?? {}}
        canEdit={can('contacts.update')}
        saving={saveCustomFields.isPending}
        onSave={(next) => saveCustomFields.mutateAsync(next)}
      />

      <ConsentPanel subject={{ contact_id: id }} />

      <AttachmentPanel entityType="contact" entityId={id} />

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
        title={t('contacts.archiveTitle')}
        text={t('contacts.archiveText')}
        confirmLabel={t('common.archive')}
        danger
        busy={archive.isPending}
        onConfirm={() => archive.mutate()}
        onCancel={() => setArchiving(false)}
      />
    </>
  )
}
