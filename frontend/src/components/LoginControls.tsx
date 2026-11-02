/**
 * Language and theme switches for the sign-in page.
 *
 * Both are real controls that write through their provider, so the choice is
 * applied without a reload and persisted for the next visit. The theme control
 * is a segmented radio group drawn with the same icon set as the rest of the
 * product — an emoji here would be the one glyph on the page from a different
 * family. The language control is a native <select>, which reuses the platform
 * picker and its accessibility for free.
 */
import { useId } from 'react'
import type { JSX } from 'react'
import { LOCALES, useI18n, type Locale } from '../i18n'
import { THEME_MODES, useTheme, type ThemeMode } from '../theme'
import { IconMonitor, IconMoon, IconSun, type IconProps } from './icons'

const THEME_ICON: Record<ThemeMode, (props: IconProps) => JSX.Element> = {
  light: IconSun,
  dark: IconMoon,
  system: IconMonitor,
}

const THEME_LABEL: Record<ThemeMode, 'theme.light' | 'theme.dark' | 'theme.system'> = {
  light: 'theme.light',
  dark: 'theme.dark',
  system: 'theme.system',
}

export function LoginControls() {
  const { locale, setLocale, t } = useI18n()
  const { mode, setMode } = useTheme()
  const groupId = useId()

  return (
    <div className="login-controls">
      <div className="login-controls__group">
        <label className="login-controls__label" htmlFor={`${groupId}-lang`}>
          {t('language.label')}
        </label>
        <select
          id={`${groupId}-lang`}
          className="login-controls__select"
          value={locale}
          onChange={(event) => setLocale(event.target.value as Locale)}
        >
          {LOCALES.map((code) => (
            <option key={code} value={code}>
              {t(`language.${code}` as 'language.en')}
            </option>
          ))}
        </select>
      </div>

      <fieldset className="login-controls__group login-controls__group--theme">
        <legend className="login-controls__label">{t('theme.label')}</legend>
        <div className="segmented" role="radiogroup" aria-label={t('theme.label')}>
          {THEME_MODES.map((option) => {
            const ThemeIcon = THEME_ICON[option]
            const label = t(THEME_LABEL[option])
            return (
              <button
                key={option}
                type="button"
                role="radio"
                aria-checked={mode === option}
                className={`segmented__item${mode === option ? ' segmented__item--active' : ''}`}
                onClick={() => setMode(option)}
                title={label}
              >
                <ThemeIcon size={14} />
                <span className="segmented__text">{label}</span>
              </button>
            )
          })}
        </div>
      </fieldset>
    </div>
  )
}
