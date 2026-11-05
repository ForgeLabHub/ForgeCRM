/**
 * Appearance page behaviour.
 *
 * The properties worth testing here are the ones a screenshot cannot show: a
 * style is applied to the document the moment it is chosen, preview is temporary
 * and never persisted, reset returns every choice to its default, and the choice
 * survives a reload because it is read from storage on the next mount.
 */
import { describe, expect, it, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nProvider } from '../i18n'
import { ThemeProvider } from '../theme'
import {
  ACCENT_STORAGE_KEY,
  DENSITY_STORAGE_KEY,
  STYLE_STORAGE_KEY,
} from '../theme/themes'
import { AppearancePage } from './AppearancePage'

function renderPage() {
  return render(
    <I18nProvider>
      <ThemeProvider>
        <AppearancePage />
      </ThemeProvider>
    </I18nProvider>,
  )
}

beforeEach(() => {
  localStorage.clear()
  localStorage.setItem('forgecrm.locale', 'en')
  document.documentElement.removeAttribute('data-style')
  document.documentElement.removeAttribute('data-accent')
  document.documentElement.removeAttribute('data-density')
})

describe('AppearancePage', () => {
  it('shows all ten styles plus the default', () => {
    renderPage()
    // Ten selectable styles, each with an Apply button.
    expect(screen.getAllByRole('button', { name: 'Apply' })).toHaveLength(10)
  })

  it('applies a style to the document and persists it', async () => {
    renderPage()
    const cyberpunk = screen.getByText('Cyberpunk').closest('.style-card') as HTMLElement
    await userEvent.click(cyberpunk.querySelector('button.btn--primary') as HTMLElement)
    expect(document.documentElement.getAttribute('data-style')).toBe('cyberpunk')
    expect(localStorage.getItem(STYLE_STORAGE_KEY)).toBe('cyberpunk')
  })

  it('previews a style without persisting it, then restores on stop', async () => {
    renderPage()
    const glass = screen.getByText('Glass').closest('.style-card') as HTMLElement
    await userEvent.click(glass.querySelector('button.btn--secondary') as HTMLElement)
    // Previewed on the document...
    expect(document.documentElement.getAttribute('data-style')).toBe('glass')
    // ...but not committed to storage.
    expect(localStorage.getItem(STYLE_STORAGE_KEY)).toBeNull()

    await userEvent.click(glass.querySelector('button.btn--secondary') as HTMLElement)
    expect(document.documentElement.getAttribute('data-style')).toBe('default')
  })

  it('changes accent and density and persists both', async () => {
    renderPage()
    await userEvent.click(screen.getByRole('button', { name: 'Emerald' }))
    expect(document.documentElement.getAttribute('data-accent')).toBe('emerald')
    expect(localStorage.getItem(ACCENT_STORAGE_KEY)).toBe('emerald')

    await userEvent.click(screen.getByRole('button', { name: 'Compact' }))
    expect(document.documentElement.getAttribute('data-density')).toBe('compact')
    expect(localStorage.getItem(DENSITY_STORAGE_KEY)).toBe('compact')
  })

  it('resets every choice to its default', async () => {
    renderPage()
    await userEvent.click(screen.getByText('Brutalist').closest('.style-card')!.querySelector('button.btn--primary') as HTMLElement)
    await userEvent.click(screen.getByRole('button', { name: 'Rose' }))
    await userEvent.click(screen.getByRole('button', { name: 'Spacious' }))

    await userEvent.click(screen.getByRole('button', { name: 'Reset to defaults' }))

    expect(document.documentElement.getAttribute('data-style')).toBe('default')
    expect(document.documentElement.getAttribute('data-accent')).toBe('theme')
    expect(document.documentElement.getAttribute('data-density')).toBe('standard')
    expect(localStorage.getItem(STYLE_STORAGE_KEY)).toBe('default')
  })

  it('restores a stored style on the next mount', () => {
    localStorage.setItem(STYLE_STORAGE_KEY, 'midnight')
    renderPage()
    expect(document.documentElement.getAttribute('data-style')).toBe('midnight')
    // The Midnight card is marked current.
    expect(screen.getByText('Midnight').closest('.style-card')?.className).toContain('style-card--active')
  })
})
