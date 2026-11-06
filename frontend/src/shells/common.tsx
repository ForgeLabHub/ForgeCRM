/**
 * The shared machinery every interface experience is built from.
 *
 * A shell answers two questions — where the navigation lives and how the chrome
 * is arranged — and everything else is the same product: the account menu, the
 * workspace switcher, the global search palette, the navigation model with its
 * permission filtering, and the mobile drawer. Those pieces are defined here
 * once so the four experiences cannot drift: a fix to the account popover lands
 * in every shell at once, and a route hidden by a permission is hidden in all
 * four navigation anatomies identically.
 */
import { Suspense, useEffect, useMemo, useRef, useState } from 'react'
import type { JSX, ReactNode } from 'react'
import { Link, Outlet, useLocation } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { GlobalSearch } from '../components/GlobalSearch'
import { useGlobalSearchShortcut } from '../hooks/useGlobalSearch'
import { useI18n, type TranslationKey } from '../i18n'
import { useTheme, THEME_MODES, type ThemeMode } from '../theme'
import { useWorkspace } from '../workspace/WorkspaceProvider'
import { APP_HOME, NAV_ENTRIES, NAV_GROUPS, navEntryFor, type NavEntry } from '../navigation'
import { WORKSPACE_DEFINITIONS, workspaceDefinition } from '../workspaces'
import { Avatar, Skeleton } from '../components/ui'
import {
  IconChevronDown,
  IconLogOut,
  IconMonitor,
  IconMoon,
  IconSearch,
  IconSliders,
  IconSun,
  IconUser,
  type IconProps,
} from '../components/icons'

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
 * True while the viewport is narrower than the shell's drawer breakpoint. All
 * shells collapse into a drawer navigation; only the width where they do it
 * differs. `null` while unknown is treated as wide so a desktop render never
 * flashes a drawer.
 */
export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(
    () => typeof window !== 'undefined' && window.matchMedia?.(query).matches === true,
  )
  useEffect(() => {
    if (!window.matchMedia) return
    const media = window.matchMedia(query)
    const update = () => setMatches(media.matches)
    update()
    media.addEventListener('change', update)
    return () => media.removeEventListener('change', update)
  }, [query])
  return matches
}

export interface RenderGroup {
  key: string
  labelKey: TranslationKey
  items: NavEntry[]
  reorder: boolean
}

/**
 * The shell's nav model: the workspace-ordered flat list for role workspaces,
 * or the full grouped navigation in the ungrouped product view. Routes the
 * user cannot read are filtered out here so that no shell can render a dead
 * link, and the workspace reorder (session-local) is computed once for every
 * consumer.
 */
export function useNavModel(): { groups: RenderGroup[]; moveRoute: (route: string, delta: -1 | 1) => void } {
  const { can } = useAuth()
  const { workspace } = useWorkspace()
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

  return { groups, moveRoute }
}

/**
 * Every entry the signed-in user may open, flattened. Permission-filtered, so
 * launchers and dropdown menus list exactly what the sidebar would have.
 */
export function useAllowedEntries(): NavEntry[] {
  const { can } = useAuth()
  return useMemo(
    () => NAV_ENTRIES.filter((item) => !item.permission || can(item.permission)),
    [can],
  )
}

/**
 * The nav entry the current location belongs to. Detail pages (`/app/accounts/42`)
 * resolve to their list entry by longest prefix, so every shell can name the
 * current section without each route declaring a breadcrumb.
 */
export function useCurrentEntry(): NavEntry | undefined {
  const { pathname } = useLocation()
  return useMemo(() => {
    const exact = navEntryFor(pathname)
    if (exact) return exact
    let best: NavEntry | undefined
    for (const entry of NAV_ENTRIES) {
      if (entry.to === APP_HOME) continue
      if (pathname.startsWith(`${entry.to}/`) && (best?.to.length ?? 0) < entry.to.length) {
        best = entry
      }
    }
    return best ?? (pathname === APP_HOME ? navEntryFor(APP_HOME) : undefined)
  }, [pathname])
}

/**
 * The account control shared by every shell: identity, theme, profile and
 * sign-out. It is a popover of controls, not an ARIA menu (see the classic
 * shell's history): it holds a theme-switch group alongside action items, and
 * closes on Escape, outside click and selection.
 */
export function AccountMenu({ avatarOnly = false }: { avatarOnly?: boolean }) {
  const { session, signOut } = useAuth()
  const { t } = useI18n()
  const { mode, setMode, saving, syncError, ready } = useTheme()
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
        {avatarOnly ? null : <span className="menu__name">{displayName || email}</span>}
        <IconChevronDown size={15} />
      </button>

      {open ? (
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
                  disabled={saving || !ready}
                  onClick={() => void setMode(option)}
                >
                  <Icon size={14} />
                  <span>{t(THEME_LABEL_KEY[option])}</span>
                </button>
              )
            })}
          </div>

          {syncError ? <p role="alert" className="text-sm">{t('appearance.saveError')}</p> : null}
          <div className="menu__separator" />

          <Link to="/app/account" className="menu__item" onClick={() => setOpen(false)}>
            <IconUser size={16} />
            {t('nav.account')}
          </Link>

          <Link to="/app/appearance" className="menu__item" onClick={() => setOpen(false)}>
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
 * The workspace switcher, shared verbatim across shells. It names the current
 * workspace and lists the others with a one-line description: the choice is
 * made by what each view is for. It grants nothing — every route still passes
 * the same permission check regardless of which workspace is active.
 */
export function WorkspaceSwitcher() {
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

/**
 * The search affordance styled by each shell: it reads as a quiet field on
 * light chrome and as a command hint on dark chrome. Opens the shared palette.
 */
export function SearchTrigger({
  onOpen,
  compact = false,
}: {
  onOpen: () => void
  compact?: boolean
}) {
  const { t } = useI18n()
  return (
    <button
      type="button"
      className={compact ? 'search-trigger search-trigger--compact' : 'search-trigger'}
      onClick={onOpen}
      aria-label={t('nav.search')}
      aria-keyshortcuts="Control+K Meta+K"
    >
      <IconSearch size={15} />
      <span className="search-trigger__label">{t('nav.search')}</span>
      <kbd className="kbd">Ctrl K</kbd>
    </button>
  )
}

/**
 * Fallback for a route whose code chunk is still loading. It mirrors the page
 * header and a content block so the shell does not collapse and then jump when
 * the real page arrives.
 */
export function RouteFallback() {
  return (
    <div aria-busy="true">
      <div className="skeleton" style={{ width: 220, height: 24, marginBottom: 12 }} />
      <div className="skeleton" style={{ width: 320, height: 14, marginBottom: 24 }} />
      <Skeleton width="100%" height={220} />
    </div>
  )
}

/**
 * The routed page plus the global search palette, wired to the same shortcut
 * in every shell. Shells place <ShellFrame> where their canvas belongs and put
 * their own chrome around it; the palette itself renders above all of it.
 */
export function ShellCanvas({ className }: { className?: string }) {
  return (
    <main className={className ? `content ${className}` : 'content'}>
      <Suspense fallback={<RouteFallback />}>
        <Outlet />
      </Suspense>
    </main>
  )
}

/**
 * Binds the Ctrl/Cmd-K shortcut and renders the palette once per shell. The
 * returned `search` object feeds the shell's own trigger button.
 */
export function useShellSearch() {
  return useGlobalSearchShortcut()
}

export function SearchPaletteLayer({ open, onClose }: { open: boolean; onClose: () => void }) {
  return open ? <GlobalSearch onClose={onClose} /> : null
}

/**
 * The off-canvas navigation every new shell collapses into on a phone. The
 * classic shell predates it and keeps its own drawer; the experiences share
 * this one. It exists only while open, so a closed drawer can never trap
 * keyboard or screen-reader navigation, and focus returns to the opener the
 * same way the modal returns it.
 */
export function MobileNavDrawer({
  open,
  onClose,
  groups,
  header,
}: {
  open: boolean
  onClose: () => void
  groups: RenderGroup[]
  /** Extra chrome the shell wants at the top of its drawer, e.g. workspace tiles. */
  header?: ReactNode
}) {
  const { t } = useI18n()
  const drawerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const previouslyFocused = document.activeElement as HTMLElement | null
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    drawerRef.current?.focus()
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        onClose()
        return
      }
      if (event.key !== 'Tab' || !drawerRef.current) return
      const controls = drawerRef.current.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), [tabindex]:not([tabindex="-1"])',
      )
      if (!controls.length) {
        event.preventDefault()
        return
      }
      const first = controls[0]
      const last = controls[controls.length - 1]
      if (event.shiftKey && (document.activeElement === first || document.activeElement === drawerRef.current)) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('keydown', onKeyDown)
      document.body.style.overflow = previousOverflow
      previouslyFocused?.focus?.()
    }
  }, [open, onClose])

  if (!open) return null

  return (
    <>
      <button type="button" className="sidebar-scrim" aria-label={t('nav.closeMenu')} onClick={onClose} />
      <div
        ref={drawerRef}
        className="mobile-drawer"
        role="dialog"
        aria-modal="true"
        aria-label={t('nav.mainNavigation')}
        tabIndex={-1}
      >
        <div className="mobile-drawer__head">
          <span className="mobile-drawer__title">{t('nav.menu')}</span>
          <button type="button" className="sidebar__close" aria-label={t('nav.closeMenu')} onClick={onClose}>
            <span aria-hidden="true">✕</span>
          </button>
        </div>
        {header}
        <nav className="mobile-drawer__nav" aria-label={t('nav.mainNavigation')}>
          {groups.map((group) => (
            <div key={group.key}>
              <div className="sidebar__group-label">{t(group.labelKey)}</div>
              {group.items.map((item) => {
                const Icon = item.icon
                return (
                  <Link key={item.to} to={item.to} className="nav-item" onClick={onClose}>
                    <Icon size={17} />
                    <span className="truncate">{t(item.labelKey)}</span>
                  </Link>
                )
              })}
            </div>
          ))}
        </nav>
      </div>
    </>
  )
}
