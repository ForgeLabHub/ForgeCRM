/** Small presentational primitives shared by every page. */
import { useEffect, useId, useRef } from 'react'
import type { ReactNode } from 'react'
import { IconClose } from './icons'

export function Badge({
  children,
  tone = 'neutral',
}: {
  children: ReactNode
  tone?: 'neutral' | 'success' | 'danger' | 'warning' | 'accent'
}) {
  const cls = tone === 'neutral' ? 'badge' : `badge badge--${tone}`
  return <span className={cls}>{children}</span>
}

/** Maps a status string to a badge tone. Shared so colours stay consistent. */
export function statusTone(status: string): 'neutral' | 'success' | 'danger' | 'warning' | 'accent' {
  switch (status) {
    case 'active':
    case 'completed':
    case 'succeeded':
    case 'delivered':
    case 'won':
    case 'resolved':
    case 'closed':
    case 'healthy':
    case 'ok':
      return 'success'
    // A dead webhook delivery is a terminal failure, not a neutral state: it is
    // the one an operator must act on, so it must not look like an ordinary row.
    case 'failed':
    case 'error':
    case 'revoked':
    case 'blocked':
    case 'lost':
    case 'breached':
    case 'dead':
      return 'danger'
    case 'pending':
    case 'running':
    case 'in_progress':
    case 'queued':
    case 'analyzing':
    case 'analyzed':
    case 'validating':
    case 'paused':
    case 'warning':
    case 'retrying':
      return 'warning'
    case 'open':
    case 'new':
      return 'accent'
    default:
      return 'neutral'
  }
}

export function StatusBadge({ status }: { status: string }) {
  return <Badge tone={statusTone(status)}>{status.replace(/_/g, ' ')}</Badge>
}

export function PageHeader({
  title,
  meta,
  actions,
}: {
  title: string
  meta?: ReactNode
  actions?: ReactNode
}) {
  return (
    <header className="page-header">
      <div>
        <h1>{title}</h1>
        {meta ? <div className="page-header__meta">{meta}</div> : null}
      </div>
      {actions ? <div className="page-header__actions">{actions}</div> : null}
    </header>
  )
}

export function Card({
  title,
  actions,
  children,
  flush = false,
}: {
  title?: string
  actions?: ReactNode
  children: ReactNode
  flush?: boolean
}) {
  return (
    <section className="card">
      {title || actions ? (
        <div className="card__header">
          {title ? <h2>{title}</h2> : <span />}
          {actions}
        </div>
      ) : null}
      <div className={flush ? 'card__body card__body--flush' : 'card__body'}>{children}</div>
    </section>
  )
}

export function Stat({
  label,
  value,
  hint,
}: {
  label: string
  value: ReactNode
  hint?: ReactNode
}) {
  return (
    <div className="stat">
      <div className="stat__label">{label}</div>
      <div className="stat__value">{value}</div>
      {hint ? <div className="stat__hint">{hint}</div> : null}
    </div>
  )
}

export function EmptyState({
  title,
  text,
  action,
}: {
  title: string
  text?: string
  action?: ReactNode
}) {
  return (
    <div className="empty-state">
      <div className="empty-state__title">{title}</div>
      {text ? <p className="empty-state__text">{text}</p> : null}
      {action}
    </div>
  )
}

export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="error-state" role="alert">
      <div className="error-state__title">Something went wrong</div>
      <p className="error-state__text">{message}</p>
      {onRetry ? (
        <button type="button" className="btn btn--secondary" onClick={onRetry}>
          Try again
        </button>
      ) : null}
    </div>
  )
}

export function Alert({
  tone,
  children,
}: {
  tone: 'error' | 'success' | 'warning' | 'info'
  children: ReactNode
}) {
  return (
    <div className={`alert alert--${tone}`} role={tone === 'error' ? 'alert' : 'status'}>
      {children}
    </div>
  )
}

/** A shimmering placeholder with the shape of the content it replaces. */
export function Skeleton({ width = '100%', height = 16 }: { width?: string; height?: number }) {
  return <div className="skeleton" style={{ width, height }} aria-hidden="true" />
}

/** An inline busy indicator for a region that is loading but already has a frame. */
export function Spinner({ label }: { label?: string }) {
  return (
    <div className="loading-block" role="status">
      <span className="spinner" aria-hidden="true" />
      {label ? <span>{label}</span> : null}
    </div>
  )
}

/**
 * A round identity mark: the user's initials on the accent colour.
 *
 * The initials come from the display name, falling back to the email's first
 * character, so the mark is never blank. It is decorative — the full name is
 * always rendered as text beside it — so it is hidden from assistive technology.
 */
export function Avatar({
  name,
  email,
  size = 'md',
}: {
  name?: string | null
  email?: string | null
  size?: 'sm' | 'md' | 'lg'
}) {
  const source = (name ?? '').trim() || (email ?? '').trim()
  const initials = source
    .split(/[\s@._-]+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase() ?? '')
    .join('')
  return (
    <span className={`avatar avatar--${size}`} aria-hidden="true">
      {initials || '?'}
    </span>
  )
}

export function TableSkeleton({ rows = 5, columns = 4 }: { rows?: number; columns?: number }) {
  return (
    <div className="card__body stack" aria-busy="true" aria-label="Loading">
      {Array.from({ length: rows }, (_, rowIndex) => (
        <div key={rowIndex} className="row">
          {Array.from({ length: columns }, (_, colIndex) => (
            <Skeleton key={colIndex} width={colIndex === 0 ? '30%' : '16%'} />
          ))}
        </div>
      ))}
    </div>
  )
}

export function Field({
  label,
  htmlFor,
  error,
  hint,
  required,
  children,
  full = false,
}: {
  label: string
  htmlFor?: string
  error?: string
  hint?: string
  required?: boolean
  children: ReactNode
  full?: boolean
}) {
  return (
    <div className={full ? 'field form-grid__full' : 'field'}>
      <label className="field__label" htmlFor={htmlFor}>
        {label}
        {required ? <span className="field__required" aria-hidden="true"> *</span> : null}
      </label>
      {children}
      {hint && !error ? <span className="field__hint">{hint}</span> : null}
      {error ? (
        <span className="field__error" role="alert">
          {error}
        </span>
      ) : null}
    </div>
  )
}

/** Confirmation dialog for operations that cannot be undone. */
export function ConfirmDialog({
  open,
  title,
  text,
  confirmLabel = 'Confirm',
  danger = false,
  busy = false,
  onConfirm,
  onCancel,
}: {
  open: boolean
  title: string
  text: string
  confirmLabel?: string
  danger?: boolean
  busy?: boolean
  onConfirm: () => void
  onCancel: () => void
}) {
  return (
    <Modal
      open={open}
      onClose={onCancel}
      title={title}
      size="sm"
      footer={
        <>
          <button type="button" className="btn btn--secondary" onClick={onCancel} disabled={busy}>
            Cancel
          </button>
          <button
            type="button"
            className={danger ? 'btn btn--danger' : 'btn btn--primary'}
            onClick={onConfirm}
            disabled={busy}
          >
            {busy ? 'Working…' : confirmLabel}
          </button>
        </>
      }
    >
      <p className="text-muted">{text}</p>
    </Modal>
  )
}

/**
 * The one modal shell every dialog is built on.
 *
 * It owns the parts that are easy to get wrong once and repeat: the overlay,
 * Escape to dismiss, a click on the backdrop, and a focus trap. The trap moves
 * focus into the panel on open, keeps Tab inside it while it is open, and
 * returns focus to whatever opened it on close — so a keyboard user is never
 * stranded behind the overlay.
 */
export function Modal({
  open,
  onClose,
  title,
  description,
  size = 'md',
  footer,
  children,
}: {
  open: boolean
  onClose: () => void
  title: string
  description?: string
  size?: 'sm' | 'md' | 'lg'
  footer?: ReactNode
  children: ReactNode
}) {
  const panelRef = useRef<HTMLDivElement>(null)
  const titleId = useId()
  // Kept in a ref so the open/close effect depends only on `open`: a parent that
  // passes an inline onClose would otherwise re-run the effect on every render,
  // yanking focus back to the first control on each keystroke.
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose

  useEffect(() => {
    if (!open) return
    const previouslyFocused = document.activeElement as HTMLElement | null

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.stopPropagation()
        onCloseRef.current()
        return
      }
      if (event.key !== 'Tab' || !panelRef.current) return
      const focusable = panelRef.current.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])',
      )
      if (focusable.length === 0) return
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }

    document.addEventListener('keydown', onKeyDown, true)
    // Focus the panel, not the first control: a confirm dialog must not put the
    // pointer on a destructive button, where a stray Enter would fire it.
    panelRef.current?.focus()

    return () => {
      document.removeEventListener('keydown', onKeyDown, true)
      previouslyFocused?.focus?.()
    }
  }, [open])

  if (!open) return null

  return (
    <div className="modal" onClick={onClose}>
      <div
        ref={panelRef}
        className={`modal__panel modal__panel--${size}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        onClick={(event) => event.stopPropagation()}
      >
        <div className="modal__header">
          <div>
            <h2 id={titleId} className="modal__title">
              {title}
            </h2>
            {description ? <p className="modal__description">{description}</p> : null}
          </div>
          <button type="button" className="modal__close" aria-label="Close" onClick={onClose}>
            <IconClose size={18} />
          </button>
        </div>
        <div className="modal__body">{children}</div>
        {footer ? <div className="modal__footer">{footer}</div> : null}
      </div>
    </div>
  )
}

export function Pagination({
  total,
  limit,
  offset,
  onChange,
}: {
  total: number
  limit: number
  offset: number
  onChange: (offset: number) => void
}) {
  const first = total === 0 ? 0 : offset + 1
  const last = Math.min(offset + limit, total)
  const hasPrevious = offset > 0
  const hasNext = offset + limit < total

  return (
    <div className="pagination">
      <span>
        {total === 0 ? 'No results' : `${first}–${last} of ${total}`}
      </span>
      <div className="pagination__controls">
        <button
          type="button"
          className="btn btn--secondary btn--sm"
          disabled={!hasPrevious}
          onClick={() => onChange(Math.max(0, offset - limit))}
        >
          Previous
        </button>
        <button
          type="button"
          className="btn btn--secondary btn--sm"
          disabled={!hasNext}
          onClick={() => onChange(offset + limit)}
        >
          Next
        </button>
      </div>
    </div>
  )
}

/** Renders a timestamp in the viewer's locale, or a dash when absent. */
export function Timestamp({ value, withTime = true }: { value?: string | null; withTime?: boolean }) {
  if (!value) return <span className="text-subtle">—</span>
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return <span className="text-subtle">—</span>
  return (
    <time dateTime={value} title={date.toISOString()}>
      {date.toLocaleString(undefined, {
        year: 'numeric',
        month: 'short',
        day: 'numeric',
        ...(withTime ? { hour: '2-digit', minute: '2-digit' } : {}),
      })}
    </time>
  )
}

export function formatMoney(amount: number, currency = 'USD'): string {
  try {
    return new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency,
      maximumFractionDigits: 2,
    }).format(amount)
  } catch {
    return `${amount.toFixed(2)} ${currency}`
  }
}

export function formatBytes(bytes?: number): string {
  if (bytes === undefined || bytes === null) return '—'
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let value = bytes / 1024
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value.toFixed(1)} ${units[unit]}`
}
