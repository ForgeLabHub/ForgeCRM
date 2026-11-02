import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { Breadcrumb } from './ui'

// The breadcrumb is the only way back to the list a detail view was opened from:
// the "back" button it replaced was an action, so it could be removed without the
// reader noticing the trail was gone. These assertions pin the two properties that
// make the trail usable — the parent is a real link, and the current page is not.
describe('Breadcrumb', () => {
  it('links the ancestor and leaves the current record as plain text', () => {
    render(
      <MemoryRouter>
        <Breadcrumb items={[{ label: 'Accounts', to: '/accounts' }, { label: 'Acme' }]} />
      </MemoryRouter>,
    )

    const link = screen.getByRole('link', { name: 'Accounts' })
    expect(link).toHaveAttribute('href', '/accounts')

    // The last item has no `to`, so it must not be a link to the page you are on.
    expect(screen.queryByRole('link', { name: 'Acme' })).toBeNull()
    expect(screen.getByText('Acme')).toHaveAttribute('aria-current', 'page')
  })

  it('exposes the trail as a labelled navigation landmark', () => {
    render(
      <MemoryRouter>
        <Breadcrumb items={[{ label: 'Deals', to: '/deals' }]} />
      </MemoryRouter>,
    )

    expect(screen.getByRole('navigation', { name: 'Breadcrumb' })).toBeInTheDocument()
  })
})
