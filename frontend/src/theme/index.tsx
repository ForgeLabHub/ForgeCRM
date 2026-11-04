/**
 * Theme, style, accent and density.
 *
 * Four independent choices, each stored in localStorage and written to an
 * attribute on <html>, which is the single element the stylesheet keys off:
 *
 *   data-theme    light | dark — the base palette, or the resolved system choice
 *   data-style    one of the ten visual styles (default is the built-in palette)
 *   data-accent   the accent colour, overriding the one the style would pick
 *   data-density  compact | standard | spacious — the layout multiplier
 *
 * The mode is separate from the style on purpose: a style supplies its own
 * palette and therefore implies light or dark, while the mode still governs the
 * default style and the `color-scheme` the browser uses for form controls and
 * scrollbars. Keeping them apart is what lets "Midnight" and "Brutalist" sit in
 * one list without one of them pretending to be a mode.
 *
 * Avoiding a flash of the wrong theme is not done here: it is done by a small
 * inline script in index.html that runs before the first paint and sets the same
 * attributes. This provider only keeps React's state in step with it afterwards.
 */
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
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
  setMode: (mode: ThemeMode) => void
  /** The active visual style. */
  style: ThemeStyle
  setStyle: (style: ThemeStyle) => void
  /** The active accent override. */
  accent: Accent
  setAccent: (accent: Accent) => void
  /** The active layout density. */
  density: Density
  setDensity: (density: Density) => void
  /** Reset style, accent and density to their defaults. */
  resetAppearance: () => void
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

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(resolveInitialMode)
  const [style, setStyleState] = useState<ThemeStyle>(resolveInitialStyle)
  const [accent, setAccentState] = useState<Accent>(resolveInitialAccent)
  const [density, setDensityState] = useState<Density>(resolveInitialDensity)
  const [prefersDark, setPrefersDark] = useState(() =>
    typeof window !== 'undefined' && window.matchMedia
      ? window.matchMedia('(prefers-color-scheme: dark)').matches
      : false,
  )

  // Track the OS preference live so "system" is not a one-time snapshot.
  useEffect(() => {
    if (typeof window === 'undefined' || !window.matchMedia) return
    const query = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = (event: MediaQueryListEvent) => setPrefersDark(event.matches)
    query.addEventListener('change', onChange)
    return () => query.removeEventListener('change', onChange)
  }, [])

  const resolved = resolveTheme(mode, prefersDark)

  useEffect(() => {
    applyTheme(resolved)
  }, [resolved])

  useEffect(() => {
    applyStyle(style)
  }, [style])

  useEffect(() => {
    applyAccent(accent)
  }, [accent])

  useEffect(() => {
    applyDensity(density)
  }, [density])

  const setMode = useCallback((next: ThemeMode) => {
    setModeState(next)
    persist(THEME_STORAGE_KEY, next)
  }, [])

  const setStyle = useCallback((next: ThemeStyle) => {
    setStyleState(next)
    persist(STYLE_STORAGE_KEY, next)
  }, [])

  const setAccent = useCallback((next: Accent) => {
    setAccentState(next)
    persist(ACCENT_STORAGE_KEY, next)
  }, [])

  const setDensity = useCallback((next: Density) => {
    setDensityState(next)
    persist(DENSITY_STORAGE_KEY, next)
  }, [])

  const resetAppearance = useCallback(() => {
    setStyleState(DEFAULT_STYLE)
    setAccentState(DEFAULT_ACCENT)
    setDensityState(DEFAULT_DENSITY)
    persist(STYLE_STORAGE_KEY, DEFAULT_STYLE)
    persist(ACCENT_STORAGE_KEY, DEFAULT_ACCENT)
    persist(DENSITY_STORAGE_KEY, DEFAULT_DENSITY)
  }, [])

  const value = useMemo<ThemeValue>(
    () => ({
      mode,
      resolved,
      setMode,
      style,
      setStyle,
      accent,
      setAccent,
      density,
      setDensity,
      resetAppearance,
    }),
    [mode, resolved, setMode, style, setStyle, accent, setAccent, density, setDensity, resetAppearance],
  )
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function useTheme(): ThemeValue {
  const ctx = useContext(ThemeContext)
  if (!ctx) throw new Error('useTheme must be used inside a ThemeProvider')
  return ctx
}
