/**
 * Overlays and floating surfaces: dropdown menu, tooltip, toast and drawer.
 *
 * Each owns the interaction that is easy to get subtly wrong — closing on an
 * outside click or Escape, keeping focus where a keyboard user expects it — so
 * a page does not have to re-implement it per feature.
 */
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import type { ReactNode } from 'react'
import {
  IconAlertTriangle,
  IconCheckCircle,
  IconClose,
  IconInfo,
  IconXCircle,
} from '../icons'

/* ------------------------------------------------------------- dropdown -- */

export interface MenuItem {
  label: string
  onSelect: () => void
  icon?: ReactNode
  danger?: boolean
  disabled?: boolean
}

export function DropdownMenu({
  trigger,
  items,
  align = 'end',
}: {
  trigger: (props: { open: boolean; toggle: () => void }) => ReactNode
  items: MenuItem[]
  align?: 'start' | 'end'
}) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onPointerDown = (event: MouseEvent) => {
      if (ref.current && !ref.current.contains(event.target as Node)) setOpen(false)
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [open])

  return (
    <div className="menu" ref={ref}>
      {trigger({ open, toggle: () => setOpen((value) => !value) })}
      {open ? (
        <div
          className="menu__popover"
          role="menu"
          style={align === 'start' ? { left: 0, right: 'auto' } : undefined}
        >
          {items.map((item) => (
            <button
              key={item.label}
              type="button"
              role="menuitem"
              disabled={item.disabled}
              className={item.danger ? 'menu__item menu__item--danger' : 'menu__item'}
              onClick={() => {
                setOpen(false)
                item.onSelect()
              }}
            >
              {item.icon}
              {item.label}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  )
}

/* -------------------------------------------------------------- tooltip -- */

export function Tooltip({ label, children }: { label: string; children: ReactNode }) {
  return (
    <span className="tooltip">
      {children}
      <span className="tooltip__bubble" role="tooltip">
        {label}
      </span>
    </span>
  )
}

/* ---------------------------------------------------------------- toast -- */

type ToastTone = 'success' | 'error' | 'warning' | 'info'

interface ToastItem {
  id: number
  tone: ToastTone
  message: string
}

interface ToastValue {
  notify: (tone: ToastTone, message: string) => void
}

const ToastContext = createContext<ToastValue | null>(null)

const TOAST_ICON: Record<ToastTone, ReactNode> = {
  success: <IconCheckCircle size={17} />,
  error: <IconXCircle size={17} />,
  warning: <IconAlertTriangle size={17} />,
  info: <IconInfo size={17} />,
}

/**
 * A stack of transient confirmations. Toasts are supplementary: every action
 * that matters also reflects its result in the page itself, so a user who
 * misses a toast is not left believing an operation failed.
 */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([])
  const counter = useRef(0)

  const dismiss = useCallback((id: number) => {
    setItems((current) => current.filter((item) => item.id !== id))
  }, [])

  const notify = useCallback(
    (tone: ToastTone, message: string) => {
      const id = (counter.current += 1)
      setItems((current) => [...current, { id, tone, message }])
      window.setTimeout(() => dismiss(id), 5000)
    },
    [dismiss],
  )

  const value = useMemo<ToastValue>(() => ({ notify }), [notify])

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div className="toast-region" role="region" aria-label="Notifications">
        {items.map((item) => (
          <div key={item.id} className={`toast toast--${item.tone}`} role="status">
            <span className="toast__icon">{TOAST_ICON[item.tone]}</span>
            <span className="toast__text">{item.message}</span>
            <button
              type="button"
              className="toast__close"
              aria-label="Dismiss"
              onClick={() => dismiss(item.id)}
            >
              <IconClose size={14} />
            </button>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}

export function useToast(): ToastValue {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error('useToast must be used inside a ToastProvider')
  return ctx
}

/* --------------------------------------------------------------- drawer -- */

export function Drawer({
  open,
  onClose,
  title,
  footer,
  children,
}: {
  open: boolean
  onClose: () => void
  title: string
  footer?: ReactNode
  children: ReactNode
}) {
  const panelRef = useRef<HTMLDivElement>(null)
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose

  useEffect(() => {
    if (!open) return
    const previouslyFocused = document.activeElement as HTMLElement | null
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onCloseRef.current()
    }
    document.addEventListener('keydown', onKeyDown)
    panelRef.current?.focus()
    return () => {
      document.removeEventListener('keydown', onKeyDown)
      previouslyFocused?.focus?.()
    }
  }, [open])

  if (!open) return null

  return (
    <>
      <div className="drawer-overlay" onClick={onClose} />
      <div
        ref={panelRef}
        className="drawer"
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
      >
        <div className="drawer__header">
          <h2 className="drawer__title">{title}</h2>
          <button type="button" className="modal__close" aria-label="Close" onClick={onClose}>
            <IconClose size={18} />
          </button>
        </div>
        <div className="drawer__body">{children}</div>
        {footer ? <div className="drawer__footer">{footer}</div> : null}
      </div>
    </>
  )
}
