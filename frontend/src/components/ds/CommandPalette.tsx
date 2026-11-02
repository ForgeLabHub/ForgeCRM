/**
 * The command palette (Ctrl/Cmd+K).
 *
 * It unifies two things a keyboard-first user expects in one place: navigation
 * ("go to Contacts") and record search across the API. It is careful about
 * permission: the destination list is built from the same filtered model the
 * sidebar uses, and record search is delegated to the server, which already
 * scopes results to the caller's permissions and organization. The palette
 * never lists a section the user cannot open.
 *
 * Create actions are offered only for the sections the user can write to, and
 * they navigate to that section rather than opening a half-built form, so the
 * palette cannot start an operation the page is not ready to finish.
 */
import { useEffect, useMemo, useRef, useState } from 'react'
import type { JSX } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { searchApi } from '../../api/crm'
import { useAuth } from '../../hooks/useAuth'
import { useI18n, type TranslationKey } from '../../i18n'
import { flatNavEntries } from '../../navigation'
import type { IconProps } from '../icons'
import {
  IconArrowRight,
  IconCornerDownLeft,
  IconPlus,
  IconSearch,
} from '../icons'
import type { SearchResult } from '../../types/api'

interface Command {
  id: string
  group: 'navigate' | 'create' | 'record'
  label: string
  hint?: string
  icon: (props: IconProps) => JSX.Element
  run: () => void
}

/** Where a search result leads. Leads have no detail route yet, so they open the list. */
const DESTINATIONS: Record<string, (id: string) => string> = {
  account: (id) => `/accounts/${id}`,
  contact: (id) => `/contacts/${id}`,
  deal: (id) => `/deals/${id}`,
  lead: () => '/leads',
  ticket: (id) => `/tickets/${id}`,
  task: (id) => `/tasks/${id}`,
}

/** Sections a user may create in, mapped to the permission that guards creation. */
const CREATE_TARGETS: { to: string; permission: string; labelKey: TranslationKey }[] = [
  { to: '/contacts', permission: 'contacts.create', labelKey: 'nav.contacts' },
  { to: '/accounts', permission: 'accounts.create', labelKey: 'nav.accounts' },
  { to: '/deals', permission: 'deals.create', labelKey: 'nav.deals' },
  { to: '/tasks', permission: 'tasks.create', labelKey: 'nav.tasks' },
]

export function CommandPalette({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate()
  const { can } = useAuth()
  const { t } = useI18n()
  const [term, setTerm] = useState('')
  const [debounced, setDebounced] = useState('')
  const [active, setActive] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  useEffect(() => {
    const id = setTimeout(() => setDebounced(term.trim()), 180)
    return () => clearTimeout(id)
  }, [term])

  const enabled = debounced.length >= 2
  const results = useQuery({
    queryKey: ['palette-search', debounced],
    queryFn: () => searchApi.search(debounced, 12),
    enabled,
    staleTime: 30_000,
  })

  const commands = useMemo<Command[]>(() => {
    const query = debounced.toLowerCase()
    const nav: Command[] = flatNavEntries(can)
      .filter((entry) => {
        if (!query) return true
        const label = t(entry.labelKey).toLowerCase()
        const keywords = (entry.keywords ?? []).join(' ').toLowerCase()
        return label.includes(query) || keywords.includes(query)
      })
      .map((entry) => ({
        id: `nav:${entry.to}`,
        group: 'navigate' as const,
        label: t(entry.labelKey),
        icon: entry.icon,
        run: () => navigate(entry.to),
      }))

    const create: Command[] = query
      ? CREATE_TARGETS.filter(
          (target) => can(target.permission) && t(target.labelKey as 'nav.contacts').toLowerCase().includes(query),
        ).map((target) => ({
          id: `create:${target.to}`,
          group: 'create' as const,
          label: t(target.labelKey as 'nav.contacts'),
          hint: t('shell.paletteCreate'),
          icon: IconPlus,
          run: () => navigate(target.to),
        }))
      : []

    const records: Command[] = (results.data?.items ?? []).flatMap((result: SearchResult) => {
      const destination = DESTINATIONS[result.entity_type]
      if (!destination) return []
      const command: Command = {
        id: `record:${result.entity_type}:${result.id}`,
        group: 'record',
        label: result.title || '(untitled)',
        hint: result.subtitle || result.entity_type,
        icon: IconArrowRight,
        run: () => navigate(destination(result.id)),
      }
      return [command]
    })

    return [...nav, ...create, ...records]
  }, [debounced, can, t, navigate, results.data])

  useEffect(() => {
    setActive(0)
  }, [debounced, commands.length])

  function runActive() {
    const chosen = commands[active]
    if (chosen) {
      chosen.run()
      onClose()
    }
  }

  function onKeyDown(event: React.KeyboardEvent) {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      setActive((current) => Math.min(current + 1, commands.length - 1))
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      setActive((current) => Math.max(current - 1, 0))
    } else if (event.key === 'Enter') {
      event.preventDefault()
      runActive()
    } else if (event.key === 'Escape') {
      event.preventDefault()
      onClose()
    }
  }

  const grouped = useMemo(() => {
    const order: Command['group'][] = ['navigate', 'create', 'record']
    return order
      .map((group) => ({ group, items: commands.filter((command) => command.group === group) }))
      .filter((entry) => entry.items.length > 0)
  }, [commands])

  const groupLabel: Record<Command['group'], string> = {
    navigate: t('shell.paletteNavigate'),
    create: t('shell.paletteCreate'),
    record: t('shell.paletteRecords'),
  }

  let runningIndex = -1

  return (
    <div className="cmdk-overlay" onClick={onClose}>
      <div
        className="cmdk"
        role="dialog"
        aria-modal="true"
        aria-label={t('shell.commandPalette')}
        onClick={(event) => event.stopPropagation()}
        onKeyDown={onKeyDown}
      >
        <div className="cmdk__search">
          <IconSearch size={18} />
          <input
            ref={inputRef}
            className="cmdk__input"
            value={term}
            placeholder={t('shell.palettePlaceholder')}
            onChange={(event) => setTerm(event.target.value)}
            aria-controls="cmdk-list"
            role="combobox"
            aria-expanded={grouped.length > 0}
          />
        </div>

        <div className="cmdk__list" id="cmdk-list" role="listbox">
          {commands.length === 0 ? (
            <p className="cmdk__empty">
              {debounced ? t('shell.paletteNoResults', { term: debounced }) : t('shell.paletteHint')}
            </p>
          ) : (
            grouped.map(({ group, items }) => (
              <div key={group}>
                <div className="cmdk__group-label">{groupLabel[group]}</div>
                {items.map((command) => {
                  runningIndex += 1
                  const index = runningIndex
                  const Icon = command.icon
                  return (
                    <button
                      key={command.id}
                      type="button"
                      role="option"
                      aria-selected={index === active}
                      className={index === active ? 'cmdk__item cmdk__item--active' : 'cmdk__item'}
                      onMouseEnter={() => setActive(index)}
                      onClick={() => {
                        command.run()
                        onClose()
                      }}
                    >
                      <Icon size={16} />
                      <span className="cmdk__item-main truncate">{command.label}</span>
                      {command.hint ? (
                        <span className="cmdk__item-hint truncate">{command.hint}</span>
                      ) : null}
                    </button>
                  )
                })}
              </div>
            ))
          )}
        </div>

        <div className="cmdk__footer">
          <span>
            <kbd className="kbd">↑</kbd>
            <kbd className="kbd">↓</kbd> {t('shell.paletteFooterNavigate')}
          </span>
          <span>
            <kbd className="kbd">
              <IconCornerDownLeft size={11} />
            </kbd>{' '}
            {t('shell.paletteFooterSelect')}
          </span>
          <span>
            <kbd className="kbd">Esc</kbd> {t('shell.paletteFooterClose')}
          </span>
        </div>
      </div>
    </div>
  )
}
