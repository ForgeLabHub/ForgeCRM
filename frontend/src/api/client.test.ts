/**
 * Tests for the HTTP client.
 *
 * These exercise the real client against a stubbed `fetch`, because the
 * behaviour that matters here — CSRF header handling, error-envelope parsing
 * and the session-expiry callback — lives in the client itself, not in any
 * component, and a mock of the client would test nothing.
 */
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiClient, ApiError, NetworkError, csrfTokenStore, errorMessage, fieldErrors } from './client'

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
