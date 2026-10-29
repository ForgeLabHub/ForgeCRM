/**
 * Tests for the Risk page.
 *
 * The page's job is to make every signal explainable and to keep the operator's
 * decisions visible. The assertions are about the reason and evidence being shown,
 * the status controls being present only for a live signal, the scan summary, and
 * the thresholds form being read-only without risk.manage.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RiskPage } from './RiskPage'
import { AuthProvider } from '../hooks/useAuth'
import type { RiskSignal } from '../api/intelligence'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const openSignal: RiskSignal = {
  id: 's1',
  detected_at: '2026-02-01T00:00:00Z',
  type: 'stalled_deal',
  severity: 'high',
  rule: 'stalled_deal',
  entity_type: 'deal',
  entity_id: 'd1',
  entity_label: 'Acme renewal',
  rationale: 'No activity for 42 days on an open deal.',
  evidence: [
    { key: 'days_since_activity', label: 'Days since activity', value: '42' },
    { key: 'amount', label: 'Amount', value: '$5,000.00' },
  ],
  status: 'open',
  resolved_by_scan: false,
}

function stubFetch(handlers: {
  signals?: RiskSignal[]
  scan?: unknown
  thresholds?: unknown
  status?: RiskSignal
  permissions?: string[]
}) {
  // The list reflects the store, so a status change is visible on the refetch the
  // page triggers after the mutation, exactly as it would live.
  const store: RiskSignal[] = [...(handlers.signals ?? [openSignal])]
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    if (url.includes('/api/v1/auth/me')) {
      return Promise.resolve(
        json({
          user: { id: 'u1', email: 'a@example.com', display_name: 'A' },
          organization: { id: 'o1', name: 'Org' },
          permissions: handlers.permissions ?? ['risk.read', 'risk.manage'],
          csrf_token: 't',
        }),
      )
    }
    if (url.includes('/api/v1/risk/scan')) {
      return Promise.resolve(
        json(handlers.scan ?? { detected: 1, opened: 1, updated: 0, resolved: 0, signals: store }),
      )
    }
    if (url.includes('/api/v1/risk/signals/') && url.includes('/status')) {
      const updated: RiskSignal = handlers.status ?? { ...openSignal, status: 'acknowledged' }
      for (let i = 0; i < store.length; i += 1) {
        if (store[i].id === updated.id) store[i] = updated
      }
      return Promise.resolve(json(updated))
    }
    if (url.includes('/api/v1/risk/signals')) {
      return Promise.resolve(json({ signals: store }))
    }
    if (url.includes('/api/v1/risk/thresholds') && init?.method === 'PUT') {
      return Promise.resolve(json(handlers.thresholds ?? defaultThresholds))
    }
    if (url.includes('/api/v1/risk/thresholds')) {
      return Promise.resolve(json(handlers.thresholds ?? defaultThresholds))
    }
    return Promise.resolve(json({}))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

const defaultThresholds = {
  stalled_deal_days: 30,
  overdue_task_days: 7,
  account_no_contact_days: 60,
  amount_spike_ratio: 30000,
  conversion_drop_ratio: 5000,
  stage_concentration_ratio: 7000,
}

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <RiskPage />
      </AuthProvider>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.unstubAllGlobals()
})

describe('RiskPage', () => {
  it('shows a signal with its reason and evidence', async () => {
    stubFetch({})
    renderPage()

    expect(await screen.findByText('No activity for 42 days on an open deal.')).toBeInTheDocument()
    // The evidence behind the decision is shown, not just the verdict.
    expect(screen.getByText('Days since activity')).toBeInTheDocument()
    expect(screen.getByText('42')).toBeInTheDocument()
    expect(screen.getByText('Stalled deal')).toBeInTheDocument()
  })

  it('runs a scan and reports what changed', async () => {
    stubFetch({})
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Scan now' }))

    expect(await screen.findByText(/Scan detected 1 condition/i)).toBeInTheDocument()
    expect(screen.getByText(/1 opened, 0 updated, 0 closed/i)).toBeInTheDocument()
  })

  it('moves a signal to acknowledged', async () => {
    stubFetch({})
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Acknowledge' }))

    await waitFor(() =>
      expect(screen.queryByRole('button', { name: 'Acknowledge' })).not.toBeInTheDocument(),
    )
  })

  it('shows no status controls for a resolved signal', async () => {
    stubFetch({
      signals: [{ ...openSignal, status: 'resolved', resolved_by_scan: true }],
    })
    renderPage()

    expect(await screen.findByText(/Closed automatically: the condition no longer holds/i)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Resolve' })).not.toBeInTheDocument()
  })

  it('renders thresholds read-only without risk.manage', async () => {
    stubFetch({ permissions: ['risk.read'] })
    renderPage()

    expect(await screen.findByText(/do not have permission to change thresholds/i)).toBeInTheDocument()
    expect(screen.getByLabelText('Stalled deal (days)')).toBeDisabled()
  })

  it('saves edited thresholds', async () => {
    stubFetch({})
    renderPage()

    const input = await screen.findByLabelText('Stalled deal (days)')
    await userEvent.clear(input)
    await userEvent.type(input, '45')
    await userEvent.click(screen.getByRole('button', { name: 'Save thresholds' }))

    expect(await screen.findByText('Saved')).toBeInTheDocument()
  })
})
