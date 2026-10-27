import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { authApi } from '../api/auth'
import { errorMessage, fieldErrors } from '../api/client'
import { useAuth } from '../hooks/useAuth'
import { Alert, Badge, Card, Field, PageHeader } from '../components/ui'

export function AccountPage() {
  const { session } = useAuth()
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)
  const [changed, setChanged] = useState(false)

  const changePassword = useMutation({
    mutationFn: () => authApi.changePassword(currentPassword, newPassword),
    onSuccess: () => {
      setChanged(true)
      setCurrentPassword('')
      setNewPassword('')
      setConfirmPassword('')
      setErrors({})
      setFormError(null)
    },
    onError: (error) => {
      setErrors(fieldErrors(error))
      setFormError(errorMessage(error))
    },
  })

  function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setFormError(null)
    setErrors({})
    setChanged(false)
    if (newPassword !== confirmPassword) {
      setErrors({ confirm_password: 'The two passwords do not match.' })
      return
    }
    changePassword.mutate()
  }

  return (
    <>
      <PageHeader title="Account" meta="Your profile, permissions and password." />

      <div className="grid grid--split">
        <Card title="Profile">
          <dl className="dl">
            <dt>Name</dt>
            <dd>{session?.user.display_name}</dd>
            <dt>Email</dt>
            <dd>{session?.user.email}</dd>
            <dt>Organization</dt>
            <dd>{session?.organization.name}</dd>
            <dt>Second factor</dt>
            <dd>
              {session?.user.mfa_enabled ? (
                <Badge tone="success">enabled</Badge>
              ) : (
                <Badge tone="warning">not enabled</Badge>
              )}
            </dd>
            <dt>Teams</dt>
            <dd>
              {session?.teams.length ? (
                <div className="row row--wrap">
                  {session.teams.map((team) => (
                    <Badge key={team} tone="neutral">
                      {team}
                    </Badge>
                  ))}
                </div>
              ) : (
                <span className="text-subtle">None</span>
              )}
            </dd>
          </dl>
        </Card>

        <Card title="Permissions">
          <p className="text-muted text-sm" style={{ marginBottom: 'var(--space-3)' }}>
            These are the permissions your account currently holds. They are enforced on the server
            for every request, not only by what the interface shows.
          </p>
          <div className="row row--wrap">
            {(session?.permissions ?? []).map((permission) => (
              <Badge key={permission} tone="neutral">
                {permission}
              </Badge>
            ))}
          </div>
        </Card>
      </div>

      <div style={{ marginTop: 'var(--space-5)' }}>
        <Card title="Change password">
          <form onSubmit={handleSubmit}>
            <div className="stack" style={{ maxWidth: 420 }}>
              {formError ? <Alert tone="error">{formError}</Alert> : null}
              {changed ? <Alert tone="success">Your password has been changed.</Alert> : null}

              <Field
                label="Current password"
                htmlFor="current-password"
                required
                error={errors.current_password}
              >
                <input
                  id="current-password"
                  type="password"
                  autoComplete="current-password"
                  value={currentPassword}
                  onChange={(event) => setCurrentPassword(event.target.value)}
                  required
                />
              </Field>

              <Field
                label="New password"
                htmlFor="new-password"
                required
                error={errors.new_password}
                hint="At least 12 characters. A longer passphrase is stronger than a short complex one."
              >
                <input
                  id="new-password"
                  type="password"
                  autoComplete="new-password"
                  value={newPassword}
                  onChange={(event) => setNewPassword(event.target.value)}
                  required
                />
              </Field>

              <Field
                label="Confirm new password"
                htmlFor="confirm-password"
                required
                error={errors.confirm_password}
              >
                <input
                  id="confirm-password"
                  type="password"
                  autoComplete="new-password"
                  value={confirmPassword}
                  onChange={(event) => setConfirmPassword(event.target.value)}
                  required
                />
              </Field>
            </div>

            <div className="form-actions">
              <button type="submit" className="btn btn--primary" disabled={changePassword.isPending}>
                {changePassword.isPending ? 'Changing…' : 'Change password'}
              </button>
            </div>
          </form>
        </Card>
      </div>
    </>
  )
}
