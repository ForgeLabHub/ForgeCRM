/**
 * Cookie consent.
 *
 * Three categories, because a single "OK" button is not consent for anything
 * but the strictly necessary cookies, and saying so is cheaper than pretending:
 *
 *   necessary  — session cookie, CSRF token. Off the table: without them the
 *                application cannot work at all, so they are not offered as a
 *                choice.
 *   analytics  — aggregate usage measurement.
 *   marketing  — advertising and retargeting identifiers.
 *
 * The decision is stored with a version and a timestamp, and the visitor can
 * reopen the panel from the footer at any time and withdraw any category. Nothing
 * in the application loads a third-party script conditional on these flags today
 * — the flags are read by a single accessor, `cookieConsent()`, so a future
 * integration has one place to check instead of guessing.
 */
import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

export type ConsentCategory = 'necessary' | 'analytics' | 'marketing'

export interface CookieDecision {
  version: number
  decidedAt: string
  /**
   * Necessary cookies are not a choice, but the decision records them anyway:
   * the stored consent then states the full set the visitor was told about,
   * rather than only the two switches. A record that omits them reads as if
   * they were optional, which is exactly what the banner says they are not.
   */
  necessary: true
  analytics: boolean
  marketing: boolean
}

export const COOKIE_STORAGE_KEY = 'forgecrm.cookie-consent'
/** Bumping the version re-asks everyone, which is what a policy change requires. */
export const COOKIE_VERSION = 1

export function readCookieDecision(): CookieDecision | null {
  try {
    const raw = window.localStorage.getItem(COOKIE_STORAGE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<CookieDecision>
    if (parsed.version !== COOKIE_VERSION) return null
    if (typeof parsed.analytics !== 'boolean' || typeof parsed.marketing !== 'boolean') return null
    return {
      version: COOKIE_VERSION,
      decidedAt: typeof parsed.decidedAt === 'string' ? parsed.decidedAt : new Date().toISOString(),
      necessary: true,
      analytics: parsed.analytics,
      marketing: parsed.marketing,
    }
  } catch {
    return null
  }
}

function writeCookieDecision(decision: CookieDecision) {
  try {
    window.localStorage.setItem(COOKIE_STORAGE_KEY, JSON.stringify(decision))
  } catch {
    /* storage unavailable: the choice holds for this page view only */
  }
}

/** The single accessor any future integration should call. */
export function cookieConsent(): { analytics: boolean; marketing: boolean } {
  const decision = readCookieDecision()
  return { analytics: decision?.analytics ?? false, marketing: decision?.marketing ?? false }
}

/**
 * Reopens the panel from anywhere. The footer links to `#cookies`, which is the
 * route a visitor will look for, and that hash dispatches this event rather than
 * requiring the panel to be mounted as a modal in every page.
 */
export const COOKIE_OPEN_EVENT = 'forgecrm:open-cookie-settings'

export function openCookieSettings() {
  window.dispatchEvent(new CustomEvent(COOKIE_OPEN_EVENT))
}

export function CookieConsent() {
  const [decision, setDecision] = useState<CookieDecision | null>(() => readCookieDecision())
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState({ analytics: false, marketing: false })

  // The panel reopens either from the explicit event or from the `#cookies`
  // hash, so a link in the footer and a link in a policy page both work.
  useEffect(() => {
    const reopen = () => {
      const current = readCookieDecision()
      setDraft({
        analytics: current?.analytics ?? false,
        marketing: current?.marketing ?? false,
      })
      setOpen(true)
    }
    window.addEventListener(COOKIE_OPEN_EVENT, reopen)
    if (window.location.hash === '#cookies') reopen()
    const onHash = () => {
      if (window.location.hash === '#cookies') reopen()
    }
    window.addEventListener('hashchange', onHash)
    return () => {
      window.removeEventListener(COOKIE_OPEN_EVENT, reopen)
      window.removeEventListener('hashchange', onHash)
    }
  }, [])

  const save = useCallback((analytics: boolean, marketing: boolean) => {
    const next: CookieDecision = {
      version: COOKIE_VERSION,
      decidedAt: new Date().toISOString(),
      necessary: true,
      analytics,
      marketing,
    }
    writeCookieDecision(next)
    setDecision(next)
    setOpen(false)
  }, [])

  if (decision && !open) return null

  return (
    <div className="cookie" role="dialog" aria-modal="false" aria-labelledby="cookie-title">
      <div className="cookie__panel">
        <div className="cookie__head">
          <p className="cookie__emoji" aria-hidden="true">
            🍪
          </p>
          <div>
            <h2 className="cookie__title" id="cookie-title">
              {decision ? 'Настройки cookies' : 'Мы используем cookies'}
            </h2>
            <p className="cookie__text">
              Необходимые cookies нужны для входа и защиты формы. Аналитические и маркетинговые
              включаются только с вашего согласия и могут быть отозваны в любой момент.{' '}
              <Link to="/privacy#cookies" className="cookie__link">
                Правила использования cookies
              </Link>
              .
            </p>
          </div>
        </div>

        {open ? (
          <ul className="cookie__categories">
            <li className="cookie__category cookie__category--locked">
              <div>
                <p className="cookie__category-title">Необходимые</p>
                <p className="cookie__category-text">
                  Сессия, защита от подделки запросов, запоминание выбранной темы. Без них вход не
                  работает.
                </p>
              </div>
              <span className="cookie__always">Всегда включены</span>
            </li>
            <li className="cookie__category">
              <div>
                <p className="cookie__category-title">Аналитические</p>
                <p className="cookie__category-text">
                  Обезличенная статистика посещений: какие страницы читают и где уходят.
                </p>
              </div>
              <label className="cookie__switch">
                <input
                  type="checkbox"
                  checked={draft.analytics}
                  onChange={(event) =>
                    setDraft((value) => ({ ...value, analytics: event.target.checked }))
                  }
                />
                <span aria-hidden="true" />
                <span className="sr-only">Аналитические cookies</span>
              </label>
            </li>
            <li className="cookie__category">
              <div>
                <p className="cookie__category-title">Маркетинговые</p>
                <p className="cookie__category-text">
                  Позволяют показывать наши материалы на других площадках и измерять отдачу от
                  рекламы.
                </p>
              </div>
              <label className="cookie__switch">
                <input
                  type="checkbox"
                  checked={draft.marketing}
                  onChange={(event) =>
                    setDraft((value) => ({ ...value, marketing: event.target.checked }))
                  }
                />
                <span aria-hidden="true" />
                <span className="sr-only">Маркетинговые cookies</span>
              </label>
            </li>
          </ul>
        ) : null}

        <div className="cookie__actions">
          {open ? (
            <button
              type="button"
              className="site-btn site-btn--primary site-btn--sm"
              onClick={() => save(draft.analytics, draft.marketing)}
            >
              Сохранить выбор
            </button>
          ) : (
            <>
              <button
                type="button"
                className="site-btn site-btn--primary site-btn--sm"
                onClick={() => save(true, true)}
              >
                Принять все
              </button>
              <button
                type="button"
                className="site-btn site-btn--secondary site-btn--sm"
                onClick={() => save(false, false)}
              >
                Только необходимые
              </button>
              <button
                type="button"
                className="site-btn site-btn--ghost site-btn--sm"
                onClick={() => {
                  setDraft({
                    analytics: decision?.analytics ?? false,
                    marketing: decision?.marketing ?? false,
                  })
                  setOpen(true)
                }}
              >
                Настроить
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
