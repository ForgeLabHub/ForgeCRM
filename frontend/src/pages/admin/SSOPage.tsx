/**
 * Single sign-on provider administration.
 *
 * An operator registers an OpenID Connect provider here and copies the redirect
 * URL the page shows into that provider's configuration. The client secret is
 * write-only: the list reports whether one is stored and never returns it, so
 * editing a provider leaves the secret field blank and an empty field keeps the
 * stored value rather than clearing it. That is why the form says so, rather than
 * letting an operator believe a blank field erased the secret.
 */
import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ssoAdminApi, rolesApi } from '../../api/admin'
import type { SSOProviderInput, SSOProviderRow } from '../../api/admin'
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
} from '../../components/ui'

export function SSOPage() {
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<SSOProviderRow | null>(null)
  const [creating, setCreating] = useState(false)
  const [deleting, setDeleting] = useState<SSOProviderRow | null>(null)

  const providers = useQuery({ queryKey: ['sso', 'providers'], queryFn: () => ssoAdminApi.list() })

  const mayManage = can('sso.manage')

  const invalidate = () => void queryClient.invalidateQueries({ queryKey: ['sso', 'providers'] })

  const remove = useMutation({
    mutationFn: (id: string) => ssoAdminApi.remove(id),
    onSuccess: () => {
      setDeleting(null)
      invalidate()
    },
  })

  if (providers.isError) {
    return (
      <Card title="Single sign-on">
        <ErrorState
          message={errorMessage(providers.error)}
          onRetry={() => void providers.refetch()}
        />
      </Card>
    )
  }

  const items = providers.data?.items ?? []

  return (
    <>
      <PageHeader
        title="Single sign-on"
        meta="OpenID Connect providers users may sign in through."
      />

      <Card
        title="Providers"
        flush
        actions={
          mayManage ? (
            <button
              type="button"
              className="btn btn--primary btn--sm"
              onClick={() => setCreating(true)}
            >
              New provider
            </button>
          ) : null
        }
      >
        {providers.isLoading ? (
          <TableSkeleton rows={3} columns={4} />
        ) : items.length ? (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Issuer</th>
                  <th>Status</th>
                  <th>Secret</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {items.map((provider) => (
                  <tr key={provider.id}>
                    <td className="table__primary">
                      <div>{provider.display_name}</div>
                      <div className="table__secondary mono text-sm">{provider.key}</div>
                    </td>
                    <td className="mono text-sm truncate">{provider.issuer}</td>
                    <td>
                      {provider.enabled ? (
                        <Badge tone="success">enabled</Badge>
                      ) : (
                        <Badge tone="neutral">disabled</Badge>
                      )}
                    </td>
                    <td>
                      {provider.has_secret ? (
                        <Badge tone="success">stored</Badge>
                      ) : (
                        <Badge tone="warning">none</Badge>
                      )}
                    </td>
                    <td>
                      {mayManage ? (
                        <div className="row">
                          <button
                            type="button"
                            className="btn btn--secondary btn--sm"
                            onClick={() => setEditing(provider)}
                          >
                            Edit
                          </button>
                          <button
                            type="button"
                            className="btn btn--ghost btn--sm"
                            onClick={() => setDeleting(provider)}
                          >
                            Delete
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
              No identity providers are registered. Password sign-in still works.
            </p>
          </div>
        )}
      </Card>

      <Card title="Redirect URL">
        <p className="text-muted text-sm">
          Register this exact callback with each provider. It is built from the server's public
          base URL, so it is the value the provider must redirect back to.
        </p>
        <div className="mono text-sm" style={{ marginTop: 'var(--space-2)', wordBreak: 'break-all' }}>
          {providers.data?.redirect_url ?? '—'}
        </div>
      </Card>

      {creating || editing ? (
        <ProviderDialog
          provider={editing}
          onClose={() => {
            setCreating(false)
            setEditing(null)
          }}
          onSaved={() => {
            setCreating(false)
            setEditing(null)
            invalidate()
          }}
        />
      ) : null}

      <ConfirmDialog
        open={deleting !== null}
        title="Delete this provider?"
        text={`"${deleting?.display_name ?? ''}" will be removed. Accounts linked only through it keep their local password if they have one, and otherwise cannot sign in until an administrator resets it.`}
        confirmLabel="Delete"
        danger
        busy={remove.isPending}
        onCancel={() => setDeleting(null)}
        onConfirm={() => {
          if (deleting) remove.mutate(deleting.id)
        }}
      />
    </>
  )
}

/** The create and edit form. A nil provider means create. */
function ProviderDialog({
  provider,
  onClose,
  onSaved,
}: {
  provider: SSOProviderRow | null
  onClose: () => void
  onSaved: () => void
}) {
  const isEdit = provider !== null
  const [form, setForm] = useState({
    key: provider?.key ?? '',
    display_name: provider?.display_name ?? '',
    issuer: provider?.issuer ?? '',
    client_id: provider?.client_id ?? '',
    client_secret: '',
    scopes: (provider?.scopes ?? []).join(' '),
    allowed_email_domains: (provider?.allowed_email_domains ?? []).join(' '),
    jit_provisioning: provider?.jit_provisioning ?? false,
    default_role_key: provider?.default_role_key ?? '',
    enabled: provider?.enabled ?? true,
  })
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)

  // Roles are needed only when just-in-time provisioning is on, so the list is
  // fetched but never blocks the form.
  const roles = useQuery({
    queryKey: ['roles', 'for-select'],
    queryFn: () => rolesApi.list(),
  })

  function set<K extends keyof typeof form>(key: K, value: (typeof form)[K]) {
    setForm((prev) => ({ ...prev, [key]: value }))
  }

  const mutation = useMutation({
    mutationFn: (input: SSOProviderInput) =>
      isEdit ? ssoAdminApi.update(provider.id, input) : ssoAdminApi.create(input),
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
    const input: SSOProviderInput = {
      key: form.key.trim(),
      display_name: form.display_name.trim(),
      issuer: form.issuer.trim(),
      client_id: form.client_id.trim(),
      scopes: splitList(form.scopes),
      allowed_email_domains: splitList(form.allowed_email_domains),
      jit_provisioning: form.jit_provisioning,
      default_role_key: form.default_role_key.trim() || undefined,
      enabled: form.enabled,
    }
    // An empty secret on edit keeps the stored one; on create it is required and
    // the server rejects the request, so it is sent only when non-empty.
    if (form.client_secret) input.client_secret = form.client_secret
    mutation.mutate(input)
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="provider-title"
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
        style={{ maxWidth: 560, width: '100%', margin: 'auto' }}
        onClick={(event) => event.stopPropagation()}
        onSubmit={handleSubmit}
      >
        <div className="card__header">
          <h2 id="provider-title">{isEdit ? 'Edit provider' : 'New provider'}</h2>
        </div>
        <div className="card__body stack">
          {formError ? <Alert tone="error">{formError}</Alert> : null}

          <div className="form-grid">
            <Field label="Key" htmlFor="sso-key" required error={errors.key} hint="A short identifier, for example acme">
              <input
                id="sso-key"
                value={form.key}
                onChange={(event) => set('key', event.target.value)}
                disabled={isEdit}
                autoFocus
              />
            </Field>
            <Field label="Display name" htmlFor="sso-name" required error={errors.display_name}>
              <input
                id="sso-name"
                value={form.display_name}
                onChange={(event) => set('display_name', event.target.value)}
              />
            </Field>
          </div>

          <Field
            label="Issuer URL"
            htmlFor="sso-issuer"
            required
            error={errors.issuer}
            hint="The provider's base URL, whose /.well-known/openid-configuration is read"
          >
            <input
              id="sso-issuer"
              value={form.issuer}
              onChange={(event) => set('issuer', event.target.value)}
              placeholder="https://idp.example.com/realms/acme"
            />
          </Field>

          <Field label="Client ID" htmlFor="sso-client-id" required error={errors.client_id}>
            <input
              id="sso-client-id"
              value={form.client_id}
              onChange={(event) => set('client_id', event.target.value)}
            />
          </Field>

          <Field
            label="Client secret"
            htmlFor="sso-secret"
            error={errors.client_secret}
            hint={
              isEdit
                ? 'Leave blank to keep the stored secret'
                : 'Required for a confidential client; leave blank for a public client that uses PKCE only'
            }
          >
            <input
              id="sso-secret"
              type="password"
              value={form.client_secret}
              onChange={(event) => set('client_secret', event.target.value)}
              autoComplete="new-password"
            />
          </Field>

          <Field label="Scopes" htmlFor="sso-scopes" hint="Space-separated; defaults to openid profile email">
            <input
              id="sso-scopes"
              value={form.scopes}
              onChange={(event) => set('scopes', event.target.value)}
            />
          </Field>

          <Field
            label="Allowed email domains"
            htmlFor="sso-domains"
            hint="Space-separated. Empty accepts any domain the provider vouches for"
          >
            <input
              id="sso-domains"
              value={form.allowed_email_domains}
              onChange={(event) => set('allowed_email_domains', event.target.value)}
            />
          </Field>

          <label className="row" style={{ gap: 'var(--space-2)' }}>
            <input
              type="checkbox"
              checked={form.jit_provisioning}
              onChange={(event) => set('jit_provisioning', event.target.checked)}
            />
            <span>Create an account on first sign-in</span>
          </label>

          {form.jit_provisioning ? (
            <Field
              label="Default role for new accounts"
              htmlFor="sso-role"
              hint="The role a just-in-time account is granted"
            >
              <select
                id="sso-role"
                value={form.default_role_key}
                onChange={(event) => set('default_role_key', event.target.value)}
              >
                <option value="">Select a role</option>
                {(roles.data?.items ?? []).map((role) => (
                  <option key={role.id} value={role.key}>
                    {role.name}
                  </option>
                ))}
              </select>
            </Field>
          ) : null}

          <label className="row" style={{ gap: 'var(--space-2)' }}>
            <input
              type="checkbox"
              checked={form.enabled}
              onChange={(event) => set('enabled', event.target.checked)}
            />
            <span>Enabled</span>
          </label>
        </div>
        <div className="form-actions" style={{ margin: 0, padding: 'var(--space-4)' }}>
          <button type="button" className="btn btn--secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="btn btn--primary" disabled={mutation.isPending}>
            {mutation.isPending ? 'Saving…' : 'Save'}
          </button>
        </div>
      </form>
    </div>
  )
}

/** Splits a whitespace-separated field into a list, dropping empties. */
function splitList(value: string): string[] {
  return value
    .split(/\s+/)
    .map((part) => part.trim())
    .filter(Boolean)
}
