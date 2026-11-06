/**
 * The site's layout switcher.
 *
 * The brief asked for one reference layout to be built and the rest to exist as
 * a choice the visitor can make. That is what this is: three complete
 * arrangements of the same content, not three colour schemes.
 *
 *   agency   — the service business: dense catalogue, price-first, orange accent
 *   media    — the publication: large covers, editorial column, reading sidebar
 *   docs     — the documentation: contents rail on the left, tight type, no fluff
 *
 * The choice is device-local, like the interface experience in the application,
 * and it is applied as `data-site-variant` on the site root, so a stylesheet can
 * rearrange the same DOM without a second React tree.
 */
import { useEffect, useState } from 'react'
import { useT } from '../../i18n'

export type SiteVariant = 'agency' | 'media' | 'docs'

export const SITE_VARIANTS: readonly { id: SiteVariant; name: string; hint: string }[] = [
  { id: 'agency', name: 'Каталог', hint: 'Плотная сетка услуг, цены и тарифы на виду' },
  { id: 'media', name: 'Издание', hint: 'Крупные обложки, широкий текст, автор материала' },
  { id: 'docs', name: 'Документация', hint: 'Оглавление слева, плотный текст, без украшений' },
]

export const SITE_VARIANT_KEY = 'forgecrm.site.variant'
export const DEFAULT_VARIANT: SiteVariant = 'agency'

export function isSiteVariant(value: string | null): value is SiteVariant {
  return value === 'agency' || value === 'media' || value === 'docs'
}

export function resolveInitialVariant(): SiteVariant {
  try {
    const stored = window.localStorage.getItem(SITE_VARIANT_KEY)
    return isSiteVariant(stored) ? stored : DEFAULT_VARIANT
  } catch {
    return DEFAULT_VARIANT
  }
}

/**
 * Applies the variant to the document so the stylesheet can react before React
 * paints, and keeps it on the site root. Removed on unmount so the CRM, which
 * shares the document, is never styled by a site variant.
 */
export function applySiteVariant(variant: SiteVariant) {
  document.documentElement.setAttribute('data-site-variant', variant)
}

export function SiteVariantSwitcher() {
  const t = useT()
  const [open, setOpen] = useState(false)
  const [variant, setVariant] = useState<SiteVariant>(() => resolveInitialVariant())

  useEffect(() => {
    applySiteVariant(variant)
    return () => document.documentElement.removeAttribute('data-site-variant')
  }, [variant])

  function choose(next: SiteVariant) {
    setVariant(next)
    try {
      window.localStorage.setItem(SITE_VARIANT_KEY, next)
    } catch {
      /* storage unavailable: the choice holds for this page view only */
    }
  }

  return (
    <div className={open ? 'site-variant site-variant--open' : 'site-variant'}>
      {open ? (
        <div className="site-variant__panel" role="group" aria-label={t('site.variant.label')}>
          <p className="site-variant__title">{t('site.variant.label')}</p>
          <ul className="site-variant__list">
            {SITE_VARIANTS.map((option) => (
              <li key={option.id}>
                <button
                  type="button"
                  className={
                    option.id === variant
                      ? 'site-variant__item site-variant__item--active'
                      : 'site-variant__item'
                  }
                  aria-pressed={option.id === variant}
                  onClick={() => choose(option.id)}
                >
                  <span className="site-variant__name">{option.name}</span>
                  <span className="site-variant__hint">{option.hint}</span>
                </button>
              </li>
            ))}
          </ul>
          <p className="site-variant__note">{t('site.variant.note')}</p>
        </div>
      ) : null}
      <button
        type="button"
        className="site-variant__toggle"
        aria-expanded={open}
        aria-label={t('site.variant.toggle')}
        onClick={() => setOpen((value) => !value)}
      >
        <span aria-hidden="true" className="site-variant__dots">
          <i />
          <i />
          <i />
        </span>
        <span className="site-variant__toggle-text">{t('site.variant.toggleShort')}</span>
      </button>
    </div>
  )
}
