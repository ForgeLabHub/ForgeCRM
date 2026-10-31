/**
 * Organization configuration.
 *
 * The platform ships defaults for every setting; this page lets an organization
 * choose its own. It renders the editor from the catalog the server publishes
 * rather than from a list kept here, so a key added on the server appears without
 * a change to the interface, and a key the server does not know is never offered.
 *
 * Three things the page is careful about, because each is a way the surface could
 * lie about what it did:
 *
 *   - "the organization chose this" is read from `has_override`, not inferred by
 *     comparing the value to the default. Choosing the default value is a real
 *     choice and must show as one.
 *   - a save shows the value the server resolved, from the response, rather than
 *     the value the form sent. If a write were transformed on the way in, the page
 *     would show the transformation rather than the truth.
 *   - "reset" asks for confirmation and names the value it removes, because it is
 *     the one action here that discards a decision someone made.
 */
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { tenantSettingsApi } from '../../api/tenantSettings'
import type { TenantSetting } from '../../api/tenantSettings'
import { errorMessage, fieldErrors } from '../../api/client'
import { useAuth } from '../../hooks/useAuth'
import {
  Alert,
  Badge,
  Card,
  ConfirmDialog,
  ErrorState,
  Field,
  PageHeader,
  Skeleton,
} from '../../components/ui'

/** Renders a value for display without pretending to know more than we do. */
function showValue(value: unknown): string {
  if (value === undefined || value === null) return '—'
  if (typeof value === 'boolean') return value ? 'on' : 'off'
  return String(value)
}

export function TenantSettingsPage() {
  const { can } = useAuth()
  const mayManage = can('tenant.settings.manage')

  const settings = useQuery({
    queryKey: ['tenant-settings'],
    queryFn: () => tenantSettingsApi.list(),
  })

  if (settings.isError) {
    return (
      <Card title="Organization configuration">
        <ErrorState
          message={errorMessage(settings.error)}
          onRetry={() => void settings.refetch()}
        />
      </Card>
    )
  }

  const items = settings.data?.items ?? []

  return (
    <>
      <PageHeader
        title="Organization configuration"
        meta="Defaults this organization chooses over the platform's. Each applies to the whole organization."
      />

      {!mayManage ? (
        <Alert tone="info">
          You can see these values but not change them. Changing one affects every user in the
          organization.
        </Alert>
      ) : null}

      {settings.isLoading ? (
        <Card title="Settings">
          <Skeleton height={20} />
          <Skeleton height={20} />
          <Skeleton height={20} />
        </Card>
      ) : items.length ? (
        <div className="stack">
          {items.map((setting) => (
            <SettingCard key={setting.key} setting={setting} mayManage={mayManage} />
          ))}
        </div>
      ) : (
        <Card title="Settings">
          <p className="text-muted">The server publishes no settings.</p>
        </Card>
      )}
    </>
  )
}

function SettingCard({ setting, mayManage }: { setting: TenantSetting; mayManage: boolean }) {
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<string>(() => toDraft(setting))
  const [confirmReset, setConfirmReset] = useState(false)

  const invalidate = () => void queryClient.invalidateQueries({ queryKey: ['tenant-settings'] })

  const save = useMutation({
    mutationFn: (value: unknown) => tenantSettingsApi.set(setting.key, value),
    onSuccess: () => invalidate(),
  })
  const reset = useMutation({
    mutationFn: () => tenantSettingsApi.reset(setting.key),
    onSuccess: () => {
      setConfirmReset(false)
      setDraft(toDraft({ ...setting, has_override: false, override: undefined }))
      invalidate()
    },
  })

  const errors = fieldErrors(save.error)
  const dirty = draft !== toDraft(setting)

  const onSave = () => {
    const value = parseDraft(setting, draft)
    if (value === undefined) return
    save.mutate(value)
  }

  return (
    <>
      <Card
        title={setting.title}
        actions={
          setting.has_override ? (
            <Badge tone="accent">organization override</Badge>
          ) : (
            <Badge tone="neutral">platform default</Badge>
          )
        }
      >
        <p className="text-muted text-sm">{setting.description}</p>
        <p className="text-subtle text-sm mono">{setting.key}</p>

        {setting.stored_under_older_schema ? (
          <Alert tone="warning">
            This value was stored under an older version of the catalog (schema{' '}
            {setting.schema_version}). Review it before relying on it.
          </Alert>
        ) : null}

        {save.isError ? <Alert tone="error">{errorMessage(save.error)}</Alert> : null}
        {reset.isError ? <Alert tone="error">{errorMessage(reset.error)}</Alert> : null}

        <div className="form-grid">
          <Field
            label="Value"
            htmlFor={`setting-${setting.key}`}
            error={errors.value}
            hint={`Default: ${showValue(setting.default)}`}
          >
            <SettingInput setting={setting} value={draft} onChange={setDraft} disabled={!mayManage} />
          </Field>
        </div>

        <div className="form-actions">
          <span className="text-sm text-muted">
            Effective now: <strong>{showValue(setting.effective)}</strong>
          </span>
          {mayManage ? (
            <>
              <button
                type="button"
                className="btn btn--secondary"
                onClick={() => setConfirmReset(true)}
                disabled={!setting.has_override || reset.isPending}
              >
                Reset to default
              </button>
              <button
                type="button"
                className="btn btn--primary"
                onClick={onSave}
                disabled={!dirty || save.isPending}
              >
                {save.isPending ? 'Saving…' : 'Save'}
              </button>
            </>
          ) : null}
        </div>
      </Card>

      <ConfirmDialog
        open={confirmReset}
        title={`Reset ${setting.title}?`}
        text={`The organization's value (${showValue(setting.override)}) is removed and the platform default (${showValue(setting.default)}) applies again. The change is recorded in the history.`}
        confirmLabel="Reset"
        danger
        busy={reset.isPending}
        onConfirm={() => reset.mutate()}
        onCancel={() => setConfirmReset(false)}
      />
    </>
  )
}

/** The draft's string form, so every kind is edited through one controlled input. */
function toDraft(setting: TenantSetting): string {
  const value = setting.has_override ? setting.override : setting.default
  if (value === undefined || value === null) return ''
  if (typeof value === 'boolean') return value ? 'true' : 'false'
  return String(value)
}

/** Parses the draft back to the kind's Go value, or undefined when it cannot. */
function parseDraft(setting: TenantSetting, draft: string): unknown {
  switch (setting.kind) {
    case 'boolean':
      return draft === 'true'
    case 'integer': {
      const n = Number(draft)
      return Number.isFinite(n) ? n : undefined
    }
    default:
      return draft
  }
}

function SettingInput({
  setting,
  value,
  onChange,
  disabled,
}: {
  setting: TenantSetting
  value: string
  onChange: (next: string) => void
  disabled: boolean
}) {
  const id = `setting-${setting.key}`
  if (setting.kind === 'enum') {
    return (
      <select
        id={id}
        className="input"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        disabled={disabled}
      >
        {(setting.enum ?? []).map((option) => (
          <option key={option} value={option}>
            {option}
          </option>
        ))}
      </select>
    )
  }
  if (setting.kind === 'boolean') {
    return (
      <select
        id={id}
        className="input"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        disabled={disabled}
      >
        <option value="true">on</option>
        <option value="false">off</option>
      </select>
    )
  }
  if (setting.kind === 'integer') {
    return (
      <input
        id={id}
        type="number"
        className="input"
        value={value}
        min={setting.min}
        max={setting.max}
        onChange={(event) => onChange(event.target.value)}
        disabled={disabled}
      />
    )
  }
  return (
    <input
      id={id}
      type="text"
      className="input"
      value={value}
      maxLength={setting.max_length || undefined}
      onChange={(event) => onChange(event.target.value)}
      disabled={disabled}
    />
  )
}
