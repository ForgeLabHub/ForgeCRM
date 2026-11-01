/**
 * The saved-views switcher for a CRM list screen.
 *
 * It is the only interface to the saved-view endpoints. The component owns no
 * filter state: the screen passes the filters it is currently showing and
 * receives back the filters of a view the user picked, so a view is applied by
 * the same code path that a hand-entered filter would take. That keeps one
 * definition of what a filter means and means the list cannot show something the
 * view does not describe.
 */
import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { errorMessage } from '../api/client'
import { viewsApi } from '../api/views'
import type { SavedView, SavedViewInput, ViewEntityType, ViewFilters } from '../api/views'
import { useAuth } from '../hooks/useAuth'
import { Alert, ConfirmDialog, Field } from './ui'

interface SavedViewsBarProps {
  entityType: ViewEntityType
  /** The filters the screen is currently showing. */
  current: ViewFilters
  /** Applies a view's filters to the screen. */
  onApply: (filters: ViewFilters) => void
  /** The id of the view currently applied, if any. */
  activeId?: string
  /** Reports which view is now applied, so the screen can keep highlighting it. */
  onActiveChange: (id: string | undefined) => void
}

/** Compares two filter documents by their meaningful fields. */
function sameFilters(a: ViewFilters, b: ViewFilters): boolean {
  return JSON.stringify(normalise(a)) === JSON.stringify(normalise(b))
}

function normalise(filters: ViewFilters): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(filters)) {
    if (value === undefined || value === null || value === '' || value === false) continue
    if (Array.isArray(value) && value.length === 0) continue
    out[key] = value
  }
  return out
}

export function SavedViewsBar({
  entityType,
  current,
  onApply,
  activeId,
  onActiveChange,
}: SavedViewsBarProps) {
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState<SavedView | null>(null)

  const mayManage = can('views.manage')

  const query = useQuery({
    queryKey: ['views', entityType],
    queryFn: () => viewsApi.list(entityType),
  })

  const items = query.data?.items ?? []
  const active = items.find((view) => view.id === activeId)

  // "Modified" means the screen no longer matches the view it came from, so the
  // user can tell that saving would overwrite a view rather than create one.
  const modified = active ? !sameFilters(active.filters, current) : false

  function select(id: string) {
    if (!id) {
      onActiveChange(undefined)
      onApply({})
      return
    }
    const view = items.find((item) => item.id === id)
    if (!view) return
    onActiveChange(view.id)
    onApply(view.filters)
  }

  return (
    <div className="saved-views" role="group" aria-label="Saved views">
      {query.isError ? (
        <Alert tone="error">{errorMessage(query.error)}</Alert>
      ) : (
        <>
          <label className="field saved-views__select">
            <span className="field__label">View</span>
            <select
              value={activeId ?? ''}
              onChange={(event) => select(event.target.value)}
              disabled={query.isLoading}
            >
              <option value="">All records</option>
              {items.map((view) => (
                <option key={view.id} value={view.id}>
                  {view.name}
                  {view.shared ? ' (shared)' : ''}
                  {view.pinned ? ' ★' : ''}
                </option>
              ))}
            </select>
          </label>

          {active && !active.is_mine ? (
            <span className="text-subtle text-sm" title="A shared view may be applied but only its owner may change it">
              shared view
            </span>
          ) : null}

          {modified ? <span className="text-sm text-subtle">modified</span> : null}

          {mayManage ? (
            <>
              <button
                type="button"
                className="btn btn--secondary btn--sm"
                onClick={() => setSaving(true)}
              >
                {active ? 'Save as new' : 'Save view'}
              </button>
              {active && active.is_mine ? (
                <button
                  type="button"
                  className="btn btn--ghost btn--sm"
                  onClick={() => setDeleting(active)}
                >
                  Delete
                </button>
              ) : null}
            </>
          ) : null}
        </>
      )}

      <SaveViewDialog
        open={saving}
        entityType={entityType}
        filters={current}
        onClose={() => setSaving(false)}
        onSaved={(view) => {
          setSaving(false)
          void queryClient.invalidateQueries({ queryKey: ['views', entityType] })
          onActiveChange(view.id)
        }}
      />

      <ConfirmDialog
        open={deleting !== null}
        title="Delete this view?"
        text={`"${deleting?.name ?? ''}" will be removed from your views. The records it showed are not affected.`}
        confirmLabel="Delete"
        danger
        onCancel={() => setDeleting(null)}
        onConfirm={() => {
          const view = deleting
          setDeleting(null)
          if (!view) return
          void viewsApi.remove(view.id).then(() => {
            void queryClient.invalidateQueries({ queryKey: ['views', entityType] })
            if (activeId === view.id) onActiveChange(undefined)
          })
        }}
      />
    </div>
  )
}

function SaveViewDialog({
  open,
  entityType,
  filters,
  onClose,
  onSaved,
}: {
  open: boolean
  entityType: ViewEntityType
  filters: ViewFilters
  onClose: () => void
  onSaved: (view: SavedView) => void
}) {
  const [name, setName] = useState('')
  const [shared, setShared] = useState(false)
  const [pinned, setPinned] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const mutation = useMutation({
    mutationFn: (input: SavedViewInput) => viewsApi.create(input),
    onSuccess: (view) => {
      setName('')
      setShared(false)
      setPinned(false)
      setError(null)
      onSaved(view)
    },
    onError: (err) => setError(errorMessage(err)),
  })

  function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setError(null)
    if (!name.trim()) {
      setError('A name is required.')
      return
    }
    mutation.mutate({ entity_type: entityType, name: name.trim(), shared, pinned, filters })
  }

  if (!open) return null

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="save-view-title"
      style={{
        position: 'fixed',
        inset: 0,
        display: 'grid',
        placeItems: 'center',
        background: 'var(--color-overlay)',
        zIndex: 60,
        padding: 'var(--space-4)',
      }}
      onClick={onClose}
    >
      <form
        className="card"
        style={{ maxWidth: 460, width: '100%' }}
        onClick={(event) => event.stopPropagation()}
        onSubmit={handleSubmit}
      >
        <div className="card__header">
          <h2 id="save-view-title">Save this view</h2>
        </div>
        <div className="card__body">
          {error ? <Alert tone="error">{error}</Alert> : null}
          <Field label="Name" htmlFor="view-name" required>
            <input
              id="view-name"
              value={name}
              onChange={(event) => setName(event.target.value)}
              autoFocus
              maxLength={100}
            />
          </Field>
          <label className="row" style={{ gap: 'var(--space-2)' }}>
            <input
              type="checkbox"
              checked={shared}
              onChange={(event) => setShared(event.target.checked)}
            />
            <span>Share with my organization</span>
          </label>
          <label className="row" style={{ gap: 'var(--space-2)' }}>
            <input
              type="checkbox"
              checked={pinned}
              onChange={(event) => setPinned(event.target.checked)}
            />
            <span>Pin to the top</span>
          </label>
        </div>
        <div className="form-actions" style={{ margin: 0, padding: 'var(--space-4)' }}>
          <button type="button" className="btn btn--secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="btn btn--primary" disabled={mutation.isPending}>
            {mutation.isPending ? 'Saving…' : 'Save'}
          </button>
        </div>
      </form>
    </div>
  )
}
