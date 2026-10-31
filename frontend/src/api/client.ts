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
  | 'unavailable'
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
  /**
   * Makes a mutating request safe to retry.
   *
   * When set, the server runs the operation at most once for this key: a repeat
   * returns the first result instead of creating a second record. Callers that
   * retry on a network failure should pass a key that is stable across the retry
   * (generate it once per user intent, not once per attempt), otherwise the retry
   * is a second request and the protection is lost.
   */
  idempotencyKey?: string
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
    if (isMutation && options.idempotencyKey) {
      headers['Idempotency-Key'] = options.idempotencyKey
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

  /**
   * Fetches a response whose body is a file rather than JSON.
   *
   * A plain link or `window.open` cannot be used for an export: the request is a
   * POST and it needs the CSRF header, which only a fetch we make ourselves can
   * set. The caller turns the returned blob into a download.
   */
  async download(path: string, body?: unknown): Promise<{ blob: Blob; filename?: string }> {
    const headers: Record<string, string> = { Accept: '*/*' }
    const token = csrfTokenStore.get()
    if (token) headers['X-CSRF-Token'] = token
    let response: Response
    try {
      response = await fetch(`${this.baseUrl}${path}`, {
        method: 'POST',
        headers: {
          ...headers,
          'Content-Type': 'application/json',
        },
        body: body === undefined ? undefined : JSON.stringify(body),
        credentials: 'same-origin',
      })
    } catch (error) {
      throw new NetworkError('Could not reach the server.', error)
    }

    if (response.status === 401) {
      csrfTokenStore.clear()
      this.onUnauthorized?.()
    }

    if (!response.ok) {
      // A failed export answers with the JSON error envelope, not a file.
      const text = await response.text()
      let envelope: ApiErrorEnvelope | undefined
      try {
        envelope = JSON.parse(text) as ApiErrorEnvelope
      } catch {
        envelope = undefined
      }
      const errBody: ApiErrorBody = envelope?.error ?? {
        code: 'internal_error',
        message: envelope?.detail ?? `Request failed (HTTP ${response.status}).`,
      }
      throw new ApiError(
        response.status,
        errBody,
        envelope?.request_id ?? response.headers.get('X-Request-ID') ?? undefined,
        envelope?.retryable ?? false,
      )
    }

    return { blob: await response.blob(), filename: filenameFromDisposition(response.headers.get('Content-Disposition')) }
  }

  post<T>(path: string, body?: unknown, options: MutationOptions = {}): Promise<T> {
    return this.request<T>(path, {
      method: 'POST',
      body,
      skipCSRF: options.skipCSRF,
      idempotencyKey: options.idempotencyKey,
    })
  }

  /**
   * Posts a request and consumes a server-sent-event stream.
   *
   * EventSource cannot be used here: it only issues GET requests and cannot send
   * the CSRF header, and both are required. The stream is read with fetch and a
   * reader, and each `event:`/`data:` frame is handed to the caller as it arrives.
   *
   * The `error` event the server sends once the stream is open is turned into an
   * ApiError, so a caller handles a streamed failure the same way it handles a
   * buffered one. A non-2xx response before the stream opens is a normal error
   * envelope and is raised as an ApiError too.
   */
  async stream(
    path: string,
    body: unknown,
    onEvent: (event: { event: string; data: unknown }) => void,
    signal?: AbortSignal,
  ): Promise<void> {
    const headers: Record<string, string> = {
      Accept: 'text/event-stream',
      'Content-Type': 'application/json',
    }
    const token = csrfTokenStore.get()
    if (token) headers['X-CSRF-Token'] = token

    let response: Response
    try {
      response = await fetch(`${this.baseUrl}${path}`, {
        method: 'POST',
        headers,
        body: JSON.stringify(body),
        credentials: 'same-origin',
        signal,
      })
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') throw error
      throw new NetworkError('Could not reach the server.', error)
    }

    if (response.status === 401) {
      csrfTokenStore.clear()
      this.onUnauthorized?.()
    }

    if (!response.ok) {
      const text = await response.text()
      let envelope: ApiErrorEnvelope | undefined
      try {
        envelope = JSON.parse(text) as ApiErrorEnvelope
      } catch {
        envelope = undefined
      }
      const errBody: ApiErrorBody = envelope?.error ?? {
        code: 'internal_error',
        message: envelope?.detail ?? `Request failed (HTTP ${response.status}).`,
      }
      throw new ApiError(
        response.status,
        errBody,
        envelope?.request_id ?? response.headers.get('X-Request-ID') ?? undefined,
        envelope?.retryable ?? false,
      )
    }
    if (!response.body) {
      throw new NetworkError('The server returned an empty stream.')
    }

    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    try {
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        // Frames are separated by a blank line. A trailing partial frame stays in
        // the buffer until the rest of it arrives.
        let sep: number
        while ((sep = buffer.indexOf('\n\n')) !== -1) {
          const frame = buffer.slice(0, sep)
          buffer = buffer.slice(sep + 2)
          const parsed = parseSSEFrame(frame)
          if (!parsed) continue
          if (parsed.event === 'error') {
            throw streamEventError(parsed.data)
          }
          onEvent(parsed)
        }
      }
      // A final frame without a trailing blank line still counts.
      const parsed = parseSSEFrame(buffer)
      if (parsed) {
        if (parsed.event === 'error') throw streamEventError(parsed.data)
        onEvent(parsed)
      }
    } finally {
      reader.releaseLock()
    }
  }

  put<T>(path: string, body?: unknown, options: MutationOptions = {}): Promise<T> {
    return this.request<T>(path, {
      method: 'PUT',
      body,
      idempotencyKey: options.idempotencyKey,
    })
  }

  delete<T>(path: string, query?: PaginationParams, options: MutationOptions = {}): Promise<T> {
    return this.request<T>(path, {
      method: 'DELETE',
      query,
      idempotencyKey: options.idempotencyKey,
    })
  }
}

/**
 * Options common to the mutating helpers.
 *
 * `idempotencyKey` is accepted here rather than only on `request` so callers use
 * the named helpers and still get the retry guarantee; a bare `request` call is
 * the exception, not the rule.
 */
export interface MutationOptions {
  skipCSRF?: boolean
  idempotencyKey?: string
}

/**
 * Generates a key for one user intent.
 *
 * `crypto.randomUUID` is available in every browser this application targets and
 * in the Node test environment. The key is generated once per intent and reused
 * across retries of that intent, which is what makes the retry safe: a new key per
 * attempt would be a new request and would defeat the mechanism.
 */
export function newIdempotencyKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  // A fallback for a runtime without Web Crypto: uniqueness only needs to hold
  // within one caller's in-flight requests, not globally.
  return `idem-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
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

/**
 * Parses one server-sent-event frame into an event name and its decoded data.
 *
 * Only the `event` and `data` fields are read: the stream never sends `id` or
 * `retry`, and a `:` comment line is ignored, which is the standard way a
 * keep-alive comment is written. Multiple `data:` lines are joined with a newline
 * as the event-stream format requires, though this server always sends one.
 */
function parseSSEFrame(frame: string): { event: string; data: unknown } | null {
  let event = 'message'
  const dataLines: string[] = []
  for (const rawLine of frame.split('\n')) {
    const line = rawLine.replace(/\r$/, '')
    if (line === '' || line.startsWith(':')) continue
    if (line.startsWith('event:')) {
      event = line.slice('event:'.length).trim()
    } else if (line.startsWith('data:')) {
      dataLines.push(line.slice('data:'.length).replace(/^ /, ''))
    }
  }
  if (dataLines.length === 0) return null
  const raw = dataLines.join('\n')
  let data: unknown = raw
  try {
    data = JSON.parse(raw)
  } catch {
    // Leave it as text: the caller can still show something, and the alternative
    // is dropping the event entirely.
  }
  return { event, data }
}

/**
 * Turns the payload of a streamed `error` event into an ApiError, so a caller
 * handles a failure that arrived mid-stream exactly like one that arrived as an
 * HTTP status.
 */
function streamEventError(data: unknown): ApiError {
  const body = (data ?? {}) as Partial<ApiErrorBody> & { retryable?: boolean }
  return new ApiError(
    200,
    {
      code: (body.code as ErrorCode) ?? 'internal_error',
      message: body.message ?? 'The request failed.',
    },
    undefined,
    body.retryable ?? false,
  )
}

/**
 * Reads the filename out of a Content-Disposition header.
 *
 * The server writes `attachment; filename="accounts-1712345678.csv"`. The value is
 * returned bare so the caller can name the downloaded file; an absent or
 * unparseable header yields undefined and the caller falls back to its own name.
 */
export function filenameFromDisposition(header: string | null): string | undefined {
  if (!header) return undefined
  const quoted = /filename="([^"]+)"/i.exec(header)
  if (quoted) return quoted[1]
  const bare = /filename=([^;]+)/i.exec(header)
  return bare ? bare[1].trim() : undefined
}
