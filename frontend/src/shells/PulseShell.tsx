/**
 * Pulse — the work-board experience, on the anatomy monday.com established: a
 * narrow, dark rail of colour-coded workspace tiles on the far left, then a
 * light panel that belongs to the selected workspace and lists everything in
 * it. Switching context is a physical act — you tap a different coloured
 * square — and the rest of the screen is one wide, bright board canvas.
 *
 * The rail never sleeps: account and preferences sit at its foot, and the
 * workspace you are in is always the one tile that is lit, so orientation is a
 * peripheral-vision question rather than a headline you have to read.
 */
import { useEffect, useState } from 'react'
import { Link, NavLink, useLocation } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { useI18n } from '../i18n'
import { useWorkspace } from '../workspace/WorkspaceProvider'
import { WORKSPACE_DEFINITIONS, workspaceDefinition, type Workspace } from '../workspaces'
import {
  AccountMenu,
  MobileNavDrawer,
  SearchPaletteLayer,
  SearchTrigger,
  ShellCanvas,
  useCurrentEntry,
  useMediaQuery,
  useNavModel,
  useShellSearch,
} from './common'
import { IconMenu, IconSearch, IconSliders } from '../components/icons'

/** The rail tile palette: the same rotation monday uses — a workspace is its colour. */
const TILE_HUES: Record<Workspace, string> = {
  sales: '#00c875',
  executive: '#a25ddc',
  support: '#037f4c',
  marketing: '#ff642e',
  operations: '#579bfc',
  analyst: '#e2445c',
  admin: '#808590',
  all: '#0073ea',
}

function WorkspaceTiles({ onPick }: { onPick?: () => void }) {
  const { t } = useI18n()
  const { workspace, setWorkspace } = useWorkspace()
  return (
    <div className="pulse-tiles" role="group" aria-label={t('workspace.switchLabel')}>
      {WORKSPACE_DEFINITIONS.map((definition) => (
        <button
          key={definition.id}
          type="button"
          className={
            definition.id === workspace
              ? 'pulse-tile pulse-tile--active'
              : 'pulse-tile'
          }
          style={{ ['--tile-hue' as string]: TILE_HUES[definition.id] }}
          aria-pressed={definition.id === workspace}
          aria-label={t(definition.labelKey)}
          title={t(definition.labelKey)}
          onClick={() => {
            setWorkspace(definition.id)
            onPick?.()
          }}
        >
          <span className="pulse-tile__glyph" aria-hidden="true">{definition.glyph}</span>
        </button>
      ))}
    </div>
  )
}

export function PulseShell() {
  const { session } = useAuth()
  const { t } = useI18n()
  const { workspace } = useWorkspace()
  const location = useLocation()
  const search = useShellSearch()
  const { groups } = useNavModel()
  const current = useCurrentEntry()
  const isPhone = useMediaQuery('(max-width: 900px)')
  const [drawerOpen, setDrawerOpen] = useState(false)

  const definition = workspaceDefinition(workspace)

  useEffect(() => { setDrawerOpen(false) }, [location.pathname])
  useEffect(() => { if (search.open) setDrawerOpen(false) }, [search.open])

  return (
    <div className="shell pulse">
      {!isPhone ? (
        <aside className="pulse-rail" aria-label={t('workspace.label')}>
          <Link to="/app" className="pulse-rail__brand" aria-label={t('brand.name')}>
            F
          </Link>
          <div className="pulse-rail__scroll">
            <WorkspaceTiles />
          </div>
          <div className="pulse-rail__foot">
            <Link to="/app/appearance" className="pulse-rail__tool" aria-label={t('nav.appearance')} title={t('nav.appearance')}>
              <IconSliders size={17} />
            </Link>
            <AccountMenu avatarOnly />
          </div>
        </aside>
      ) : null}

      {!isPhone ? (
        <aside className="pulse-panel">
          <div className="pulse-panel__head">
            <span className="pulse-panel__glyph" style={{ ['--tile-hue' as string]: TILE_HUES[workspace] }} aria-hidden="true">
              {definition.glyph}
            </span>
            <div className="pulse-panel__heading">
              <div className="pulse-panel__name truncate">{t(definition.labelKey)}</div>
              <div className="pulse-panel__sub truncate">{session?.organization?.name ?? t('brand.name')}</div>
            </div>
          </div>

          <button
            type="button"
            className="pulse-panel__search"
            onClick={() => search.setOpen(true)}
            aria-label={t('nav.search')}
            aria-keyshortcuts="Control+K Meta+K"
          >
            <IconSearch size={14} />
            <span>{t('nav.search')}</span>
            <kbd className="kbd">Ctrl K</kbd>
          </button>

          <nav className="pulse-panel__nav" aria-label={t('nav.mainNavigation')}>
            {groups.map((group) => (
              <div key={group.key} className="pulse-panel__group">
                <div className="pulse-panel__group-label">{t(group.labelKey)}</div>
                {group.items.map((item, index) => {
                  const Icon = item.icon
                  return (
                    <NavLink
                      key={item.to}
                      to={item.to}
                      end={item.to === '/'}
                      className={({ isActive }) => (isActive ? 'pulse-item pulse-item--active' : 'pulse-item')}
                    >
                      <span
                        className={`pulse-item__dot pulse-item__dot--${index % 6}`}
                        aria-hidden="true"
                      >
                        <Icon size={13} />
                      </span>
                      <span className="truncate">{t(item.labelKey)}</span>
                    </NavLink>
                  )
                })}
              </div>
            ))}
          </nav>
        </aside>
      ) : null}

      <div className="pulse-main">
        <header className="pulse-top">
          {isPhone ? (
            <>
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                aria-label={t('nav.menu')}
                aria-expanded={drawerOpen}
                onClick={() => setDrawerOpen(true)}
              >
                <IconMenu size={18} />
              </button>
              <span className="pulse-top__brand">{t('brand.name')}</span>
            </>
          ) : (
            <span className="pulse-top__section truncate">
              {current ? t(current.labelKey) : t('nav.overview')}
            </span>
          )}
          <div className="pulse-top__spacer" />
          <SearchTrigger compact onOpen={() => search.setOpen(true)} />
          {isPhone ? <AccountMenu avatarOnly /> : null}
        </header>

        <MobileNavDrawer
          open={drawerOpen}
          onClose={() => setDrawerOpen(false)}
          groups={groups}
          header={<div className="mobile-drawer__tiles"><WorkspaceTiles onPick={() => setDrawerOpen(false)} /></div>}
        />

        <ShellCanvas className="content--pulse" />
      </div>

      <SearchPaletteLayer open={search.open} onClose={() => search.setOpen(false)} />
    </div>
  )
}
