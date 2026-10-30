/**
 * Tests for the record collaboration panel.
 *
 * The panel is the only interface to the tags and comments the server already
 * implements, so the assertions are about what it sends and what it refuses to
 * show: reads are narrowed to the bound record, a write carries the record's
 * type and id, and a user without the write permission sees no control that
 * would be refused.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RecordCollabPanel } from './RecordCollabPanel'
import { AuthProvider } from '../hooks/useAuth'

const ENTITY_ID = '11111111-1111-1111-1111-111111111111'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

interface StubOptions {
  permissions?: string[]
  attached?: unknown
  allTags?: unknown
  comments?: unknown
  /** Takes the parsed body of a write so an assertion can inspect it. */
  onWrite?: (url: string, body: unknown) => void
}

function stubFetch(options: StubOptions = {}) {
  const permissions = options.permissions ?? [
    'contacts.read',
    'tags.manage',
    'comments.create',
  ]
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    const method = init?.method ?? 'GET'

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
    if (url.includes('/api/v1/tags/attached')) {
      return Promise.resolve(json({ items: options.attached ?? [] }))
    }
    if (url.includes('/api/v1/tags')) {
      if (method === 'POST') {
        options.onWrite?.(url, JSON.parse(String(init?.body ?? '{}')))
        if (url.includes('/attach')) return Promise.resolve(json({ status: 'tag attached' }))
        return Promise.resolve(json({ id: 'new-tag', name: 'New', color: '#64748b', created_at: 'x' }))
      }
      return Promise.resolve(json({ items: options.allTags ?? [] }))
    }
    if (url.includes('/api/v1/comments')) {
      if (method === 'POST') {
        options.onWrite?.(url, JSON.parse(String(init?.body ?? '{}')))
        return Promise.resolve(json({ id: 'c1' }, 201))
      }
      return Promise.resolve(json({ items: options.comments ?? [] }))
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
        <RecordCollabPanel entityType="deal" entityId={ENTITY_ID} />
      </AuthProvider>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.unstubAllGlobals()
})

describe('RecordCollabPanel', () => {
  it('shows tags already attached to the record', async () => {
    stubFetch({
      attached: [{ id: 't1', name: 'Hot', color: '#f00', created_at: 'x' }],
    })
    renderPanel()

    expect(await screen.findByText('Hot')).toBeInTheDocument()
  })

  it('attaches a tag with the bound record type and id', async () => {
    const writes: { url: string; body: unknown }[] = []
    stubFetch({
      allTags: [{ id: 't9', name: 'Enterprise', color: '#0f0', created_at: 'x' }],
      onWrite: (url, body) => writes.push({ url, body }),
    })
    renderPanel()

    await screen.findByRole('option', { name: 'Enterprise' })
    await userEvent.selectOptions(screen.getByLabelText('Attach an existing tag'), 't9')
    await userEvent.click(screen.getByRole('button', { name: 'Attach' }))

    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].url).toContain('/api/v1/tags/t9/attach')
    expect(writes[0].body).toEqual({ entity_type: 'deal', entity_id: ENTITY_ID })
  })

  it('posts a comment against the bound record', async () => {
    const writes: { url: string; body: unknown }[] = []
    stubFetch({ onWrite: (url, body) => writes.push({ url, body }) })
    renderPanel()

    await userEvent.type(await screen.findByLabelText('Add a comment'), 'Called the buyer')
    await userEvent.click(screen.getByRole('button', { name: 'Comment' }))

    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].body).toEqual({
      entity_type: 'deal',
      entity_id: ENTITY_ID,
      body: 'Called the buyer',
    })
  })

  it('renders existing comments with their author', async () => {
    stubFetch({
      comments: [
        {
          id: 'c1',
          entity_type: 'deal',
          entity_id: ENTITY_ID,
          body: 'Left a voicemail',
          author_name: 'Dana',
          created_at: '2026-01-01T00:00:00Z',
        },
      ],
    })
    renderPanel()

    expect(await screen.findByText('Left a voicemail')).toBeInTheDocument()
    expect(screen.getByText('Dana')).toBeInTheDocument()
  })

  it('offers no write controls to a user without the write permissions', async () => {
    stubFetch({ permissions: ['contacts.read'] })
    renderPanel()

    // The panel still reads, but neither form appears.
    expect(await screen.findByText('No tags on this record.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Attach' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Add a comment')).not.toBeInTheDocument()
  })

  it('shows nothing at all when the user cannot read the record', async () => {
    stubFetch({ permissions: [] })
    const { container } = renderPanel()

    await waitFor(() => expect(container).toBeEmptyDOMElement())
  })
})
