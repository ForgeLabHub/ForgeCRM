import { describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { I18nProvider } from '../i18n'
import { aiApi, mcpApi } from '../api/ai'
import { AiPage } from './AiPage'

vi.mock('../hooks/useAuth', () => ({ useAuth: () => ({ can: () => true, session: { user: { id: 'u' }, organization: { id: 'o' } } }) }))

function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><I18nProvider><MemoryRouter><AiPage /></MemoryRouter></I18nProvider></QueryClientProvider>)
}

describe('AI status', () => {
  it('shows the server provider health and the actual token fields', async () => {
    vi.spyOn(aiApi, 'status').mockResolvedValue({ enabled: true, provider: 'groq', model: 'test-model', healthy: false, error: 'not reachable' })
    vi.spyOn(mcpApi, 'tools').mockResolvedValue({ tools: [] })
    vi.spyOn(aiApi, 'tasks').mockResolvedValue({ items: [{ id: 'task', kind: 'chat', status: 'succeeded', model: 'test-model', input_tokens: 12, output_tokens: 34, created_at: '2026-01-01T00:00:00Z' }], total: 1, limit: 20, offset: 0 })
    mount()
    expect(await screen.findByText('groq')).toBeInTheDocument()
    expect(screen.getByText('unhealthy')).toBeInTheDocument()
    expect(screen.getByText('not reachable')).toBeInTheDocument()
    const task = await screen.findByText('chat')
    expect(within(task.closest('tr')!).getByText('12 / 34')).toBeInTheDocument()
  })

  it('explains when no AI provider is configured', async () => {
    vi.spyOn(aiApi, 'status').mockResolvedValue({ enabled: false, provider: '', model: '', reason: 'no AI provider is configured' })
    vi.spyOn(mcpApi, 'tools').mockResolvedValue({ tools: [] })
    vi.spyOn(aiApi, 'tasks').mockResolvedValue({ items: [], total: 0, limit: 20, offset: 0 })
    mount()
    expect(await screen.findByText(/No AI provider is configured/)).toBeInTheDocument()
  })
})
