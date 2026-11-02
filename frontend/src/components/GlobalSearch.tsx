import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { searchApi } from '../api/crm'
import { errorMessage } from '../api/client'
import { useT } from '../i18n'
import { Modal } from './ui'
import type { SearchResult } from '../types/api'

/**
 * The entity types the server can return, mapped to where a result should lead.
 *
 * Leads have no detail route yet, so a result opens the list page rather than a
 * page that does not exist. That is a smaller gap than a link that 404s, and it
 * is stated here so the omission is visible rather than discovered by clicking.
 */
const DESTINATIONS: Record<string, (id: string) => string> = {
  account: (id) => `/accounts/${id}`,
  contact: (id) => `/contacts/${id}`,
  deal: (id) => `/deals/${id}`,
  lead: () => '/leads',
  ticket: (id) => `/tickets/${id}`,
  task: (id) => `/tasks/${id}`,
}

const TYPE_LABEL_KEYS = {
  account: 'search.types.account',
  contact: 'search.types.contact',
  lead: 'search.types.lead',
  deal: 'search.types.deal',
  ticket: 'search.types.ticket',
  task: 'search.types.task',
} as const

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
  const t = useT()
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
    <Modal open onClose={() => onClose?.()} title={t('search.label')} size="md">
      <div className="search-palette">
        <div className="search-palette__field">
          <label className="field" style={{ margin: 0 }}>
            <span className="sr-only">{t('search.ariaLabel')}</span>
            <input
              ref={inputRef}
              type="text"
              placeholder={t('search.placeholder')}
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

        <div id="global-search-results" role="listbox" className="search-palette__results">
          {!enabled ? (
            <p className="text-muted text-sm" style={{ padding: 'var(--space-4)' }}>
              {t('search.typeHint')} {t('search.permissionHint')}
            </p>
          ) : results.isLoading ? (
            <p className="text-muted text-sm" style={{ padding: 'var(--space-4)' }}>
              {t('search.searching')}
            </p>
          ) : results.isError ? (
            <p className="text-sm" style={{ padding: 'var(--space-4)', color: 'var(--color-danger)' }}>
              {errorMessage(results.error)}
            </p>
          ) : items.length === 0 ? (
            <p className="text-muted text-sm" style={{ padding: 'var(--space-4)' }}>
              {t('search.empty', { term: debounced })} {t('search.emptyHint')}
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
                className="search-palette__result"
                data-active={index === active}
              >
                <span className="text-subtle text-sm" style={{ marginRight: 8 }}>
                  {result.entity_type in TYPE_LABEL_KEYS
                    ? t(TYPE_LABEL_KEYS[result.entity_type as keyof typeof TYPE_LABEL_KEYS])
                    : result.entity_type}
                </span>
                <span style={{ fontWeight: 500 }}>{result.title || t('search.noTitle')}</span>
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
    </Modal>
  )
}
