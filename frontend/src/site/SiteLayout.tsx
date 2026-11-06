/**
 * The public site's chrome: promo strip, header with a mega menu, footer, and
 * the cookie gate.
 *
 * The site and the CRM share one origin and one token system, so the marketing
 * pages inherit whichever of the five interface palettes the visitor has chosen
 * in the application. What changes here is composition, not colour: the
 * `variant` attribute swaps the three arrangements (agency catalogue, media
 * publication, documentation) while the palette stays the product's.
 */
import { Suspense, useEffect, useId, useRef, useState } from 'react'
import { Link, NavLink, Outlet, useLocation } from 'react-router-dom'
import { useTheme } from '../theme'
import { CONTACTS, PROMO, SERVICE_GROUPS, SERVICES } from './content'
import { CookieConsent } from './components/CookieConsent'
import { SiteVariantSwitcher } from './components/SiteVariantSwitcher'
import { useAuth } from '../hooks/useAuth'

/** Top-level destinations. Order is the reading order of the site. */
const NAV = [
  { to: '/services', label: 'Услуги', mega: true },
  { to: '/pricing', label: 'Тарифы и лицензии' },
  { to: '/cases', label: 'Кейсы' },
  { to: '/blog', label: 'Блог' },
  { to: '/about', label: 'О компании' },
  { to: '/contacts', label: 'Контакты' },
] as const

function Logo() {
  return (
    <Link to="/" className="site-logo" aria-label={`${CONTACTS.brand} — на главную`}>
      <span className="site-logo__mark" aria-hidden="true">
        <svg viewBox="0 0 32 32" width="32" height="32" fill="none" aria-hidden="true">
          <rect x="1" y="1" width="30" height="30" rx="9" fill="currentColor" opacity="0.12" />
          <path
            d="M10 22V10h9a4 4 0 0 1 0 8h-5"
            stroke="currentColor"
            strokeWidth="2.4"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
          <circle cx="21.5" cy="21.5" r="2.2" fill="currentColor" />
        </svg>
      </span>
      <span className="site-logo__text">
        <span className="site-logo__name">Pinscher</span>
        <span className="site-logo__suffix">CRM</span>
      </span>
    </Link>
  )
}

/**
 * One mega-menu column per service group. Rendered as links rather than a
 * decorative panel: every entry goes somewhere real, because a menu that opens
 * and does nothing is worse than no menu.
 */
function ServicesMega({ open, onNavigate }: { open: boolean; onNavigate: () => void }) {
  const panelRef = useRef<HTMLDivElement>(null)
  if (!open) return null
  return (
    <div className="site-mega" ref={panelRef} role="group" aria-label="Услуги">
      <div className="site-mega__groups">
        {SERVICE_GROUPS.map((group) => {
          const items = SERVICES.filter((service) => service.group === group.id)
          return (
            <div className="site-mega__group" key={group.id}>
              <p className="site-mega__title">{group.title}</p>
              <p className="site-mega__blurb">{group.blurb}</p>
              {items.length > 0 ? (
                <ul className="site-mega__list">
                  {items.map((service) => (
                    <li key={service.slug}>
                      <Link to={`/services/${service.slug}`} onClick={onNavigate}>
                        {service.title}
                      </Link>
                    </li>
                  ))}
                </ul>
              ) : (
                <Link className="site-mega__more" to="/services" onClick={onNavigate}>
                  Все услуги группы
                </Link>
              )}
            </div>
          )
        })}
      </div>
      <div className="site-mega__footer">
        <span>Не знаете, что выбрать? Начните с бесплатной диагностики.</span>
        <Link to="/contacts" onClick={onNavigate} className="site-btn site-btn--primary site-btn--sm">
          Записаться
        </Link>
      </div>
    </div>
  )
}

function Header() {
  const [mega, setMega] = useState(false)
  const [drawer, setDrawer] = useState(false)
  const location = useLocation()
  const { status } = useAuth()

  // A menu that survives navigation covers the page it just opened.
  useEffect(() => {
    setMega(false)
    setDrawer(false)
  }, [location.pathname])

  useEffect(() => {
    if (!mega && !drawer) return
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setMega(false)
        setDrawer(false)
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [mega, drawer])

  const crmHref = status === 'authenticated' ? '/app' : '/login'
  const crmLabel = status === 'authenticated' ? 'Открыть CRM' : 'Войти в CRM'

  return (
    <header className="site-header">
      <div className="site-container site-header__row">
        <Logo />

        <nav className="site-nav" aria-label="Основная навигация">
          {NAV.map((item) =>
            'mega' in item && item.mega ? (
              <div className="site-nav__mega-trigger" key={item.to}>
                <button
                  type="button"
                  className={mega ? 'site-nav__link site-nav__link--open' : 'site-nav__link'}
                  aria-expanded={mega}
                  aria-haspopup="true"
                  onClick={() => setMega((value) => !value)}
                >
                  {item.label}
                  <span className="site-nav__caret" aria-hidden="true" />
                </button>
              </div>
            ) : (
              <NavLink
                key={item.to}
                to={item.to}
                className={({ isActive }) =>
                  isActive ? 'site-nav__link site-nav__link--active' : 'site-nav__link'
                }
              >
                {item.label}
              </NavLink>
            ),
          )}
        </nav>

        <div className="site-header__actions">
          <a className="site-phone" href={CONTACTS.phoneHref}>
            {CONTACTS.phone}
          </a>
          <Link className="site-btn site-btn--ghost site-btn--sm" to={crmHref}>
            {crmLabel}
          </Link>
          <button
            type="button"
            className="site-burger"
            aria-label={drawer ? 'Закрыть меню' : 'Открыть меню'}
            aria-expanded={drawer}
            onClick={() => setDrawer((value) => !value)}
          >
            <span aria-hidden="true" />
            <span aria-hidden="true" />
          </button>
        </div>
      </div>

      <div className="site-container">
        <ServicesMega open={mega} onNavigate={() => setMega(false)} />
      </div>

      {drawer ? (
        <div className="site-drawer" role="dialog" aria-label="Меню сайта">
          <div className="site-drawer__head">
            <Logo />
            <button
              type="button"
              className="site-btn site-btn--ghost site-btn--sm"
              onClick={() => setDrawer(false)}
            >
              Закрыть
            </button>
          </div>
          <nav className="site-drawer__nav" aria-label="Мобильная навигация">
            {NAV.map((item) => (
              <NavLink key={item.to} to={item.to} className="site-drawer__link">
                {item.label}
              </NavLink>
            ))}
            <Link to={crmHref} className="site-drawer__link site-drawer__link--accent">
              {crmLabel}
            </Link>
          </nav>
          <a className="site-drawer__phone" href={CONTACTS.phoneHref}>
            {CONTACTS.phone}
          </a>
        </div>
      ) : null}
    </header>
  )
}

function Footer() {
  const serviceLinks = SERVICES.slice(0, 6)
  return (
    <footer className="site-footer">
      <div className="site-container">
        <div className="site-footer__top">
          <div className="site-footer__brand">
            <Logo />
            <p className="site-footer__about">
              Внедряем amoCRM и Битрикс24, разрабатываем собственные системы и учим команды
              работать в них самостоятельно.
            </p>
            <p className="site-footer__hours">{CONTACTS.hours}</p>
          </div>

          <div className="site-footer__col">
            <p className="site-footer__title">Услуги</p>
            <ul>
              {serviceLinks.map((service) => (
                <li key={service.slug}>
                  <Link to={`/services/${service.slug}`}>{service.title}</Link>
                </li>
              ))}
              <li>
                <Link to="/services">Все услуги</Link>
              </li>
            </ul>
          </div>

          <div className="site-footer__col">
            <p className="site-footer__title">Компания</p>
            <ul>
              <li>
                <Link to="/about">О компании</Link>
              </li>
              <li>
                <Link to="/cases">Кейсы</Link>
              </li>
              <li>
                <Link to="/blog">Блог</Link>
              </li>
              <li>
                <Link to="/pricing">Тарифы и лицензии</Link>
              </li>
              <li>
                <Link to="/contacts">Контакты</Link>
              </li>
            </ul>
          </div>

          <div className="site-footer__col site-footer__col--contacts">
            <p className="site-footer__title">Связаться</p>
            <a className="site-footer__phone" href={CONTACTS.phoneHref}>
              {CONTACTS.phone}
            </a>
            <a className="site-footer__link" href={`mailto:${CONTACTS.email}`}>
              {CONTACTS.email}
            </a>
            <p className="site-footer__addr">{CONTACTS.address}</p>
            <ul className="site-footer__socials">
              {CONTACTS.socials.map((social) => (
                <li key={social.name}>
                  <a href={social.href}>{social.name}</a>
                </li>
              ))}
            </ul>
          </div>
        </div>

        <div className="site-footer__bottom">
          <p>
            © {CONTACTS.legal}, 2026. Все права защищены. <span className="site-footer__age">16+</span>
          </p>
          <ul className="site-footer__legal">
            <li>
              <Link to="/privacy">Политика конфиденциальности и cookies</Link>
            </li>
            <li>
              <Link to="/privacy#offer">Договор оферты</Link>
            </li>
            <li>
              <Link to="/privacy#cookies">Правила использования cookies</Link>
            </li>
          </ul>
        </div>
      </div>
    </footer>
  )
}

/**
 * The promo strip. It is dismissible and the dismissal is remembered, so a
 * returning visitor is not shown the same banner on every page.
 */
const PROMO_STORAGE_KEY = 'forgecrm.site.promo'

function PromoBar() {
  const [visible, setVisible] = useState(() => {
    try {
      return window.localStorage.getItem(PROMO_STORAGE_KEY) !== 'dismissed'
    } catch {
      return true
    }
  })
  if (!visible) return null
  return (
    <div className="site-promo">
      <div className="site-container site-promo__row">
        <p className="site-promo__text">
          <strong>Скидки {PROMO.discount}</strong> на лицензии и <strong>{PROMO.gift}</strong> к
          внедрению
          <span className="site-promo__note"> — {PROMO.note}</span>
        </p>
        <div className="site-promo__actions">
          <Link to="/pricing" className="site-btn site-btn--inverse site-btn--sm">
            Выбрать
          </Link>
          <button
            type="button"
            className="site-promo__close"
            aria-label="Скрыть предложение"
            onClick={() => {
              setVisible(false)
              try {
                window.localStorage.setItem(PROMO_STORAGE_KEY, 'dismissed')
              } catch {
                /* storage unavailable: the strip simply reappears next visit */
              }
            }}
          >
            ×
          </button>
        </div>
      </div>
    </div>
  )
}

export function SiteLayout() {
  const { layout } = useTheme()
  const location = useLocation()
  const mainId = useId()

  // A new page starts at the top; without this a route change keeps the scroll
  // position of the previous page, which reads as "the link did nothing".
  useEffect(() => {
    window.scrollTo({ top: 0, behavior: 'auto' })
  }, [location.pathname])

  return (
    <div className="site" data-site-layout={layout}>
      <a className="site-skip" href={`#${mainId}`}>
        К содержимому
      </a>
      <PromoBar />
      <Header />
      <main id={mainId} className="site-main">
        {/* Each page is a separate chunk; the fallback keeps the header and
            footer in place so a slow connection does not flash an empty page. */}
        <Suspense fallback={<div className="site-section" aria-busy="true" />}>
          <Outlet />
        </Suspense>
      </main>
      <Footer />
      <SiteVariantSwitcher />
      <CookieConsent />
    </div>
  )
}
