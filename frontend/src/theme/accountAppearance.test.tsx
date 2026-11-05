import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { I18nProvider } from '../i18n'
import { AppearancePage } from '../pages/AppearancePage'
import { ThemeProvider, useTheme } from './index'
import { appearanceApi, type AppearanceResponse } from '../api/appearance'

vi.mock('../api/appearance', () => ({ appearanceApi: { get: vi.fn(), save: vi.fn() } }))

const initial: AppearanceResponse = {
  style: null, mode: 'system', accent: 'theme', density: 'standard',
  profile: 'executive', default_style: 'premium', effective_style: 'premium',
}
function State() {
  const { ready } = useTheme()
  return <span data-testid="ready">{String(ready)}</span>
}
function tree(key: string) {
  return <I18nProvider><ThemeProvider account={{ status: 'authenticated', key }}><State /><AppearancePage /></ThemeProvider></I18nProvider>
}

beforeEach(() => {
  localStorage.clear()
  localStorage.setItem('forgecrm.locale', 'en')
  vi.resetAllMocks()
  vi.mocked(appearanceApi.get).mockResolvedValue(initial)
})

describe('account appearance', () => {
  it('loads the role default and previews without saving, then persists on apply', async () => {
    vi.mocked(appearanceApi.save).mockImplementation(async (input) => ({ ...initial, ...input, effective_style: input.style ?? 'premium' }))
    render(tree('alice:org'))
    await waitFor(() => expect(document.documentElement.getAttribute('data-style')).toBe('premium'))
    const glass = screen.getByText('Glass').closest('.style-card') as HTMLElement
    fireEvent.click(within(glass).getByRole('button', { name: 'Preview' }))
    expect(document.documentElement.getAttribute('data-style')).toBe('glass')
    expect(appearanceApi.save).not.toHaveBeenCalled()
    fireEvent.click(within(glass).getByRole('button', { name: 'Apply' }))
    await waitFor(() => expect(appearanceApi.save).toHaveBeenCalledWith({ style: 'glass', mode: 'system', accent: 'theme', density: 'standard' }))
    await waitFor(() => expect(document.documentElement.getAttribute('data-style')).toBe('glass'))
    expect(localStorage.getItem('forgecrm.style')).toBeNull()
  })

  it('does not claim a failed write succeeded and reset uses the role default', async () => {
    vi.mocked(appearanceApi.save).mockRejectedValueOnce(new Error('offline')).mockImplementation(async (input) => ({ ...initial, ...input, effective_style: input.style ?? 'premium' }))
    render(tree('alice:org'))
    await waitFor(() => expect(document.documentElement.getAttribute('data-style')).toBe('premium'))
    const glass = screen.getByText('Glass').closest('.style-card') as HTMLElement
    fireEvent.click(within(glass).getByRole('button', { name: 'Apply' }))
    expect(await screen.findByText(/Could not confirm the save/)).toBeTruthy()
    expect(document.documentElement.getAttribute('data-style')).toBe('premium')
    fireEvent.click(screen.getByRole('button', { name: 'Reset to defaults' }))
    await screen.findByText('Appearance reset to defaults.')
    expect(appearanceApi.save).toHaveBeenLastCalledWith({ style: null, mode: 'system', accent: 'theme', density: 'standard' })
    expect(document.documentElement.getAttribute('data-style')).toBe('premium')
  })

  it('disables changes and distinguishes a load failure from a save failure', async () => {
    vi.mocked(appearanceApi.get).mockRejectedValueOnce(new Error('offline'))
    render(tree('alice:org'))
    await waitFor(() => expect(screen.getAllByRole('button', { name: 'Preview' })[0]).toBeDisabled())
    expect(screen.getByRole('button', { name: 'Reset to defaults' })).toBeDisabled()
    expect(screen.queryByText(/Could not confirm the save/)).toBeNull()
    expect(appearanceApi.save).not.toHaveBeenCalled()
  })

  it('ignores a late save from an earlier session of the same user', async () => {
    let finish!: (value: AppearanceResponse) => void
    vi.mocked(appearanceApi.save).mockImplementationOnce(() => new Promise((resolve) => { finish = resolve }))
    const view = render(tree('alice:org'))
    await waitFor(() => expect(document.documentElement.getAttribute('data-style')).toBe('premium'))
    const glass = screen.getByText('Glass').closest('.style-card') as HTMLElement
    fireEvent.click(within(glass).getByRole('button', { name: 'Apply' }))
    expect(appearanceApi.save).toHaveBeenCalledTimes(1)
    view.rerender(<I18nProvider><ThemeProvider account={{ status: 'anonymous', key: null }}><State /></ThemeProvider></I18nProvider>)
    view.rerender(tree('alice:org'))
    await waitFor(() => expect(screen.getByTestId('ready').textContent).toBe('true'))
    await act(async () => { finish({ ...initial, style: 'glass', effective_style: 'glass' }) })
    expect(document.documentElement.getAttribute('data-style')).toBe('premium')
    expect(screen.queryByText(/Could not confirm the save/)).toBeNull()
  })

  it('does not show the previous account style while a new account loads', async () => {
    let finish!: (value: AppearanceResponse) => void
    vi.mocked(appearanceApi.get).mockResolvedValueOnce(initial).mockImplementationOnce(() => new Promise((resolve) => { finish = resolve }))
    const view = render(tree('alice:org'))
    await waitFor(() => expect(document.documentElement.getAttribute('data-style')).toBe('premium'))
    view.rerender(tree('bob:org'))
    await waitFor(() => expect(screen.getByTestId('ready').textContent).toBe('false'))
    expect(document.documentElement.getAttribute('data-style')).toBe('default')
    finish({ ...initial, profile: 'support_agent', default_style: 'glass', effective_style: 'glass' })
    await waitFor(() => expect(document.documentElement.getAttribute('data-style')).toBe('glass'))
    expect(screen.getByTestId('ready').textContent).toBe('true')
  })
})
