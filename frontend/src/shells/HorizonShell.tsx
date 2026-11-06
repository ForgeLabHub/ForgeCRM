/**
 * Horizon — the growth-suite experience, built on the pattern HubSpot uses:
 * one dark top bar that is nothing but navigation. Each product area is a
 * dropdown ("Workspace", "Data", "Platform", "Administration"), so the full
 * depth of the CRM sits behind a single restrained row of words instead of a
 * permanent rail; the coral accent and the light contextual sub-bar under the
 * header keep the suite feeling warm rather than corporate.
 *
 * The sub-bar names where you are (the current section) on the left and keeps
 * the workspace switcher on the right — HubSpot's separation of global chrome
 * (dark bar) from page context (light bar) — and the content canvas below is
 * wide, bright and card-first.
 */
import { useEffect, useRef, useState } from 'react'
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
  type RenderGroup,
} from './common'
import { IconChevronDown, IconLayers, IconMenu, IconSearch } from '../components/icons'

/**
 * One product-area dropdown in the dark bar. Opens on click (touch-safe, and a
 * hover-only menu is unusable on half the devices this app runs on); Escape,
 * an outside click, or choosing an item closes it.
 */
function HorizonMenu({
  group,
  open,
  onToggle,
  onClose,
}: {
  group: RenderGroup
  open: boolean
  onToggle: () => void
  onClose: () => void
}) {
  const { t } = useI18n()
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
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
  }, [open, onClose])

  return (
    <div className="horizon-menu" ref={ref}>
      <button
        type="button"
        className={open ? 'horizon-menu__trigger horizon-menu__trigger--open' : 'horizon-menu__trigger'}
        aria-haspopup="true"
        aria-expanded={open}
        onClick={onToggle}
      >
        <span>{t(group.labelKey)}</span>
        <IconChevronDown size={13} />
      </button>
      {open ? (
        <div className="horizon-menu__panel" role="group" aria-label={t(group.labelKey)}>
          {group.items.map((item) => {
            const Icon = item.icon
            return (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.to === '/'}
                className={({ isActive }) =>
                  isActive ? 'horizon-menu__item horizon-menu__item--active' : 'horizon-menu__item'
                }
                onClick={onClose}
              >
                <Icon size={15} />
                <span>{t(item.labelKey)}</span>
              </NavLink>
            )
          })}
        </div>
      ) : null}
    </div>
  )
}

export function HorizonShell() {
  const { session } = useAuth()
  const { t } = useI18n()
  const location = useLocation()
  const search = useShellSearch()
  const { groups } = useNavModel()
  const current = useCurrentEntry()
  const isPhone = useMediaQuery('(max-width: 900px)')
  const [openGroup, setOpenGroup] = useState<string | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)

  useEffect(() => {
    setOpenGroup(null)
    setDrawerOpen(false)
  }, [location.pathname])
  useEffect(() => {
    if (search.open) {
      setOpenGroup(null)
      setDrawerOpen(false)
    }
  }, [search.open])

  return (
    <div className="shell horizon">
      <header className="horizon-bar">
        {isPhone ? (
          <button
            type="button"
            className="horizon-iconbtn"
            aria-label={t('nav.menu')}
            aria-expanded={drawerOpen}
            onClick={() => setDrawerOpen(true)}
          >
            <IconMenu size={19} />
          </button>
        ) : null}

        <Link to="/app" className="horizon-brand">
          <span className="horizon-brand__mark" aria-hidden="true">F</span>
          <span className="horizon-brand__name">{t('brand.name')}</span>
        </Link>

        {!isPhone ? (
          <nav className="horizon-nav" aria-label={t('nav.mainNavigation')}>
            {groups.map((group) => (
              <HorizonMenu
                key={group.key}
                group={group}
                open={openGroup === group.key}
                onToggle={() => setOpenGroup((key) => (key === group.key ? null : group.key))}
                onClose={() => setOpenGroup(null)}
              />
            ))}
          </nav>
        ) : null}

        <div className="horizon-bar__spacer" />

        <button
          type="button"
          className="horizon-search"
          onClick={() => search.setOpen(true)}
          aria-label={t('nav.search')}
          aria-keyshortcuts="Control+K Meta+K"
        >
          <IconSearch size={14} />
          <span className="horizon-search__label">{t('nav.search')}</span>
        </button>
        <AccountMenu avatarOnly />
      </header>

      <div className="horizon-subbar">
        <div className="horizon-subbar__context">
          <span className="horizon-subbar__section truncate">
            {current ? t(current.labelKey) : t('nav.overview')}
          </span>
          {session?.organization?.name ? (
            <span className="horizon-subbar__org">
              <IconLayers size={13} />
              <span className="truncate">{session.organization.name}</span>
            </span>
          ) : null}
        </div>
        <WorkspaceSwitcher />
      </div>

      <MobileNavDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} groups={groups} />

      <ShellCanvas className="content--horizon" />

      <SearchPaletteLayer open={search.open} onClose={() => search.setOpen(false)} />
    </div>
  )
}
