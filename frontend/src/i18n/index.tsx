/**
 * Localisation.
 *
 * A small provider over two flat dictionaries rather than a set of ternaries in
 * each component: the ternaries are what produce the mixed RU/EN screens this
 * replaces. English is the reference shape, so a key missing from another locale
 * is a TypeScript error at build time, not a blank label at runtime.
 *
 * The chosen locale is persisted in localStorage and applied to <html lang>, so
 * screen readers and the browser's own hyphenation follow the selection.
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
import { en, type Dictionary } from './en'
import { ru } from './ru'

export const LOCALES = ['en', 'ru'] as const
export type Locale = (typeof LOCALES)[number]

const DICTIONARIES: Record<Locale, Dictionary> = { en, ru }

const STORAGE_KEY = 'forgecrm.locale'
export const DEFAULT_LOCALE: Locale = 'en'

/** Every dot-path to a string leaf, for type-safe lookup. */
type Leaf<T, Prefix extends string = ''> = {
  [K in keyof T & string]: T[K] extends string
    ? `${Prefix}${K}`
    : Leaf<T[K], `${Prefix}${K}.`>
}[keyof T & string]

export type TranslationKey = Leaf<Dictionary>

interface I18nValue {
  locale: Locale
  setLocale: (locale: Locale) => void
  t: (key: TranslationKey, vars?: Record<string, string | number>) => string
}

const I18nContext = createContext<I18nValue | null>(null)

function isLocale(value: string | null): value is Locale {
  return value !== null && (LOCALES as readonly string[]).includes(value)
}

/** Reads the stored locale, falling back to the browser's preference. */
export function resolveInitialLocale(): Locale {
  if (typeof window === 'undefined') return DEFAULT_LOCALE
  const stored = window.localStorage.getItem(STORAGE_KEY)
  if (isLocale(stored)) return stored
  const preferred = (window.navigator.language ?? '').slice(0, 2).toLowerCase()
  if (isLocale(preferred)) return preferred
  return DEFAULT_LOCALE
}

function lookup(dict: Dictionary, key: string): string | undefined {
  const parts = key.split('.')
  let node: unknown = dict
  for (const part of parts) {
    if (typeof node !== 'object' || node === null) return undefined
    node = (node as Record<string, unknown>)[part]
  }
  return typeof node === 'string' ? node : undefined
}

/** Replaces {name} placeholders. A missing variable is left visible, not blank. */
function interpolate(template: string, vars?: Record<string, string | number>): string {
  if (!vars) return template
  return template.replace(/\{(\w+)\}/g, (match, name: string) =>
    name in vars ? String(vars[name]) : match,
  )
}

export function I18nProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(resolveInitialLocale)

  useEffect(() => {
    document.documentElement.lang = locale
  }, [locale])

  const setLocale = useCallback((next: Locale) => {
    setLocaleState(next)
    try {
      window.localStorage.setItem(STORAGE_KEY, next)
    } catch {
      // Private mode or a blocked storage API: the choice still applies for this
      // session, it just is not remembered. That is better than a crash.
    }
  }, [])

  const t = useCallback(
    (key: TranslationKey, vars?: Record<string, string | number>) => {
      // Fall back to English rather than showing the raw key: a missing
      // translation should degrade to a readable label, not to "login.submit".
      const value = lookup(DICTIONARIES[locale], key) ?? lookup(en, key) ?? key
      return interpolate(value, vars)
    },
    [locale],
  )

  const value = useMemo<I18nValue>(() => ({ locale, setLocale, t }), [locale, setLocale, t])
  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>
}

export function useI18n(): I18nValue {
  const ctx = useContext(I18nContext)
  if (!ctx) throw new Error('useI18n must be used inside an I18nProvider')
  return ctx
}

/** Convenience hook for components that only need the translator. */
export function useT() {
  return useI18n().t
}

export type { Dictionary }
