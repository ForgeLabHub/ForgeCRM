/**
 * Turns an API failure into a message in the active locale.
 *
 * The server speaks one language (English) and returns a stable machine-readable
 * `error.code` alongside its human-readable `message`. Rendering `message`
 * directly is what produced an English sentence under a Russian interface. Here
 * the code selects a translated string, and the server's own text is only used
 * when a code has no translation — so a new server error still shows something
 * meaningful instead of a raw key.
 */
import { ApiError, NetworkError } from '../api/client'
import { translationExists, type Locale, type TFunction } from './index'

/** A translation key for every `ErrorCode` the client can receive. */
const CODE_KEYS = [
  'unauthorized',
  'forbidden',
  'not_found',
  'conflict',
  'rate_limited',
  'validation_failed',
  'bad_request',
  'disabled',
  'unavailable',
  'internal_error',
] as const

type ErrorKey = (typeof CODE_KEYS)[number]

function isKnownCode(code: string): code is ErrorKey {
  return (CODE_KEYS as readonly string[]).includes(code)
}

/**
 * Localises an error for display.
 *
 * `t` is the translator from `useI18n()`; `locale` is only needed for the
 * existence check on dynamically built keys. Field-level messages are left to
 * the caller, which renders them next to the field that failed.
 */
export function localizeError(error: unknown, t: TFunction, locale: Locale): string {
  if (error instanceof NetworkError) return t('errors.network')

  if (error instanceof ApiError) {
    // A validation failure carries per-field detail; the summary is generic so
    // the field messages stay the specific ones.
    if (error.code === 'validation_failed') return t('errors.validation_failed')

    if (isKnownCode(error.code) && translationExists(locale, `errors.${error.code}`)) {
      return t(`errors.${error.code}` as 'errors.unknown')
    }

    // An unrecognised code: prefer the server's own sentence over a bare key.
    const message = error.message?.trim()
    if (message) return message
  }

  if (error instanceof Error) {
    const message = error.message.trim()
    if (message) return message
  }

  return t('errors.unknown')
}
