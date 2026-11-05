/**
 * The classic shell: navigation rail on the left, utility top bar above the
 * content. This is the anatomy ForgeCRM shipped with, preserved unchanged as
 * one of the experiences — the account menu, workspace switcher, search and
 * nav model it renders are the shared ones from `./common`, so it drifts from
 * the other shells only in arrangement, never in behaviour.
 */
import { useEffect, useRef, useState } from 'react'
import { Link, NavLink, useLocation } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { useI18n } from '../i18n'
import { Avatar } from '../components/ui'
import { IconClose, IconLayers, IconMenu, IconSearch } from '../components/icons'
import {
  AccountMenu,
  SearchPaletteLayer,
  ShellCanvas,
  WorkspaceSwitcher,
  useNavModel,
  useShellSearch,
} from './common'

export function ClassicShell() {
  const { session } = useAuth()
  const { t } = useI18n()
  const [menuOpen, setMenuOpen] = useState(false)
  const [drawerLayout, setDrawerLayout] = useState(
    () => typeof window !== 'undefined' && window.matchMedia?.('(max-width: 860px)').matches === true,
  )
  const sidebarRef = useRef<HTMLElement>(null)
  const toggleRef = useRef<HTMLButtonElement>(null)
  const location = useLocation()
  const search = useShellSearch()
  const { groups, moveRoute } = useNavModel()

  // The off-screen drawer must not leave links in the keyboard or screen-reader
  // navigation order. Match the same breakpoint as the CSS, including resizing.
  useEffect(() => {
    if (!window.matchMedia) return
    const media = window.matchMedia('(max-width: 860px)')
    const update = () => {
      setDrawerLayout(media.matches)
      if (!media.matches) setMenuOpen(false)
    }
    update()
    media.addEventListener('change', update)
    return () => media.removeEventListener('change', update)
  }, [])

  // Browser back/forward, navigation and the global search shortcut must not
  // leave a second modal drawer open behind a new page or search dialog.
  useEffect(() => { setMenuOpen(false) }, [location.pathname])
  useEffect(() => { if (search.open) setMenuOpen(false) }, [search.open])

  useEffect(() => {
    if (!drawerLayout || !menuOpen) return
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    const drawer = sidebarRef.current
    const toggle = toggleRef.current
    drawer?.focus()
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        setMenuOpen(false)
        return
      }
      if (event.key !== 'Tab' || !drawer) return
      const controls = drawer.querySelectorAll<HTMLElement>('a[href], button:not([disabled]), [tabindex]:not([tabindex="-1"])')
      if (!controls.length) { event.preventDefault(); return }
      const first = controls[0]
      const last = controls[controls.length - 1]
      if (event.shiftKey && (document.activeElement === first || document.activeElement === drawer)) {
        event.preventDefault(); last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault(); first.focus()
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('keydown', onKeyDown)
      document.body.style.overflow = previousOverflow
      // Only return focus when it was still inside the drawer; do not steal it
      // from a newly opened page or dialog after a navigation.
      if (drawer?.contains(document.activeElement)) toggle?.focus()
    }
  }, [drawerLayout, menuOpen])

  return (
    <div className="app-shell">
      <aside
        id="main-navigation-drawer"
        ref={sidebarRef}
        className={menuOpen ? 'sidebar sidebar--open' : 'sidebar'}
        role={drawerLayout && menuOpen ? 'dialog' : undefined}
        aria-modal={drawerLayout && menuOpen ? true : undefined}
        aria-label={drawerLayout && menuOpen ? t('nav.mainNavigation') : undefined}
        aria-hidden={drawerLayout && !menuOpen ? true : undefined}
        inert={drawerLayout && !menuOpen ? true : undefined}
        tabIndex={-1}
      >
        <Link to="/" className="sidebar__brand" onClick={() => setMenuOpen(false)}>
          <Avatar name="ForgeCRM" size="md" />
          <span className="sidebar__brand-text">
            <span>{t('brand.name')}</span>
            <span className="sidebar__brand-sub">{t('brand.shortTagline')}</span>
          </span>
        </Link>
        <button type="button" className="sidebar__close" aria-label={t('nav.closeMenu')} onClick={() => setMenuOpen(false)}>
          <IconClose size={20} />
        </button>
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
              ref={toggleRef}
              aria-controls="main-navigation-drawer"
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

        <ShellCanvas />
      </div>

      <SearchPaletteLayer open={search.open} onClose={() => search.setOpen(false)} />
    </div>
  )
}
