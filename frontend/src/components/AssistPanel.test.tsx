/**
 * Tests for the contextual AI panel.
 *
 * The panel is where the separation between CRM facts and the model's output is
 * either kept or lost, so the assertions here are about that boundary: the facts
 * are labelled as coming from the CRM, the analysis and recommendations are
 * labelled as the model's, and a draft is never sent by the panel itself.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AssistPanel } from './AssistPanel'
import { AuthProvider } from '../hooks/useAuth'

/**
 * Builds a fetch stub that answers the panel's calls.
 *
 * The streaming route answers with a real server-sent-event body derived from the
 * same insight the buffered route would return, so the tests drive the actual
 * streaming path rather than a stub of it. `streamMode` lets a test force the
 * fallback: `empty` sends no events, which is what a proxy that swallows the body
 * produces, and the panel must then fall back to the buffered request.
 */
function stubFetch(handlers: {
  status?: unknown
  context?: unknown
  assist?: unknown
  assistStatus?: number
  /** When set, the error envelope carries this retryable flag. */
  assistRetryable?: boolean
  /** 'events' (default) streams the insight; 'empty' streams nothing. */
  streamMode?: 'events' | 'empty'
}) {
  const fetchMock = vi.fn((input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString()
    if (url.includes('/api/v1/ai/status')) {
      return Promise.resolve(json(handlers.status ?? { enabled: true }))
    }
    if (url.includes('/context')) {
      return Promise.resolve(json(handlers.context ?? { facts: [], disclosures: [], available: true }))
    }
    if (url.includes('/api/v1/ai/stream/assist/')) {
      if (handlers.assistStatus && handlers.assistStatus >= 400) {
        return Promise.resolve(
          json(
            {
              retryable: handlers.assistRetryable ?? false,
              error: { code: 'forbidden', message: 'permission deals.read is required' },
            },
            handlers.assistStatus,
          ),
        )
      }
      if (handlers.streamMode === 'empty') {
        return Promise.resolve(sse(''))
      }
      const body = handlers.assist ?? {}
      const analysis = typeof body === 'object' && body !== null ? (body as { analysis?: string }).analysis : ''
      let out = ''
      if (analysis) out += sseFrame('delta', { text: analysis })
      out += sseFrame('done', body)
      return Promise.resolve(sse(out))
    }
    if (url.includes('/api/v1/ai/assist/')) {
      if (handlers.assistStatus && handlers.assistStatus >= 400) {
        return Promise.resolve(
          json(
            {
              retryable: handlers.assistRetryable ?? false,
              error: { code: 'forbidden', message: 'permission deals.read is required' },
            },
            handlers.assistStatus,
          ),
        )
      }
      return Promise.resolve(json(handlers.assist))
    }
    if (url.includes('/api/v1/auth/me')) {
      return Promise.resolve(
        json({
          user: { id: 'u1', email: 'a@example.com', display_name: 'A' },
          organization: { id: 'o1', name: 'Org' },
          permissions: ['ai.read', 'ai.tools.execute', 'deals.read'],
          csrf_token: 't',
        }),
      )
    }
    return Promise.resolve(json({}))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

/** One server-sent-event frame, in the exact shape the server writes. */
function sseFrame(event: string, data: unknown): string {
  return `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`
}

/** A streaming Response whose body is the given event frames. */
function sse(body: string): Response {
  return new Response(body, {
    status: 200,
    headers: { 'Content-Type': 'text/event-stream' },
  })
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function renderPanel(entityType: 'deal' | 'contact' = 'deal') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <AssistPanel entityType={entityType} entityId="11111111-1111-1111-1111-111111111111" />
      </AuthProvider>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.unstubAllGlobals()
})

const insight = {
  kind: 'summary',
  entity_type: 'deal',
  entity_id: '11111111-1111-1111-1111-111111111111',
  cached: false,
  provider: 'openai_compatible',
  model: 'test-model',
  facts: [{ label: 'Amount', value: '1000.00 USD' }],
  analysis: 'The deal has stalled for two weeks.',
  recommendations: ['Call the buyer', 'Send a revised quote'],
  disclosures: [],
  confidence: {
    level: 'high',
    score: 0.9,
    basis: ['every figure in the answer (1) appears in the record'],
  },
  generated_at: '2026-01-01T00:00:00Z',
}

describe('AssistPanel', () => {
  it('reports when no provider is configured instead of failing', async () => {
    stubFetch({ status: { enabled: false } })
    renderPanel()

    expect(await screen.findByText(/No AI provider is configured/i)).toBeInTheDocument()
  })

  it('runs an assistance request and separates facts from model output', async () => {
    stubFetch({ assist: insight })
    renderPanel()

    await userEvent.click(await screen.findByRole('button', { name: 'Run' }))

    // The CRM fact is shown, and it is labelled as coming from the CRM.
    expect(await screen.findByText(/What the assistant was told/i)).toBeInTheDocument()
    expect(screen.getByText(/1000.00 USD/)).toBeInTheDocument()

    // The model's analysis and recommendations are labelled as its own output.
    expect(screen.getByText(/The deal has stalled for two weeks/)).toBeInTheDocument()
    expect(screen.getAllByText('model output').length).toBeGreaterThan(0)
    expect(screen.getByText('Call the buyer')).toBeInTheDocument()

    // And it must state that nothing was changed.
    expect(screen.getByText(/Nothing has been changed/i)).toBeInTheDocument()
  })

  it('requires a question before asking', async () => {
    stubFetch({ assist: insight })
    renderPanel()

    await userEvent.click(await screen.findByRole('button', { name: 'Ask a question' }))
    const run = screen.getByRole('button', { name: 'Run' })
    expect(run).toBeDisabled()

    await userEvent.type(screen.getByLabelText(/Your question/i), 'What is blocking this?')
    await waitFor(() => expect(run).toBeEnabled())
  })

  it('shows a draft read-only, with no send control', async () => {
    stubFetch({
      assist: {
        ...insight,
        kind: 'draft',
        draft: { subject: 'Following up', body: 'Hello, just checking in.' },
      },
    })
    renderPanel()

    await userEvent.click(await screen.findByRole('button', { name: 'Draft a message' }))
    await userEvent.click(screen.getByRole('button', { name: 'Run' }))

    const area = await screen.findByDisplayValue(/just checking in/)
    expect(area).toHaveAttribute('readonly')
    expect(screen.getByText(/Nothing is sent automatically/i)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /send/i })).toBeNull()
  })

  it('surfaces a permission refusal from the server', async () => {
    stubFetch({ assist: insight, assistStatus: 403 })
    renderPanel()

    await userEvent.click(await screen.findByRole('button', { name: 'Run' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/permission deals.read is required/i)
  })

  it('warns when a provider is configured but not reachable, before a request is spent', async () => {
    stubFetch({ status: { enabled: true, healthy: false }, assist: insight })
    renderPanel()

    expect(await screen.findByText(/configured but not reachable/i)).toBeInTheDocument()
    // The controls stay available: the probe can be stale, so the user is warned,
    // not blocked.
    expect(screen.getByRole('button', { name: 'Run' })).toBeEnabled()
  })

  it('shows no reachability warning when the provider is healthy', async () => {
    stubFetch({ status: { enabled: true, healthy: true }, assist: insight })
    renderPanel()

    await screen.findByRole('button', { name: 'Run' })
    expect(screen.queryByText(/configured but not reachable/i)).not.toBeInTheDocument()
  })

  it('offers a retry only when the server says the failure is retryable', async () => {
    // A provider outage is retryable: the server marks it so, and the panel gives
    // the user the one action that could help.
    stubFetch({ assist: insight, assistStatus: 503, assistRetryable: true })
    renderPanel()

    await userEvent.click(await screen.findByRole('button', { name: 'Run' }))

    expect(await screen.findByRole('button', { name: 'Try again' })).toBeInTheDocument()
  })

  it('does not offer a retry for a deterministic failure', async () => {
    // A permission refusal cannot succeed by resending it, so a retry button
    // would be a control that never works.
    stubFetch({ assist: insight, assistStatus: 403, assistRetryable: false })
    renderPanel()

    await userEvent.click(await screen.findByRole('button', { name: 'Run' }))

    await screen.findByRole('alert')
    expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument()
  })

  it('shows the disclosures that qualify an answer', async () => {
    stubFetch({
      assist: {
        ...insight,
        disclosures: [{ kind: 'truncated', detail: 'Only the most recent 20 activities were included.' }],
      },
    })
    renderPanel()

    await userEvent.click(await screen.findByRole('button', { name: 'Run' }))

    expect(await screen.findByText(/Only the most recent 20 activities/i)).toBeInTheDocument()
  })

  it('shows the grounding level and its reason, not a bare number', async () => {
    stubFetch({
      assist: {
        ...insight,
        confidence: {
          level: 'low',
          score: 0.4,
          basis: ['2 of 3 figures in the answer do not appear in the record (e.g. 48,000)'],
        },
      },
    })
    renderPanel()

    await userEvent.click(await screen.findByRole('button', { name: 'Run' }))

    expect(await screen.findByText(/Weakly grounded/i)).toBeInTheDocument()
    expect(screen.getByText(/do not appear in the record/i)).toBeInTheDocument()
  })

  it('streams the reply and replaces the raw text with the structured insight', async () => {
    stubFetch({ assist: insight })
    renderPanel()

    await userEvent.click(await screen.findByRole('button', { name: 'Run' }))

    // The final structured view is what the user is left with: the facts block,
    // the analysis and the recommendations, each labelled. The transient raw-text
    // section must be gone once the stream closes.
    expect(await screen.findByText(/What the assistant was told/i)).toBeInTheDocument()
    expect(screen.getByText(/1000.00 USD/)).toBeInTheDocument()
    expect(screen.queryByText(/model output, in progress/i)).not.toBeInTheDocument()
  })

  it('falls back to the buffered request when the stream delivers nothing', async () => {
    // A proxy that swallows the stream body ends the stream with no `done` event.
    // The panel must not treat that as success with an empty answer; it falls back
    // to the buffered route, which is served by the same service.
    const fetchMock = stubFetch({ assist: insight, streamMode: 'empty' })
    renderPanel()

    await userEvent.click(await screen.findByRole('button', { name: 'Run' }))

    expect(await screen.findByText(/The deal has stalled for two weeks/)).toBeInTheDocument()
    const urls = fetchMock.mock.calls.map((c) => String(c[0]))
    expect(urls.some((u) => u.includes('/api/v1/ai/stream/assist/'))).toBe(true)
    expect(urls.some((u) => /\/api\/v1\/ai\/assist\/[^/]*\?/.test(u) || u.includes('/api/v1/ai/assist/'))).toBe(
      true,
    )
  })

  it('surfaces a streamed error as a retryable failure with a retry button', async () => {
    stubFetch({ assist: insight, assistStatus: 429, assistRetryable: true })
    renderPanel()

    await userEvent.click(await screen.findByRole('button', { name: 'Run' }))

    // The stream route answered 429 before opening; the panel shows the message and,
    // because the server marked it retryable, a retry button.
    expect(await screen.findByText(/Try again/i)).toBeInTheDocument()
  })
})
