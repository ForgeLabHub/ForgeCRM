import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { usersApi } from '../../api/auth'
import { rolesApi } from '../../api/admin'
import { errorMessage, fieldErrors } from '../../api/client'
import type { User } from '../../types/api'
import { useAuth } from '../../hooks/useAuth'
import { ResourceList, type Column } from '../../components/ResourceList'
import {
  Alert,
  Badge,
  ConfirmDialog,
  Field,
  PageHeader,
  StatusBadge,
  Timestamp,
} from '../../components/ui'

export function UsersPage() {
  const { can } = useAuth()
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
      header: 'User',
      render: (row) => (
        <div>
          <div className="table__primary">
            {row.display_name}
            {row.is_platform_owner ? <Badge tone="accent">platform owner</Badge> : null}
          </div>
          <div className="table__secondary">{row.email}</div>
        </div>
      ),
    },
    { key: 'status', header: 'Status', render: (row) => <StatusBadge status={row.status} /> },
    {
      key: 'mfa',
      header: 'MFA',
      render: (row) => (row.mfa_enabled ? <Badge tone="success">on</Badge> : <Badge tone="warning">off</Badge>),
    },
    {
      key: 'lastLogin',
      header: 'Last sign-in',
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
              title="Sign this user out everywhere"
            >
              Revoke sessions
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
              {row.status === 'blocked' ? 'Unblock' : 'Block'}
            </button>
          ) : null}
        </div>
      ),
    },
  ]

  return (
    <>
      <PageHeader
        title="Users"
        meta="People who can sign in, their status and their second factor."
      />

      {actionError ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="error">{actionError}</Alert>
        </div>
      ) : null}

      <ResourceList
        title="All users"
        queryKey={['users']}
        fetchPage={(params) => usersApi.list(params)}
        columns={columns}
        rowKey={(row) => row.id}
        emptyTitle="No users"
        emptyText="Invite a colleague to give them access."
        searchable
        searchPlaceholder="Search by name or email…"
        actions={
          can('users.manage') ? (
            <button type="button" className="btn btn--primary btn--sm" onClick={() => setInviting(true)}>
              Invite user
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
        title={blocking?.status === 'blocked' ? 'Unblock this user?' : 'Block this user?'}
        text={
          blocking?.status === 'blocked'
            ? 'They will be able to sign in again.'
            : 'Their sessions stop working immediately, not when the session would have expired. This takes effect on the next request they make.'
        }
        confirmLabel={blocking?.status === 'blocked' ? 'Unblock' : 'Block'}
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
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="invite-user-title"
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
      <div className="card" style={{ maxWidth: 520, margin: '0 auto' }} onClick={(e) => e.stopPropagation()}>
        <div className="card__header">
          <h2 id="invite-user-title">Invite a user</h2>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="card__body stack">
            {formError ? <Alert tone="error">{formError}</Alert> : null}

            <Field label="Email" htmlFor="invite-email" required error={errors.email}>
              <input
                id="invite-email"
                type="email"
                value={email}
                onChange={(event) => setEmail(event.target.value)}
                required
              />
            </Field>

            <Field label="Display name" htmlFor="invite-name" required error={errors.display_name}>
              <input
                id="invite-name"
                value={displayName}
                onChange={(event) => setDisplayName(event.target.value)}
                required
              />
            </Field>

            <fieldset style={{ border: 'none', padding: 0, margin: 0 }}>
              <legend className="field__label" style={{ marginBottom: 'var(--space-2)' }}>
                Roles
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
                Roles are starting points: they grant a set of permissions you can adjust later.
              </span>
            </fieldset>
          </div>
          <div className="form-actions">
            <button type="button" className="btn btn--secondary" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="btn btn--primary" disabled={mutation.isPending}>
              {mutation.isPending ? 'Inviting…' : 'Send invitation'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
