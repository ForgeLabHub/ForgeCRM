/**
 * Appearance — choose the visual style, accent colour and density.
 *
 * The choice is applied live through the theme provider, which writes the
 * attributes the stylesheet keys off, so a click here restyles the page you are
 * looking at rather than a settings preview elsewhere. Preview temporarily shows
 * a style without committing it, and stops (or is left) on navigation.
 *
 * Signed-in settings are saved to the account; preview never writes to the server.
 * Appearance does not grant access to any CRM data or operation.
 */
import { useEffect, useRef, useState } from 'react'
import { Alert, Card, PageHeader } from '../components/ui'
import { useT } from '../i18n'
import { useTheme } from '../theme'
import {
  ACCENTS,
  DENSITIES,
  THEME_STYLES,
  STYLE_SWATCH,
  accentLabelKey,
  densityLabelKey,
  styleDescriptionKey,
  styleLabelKey,
  type ThemeStyle,
} from '../theme/themes'

/** A small, abstract rendering of what a style looks like. */
function StyleThumbnail({ style }: { style: ThemeStyle }) {
  const swatch = STYLE_SWATCH[style]
  return (
    <span
      className="style-thumb"
      aria-hidden="true"
      style={{ background: swatch.bg, borderColor: swatch.accent }}
    >
      <span className="style-thumb__rail" style={{ background: swatch.surface, borderColor: swatch.text }} />
      <span className="style-thumb__body">
        <span className="style-thumb__bar" style={{ background: swatch.accent }} />
        <span className="style-thumb__card" style={{ background: swatch.surface, borderColor: swatch.text }} />
        <span className="style-thumb__card" style={{ background: swatch.surface, borderColor: swatch.text }} />
      </span>
    </span>
  )
}

export function AppearancePage() {
  const t = useT()
  const { style, setStyle, accent, setAccent, density, setDensity, resetAppearance, saving, syncError, automatic, ready } = useTheme()
  const [preview, setPreview] = useState<ThemeStyle | null>(null)
  const [resetDone, setResetDone] = useState(false)
  const committedStyle = useRef(style)
  committedStyle.current = style

  // A preview is temporary by design: leaving the page (or unmounting) drops it
  // so the committed style is what the rest of the app returns to.
  useEffect(() => {
    if (preview === null) return
    document.documentElement.setAttribute('data-style', preview)
    return () => document.documentElement.setAttribute('data-style', committedStyle.current)
  }, [preview, style])

  const shown = preview ?? style

  return (
    <>
      <PageHeader title={t('appearance.title')} meta={t('appearance.meta')} />

      <div className="stack">
        {syncError ? <Alert tone="error">{t('appearance.saveError')}</Alert> : null}
        {saving ? <p role="status">{t('appearance.saving')}</p> : null}
        <Card title={t('appearance.stylesHeading')}>
          <p className="text-muted text-sm" style={{ marginBottom: 'var(--space-4)' }}>
            {t('appearance.stylesHint')}
          </p>
          <ul className="style-grid">
            {THEME_STYLES.map((option) => {
              const active = option === shown
              return (
                <li key={option} className="style-grid__item">
                  <div
                    className={active ? 'style-card style-card--active' : 'style-card'}
                    aria-current={option === style ? 'true' : undefined}
                  >
                    <StyleThumbnail style={option} />
                    <div className="style-card__text">
                      <div className="style-card__name">
                        {t(styleLabelKey(option))}
                        {option === style ? (
                          <span className="badge badge--accent">{t('appearance.current')}</span>
                        ) : null}
                      </div>
                      <p className="style-card__desc">{t(styleDescriptionKey(option))}</p>
                    </div>
                    <div className="style-card__actions">
                      <button
                        type="button"
                        className="btn btn--secondary btn--sm"
                        aria-pressed={preview === option}
                        disabled={saving || !ready}
                        onClick={() => setPreview(preview === option ? null : option)}
                      >
                        {preview === option ? t('appearance.stopPreview') : t('appearance.preview')}
                      </button>
                      <button
                        type="button"
                        className="btn btn--primary btn--sm"
                        disabled={saving || !ready || (option === style && !automatic)}
                        onClick={() => {
                          void setStyle(option).then(() => setPreview(null))
                        }}
                      >
                        {t('appearance.apply')}
                      </button>
                    </div>
                  </div>
                </li>
              )
            })}
          </ul>
        </Card>

        <Card title={t('appearance.accentHeading')}>
          <p className="text-muted text-sm" style={{ marginBottom: 'var(--space-4)' }}>
            {t('appearance.accentHint')}
          </p>
          <div className="swatch-row" role="group" aria-label={t('appearance.accentHeading')}>
            {ACCENTS.map((option) => (
              <button
                key={option}
                type="button"
                className={option === accent ? 'swatch swatch--active' : 'swatch'}
                aria-pressed={option === accent}
                disabled={saving || !ready}
                onClick={() => void setAccent(option)}
              >
                <span className={`swatch__dot swatch__dot--${option}`} aria-hidden="true" />
                <span>{t(accentLabelKey(option))}</span>
              </button>
            ))}
          </div>
        </Card>

        <Card title={t('appearance.densityHeading')}>
          <p className="text-muted text-sm" style={{ marginBottom: 'var(--space-4)' }}>
            {t('appearance.densityHint')}
          </p>
          <div className="segmented" role="group" aria-label={t('appearance.densityHeading')}>
            {DENSITIES.map((option) => (
              <button
                key={option}
                type="button"
                className={option === density ? 'segmented__item segmented__item--active' : 'segmented__item'}
                aria-pressed={option === density}
                disabled={saving || !ready}
                onClick={() => void setDensity(option)}
              >
                {t(densityLabelKey(option))}
              </button>
            ))}
          </div>
        </Card>

        <div className="row">
          <button
            type="button"
            className="btn btn--secondary btn--sm"
            disabled={saving || !ready}
            onClick={() => {
              setResetDone(false)
              void resetAppearance().then((success) => {
                setPreview(null)
                if (success) setResetDone(true)
              })
            }}
          >
            {t('appearance.reset')}
          </button>
        </div>

        {resetDone ? <Alert tone="success">{t('appearance.resetDone')}</Alert> : null}
      </div>
    </>
  )
}
