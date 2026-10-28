import { describe, expect, it } from 'vitest'

import { statusTone } from './ui'

// The dead state is the one a webhook operator has to act on, so it must be
// visually distinct from an ordinary failed attempt. A regression here would let a
// permanently broken subscription look like a transient one in the deliveries
// table, which is exactly the failure this suite exists to prevent.
describe('statusTone', () => {
  it('marks a dead webhook delivery as a terminal failure', () => {
    expect(statusTone('dead')).toBe('danger')
  })

  it('still distinguishes a delivered delivery from a dead one', () => {
    expect(statusTone('delivered')).toBe('success')
    expect(statusTone('dead')).not.toBe(statusTone('delivered'))
  })
})
