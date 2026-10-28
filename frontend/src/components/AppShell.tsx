import { useState } from 'react'
import { NavLink, Outlet } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { GlobalSearch } from './GlobalSearch'
import { useGlobalSearchShortcut } from '../hooks/useGlobalSearch'

interface NavEntry {
  to: string
  label: string
  permission?: string
}

interface NavGroup {
  label: string
  items: NavEntry[]
}

/**
 * Navigation is grouped by intent rather than by API prefix, so the sidebar
 * reads as a product rather than as a route table. Each entry declares the
 * permission it needs and is hidden without it, but the server enforces the
 * same permission independently: hiding a link is convenience, not security.
 */
const NAV_GROUPS: NavGroup[] = [
  {
    label: 'Workspace',
    items: [
      { to: '/', label: 'Overview' },
      { to: '/accounts', label: 'Accounts', permission: 'accounts.read' },
      { to: '/contacts', label: 'Contacts', permission: 'contacts.read' },
      { to: '/leads', label: 'Leads', permission: 'leads.read' },
      { to: '/deals', label: 'Deals', permission: 'deals.read' },
      { to: '/tasks', label: 'Tasks', permission: 'tasks.read' },
      { to: '/tickets', label: 'Support', permission: 'tickets.read' },
      { to: '/reports', label: 'Reports', permission: 'reports.read' },
    ],
  },
  {
    label: 'Data',
    items: [
      { to: '/imports', label: 'Import Center', permission: 'imports.read' },
      { to: '/migrations', label: 'Migrations', permission: 'migrations.read' },
      { to: '/integrations', label: 'Integrations', permission: 'integrations.read' },
    ],
  },
  {
    label: 'Platform',
    items: [
      { to: '/webhooks', label: 'Webhooks', permission: 'webhooks.manage' },
      { to: '/events', label: 'Events', permission: 'events.read' },
      { to: '/jobs', label: 'Background jobs', permission: 'jobs.read' },
      { to: '/ai', label: 'AI & MCP', permission: 'ai.read' },
    ],
  },
  {
    label: 'Administration',
    items: [
      { to: '/admin/users', label: 'Users', permission: 'users.read' },
      { to: '/admin/roles', label: 'Roles', permission: 'roles.read' },
      { to: '/admin/organizations', label: 'Organizations', permission: 'organizations.read' },
      { to: '/admin/teams', label: 'Teams', permission: 'teams.manage' },
      { to: '/admin/email', label: 'Email', permission: 'email.manage' },
      { to: '/admin/telegram', label: 'Telegram', permission: 'email.manage' },
      { to: '/admin/audit', label: 'Audit log', permission: 'audit.read' },
      { to: '/admin/security', label: 'Security', permission: 'security.read' },
      { to: '/admin/diagnostics', label: 'Diagnostics', permission: 'logs.read' },
    ],
  },
]

export function AppShell() {
  const { session, signOut, can } = useAuth()
  const [menuOpen, setMenuOpen] = useState(false)
  const search = useGlobalSearchShortcut()

  const groups = NAV_GROUPS.map((group) => ({
    ...group,
    items: group.items.filter((item) => !item.permission || can(item.permission)),
  })).filter((group) => group.items.length > 0)

  return (
    <div className="app-shell">
      <aside className={menuOpen ? 'sidebar sidebar--open' : 'sidebar'}>
        <div className="sidebar__brand">
          <span className="sidebar__mark" aria-hidden="true">F</span>
          <span>ForgeCRM</span>
        </div>
        <nav aria-label="Main navigation">
          {groups.map((group) => (
            <div key={group.label}>
              <div className="sidebar__group-label">{group.label}</div>
              {group.items.map((item) => (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.to === '/'}
                  className={({ isActive }) => (isActive ? 'nav-item nav-item--active' : 'nav-item')}
                  onClick={() => setMenuOpen(false)}
                >
                  {item.label}
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
              aria-label="Toggle navigation"
              aria-expanded={menuOpen}
              onClick={() => setMenuOpen((open) => !open)}
            >
              Menu
            </button>
            <span className="topbar__org">
              {session?.organization?.name || 'ForgeCRM'}
            </span>
            <button
              type="button"
              className="btn btn--ghost btn--sm"
              onClick={() => search.setOpen(true)}
              aria-keyshortcuts="Control+K Meta+K"
            >
              Search
              <kbd className="kbd">Ctrl K</kbd>
            </button>
          </div>
          <div className="topbar__user">
            <div style={{ textAlign: 'right' }}>
              <div style={{ fontWeight: 500, fontSize: 13 }}>
                {session?.user?.display_name}
              </div>
              <div className="text-subtle" style={{ fontSize: 12 }}>
                {session?.user?.email}
              </div>
            </div>
            <NavLink to="/account" className="btn btn--ghost btn--sm">
              Account
            </NavLink>
            <button type="button" className="btn btn--secondary btn--sm" onClick={() => void signOut()}>
              Sign out
            </button>
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
