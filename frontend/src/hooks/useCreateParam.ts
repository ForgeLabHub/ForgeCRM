import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

/**
 * Opens a page's create dialog when it is reached with `?new=1`.
 *
 * The overview quick actions link here, so a user who chose "New deal" lands
 * with the deal form already open rather than on a list they must click through
 * again. The parameter is cleared once consumed, so a refresh or a back
 * navigation does not reopen the dialog.
 *
 * `enabled` is the caller's create permission. The URL alone must not open a
 * form the user cannot submit: a read-only user who follows a deep link — or a
 * bookmark made while their role allowed creating — would otherwise be handed a
 * dialog whose save the server rejects. The permission is read from the session,
 * which resolves after mount, so the dialog opens once `enabled` turns true
 * rather than being lost while the session loads.
 */
export function useCreateParam(enabled = true): [boolean, (open: boolean) => void] {
  const [params, setParams] = useSearchParams()
  const [open, setOpen] = useState(() => enabled && params.get('new') === '1')

  useEffect(() => {
    if (!enabled) return
    if (params.get('new') !== '1') return
    setOpen(true)
    const next = new URLSearchParams(params)
    next.delete('new')
    setParams(next, { replace: true })
  }, [enabled, params, setParams])

  return [open, setOpen]
}
