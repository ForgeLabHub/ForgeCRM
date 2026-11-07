/**
 * Stratus — the cloud-suite experience, in the pattern Salesforce Lightning
 * made the default shape of enterprise CRM.
 *
 * The anatomy: a gradient brand header holding the app launcher, the workspace
 * name, search and the account; a second strip of object tabs — the routes of
 * the active workspace — directly beneath it, so moving between records is a
 * one-tap, always-visible act; and a utility bar pinned to the bottom edge for
 * the handful of actions (notifications, tasks, the AI console) that belong
 * everywhere regardless of which tab is open.
 *
 * The app launcher is the signature piece: one waffle button opens a panel
 * listing every section of the product, grouped the way the classic sidebar
 * groups them, so the full app is two clicks deep and the tab strip can stay
 * short and job-shaped.
 */
import { useEffect, useRef, useState } from 'react'
import { Link, NavLink, useLocation } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { useI18n } from '../i18n'
import { NAV_GROUPS } from '../navigation'
import {
  AccountMenu,
  MobileNavDrawer,
  MobileWorkspacePicker,
  SearchPaletteLayer,
  ShellCanvas,
  WorkspaceSwitcher,
  useAllowedEntries,
  useMediaQuery,
  useNavModel,
  useShellSearch,
} from './common'
import { IconMenu, IconSearch } from '../components/icons'

/** Quick actions pinned to the utility bar, in priority order. */
const UTILITY_ROUTES = ['/app/notifications', '/app/tasks', '/app/ai', '/app/approvals']

/** The waffle: a 3×3 dot grid, the cloud-suite convention for "everything". */
function LauncherGlyph() {
  return (
    <span className="stratus-launcher__glyph" aria-hidden="true">
      {Array.from({ length: 9 }, (_, i) => (
        <i key={i} />
      ))}
    </span>
  )
}

/**
 * The launcher panel: every section, grouped, with a short description per
 * group taken from the group's role. Closes like a popover — Escape, outside
 * click, or choosing a destination.
 */
function AppLauncher({ onClose }: { onClose: () => void }) {
  const { can } = useAuth()
  const { t } = useI18n()
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const onPointerDown = (event: MouseEvent) => {
      if (ref.current && !ref.current.contains(event.target as Node)) onClose()
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [onClose])

  return (
    <div className="stratus-launcher__panel" ref={ref} role="group" aria-label={t('shell.launcher')}>
      <p className="stratus-launcher__hint">{t('shell.launcherHint')}</p>
      <div className="stratus-launcher__grid">
        {NAV_GROUPS.map((group) => {
          const items = group.items.filter((item) => !item.permission || can(item.permission))
          if (items.length === 0) return null
          return (
            <section key={group.labelKey} className="stratus-launcher__section">
              <h3 className="stratus-launcher__heading">{t(group.labelKey)}</h3>
              <div className="stratus-launcher__items">
                {items.map((item) => {
                  const Icon = item.icon
                  return (
                    <Link key={item.to} to={item.to} className="stratus-launcher__item" onClick={onClose}>
                      <span className="stratus-launcher__tile" aria-hidden="true">
                        <Icon size={16} />
                      </span>
                      <span className="truncate">{t(item.labelKey)}</span>
                    </Link>
                  )
                })}
              </div>
            </section>
          )
        })}
      </div>
    </div>
  )
}

export function StratusShell() {
  const { t } = useI18n()
  const location = useLocation()
  const search = useShellSearch()
  const { groups } = useNavModel()
  const allowed = useAllowedEntries()
  const isPhone = useMediaQuery('(max-width: 900px)')
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [launcherOpen, setLauncherOpen] = useState(false)
  // The tab strip leads with the active workspace's routes — in the ungrouped
  // product view that is the main record group — and the launcher holds the
  // rest, so the strip stays short enough to scan.
  const tabs = groups[0]?.items ?? []

  useEffect(() => {
    setLauncherOpen(false)
    setDrawerOpen(false)
  }, [location.pathname])
  useEffect(() => {
    if (search.open) {
      setLauncherOpen(false)
      setDrawerOpen(false)
    }
  }, [search.open])

  const utilities = UTILITY_ROUTES
    .map((route) => allowed.find((entry) => entry.to === route))
    .filter((entry): entry is NonNullable<typeof entry> => entry !== undefined)

  return (
    <div className="shell stratus">
      <header className="stratus-header">
        <div className="stratus-header__row">
          {isPhone ? (
            <button
              type="button"
              className="stratus-iconbtn"
              aria-label={t('nav.menu')}
              aria-expanded={drawerOpen}
              onClick={() => setDrawerOpen(true)}
            >
              <IconMenu size={19} />
            </button>
          ) : (
            <div className="stratus-launcher">
              <button
                type="button"
                className="stratus-iconbtn stratus-launcher__trigger"
                aria-label={t('shell.launcher')}
                aria-haspopup="true"
                aria-expanded={launcherOpen}
                onClick={() => setLauncherOpen((open) => !open)}
              >
                <LauncherGlyph />
              </button>
              {launcherOpen ? <AppLauncher onClose={() => setLauncherOpen(false)} /> : null}
            </div>
          )}

          <div className="stratus-brand">
            <span className="stratus-brand__cloud" aria-hidden="true" />
            <NavLink to="/app" end className="stratus-brand__name">{t('brand.name')}</NavLink>
          </div>

          {/* The workspace menu rides in the header like the app menu in a
              cloud suite: it names the current view and switches to another. */}
          {!isPhone ? <WorkspaceSwitcher /> : null}

          <div className="stratus-header__spacer" />

          <button
            type="button"
            className="stratus-search"
            onClick={() => search.setOpen(true)}
            aria-label={t('nav.search')}
            aria-keyshortcuts="Control+K Meta+K"
          >
            <IconSearch size={14} />
            <span className="stratus-search__label">{t('nav.search')}</span>
            <kbd className="kbd">Ctrl K</kbd>
          </button>

          <AccountMenu avatarOnly />
        </div>

        {!isPhone ? (
          <nav className="stratus-tabs" aria-label={t('nav.mainNavigation')}>
            {tabs.map((item) => {
              const Icon = item.icon
              return (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.to === '/'}
                  className={({ isActive }) => (isActive ? 'stratus-tab stratus-tab--active' : 'stratus-tab')}
                >
                  <Icon size={15} />
                  <span>{t(item.labelKey)}</span>
                </NavLink>
              )
            })}
          </nav>
        ) : null}
      </header>

      <MobileNavDrawer
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        groups={groups}
        header={<MobileWorkspacePicker onPick={() => setDrawerOpen(false)} />}
      />

      <ShellCanvas className="content--stratus" />

      {utilities.length > 0 && !isPhone ? (
        <nav className="stratus-utility" aria-label={t('shell.quickLinks')}>
          {utilities.map((entry) => {
            const Icon = entry.icon
            return (
              <NavLink
                key={entry.to}
                to={entry.to}
                className={({ isActive }) => (isActive ? 'stratus-utility__item stratus-utility__item--active' : 'stratus-utility__item')}
              >
                <Icon size={14} />
                <span>{t(entry.labelKey)}</span>
              </NavLink>
            )
          })}
        </nav>
      ) : null}

      <SearchPaletteLayer open={search.open} onClose={() => search.setOpen(false)} />
    </div>
  )
}
