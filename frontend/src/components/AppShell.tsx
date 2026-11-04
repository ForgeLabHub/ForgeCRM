/**
 * AppShell — the frame every authenticated page renders inside.
 *
 * It owns four things the pages should not each re-solve: the navigation model
 * (shared with the command palette through `navigation.ts`), the account menu
 * with the theme switch, the global search entry point, and a breadcrumb trail
 * derived from the route so a deep page always shows where it sits.
 *
 * The sidebar collapses to an icon rail on a wide screen and slides over the
 * content on a narrow one; the state is remembered, so a user who prefers the
 * rail does not re-collapse it on every visit.
 */
import { useEffect, useMemo, useRef, useState, Suspense } from 'react'
import type { JSX } from 'react'
import { Link, NavLink, Outlet, useLocation } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { useGlobalSearchShortcut } from '../hooks/useGlobalSearch'
import { useI18n, type TranslationKey } from '../i18n'
import { useTheme, THEME_MODES, type ThemeMode } from '../theme'
import { visibleNavGroups, type NavEntry } from '../navigation'
import { Avatar, Skeleton } from './ui'
import { CommandPalette } from './ds/CommandPalette'
import { Tooltip } from './ds/overlays'
import {
  IconChevronDown,
  IconChevronRight,
  IconClose,
  IconLayers,
  IconLogOut,
  IconMenu,
  IconMonitor,
  IconMoon,
  IconPanelLeft,
  IconSearch,
  IconSun,
  IconUser,
  type IconProps,
} from './icons'

const THEME_ICON: Record<ThemeMode, (props: IconProps) => JSX.Element> = {
  light: IconSun,
  dark: IconMoon,
  system: IconMonitor,
}

const THEME_LABEL_KEY: Record<ThemeMode, TranslationKey> = {
  light: 'theme.light',
  dark: 'theme.dark',
  system: 'theme.system',
}

const COLLAPSE_KEY = 'forgecrm.sidebar.collapsed'

/** The account control: identity, theme switch, profile and sign-out. */
function AccountMenu() {
  const { session, signOut } = useAuth()
  const { t } = useI18n()
  const { mode, setMode } = useTheme()
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onPointerDown = (event: MouseEvent) => {
      if (ref.current && !ref.current.contains(event.target as Node)) setOpen(false)
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [open])

  const displayName = session?.user?.display_name ?? ''
  const email = session?.user?.email ?? ''

  return (
    <div className="menu" ref={ref}>
      <button
        type="button"
        className="menu__trigger"
        aria-haspopup="true"
        aria-expanded={open}
        aria-label={t('nav.account')}
        onClick={() => setOpen((value) => !value)}
      >
        <Avatar name={displayName} email={email} size="md" />
        <span className="menu__name">{displayName || email}</span>
        <IconChevronDown size={15} />
      </button>

      {open ? (
        // A popover of controls, not an ARIA menu: it holds an identity block, a
        // theme-switch group and action items, which is not the menuitem-only
        // structure role="menu" requires. The trigger's aria-haspopup/aria-expanded
        // and the Escape handling remain; dropping the role removes an invalid tree
        // rather than promising a menu contract the popover does not meet.
        <div className="menu__popover">
          <div className="menu__identity">
            <Avatar name={displayName} email={email} size="lg" />
            <div className="menu__identity-text">
              <div className="menu__identity-name truncate">{displayName || email}</div>
              {displayName ? <div className="menu__identity-mail truncate">{email}</div> : null}
            </div>
          </div>

          <div className="menu__separator" />

          <div className="menu__theme" role="group" aria-label={t('theme.label')}>
            {THEME_MODES.map((option) => {
              const Icon = THEME_ICON[option]
              return (
                <button
                  key={option}
                  type="button"
                  className={
                    option === mode
                      ? 'menu__theme-option menu__theme-option--active'
                      : 'menu__theme-option'
                  }
                  aria-pressed={option === mode}
                  onClick={() => setMode(option)}
                >
                  <Icon size={14} />
                  <span>{t(THEME_LABEL_KEY[option])}</span>
                </button>
              )
            })}
          </div>

          <div className="menu__separator" />

          <Link to="/account" className="menu__item" onClick={() => setOpen(false)}>
            <IconUser size={16} />
            {t('nav.account')}
          </Link>

          <button
            type="button"
            className="menu__item menu__item--danger"
            onClick={() => {
              setOpen(false)
              void signOut()
            }}
          >
            <IconLogOut size={16} />
            {t('common.signOut')}
          </button>
        </div>
      ) : null}
    </div>
  )
}

/**
 * A breadcrumb trail built from the current route, resolved against the
 * navigation model so each segment reuses the same label as the sidebar. A
 * record id at the end is shown as a generic "Details" crumb rather than a raw
 * UUID, which would mean nothing to a reader.
 */
function Breadcrumbs() {
  const location = useLocation()
  const { t } = useI18n()
  const { can } = useAuth()

  const crumbs = useMemo(() => {
    const path = location.pathname
    if (path === '/') return [{ to: '/', label: t('nav.overview'), current: true }]

    const segments = path.split('/').filter(Boolean)
    const trail: { to: string; label: string; current: boolean }[] = []
    let accumulated = ''

    for (const segment of segments) {
      accumulated += `/${segment}`
      const match = findEntry(accumulated, can)
      if (match) {
        trail.push({ to: accumulated, label: t(match.labelKey), current: false })
      } else if (/^[0-9a-f-]{8,}$/i.test(segment)) {
        trail.push({ to: accumulated, label: t('common.details'), current: false })
      } else {
        // An unmapped segment (e.g. "account") is title-cased so the trail still
        // reads as words rather than as a path fragment.
        trail.push({
          to: accumulated,
          label: segment.charAt(0).toUpperCase() + segment.slice(1).replace(/-/g, ' '),
          current: false,
        })
      }
    }
    if (trail.length > 0) trail[trail.length - 1].current = true
    return trail
  }, [location.pathname, t, can])

  if (crumbs.length === 0) return null

  return (
    <nav className="breadcrumbs" aria-label="Breadcrumb">
      {crumbs.map((crumb, index) => (
        <span key={crumb.to} style={{ display: 'contents' }}>
          {index > 0 ? (
            <span className="breadcrumbs__sep" aria-hidden="true">
              <IconChevronRight size={13} />
            </span>
          ) : null}
          {crumb.current ? (
            <span className="breadcrumbs__item breadcrumbs__item--current" aria-current="page">
              {crumb.label}
            </span>
          ) : (
            <Link to={crumb.to} className="breadcrumbs__item">
              {crumb.label}
            </Link>
          )}
        </span>
      ))}
    </nav>
  )
}

/** Finds the nav entry for a path, so a crumb reuses the sidebar's label. */
function findEntry(path: string, can: (permission: string) => boolean): NavEntry | undefined {
  return visibleNavGroups(can)
    .flatMap((group) => group.items)
    .find((entry) => entry.to === path)
}

/** The route-chunk loading fallback: a header and a block, to avoid a jump. */
function RouteFallback() {
  return (
    <div aria-busy="true">
      <div className="skeleton" style={{ width: 220, height: 24, marginBottom: 12 }} />
      <div className="skeleton" style={{ width: 320, height: 14, marginBottom: 24 }} />
      <Skeleton width="100%" height={220} />
    </div>
  )
}

export function AppShell() {
  const { session, can } = useAuth()
  const { t } = useI18n()
  const [menuOpen, setMenuOpen] = useState(false)
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return window.localStorage.getItem(COLLAPSE_KEY) === '1'
    } catch {
      return false
    }
  })
  const palette = useGlobalSearchShortcut()

  const groups = visibleNavGroups(can)

  function toggleCollapsed() {
    setCollapsed((value) => {
      const next = !value
      try {
        window.localStorage.setItem(COLLAPSE_KEY, next ? '1' : '0')
      } catch {
        // Preference is not persisted when storage is unavailable; the rail
        // still toggles for this session.
      }
      return next
    })
  }

  const sidebarClass = [
    'sidebar',
    menuOpen ? 'sidebar--open' : '',
    collapsed ? 'sidebar--collapsed' : '',
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <div className={collapsed ? 'app-shell app-shell--collapsed' : 'app-shell'}>
      <aside className={sidebarClass}>
        <Link to="/" className="sidebar__brand" onClick={() => setMenuOpen(false)}>
          <span className="brand-tile" aria-hidden="true">
            F
          </span>
          <span className="sidebar__brand-text">
            <span>{t('brand.name')}</span>
            <span className="sidebar__brand-sub">{t('brand.shortTagline')}</span>
          </span>
        </Link>

        <nav className="sidebar__nav" aria-label={t('nav.mainNavigation')}>
          {groups.map((group) => (
            <div key={group.labelKey}>
              <div className="sidebar__group-label">{t(group.labelKey)}</div>
              {group.items.map((item) => {
                const Icon = item.icon
                const label = t(item.labelKey)
                return (
                  <NavLink
                    key={item.to}
                    to={item.to}
                    end={item.to === '/'}
                    title={collapsed ? label : undefined}
                    className={({ isActive }) =>
                      isActive ? 'nav-item nav-item--active' : 'nav-item'
                    }
                    onClick={() => setMenuOpen(false)}
                  >
                    <Icon size={17} />
                    <span className="truncate">{label}</span>
                  </NavLink>
                )
              })}
            </div>
          ))}
        </nav>

        <div className="sidebar__footer">
          {session?.organization?.name ? (
            <div className="sidebar__footer-org">
              <IconLayers size={15} />
              <span className="truncate sidebar__footer-text">{session.organization.name}</span>
            </div>
          ) : null}
          <Tooltip label={collapsed ? t('nav.expand') : t('nav.collapse')}>
            <button
              type="button"
              className="btn btn--ghost btn--sm btn--icon sidebar-collapse"
              aria-label={collapsed ? t('nav.expand') : t('nav.collapse')}
              aria-pressed={collapsed}
              onClick={toggleCollapsed}
            >
              <IconPanelLeft size={17} />
            </button>
          </Tooltip>
        </div>
      </aside>

      {menuOpen ? (
        <button
          type="button"
          className="sidebar-scrim"
          aria-label={t('nav.closeMenu')}
          onClick={() => setMenuOpen(false)}
        />
      ) : null}

      <div className="main">
        <header className="topbar">
          <div className="topbar__lead">
            <button
              type="button"
              className="btn btn--ghost btn--sm btn--icon sidebar-toggle"
              aria-label={t('nav.menu')}
              aria-expanded={menuOpen}
              onClick={() => setMenuOpen((open) => !open)}
            >
              {menuOpen ? <IconClose size={18} /> : <IconMenu size={18} />}
            </button>
            <span className="topbar__brand">
              <span className="brand-tile" aria-hidden="true">
                F
              </span>
              <span className="topbar__brand-name">{t('brand.name')}</span>
            </span>
            <Breadcrumbs />
          </div>

          <div className="topbar__actions">
            <button
              type="button"
              className="search-trigger"
              onClick={() => palette.setOpen(true)}
              aria-keyshortcuts="Control+K Meta+K"
              aria-label={t('nav.search')}
            >
              <IconSearch size={15} />
              <span className="search-trigger__label">{t('nav.search')}</span>
              <kbd className="kbd">Ctrl K</kbd>
            </button>
            <AccountMenu />
          </div>
        </header>

        <main className="content">
          <Suspense fallback={<RouteFallback />}>
            <Outlet />
          </Suspense>
        </main>
      </div>

      {palette.open ? <CommandPalette onClose={() => palette.setOpen(false)} /> : null}
    </div>
  )
}
