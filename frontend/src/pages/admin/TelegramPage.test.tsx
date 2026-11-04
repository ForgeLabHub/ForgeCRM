import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { I18nProvider } from '../../i18n'
import { TelegramPage } from './TelegramPage'
import { telegramApi } from '../../api/notifications'

vi.mock('../../api/notifications', () => ({
  telegramApi: { status: vi.fn(), linkCode: vi.fn(), unlink: vi.fn() },
}))

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><I18nProvider><TelegramPage /></I18nProvider></QueryClientProvider>)
}

beforeEach(() => {
  localStorage.setItem('forgecrm.locale', 'en')
  vi.resetAllMocks()
})

describe('TelegramPage', () => {
  it('does not claim a disabled service has a missing token', async () => {
    vi.mocked(telegramApi.status).mockResolvedValue({ enabled: false })
    renderPage()
    expect(await screen.findByText(/Telegram is disabled or not configured/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Generate link code' })).not.toBeInTheDocument()
  })

  it('does not offer linking when the server has no inbound transport', async () => {
    vi.mocked(telegramApi.status).mockResolvedValue({ enabled: true })
    renderPage()
    expect(await screen.findByText(/incoming Telegram messages and account linking are not available yet/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Generate link code' })).not.toBeInTheDocument()
  })

  it('uses the API duration for the link code expiry when linking is available', async () => {
    vi.mocked(telegramApi.status).mockResolvedValue({ enabled: true, linking_available: true })
    vi.mocked(telegramApi.linkCode).mockResolvedValue({ code: '12345678', expires_in_seconds: 900 })
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: 'Generate link code' }))
    expect(await screen.findByText('12345678')).toBeInTheDocument()
    expect(screen.getByText(/It expires within 900 seconds/)).toBeInTheDocument()
  })
})
