import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { searchApi } from '../api/crm'
import { errorMessage } from '../api/client'
import type { SearchResult } from '../types/api'

/**
 * The entity types the server can return, mapped to where a result should lead.
 *
 * Leads and tickets have no detail route yet, so a result opens the list page
 * filtered to nothing in particular rather than a page that does not exist. That
 * is a smaller gap than a link that 404s, and it is stated here so the omission
 * is visible rather than discovered by clicking.
 */
const DESTINATIONS: Record<string, (id: string) => string> = {
  account: (id) => `/accounts/${id}`,
  contact: (id) => `/contacts/${id}`,
  deal: (id) => `/deals/${id}`,
  lead: () => '/leads',
  ticket: () => '/tickets',
}

const TYPE_LABELS: Record<string, string> = {
  account: 'Account',
  contact: 'Contact',
  lead: 'Lead',
  deal: 'Deal',
  ticket: 'Ticket',
}

/**
 * Global search over the records the caller is allowed to see.
 *
 * The server filters results by the caller's permissions and organization, so an
 * empty result can mean "no match" or "no permission" and the two are not
 * distinguishable here by design: telling a user that a record exists but is
 * hidden would leak its existence.
 */
export function GlobalSearch({ onClose }: { onClose?: () => void }) {
  const navigate = useNavigate()
  const [term, setTerm] = useState('')
  const [debounced, setDebounced] = useState('')
  const [active, setActive] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  // Debounce so a keystroke does not issue a request. The server requires at
  // least two characters, so anything shorter is not sent at all.
  useEffect(() => {
    const id = setTimeout(() => setDebounced(term.trim()), 200)
    return () => clearTimeout(id)
  }, [term])

  const enabled = debounced.length >= 2
  const results = useQuery({
    queryKey: ['search', debounced],
    queryFn: () => searchApi.search(debounced, 20),
    enabled,
    staleTime: 30_000,
  })

  const items = useMemo(() => results.data?.items ?? [], [results.data])

  useEffect(() => {
    setActive(0)
  }, [debounced])

  function open(result: SearchResult) {
    const destination = DESTINATIONS[result.entity_type]
    if (!destination) return
    navigate(destination(result.id))
    onClose?.()
  }

  function onKeyDown(event: React.KeyboardEvent) {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      setActive((current) => Math.min(current + 1, items.length - 1))
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      setActive((current) => Math.max(current - 1, 0))
    } else if (event.key === 'Enter') {
      const chosen = items[active]
      if (chosen) {
        event.preventDefault()
        open(chosen)
      }
    } else if (event.key === 'Escape') {
      onClose?.()
    }
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Global search"
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(16, 24, 40, 0.45)',
        zIndex: 70,
        padding: 'var(--space-4)',
        paddingTop: '12vh',
      }}
      onClick={onClose}
    >
      <div
        className="card"
        style={{ maxWidth: 620, width: '100%', margin: '0 auto', padding: 0 }}
        onClick={(event) => event.stopPropagation()}
      >
        <div style={{ padding: 'var(--space-3)', borderBottom: '1px solid var(--color-border)' }}>
          <label className="field" style={{ margin: 0 }}>
            <span className="sr-only">Search accounts, contacts, leads, deals and tickets</span>
            <input
              ref={inputRef}
              type="text"
              placeholder="Search accounts, contacts, leads, deals, tickets…"
              value={term}
              onChange={(event) => setTerm(event.target.value)}
              onKeyDown={onKeyDown}
              aria-controls="global-search-results"
              aria-activedescendant={items[active] ? `search-result-${active}` : undefined}
              role="combobox"
              aria-expanded={enabled}
            />
          </label>
        </div>

        <div id="global-search-results" role="listbox" style={{ maxHeight: 380, overflowY: 'auto' }}>
          {!enabled ? (
            <p className="text-muted text-sm" style={{ padding: 'var(--space-4)' }}>
              Type at least two characters. Results are limited to the records your permissions
              allow, so a record you cannot open does not appear.
            </p>
          ) : results.isLoading ? (
            <p className="text-muted text-sm" style={{ padding: 'var(--space-4)' }}>
              Searching…
            </p>
          ) : results.isError ? (
            <p className="text-sm" style={{ padding: 'var(--space-4)', color: 'var(--color-danger)' }}>
              {errorMessage(results.error)}
            </p>
          ) : items.length === 0 ? (
            <p className="text-muted text-sm" style={{ padding: 'var(--space-4)' }}>
              Nothing matched “{debounced}”. Either no record matches, or none of the matches is
              available to you.
            </p>
          ) : (
            items.map((result, index) => (
              <button
                key={`${result.entity_type}-${result.id}`}
                id={`search-result-${index}`}
                role="option"
                aria-selected={index === active}
                type="button"
                onMouseEnter={() => setActive(index)}
                onClick={() => open(result)}
                style={{
                  display: 'block',
                  width: '100%',
                  textAlign: 'left',
                  padding: 'var(--space-2) var(--space-3)',
                  background: index === active ? 'var(--color-accent-soft)' : 'transparent',
                  border: 'none',
                  borderBottom: '1px solid var(--color-border)',
                  cursor: 'pointer',
                }}
              >
                <span className="text-subtle text-sm" style={{ marginRight: 8 }}>
                  {TYPE_LABELS[result.entity_type] ?? result.entity_type}
                </span>
                <span style={{ fontWeight: 500 }}>{result.title || '(no title)'}</span>
                {result.subtitle ? (
                  <span className="text-muted text-sm" style={{ marginLeft: 8 }}>
                    {result.subtitle}
                  </span>
                ) : null}
              </button>
            ))
          )}
        </div>
      </div>
    </div>
  )
}
