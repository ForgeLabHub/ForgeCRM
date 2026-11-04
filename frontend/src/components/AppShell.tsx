import { useEffect, useMemo, useRef, useState, Suspense } from 'react'
import type { JSX } from 'react'
import { Link, NavLink, Outlet } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { GlobalSearch } from './GlobalSearch'
import { useGlobalSearchShortcut } from '../hooks/useGlobalSearch'
import { useI18n, type TranslationKey } from '../i18n'
import { useTheme, THEME_MODES, type ThemeMode } from '../theme'
import { useWorkspace } from '../workspace/WorkspaceProvider'
import { NAV_GROUPS, navEntryFor, type NavEntry } from '../navigation'
import { WORKSPACE_DEFINITIONS, workspaceDefinition } from '../workspaces'
import { Avatar, Skeleton } from './ui'
import {
  IconChevronDown,
  IconClose,
  IconLayers,
  IconLogOut,
  IconMenu,
  IconMonitor,
  IconMoon,
  IconSearch,
  IconSliders,
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

/**
 * The account control in the top bar: identity, theme, profile and sign-out.
 *
 * It is a menu rather than a row of buttons so the top bar stays quiet and the
 * theme choice becomes a visible three-way switch instead of a control that
 * cycles to a state the user cannot see. It closes on Escape, on a click
 * outside, and on choosing an item.
 */
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
              {displayName ? <div className="menu__identity-mail">{email}</div> : null}
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

          <Link to="/appearance" className="menu__item" onClick={() => setOpen(false)}>
            <IconSliders size={16} />
            {t('nav.appearance')}
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
 * Fallback for a route whose code chunk is still loading. It mirrors the page
 * header and a content block so the shell does not collapse and then jump when
 * the real page arrives.
 */
function RouteFallback() {
  return (
    <div aria-busy="true">
      <div className="skeleton" style={{ width: 220, height: 24, marginBottom: 12 }} />
      <div className="skeleton" style={{ width: 320, height: 14, marginBottom: 24 }} />
      <Skeleton width="100%" height={220} />
    </div>
  )
}

/**
 * The workspace switcher in the top bar.
 *
 * It names the current workspace and lists the others with a one-line
 * description, so the choice is made by what each view is for rather than by
 * guessing from a name. It grants nothing: the shell filters every route through
 * the same permission check regardless of which workspace is active.
 */
function WorkspaceSwitcher() {
  const { t } = useI18n()
  const { workspace, setWorkspace } = useWorkspace()
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

  const current = workspaceDefinition(workspace)

  return (
    <div className="menu" ref={ref}>
      <button
        type="button"
        className="workspace-switcher"
        aria-haspopup="true"
        aria-expanded={open}
        aria-label={t('workspace.switchLabel')}
        onClick={() => setOpen((value) => !value)}
      >
        <span className="workspace-switcher__glyph" aria-hidden="true">{current.glyph}</span>
        <span className="workspace-switcher__text">
          <span className="workspace-switcher__sub">{t('workspace.label')}</span>
          <span className="workspace-switcher__name truncate">{t(current.labelKey)}</span>
        </span>
        <IconChevronDown size={15} />
      </button>

      {open ? (
        <div className="menu__popover workspace-menu" role="group" aria-label={t('workspace.switchLabel')}>
          <p className="workspace-menu__hint">{t('workspace.hint')}</p>
          {WORKSPACE_DEFINITIONS.map((definition) => (
            <button
              key={definition.id}
              type="button"
              className={
                definition.id === workspace
                  ? 'workspace-option workspace-option--active'
                  : 'workspace-option'
              }
              aria-pressed={definition.id === workspace}
              onClick={() => {
                setWorkspace(definition.id)
                setOpen(false)
              }}
            >
              <span className="workspace-option__glyph" aria-hidden="true">{definition.glyph}</span>
              <span className="workspace-option__text">
                <span className="workspace-option__name">{t(definition.labelKey)}</span>
                <span className="workspace-option__desc">{t(definition.descriptionKey)}</span>
              </span>
            </button>
          ))}
        </div>
      ) : null}
    </div>
  )
}

interface RenderGroup {
  key: string
  labelKey: TranslationKey
  items: NavEntry[]
  reorder: boolean
}

export function AppShell() {
  const { session, can } = useAuth()
  const { t } = useI18n()
  const { workspace } = useWorkspace()
  const [menuOpen, setMenuOpen] = useState(false)
  const search = useGlobalSearchShortcut()

  const definition = workspaceDefinition(workspace)
  // The order is local state so the up/down controls can rearrange it for this
  // session; it is seeded from the workspace definition and re-seeded when the
  // workspace changes, so switching away and back restores the intended order.
  const [order, setOrder] = useState<string[]>(definition.routes)
  useEffect(() => {
    setOrder(workspaceDefinition(workspace).routes)
  }, [workspace])

  const moveRoute = (route: string, delta: -1 | 1) =>
    setOrder((previous) => {
      const from = previous.indexOf(route)
      const to = from + delta
      if (from < 0 || to < 0 || to >= previous.length) return previous
      const next = [...previous]
      ;[next[from], next[to]] = [next[to], next[from]]
      return next
    })

  const groups = useMemo<RenderGroup[]>(() => {
    const allowed = (item: NavEntry) => !item.permission || can(item.permission)
    if (workspace === 'all') {
      // The ungrouped product view: the full, grouped navigation, unchanged.
      return NAV_GROUPS.map((group) => ({
        key: group.labelKey,
        labelKey: group.labelKey,
        items: group.items.filter(allowed),
        reorder: false,
      })).filter((group) => group.items.length > 0)
    }
    // A workspace is one flat list in the order the role cares about. Routes the
    // user cannot read are dropped, so a workspace never shows a dead link.
    const items = order
      .map((route) => navEntryFor(route))
      .filter((entry): entry is NavEntry => entry !== undefined)
      .filter(allowed)
    return items.length > 0
      ? [{ key: definition.labelKey, labelKey: definition.labelKey, items, reorder: true }]
      : []
  }, [workspace, order, can, definition.labelKey])

  return (
    <div className="app-shell">
      <aside className={menuOpen ? 'sidebar sidebar--open' : 'sidebar'}>
        <Link to="/" className="sidebar__brand" onClick={() => setMenuOpen(false)}>
          <Avatar name="ForgeCRM" size="md" />
          <span className="sidebar__brand-text">
            <span>{t('brand.name')}</span>
            <span className="sidebar__brand-sub">{t('brand.shortTagline')}</span>
          </span>
        </Link>
        <nav className="sidebar__nav" aria-label={t('nav.mainNavigation')}>
          {groups.map((group) => (
            <div key={group.key}>
              <div className="sidebar__group-label">{t(group.labelKey)}</div>
              {group.items.map((item, index) => {
                const Icon = item.icon
                return (
                  <div key={item.to} className="nav-row">
                    <NavLink
                      to={item.to}
                      end={item.to === '/'}
                      className={({ isActive }) => (isActive ? 'nav-item nav-item--active' : 'nav-item')}
                      onClick={() => setMenuOpen(false)}
                    >
                      <Icon size={17} />
                      <span className="truncate">{t(item.labelKey)}</span>
                    </NavLink>
                    {group.reorder ? (
                      <span className="nav-row__controls">
                        <button
                          type="button"
                          className="nav-row__move"
                          aria-label={t('workspace.moveUp', { name: t(item.labelKey) })}
                          disabled={index === 0}
                          onClick={() => moveRoute(item.to, -1)}
                        >
                          ▲
                        </button>
                        <button
                          type="button"
                          className="nav-row__move"
                          aria-label={t('workspace.moveDown', { name: t(item.labelKey) })}
                          disabled={index === group.items.length - 1}
                          onClick={() => moveRoute(item.to, 1)}
                        >
                          ▼
                        </button>
                      </span>
                    ) : null}
                  </div>
                )
              })}
            </div>
          ))}
        </nav>
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
          <div className="topbar__actions">
            <button
              type="button"
              className="btn btn--ghost btn--sm sidebar-toggle"
              aria-label={t('nav.menu')}
              aria-expanded={menuOpen}
              onClick={() => setMenuOpen((open) => !open)}
            >
              {menuOpen ? <IconClose size={18} /> : <IconMenu size={18} />}
            </button>
            {/* The wordmark only appears once the sidebar is a drawer: on a wide
                screen the brand already sits at the top of the rail, and showing
                it twice would read as two different things. */}
            <span className="topbar__brand">
              <Avatar name="ForgeCRM" size="sm" />
              <span className="topbar__brand-name">{t('brand.name')}</span>
            </span>
            <button
              type="button"
              className="search-trigger"
              onClick={() => search.setOpen(true)}
              aria-label={t('nav.search')}
              aria-keyshortcuts="Control+K Meta+K"
            >
              <IconSearch size={15} />
              <span className="search-trigger__label">{t('nav.search')}</span>
              <kbd className="kbd">Ctrl K</kbd>
            </button>
            {session?.organization?.name ? (
              <span className="topbar__org">
                <IconLayers size={14} />
                <span className="topbar__org-name">{session.organization.name}</span>
              </span>
            ) : null}
          </div>

          <div className="topbar__user">
            <WorkspaceSwitcher />
            <AccountMenu />
          </div>
        </header>

        <main className="content">
          <Suspense fallback={<RouteFallback />}>
            <Outlet />
          </Suspense>
        </main>
      </div>

      {search.open ? <GlobalSearch onClose={() => search.setOpen(false)} /> : null}
    </div>
  )
}
