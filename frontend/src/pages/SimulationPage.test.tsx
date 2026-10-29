/**
 * Tests for the Simulation Lab.
 *
 * The page's whole job is to keep observed CRM figures and modelled figures apart
 * and to show the assumptions behind a result, so the assertions are about that
 * separation: the baseline column is labelled as the CRM's, the variant as the
 * model's, assumptions are rendered, and a scenario that cannot be computed shows
 * the server's reason rather than a number.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { SimulationPage } from './SimulationPage'
import { AuthProvider } from '../hooks/useAuth'
import { formatMinorUnits, formatMetricValue, type Metric } from '../api/simulation'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const comparison = {
  kind: 'funnel',
  algorithm_version: 'simulate-v1',
  currency: 'USD',
  metrics: [
    {
      key: 'expected_won_value',
      label: 'Expected won value',
      unit: 'money',
      currency: 'USD',
      baseline: 140_000,
      variant: 190_000,
      delta: 50_000,
      relative_basis_points: 3571,
      relative_defined: true,
    },
    {
      key: 'expected_won_count',
      label: 'Expected deals won',
      unit: 'count',
      baseline: 3,
      variant: 4,
      delta: 1,
      relative_basis_points: 3333,
      relative_defined: true,
    },
  ],
  assumptions: ['Expected values are a deterministic walk, not a statistical forecast.'],
  warnings: [],
}

function stubFetch(handlers: {
  catalog?: unknown
  scenarios?: unknown
  pipelines?: unknown
  runs?: unknown
  preview?: unknown
  previewStatus?: number
  run?: unknown
  permissions?: string[]
}) {
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    if (url.includes('/api/v1/auth/me')) {
      return Promise.resolve(
        json({
          user: { id: 'u1', email: 'a@example.com', display_name: 'A' },
          organization: { id: 'o1', name: 'Org' },
          permissions: handlers.permissions ?? [
            'simulation.read',
            'simulation.manage',
            'simulation.run',
          ],
          csrf_token: 't',
        }),
      )
    }
    if (url.includes('/api/v1/simulation/catalog')) {
      return Promise.resolve(
        json({
          scenarios: handlers.catalog ?? [
            {
              kind: 'funnel',
              name: 'Sales funnel',
              description: 'Change stage conversions or deal inflow.',
              parameters: [
                { key: 'lead_volume', label: 'Deal inflow', description: 'Change inflow.', default: '0', kind: 'percent', stage_scoped: false },
              ],
            },
          ],
        }),
      )
    }
    if (url.includes('/api/v1/simulation/scenarios') && (!init || init.method !== 'POST')) {
      if (url.includes('/runs')) {
        return Promise.resolve(json({ runs: handlers.runs ?? [] }))
      }
      return Promise.resolve(json({ scenarios: handlers.scenarios ?? [] }))
    }
    if (url.includes('/api/v1/simulation/preview')) {
      if (handlers.previewStatus && handlers.previewStatus >= 400) {
        return Promise.resolve(
          json({ error: { code: 'validation_failed', message: 'insufficient data for this scenario: no active customers in the period' } }, handlers.previewStatus),
        )
      }
      return Promise.resolve(json(handlers.preview ?? { comparison, baseline: {}, warnings: [] }))
    }
    if (url.includes('/api/v1/simulation/scenarios') && init?.method === 'POST') {
      return Promise.resolve(json(handlers.run ?? { id: 'r1', kind: 'funnel', result: comparison, created_at: '2026-01-01T00:00:00Z', algorithm_version: 'simulate-v1', params: {}, baseline: {} }))
    }
    if (url.includes('/api/v1/pipelines')) {
      return Promise.resolve(
        json(
          handlers.pipelines ?? {
            items: [{ id: 'p1', name: 'Sales', is_default: true, currency: 'USD', stages: [] }],
          },
        ),
      )
    }
    return Promise.resolve(json({}))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <SimulationPage />
      </AuthProvider>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.unstubAllGlobals()
})

describe('SimulationPage', () => {
  it('shows an empty state and the scenario catalog when nothing is saved', async () => {
    stubFetch({})
    renderPage()

    expect(await screen.findByText('No scenarios yet')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Sales funnel' })).toBeInTheDocument()
  })

  it('lists saved scenarios and opens one', async () => {
    stubFetch({
      scenarios: [
        {
          id: 's1',
          name: 'Q3 best case',
          kind: 'revenue',
          params: {},
          algorithm_version: 'simulate-v1',
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
      ],
    })
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: /Q3 best case/ }))

    expect(await screen.findByRole('button', { name: 'Run scenario' })).toBeInTheDocument()
  })

  it('previews a scenario and separates the CRM baseline from the model output', async () => {
    stubFetch({})
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Sales funnel' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Preview' }))

    // The comparison table labels the observed column as the CRM's and the modelled
    // column as the model's.
    expect(await screen.findByText('Observed (CRM)')).toBeInTheDocument()
    expect(screen.getByText('Modelled')).toBeInTheDocument()

    // The baseline and variant money figures are both present.
    expect(screen.getAllByText(/\$1,400\.00/).length).toBeGreaterThan(0)
    expect(screen.getAllByText(/\$1,900\.00/).length).toBeGreaterThan(0)

    // The assumptions are stated, not hidden.
    expect(screen.getByText(/deterministic walk/)).toBeInTheDocument()
  })

  it('shows the server reason when a scenario has too little data', async () => {
    stubFetch({ previewStatus: 422 })
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Sales funnel' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Preview' }))

    expect(await screen.findByText(/insufficient data for this scenario/i)).toBeInTheDocument()
  })

  it('hides the run control when the user may not compute', async () => {
    stubFetch({ permissions: ['simulation.read'] })
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Sales funnel' }))

    // The save control is present in the builder only when the user may manage; the
    // preview control is disabled without simulation.run.
    const preview = await screen.findByRole('button', { name: 'Preview' })
    expect(preview).toBeDisabled()
  })

  it('runs a saved scenario and shows the result', async () => {
    stubFetch({
      scenarios: [
        {
          id: 's1',
          name: 'Q3 best case',
          kind: 'funnel',
          params: {},
          algorithm_version: 'simulate-v1',
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
      ],
    })
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: /Q3 best case/ }))
    await userEvent.click(await screen.findByRole('button', { name: 'Run scenario' }))

    await waitFor(() => expect(screen.getByText('Observed (CRM)')).toBeInTheDocument())
  })
})

describe('money formatting', () => {
  it('formats integer minor units without a float round-trip', () => {
    expect(formatMinorUnits(140_000)).toBe('$1,400.00')
    expect(formatMinorUnits(-5_050)).toBe('-$50.50')
    expect(formatMinorUnits(0)).toBe('$0.00')
  })

  it('formats each metric in its own unit', () => {
    const money: Metric = { key: 'm', label: 'M', unit: 'money', currency: 'USD', baseline: 0, variant: 0, delta: 0, relative_defined: false }
    const count: Metric = { ...money, unit: 'count' }
    const percent: Metric = { ...money, unit: 'percent' }

    expect(formatMetricValue(money, 140_000)).toBe('$1,400.00')
    expect(formatMetricValue(count, 1_234)).toBe('1,234')
    expect(formatMetricValue(percent, 2_500)).toBe('25.00%')
  })
})
