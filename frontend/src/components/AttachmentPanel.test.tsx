/**
 * Tests for the attachment panel.
 *
 * The panel is the interface's only path to a file upload, so the assertions here
 * are about the conditions under which it appears and what it refuses. It must not
 * render at all when the server reports storage off — an upload button on a
 * deployment with no object store is a button that answers 503 — and it must refuse
 * an oversized file before spending the upload.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AttachmentPanel } from './AttachmentPanel'
import { AuthProvider } from '../hooks/useAuth'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function stubFetch(handlers: { storage?: boolean; maxBytes?: number; documents?: unknown[] }) {
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    if (url.includes('/api/v1/system/capabilities')) {
      return Promise.resolve(
        json({
          storage: handlers.storage ?? true,
          max_upload_bytes: handlers.maxBytes ?? 1024,
        }),
      )
    }
    if (url.includes('/api/v1/documents') && init?.method === 'POST') {
      return Promise.resolve(
        json(
          {
            id: 'd2',
            filename: 'new.txt',
            content_type: 'text/plain',
            size_bytes: 3,
            download_url: '/api/v1/documents/d2/download',
            created_at: '2026-01-01T00:00:00Z',
            deal_id: '11111111-1111-1111-1111-111111111111',
          },
          201,
        ),
      )
    }
    if (url.includes('/api/v1/documents')) {
      return Promise.resolve(json({ items: handlers.documents ?? [] }))
    }
    if (url.includes('/api/v1/auth/me')) {
      return Promise.resolve(
        json({
          user: { id: 'u1', email: 'a@example.com', display_name: 'A' },
          organization: { id: 'o1', name: 'Org' },
          permissions: ['documents.read', 'documents.create', 'documents.archive'],
          csrf_token: 't',
        }),
      )
    }
    return Promise.resolve(json({}))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderPanel() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <AttachmentPanel entityType="deal" entityId="11111111-1111-1111-1111-111111111111" />
      </AuthProvider>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.unstubAllGlobals()
})

describe('AttachmentPanel', () => {
  it('renders nothing when the server reports storage off', async () => {
    stubFetch({ storage: false })
    const { container } = renderPanel()
    await waitFor(() => expect(container.querySelector('[data-testid]')).toBeNull())
    expect(screen.queryByText(/Attachments/)).toBeNull()
  })

  it('shows the attachments bound to this record and not another record', async () => {
    stubFetch({
      documents: [
        {
          id: 'd1',
          filename: 'mine.pdf',
          size_bytes: 10,
          download_url: '/api/v1/documents/d1/download',
          created_at: '2026-01-01T00:00:00Z',
          deal_id: '11111111-1111-1111-1111-111111111111',
        },
        {
          id: 'd9',
          filename: 'other.pdf',
          size_bytes: 10,
          download_url: '/api/v1/documents/d9/download',
          created_at: '2026-01-01T00:00:00Z',
          deal_id: '22222222-2222-2222-2222-222222222222',
        },
      ],
    })
    renderPanel()
    expect(await screen.findByText('mine.pdf')).toBeInTheDocument()
    expect(screen.queryByText('other.pdf')).toBeNull()
  })

  it('refuses a file larger than the server limit before uploading', async () => {
    stubFetch({ maxBytes: 4 })
    renderPanel()
    const input = await screen.findByLabelText('Attach a file')
    const big = new File(['way too many bytes'], 'big.txt', { type: 'text/plain' })
    await userEvent.upload(input, big)
    expect(await screen.findByText(/the limit is/)).toBeInTheDocument()
  })

  it('uploads a file within the limit', async () => {
    const fetchMock = stubFetch({ maxBytes: 1024 })
    renderPanel()
    const input = await screen.findByLabelText('Attach a file')
    const small = new File(['abc'], 'small.txt', { type: 'text/plain' })
    await userEvent.upload(input, small)
    await waitFor(() => {
      const posted = fetchMock.mock.calls.some(
        ([, init]) => (init as RequestInit | undefined)?.method === 'POST',
      )
      expect(posted).toBe(true)
    })
  })
})
