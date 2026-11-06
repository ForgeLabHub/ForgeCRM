import { describe, expect, it, vi, afterEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { I18nProvider } from '../i18n'
import { api } from '../api/client'
import { AiAssistantChat, AiAssistantLauncher } from './AiAssistant'

const auth = vi.hoisted(() => ({ status: 'authenticated', permissions: ['ai.read', 'ai.tools.execute', 'approvals.read'] }))
vi.mock('../hooks/useAuth', () => ({ useAuth: () => ({ status: auth.status, can: (permission: string) => auth.permissions.includes(permission) }) }))

function mount(node: React.ReactNode) {
  return render(<I18nProvider><MemoryRouter>{node}</MemoryRouter></I18nProvider>)
}

afterEach(() => {
  auth.status = 'authenticated'
  auth.permissions = ['ai.read', 'ai.tools.execute', 'approvals.read']
})

describe('AI assistant', () => {
  it('invites anonymous visitors to sign in without calling AI', async () => {
    auth.status = 'anonymous'
    const stream = vi.spyOn(api, 'stream')
    mount(<AiAssistantLauncher />)
    await userEvent.click(screen.getByRole('button', { name: /AI assistant/i }))
    expect(screen.getByRole('link', { name: /sign in/i })).toHaveAttribute('href', '/login')
    expect(stream).not.toHaveBeenCalled()
  })

  it('streams a response and links to approvals without claiming CRM access', async () => {
    const stream = vi.spyOn(api, 'stream').mockImplementation(async (_path, _body, onEvent) => {
      onEvent({ event: 'delta', data: { text: 'Hello' } })
      onEvent({ event: 'done', data: {} })
    })
    mount(<AiAssistantChat />)
    await userEvent.type(screen.getByLabelText('Your question'), 'Hello')
    await userEvent.click(screen.getByRole('button', { name: 'Send' }))
    expect(await screen.findByText('Hello', { selector: '.assistant-chat__answer p' })).toBeInTheDocument()
    expect(screen.getByText(/cannot access or change CRM records/i)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Review approvals' })).toHaveAttribute('href', '/app/approvals')
    expect(stream).toHaveBeenCalledWith('/api/v1/ai/stream/chat', expect.objectContaining({ prompt: 'Hello' }), expect.any(Function), expect.any(AbortSignal))
  })

  it('does not permit requests without the execute permission', () => {
    auth.permissions = ['ai.read']
    mount(<AiAssistantChat />)
    expect(screen.getByText(/does not have permission/i)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Send' })).not.toBeInTheDocument()
  })

  it('reports an incomplete stream instead of treating it as a complete answer', async () => {
    vi.spyOn(api, 'stream').mockImplementation(async (_path, _body, onEvent) => {
      onEvent({ event: 'delta', data: { text: 'Partial' } })
    })
    mount(<AiAssistantChat />)
    await userEvent.type(screen.getByLabelText('Your question'), 'Hello')
    await userEvent.click(screen.getByRole('button', { name: 'Send' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(/interrupted/i)
  })
})
