/**
 * The public site, rendered through the real router.
 *
 * These assertions are about routing rather than about typography: that each
 * public URL resolves to its page without a session, that the application stays
 * behind `/app`, that an unknown URL inside the site does not fall through to the
 * CRM, and that the cookie gate asks for a decision once and then remembers it.
 *
 * The session endpoint is stubbed to fail, which is what an anonymous visitor
 * looks like to the app: the site has to render anyway. That is the whole point
 * of moving the marketing pages out from behind the old auth branch, so it is
 * pinned here rather than left to be discovered in production.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { App } from '../App'
import { AuthProvider } from '../hooks/useAuth'
import { I18nProvider } from '../i18n'
import { ThemeProvider } from '../theme'
import { WorkspaceProvider } from '../workspace/WorkspaceProvider'
import { COOKIE_STORAGE_KEY, readCookieDecision } from './components/CookieConsent'
import { SITE_VARIANT_KEY } from './components/SiteVariantSwitcher'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

/** No session: every API call answers 401, as it would for a stranger. */
function stubAnonymous() {
  vi.stubGlobal(
    'fetch',
    vi.fn(() =>
      Promise.resolve(json({ error: { code: 'unauthorized', message: 'no session' } }, 401)),
    ),
  )
}

function renderSite(path: string) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider>
        <ThemeProvider>
          <MemoryRouter initialEntries={[path]}>
            <AuthProvider>
              <WorkspaceProvider>
                <App />
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
  document.documentElement.removeAttribute('data-site-variant')
  stubAnonymous()
})

describe('public routing', () => {
  it('serves the home page to an anonymous visitor', async () => {
    renderSite('/')
    expect(
      await screen.findByRole('heading', { level: 1, name: /команда/i }),
    ).toBeInTheDocument()
  })

  it('serves the service catalogue', async () => {
    renderSite('/services')
    expect(await screen.findByRole('heading', { level: 1, name: 'Что мы делаем' })).toBeInTheDocument()
  })

  it('opens a service page by slug', async () => {
    renderSite('/services/amocrm-implementation')
    expect(
      await screen.findByRole('heading', { level: 1, name: 'Внедрение amoCRM' }),
    ).toBeInTheDocument()
  })

  it('opens the pricing page', async () => {
    renderSite('/pricing')
    expect(
      await screen.findByRole('heading', { level: 1, name: 'Тарифы и лицензии' }),
    ).toBeInTheDocument()
  })

  it('opens a case study by slug', async () => {
    renderSite('/cases/stroykontur')
    expect(
      await screen.findByRole('heading', { level: 1, name: /Сметы и объекты/i }),
    ).toBeInTheDocument()
  })

  it('opens an article by slug', async () => {
    renderSite('/blog/kakie-byvayut-crm')
    expect(
      await screen.findByRole('heading', { level: 1, name: /Какие бывают CRM/i }),
    ).toBeInTheDocument()
  })

  it('opens the contacts page with its form', async () => {
    renderSite('/contacts')
    expect(await screen.findByRole('heading', { level: 1, name: 'Давайте поговорим' })).toBeInTheDocument()
    // The submit control exists before anything is typed and refuses to send an
    // empty request, rather than being a link that only appears when valid.
    expect(screen.getByRole('button', { name: 'Отправить' })).toBeDisabled()
  })

  it('opens the cookie policy page', async () => {
    renderSite('/privacy')
    expect(
      await screen.findByRole('heading', { level: 1, name: /Политика обработки данных/i }),
    ).toBeInTheDocument()
  })

  it('keeps an unknown site URL inside the site, not inside the CRM', async () => {
    renderSite('/no-such-page')
    expect(await screen.findByRole('heading', { level: 1, name: 'Такой страницы нет' })).toBeInTheDocument()
    // The CRM's empty state would say this; the site's page must not.
    expect(screen.queryByText(/Something went wrong|Что-то пошло не так/i)).toBeNull()
  })

  it('does not render application navigation to a stranger', async () => {
    renderSite('/')
    await screen.findByRole('heading', { level: 1, name: /команда/i })
    expect(screen.queryByText('Global search')).toBeNull()
  })

  it('sends a stranger from the application to the sign-in form', async () => {
    renderSite('/app/deals')
    // The form's submit control, in whichever language the last test left set.
    await waitFor(() => expect(screen.getByLabelText(/email/i)).toBeInTheDocument())
    expect(screen.getByLabelText(/^password$/i)).toBeInTheDocument()
  })

  it('offers the sign-in form at its own URL', async () => {
    renderSite('/login')
    await waitFor(() => expect(screen.getByLabelText(/email/i)).toBeInTheDocument())
  })
})

describe('cookie consent', () => {
  it('asks for a decision on the first visit', async () => {
    renderSite('/')
    await screen.findByRole('heading', { level: 1, name: /команда/i })
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByRole('heading', { name: /cookies/i })).toBeInTheDocument()
    // Three exits, so refusing is as easy as accepting.
    expect(within(dialog).getByRole('button', { name: 'Принять все' })).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: 'Только необходимые' })).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: 'Настроить' })).toBeInTheDocument()
  })

  it('reveals the categories, with the necessary ones locked on', async () => {
    const user = userEvent.setup()
    renderSite('/')
    const dialog = await screen.findByRole('dialog')

    await user.click(within(dialog).getByRole('button', { name: 'Настроить' }))

    // Necessary cookies are stated, not offered: the row says they are always on
    // and carries no switch.
    expect(within(dialog).getByText('Необходимые')).toBeInTheDocument()
    expect(within(dialog).getByText('Всегда включены')).toBeInTheDocument()
    const analytics = within(dialog).getByRole('checkbox', { name: /аналитические/i })
    const marketing = within(dialog).getByRole('checkbox', { name: /маркетинговые/i })
    // A fresh visitor starts opted out; nothing is pre-ticked on their behalf.
    expect(analytics).not.toBeChecked()
    expect(marketing).not.toBeChecked()

    await user.click(analytics)
    await user.click(within(dialog).getByRole('button', { name: 'Сохранить выбор' }))

    await waitFor(() => expect(readCookieDecision()).not.toBeNull())
    expect(readCookieDecision()?.analytics).toBe(true)
    expect(readCookieDecision()?.marketing).toBe(false)
  })

  it('stores only the categories the visitor accepted', async () => {
    const user = userEvent.setup()
    renderSite('/')
    const dialog = await screen.findByRole('dialog')

    await user.click(within(dialog).getByRole('button', { name: /только необходимые/i }))

    await waitFor(() => expect(readCookieDecision()).not.toBeNull())
    const decision = readCookieDecision()
    expect(decision?.analytics).toBe(false)
    expect(decision?.marketing).toBe(false)
    expect(decision?.necessary).toBe(true)
  })

  it('does not ask again once a decision is stored', async () => {
    window.localStorage.setItem(
      COOKIE_STORAGE_KEY,
      JSON.stringify({
        version: 1,
        decidedAt: '2026-11-01T00:00:00.000Z',
        necessary: true,
        analytics: true,
        marketing: false,
      }),
    )
    renderSite('/')
    await screen.findByRole('heading', { level: 1, name: /команда/i })
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('reopens on demand from the policy page', async () => {
    const user = userEvent.setup()
    window.localStorage.setItem(
      COOKIE_STORAGE_KEY,
      JSON.stringify({
        version: 1,
        decidedAt: '2026-11-01T00:00:00.000Z',
        necessary: true,
        analytics: false,
        marketing: false,
      }),
    )
    renderSite('/privacy')
    await user.click(await screen.findByRole('button', { name: /Открыть настройки cookie/i }))
    expect(await screen.findByRole('dialog')).toBeInTheDocument()
  })
})

describe('site variants', () => {
  it('defaults to the agency arrangement', async () => {
    renderSite('/')
    await screen.findByRole('heading', { level: 1, name: /команда/i })
    expect(document.documentElement.getAttribute('data-site-variant')).toBe('agency')
  })

  it('restores a stored arrangement and forgets it when leaving the site', async () => {
    window.localStorage.setItem(SITE_VARIANT_KEY, 'docs')
    const view = renderSite('/services')
    await screen.findByRole('heading', { level: 1, name: 'Что мы делаем' })
    expect(document.documentElement.getAttribute('data-site-variant')).toBe('docs')

    // The CRM must not inherit an arrangement it has no styles for.
    view.unmount()
    expect(document.documentElement.getAttribute('data-site-variant')).toBeNull()
  })
})
