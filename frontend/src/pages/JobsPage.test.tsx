/**
 * Tests for the Background jobs page.
 *
 * The page reads two shapes from the API: a paginated list of jobs and a
 * per-status summary. The summary arrives wrapped — `{by_status: {...}}` — and
 * reading it from the top level would put the wrapper object where a number is
 * expected, which React refuses ("Objects are not valid as a React child") and
 * which blanks the page. These tests pin the unwrapping so the page cannot
 * regress to that state.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { JobsPage } from './JobsPage'
import { AuthProvider } from '../hooks/useAuth'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function stubFetch(statsBody: unknown, jobs: unknown[] = []) {
  const fetchMock = vi.fn((input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString()
    if (url.includes('/api/v1/auth/me')) {
      return Promise.resolve(
        json({
          user: { id: 'u1', email: 'a@example.com', display_name: 'A' },
          organization: { id: 'o1', name: 'Org' },
          permissions: ['jobs.read', 'jobs.manage'],
          csrf_token: 't',
        }),
      )
    }
    if (url.includes('/api/v1/jobs/stats')) return Promise.resolve(json(statsBody))
    if (url.includes('/api/v1/jobs')) {
      return Promise.resolve(json({ items: jobs, total: jobs.length, limit: 25, offset: 0 }))
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
      <MemoryRouter>
        <AuthProvider>
          <JobsPage />
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.unstubAllGlobals()
})

describe('JobsPage', () => {
  it('renders one tile per status from the wrapped summary', async () => {
    stubFetch({ by_status: { queued: 3, failed: 1 } })
    renderPage()

    const queued = await screen.findByText('queued', { selector: '.stat__label' })
    expect(queued.closest('.stat')?.textContent).toContain('3')
    const failed = await screen.findByText('failed', { selector: '.stat__label' })
    expect(failed.closest('.stat')?.textContent).toContain('1')
  })

  it('mounts with an empty summary instead of failing on an object value', async () => {
    stubFetch({ by_status: {} })
    renderPage()

    // The list's own empty state is what proves the page mounted; a render error
    // from an object value would remove it entirely.
    expect(await screen.findByText('No jobs')).toBeInTheDocument()
  })
})
