/**
 * Slate — the command-first experience in the Linear mould. One slim, dark,
 * quiet sidebar: the workspace at the top, then every section in compact
 * small-caps groups, then the account docked at the foot. The top bar is not
 * chrome but context — a breadcrumb naming the current section and the one
 * verb this interface is built around, the search palette (Ctrl K), rendered
 * as a keyhint rather than a button.
 *
 * Everything is a touch smaller and tighter than the classic shell, content is
 * centred into a focused column, and the palette is permanently dark with the
 * signature iris accent — an interface that gets out of the way and lets the
 * records be the bright thing on screen.
 */
import { useEffect, useState } from 'react'
import { Link, NavLink, useLocation } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { useI18n } from '../i18n'
import {
  AccountMenu,
  MobileNavDrawer,
  SearchPaletteLayer,
  ShellCanvas,
  WorkspaceSwitcher,
  useCurrentEntry,
  useMediaQuery,
  useNavModel,
  useShellSearch,
} from './common'
import { IconChevronDown, IconMenu, IconSearch } from '../components/icons'

export function SlateShell() {
  const { session } = useAuth()
  const { t } = useI18n()
  const location = useLocation()
  const search = useShellSearch()
  const { groups } = useNavModel()
  const current = useCurrentEntry()
  const isPhone = useMediaQuery('(max-width: 900px)')
  const [drawerOpen, setDrawerOpen] = useState(false)

  useEffect(() => { setDrawerOpen(false) }, [location.pathname])
  useEffect(() => { if (search.open) setDrawerOpen(false) }, [search.open])

  const sidebar = (
    <aside className="slate-side">
      <div className="slate-side__head">
        <div className="slate-side__org">
          <span className="slate-side__org-mark" aria-hidden="true">
            {(session?.organization?.name ?? 'F').slice(0, 1).toUpperCase()}
          </span>
          <span className="slate-side__org-name truncate">
            {session?.organization?.name ?? t('brand.name')}
          </span>
          <IconChevronDown size={13} />
        </div>
        <WorkspaceSwitcher />
      </div>

      <button
        type="button"
        className="slate-search"
        onClick={() => search.setOpen(true)}
        aria-label={t('nav.search')}
        aria-keyshortcuts="Control+K Meta+K"
      >
        <IconSearch size={14} />
        <span className="slate-search__label">{t('nav.search')}</span>
        <kbd className="kbd">Ctrl K</kbd>
      </button>

      <nav className="slate-side__nav" aria-label={t('nav.mainNavigation')}>
        {groups.map((group) => (
          <div key={group.key} className="slate-group">
            <div className="slate-group__label">{t(group.labelKey)}</div>
            {group.items.map((item) => {
              const Icon = item.icon
              return (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.to === '/'}
                  className={({ isActive }) => (isActive ? 'slate-item slate-item--active' : 'slate-item')}
                >
                  <Icon size={15} />
                  <span className="truncate">{t(item.labelKey)}</span>
                </NavLink>
              )
            })}
          </div>
        ))}
      </nav>

      <div className="slate-side__foot">
        <AccountMenu />
      </div>
    </aside>
  )

  return (
    <div className="shell slate">
      {!isPhone ? sidebar : null}

      <div className="slate-main">
        <header className="slate-top">
          {isPhone ? (
            <button
              type="button"
              className="slate-iconbtn"
              aria-label={t('nav.menu')}
              aria-expanded={drawerOpen}
              onClick={() => setDrawerOpen(true)}
            >
              <IconMenu size={18} />
            </button>
          ) : null}
          <span className="slate-top__crumb truncate">
            <Link to="/" className="slate-top__crumb-home">{t('brand.name')}</Link>
            <span className="slate-top__crumb-sep" aria-hidden="true">/</span>
            <span aria-current="page">{current ? t(current.labelKey) : t('nav.overview')}</span>
          </span>
          <div className="slate-top__spacer" />
          <button
            type="button"
            className="slate-top__keyhint"
            onClick={() => search.setOpen(true)}
            aria-label={t('nav.search')}
          >
            <IconSearch size={13} />
            <kbd className="kbd">Ctrl K</kbd>
          </button>
          {isPhone ? <AccountMenu avatarOnly /> : null}
        </header>

        {isPhone ? (
          <MobileNavDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} groups={groups} />
        ) : null}

        <ShellCanvas className="content--slate" />
      </div>

      <SearchPaletteLayer open={search.open} onClose={() => search.setOpen(false)} />
    </div>
  )
}
