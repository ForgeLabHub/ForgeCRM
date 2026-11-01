import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { rolesApi } from '../../api/admin'
import { errorMessage } from '../../api/client'
import type { Role } from '../../types/api'
import { useAuth } from '../../hooks/useAuth'
import {
  Alert,
  Badge,
  Card,
  ConfirmDialog,
  ErrorState,
  PageHeader,
  TableSkeleton,
} from '../../components/ui'

export function RolesPage() {
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<Role | null>(null)
  const [saving, setSaving] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)

  const roles = useQuery({ queryKey: ['roles'], queryFn: () => rolesApi.list() })
  const permissions = useQuery({
    queryKey: ['permissions'],
    queryFn: () => rolesApi.permissions(),
  })

  const setPermissions = useMutation({
    mutationFn: ({ id, keys }: { id: string; keys: string[] }) => rolesApi.setPermissions(id, keys),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['roles'] })
      setEditing(null)
      setSaving(false)
      setActionError(null)
    },
    onError: (error) => {
      setSaving(false)
      setActionError(errorMessage(error))
    },
  })

  // Group permission keys by their prefix so the list reads as areas rather
  // than as an undifferentiated wall of strings.
  const grouped = new Map<string, { key: string; description?: string }[]>()
  for (const permission of permissions.data?.items ?? []) {
    const group = permission.key.split('.')[0]
    const list = grouped.get(group) ?? []
    list.push(permission)
    grouped.set(group, list)
  }

  if (roles.isError) {
    return (
      <Card title="Roles">
        <ErrorState message={errorMessage(roles.error)} onRetry={() => void roles.refetch()} />
      </Card>
    )
  }

  return (
    <>
      <PageHeader
        title="Roles & permissions"
        meta="A role is a named set of permissions. Changing a role affects every user who holds it."
      />

      {actionError ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="error">{actionError}</Alert>
        </div>
      ) : null}

      {roles.isLoading ? (
        <TableSkeleton rows={6} columns={3} />
      ) : (
        <div className="stack">
          {(roles.data?.items ?? []).map((role) => (
            <Card
              key={role.id}
              title={role.name}
              actions={
                <div className="row">
                  <Badge tone="neutral" >{role.key}</Badge>
                  <Badge tone="accent">{role.permissions.length} permissions</Badge>
                  {role.is_system ? (
                    <Badge tone="warning">built-in</Badge>
                  ) : null}
                  {can('roles.manage') ? (
                    <button
                      type="button"
                      className="btn btn--secondary btn--sm"
                      onClick={() => setEditing(role)}
                    >
                      Edit permissions
                    </button>
                  ) : null}
                </div>
              }
            >
              {role.description ? (
                <p className="text-muted text-sm" style={{ marginBottom: 'var(--space-3)' }}>
                  {role.description}
                </p>
              ) : null}
              <div className="row row--wrap">
                {role.permissions.length === 0 ? (
                  <span className="text-subtle text-sm">No permissions</span>
                ) : (
                  role.permissions.map((permission) => (
                    <Badge key={permission} tone="neutral">
                      {permission}
                    </Badge>
                  ))
                )}
              </div>
            </Card>
          ))}
        </div>
      )}

      {editing ? (
        <PermissionEditor
          role={editing}
          groups={grouped}
          busy={saving || setPermissions.isPending}
          onClose={() => setEditing(null)}
          onSave={(keys) => {
            setSaving(true)
            setPermissions.mutate({ id: editing.id, keys })
          }}
        />
      ) : null}
    </>
  )
}

function PermissionEditor({
  role,
  groups,
  busy,
  onClose,
  onSave,
}: {
  role: Role
  groups: Map<string, { key: string; description?: string }[]>
  busy: boolean
  onClose: () => void
  onSave: (keys: string[]) => void
}) {
  const [selected, setSelected] = useState<string[]>(role.permissions)
  const [confirming, setConfirming] = useState(false)

  const dirty =
    selected.length !== role.permissions.length ||
    selected.some((key) => !role.permissions.includes(key))

  const removed = role.permissions.filter((key) => !selected.includes(key))

  return (
    <>
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="permission-editor-title"
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
        <div className="card" style={{ maxWidth: 760, margin: '0 auto' }} onClick={(e) => e.stopPropagation()}>
          <div className="card__header">
            <h2 id="permission-editor-title">Permissions for {role.name}</h2>
            <Badge tone="accent">{selected.length} selected</Badge>
          </div>
          <div className="card__body stack">
            <Alert tone="warning">
              Removing a permission takes effect on the next request made by every user holding this
              role. It does not wait for them to sign in again.
            </Alert>

            {Array.from(groups.entries()).map(([group, items]) => (
              <fieldset key={group} style={{ border: 'none', padding: 0, margin: 0 }}>
                <legend
                  className="field__label"
                  style={{ textTransform: 'capitalize', marginBottom: 'var(--space-2)' }}
                >
                  {group}
                </legend>
                <div
                  style={{
                    display: 'grid',
                    gridTemplateColumns: 'repeat(auto-fill, minmax(240px, 1fr))',
                    gap: 'var(--space-2)',
                  }}
                >
                  {items.map((permission) => (
                    <label key={permission.key} className="checkbox">
                      <input
                        type="checkbox"
                        checked={selected.includes(permission.key)}
                        onChange={(event) =>
                          setSelected((previous) =>
                            event.target.checked
                              ? [...previous, permission.key]
                              : previous.filter((key) => key !== permission.key),
                          )
                        }
                      />
                      <span className="mono" title={permission.description}>
                        {permission.key}
                      </span>
                    </label>
                  ))}
                </div>
              </fieldset>
            ))}
          </div>
          <div className="form-actions" style={{ margin: 0, padding: 'var(--space-4)' }}>
            <button type="button" className="btn btn--secondary" onClick={onClose} disabled={busy}>
              Cancel
            </button>
            <button
              type="button"
              className="btn btn--primary"
              disabled={busy || !dirty}
              onClick={() => (removed.length > 0 ? setConfirming(true) : onSave(selected))}
            >
              {busy ? 'Saving…' : 'Save permissions'}
            </button>
          </div>
        </div>
      </div>

      <ConfirmDialog
        open={confirming}
        title="Remove permissions?"
        text={`This removes ${removed.length} permission${removed.length === 1 ? '' : 's'} from ${role.name}: ${removed.join(', ')}. Users holding this role lose that access immediately.`}
        confirmLabel="Save changes"
        danger
        busy={busy}
        onConfirm={() => {
          setConfirming(false)
          onSave(selected)
        }}
        onCancel={() => setConfirming(false)}
      />
    </>
  )
}
