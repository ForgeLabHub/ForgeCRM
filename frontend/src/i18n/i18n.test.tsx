/**
 * Localisation and theme behaviour.
 *
 * These cover the properties that are easy to break and hard to see: the choice
 * survives a reload, a missing key falls back rather than blanking a label, and
 * "system" resolves against the live OS preference instead of a snapshot taken
 * once at start.
 */
import { describe, expect, it, beforeEach, vi } from 'vitest'
import { render, screen, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nProvider, useI18n, resolveInitialLocale } from './index'
import {
  ThemeProvider,
  useTheme,
  resolveTheme,
  resolveInitialMode,
  THEME_STORAGE_KEY,
} from '../theme'
import { en } from './en'
import { ru } from './ru'

function LocaleProbe() {
  const { locale, setLocale, t } = useI18n()
  return (
    <div>
      <span data-testid="locale">{locale}</span>
      <span data-testid="title">{t('login.title')}</span>
      <button type="button" onClick={() => setLocale(locale === 'en' ? 'ru' : 'en')}>
        toggle
      </button>
    </div>
  )
}

function ThemeProbe() {
  const { mode, resolved, setMode } = useTheme()
  return (
    <div>
      <span data-testid="mode">{mode}</span>
      <span data-testid="resolved">{resolved}</span>
      <button type="button" onClick={() => setMode('light')}>
        light
      </button>
      <button type="button" onClick={() => setMode('dark')}>
        dark
      </button>
      <button type="button" onClick={() => setMode('system')}>
        system
      </button>
    </div>
  )
}

beforeEach(() => {
  window.localStorage.clear()
  document.documentElement.removeAttribute('data-theme')
})

describe('locale', () => {
  it('switches language without a reload and updates the dictionary', async () => {
    render(
      <I18nProvider>
        <LocaleProbe />
      </I18nProvider>,
    )
    expect(screen.getByTestId('title').textContent).toBe(en.login.title)
    await userEvent.click(screen.getByRole('button', { name: 'toggle' }))
    expect(screen.getByTestId('locale').textContent).toBe('ru')
    expect(screen.getByTestId('title').textContent).toBe(ru.login.title)
  })

  it('persists the choice and restores it on the next visit', async () => {
    const first = render(
      <I18nProvider>
        <LocaleProbe />
      </I18nProvider>,
    )
    await userEvent.click(screen.getByRole('button', { name: 'toggle' }))
    expect(window.localStorage.getItem('forgecrm.locale')).toBe('ru')
    first.unmount()

    expect(resolveInitialLocale()).toBe('ru')
  })

  it('applies the locale to the document lang attribute', async () => {
    render(
      <I18nProvider>
        <LocaleProbe />
      </I18nProvider>,
    )
    await userEvent.click(screen.getByRole('button', { name: 'toggle' }))
    expect(document.documentElement.lang).toBe('ru')
  })

  it('keeps both dictionaries the same shape', () => {
    const flatten = (obj: Record<string, unknown>, prefix = ''): string[] =>
      Object.entries(obj).flatMap(([key, value]) =>
        typeof value === 'string'
          ? [`${prefix}${key}`]
          : flatten(value as Record<string, unknown>, `${prefix}${key}.`),
      )
    expect(flatten(ru).sort()).toEqual(flatten(en).sort())
  })

  it('leaves no Cyrillic text in the English dictionary outside language names', () => {
    // Language names are shown in their own script on purpose ("Русский" is the
    // right label for a Russian speaker in any locale), so that namespace is the
    // one legitimate exception. Everywhere else a Cyrillic string in the English
    // dictionary is a string that was never translated.
    const cyrillic = /[\u0400-\u04FF]/
    const walk = (obj: Record<string, unknown>): boolean =>
      Object.values(obj).some((value) =>
        typeof value === 'string' ? cyrillic.test(value) : walk(value as Record<string, unknown>),
      )
    const { language: _language, ...rest } = en
    expect(walk(rest)).toBe(false)
  })
})

describe('theme', () => {
  it('resolves system against the OS preference', () => {
    expect(resolveTheme('system', true)).toBe('dark')
    expect(resolveTheme('system', false)).toBe('light')
    expect(resolveTheme('light', true)).toBe('light')
    expect(resolveTheme('dark', false)).toBe('dark')
  })

  it('writes the resolved theme to the document element', async () => {
    render(
      <ThemeProvider>
        <ThemeProbe />
      </ThemeProvider>,
    )
    await userEvent.click(screen.getByRole('button', { name: 'dark' }))
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
    await userEvent.click(screen.getByRole('button', { name: 'light' }))
    expect(document.documentElement.getAttribute('data-theme')).toBe('light')
  })

  it('persists the chosen mode and restores it', async () => {
    const view = render(
      <ThemeProvider>
        <ThemeProbe />
      </ThemeProvider>,
    )
    await userEvent.click(screen.getByRole('button', { name: 'dark' }))
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark')
    view.unmount()

    expect(resolveInitialMode()).toBe('dark')
  })

  it('follows a live change of the OS preference while in system mode', async () => {
    // A controllable matchMedia: the provider must subscribe, not sample once.
    let listener: ((event: MediaQueryListEvent) => void) | null = null
    const matches = { value: false }
    vi.stubGlobal(
      'matchMedia',
      vi.fn(() => ({
        get matches() {
          return matches.value
        },
        media: '(prefers-color-scheme: dark)',
        addEventListener: (_: string, cb: (event: MediaQueryListEvent) => void) => {
          listener = cb
        },
        removeEventListener: () => {},
      })),
    )

    render(
      <ThemeProvider>
        <ThemeProbe />
      </ThemeProvider>,
    )
    expect(screen.getByTestId('resolved').textContent).toBe('light')

    matches.value = true
    act(() => {
      listener?.({ matches: true } as MediaQueryListEvent)
    })
    expect(screen.getByTestId('resolved').textContent).toBe('dark')
  })
})
