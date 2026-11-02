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
/**
 * The sign-in variant of `localizeError`.
 *
 * A blocked or suspended account is reported as 403 `forbidden`, the same code
 * the API uses for a permissions refusal, so `localizeError` alone would tell a
 * locked-out person they "do not have permission". The server's detail sentence
 * distinguishes the two here, and the sign-in page gets the account-state
 * message instead.
 */
export function localizeLoginError(error: unknown, t: TFunction, locale: Locale): string {
  if (error instanceof ApiError && error.status === 403 && /not active/i.test(error.message ?? '')) {
    return t('login.errors.account_inactive')
  }
  return localizeError(error, t, locale)
}

export function localizeError(error: unknown, t: TFunction, locale: Locale): string {
  if (error instanceof NetworkError) return t('errors.network')

  if (error instanceof ApiError) {
    // A blocked or suspended account comes back as 403 with the same code as a
    // permissions refusal, so the status alone cannot tell them apart. The login
    // flow sets `account_inactive` explicitly; here the generic forbidden
    // sentence is the fallback for every other 403.

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
