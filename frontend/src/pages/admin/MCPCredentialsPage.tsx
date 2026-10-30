/**
 * MCP client credentials.
 *
 * An external MCP client authenticates with a credential rather than a browser
 * session, which a long-running client cannot hold and an operator cannot revoke
 * without ending the person's own login.
 *
 * The token is shown exactly once, at creation or rotation, and is never
 * returned again. The page makes that explicit and offers a copy button, because
 * an operator who closes the dialog without copying the token has to rotate it to
 * get a new one. The list reports only a non-secret prefix.
 *
 * A credential is always narrower than its owner: the permission picker is the
 * whole point, and the server refuses any permission the operator does not hold,
 * so the UI cannot be used to grant more than the operator has.
 */
import { useMemo, useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  mcpCredentialsApi,
  type MCPCredential,
  type MCPCredentialCreated,
  type MCPCredentialInput,
} from '../../api/mcpCredentials'
import { rolesApi } from '../../api/admin'
import { errorMessage, fieldErrors } from '../../api/client'
import { useAuth } from '../../hooks/useAuth'
import {
  Alert,
  Badge,
  Card,
  ConfirmDialog,
  ErrorState,
  Field,
  PageHeader,
  TableSkeleton,
  Timestamp,
} from '../../components/ui'

const EXPIRY_OPTIONS = [
  { value: 0, label: 'Never expires' },
  { value: 24, label: '24 hours' },
  { value: 24 * 7, label: '7 days' },
  { value: 24 * 30, label: '30 days' },
  { value: 24 * 90, label: '90 days' },
]

function statusTone(status: string): 'success' | 'neutral' | 'danger' {
  if (status === 'active') return 'success'
  if (status === 'revoked') return 'danger'
  return 'neutral'
}

export function MCPCredentialsPage() {
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [rotating, setRotating] = useState<MCPCredential | null>(null)
  const [revoking, setRevoking] = useState<MCPCredential | null>(null)
  const [revealed, setRevealed] = useState<MCPCredentialCreated | null>(null)

  const credentials = useQuery({
    queryKey: ['mcp', 'credentials'],
    queryFn: () => mcpCredentialsApi.list(),
  })

  const mayManage = can('mcp.credentials.manage')
  const invalidate = () => void queryClient.invalidateQueries({ queryKey: ['mcp', 'credentials'] })

  const revoke = useMutation({
    mutationFn: (id: string) => mcpCredentialsApi.revoke(id),
    onSuccess: () => {
      setRevoking(null)
      invalidate()
    },
  })

  if (credentials.isError) {
    return (
      <Card title="MCP client credentials">
        <ErrorState
          message={errorMessage(credentials.error)}
          onRetry={() => void credentials.refetch()}
        />
      </Card>
    )
  }

  const items = credentials.data?.items ?? []

  return (
    <>
      <PageHeader
        title="MCP client credentials"
        meta="Scoped, revocable tokens that let an external MCP client reach the CRM without a browser session."
      />

      <Card title="Connecting a client">
        <p className="text-muted text-sm">
          Send the token in an <code className="mono">Authorization: Bearer …</code> header to{' '}
          <code className="mono">/api/v1/mcp</code>. A credential acts only within the permissions
          you grant it, never the full authority of your account, so a read-only token cannot change
          CRM data even if you are an administrator.
        </p>
      </Card>

      <Card
        title="Credentials"
        flush
        actions={
          mayManage ? (
            <button
              type="button"
              className="btn btn--primary btn--sm"
              onClick={() => setCreating(true)}
            >
              New credential
            </button>
          ) : null
        }
      >
        {credentials.isLoading ? (
          <TableSkeleton rows={3} columns={5} />
        ) : items.length ? (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Token</th>
                  <th>Scope</th>
                  <th>Status</th>
                  <th>Last used</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {items.map((credential) => (
                  <tr key={credential.id}>
                    <td className="table__primary">
                      <div>{credential.name}</div>
                      <div className="table__secondary text-sm">
                        <Timestamp value={credential.created_at} />
                      </div>
                    </td>
                    <td className="mono text-sm">{credential.token_prefix}…</td>
                    <td>
                      <Badge tone="accent">
                        {credential.permissions.length} permission
                        {credential.permissions.length === 1 ? '' : 's'}
                      </Badge>
                    </td>
                    <td>
                      <Badge tone={statusTone(credential.status)}>{credential.status}</Badge>
                      {credential.expires_at ? (
                        <div className="table__secondary text-sm">
                          expires <Timestamp value={credential.expires_at} />
                        </div>
                      ) : null}
                    </td>
                    <td className="text-sm">
                      {credential.last_used_at ? (
                        <Timestamp value={credential.last_used_at} />
                      ) : (
                        <span className="text-subtle">never</span>
                      )}
                    </td>
                    <td>
                      {mayManage ? (
                        <div className="row">
                          <button
                            type="button"
                            className="btn btn--secondary btn--sm"
                            onClick={() => setRotating(credential)}
                          >
                            Rotate
                          </button>
                          <button
                            type="button"
                            className="btn btn--ghost btn--sm"
                            onClick={() => setRevoking(credential)}
                            disabled={credential.status === 'revoked'}
                          >
                            Revoke
                          </button>
                        </div>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="card__body">
            <p className="text-muted">
              No credentials yet. A session cookie still works for this interface; a credential is
              what lets an external client connect.
            </p>
          </div>
        )}
      </Card>

      {creating ? (
        <CredentialDialog
          onClose={() => setCreating(false)}
          onSaved={(created) => {
            setCreating(false)
            invalidate()
            setRevealed(created)
          }}
        />
      ) : null}

      <RotateDialog
        credential={rotating}
        onClose={() => setRotating(null)}
        onSaved={(created) => {
          setRotating(null)
          invalidate()
          setRevealed(created)
        }}
      />

      {revealed ? (
        <TokenDialog created={revealed} onClose={() => setRevealed(null)} />
      ) : null}

      <ConfirmDialog
        open={revoking !== null}
        title="Revoke this credential?"
        text={`"${revoking?.name ?? ''}" will stop working immediately. Any client using it is disconnected until it is given a new token. The row is kept for the audit trail.`}
        confirmLabel="Revoke"
        danger
        busy={revoke.isPending}
        onCancel={() => setRevoking(null)}
        onConfirm={() => {
          if (revoking) revoke.mutate(revoking.id)
        }}
      />
    </>
  )
}

/** The create form. */
function CredentialDialog({
  onClose,
  onSaved,
}: {
  onClose: () => void
  onSaved: (created: MCPCredentialCreated) => void
}) {
  const [name, setName] = useState('')
  const [expiresInHours, setExpiresInHours] = useState(0)
  const [selected, setSelected] = useState<string[]>([])
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)

  const permissions = useQuery({
    queryKey: ['permissions'],
    queryFn: () => rolesApi.permissions(),
  })

  // Group permission keys by their prefix so the picker reads as areas rather
  // than an undifferentiated wall of strings.
  const grouped = useMemo(() => {
    const map = new Map<string, { key: string; description?: string }[]>()
    for (const permission of permissions.data?.items ?? []) {
      const group = permission.key.split('.')[0]
      const list = map.get(group) ?? []
      list.push(permission)
      map.set(group, list)
    }
    return map
  }, [permissions.data])

  const mutation = useMutation({
    mutationFn: (input: MCPCredentialInput) => mcpCredentialsApi.create(input),
    onSuccess: onSaved,
    onError: (error) => {
      setErrors(fieldErrors(error))
      setFormError(errorMessage(error))
    },
  })

  function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setFormError(null)
    setErrors({})
    const input: MCPCredentialInput = {
      name: name.trim(),
      permissions: selected,
    }
    if (expiresInHours > 0) input.expires_in_hours = expiresInHours
    mutation.mutate(input)
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="credential-title"
      style={{
        position: 'fixed',
        inset: 0,
        display: 'grid',
        placeItems: 'center',
        background: 'rgba(16, 24, 40, 0.45)',
        zIndex: 60,
        padding: 'var(--space-4)',
        overflowY: 'auto',
      }}
      onClick={onClose}
    >
      <form
        className="card"
        style={{ maxWidth: 640, width: '100%', margin: 'auto' }}
        onClick={(event) => event.stopPropagation()}
        onSubmit={handleSubmit}
      >
        <div className="card__header">
          <h2 id="credential-title">New MCP credential</h2>
        </div>
        <div className="card__body stack">
          {formError ? <Alert tone="error">{formError}</Alert> : null}

          <div className="form-grid">
            <Field
              label="Name"
              htmlFor="credential-name"
              required
              error={errors.name}
              hint="A label to tell this client apart, for example Laptop agent"
            >
              <input
                id="credential-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                autoFocus
              />
            </Field>
            <Field label="Expiry" htmlFor="credential-expiry" hint="A shorter life limits a leaked token">
              <select
                id="credential-expiry"
                value={expiresInHours}
                onChange={(event) => setExpiresInHours(Number(event.target.value))}
              >
                {EXPIRY_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </Field>
          </div>

          <div>
            <div className="row" style={{ justifyContent: 'space-between' }}>
              <span className="field__label">Permissions</span>
              <Badge tone="accent">{selected.length} selected</Badge>
            </div>
            <p className="field__hint">
              The credential can do only what is checked here. You cannot grant a permission you do
              not hold yourself.
            </p>
            {errors.permissions ? (
              <span className="field__error" role="alert">
                {errors.permissions}
              </span>
            ) : null}
          </div>

          {permissions.isLoading ? (
            <TableSkeleton rows={3} columns={3} />
          ) : (
            Array.from(grouped.entries()).map(([group, entries]) => (
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
                  {entries.map((permission) => (
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
            ))
          )}
        </div>
        <div className="form-actions" style={{ margin: 0, padding: 'var(--space-4)' }}>
          <button type="button" className="btn btn--secondary" onClick={onClose}>
            Cancel
          </button>
          <button
            type="button"
            className="btn btn--primary"
            disabled={mutation.isPending}
            onClick={handleSubmit}
          >
            {mutation.isPending ? 'Creating…' : 'Create credential'}
          </button>
        </div>
      </form>
    </div>
  )
}

/** The rotate confirmation, which reveals a fresh token. */
function RotateDialog({
  credential,
  onClose,
  onSaved,
}: {
  credential: MCPCredential | null
  onClose: () => void
  onSaved: (created: MCPCredentialCreated) => void
}) {
  const mutation = useMutation({
    mutationFn: (id: string) => mcpCredentialsApi.rotate(id),
    onSuccess: onSaved,
  })

  if (!credential) return null

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="rotate-title"
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
      <div
        className="card"
        style={{ maxWidth: 460, width: '100%' }}
        onClick={(event) => event.stopPropagation()}
      >
        <div className="card__header">
          <h2 id="rotate-title">Rotate this credential?</h2>
        </div>
        <div className="card__body stack">
          <p className="text-muted">
            A new token is issued for <strong>{credential.name}</strong> with the same permissions.
            The current token stops working the instant the new one is issued, so update the client
            right away.
          </p>
          {mutation.isError ? <Alert tone="error">{errorMessage(mutation.error)}</Alert> : null}
        </div>
        <div className="form-actions" style={{ margin: 0, padding: 'var(--space-4)' }}>
          <button type="button" className="btn btn--secondary" onClick={onClose}>
            Cancel
          </button>
          <button
            type="button"
            className="btn btn--primary"
            disabled={mutation.isPending}
            onClick={() => mutation.mutate(credential.id)}
          >
            {mutation.isPending ? 'Rotating…' : 'Rotate'}
          </button>
        </div>
      </div>
    </div>
  )
}

/** The one-time token reveal. */
function TokenDialog({
  created,
  onClose,
}: {
  created: MCPCredentialCreated
  onClose: () => void
}) {
  const [copied, setCopied] = useState(false)

  async function copy() {
    try {
      await navigator.clipboard.writeText(created.token)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="token-title"
      style={{
        position: 'fixed',
        inset: 0,
        display: 'grid',
        placeItems: 'center',
        background: 'rgba(16, 24, 40, 0.45)',
        zIndex: 70,
        padding: 'var(--space-4)',
      }}
    >
      <div className="card" style={{ maxWidth: 560, width: '100%' }}>
        <div className="card__header">
          <h2 id="token-title">Copy this token now</h2>
        </div>
        <div className="card__body stack">
          <Alert tone="warning">
            This is the only time the token is shown. It is stored hashed and cannot be recovered — if
            you lose it, rotate the credential to get a new one.
          </Alert>
          <div
            className="mono"
            style={{
              wordBreak: 'break-all',
              padding: 'var(--space-3)',
              background: 'var(--surface-sunken, #f4f4f5)',
              borderRadius: 6,
            }}
          >
            {created.token}
          </div>
          <p className="text-muted text-sm">
            Credential <strong>{created.credential.name}</strong> carries{' '}
            {created.credential.permissions.length} permission
            {created.credential.permissions.length === 1 ? '' : 's'}.
          </p>
        </div>
        <div className="form-actions" style={{ margin: 0, padding: 'var(--space-4)' }}>
          <button type="button" className="btn btn--secondary" onClick={() => void copy()}>
            {copied ? 'Copied' : 'Copy token'}
          </button>
          <button type="button" className="btn btn--primary" onClick={onClose}>
            Done
          </button>
        </div>
      </div>
    </div>
  )
}
