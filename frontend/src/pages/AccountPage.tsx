import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { authApi } from '../api/auth'
import { errorMessage, fieldErrors } from '../api/client'
import { useAuth } from '../hooks/useAuth'
import { useT } from '../i18n'
import { Alert, Badge, Card, Field, PageHeader } from '../components/ui'

export function AccountPage() {
  const { session } = useAuth()
  const t = useT()
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
      setErrors({ confirm_password: t('profile.mismatch') })
      return
    }
    changePassword.mutate()
  }

  return (
    <>
      <PageHeader title={t('profile.title')} meta={t('profile.meta')} />

      <div className="grid grid--split">
        <Card title={t('profile.card')}>
          <dl className="dl">
            <dt>{t('fields.name')}</dt>
            <dd>{session?.user.display_name}</dd>
            <dt>{t('fields.email')}</dt>
            <dd>{session?.user.email}</dd>
            <dt>{t('profile.organization')}</dt>
            <dd>{session?.organization.name}</dd>
            <dt>{t('profile.secondFactor')}</dt>
            <dd>
              {session?.user.mfa_enabled ? (
                <Badge tone="success">{t('profile.enabled')}</Badge>
              ) : (
                <Badge tone="warning">{t('profile.notEnabled')}</Badge>
              )}
            </dd>
            <dt>{t('profile.teams')}</dt>
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
                <span className="text-subtle">{t('profile.none')}</span>
              )}
            </dd>
          </dl>
        </Card>

        <Card title={t('profile.permissions')}>
          <p className="text-muted text-sm" style={{ marginBottom: 'var(--space-3)' }}>
            {t('profile.permissionsHint')}
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
        <Card title={t('profile.changePassword')}>
          <form onSubmit={handleSubmit}>
            <div className="stack" style={{ maxWidth: 420 }}>
              {formError ? <Alert tone="error">{formError}</Alert> : null}
              {changed ? <Alert tone="success">{t('profile.changed')}</Alert> : null}

              <Field
                label={t('profile.currentPassword')}
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
                label={t('profile.newPassword')}
                htmlFor="new-password"
                required
                error={errors.new_password}
                hint={t('profile.passwordHint')}
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
                label={t('profile.confirmPassword')}
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
                {changePassword.isPending ? t('profile.changing') : t('profile.changePassword')}
              </button>
            </div>
          </form>
        </Card>
      </div>
    </>
  )
}
