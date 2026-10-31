/**
 * Language and theme switches for the sign-in page.
 *
 * Both are plain, keyboard-reachable controls: a <select> for language (so the
 * native picker and its accessibility are reused) and a segmented radio group
 * for the three themes. Neither is a fake control — each writes through the
 * provider, which persists the choice and applies it without a reload.
 */
import { useId } from 'react'
import { LOCALES, useI18n, type Locale } from '../i18n'
import { THEME_MODES, useTheme, type ThemeMode } from '../theme'

const THEME_ICON: Record<ThemeMode, string> = {
  light: '☀',
  dark: '☾',
  system: '◐',
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
          {THEME_MODES.map((option) => (
            <button
              key={option}
              type="button"
              role="radio"
              aria-checked={mode === option}
              className={`segmented__item${mode === option ? ' segmented__item--active' : ''}`}
              onClick={() => setMode(option)}
              title={t(`theme.${option}` as 'theme.light')}
            >
              <span aria-hidden="true" className="segmented__icon">
                {THEME_ICON[option]}
              </span>
              <span className="segmented__text">{t(`theme.${option}` as 'theme.light')}</span>
            </button>
          ))}
        </div>
      </fieldset>
    </div>
  )
}
