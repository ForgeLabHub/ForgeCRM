import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'

import { Modal } from './ui'

// The Modal owns the behaviour that is easy to get subtly wrong and impossible to
// notice by eye: where focus lands on open, that Escape dismisses, that Tab cannot
// leave the panel, and that focus returns to whatever opened it. Each of those is a
// keyboard user stranded behind the overlay if it regresses, so each is asserted
// against real DOM focus rather than the rendered markup.
function Harness() {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Open
      </button>
      <Modal
        open={open}
        onClose={() => setOpen(false)}
        title="Confirm"
        footer={
          <>
            <button type="button">Cancel</button>
            <button type="button">Delete</button>
          </>
        }
      >
        <p>Body</p>
      </Modal>
    </>
  )
}

describe('Modal', () => {
  it('renders nothing while closed', () => {
    render(<Harness />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('focuses the panel on open, not a destructive control', async () => {
    const user = userEvent.setup()
    render(<Harness />)
    await user.click(screen.getByRole('button', { name: 'Open' }))

    expect(screen.getByRole('dialog')).toHaveFocus()
  })

  it('closes on Escape', async () => {
    const user = userEvent.setup()
    render(<Harness />)
    await user.click(screen.getByRole('button', { name: 'Open' }))
    await user.keyboard('{Escape}')

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('returns focus to the opener when it closes', async () => {
    const user = userEvent.setup()
    render(<Harness />)
    const opener = screen.getByRole('button', { name: 'Open' })
    await user.click(opener)
    await user.keyboard('{Escape}')

    expect(opener).toHaveFocus()
  })

  it('wraps Tab from the last control back to the first', async () => {
    const user = userEvent.setup()
    render(<Harness />)
    await user.click(screen.getByRole('button', { name: 'Open' }))

    await user.tab() // panel -> close
    expect(screen.getByRole('button', { name: 'Close' })).toHaveFocus()
    await user.tab() // close -> Cancel
    expect(screen.getByRole('button', { name: 'Cancel' })).toHaveFocus()
    await user.tab() // Cancel -> Delete
    expect(screen.getByRole('button', { name: 'Delete' })).toHaveFocus()
    await user.tab() // Delete -> wraps back to close, not out of the dialog

    expect(screen.getByRole('button', { name: 'Close' })).toHaveFocus()
    expect(screen.getByRole('dialog')).toContainElement(document.activeElement as HTMLElement)
  })

  it('wraps Shift+Tab from the first control back to the last', async () => {
    const user = userEvent.setup()
    render(<Harness />)
    await user.click(screen.getByRole('button', { name: 'Open' }))
    await user.tab()
    expect(screen.getByRole('button', { name: 'Close' })).toHaveFocus()

    await user.tab({ shift: true })

    expect(screen.getByRole('button', { name: 'Delete' })).toHaveFocus()
  })

  it('closes when the backdrop is clicked but not when the panel is', async () => {
    const user = userEvent.setup()
    const { container } = render(<Harness />)
    await user.click(screen.getByRole('button', { name: 'Open' }))

    await user.click(screen.getByRole('dialog'))
    expect(screen.getByRole('dialog')).toBeInTheDocument()

    const backdrop = container.querySelector('.modal') as HTMLElement
    await user.click(backdrop)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})
