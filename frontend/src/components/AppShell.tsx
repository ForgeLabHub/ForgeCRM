import { useEffect, useState } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { GlobalSearch } from './GlobalSearch'
import { useGlobalSearchShortcut } from '../hooks/useGlobalSearch'
import { BrandMark } from './BrandMark'
import { NAV_ICONS } from './NavIcons'
import { useI18n, type TranslationKey } from '../i18n'
import { useTheme, type ThemeMode } from '../theme'

interface NavEntry {
  to: string
  /** Translation key for the label, resolved at render so a language change
   *  relabels the whole sidebar without a reload. */
  labelKey: TranslationKey
  permission?: string
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
 */
const NAV_GROUPS: NavGroup[] = [
  {
    labelKey: 'nav.workspace',
    items: [
      { to: '/', labelKey: 'nav.overview' },
      { to: '/accounts', labelKey: 'nav.accounts', permission: 'accounts.read' },
      { to: '/contacts', labelKey: 'nav.contacts', permission: 'contacts.read' },
      { to: '/leads', labelKey: 'nav.leads', permission: 'leads.read' },
      { to: '/deals', labelKey: 'nav.deals', permission: 'deals.read' },
      { to: '/tasks', labelKey: 'nav.tasks', permission: 'tasks.read' },
      { to: '/tickets', labelKey: 'nav.support', permission: 'tickets.read' },
      { to: '/reports', labelKey: 'nav.reports', permission: 'reports.read' },
      { to: '/simulation', labelKey: 'nav.simulation', permission: 'simulation.read' },
      { to: '/forecast', labelKey: 'nav.forecasting', permission: 'forecast.read' },
      { to: '/risk', labelKey: 'nav.risk', permission: 'risk.read' },
    ],
  },
  {
    labelKey: 'nav.data',
    items: [
      { to: '/imports', labelKey: 'nav.importCenter', permission: 'imports.read' },
      { to: '/migrations', labelKey: 'nav.migrations', permission: 'migrations.read' },
      { to: '/integrations', labelKey: 'nav.integrations', permission: 'integrations.read' },
    ],
  },
  {
    labelKey: 'nav.platform',
    items: [
      { to: '/webhooks', labelKey: 'nav.webhooks', permission: 'webhooks.manage' },
      { to: '/events', labelKey: 'nav.events', permission: 'events.read' },
      { to: '/workflows', labelKey: 'nav.workflows', permission: 'workflows.read' },
      { to: '/jobs', labelKey: 'nav.jobs', permission: 'jobs.read' },
      { to: '/approvals', labelKey: 'nav.approvals', permission: 'approvals.read' },
      { to: '/notifications', labelKey: 'nav.notifications', permission: 'notifications.read' },
      { to: '/ai', labelKey: 'nav.ai', permission: 'ai.read' },
    ],
  },
  {
    labelKey: 'nav.administration',
    items: [
      { to: '/admin/users', labelKey: 'nav.users', permission: 'users.read' },
      { to: '/admin/roles', labelKey: 'nav.roles', permission: 'roles.read' },
      { to: '/admin/organizations', labelKey: 'nav.organizations', permission: 'organizations.read' },
      { to: '/admin/teams', labelKey: 'nav.teams', permission: 'teams.manage' },
      { to: '/admin/email', labelKey: 'nav.email', permission: 'email.manage' },
      { to: '/admin/telegram', labelKey: 'nav.telegram', permission: 'email.manage' },
      { to: '/admin/consent', labelKey: 'nav.consent', permission: 'consent.read' },
      { to: '/admin/sso', labelKey: 'nav.sso', permission: 'sso.read' },
      { to: '/admin/mcp-credentials', labelKey: 'nav.mcpCredentials', permission: 'mcp.credentials.read' },
      { to: '/admin/configuration', labelKey: 'nav.configuration', permission: 'tenant.settings.read' },
      { to: '/admin/audit', labelKey: 'nav.audit', permission: 'audit.read' },
      { to: '/admin/security', labelKey: 'nav.security', permission: 'security.read' },
      { to: '/admin/diagnostics', labelKey: 'nav.diagnostics', permission: 'logs.read' },
    ],
  },
]

const THEME_ICON: Record<ThemeMode, string> = { light: '☀', dark: '☾', system: '◐' }

/** The initials shown in the avatar: the display name, or the email's first letter. */
function initials(name?: string, email?: string): string {
  const source = (name ?? '').trim() || (email ?? '').trim()
  if (!source) return '?'
  const parts = source.split(/\s+/).filter(Boolean)
  if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase()
  return source.slice(0, 2).toUpperCase()
}

export function AppShell() {
  const { session, signOut, can } = useAuth()
  const { t } = useI18n()
  const { mode, cycle } = useTheme()
  const [menuOpen, setMenuOpen] = useState(false)
  const search = useGlobalSearchShortcut()
  const location = useLocation()

  // The drawer is a fixed overlay on a phone. Navigating away must close it, and
  // Escape must close it, or it would cover the page the user just opened.
  useEffect(() => {
    setMenuOpen(false)
  }, [location.pathname])

  useEffect(() => {
    if (!menuOpen) return
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setMenuOpen(false)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [menuOpen])

  const groups = NAV_GROUPS.map((group) => ({
    ...group,
    items: group.items.filter((item) => !item.permission || can(item.permission)),
  })).filter((group) => group.items.length > 0)

  return (
    <div className="app-shell">
      <a className="skip-link" href="#main-content">
        {t('nav.skipToContent')}
      </a>

      {menuOpen ? (
        <div className="sidebar-backdrop" aria-hidden="true" onClick={() => setMenuOpen(false)} />
      ) : null}

      <aside className={menuOpen ? 'sidebar sidebar--open' : 'sidebar'}>
        <div className="sidebar__brand">
          <BrandMark size={28} />
          <span>{t('brand.name')}</span>
        </div>
        <nav aria-label={t('nav.mainNavigation')}>
          {groups.map((group) => (
            <div key={group.labelKey}>
              <div className="sidebar__group-label">{t(group.labelKey)}</div>
              {group.items.map((item) => (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.to === '/'}
                  className={({ isActive }) => (isActive ? 'nav-item nav-item--active' : 'nav-item')}
                  onClick={() => setMenuOpen(false)}
                >
                  {NAV_ICONS[item.to] ? (
                    <span className="nav-item__icon">{NAV_ICONS[item.to]}</span>
                  ) : null}
                  {t(item.labelKey)}
                </NavLink>
              ))}
            </div>
          ))}
        </nav>
      </aside>

      <div className="main">
        <header className="topbar">
          <div className="row">
            <button
              type="button"
              className="btn btn--ghost btn--sm sidebar-toggle"
              aria-label={t('nav.menu')}
              aria-expanded={menuOpen}
              onClick={() => setMenuOpen((open) => !open)}
            >
              <MenuIcon />
            </button>
            <span className="topbar__org">
              {session?.organization?.name || t('brand.name')}
            </span>
            <button
              type="button"
              className="btn btn--ghost btn--sm"
              onClick={() => search.setOpen(true)}
              aria-keyshortcuts="Control+K Meta+K"
            >
              <SearchIcon />
              <span className="topbar__search-label">{t('nav.search')}</span>
              <kbd className="kbd">Ctrl K</kbd>
            </button>
          </div>
          <div className="topbar__user">
            <button
              type="button"
              className="btn btn--ghost btn--sm theme-toggle"
              onClick={cycle}
              aria-label={t('theme.cycle', { mode: t(`theme.${mode}` as 'theme.light') })}
              title={t('theme.cycle', { mode: t(`theme.${mode}` as 'theme.light') })}
            >
              <span aria-hidden="true">{THEME_ICON[mode]}</span>
              <span className="theme-toggle__text">{t(`theme.${mode}` as 'theme.light')}</span>
            </button>
            <NavLink to="/account" className="topbar__user-link">
              <span className="avatar" aria-hidden="true">
                {initials(session?.user?.display_name, session?.user?.email)}
              </span>
              <span className="topbar__user-meta">
                <span className="topbar__user-name">{session?.user?.display_name}</span>
                <span className="topbar__user-email">{session?.user?.email}</span>
              </span>
            </NavLink>
            <button type="button" className="btn btn--secondary btn--sm" onClick={() => void signOut()}>
              {t('common.signOut')}
            </button>
          </div>
        </header>

        <main className="content" id="main-content">
          <Outlet />
        </main>
      </div>

      {search.open ? <GlobalSearch onClose={() => search.setOpen(false)} /> : null}
    </div>
  )
}

function MenuIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true">
      <path d="M4 7h16M4 12h16M4 17h16" />
    </svg>
  )
}

function SearchIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true">
      <circle cx="11" cy="11" r="7" />
      <path d="M20 20l-3.2-3.2" />
    </svg>
  )
}
