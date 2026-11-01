/**
 * Theme selection: light, dark, or follow the system.
 *
 * The mode is stored in localStorage and written to `data-theme` on <html>, which
 * is the single attribute the stylesheet keys off. The system preference is read
 * live through matchMedia, so a user who changes their OS theme sees the change
 * without a reload, and "system" keeps working after the choice is stored.
 *
 * Avoiding a flash of the wrong theme is not done here: it is done by a small
 * inline script in index.html that runs before the first paint and sets the same
 * attribute. This provider only keeps React's state in step with it afterwards.
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
}

const ThemeContext = createContext<ThemeValue | null>(null)

export function isThemeMode(value: string | null): value is ThemeMode {
  return value !== null && (THEME_MODES as readonly string[]).includes(value)
}

/** Reads the stored mode, defaulting to "system". */
export function resolveInitialMode(): ThemeMode {
  if (typeof window === 'undefined') return DEFAULT_THEME_MODE
  const stored = window.localStorage.getItem(THEME_STORAGE_KEY)
  return isThemeMode(stored) ? stored : DEFAULT_THEME_MODE
}

/** Resolves a mode to the concrete theme, consulting the system when needed. */
export function resolveTheme(mode: ThemeMode, prefersDark: boolean): ResolvedTheme {
  if (mode === 'system') return prefersDark ? 'dark' : 'light'
  return mode
}

/** Writes the resolved theme to <html> so the stylesheet can react to it. */
export function applyTheme(resolved: ResolvedTheme): void {
  document.documentElement.setAttribute('data-theme', resolved)
  document.documentElement.style.colorScheme = resolved
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(resolveInitialMode)
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

  const setMode = useCallback((next: ThemeMode) => {
    setModeState(next)
    try {
      window.localStorage.setItem(THEME_STORAGE_KEY, next)
    } catch {
      // The choice still applies for this session if storage is unavailable.
    }
  }, [])

  const value = useMemo<ThemeValue>(
    () => ({ mode, resolved, setMode }),
    [mode, resolved, setMode],
  )
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function useTheme(): ThemeValue {
  const ctx = useContext(ThemeContext)
  if (!ctx) throw new Error('useTheme must be used inside a ThemeProvider')
  return ctx
}
