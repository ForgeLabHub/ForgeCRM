import { describe, expect, it, vi, afterEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { I18nProvider } from '../i18n'
import { api } from '../api/client'
import * as assistant from '../api/assistantCrm'
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
    const request = vi.spyOn(api, 'request')
    mount(<AiAssistantLauncher />)
    await userEvent.click(screen.getByRole('button', { name: /AI assistant/i }))
    expect(screen.getByRole('link', { name: /sign in/i })).toHaveAttribute('href', '/login')
    expect(request).not.toHaveBeenCalled()
  })

  it('answers and explains that permitted CRM data may be sent to the provider', async () => {
    vi.spyOn(assistant, 'askAssistant').mockResolvedValue({ kind: 'answer', text: 'Hello' })
    mount(<AiAssistantChat />)
    await userEvent.type(screen.getByLabelText('Your question'), 'Hello')
    await userEvent.click(screen.getByRole('button', { name: 'Send' }))
    expect(await screen.findByText('Hello', { selector: '.assistant-chat__answer p' })).toBeInTheDocument()
    expect(screen.getByText(/may be sent to the configured AI provider/i)).toBeInTheDocument()
  })

  it('states that proposed actions have not yet executed', async () => {
    vi.spyOn(assistant, 'askAssistant').mockResolvedValue({ kind: 'proposal', actionId: 'a1' })
    mount(<AiAssistantChat />)
    await userEvent.type(screen.getByLabelText('Your question'), 'Create task')
    await userEvent.click(screen.getByRole('button', { name: 'Send' }))
    expect(await screen.findByRole('status')).toHaveTextContent(/has not been executed/i)
  })

  it('does not permit requests without the execute permission', () => {
    auth.permissions = ['ai.read']
    mount(<AiAssistantChat />)
    expect(screen.getByText(/does not have permission/i)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Send' })).not.toBeInTheDocument()
  })
})
