/**
 * Tests for the workflow definition editor.
 *
 * The editor is a view over the JSON contract, not a second format, so the
 * assertions here are about that: a step added in the form appears in the JSON the
 * editor would send, and a definition the engine would reject is reported before the
 * save. The step types offered come from the same list the engine runs, so the
 * editor cannot offer a step that does not exist.
 */
import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { WorkflowEditor, definitionToJSON, validateDefinition } from './WorkflowEditor'
import type { WorkflowStep } from '../api/workflows'

describe('validateDefinition', () => {
  it('accepts a find_deals sweep with a foreach', () => {
    const steps: WorkflowStep[] = [
      { id: 'overdue', type: 'find_deals', args: { status: 'open' } },
      { id: 'each', type: 'foreach', do: [{ id: 't', type: 'create_task', args: { title: 'Chase' } }] },
    ]
    expect(validateDefinition(steps).ok).toBe(true)
  })

  it('rejects an empty definition', () => {
    const result = validateDefinition([])
    expect(result.ok).toBe(false)
    expect(result.errors.join(' ')).toMatch(/non-empty/)
  })

  it('rejects a duplicate step id', () => {
    const result = validateDefinition([
      { id: 'a', type: 'create_task' },
      { id: 'a', type: 'notify' },
    ])
    expect(result.ok).toBe(false)
    expect(result.errors.join(' ')).toMatch(/Duplicate/)
  })

  it('rejects a condition step with no operator', () => {
    const result = validateDefinition([{ id: 'c', type: 'condition', condition: { field: 'payload.x', op: '' } }])
    expect(result.ok).toBe(false)
    expect(result.errors.join(' ')).toMatch(/operator/)
  })

  it('rejects a delay inside a foreach, which the engine cannot resume', () => {
    const result = validateDefinition([
      { id: 'f', type: 'foreach', do: [{ id: 'd', type: 'delay', args: { minutes: 5 } }] },
    ])
    expect(result.ok).toBe(false)
    expect(result.errors.join(' ')).toMatch(/foreach body cannot contain/)
  })

  it('rejects a step type the engine does not run', () => {
    const result = validateDefinition([{ id: 'x', type: 'launch_rocket' }])
    expect(result.ok).toBe(false)
    expect(result.errors.join(' ')).toMatch(/Unknown step type/)
  })
})

describe('WorkflowEditor', () => {
  it('renders every step type the engine runs', async () => {
    render(<WorkflowEditor steps={[{ id: 's1', type: 'create_task' }]} eventTypes={[]} onChange={vi.fn()} />)
    const select = screen.getByLabelText('step type s1')
    const options = Array.from(select.querySelectorAll('option')).map((o) => o.textContent)
    expect(options).toContain('find_deals')
    expect(options).toContain('foreach')
    expect(options).toContain('branch')
  })

  it('adds a step and reports it through onChange', async () => {
    const onChange = vi.fn()
    render(<WorkflowEditor steps={[{ id: 's1', type: 'create_task' }]} eventTypes={[]} onChange={onChange} />)
    await userEvent.click(screen.getByRole('button', { name: '+ Add step' }))
    expect(onChange).toHaveBeenCalledTimes(1)
    const next = onChange.mock.calls[0][0] as WorkflowStep[]
    expect(next).toHaveLength(2)
  })

  it('removes a step', async () => {
    const onChange = vi.fn()
    render(<WorkflowEditor steps={[{ id: 's1', type: 'create_task' }]} eventTypes={[]} onChange={onChange} />)
    await userEvent.click(screen.getByRole('button', { name: 'Remove' }))
    expect((onChange.mock.calls[0][0] as WorkflowStep[]).length).toBe(0)
  })

  it('edits a condition field and operator', async () => {
    const onChange = vi.fn()
    render(
      <WorkflowEditor
        steps={[{ id: 'c1', type: 'condition', condition: { field: 'payload.status', op: 'eq', value: 'won' } }]}
        eventTypes={[]}
        onChange={onChange}
      />,
    )
    const field = screen.getByLabelText('condition field')
    await userEvent.clear(field)
    await userEvent.type(field, 'payload.priority')
    expect(onChange).toHaveBeenCalled()
  })

  it('shows the raw JSON of the definition it would send', () => {
    const json = definitionToJSON([{ id: 's1', type: 'create_task', args: { title: 'X' } }])
    expect(JSON.parse(json)).toEqual({ steps: [{ id: 's1', type: 'create_task', args: { title: 'X' } }] })
  })
})
