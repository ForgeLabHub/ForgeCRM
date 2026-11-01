/**
 * Tests for the task detail page.
 *
 * The page reads a single task through `GET /api/v1/tasks/{id}`, which the
 * interface only reaches by clicking a row. These tests hold the page to that
 * path: the row click navigates to the detail route, the page reads the record
 * the server returns rather than a list, and a failure is surfaced instead of
 * rendering an empty card. A task that is already complete must not offer the
 * Complete control, because the server would reject a no-op update that the
 * button implies succeeded.
 */
import { describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { TaskDetailPage, TasksPage } from './TasksPage'
import { AuthProvider } from '../hooks/useAuth'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const task = {
  id: 'task-1',
  organization_id: 'org-1',
  title: 'Call Acme about renewal',
  description: 'Confirm the contract terms',
  status: 'open',
  priority: 'high',
  due_at: '2026-11-05T09:00:00Z',
  created_at: '2026-10-01T09:00:00Z',
  updated_at: '2026-10-02T09:00:00Z',
}

function stubFetch(handler: (url: string) => Response | undefined) {
  const fetchMock = vi.fn((input: RequestInfo | URL, _init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    const path = url.replace(/^https?:\/\/[^/]+/, '')
    if (path.includes('/api/v1/auth/me')) {
      return Promise.resolve(
        json({
          user: { id: 'u1', email: 'a@example.com', display_name: 'A' },
          organization: { id: 'org-1', name: 'Org' },
          permissions: ['tasks.read', 'tasks.update'],
          csrf_token: 't',
        }),
      )
    }
    const handled = handler(path)
    if (handled) return Promise.resolve(handled)
    return Promise.resolve(json({ items: [], total: 0, limit: 25, offset: 0 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderDetail() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/tasks/task-1']}>
        <AuthProvider>
          <Routes>
            <Route path="/tasks/:id" element={<TaskDetailPage />} />
          </Routes>
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('TaskDetailPage', () => {
  it('renders the task the server returns for the id in the path', async () => {
    const fetchMock = stubFetch((path) =>
      path === '/api/v1/tasks/task-1' ? json(task) : undefined,
    )

    renderDetail()

    expect(await screen.findByRole('heading', { name: 'Call Acme about renewal' })).toBeDefined()
    expect(screen.getByText('Confirm the contract terms')).toBeDefined()
    expect(
      fetchMock.mock.calls.some(([input]) => String(input).includes('/api/v1/tasks/task-1')),
    ).toBe(true)
  })

  it('surfaces a load failure instead of an empty card', async () => {
    stubFetch((path) =>
      path === '/api/v1/tasks/task-1'
        ? json({ error: { code: 'not_found', message: 'task not found' } }, 404)
        : undefined,
    )

    renderDetail()

    expect(await screen.findByText(/task not found/i)).toBeDefined()
  })

  it('does not offer to complete a task that is already completed', async () => {
    stubFetch((path) =>
      path === '/api/v1/tasks/task-1'
        ? json({ ...task, status: 'completed', completed_at: '2026-10-03T09:00:00Z' })
        : undefined,
    )

    renderDetail()

    await screen.findByRole('heading', { name: 'Call Acme about renewal' })
    expect(screen.queryByRole('button', { name: 'Complete' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Edit' })).toBeDefined()
  })
})

describe('TasksPage row navigation', () => {
  it('opens the detail route when a row is clicked', async () => {
    stubFetch((path) =>
      path.startsWith('/api/v1/tasks?') || path === '/api/v1/tasks'
        ? json({ items: [task], total: 1, limit: 25, offset: 0 })
        : undefined,
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/tasks']}>
          <AuthProvider>
            <Routes>
              <Route path="/tasks" element={<TasksPage />} />
              <Route path="/tasks/:id" element={<div>Task detail page</div>} />
            </Routes>
          </AuthProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const user = userEvent.setup()
    await user.click(await screen.findByText('Call Acme about renewal'))
    await waitFor(() => expect(screen.getByText('Task detail page')).toBeDefined())
  })
})
