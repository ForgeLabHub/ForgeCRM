/**
 * Custom fields for one CRM record.
 *
 * The server lets a tenant define extra fields per entity type and stores the
 * value for each record in that record's own `custom_fields` map. Every part
 * existed except a consumer: definitions could be created and listed over the
 * API, records already carried the values, and the edit dialog passed them back
 * through untouched — but nothing rendered them, so a field a tenant defined was
 * invisible and uneditable in the product.
 *
 * Values live on the record, so saving them is the record's own update: this
 * panel hands the merged map back to the page, which owns the mutation and its
 * permission. Managing the definitions themselves is a separate permission
 * (`custom_fields.manage`) and a separate request.
 */
import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { customFieldsApi } from '../api/crm'
import { errorMessage } from '../api/client'
import type { CustomFieldDefinition, CustomFieldType, CustomFields } from '../types/api'
import { useAuth } from '../hooks/useAuth'
import { Alert, Card, Skeleton } from './ui'

export type CustomFieldEntityType = 'account' | 'contact' | 'lead' | 'deal'

interface CustomFieldsPanelProps {
  entityType: CustomFieldEntityType
  /** The record's current `custom_fields` map. */
  values: CustomFields
  /** Whether the caller holds the record's update permission. */
  canEdit: boolean
  saving?: boolean
  /** Persists the merged map. Rejects on failure so the panel can show why. */
  onSave: (next: CustomFields) => Promise<unknown>
}

export function CustomFieldsPanel({
  entityType,
  values,
  canEdit,
  saving = false,
  onSave,
}: CustomFieldsPanelProps) {
  const { can } = useAuth()
  const canManage = can('custom_fields.manage')

  const definitions = useQuery({
    queryKey: ['custom-fields', entityType],
    queryFn: () => customFieldsApi.list(entityType),
  })

  const fields = definitions.data?.items ?? []

  // Nothing to show and nothing the user may define: stay out of the way.
  if (definitions.isSuccess && fields.length === 0 && !canManage) return null

  return (
    <div style={{ marginTop: 'var(--space-5)' }}>
      <Card title="Custom fields">
        {definitions.isError ? (
          <Alert tone="error">{errorMessage(definitions.error)}</Alert>
        ) : definitions.isLoading ? (
          <Skeleton height={64} />
        ) : fields.length === 0 ? (
          <p className="text-muted text-sm">No custom fields are defined yet.</p>
        ) : (
          <div className="stack stack--sm">
            {fields.map((field) => (
              <CustomFieldRow
                key={field.id}
                field={field}
                values={values}
                canEdit={canEdit}
                saving={saving}
                onSave={onSave}
              />
            ))}
          </div>
        )}

        {canManage ? <DefineFieldForm entityType={entityType} /> : null}
      </Card>
    </div>
  )
}

function displayValue(field: CustomFieldDefinition, raw: unknown): string {
  if (raw === undefined || raw === null || raw === '') return '—'
  if (field.field_type === 'boolean') return raw ? 'Yes' : 'No'
  if (field.field_type === 'multiselect' && Array.isArray(raw)) return raw.join(', ')
  if (field.field_type === 'date') return String(raw).slice(0, 10)
  return String(raw)
}

function CustomFieldRow({
  field,
  values,
  canEdit,
  saving,
  onSave,
}: {
  field: CustomFieldDefinition
  values: CustomFields
  canEdit: boolean
  saving: boolean
  onSave: (next: CustomFields) => Promise<unknown>
}) {
  const [error, setError] = useState<string | null>(null)
  const current = values[field.key]

  const save = useMutation({
    mutationFn: (next: CustomFields) => onSave(next),
    onError: (err) => setError(errorMessage(err)),
  })

  if (!canEdit) {
    return (
      <div className="row row--between">
        <span className="text-subtle text-sm">{field.label}</span>
        <span className="text-sm">{displayValue(field, current)}</span>
      </div>
    )
  }

  function commit(value: unknown) {
    setError(null)
    save.mutate({ ...values, [field.key]: value })
  }

  const busy = saving || save.isPending

  return (
    <div className="stack stack--sm">
      <label className="text-subtle text-sm" htmlFor={`cf-${field.id}`}>
        {field.label}
        {field.required ? ' *' : ''}
      </label>
      {field.field_type === 'boolean' ? (
        <input
          id={`cf-${field.id}`}
          type="checkbox"
          checked={Boolean(current)}
          disabled={busy}
          onChange={(event) => commit(event.target.checked)}
        />
      ) : field.field_type === 'select' || field.field_type === 'multiselect' ? (
        <SelectField field={field} current={current} busy={busy} commit={commit} />
      ) : (
        <input
          id={`cf-${field.id}`}
          type={
            field.field_type === 'number' ? 'number' : field.field_type === 'date' ? 'date' : 'text'
          }
          defaultValue={current == null ? '' : String(current)}
          disabled={busy}
          onBlur={(event) => {
            const raw = event.target.value
            if (raw === (current == null ? '' : String(current))) return
            commit(field.field_type === 'number' ? (raw === '' ? null : Number(raw)) : raw || null)
          }}
        />
      )}
      {error ? <Alert tone="error">{error}</Alert> : null}
    </div>
  )
}

function SelectField({
  field,
  current,
  busy,
  commit,
}: {
  field: CustomFieldDefinition
  current: unknown
  busy: boolean
  commit: (value: unknown) => void
}) {
  const choices = readChoices(field)
  if (field.field_type === 'multiselect') {
    const selected = Array.isArray(current) ? current.map(String) : []
    return (
      <div className="stack stack--sm">
        {choices.map((choice) => (
          <label key={choice} className="row" style={{ gap: 'var(--space-2)' }}>
            <input
              type="checkbox"
              checked={selected.includes(choice)}
              disabled={busy}
              onChange={(event) => {
                const next = event.target.checked
                  ? [...selected, choice]
                  : selected.filter((value) => value !== choice)
                commit(next)
              }}
            />
            <span className="text-sm">{choice}</span>
          </label>
        ))}
      </div>
    )
  }
  return (
    <select
      id={`cf-${field.id}`}
      value={current == null ? '' : String(current)}
      disabled={busy}
      onChange={(event) => commit(event.target.value || null)}
    >
      <option value="">—</option>
      {choices.map((choice) => (
        <option key={choice} value={choice}>
          {choice}
        </option>
      ))}
    </select>
  )
}

function readChoices(field: CustomFieldDefinition): string[] {
  const raw = field.options?.choices
  if (!Array.isArray(raw)) return []
  return raw.map(String)
}

const NEW_FIELD_TYPES: CustomFieldType[] = [
  'text',
  'number',
  'boolean',
  'date',
  'select',
  'multiselect',
]

function DefineFieldForm({ entityType }: { entityType: CustomFieldEntityType }) {
  const queryClient = useQueryClient()
  const [key, setKey] = useState('')
  const [label, setLabel] = useState('')
  const [fieldType, setFieldType] = useState<CustomFieldType>('text')
  const [choices, setChoices] = useState('')
  const [error, setError] = useState<string | null>(null)

  const create = useMutation({
    mutationFn: (body: Parameters<typeof customFieldsApi.create>[0]) =>
      customFieldsApi.create(body),
    onSuccess: () => {
      setKey('')
      setLabel('')
      setChoices('')
      setError(null)
      void queryClient.invalidateQueries({ queryKey: ['custom-fields', entityType] })
    },
    onError: (err) => setError(errorMessage(err)),
  })

  function onSubmit(event: FormEvent) {
    event.preventDefault()
    setError(null)
    const trimmedKey = key.trim().toLowerCase()
    if (!trimmedKey || !label.trim()) {
      setError('A field needs a key and a label.')
      return
    }
    const needsChoices = fieldType === 'select' || fieldType === 'multiselect'
    const parsed = choices
      .split(',')
      .map((choice) => choice.trim())
      .filter(Boolean)
    if (needsChoices && parsed.length === 0) {
      setError('A select field needs at least one choice.')
      return
    }
    create.mutate({
      entity_type: entityType,
      key: trimmedKey,
      label: label.trim(),
      field_type: fieldType,
      options: needsChoices ? { choices: parsed } : undefined,
    })
  }

  const needsChoices = fieldType === 'select' || fieldType === 'multiselect'

  return (
    <form
      className="stack stack--sm"
      style={{
        marginTop: 'var(--space-4)',
        borderTop: '1px solid var(--color-border)',
        paddingTop: 'var(--space-3)',
      }}
      onSubmit={onSubmit}
    >
      <span className="text-subtle text-sm">Define a new field</span>
      <div className="row" style={{ gap: 'var(--space-2)' }}>
        <input
          aria-label="Field key"
          placeholder="key"
          value={key}
          onChange={(event) => setKey(event.target.value)}
          disabled={create.isPending}
        />
        <input
          aria-label="Field label"
          placeholder="Label"
          value={label}
          onChange={(event) => setLabel(event.target.value)}
          disabled={create.isPending}
        />
      </div>
      <div className="row" style={{ gap: 'var(--space-2)' }}>
        <select
          aria-label="Field type"
          value={fieldType}
          onChange={(event) => setFieldType(event.target.value as CustomFieldType)}
          disabled={create.isPending}
        >
          {NEW_FIELD_TYPES.map((type) => (
            <option key={type} value={type}>
              {type}
            </option>
          ))}
        </select>
        {needsChoices ? (
          <input
            aria-label="Choices"
            placeholder="Comma-separated choices"
            value={choices}
            onChange={(event) => setChoices(event.target.value)}
            disabled={create.isPending}
          />
        ) : null}
        <button type="submit" className="btn btn--secondary btn--sm" disabled={create.isPending}>
          {create.isPending ? 'Adding…' : 'Add field'}
        </button>
      </div>
      {error ? <Alert tone="error">{error}</Alert> : null}
    </form>
  )
}
