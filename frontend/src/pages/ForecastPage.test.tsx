/**
 * Tests for the Forecasting page.
 *
 * The page's job is to present a projection as an estimate: the projected points
 * are shown with the observed series they came from, and the measured accuracy is
 * shown next to the naive baseline it must beat. The assertions are about that
 * separation and about the honest "did not beat the baseline" state, not about
 * decoration.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ForecastPage } from './ForecastPage'
import { AuthProvider } from '../hooks/useAuth'
import { formatMinorUnits, formatBasisPoints, type ForecastRun } from '../api/intelligence'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const run: ForecastRun = {
  id: 'f1',
  method: 'mean',
  algorithm_version: 'forecast-v1',
  horizon: 2,
  history: [],
  created_at: '2026-02-01T00:00:00Z',
  result: {
    method: 'mean',
    algorithm_version: 'forecast-v1',
    currency: 'USD',
    history: [
      { period: '2026-01', value: { amount_minor: 100_000, currency: 'USD' } },
      { period: '2026-02', value: { amount_minor: 120_000, currency: 'USD' } },
    ],
    points: [
      {
        period: '2026-03',
        value: { amount_minor: 110_000, currency: 'USD' },
        lower: { amount_minor: 100_000, currency: 'USD' },
        upper: { amount_minor: 120_000, currency: 'USD' },
      },
      {
        period: '2026-04',
        value: { amount_minor: 110_000, currency: 'USD' },
        lower: { amount_minor: 100_000, currency: 'USD' },
        upper: { amount_minor: 120_000, currency: 'USD' },
      },
    ],
    quality: {
      method: 'mean',
      folds: 3,
      mae: { amount_minor: 5_000, currency: 'USD' },
      rmse: { amount_minor: 6_000, currency: 'USD' },
      mape: { basis_points: 500 },
      folds_with_zero_actual: 0,
      naive_mae: { amount_minor: 9_000, currency: 'USD' },
      naive_mape: { basis_points: 900 },
      beats_naive: true,
    },
    assumptions: ['History is the last twelve months of won-deal revenue.'],
    warnings: [],
  },
}

function stubFetch(handlers: {
  catalog?: unknown
  runs?: unknown
  run?: unknown
  runStatus?: number
  permissions?: string[]
}) {
  // The list endpoint reflects what the store holds, so a created run appears on
  // the refetch the page triggers after the POST, exactly as it would live.
  const stored: unknown[] = [...((handlers.runs as unknown[]) ?? [])]
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    if (url.includes('/api/v1/auth/me')) {
      return Promise.resolve(
        json({
          user: { id: 'u1', email: 'a@example.com', display_name: 'A' },
          organization: { id: 'o1', name: 'Org' },
          permissions: handlers.permissions ?? ['forecast.read', 'forecast.run'],
          csrf_token: 't',
        }),
      )
    }
    if (url.includes('/api/v1/forecast/catalog')) {
      return Promise.resolve(
        json({
          methods: handlers.catalog ?? [
            { method: 'mean', name: 'Historical average', description: 'Average of the observed periods.', windowed: false },
            { method: 'moving_average', name: 'Moving average', description: 'Average of the last few periods.', windowed: true, default_window: 3 },
          ],
        }),
      )
    }
    if (url.includes('/api/v1/forecast/runs') && (!init || init.method !== 'POST')) {
      return Promise.resolve(json({ runs: stored }))
    }
    if (url.includes('/api/v1/forecast/runs') && init?.method === 'POST') {
      if (handlers.runStatus && handlers.runStatus >= 400) {
        return Promise.resolve(
          json({ error: { code: 'validation_failed', message: 'insufficient history to forecast' } }, handlers.runStatus),
        )
      }
      const created = handlers.run ?? run
      stored.unshift(created)
      return Promise.resolve(json(created))
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
        <ForecastPage />
      </AuthProvider>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.unstubAllGlobals()
})

describe('ForecastPage', () => {
  it('shows the catalog and an empty history when nothing is stored', async () => {
    stubFetch({})
    renderPage()

    expect(await screen.findByText('No forecasts yet')).toBeInTheDocument()
    expect(await screen.findByRole('option', { name: 'Historical average' })).toBeInTheDocument()
  })

  it('computes a forecast and shows the projection with its observed series', async () => {
    stubFetch({})
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Compute forecast' }))

    // The projection and the observed series are both shown.
    await waitFor(() => expect(screen.getByText('2026-03')).toBeInTheDocument())
    expect(screen.getByText('Observed series')).toBeInTheDocument()
    expect(screen.getByText('Measured accuracy')).toBeInTheDocument()
    expect(screen.getAllByText('$1,100.00').length).toBeGreaterThan(0)
    // The naive baseline is shown so the reader can judge the method.
    expect(screen.getByText('$90.00')).toBeInTheDocument()
  })

  it('reports when a method did not beat the naive baseline', async () => {
    stubFetch({
      run: { ...run, result: { ...run.result, quality: { ...run.result.quality, beats_naive: false } } },
    })
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Compute forecast' }))

    await waitFor(() => expect(screen.getByText('No')).toBeInTheDocument())
    expect(screen.getByText(/did not beat the naive baseline/i)).toBeInTheDocument()
  })

  it('shows the server reason when there is too little history', async () => {
    stubFetch({ runStatus: 422 })
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Compute forecast' }))

    expect(await screen.findByText(/insufficient history to forecast/i)).toBeInTheDocument()
  })

  it('hides the compute control when the user may not run a forecast', async () => {
    stubFetch({ permissions: ['forecast.read'] })
    renderPage()

    expect(await screen.findByText(/do not have permission to compute a forecast/i)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Compute forecast' })).not.toBeInTheDocument()
  })
})

describe('forecast formatting', () => {
  it('formats integer minor units without a float round-trip', () => {
    expect(formatMinorUnits(110_000)).toBe('$1,100.00')
    expect(formatMinorUnits(-5_050)).toBe('-$50.50')
  })

  it('formats basis points as a percentage', () => {
    expect(formatBasisPoints(500)).toBe('5.00%')
    expect(formatBasisPoints(10_000)).toBe('100.00%')
  })
})
