/**
 * Tests for the HTTP client.
 *
 * These exercise the real client against a stubbed `fetch`, because the
 * behaviour that matters here — CSRF header handling, error-envelope parsing
 * and the session-expiry callback — lives in the client itself, not in any
 * component, and a mock of the client would test nothing.
 */
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  ApiClient,
  ApiError,
  NetworkError,
  csrfTokenStore,
  errorMessage,
  fieldErrors,
  filenameFromDisposition,
  newIdempotencyKey,
} from './client'

/** Builds a client whose fetch returns one canned response. */
function clientWithResponse(response: Response, onUnauthorized?: () => void) {
  const fetchMock = vi.fn().mockResolvedValue(response)
  vi.stubGlobal('fetch', fetchMock)
  const client = new ApiClient({ onUnauthorized })
  return { client, fetchMock }
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

afterEach(() => {
  csrfTokenStore.clear()
})

describe('Idempotency-Key handling', () => {
  it('sends the header on a mutation when a key is given', async () => {
    const { client, fetchMock } = clientWithResponse(jsonResponse({ ok: true }))

    await client.post('/api/v1/contacts', { name: 'Acme' }, { idempotencyKey: 'key-1' })

    const headers = (fetchMock.mock.calls[0][1] as RequestInit).headers as Record<string, string>
    expect(headers['Idempotency-Key']).toBe('key-1')
  })

  it('omits the header when no key is given, so the feature stays opt-in', async () => {
    const { client, fetchMock } = clientWithResponse(jsonResponse({ ok: true }))

    await client.post('/api/v1/contacts', { name: 'Acme' })

    const headers = (fetchMock.mock.calls[0][1] as RequestInit).headers as Record<string, string>
    expect(headers['Idempotency-Key']).toBeUndefined()
  })

  it('never sends the header on a GET', async () => {
    const { client, fetchMock } = clientWithResponse(jsonResponse({ items: [] }))

    // A key on a read would be ignored by the server, so the client must not send one.
    await client.request('/api/v1/contacts', { method: 'GET', idempotencyKey: 'key-1' })

    const headers = (fetchMock.mock.calls[0][1] as RequestInit).headers as Record<string, string>
    expect(headers['Idempotency-Key']).toBeUndefined()
  })

  it('sends the header on PUT and DELETE', async () => {
    // Each response body can only be read once, so the stub must return a fresh
    // one per call.
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ ok: true }))
      .mockResolvedValueOnce(jsonResponse({ ok: true }))
    vi.stubGlobal('fetch', fetchMock)
    const client = new ApiClient()

    await client.put('/api/v1/contacts/1', { name: 'Acme' }, { idempotencyKey: 'put-1' })
    await client.delete('/api/v1/contacts/1', undefined, { idempotencyKey: 'del-1' })

    const putHeaders = (fetchMock.mock.calls[0][1] as RequestInit).headers as Record<string, string>
    const delHeaders = (fetchMock.mock.calls[1][1] as RequestInit).headers as Record<string, string>
    expect(putHeaders['Idempotency-Key']).toBe('put-1')
    expect(delHeaders['Idempotency-Key']).toBe('del-1')
  })
})

describe('newIdempotencyKey', () => {
  it('returns a non-empty value', () => {
    expect(newIdempotencyKey().length).toBeGreaterThan(0)
  })

  it('returns a different value on each call, so distinct intents do not collide', () => {
    const keys = new Set(Array.from({ length: 100 }, () => newIdempotencyKey()))
    expect(keys.size).toBe(100)
  })
})

describe('CSRF handling', () => {
  it('omits the CSRF header when no token has been issued yet', async () => {
    const { client, fetchMock } = clientWithResponse(jsonResponse({ ok: true }))

    await client.post('/api/v1/contacts', { name: 'Acme' })

    const init = fetchMock.mock.calls[0][1] as RequestInit
    const headers = init.headers as Record<string, string>
    expect(headers['X-CSRF-Token']).toBeUndefined()
  })

  it('sends the CSRF header on a mutation once a token is known', async () => {
    csrfTokenStore.set('token-abc')
    const { client, fetchMock } = clientWithResponse(jsonResponse({ ok: true }))

    await client.post('/api/v1/contacts', { name: 'Acme' })

    const headers = (fetchMock.mock.calls[0][1] as RequestInit).headers as Record<string, string>
    expect(headers['X-CSRF-Token']).toBe('token-abc')
  })

  it('never sends the CSRF header on a GET', async () => {
    csrfTokenStore.set('token-abc')
    const { client, fetchMock } = clientWithResponse(jsonResponse({ items: [] }))

    await client.get('/api/v1/contacts')

    const headers = (fetchMock.mock.calls[0][1] as RequestInit).headers as Record<string, string>
    expect(headers['X-CSRF-Token']).toBeUndefined()
  })

  it('omits the header when skipCSRF is set, as login must', async () => {
    csrfTokenStore.set('stale-token')
    const { client, fetchMock } = clientWithResponse(jsonResponse({ ok: true }))

    await client.post('/api/v1/auth/login', { email: 'a@b.c' }, { skipCSRF: true })

    const headers = (fetchMock.mock.calls[0][1] as RequestInit).headers as Record<string, string>
    expect(headers['X-CSRF-Token']).toBeUndefined()
  })
})

describe('request bodies', () => {
  it('serialises a JSON body and sets the content type', async () => {
    const { client, fetchMock } = clientWithResponse(jsonResponse({ id: '1' }))

    await client.post('/api/v1/contacts', { first_name: 'Ada' })

    const init = fetchMock.mock.calls[0][1] as RequestInit
    expect(init.body).toBe(JSON.stringify({ first_name: 'Ada' }))
    expect((init.headers as Record<string, string>)['Content-Type']).toBe('application/json')
  })

  it('leaves a FormData body alone so the multipart boundary survives', async () => {
    const { client, fetchMock } = clientWithResponse(jsonResponse({ id: '1' }))
    const form = new FormData()
    form.append('file', new Blob(['a,b']), 'data.csv')

    await client.post('/api/v1/imports/1/upload', form)

    const init = fetchMock.mock.calls[0][1] as RequestInit
    expect(init.body).toBe(form)
    // Setting Content-Type by hand would drop the boundary and break the upload.
    expect((init.headers as Record<string, string>)['Content-Type']).toBeUndefined()
  })

  it('encodes query parameters and drops empty ones', async () => {
    const { client, fetchMock } = clientWithResponse(jsonResponse({ items: [] }))

    await client.get('/api/v1/contacts', { limit: 25, offset: 0, q: '', status: 'active' })

    const url = fetchMock.mock.calls[0][0] as string
    expect(url).toContain('limit=25')
    expect(url).toContain('offset=0')
    expect(url).toContain('status=active')
    expect(url).not.toContain('q=')
  })

  it('always sends credentials so the session cookie is included', async () => {
    const { client, fetchMock } = clientWithResponse(jsonResponse({}))

    await client.get('/api/v1/auth/me')

    expect((fetchMock.mock.calls[0][1] as RequestInit).credentials).toBe('same-origin')
  })
})

describe('responses', () => {
  it('returns undefined for 204 without trying to parse a body', async () => {
    const { client } = clientWithResponse(new Response(null, { status: 204 }))

    await expect(client.delete('/api/v1/contacts/1')).resolves.toBeUndefined()
  })

  it('parses the error envelope into an ApiError', async () => {
    const { client } = clientWithResponse(
      jsonResponse(
        { error: { code: 'validation_failed', message: 'Invalid', details: { email: 'required' } } },
        422,
      ),
    )

    await expect(client.post('/api/v1/contacts', {})).rejects.toMatchObject({
      name: 'ApiError',
      status: 422,
      code: 'validation_failed',
      details: { email: 'required' },
    })
  })

  it('falls back to a generic error when the failure body is not the envelope', async () => {
    const { client } = clientWithResponse(new Response('<html>502</html>', { status: 502 }))

    const error = await client.get('/api/v1/contacts').catch((e: unknown) => e)
    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).status).toBe(502)
    expect((error as ApiError).code).toBe('internal_error')
  })

  it('reads a bare RFC 9457 problem response', async () => {
    // A service that speaks plain Problem Details omits the nested `error`
    // object. The client must still surface the server's detail text rather
    // than a generic "Request failed".
    const { client } = clientWithResponse(
      jsonResponse(
        {
          type: 'urn:forgecrm:error:conflict',
          title: 'Conflict',
          status: 409,
          detail: 'A contact with that email already exists',
          instance: '/api/v1/contacts',
        },
        409,
      ),
    )

    const error = (await client.post('/api/v1/contacts', {}).catch((e: unknown) => e)) as ApiError
    expect(error).toBeInstanceOf(ApiError)
    expect(error.status).toBe(409)
    expect(error.message).toBe('A contact with that email already exists')
  })

  it('carries the retryable flag from the envelope', async () => {
    const { client } = clientWithResponse(
      jsonResponse(
        {
          type: 'urn:forgecrm:error:rate_limited',
          status: 429,
          detail: 'slow down',
          retryable: true,
          error: { code: 'rate_limited', message: 'slow down' },
        },
        429,
      ),
    )

    const error = (await client.get('/api/v1/contacts').catch((e: unknown) => e)) as ApiError
    expect(error.retryable).toBe(true)
    // A client that does not know about the flag must not retry by default.
    expect(new ApiError(500, { code: 'internal_error', message: 'x' }).retryable).toBe(false)
  })

  it('reports a transport failure as a NetworkError', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('failed to fetch')))
    const client = new ApiClient()

    await expect(client.get('/api/v1/contacts')).rejects.toBeInstanceOf(NetworkError)
  })
})

describe('session expiry', () => {
  it('clears the CSRF token and notifies the app on a 401', async () => {
    csrfTokenStore.set('token-abc')
    const onUnauthorized = vi.fn()
    const { client } = clientWithResponse(
      jsonResponse({ error: { code: 'unauthorized', message: 'Session expired' } }, 401),
      onUnauthorized,
    )

    await client.get('/api/v1/auth/me').catch(() => undefined)

    expect(csrfTokenStore.get()).toBeNull()
    expect(onUnauthorized).toHaveBeenCalledOnce()
  })

  it('does not treat a 403 as a session expiry', async () => {
    csrfTokenStore.set('token-abc')
    const onUnauthorized = vi.fn()
    const { client } = clientWithResponse(
      jsonResponse({ error: { code: 'forbidden', message: 'Not allowed' } }, 403),
      onUnauthorized,
    )

    await client.get('/api/v1/contacts').catch(() => undefined)

    // A permission failure must not sign the user out.
    expect(csrfTokenStore.get()).toBe('token-abc')
    expect(onUnauthorized).not.toHaveBeenCalled()
  })
})

describe('error presentation', () => {
  it('joins validation details so the user sees every problem', () => {
    const error = new ApiError(422, {
      code: 'validation_failed',
      message: 'Invalid',
      details: { email: 'must be unique', name: 'is required' },
    })

    expect(errorMessage(error)).toBe('must be unique is required')
    expect(fieldErrors(error)).toEqual({ email: 'must be unique', name: 'is required' })
  })

  it('prefers the server message for non-validation errors', () => {
    const error = new ApiError(409, { code: 'conflict', message: 'Already exists' })
    expect(errorMessage(error)).toBe('Already exists')
  })

  it('describes unknown thrown values safely', () => {
    expect(errorMessage(new Error('boom'))).toBe('boom')
    expect(errorMessage('a string')).toBe('Something went wrong.')
    expect(fieldErrors(new Error('boom'))).toEqual({})
  })
})

describe('ApiError classification', () => {
  it('distinguishes an expired session from a missing permission', () => {
    expect(new ApiError(401, { code: 'unauthorized', message: 'x' }).isAuthFailure).toBe(true)
    expect(new ApiError(403, { code: 'forbidden', message: 'x' }).isPermissionFailure).toBe(true)
    expect(new ApiError(403, { code: 'forbidden', message: 'x' }).isAuthFailure).toBe(false)
  })
})

describe('file downloads', () => {
  it('reads a blob and the filename the server named', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response('id,name\n1,Acme\n', {
        status: 200,
        headers: {
          'Content-Type': 'text/csv; charset=utf-8',
          'Content-Disposition': 'attachment; filename="account-1712345678.csv"',
        },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)
    const client = new ApiClient()

    const { blob, filename } = await client.download('/api/v1/exports', { entity_type: 'account' })

    expect(filename).toBe('account-1712345678.csv')
    expect(blob.size).toBeGreaterThan(0)
    expect(blob.type).toContain('text/csv')

    // The download is a POST that carries the CSRF header, which is why a plain
    // link cannot be used.
    const init = fetchMock.mock.calls[0][1] as RequestInit
    expect(init.method).toBe('POST')
    expect(init.credentials).toBe('same-origin')
  })

  it('raises the JSON error envelope when the export is refused', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(
        { error: { code: 'forbidden', message: 'you do not have permission to read this entity type' } },
        403,
      ),
    )
    vi.stubGlobal('fetch', fetchMock)
    const client = new ApiClient()

    await expect(client.download('/api/v1/exports', { entity_type: 'account' })).rejects.toThrow(
      'you do not have permission',
    )
  })

  it('parses the filename out of a Content-Disposition header', () => {
    expect(filenameFromDisposition('attachment; filename="a.csv"')).toBe('a.csv')
    expect(filenameFromDisposition('attachment; filename=b.jsonl')).toBe('b.jsonl')
    expect(filenameFromDisposition(null)).toBeUndefined()
    expect(filenameFromDisposition('inline')).toBeUndefined()
  })
})

describe('server-sent event streams', () => {
  /** Builds a client whose fetch returns a streaming response. */
  function clientWithStream(body: string, status = 200) {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(body, { status, headers: { 'Content-Type': 'text/event-stream' } }),
    )
    vi.stubGlobal('fetch', fetchMock)
    return { client: new ApiClient(), fetchMock }
  }

  it('delivers each event as it arrives, in order', async () => {
    const { client } = clientWithStream(
      'event: delta\ndata: {"text":"a"}\n\n' +
        'event: delta\ndata: {"text":"b"}\n\n' +
        'event: done\ndata: {"model":"m"}\n\n',
    )
    const seen: { event: string; data: unknown }[] = []
    await client.stream('/api/v1/ai/stream/chat', { prompt: 'hi' }, (e) => seen.push(e))

    expect(seen.map((e) => e.event)).toEqual(['delta', 'delta', 'done'])
    expect(seen[0].data).toEqual({ text: 'a' })
    expect(seen[2].data).toEqual({ model: 'm' })
  })

  it('sends the CSRF header and the request body', async () => {
    csrfTokenStore.set('token-123')
    const { client, fetchMock } = clientWithStream('event: done\ndata: {}\n\n')
    await client.stream('/api/v1/ai/stream/chat', { prompt: 'hello' }, () => {})

    const init = fetchMock.mock.calls[0][1] as RequestInit
    const headers = init.headers as Record<string, string>
    expect(headers['X-CSRF-Token']).toBe('token-123')
    expect(headers['Accept']).toBe('text/event-stream')
    expect(init.body).toBe(JSON.stringify({ prompt: 'hello' }))
  })

  it('turns a streamed error event into an ApiError', async () => {
    const { client } = clientWithStream(
      'event: error\ndata: {"code":"unavailable","message":"The AI provider could not be reached.","retryable":true}\n\n',
    )
    await expect(client.stream('/api/v1/ai/stream/chat', {}, () => {})).rejects.toMatchObject({
      code: 'unavailable',
      retryable: true,
    })
  })

  it('raises a non-2xx response as an ApiError before reading the stream', async () => {
    const { client } = clientWithStream(
      JSON.stringify({ error: { code: 'rate_limited', message: 'too many requests' } }),
      429,
    )
    await expect(client.stream('/api/v1/ai/stream/chat', {}, () => {})).rejects.toMatchObject({
      code: 'rate_limited',
      status: 429,
    })
  })

  it('handles a frame split across two chunks', async () => {
    // A real stream arrives in arbitrary TCP-sized pieces; a frame that straddles
    // two reads must still be parsed whole.
    const encoder = new TextEncoder()
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(encoder.encode('event: delta\ndata: {"te'))
        controller.enqueue(encoder.encode('xt":"split"}\n\nevent: done\ndata: {}\n\n'))
        controller.close()
      },
    })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(stream, { status: 200 })))
    const client = new ApiClient()
    const seen: { event: string; data: unknown }[] = []
    await client.stream('/api/v1/ai/stream/chat', {}, (e) => seen.push(e))

    expect(seen).toHaveLength(2)
    expect(seen[0]).toEqual({ event: 'delta', data: { text: 'split' } })
  })

  it('ignores keep-alive comment lines', async () => {
    const { client } = clientWithStream(': keep-alive\n\nevent: done\ndata: {"model":"m"}\n\n')
    const seen: string[] = []
    await client.stream('/api/v1/ai/stream/chat', {}, (e) => seen.push(e.event))
    expect(seen).toEqual(['done'])
  })
})
