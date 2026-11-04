/**
 * Workspace behaviour.
 *
 * The properties that matter: the choice persists across a reload, a workspace
 * shows only routes the user can actually read, it reorders rather than widens,
 * and switching back to "Everything" restores the full grouped navigation.
 */
import { describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { AuthProvider } from '../hooks/useAuth'
import { I18nProvider } from '../i18n'
import { ThemeProvider } from '../theme'
import { WorkspaceProvider } from './WorkspaceProvider'
import { WORKSPACE_STORAGE_KEY } from '../workspaces'
import { AppShell } from '../components/AppShell'
import type { MeResponse } from '../types/api'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function me(permissions: string[]): MeResponse {
  return {
    user: {
      id: 'u-1',
      email: 'user@example.com',
      display_name: 'Test User',
      status: 'active',
      is_platform_owner: false,
      mfa_enabled: false,
    },
    organization: { id: 'o-1', name: 'Acme Inc' },
    permissions,
    teams: [],
    csrf_token: 'csrf-from-me',
  }
}

function renderShell(permissions: string[]) {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString()
    const path = url.replace(/^https?:\/\/[^/]+/, '').split('?')[0]
    if (path === '/api/v1/auth/me') return json(me(permissions))
    return json({ error: { code: 'not_found', message: `no stub for ${path}` } }, 404)
  })
  vi.stubGlobal('fetch', fetchMock)

  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider>
        <ThemeProvider>
          <MemoryRouter initialEntries={['/']}>
            <AuthProvider>
              <WorkspaceProvider>
                <Routes>
                  <Route path="/" element={<AppShell />}>
                    <Route index element={<div>Overview</div>} />
                  </Route>
                </Routes>
              </WorkspaceProvider>
            </AuthProvider>
          </MemoryRouter>
        </ThemeProvider>
      </I18nProvider>
    </QueryClientProvider>,
  )
}

const nav = () => screen.getByRole('navigation', { name: /main navigation/i })

describe('workspaces', () => {
  it('defaults to the full grouped navigation', async () => {
    localStorage.setItem('forgecrm.locale', 'en')
    renderShell(['deals.read', 'accounts.read', 'users.read'])
    // The grouped view labels its sections, which the flat workspace view does not.
    expect(within(await screen.findByRole('navigation')).getByText('Workspace')).toBeInTheDocument()
    expect(within(nav()).getByText('Administration')).toBeInTheDocument()
  })

  it('switches to a workspace, reorders and persists the choice', async () => {
    localStorage.setItem('forgecrm.locale', 'en')
    renderShell(['deals.read', 'leads.read', 'accounts.read'])
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: /switch workspace/i }))
    await user.click(screen.getByRole('button', { name: /^Sales/ }))

    expect(localStorage.getItem(WORKSPACE_STORAGE_KEY)).toBe('sales')
    // In a workspace the section label is the workspace name, not "Workspace".
    expect(within(nav()).getByText('Sales')).toBeInTheDocument()
    expect(within(nav()).queryByText('Administration')).not.toBeInTheDocument()
    // Deals is ahead of Accounts in the Sales order.
    const links = within(nav()).getAllByRole('link')
    const labels = links.map((l) => l.textContent ?? '')
    expect(labels.indexOf('Deals')).toBeLessThan(labels.indexOf('Accounts'))
  })

  it('never shows a route the user cannot read, even inside a workspace', async () => {
    localStorage.setItem('forgecrm.locale', 'en')
    // Sales workspace lists deals and accounts, but the user may read neither.
    renderShell([])
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: /switch workspace/i }))
    await user.click(screen.getByRole('button', { name: /^Sales/ }))

    expect(within(nav()).queryByText('Deals')).not.toBeInTheDocument()
    expect(within(nav()).queryByText('Accounts')).not.toBeInTheDocument()
  })

  it('restores a stored workspace on the next visit', async () => {
    localStorage.setItem('forgecrm.locale', 'en')
    localStorage.setItem(WORKSPACE_STORAGE_KEY, 'support')
    renderShell(['tickets.read'])
    expect(within(nav()).getByText('Support')).toBeInTheDocument()
  })

  it('lets the user reorder the sidebar within a workspace', async () => {
    localStorage.setItem('forgecrm.locale', 'en')
    localStorage.setItem(WORKSPACE_STORAGE_KEY, 'support')
    renderShell(['tickets.read', 'contacts.read'])
    const user = userEvent.setup()

    await within(nav()).findByText('Support')

    const before = within(nav())
      .getAllByRole('link')
      .map((l) => (l.textContent ?? '').trim())
    expect(before.indexOf('Support')).toBeLessThan(before.indexOf('Contacts'))

    // Move "Contacts" up one place.
    await user.click(within(nav()).getByRole('button', { name: /move contacts up/i }))

    const after = within(nav())
      .getAllByRole('link')
      .map((l) => l.textContent ?? '')
    expect(after.indexOf('Contacts')).toBeLessThan(after.indexOf('Support'))
  })
})
