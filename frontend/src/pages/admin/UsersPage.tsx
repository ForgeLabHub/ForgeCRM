import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { usersApi } from '../../api/auth'
import { rolesApi } from '../../api/admin'
import { errorMessage, fieldErrors } from '../../api/client'
import type { User } from '../../types/api'
import { useAuth } from '../../hooks/useAuth'
import { useT } from '../../i18n'
import { ResourceList, type Column } from '../../components/ResourceList'
import {
  Alert,
  Badge,
  ConfirmDialog,
  Field,
  Modal,
  PageHeader,
  StatusBadge,
  Timestamp,
} from '../../components/ui'

export function UsersPage() {
  const { can } = useAuth()
  const t = useT()
  const queryClient = useQueryClient()
  const [inviting, setInviting] = useState(false)
  const [blocking, setBlocking] = useState<User | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const block = useMutation({
    mutationFn: (user: User) => (user.status === 'blocked' ? usersApi.unblock(user.id) : usersApi.block(user.id)),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['users'] })
      setBlocking(null)
      setActionError(null)
    },
    onError: (error) => {
      setBlocking(null)
      setActionError(errorMessage(error))
    },
  })

  const revokeSessions = useMutation({
    mutationFn: (id: string) => usersApi.revokeSessions(id),
    onSuccess: () => setActionError(null),
    onError: (error) => setActionError(errorMessage(error)),
  })

  const columns: Column<User>[] = [
    {
      key: 'name',
      header: t('users.columnUser'),
      render: (row) => (
        <div>
          <div className="table__primary">
            {row.display_name}
            {row.is_platform_owner ? (
              <Badge tone="accent">{t('users.platformOwner')}</Badge>
            ) : null}
          </div>
          <div className="table__secondary">{row.email}</div>
        </div>
      ),
    },
    { key: 'status', header: t('users.columnStatus'), render: (row) => <StatusBadge status={row.status} /> },
    {
      key: 'mfa',
      header: t('users.columnMfa'),
      render: (row) =>
        row.mfa_enabled ? (
          <Badge tone="success">{t('users.mfaOn')}</Badge>
        ) : (
          <Badge tone="warning">{t('users.mfaOff')}</Badge>
        ),
    },
    {
      key: 'lastLogin',
      header: t('users.columnLastSignIn'),
      render: (row) => <Timestamp value={row.last_login_at} />,
    },
    {
      key: 'actions',
      header: '',
      width: '220px',
      render: (row) => (
        <div className="row">
          {can('users.manage') ? (
            <button
              type="button"
              className="btn btn--ghost btn--sm"
              onClick={(event) => {
                event.stopPropagation()
                revokeSessions.mutate(row.id)
              }}
              title={t('users.revokeTitle')}
            >
              {t('users.revokeSessions')}
            </button>
          ) : null}
          {can('users.manage') ? (
            <button
              type="button"
              className={row.status === 'blocked' ? 'btn btn--secondary btn--sm' : 'btn btn--danger btn--sm'}
              onClick={(event) => {
                event.stopPropagation()
                setBlocking(row)
              }}
            >
              {row.status === 'blocked' ? t('users.unblock') : t('users.block')}
            </button>
          ) : null}
        </div>
      ),
    },
  ]

  return (
    <>
      <PageHeader title={t('users.title')} meta={t('users.meta')} />

      {actionError ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="error">{actionError}</Alert>
        </div>
      ) : null}

      <ResourceList
        title={t('users.allUsers')}
        queryKey={['users']}
        fetchPage={(params) => usersApi.list(params)}
        columns={columns}
        rowKey={(row) => row.id}
        emptyTitle={t('users.emptyTitle')}
        emptyText={t('users.emptyText')}
        searchable
        searchPlaceholder={t('users.searchPlaceholder')}
        actions={
          can('users.manage') ? (
            <button type="button" className="btn btn--primary btn--sm" onClick={() => setInviting(true)}>
              {t('users.invite')}
            </button>
          ) : null
        }
      />

      <InviteUserDialog
        open={inviting}
        onClose={() => setInviting(false)}
        onInvited={() => {
          setInviting(false)
          void queryClient.invalidateQueries({ queryKey: ['users'] })
        }}
      />

      <ConfirmDialog
        open={blocking !== null}
        title={
          blocking?.status === 'blocked' ? t('users.unblockTitle') : t('users.blockTitle')
        }
        text={
          blocking?.status === 'blocked'
            ? t('users.unblockText')
            : t('users.blockText')
        }
        confirmLabel={blocking?.status === 'blocked' ? t('users.unblock') : t('users.block')}
        danger={blocking?.status !== 'blocked'}
        busy={block.isPending}
        onConfirm={() => blocking && block.mutate(blocking)}
        onCancel={() => setBlocking(null)}
      />
    </>
  )
}

function InviteUserDialog({
  open,
  onClose,
  onInvited,
}: {
  open: boolean
  onClose: () => void
  onInvited: () => void
}) {
  const t = useT()
  const [email, setEmail] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [roleKeys, setRoleKeys] = useState<string[]>([])
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)

  const roles = useQuery({ queryKey: ['roles'], queryFn: () => rolesApi.list(), enabled: open })

  const mutation = useMutation({
    mutationFn: () => usersApi.invite({ email, display_name: displayName, role_keys: roleKeys }),
    onSuccess: onInvited,
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
      title={t('users.inviteTitle')}
      footer={<>
        <button type="button" className="btn btn--secondary" onClick={onClose}>
          {t('common.cancel')}
        </button>
        <button type="submit" className="btn btn--primary" disabled={mutation.isPending}>
          {mutation.isPending ? t('users.inviting') : t('users.sendInvitation')}
        </button>
      </>}
      formProps={{ onSubmit: handleSubmit }}
    >
      <div className="stack">
        {formError ? <Alert tone="error">{formError}</Alert> : null}

        <Field label={t('common.email')} htmlFor="invite-email" required error={errors.email}>
          <input
            id="invite-email"
            type="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            required
          />
        </Field>

        <Field label={t('users.displayName')} htmlFor="invite-name" required error={errors.display_name}>
          <input
            id="invite-name"
            value={displayName}
            onChange={(event) => setDisplayName(event.target.value)}
            required
          />
        </Field>

        <fieldset style={{ border: 'none', padding: 0, margin: 0 }}>
          <legend className="field__label" style={{ marginBottom: 'var(--space-2)' }}>
            {t('users.roles')}
            </legend>
          <div className="stack stack--sm" style={{ maxHeight: 200, overflowY: 'auto' }}>
            {(roles.data?.items ?? []).map((role) => (
              <label key={role.key} className="checkbox">
                <input
                  type="checkbox"
                  checked={roleKeys.includes(role.key)}
                  onChange={(event) =>
                    setRoleKeys((previous) =>
                      event.target.checked
                        ? [...previous, role.key]
                        : previous.filter((key) => key !== role.key),
                    )
                  }
                />
                <span>
                  {role.name} <span className="text-subtle mono">{role.key}</span>
                </span>
              </label>
            ))}
          </div>
          <span className="field__hint">
            {t('users.rolesHint')}
          </span>
        </fieldset>
      </div>
    </Modal>
  )
}
