/**
 * The single HTTP entry point for the application.
 *
 * Two rules drive the design:
 *
 * 1. Authentication is a server-side session in an HttpOnly cookie. The browser
 *    cannot read it, and this module never tries to. Nothing is kept in
 *    localStorage, so an XSS bug cannot exfiltrate a long-lived credential.
 * 2. Every mutating request needs the CSRF token the server derived for the
 *    current session. That token is not a secret on its own: it only proves the
 *    request came from our own page. It lives in memory, and is refreshed from
 *    `/auth/me` after a reload.
 */

export type ErrorCode =
  | 'bad_request'
  | 'validation_failed'
  | 'unauthorized'
  | 'forbidden'
  | 'not_found'
  | 'conflict'
  | 'rate_limited'
  | 'payload_too_large'
  | 'unsupported'
  | 'internal_error'
  | 'disabled'
  | 'mfa_required'

export interface ApiErrorBody {
  code: ErrorCode
  message: string
  details?: Record<string, string>
}

/**
 * The error response shape.
 *
 * The server returns RFC 9457 Problem Details members at the top level and
 * repeats the code and field errors under `error`. Only `error` is guaranteed
 * here: a service that speaks plain RFC 9457 and omits the nested object is
 * still handled, using `detail` for the message.
 */
export interface ApiErrorEnvelope {
  type?: string
  title?: string
  status?: number
  detail?: string
  instance?: string
  /** Whether resending the identical request could succeed. */
  retryable?: boolean
  request_id?: string
  correlation_id?: string
  error?: ApiErrorBody
}

/** An error the server described in its standard envelope. */
export class ApiError extends Error {
  readonly status: number
  readonly code: ErrorCode
  readonly details: Record<string, string>
  readonly requestId?: string
  /** True when resending the identical request could succeed. */
  readonly retryable: boolean

  constructor(status: number, body: ApiErrorBody, requestId?: string, retryable = false) {
    super(body.message)
    this.name = 'ApiError'
    this.status = status
    this.code = body.code
    this.details = body.details ?? {}
    this.requestId = requestId
    this.retryable = retryable
  }

  /** True when the session is gone and the user must sign in again. */
  get isAuthFailure(): boolean {
    return this.status === 401
  }

  /** True when the user is signed in but lacks the permission. */
  get isPermissionFailure(): boolean {
    return this.status === 403
  }
}

/** A transport-level failure: no response reached us at all. */
export class NetworkError extends Error {
  readonly cause?: unknown

  constructor(message: string, cause?: unknown) {
    super(message)
    this.name = 'NetworkError'
    this.cause = cause
  }
}

export interface ListResponse<T> {
  items: T[]
  total: number
  limit: number
  offset: number
}

export interface PaginationParams {
  limit?: number
  offset?: number
  q?: string
  sort?: string
  order?: 'asc' | 'desc'
  [key: string]: string | number | boolean | undefined
}

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  body?: unknown
  query?: PaginationParams
  signal?: AbortSignal
  /** Set for requests that must not carry the CSRF header, such as login. */
  skipCSRF?: boolean
}

/**
 * Holds the CSRF token for the current session.
 *
 * A module-level value rather than React state: the token is needed by code
 * that runs outside the component tree, and it must be cleared the moment the
 * session ends so a stale token is never sent.
 */
class CSRFTokenStore {
  private token: string | null = null

  set(token: string | null | undefined): void {
    this.token = token && token.length > 0 ? token : null
  }

  get(): string | null {
    return this.token
  }

  clear(): void {
    this.token = null
  }
}

export const csrfTokenStore = new CSRFTokenStore()

export interface ApiClientOptions {
  baseUrl?: string
  /** Called when a response indicates the session is no longer valid. */
  onUnauthorized?: () => void
}

export class ApiClient {
  private readonly baseUrl: string
  private onUnauthorized?: () => void

  constructor(options: ApiClientOptions = {}) {
    // Same-origin by default. The cookie and CSRF scheme only work this way, and
    // it means no CORS configuration is needed at all.
    this.baseUrl = options.baseUrl ?? ''
    this.onUnauthorized = options.onUnauthorized
  }

  setUnauthorizedHandler(handler: (() => void) | undefined): void {
    this.onUnauthorized = handler
  }

  private buildURL(path: string, query?: PaginationParams): string {
    const url = `${this.baseUrl}${path}`
    if (!query) return url
    const params = new URLSearchParams()
    for (const [key, value] of Object.entries(query)) {
      if (value === undefined || value === null || value === '') continue
      params.set(key, String(value))
    }
    const qs = params.toString()
    return qs ? `${url}?${qs}` : url
  }

  async request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const method = options.method ?? 'GET'
    const headers: Record<string, string> = { Accept: 'application/json' }

    const isMutation = method !== 'GET'
    if (isMutation && !options.skipCSRF) {
      const token = csrfTokenStore.get()
      if (token) headers['X-CSRF-Token'] = token
    }

    let body: BodyInit | undefined
    if (options.body !== undefined) {
      // FormData must keep its own generated Content-Type, which carries the
      // multipart boundary. Setting it by hand would break the upload.
      if (options.body instanceof FormData) {
        body = options.body
      } else {
        headers['Content-Type'] = 'application/json'
        body = JSON.stringify(options.body)
      }
    }

    let response: Response
    try {
      response = await fetch(this.buildURL(path, options.query), {
        method,
        headers,
        body,
        credentials: 'same-origin',
        signal: options.signal,
      })
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') throw error
      throw new NetworkError('Could not reach the server.', error)
    }

    if (response.status === 401) {
      csrfTokenStore.clear()
      this.onUnauthorized?.()
    }

    if (response.status === 204) return undefined as T

    const text = await response.text()
    let parsed: unknown = undefined
    if (text.length > 0) {
      try {
        parsed = JSON.parse(text)
      } catch {
        if (!response.ok) {
          throw new ApiError(
            response.status,
            { code: 'internal_error', message: `Unexpected response (HTTP ${response.status}).` },
            response.headers.get('X-Request-ID') ?? undefined,
          )
        }
        throw new NetworkError('The server returned a response that is not JSON.')
      }
    }

    if (!response.ok) {
      const envelope = parsed as ApiErrorEnvelope | undefined
      // Prefer the nested object, which carries the field-level details; fall
      // back to the RFC 9457 members so a service that returns bare Problem
      // Details still yields a readable message rather than a generic one.
      const errBody: ApiErrorBody = envelope?.error ?? {
        code: 'internal_error',
        message:
          envelope?.detail ?? `Request failed (HTTP ${response.status}).`,
      }
      throw new ApiError(
        response.status,
        errBody,
        envelope?.request_id ?? response.headers.get('X-Request-ID') ?? undefined,
        envelope?.retryable ?? false,
      )
    }

    return parsed as T
  }

  get<T>(path: string, query?: PaginationParams, signal?: AbortSignal): Promise<T> {
    return this.request<T>(path, { method: 'GET', query, signal })
  }

  post<T>(path: string, body?: unknown, options: { skipCSRF?: boolean } = {}): Promise<T> {
    return this.request<T>(path, { method: 'POST', body, skipCSRF: options.skipCSRF })
  }

  put<T>(path: string, body?: unknown): Promise<T> {
    return this.request<T>(path, { method: 'PUT', body })
  }

  delete<T>(path: string, query?: PaginationParams): Promise<T> {
    return this.request<T>(path, { method: 'DELETE', query })
  }
}

/** The application client. Tests construct their own instance with a stub fetch. */
export const api = new ApiClient()

/** Turns any thrown value into a message safe to show a user. */
export function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.code === 'validation_failed' && Object.keys(error.details).length > 0) {
      return Object.values(error.details).join(' ')
    }
    return error.message
  }
  if (error instanceof NetworkError) return error.message
  if (error instanceof Error) return error.message
  return 'Something went wrong.'
}

/** Field-level validation messages, when the server supplied them. */
export function fieldErrors(error: unknown): Record<string, string> {
  if (error instanceof ApiError) return error.details
  return {}
}
