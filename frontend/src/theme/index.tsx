/** Appearance preferences: server-backed per signed-in user, device-local on
 * the anonymous sign-in screen. Styles never change permissions or data scope.
 * Named styles currently provide their own palettes; only the original palette
 * follows light/dark/system. */
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import {
  ACCENT_STORAGE_KEY,
  DEFAULT_ACCENT,
  DEFAULT_DENSITY,
  DEFAULT_STYLE,
  DENSITY_STORAGE_KEY,
  STYLE_STORAGE_KEY,
  isAccent,
  isDensity,
  isThemeStyle,
  type Accent,
  type Density,
  type ThemeStyle,
} from './themes'
import { appearanceApi, type AppearanceInput, type AppearanceResponse } from '../api/appearance'

export { THEME_STYLES, SELECTABLE_STYLES, ACCENTS, DENSITIES, DEFAULT_STYLE } from './themes'
export type { ThemeStyle, Accent, Density } from './themes'

export const THEME_MODES = ['light', 'dark', 'system'] as const
export type ThemeMode = (typeof THEME_MODES)[number]
export type ResolvedTheme = 'light' | 'dark'

export const THEME_STORAGE_KEY = 'forgecrm.theme'
export const DEFAULT_THEME_MODE: ThemeMode = 'system'

interface ThemeValue {
  mode: ThemeMode
  /** The theme actually in effect after resolving "system". */
  resolved: ResolvedTheme
  setMode: (mode: ThemeMode) => Promise<boolean>
  /** The active visual style. */
  style: ThemeStyle
  setStyle: (style: ThemeStyle) => Promise<boolean>
  /** The active accent override. */
  accent: Accent
  setAccent: (accent: Accent) => Promise<boolean>
  /** The active layout density. */
  density: Density
  setDensity: (density: Density) => Promise<boolean>
  /** Reset style, accent and density to their defaults. */
  resetAppearance: () => Promise<boolean>
  ready: boolean
  automatic: boolean
  saving: boolean
  syncError: boolean
  loadError: boolean
  retry: () => void
}

const ThemeContext = createContext<ThemeValue | null>(null)

export function isThemeMode(value: string | null): value is ThemeMode {
  return value !== null && (THEME_MODES as readonly string[]).includes(value)
}

function readStored<T extends string>(
  key: string,
  fallback: T,
  valid: (v: string | null) => v is T,
): T {
  if (typeof window === 'undefined') return fallback
  try {
    const stored = window.localStorage.getItem(key)
    return valid(stored) ? stored : fallback
  } catch {
    return fallback
  }
}

/** Reads the stored mode, defaulting to "system". */
export function resolveInitialMode(): ThemeMode {
  return readStored(THEME_STORAGE_KEY, DEFAULT_THEME_MODE, isThemeMode)
}

export function resolveInitialStyle(): ThemeStyle {
  return readStored(STYLE_STORAGE_KEY, DEFAULT_STYLE, isThemeStyle)
}

export function resolveInitialAccent(): Accent {
  return readStored(ACCENT_STORAGE_KEY, DEFAULT_ACCENT, isAccent)
}

export function resolveInitialDensity(): Density {
  return readStored(DENSITY_STORAGE_KEY, DEFAULT_DENSITY, isDensity)
}

/** Resolves a mode to the concrete theme, consulting the system when needed. */
export function resolveTheme(mode: ThemeMode, prefersDark: boolean): ResolvedTheme {
  if (mode === 'system') return prefersDark ? 'dark' : 'light'
  return mode
}

/**
 * Writes the resolved theme to <html>. Only the base style follows the mode;
 * a named style supplies its own palette, so the mode is written regardless and
 * the stylesheet decides whether to read it.
 */
export function applyTheme(resolved: ResolvedTheme): void {
  document.documentElement.setAttribute('data-theme', resolved)
  document.documentElement.style.colorScheme = resolved
}

export function applyStyle(style: ThemeStyle): void {
  document.documentElement.setAttribute('data-style', style)
}

export function applyAccent(accent: Accent): void {
  document.documentElement.setAttribute('data-accent', accent)
}

export function applyDensity(density: Density): void {
  document.documentElement.setAttribute('data-density', density)
}

function persist(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value)
  } catch {
    // The choice still applies for this session if storage is unavailable.
  }
}

type AccountIdentity = { status: 'loading' | 'authenticated' | 'anonymous'; key: string | null }

// Without an account prop the provider remains usable on the sign-in page and
// in standalone consumers. Authenticated settings are authoritative on the server.
export function ThemeProvider({ children, account }: { children: ReactNode; account?: AccountIdentity }) {
  const [mode, setModeState] = useState<ThemeMode>(() => account ? DEFAULT_THEME_MODE : resolveInitialMode())
  const [style, setStyleState] = useState<ThemeStyle>(() => account ? DEFAULT_STYLE : resolveInitialStyle())
  const [accent, setAccentState] = useState<Accent>(() => account ? DEFAULT_ACCENT : resolveInitialAccent())
  const [density, setDensityState] = useState<Density>(() => account ? DEFAULT_DENSITY : resolveInitialDensity())
  const [prefersDark, setPrefersDark] = useState(() =>
    typeof window !== 'undefined' && window.matchMedia
      ? window.matchMedia('(prefers-color-scheme: dark)').matches
      : false,
  )
  const [loadedKey, setLoadedKey] = useState<string | null>(null)
  const [syncError, setSyncError] = useState(false)
  const [loadError, setLoadError] = useState(false)
  const [saving, setSaving] = useState(false)
  const [automatic, setAutomatic] = useState(false)
  const [attempt, setAttempt] = useState(0)
  const accountMode = Boolean(account)
  const busy = useRef<number | null>(null)
  const currentKey = account?.status === 'authenticated' ? account.key : null
  const currentKeyRef = useRef(currentKey)
  currentKeyRef.current = currentKey
  // A key alone is insufficient: logging out and back into the same account
  // must also invalidate responses from the earlier session.
  const identity = `${account?.status ?? 'standalone'}:${currentKey ?? ''}`
  const generation = useRef({ identity, value: 0 })
  if (generation.current.identity !== identity) {
    generation.current = { identity, value: generation.current.value + 1 }
  }
  const saved = useRef<AppearanceInput>({ style: null, mode: DEFAULT_THEME_MODE, accent: DEFAULT_ACCENT, density: DEFAULT_DENSITY })

  const applyResponse = useCallback((response: AppearanceResponse) => {
    if (!isThemeStyle(response.effective_style) || !isThemeMode(response.mode) ||
        !isAccent(response.accent) || !isDensity(response.density) ||
        (response.style !== null && !isThemeStyle(response.style))) {
      throw new Error('Invalid appearance response')
    }
    setAutomatic(response.style === null)
    saved.current = { style: response.style, mode: response.mode, accent: response.accent, density: response.density }
    setModeState(response.mode)
    setStyleState(response.effective_style)
    setAccentState(response.accent)
    setDensityState(response.density)
  }, [])

  useEffect(() => {
    if (typeof window === 'undefined' || !window.matchMedia) return
    const query = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = (event: MediaQueryListEvent) => setPrefersDark(event.matches)
    query.addEventListener('change', onChange)
    return () => query.removeEventListener('change', onChange)
  }, [])

  useEffect(() => {
    if (!accountMode) return
    let cancelled = false
    setLoadedKey(null)
    setSyncError(false)
    setLoadError(false)
    setSaving(false)
    setAutomatic(false)
    setStyleState(DEFAULT_STYLE)
    setAccentState(DEFAULT_ACCENT)
    setDensityState(DEFAULT_DENSITY)
    if (currentKey) setModeState(DEFAULT_THEME_MODE)
    if (!currentKey) {
      saved.current = { style: null, mode: DEFAULT_THEME_MODE, accent: DEFAULT_ACCENT, density: DEFAULT_DENSITY }
      setModeState(account?.status === 'anonymous' ? resolveInitialMode() : DEFAULT_THEME_MODE)
      setStyleState(DEFAULT_STYLE)
      setAccentState(DEFAULT_ACCENT)
      setDensityState(DEFAULT_DENSITY)
      return
    }
    void appearanceApi.get().then((result) => {
      if (cancelled || currentKeyRef.current !== currentKey) return
      applyResponse(result)
      setLoadedKey(currentKey)
    }).catch(() => {
      if (!cancelled && currentKeyRef.current === currentKey) setLoadError(true)
    })
    return () => { cancelled = true }
  }, [accountMode, account?.status, currentKey, attempt, applyResponse])

  const neutral = accountMode && (!currentKey || loadedKey !== currentKey)
  const resolved = resolveTheme(currentKey && neutral ? DEFAULT_THEME_MODE : mode, prefersDark)
  const shownStyle = neutral ? DEFAULT_STYLE : style
  const shownAccent = neutral ? DEFAULT_ACCENT : accent
  const shownDensity = neutral ? DEFAULT_DENSITY : density
  // Applied before paint so the authenticated page does not flash another user's style.
  useLayoutEffect(() => { applyTheme(resolved) }, [resolved])
  useLayoutEffect(() => { applyStyle(shownStyle) }, [shownStyle])
  useLayoutEffect(() => { applyAccent(shownAccent) }, [shownAccent])
  useLayoutEffect(() => { applyDensity(shownDensity) }, [shownDensity])

  const save = useCallback(async (patch: Partial<AppearanceInput>): Promise<boolean> => {
    if (!currentKey || loadedKey !== currentKey || busy.current === generation.current.value) return false
    const key = currentKey
    const epoch = generation.current.value
    busy.current = epoch
    setSaving(true)
    setSyncError(false)
    try {
      const response = await appearanceApi.save({ ...saved.current, ...patch })
      if (currentKeyRef.current !== key || generation.current.value !== epoch) return false
      applyResponse(response)
      return true
    } catch {
      if (currentKeyRef.current === key && generation.current.value === epoch) setSyncError(true)
      return false
    } finally {
      if (busy.current === epoch) busy.current = null
      if (generation.current.value === epoch) setSaving(false)
    }
  }, [currentKey, loadedKey, applyResponse])

  const setMode = useCallback(async (next: ThemeMode) => {
    if (currentKey) return save({ mode: next })
    setModeState(next)
    persist(THEME_STORAGE_KEY, next)
    return true
  }, [currentKey, save])
  const setStyle = useCallback(async (next: ThemeStyle) => {
    if (currentKey) return save({ style: next })
    setStyleState(next)
    persist(STYLE_STORAGE_KEY, next)
    return true
  }, [currentKey, save])
  const setAccent = useCallback(async (next: Accent) => {
    if (currentKey) return save({ accent: next })
    setAccentState(next)
    persist(ACCENT_STORAGE_KEY, next)
    return true
  }, [currentKey, save])
  const setDensity = useCallback(async (next: Density) => {
    if (currentKey) return save({ density: next })
    setDensityState(next)
    persist(DENSITY_STORAGE_KEY, next)
    return true
  }, [currentKey, save])
  const resetAppearance = useCallback(async () => {
    if (currentKey) return save({ style: null, accent: DEFAULT_ACCENT, density: DEFAULT_DENSITY })
    setStyleState(DEFAULT_STYLE)
    setAccentState(DEFAULT_ACCENT)
    setDensityState(DEFAULT_DENSITY)
    persist(STYLE_STORAGE_KEY, DEFAULT_STYLE)
    persist(ACCENT_STORAGE_KEY, DEFAULT_ACCENT)
    persist(DENSITY_STORAGE_KEY, DEFAULT_DENSITY)
    return true
  }, [currentKey, save])
  const retry = useCallback(() => setAttempt((n) => n + 1), [])
  const ready = !currentKey || loadedKey === currentKey
  const value = useMemo<ThemeValue>(
    () => ({ mode, resolved, setMode, style: shownStyle, setStyle, accent: shownAccent, setAccent, density: shownDensity, setDensity,
      resetAppearance, ready, automatic, saving, syncError, loadError, retry }),
    [mode, resolved, setMode, shownStyle, setStyle, shownAccent, setAccent, shownDensity, setDensity, resetAppearance, ready, automatic, saving, syncError, loadError, retry],
  )
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function useTheme(): ThemeValue {
  const ctx = useContext(ThemeContext)
  if (!ctx) throw new Error('useTheme must be used inside a ThemeProvider')
  return ctx
}
