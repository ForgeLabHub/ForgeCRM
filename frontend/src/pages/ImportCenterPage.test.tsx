/**
 * Tests for the export control of the Import Center.
 *
 * The export route streams a file in the response, but nothing in the interface
 * reached it, and the client method that pointed at it read a JSON body the route
 * never sends. These tests hold the page to the behaviour that matters: the
 * control appears only for a caller who may export, and choosing an entity issues
 * a POST to /exports with the chosen entity and format.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { ImportCenterPage } from './ImportCenterPage'
import { AuthProvider } from '../hooks/useAuth'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function stubFetch(permissions: string[] = ['exports.execute', 'imports.create']) {
  const fetchMock = vi.fn((input: RequestInfo | URL, _init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    if (url.includes('/api/v1/auth/me')) {
      return Promise.resolve(
        json({
          user: { id: 'u1', email: 'a@example.com', display_name: 'A' },
          organization: { id: 'o1', name: 'Org' },
          permissions,
          csrf_token: 't',
        }),
      )
    }
    if (url.includes('/api/v1/exports')) {
      return Promise.resolve(
        new Response('id,name\n1,Acme\n', {
          status: 200,
          headers: {
            'Content-Type': 'text/csv; charset=utf-8',
            'Content-Disposition': 'attachment; filename="account-1.csv"',
          },
        }),
      )
    }
    return Promise.resolve(json({ items: [], total: 0, limit: 25, offset: 0 }))
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
          <ImportCenterPage />
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.unstubAllGlobals()
  // jsdom has no object-URL support; the download path only needs it to resolve.
  vi.stubGlobal('URL', {
    ...URL,
    createObjectURL: vi.fn(() => 'blob:stub'),
    revokeObjectURL: vi.fn(),
  })
})

describe('ImportCenterPage export', () => {
  it('offers an export control to a caller who may export', async () => {
    stubFetch()
    renderPage()
    expect(await screen.findByRole('button', { name: 'Export' })).toBeInTheDocument()
  })

  it('hides the export control without the permission', async () => {
    stubFetch([])
    renderPage()
    // The list has loaded by the time the heading is present.
    expect(await screen.findByText('Import jobs')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Export' })).not.toBeInTheDocument()
  })

  it('posts the chosen entity and format to the export route', async () => {
    const fetchMock = stubFetch()
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Export' }))
    await userEvent.selectOptions(screen.getByLabelText('Entity'), 'deal')
    await userEvent.selectOptions(screen.getByLabelText('Format'), 'jsonl')
    await userEvent.click(screen.getByRole('button', { name: 'Download' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(([input]) =>
        String(input).includes('/api/v1/exports'),
      )
      expect(call).toBeTruthy()
      const [, init] = call!
      expect((init as RequestInit).method).toBe('POST')
      expect(JSON.parse(String((init as RequestInit).body))).toEqual({
        entity_type: 'deal',
        format: 'jsonl',
      })
    })
  })
})
