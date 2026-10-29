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

/** Builds a fetch stub that answers the panel's calls. */
function stubFetch(handlers: {
  status?: unknown
  context?: unknown
  assist?: unknown
  assistStatus?: number
}) {
  const fetchMock = vi.fn((input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString()
    if (url.includes('/api/v1/ai/status')) {
      return Promise.resolve(json(handlers.status ?? { enabled: true }))
    }
    if (url.includes('/context')) {
      return Promise.resolve(json(handlers.context ?? { facts: [], disclosures: [], available: true }))
    }
    if (url.includes('/api/v1/ai/assist/')) {
      if (handlers.assistStatus && handlers.assistStatus >= 400) {
        return Promise.resolve(
          json({ error: { code: 'forbidden', message: 'permission deals.read is required' } }, handlers.assistStatus),
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
})
