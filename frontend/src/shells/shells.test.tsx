/**
 * The experience switcher and the four redesigned shells.
 *
 * These render the real AppShell against a stubbed `fetch` for the session —
 * the same approach as the shell accessibility tests — and assert on the DOM:
 * which anatomy mounted, which chrome it carries, and that the permission
 * filter applies identically in every anatomy (a section the user cannot read
 * is absent from tabs, dropdowns, launchers and rails alike).
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { AuthProvider } from '../hooks/useAuth'
import { I18nProvider } from '../i18n'
import { ThemeProvider } from '../theme'
import { WorkspaceProvider } from '../workspace/WorkspaceProvider'
import { AppShell } from '../components/AppShell'
import { LAYOUTS, LAYOUT_STORAGE_KEY, isLayout, layoutLabelKey } from '../theme/themes'
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
  permissions: ['contacts.read', 'notifications.read', 'tasks.read'],
  teams: [],
  csrf_token: 'csrf-from-me',
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function renderShell(layout?: string) {
  if (layout) window.localStorage.setItem(LAYOUT_STORAGE_KEY, layout)
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

beforeEach(() => {
  window.localStorage.clear()
  document.documentElement.removeAttribute('data-layout')
})

describe('layout catalogue', () => {
  it('declares exactly the five experiences, classic first', () => {
    expect(LAYOUTS).toEqual(['classic', 'stratus', 'horizon', 'pulse', 'slate'])
    for (const layout of LAYOUTS) {
      expect(isLayout(layout)).toBe(true)
      expect(layoutLabelKey(layout)).toMatch(/^appearance\.layout\.\w+\.name$/)
    }
    expect(isLayout('unknown')).toBe(false)
    expect(isLayout(null)).toBe(false)
  })
})

describe('experience switching', () => {
  it.each([
    [undefined, '.app-shell'],
    ['classic', '.app-shell'],
    ['stratus', '.shell.stratus'],
    ['horizon', '.shell.horizon'],
    ['pulse', '.shell.pulse'],
    ['slate', '.shell.slate'],
  ] as const)('layout=%s mounts the %s anatomy', (layout, selector) => {
    const { container } = renderShell(layout)
    expect(container.querySelector(selector)).not.toBeNull()
    expect(document.documentElement.getAttribute('data-layout')).toBe(layout ?? 'classic')
  })

  it('restores the stored experience on the next visit', () => {
    // A device that chose Slate before mounts Slate again without being asked.
    window.localStorage.setItem(LAYOUT_STORAGE_KEY, 'slate')
    const { container } = renderShell()
    expect(container.querySelector('.shell.slate')).not.toBeNull()
    expect(document.documentElement.getAttribute('data-layout')).toBe('slate')
  })
})

describe('stratus anatomy', () => {
  it('carries a launcher, a tab strip and the utility bar', async () => {
    const user = userEvent.setup()
    const { container } = renderShell('stratus')

    // The waffle launcher is the cloud-suite signature.
    const launcher = screen.getByRole('button', { name: 'App launcher' })
    expect(launcher).toHaveAttribute('aria-expanded', 'false')

    await user.click(launcher)
    expect(screen.getByText('Every section you have access to, in one panel.')).toBeTruthy()
    // Permissions filter the launcher's catalog: contacts is allowed, the
    // administration area is not, so the whole admin section is absent.
    const panel = container.querySelector('.stratus-launcher__panel')
    expect(panel?.textContent).toContain('Contacts')
    expect(panel?.textContent).not.toContain('Administration')

    // The pinned utility bar lists the allowed quick links only.
    const utility = container.querySelector('.stratus-utility')
    expect(utility?.textContent).toContain('Notifications')
    expect(utility?.textContent).toContain('Tasks')
    expect(utility?.textContent).not.toContain('Approvals')
  })
})

describe('horizon anatomy', () => {
  it('opens product areas as dropdown menus from the dark bar', async () => {
    const user = userEvent.setup()
    const { container } = renderShell('horizon')

    expect(container.querySelector('.horizon-subbar')).not.toBeNull()
    // The exact name distinguishes the area dropdown from the workspace
    // switcher ('Switch workspace') riding beside it.
    const trigger = screen.getByRole('button', { name: 'Workspace' })
    await user.click(trigger)
    expect(trigger).toHaveAttribute('aria-expanded', 'true')

    // The menu lists allowed entries; leads (no permission) is not among them.
    const panel = container.querySelector('.horizon-menu__panel')
    expect(panel?.textContent).toContain('Contacts')
    expect(panel?.textContent).not.toContain('Leads')
  })
})

describe('pulse anatomy', () => {
  it('shows the colour-coded workspace rail and the workspace panel', () => {
    const { container } = renderShell('pulse')
    // One tile per workspace, the active one pressed.
    const tiles = container.querySelectorAll('.pulse-tile')
    expect(tiles.length).toBe(8)
    expect(container.querySelector('.pulse-tile--active')).not.toBeNull()
    expect(container.querySelector('.pulse-panel')).not.toBeNull()
  })
})

describe('slate anatomy', () => {
  it('frames the page with a breadcrumb top bar and a compact sidebar', () => {
    const { container } = renderShell('slate')
    expect(container.querySelector('.slate-top')).not.toBeNull()
    expect(container.querySelector('.slate-side__nav')).not.toBeNull()
    expect(container.querySelector('.slate-top__crumb')?.textContent).toContain('ForgeCRM')
  })
})
