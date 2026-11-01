import { useEffect, useId, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Alert } from '../components/ui'
import { BrandMark } from '../components/BrandMark'
import { LoginControls } from '../components/LoginControls'
import { useAuth } from '../hooks/useAuth'
import { errorMessage, fieldErrors } from '../api/client'
import { ssoApi, type SSOProvider } from '../api/auth'
import { useI18n } from '../i18n'

/**
 * Sign-in page.
 *
 * The layout is two panes on a wide screen: a branded panel that states what the
 * product is, and a focused form. On a narrow screen the brand panel collapses to
 * a compact header so the form is not pushed below the fold. All copy comes from
 * the dictionary, and every control is a real control — there is no decorative
 * button or placeholder link.
 *
 * Client-side validation runs on submit only (never on every keystroke, which
 * makes a form feel broken) and the server's own field errors replace it when the
 * request fails, so the two never disagree on screen.
 */
export function LoginPage() {
  const { signIn, completeMFA } = useAuth()
  const { t } = useI18n()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [ssoProviders, setSsoProviders] = useState<SSOProvider[]>([])
  const ssoError = searchParams.get('sso_error')

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [mfaCode, setMfaCode] = useState('')
  const [mfaTicket, setMfaTicket] = useState<string | null>(null)
  const [showPassword, setShowPassword] = useState(false)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const emailId = useId()
  const passwordId = useId()
  const mfaId = useId()
  const formErrorRef = useRef<HTMLDivElement | null>(null)

  // The provider list is anonymous and optional. A failure must not break
  // password sign-in, so it is swallowed and the form remains the fallback.
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

  // Move focus to the error region so a screen-reader user hears the failure
  // without hunting for it after a submit.
  useEffect(() => {
    if (formError) formErrorRef.current?.focus()
  }, [formError])

  function validate(): Record<string, string> {
    const next: Record<string, string> = {}
    if (mfaTicket) {
      if (mfaCode.trim() === '') next.mfa = t('login.errorPasswordRequired')
      return next
    }
    if (email.trim() === '') {
      next.email = t('login.errorEmailRequired')
    } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())) {
      next.email = t('login.errorEmailInvalid')
    }
    if (password === '') next.password = t('login.errorPasswordRequired')
    return next
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    const clientErrors = validate()
    if (Object.keys(clientErrors).length > 0) {
      setErrors(clientErrors)
      setFormError(null)
      return
    }

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
      setErrors(fieldErrors(error))
      setFormError(errorMessage(error))
    } finally {
      setBusy(false)
    }
  }

  const ssoMessage = ssoError
    ? t(`login.errors.${ssoError}` as 'login.errors.sign_in_failed') || t('login.errors.sign_in_failed')
    : null

  return (
    <div className="login">
      <aside className="login__aside" aria-hidden="false">
        <div className="login__aside-inner">
          <div className="login__brand">
            <BrandMark size={30} />
            <span className="login__brand-name">{t('brand.name')}</span>
          </div>
          <p className="login__tagline">{t('brand.tagline')}</p>
          <p className="login__description">{t('brand.description')}</p>
          <ul className="login__features">
            {(['feature1', 'feature2', 'feature3'] as const).map((key) => (
              <li key={key} className="login__feature">
                <span className="login__feature-dot" aria-hidden="true" />
                {t(`brand.${key}`)}
              </li>
            ))}
          </ul>
        </div>
        <ul className="login__trust">
          {(['selfHosted', 'encrypted', 'audit'] as const).map((key) => (
            <li key={key}>{t(`login.trust.${key}`)}</li>
          ))}
        </ul>
      </aside>

      <main className="login__main">
        <div className="login__topbar">
          <div className="login__brand login__brand--compact">
            <BrandMark size={24} />
            <span className="login__brand-name">{t('brand.name')}</span>
          </div>
          <LoginControls />
        </div>

        <div className="login__panel">
          <header className="login__header">
            <h1 className="login__title">{mfaTicket ? t('login.mfaTitle') : t('login.title')}</h1>
            {!mfaTicket ? <p className="login__subtitle">{t('login.subtitle')}</p> : null}
          </header>

          <div ref={formErrorRef} tabIndex={-1} className="login__error-slot">
            {formError ? <Alert tone="error">{formError}</Alert> : null}
            {!mfaTicket && ssoMessage ? <Alert tone="error">{ssoMessage}</Alert> : null}
          </div>

          <form onSubmit={handleSubmit} className="login__form" noValidate>
            {mfaTicket ? (
              <div className="login-field">
                <label className="login-field__label" htmlFor={mfaId}>
                  {t('login.mfaLabel')}
                </label>
                <input
                  id={mfaId}
                  className={`login-field__input${errors.mfa ? ' login-field__input--invalid' : ''}`}
                  type="text"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  autoFocus
                  value={mfaCode}
                  onChange={(event) => setMfaCode(event.target.value)}
                  aria-invalid={errors.mfa ? true : undefined}
                />
                <span className="login-field__hint">{t('login.mfaHint')}</span>
                {errors.mfa ? (
                  <span className="login-field__error" role="alert">
                    {errors.mfa}
                  </span>
                ) : null}
              </div>
            ) : (
              <>
                <div className="login-field">
                  <label className="login-field__label" htmlFor={emailId}>
                    {t('common.email')}
                  </label>
                  <input
                    id={emailId}
                    className={`login-field__input${errors.email ? ' login-field__input--invalid' : ''}`}
                    type="email"
                    name="email"
                    autoComplete="username"
                    autoFocus
                    spellCheck={false}
                    autoCapitalize="none"
                    value={email}
                    onChange={(event) => setEmail(event.target.value)}
                    aria-invalid={errors.email ? true : undefined}
                    aria-describedby={errors.email ? `${emailId}-error` : undefined}
                  />
                  {errors.email ? (
                    <span className="login-field__error" id={`${emailId}-error`} role="alert">
                      {errors.email}
                    </span>
                  ) : null}
                </div>

                <div className="login-field">
                  <label className="login-field__label" htmlFor={passwordId}>
                    {t('common.password')}
                  </label>
                  <div className="login-field__control">
                    <input
                      id={passwordId}
                      className={`login-field__input${errors.password ? ' login-field__input--invalid' : ''}`}
                      type={showPassword ? 'text' : 'password'}
                      name="password"
                      autoComplete="current-password"
                      value={password}
                      onChange={(event) => setPassword(event.target.value)}
                      aria-invalid={errors.password ? true : undefined}
                      aria-describedby={errors.password ? `${passwordId}-error` : undefined}
                    />
                    <button
                      type="button"
                      className="login-field__reveal"
                      onClick={() => setShowPassword((value) => !value)}
                      aria-pressed={showPassword}
                      aria-label={showPassword ? t('login.hidePassword') : t('login.showPassword')}
                      title={showPassword ? t('login.hidePassword') : t('login.showPassword')}
                    >
                      {showPassword ? <EyeOffIcon /> : <EyeIcon />}
                    </button>
                  </div>
                  {errors.password ? (
                    <span className="login-field__error" id={`${passwordId}-error`} role="alert">
                      {errors.password}
                    </span>
                  ) : null}
                </div>
              </>
            )}

            <button type="submit" className="btn btn--primary login__submit" disabled={busy}>
              {busy
                ? mfaTicket
                  ? t('login.mfaSubmitting')
                  : t('login.submitting')
                : mfaTicket
                  ? t('login.mfaSubmit')
                  : t('login.submit')}
            </button>

            {mfaTicket ? (
              <button
                type="button"
                className="btn btn--ghost login__back"
                onClick={() => {
                  setMfaTicket(null)
                  setMfaCode('')
                  setFormError(null)
                }}
              >
                {t('login.backToSignIn')}
              </button>
            ) : null}
          </form>

          {!mfaTicket && ssoProviders.length > 0 ? (
            <div className="login__sso">
              <div className="login__divider" role="separator" aria-label={t('common.or')}>
                <span />
                {t('common.or')}
                <span />
              </div>
              {ssoProviders.map((provider) => (
                <button
                  key={provider.id}
                  type="button"
                  className="btn btn--secondary login__sso-button"
                  onClick={() => ssoApi.start(provider.key)}
                >
                  {t('login.ssoContinue', { provider: provider.display_name })}
                </button>
              ))}
            </div>
          ) : null}

          <p className="login__foot">{t('login.help')}</p>
        </div>
      </main>
    </div>
  )
}


/**
 * The reveal-password control is an icon, not a text button: the Russian label
 * ("Показать пароль") is wide enough to overlap the typed password inside the
 * field. The accessible name is supplied by aria-label, so the glyph is purely
 * decorative and hidden from assistive technology.
 */
function EyeIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path
        d="M12 5c-5 0-9 4.5-10 7 1 2.5 5 7 10 7s9-4.5 10-7c-1-2.5-5-7-10-7zm0 11a4 4 0 110-8 4 4 0 010 8zm0-2a2 2 0 100-4 2 2 0 000 4z"
        fill="currentColor"
      />
    </svg>
  )
}

function EyeOffIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path
        d="M3 4.3 4.3 3l16.7 16.7-1.3 1.3-3-3A10.6 10.6 0 0112 19c-5 0-9-4.5-10-7a12 12 0 013.2-4.4L3 4.3zM12 7a4 4 0 013.9 5l-1.6-1.6a2 2 0 00-2.7-2.7L9.9 6.1A5 5 0 0112 7zm-6.5.9 1.5 1.5A10 10 0 002.3 12c1 2.5 5 7 9.7 7 1.2 0 2.3-.3 3.3-.7l1.5 1.5A10.5 10.5 0 0112 21c-5 0-9-4.5-10-7a12.6 12.6 0 013.5-6.1z"
        fill="currentColor"
      />
    </svg>
  )
}
