import { useEffect, useState } from 'react'

/**
 * Registers the global keyboard shortcut and renders the palette when open.
 *
 * The handler ignores the shortcut while the user is typing in a field, so a
 * plain "k" inside a text input is never swallowed.
 */
export function useGlobalSearchShortcut() {
  const [open, setOpen] = useState(false)

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      const target = event.target as HTMLElement | null
      const typing =
        target?.tagName === 'INPUT' ||
        target?.tagName === 'TEXTAREA' ||
        target?.isContentEditable === true
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        setOpen((current) => !current)
        return
      }
      if (event.key === 'Escape' && !typing) {
        setOpen(false)
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [])

  return { open, setOpen }
}
