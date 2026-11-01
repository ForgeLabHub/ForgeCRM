import { useEffect, useRef, useState } from 'react'
import type { JSX } from 'react'
import { Link, NavLink, Outlet } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { GlobalSearch } from './GlobalSearch'
import { useGlobalSearchShortcut } from '../hooks/useGlobalSearch'
import { useI18n, type TranslationKey } from '../i18n'
import { useTheme, THEME_MODES, type ThemeMode } from '../theme'
import { Avatar } from './ui'
import {
  IconActivity,
  IconBadgeCheck,
  IconBell,
  IconBuilding,
  IconChart,
  IconCheckSquare,
  IconChevronDown,
  IconClipboardCheck,
  IconClock,
  IconClose,
  IconDatabase,
  IconFlask,
  IconGlobe,
  IconGrid,
  IconInbox,
  IconKanban,
  IconKeyRound,
  IconLayers,
  IconLifeBuoy,
  IconLock,
  IconLogOut,
  IconMail,
  IconMenu,
  IconMonitor,
  IconMoon,
  IconPlug,
  IconScroll,
  IconSearch,
  IconSend,
  IconShield,
  IconShieldAlert,
  IconSliders,
  IconSparkles,
  IconStethoscope,
  IconSun,
  IconTarget,
  IconTerminal,
  IconTrendingUp,
  IconUser,
  IconUserCog,
  IconUsers,
  IconUsersRound,
  IconWebhook,
  IconWorkflow,
  type IconProps,
} from './icons'

interface NavEntry {
  to: string
  /** Translation key for the label, resolved at render so a language change
   *  relabels the whole sidebar without a reload. */
  labelKey: TranslationKey
  permission?: string
  icon: (props: IconProps) => JSX.Element
}

interface NavGroup {
  labelKey: TranslationKey
  items: NavEntry[]
}

/**
 * Navigation is grouped by intent rather than by API prefix, so the sidebar
 * reads as a product rather than as a route table. Each entry declares the
 * permission it needs and is hidden without it, but the server enforces the
 * same permission independently: hiding a link is convenience, not security.
 *
 * Labels are translation keys, not literals, so switching language relabels the
 * navigation as one unit instead of leaving half the sidebar in the old tongue.
 * Every entry carries an icon so the eye can find a section by shape before it
 * reads the label.
 */
const NAV_GROUPS: NavGroup[] = [
  {
    labelKey: 'nav.workspace',
    items: [
      { to: '/', labelKey: 'nav.overview', icon: IconGrid },
      { to: '/accounts', labelKey: 'nav.accounts', permission: 'accounts.read', icon: IconBuilding },
      { to: '/contacts', labelKey: 'nav.contacts', permission: 'contacts.read', icon: IconUsers },
      { to: '/leads', labelKey: 'nav.leads', permission: 'leads.read', icon: IconTarget },
      { to: '/deals', labelKey: 'nav.deals', permission: 'deals.read', icon: IconKanban },
      { to: '/tasks', labelKey: 'nav.tasks', permission: 'tasks.read', icon: IconCheckSquare },
      { to: '/tickets', labelKey: 'nav.support', permission: 'tickets.read', icon: IconLifeBuoy },
      { to: '/reports', labelKey: 'nav.reports', permission: 'reports.read', icon: IconChart },
      { to: '/simulation', labelKey: 'nav.simulation', permission: 'simulation.read', icon: IconFlask },
      { to: '/forecast', labelKey: 'nav.forecasting', permission: 'forecast.read', icon: IconTrendingUp },
      { to: '/risk', labelKey: 'nav.risk', permission: 'risk.read', icon: IconShieldAlert },
    ],
  },
  {
    labelKey: 'nav.data',
    items: [
      { to: '/imports', labelKey: 'nav.importCenter', permission: 'imports.read', icon: IconInbox },
      { to: '/migrations', labelKey: 'nav.migrations', permission: 'migrations.read', icon: IconDatabase },
      { to: '/integrations', labelKey: 'nav.integrations', permission: 'integrations.read', icon: IconPlug },
    ],
  },
  {
    labelKey: 'nav.platform',
    items: [
      { to: '/webhooks', labelKey: 'nav.webhooks', permission: 'webhooks.manage', icon: IconWebhook },
      { to: '/events', labelKey: 'nav.events', permission: 'events.read', icon: IconActivity },
      { to: '/workflows', labelKey: 'nav.workflows', permission: 'workflows.read', icon: IconWorkflow },
      { to: '/jobs', labelKey: 'nav.jobs', permission: 'jobs.read', icon: IconClock },
      { to: '/approvals', labelKey: 'nav.approvals', permission: 'approvals.read', icon: IconBadgeCheck },
      { to: '/notifications', labelKey: 'nav.notifications', permission: 'notifications.read', icon: IconBell },
      { to: '/ai', labelKey: 'nav.ai', permission: 'ai.read', icon: IconSparkles },
    ],
  },
  {
    labelKey: 'nav.administration',
    items: [
      { to: '/admin/users', labelKey: 'nav.users', permission: 'users.read', icon: IconUserCog },
      { to: '/admin/roles', labelKey: 'nav.roles', permission: 'roles.read', icon: IconKeyRound },
      { to: '/admin/organizations', labelKey: 'nav.organizations', permission: 'organizations.read', icon: IconGlobe },
      { to: '/admin/teams', labelKey: 'nav.teams', permission: 'teams.manage', icon: IconUsersRound },
      { to: '/admin/email', labelKey: 'nav.email', permission: 'email.manage', icon: IconMail },
      { to: '/admin/telegram', labelKey: 'nav.telegram', permission: 'email.manage', icon: IconSend },
      { to: '/admin/consent', labelKey: 'nav.consent', permission: 'consent.read', icon: IconScroll },
      { to: '/admin/sso', labelKey: 'nav.sso', permission: 'sso.read', icon: IconLock },
      { to: '/admin/mcp-credentials', labelKey: 'nav.mcpCredentials', permission: 'mcp.credentials.read', icon: IconTerminal },
      { to: '/admin/configuration', labelKey: 'nav.configuration', permission: 'tenant.settings.read', icon: IconSliders },
      { to: '/admin/audit', labelKey: 'nav.audit', permission: 'audit.read', icon: IconClipboardCheck },
      { to: '/admin/security', labelKey: 'nav.security', permission: 'security.read', icon: IconShield },
      { to: '/admin/diagnostics', labelKey: 'nav.diagnostics', permission: 'logs.read', icon: IconStethoscope },
    ],
  },
]

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
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={t('nav.account')}
        onClick={() => setOpen((value) => !value)}
      >
        <Avatar name={displayName} email={email} size="md" />
        <span className="menu__name">{displayName || email}</span>
        <IconChevronDown size={15} />
      </button>

      {open ? (
        <div className="menu__popover" role="menu">
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

          <Link to="/account" className="menu__item" role="menuitem" onClick={() => setOpen(false)}>
            <IconUser size={16} />
            {t('nav.account')}
          </Link>

          <button
            type="button"
            className="menu__item menu__item--danger"
            role="menuitem"
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

export function AppShell() {
  const { session, can } = useAuth()
  const { t } = useI18n()
  const [menuOpen, setMenuOpen] = useState(false)
  const search = useGlobalSearchShortcut()

  const groups = NAV_GROUPS.map((group) => ({
    ...group,
    items: group.items.filter((item) => !item.permission || can(item.permission)),
  })).filter((group) => group.items.length > 0)

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
            <div key={group.labelKey}>
              <div className="sidebar__group-label">{t(group.labelKey)}</div>
              {group.items.map((item) => {
                const Icon = item.icon
                return (
                  <NavLink
                    key={item.to}
                    to={item.to}
                    end={item.to === '/'}
                    className={({ isActive }) => (isActive ? 'nav-item nav-item--active' : 'nav-item')}
                    onClick={() => setMenuOpen(false)}
                  >
                    <Icon size={17} />
                    <span className="truncate">{t(item.labelKey)}</span>
                  </NavLink>
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
            <button
              type="button"
              className="search-trigger"
              onClick={() => search.setOpen(true)}
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
            <AccountMenu />
          </div>
        </header>

        <main className="content">
          <Outlet />
        </main>
      </div>

      {search.open ? <GlobalSearch onClose={() => search.setOpen(false)} /> : null}
    </div>
  )
}
