/**
 * The `?new=1` create-dialog contract.
 *
 * The overview quick actions link to a list page with `?new=1` so the user lands
 * with the create form already open. Two properties matter and neither is
 * visible from the hook's return type: the parameter is consumed exactly once
 * (a refresh or a back navigation must not reopen the dialog), and a dialog the
 * user then closes stays closed while the page is mounted.
 */
import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { useCreateParam } from './useCreateParam'

function Probe({ enabled = true }: { enabled?: boolean }) {
  const [open, setOpen] = useCreateParam(enabled)
  const location = useLocation()
  return (
    <div>
      <span data-testid="open">{String(open)}</span>
      <span data-testid="search">{location.search}</span>
      <button type="button" onClick={() => setOpen(true)}>
        open
      </button>
      <button type="button" onClick={() => setOpen(false)}>
        close
      </button>
    </div>
  )
}

function renderAt(path: string, enabled = true) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Probe enabled={enabled} />
    </MemoryRouter>,
  )
}

describe('useCreateParam', () => {
  it('opens the dialog when reached with ?new=1', () => {
    renderAt('/deals?new=1')
    expect(screen.getByTestId('open').textContent).toBe('true')
  })

  it('clears the parameter once consumed', () => {
    renderAt('/deals?new=1')
    expect(screen.getByTestId('search').textContent).toBe('')
  })

  it('keeps unrelated query parameters when clearing the flag', () => {
    renderAt('/deals?status=open&new=1')
    const search = screen.getByTestId('search').textContent ?? ''
    expect(search).toContain('status=open')
    expect(search).not.toContain('new=1')
  })

  it('stays closed when reached without the parameter', () => {
    renderAt('/deals')
    expect(screen.getByTestId('open').textContent).toBe('false')
  })

  it('stays closed after the user closes the dialog', async () => {
    renderAt('/deals?new=1')
    expect(screen.getByTestId('open').textContent).toBe('true')
    await userEvent.click(screen.getByRole('button', { name: 'close' }))
    expect(screen.getByTestId('open').textContent).toBe('false')
  })

  it('opens from a local toggle as well as the parameter', async () => {
    renderAt('/deals')
    await userEvent.click(screen.getByRole('button', { name: 'open' }))
    expect(screen.getByTestId('open').textContent).toBe('true')
  })

  it('does not open for a user who may not create', () => {
    renderAt('/deals?new=1', false)
    expect(screen.getByTestId('open').textContent).toBe('false')
  })

  it('opens once the create permission arrives after mount', () => {
    const { rerender } = render(
      <MemoryRouter initialEntries={['/deals?new=1']}>
        <Probe enabled={false} />
      </MemoryRouter>,
    )
    expect(screen.getByTestId('open').textContent).toBe('false')

    rerender(
      <MemoryRouter initialEntries={['/deals?new=1']}>
        <Probe enabled />
      </MemoryRouter>,
    )
    expect(screen.getByTestId('open').textContent).toBe('true')
  })
})
