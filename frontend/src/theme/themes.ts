/**
 * The visual styles the whole application can wear.
 *
 * A style is not a colour scheme bolted onto the existing palette: each one is a
 * complete set of design tokens (colour, radius, shadow, and a few structural
 * rules) applied to the root element, so it restyles every screen — dashboard,
 * tables, forms, modals, the sidebar — from one attribute. The palettes live in
 * `index.css` keyed on `[data-style='…']`; this module is the catalogue the UI
 * renders and the list the provider validates a stored choice against.
 *
 * `default` is the palette the application shipped with and is the only style
 * that follows the light/dark/system mode. The ten named styles carry their own
 * palette so a switch changes the character of the app, not just its accent.
 */
import type { TranslationKey } from '../i18n'

export const THEME_STYLES = [
  'default',
  'minimal',
  'midnight',
  'glass',
  'premium',
  'brutalist',
  'cyberpunk',
  'soft',
  'editorial',
  'warm',
  'colorful',
] as const

export type ThemeStyle = (typeof THEME_STYLES)[number]

/** The ten selectable styles, in the order the appearance page shows them. */
export const SELECTABLE_STYLES: readonly ThemeStyle[] = THEME_STYLES.filter(
  (style) => style !== 'default',
)

export const DEFAULT_STYLE: ThemeStyle = 'default'

export const STYLE_STORAGE_KEY = 'forgecrm.style'

/** Accent colours offered independently of the style. */
export const ACCENTS = ['theme', 'indigo', 'emerald', 'amber', 'rose', 'cyan'] as const
export type Accent = (typeof ACCENTS)[number]
export const DEFAULT_ACCENT: Accent = 'theme'
export const ACCENT_STORAGE_KEY = 'forgecrm.accent'

/** How tight the layout is. Density is a token multiplier, not a separate sheet. */
export const DENSITIES = ['compact', 'standard', 'spacious'] as const
export type Density = (typeof DENSITIES)[number]
export const DEFAULT_DENSITY: Density = 'standard'
export const DENSITY_STORAGE_KEY = 'forgecrm.density'

/** The label key for a style card. `default` reuses the light/dark wording. */
export function styleLabelKey(style: ThemeStyle): TranslationKey {
  return `appearance.style.${style}.name` as TranslationKey
}

export function styleDescriptionKey(style: ThemeStyle): TranslationKey {
  return `appearance.style.${style}.description` as TranslationKey
}

export function accentLabelKey(accent: Accent): TranslationKey {
  return `appearance.accent.${accent}` as TranslationKey
}

export function densityLabelKey(density: Density): TranslationKey {
  return `appearance.density.${density}` as TranslationKey
}

/**
 * A few representative colours per style, for the picker's thumbnail.
 *
 * The live styles are CSS blocks keyed on a root attribute, which a small
 * element cannot opt into without duplicating the whole token set. A thumbnail
 * needs four colours and no behaviour, so they are listed here explicitly. They
 * mirror the values in index.css; the picker is a preview, not a second source
 * of truth for the theme.
 */
export const STYLE_SWATCH: Record<ThemeStyle, { bg: string; surface: string; accent: string; text: string }> = {
  default: { bg: '#f2f4f8', surface: '#ffffff', accent: '#2258d8', text: '#101828' },
  minimal: { bg: '#ffffff', surface: '#f7f7f8', accent: '#111827', text: '#16181d' },
  midnight: { bg: '#070a12', surface: '#0e1422', accent: '#5b8cff', text: '#e7ecf5' },
  glass: { bg: '#e9eefb', surface: 'rgba(255,255,255,0.72)', accent: '#5b4bf0', text: '#101828' },
  premium: { bg: '#0f1115', surface: '#171a21', accent: '#c9a227', text: '#f3f1ec' },
  brutalist: { bg: '#f5f5ef', surface: '#ffffff', accent: '#000000', text: '#000000' },
  cyberpunk: { bg: '#05010f', surface: '#0c0420', accent: '#ff2bd6', text: '#eafcff' },
  soft: { bg: '#f6f7fb', surface: '#ffffff', accent: '#4b62d8', text: '#1f2430' },
  editorial: { bg: '#fbfbf9', surface: '#ffffff', accent: '#1a1a1a', text: '#14140f' },
  warm: { bg: '#faf5ef', surface: '#fffdfa', accent: '#9c5220', text: '#3a2f26' },
  colorful: { bg: '#f3f0ff', surface: '#ffffff', accent: '#7c3aed', text: '#1b1533' },
}

export function isThemeStyle(value: string | null): value is ThemeStyle {
  return value !== null && (THEME_STYLES as readonly string[]).includes(value)
}

export function isAccent(value: string | null): value is Accent {
  return value !== null && (ACCENTS as readonly string[]).includes(value)
}

export function isDensity(value: string | null): value is Density {
  return value !== null && (DENSITIES as readonly string[]).includes(value)
}
