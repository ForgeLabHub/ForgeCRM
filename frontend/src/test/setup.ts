import '@testing-library/jest-dom/vitest'
import { afterEach, vi } from 'vitest'
import { cleanup } from '@testing-library/react'

// Components read the active locale, so every render needs an I18nProvider.
// Wrapping the shared render once here keeps the individual test files focused
// on behaviour instead of repeating provider boilerplate. A test that needs a
// specific locale still wraps its own provider, which wins as the inner one.
vi.mock('@testing-library/react', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@testing-library/react')>()
  const { createElement } = await import('react')
  const { I18nProvider } = await import('../i18n')
  return {
    ...actual,
    render: (ui: Parameters<typeof actual.render>[0], options?: Parameters<typeof actual.render>[1]) =>
      actual.render(ui, {
        wrapper: ({ children }) => createElement(I18nProvider, null, children),
        ...options,
      }),
  }
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})
