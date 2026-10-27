import { useState } from 'react'
import type { FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { Alert, Field } from '../components/ui'
import { useAuth } from '../hooks/useAuth'
import { errorMessage, fieldErrors } from '../api/client'

export function LoginPage() {
  const { signIn, completeMFA } = useAuth()
  const navigate = useNavigate()

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [mfaCode, setMfaCode] = useState('')
  const [mfaTicket, setMfaTicket] = useState<string | null>(null)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

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
          </div>
        </div>
      </div>
    </div>
  )
}
