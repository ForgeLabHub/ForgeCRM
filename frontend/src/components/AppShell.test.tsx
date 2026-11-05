/**
 * Accessibility regression tests for the application shell.
 *
 * These render the real shell against a stubbed `fetch` for the session, so the
 * assertions are about the DOM the browser builds, not about a mock of our own
 * components. Each one pins a defect a live axe scan found: the account popover
 * promised an ARIA menu it did not implement, and the topbar search button lost
 * its accessible name below the breakpoint where its label is hidden.
 */
import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { AuthProvider } from '../hooks/useAuth'
import { I18nProvider } from '../i18n'
import { ThemeProvider } from '../theme'
import { WorkspaceProvider } from '../workspace/WorkspaceProvider'
import { AppShell } from './AppShell'
import type { MeResponse } from '../types/api'

const ME: MeResponse = {
  user: {
    id: 'u-1',
    email: 'sales@example.com',
    display_name: 'Sam Sales',
    status: 'active',
    is_platform_owner: false,
    mfa_enabled: false,
  },
  organization: { id: 'o-1', name: 'Acme Inc' },
  permissions: ['contacts.read'],
  teams: [],
  csrf_token: 'csrf-from-me',
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function renderShell() {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString()
    const path = url.replace(/^https?:\/\/[^/]+/, '').split('?')[0]
    if (path === '/api/v1/auth/me') return json(ME)
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

describe('AppShell accessibility', () => {
  it('makes a closed phone drawer inert and restores focus after Escape', async () => {
    vi.stubGlobal('matchMedia', vi.fn(() => ({
      matches: true,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })))
    renderShell()
    const user = userEvent.setup()
    const toggle = await screen.findByRole('button', { name: /^menu$/i })
    const drawer = document.getElementById('main-navigation-drawer')!
    expect(drawer).toHaveAttribute('inert')
    expect(drawer).toHaveAttribute('aria-hidden', 'true')

    await user.click(toggle)
    expect(drawer).not.toHaveAttribute('inert')
    expect(drawer).toHaveAttribute('role', 'dialog')
    expect(drawer).toHaveFocus()
    expect(document.body.style.overflow).toBe('hidden')
    await user.keyboard('{Escape}')
    expect(drawer).toHaveAttribute('inert')
    expect(document.body.style.overflow).toBe('')
    expect(toggle).toHaveFocus()
  })

  // The account popover holds an identity block, a theme-switch group and action
  // items. role="menu" requires its children to be menuitems, which this popover
  // is not, so the role must not be present — an invalid tree axe reports.
  it('does not expose the account popover as an ARIA menu', async () => {
    renderShell()
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: /my account/i }))

    await screen.findByText(/sign out/i)
    expect(screen.queryByRole('menu')).toBeNull()
    expect(screen.queryByRole('menuitem')).toBeNull()
  })

  // Below the phone breakpoint the search button's label and kbd hint are hidden
  // and its icon is aria-hidden, so without an explicit label the button has no
  // accessible name at all. It must always have one.
  it('gives the topbar search button an accessible name', async () => {
    renderShell()
    await screen.findByRole('button', { name: /^search$/i })
  })

  // Dropping role="menu" must not lose the keyboard behaviour the popover did
  // have: Escape still closes it.
  it('closes the account popover on Escape', async () => {
    renderShell()
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: /my account/i }))
    await screen.findByText(/sign out/i)

    await user.keyboard('{Escape}')
    expect(screen.queryByText(/sign out/i)).toBeNull()
  })
})
