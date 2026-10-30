import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Alert, Field } from '../components/ui'
import { useAuth } from '../hooks/useAuth'
import { errorMessage, fieldErrors } from '../api/client'
import { ssoApi, type SSOProvider } from '../api/auth'

// The codes the callback redirects with, mapped to something a person can act on.
// A bare code on screen would be unhelpful; a wrong guess about the cause would be
// worse, so each code states only what is known.
const SSO_ERROR_MESSAGES: Record<string, string> = {
  no_account:
    'Your identity was verified, but no ForgeCRM account is linked to it. Ask an administrator to invite you.',
  account_inactive: 'This account is not active. Contact an administrator.',
  no_organization:
    'Your identity is verified, but it is not a member of any organization in this deployment.',
  provider_error: 'The identity provider reported an error. Try again or use a password.',
  sign_in_failed: 'Single sign-on could not be completed. Try again or use a password.',
}

export function LoginPage() {
  const { signIn, completeMFA } = useAuth()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [ssoProviders, setSsoProviders] = useState<SSOProvider[]>([])
  const ssoError = searchParams.get('sso_error')

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [mfaCode, setMfaCode] = useState('')
  const [mfaTicket, setMfaTicket] = useState<string | null>(null)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  // Load the available providers once. A failure here must not break password
  // sign-in, so it is swallowed: the password form is the fallback.
  useEffect(() => {
    let cancelled = false
    ssoApi
      .providers()
      .then((result) => {
        if (!cancelled) setSsoProviders(result.enabled ? result.providers : [])
      })
      .catch(() => {
        if (!cancelled) setSsoProviders([])
      })
    return () => {
      cancelled = true
    }
  }, [])

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setFormError(null)
    setErrors({})
    try {
      if (mfaTicket) {
        await completeMFA(mfaTicket, mfaCode)
      } else {
        const result = await signIn(email, password)
        if (result.mfaRequired) {
          setMfaTicket(result.ticket ?? '')
          setBusy(false)
          return
        }
      }
      navigate('/', { replace: true })
    } catch (error) {
      // The server reports validation problems per field, so surface those
      // inline instead of collapsing everything into one message.
      setErrors(fieldErrors(error))
      setFormError(errorMessage(error))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'grid',
        placeItems: 'center',
        padding: 'var(--space-5)',
      }}
    >
      <div style={{ width: '100%', maxWidth: 380 }}>
        <div className="row" style={{ justifyContent: 'center', marginBottom: 'var(--space-5)' }}>
          <span className="sidebar__mark" aria-hidden="true">F</span>
          <h1 style={{ fontSize: 20 }}>ForgeCRM</h1>
        </div>

        <div className="card">
          <div className="card__body">
            <form onSubmit={handleSubmit} className="stack" noValidate>
              <h2 style={{ marginBottom: 'var(--space-2)' }}>
                {mfaTicket ? 'Two-factor verification' : 'Sign in'}
              </h2>

              {formError ? <Alert tone="error">{formError}</Alert> : null}

              {!mfaTicket && ssoError ? (
                <Alert tone="error">
                  {SSO_ERROR_MESSAGES[ssoError] ?? SSO_ERROR_MESSAGES.sign_in_failed}
                </Alert>
              ) : null}

              {mfaTicket ? (
                <Field
                  label="Authentication code"
                  htmlFor="mfa-code"
                  required
                  hint="Enter the code from your authenticator app."
                >
                  <input
                    id="mfa-code"
                    type="text"
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    autoFocus
                    value={mfaCode}
                    onChange={(event) => setMfaCode(event.target.value)}
                    required
                  />
                </Field>
              ) : (
                <>
                  <Field label="Email" htmlFor="email" required error={errors.email}>
                    <input
                      id="email"
                      type="email"
                      autoComplete="username"
                      autoFocus
                      value={email}
                      onChange={(event) => setEmail(event.target.value)}
                      required
                    />
                  </Field>
                  <Field label="Password" htmlFor="password" required error={errors.password}>
                    <input
                      id="password"
                      type="password"
                      autoComplete="current-password"
                      value={password}
                      onChange={(event) => setPassword(event.target.value)}
                      required
                    />
                  </Field>
                </>
              )}

              <button type="submit" className="btn btn--primary" disabled={busy}>
                {busy ? 'Signing in…' : mfaTicket ? 'Verify' : 'Sign in'}
              </button>

              {mfaTicket ? (
                <button
                  type="button"
                  className="btn btn--ghost btn--sm"
                  onClick={() => {
                    setMfaTicket(null)
                    setMfaCode('')
                    setFormError(null)
                  }}
                >
                  Back to sign in
                </button>
              ) : null}
            </form>

            {!mfaTicket && ssoProviders.length > 0 ? (
              <div className="stack" style={{ marginTop: 'var(--space-4)' }}>
                <div
                  role="separator"
                  aria-label="or"
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 'var(--space-3)',
                    color: 'var(--color-text-muted)',
                    fontSize: 12,
                  }}
                >
                  <span style={{ flex: 1, height: 1, background: 'var(--color-border)' }} />
                  or
                  <span style={{ flex: 1, height: 1, background: 'var(--color-border)' }} />
                </div>
                {ssoProviders.map((provider) => (
                  <button
                    key={provider.id}
                    type="button"
                    className="btn btn--secondary"
                    onClick={() => ssoApi.start(provider.key)}
                  >
                    Continue with {provider.display_name}
                  </button>
                ))}
              </div>
            ) : null}
          </div>
        </div>
      </div>
    </div>
  )
}
