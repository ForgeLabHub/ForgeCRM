import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { webhooksApi } from '../api/integrations'
import { systemApi } from '../api/admin'
import { errorMessage, fieldErrors } from '../api/client'
import type { Webhook } from '../types/api'
import { useAuth } from '../hooks/useAuth'
import { ResourceList, type Column } from '../components/ResourceList'
import {
  Alert,
  Badge,
  ConfirmDialog,
  Field,
  PageHeader,
  StatusBadge,
  Timestamp,
} from '../components/ui'

/**
 * The event vocabulary is read from the server rather than hard-coded.
 *
 * A local list once used shortened names ("contact.created") while the dispatcher
 * matched "crm.contact.created", so a subscription built from this form selected
 * no events and the endpoint silently received nothing. Reading
 * /system/capabilities keeps the picker equal to what the server actually emits.
 */
function useEventTypes() {
  const query = useQuery({
    queryKey: ['system', 'capabilities'],
    queryFn: systemApi.capabilities,
    staleTime: 5 * 60 * 1000,
  })
  return { eventTypes: query.data?.event_types ?? [], isLoading: query.isLoading }
}

export function WebhooksPage() {
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [detail, setDetail] = useState<Webhook | null>(null)
  const [toggling, setToggling] = useState<Webhook | null>(null)
  const [testResult, setTestResult] = useState<Record<string, { ok: boolean; response_status?: number; error?: string }>>({})
  const [actionError, setActionError] = useState<string | null>(null)

  const setEnabled = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      webhooksApi.setEnabled(id, enabled),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['webhooks'] })
      setToggling(null)
      setActionError(null)
    },
    onError: (error) => {
      setToggling(null)
      setActionError(errorMessage(error))
    },
  })

  const test = useMutation({
    mutationFn: (id: string) => webhooksApi.test(id),
    onSuccess: (result, id) => setTestResult((previous) => ({ ...previous, [id]: result })),
    onError: (error, id) =>
      setTestResult((previous) => ({ ...previous, [id]: { ok: false, error: errorMessage(error) } })),
  })

  const columns: Column<Webhook>[] = [
    {
      key: 'name',
      header: 'Subscription',
      render: (row) => (
        <div>
          <div className="table__primary">{row.name}</div>
          <div className="table__secondary truncate mono" style={{ maxWidth: 320 }}>
            {row.target_url}
          </div>
        </div>
      ),
    },
    {
      key: 'events',
      header: 'Events',
      render: (row) =>
        row.event_types.length > 8 ? (
          <Badge tone="neutral">{row.event_types.length} types</Badge>
        ) : (
          <div className="row row--wrap">
            {row.event_types.map((type) => (
              <Badge key={type} tone="neutral">
                {type}
              </Badge>
            ))}
          </div>
        ),
    },
    {
      key: 'enabled',
      header: 'Enabled',
      render: (row) =>
        row.enabled ? <Badge tone="success">enabled</Badge> : <Badge tone="neutral">disabled</Badge>,
    },
    {
      key: 'delivery',
      header: 'Last test',
      render: (row) => {
        const result = testResult[row.id]
        if (!result) return <span className="text-subtle">Not tested</span>
        return result.ok ? (
          <Badge tone="success">HTTP {result.response_status}</Badge>
        ) : (
          <span title={result.error}>
            <Badge tone="danger">failed</Badge>
          </span>
        )
      },
    },
    {
      key: 'actions',
      header: '',
      width: '230px',
      render: (row) => (
        <div className="row">
          <button
            type="button"
            className="btn btn--secondary btn--sm"
            onClick={(event) => {
              event.stopPropagation()
              test.mutate(row.id)
            }}
            disabled={test.isPending}
          >
            Send test
          </button>
          <button
            type="button"
            className="btn btn--ghost btn--sm"
            onClick={(event) => {
              event.stopPropagation()
              setToggling(row)
            }}
          >
            {row.enabled ? 'Disable' : 'Enable'}
          </button>
          <button
            type="button"
            className="btn btn--ghost btn--sm"
            onClick={(event) => {
              event.stopPropagation()
              setDetail(row)
            }}
          >
            Deliveries
          </button>
        </div>
      ),
    },
  ]

  return (
    <>
      <PageHeader
        title="Webhooks"
        meta="Send platform events to external systems, with retries and a delivery history."
      />

      {actionError ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="error">{actionError}</Alert>
        </div>
      ) : null}

      <ResourceList
        title="Subscriptions"
        queryKey={['webhooks']}
        fetchPage={(params) => webhooksApi.list(params)}
        columns={columns}
        rowKey={(row) => row.id}
        emptyTitle="No webhook subscriptions"
        emptyText="Subscribe an external endpoint to receive events when records change."
        searchable
        searchPlaceholder="Search subscriptions…"
        actions={
          can('webhooks.manage') ? (
            <button type="button" className="btn btn--primary btn--sm" onClick={() => setCreating(true)}>
              New subscription
            </button>
          ) : null
        }
      />

      <NewWebhookDialog
        open={creating}
        onClose={() => setCreating(false)}
        onCreated={() => {
          setCreating(false)
          void queryClient.invalidateQueries({ queryKey: ['webhooks'] })
        }}
      />

      {detail ? <DeliveriesDialog webhook={detail} onClose={() => setDetail(null)} /> : null}

      <ConfirmDialog
        open={toggling !== null}
        title={toggling?.enabled ? 'Disable this subscription?' : 'Enable this subscription?'}
        text={
          toggling?.enabled
            ? 'No further deliveries are attempted. Pending deliveries already queued may still be sent.'
            : 'Deliveries resume for events that occur from now on. Events missed while disabled are not replayed automatically.'
        }
        confirmLabel={toggling?.enabled ? 'Disable' : 'Enable'}
        danger={Boolean(toggling?.enabled)}
        busy={setEnabled.isPending}
        onConfirm={() =>
          toggling && setEnabled.mutate({ id: toggling.id, enabled: !toggling.enabled })
        }
        onCancel={() => setToggling(null)}
      />
    </>
  )
}

function NewWebhookDialog({
  open,
  onClose,
  onCreated,
}: {
  open: boolean
  onClose: () => void
  onCreated: () => void
}) {
  const [name, setName] = useState('')
  const [targetUrl, setTargetUrl] = useState('')
  const [selected, setSelected] = useState<string[]>([])
  const [secret, setSecret] = useState('')
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)
  const { eventTypes, isLoading: eventsLoading } = useEventTypes()

  const mutation = useMutation({
    mutationFn: () =>
      webhooksApi.create({
        name,
        target_url: targetUrl,
        event_types: selected,
        secret: secret || undefined,
      }),
    onSuccess: onCreated,
    onError: (error) => {
      setErrors(fieldErrors(error))
      setFormError(errorMessage(error))
    },
  })

  function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setFormError(null)
    setErrors({})
    mutation.mutate()
  }

  if (!open) return null

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="new-webhook-title"
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(16, 24, 40, 0.45)',
        zIndex: 60,
        overflowY: 'auto',
        padding: 'var(--space-5)',
      }}
      onClick={onClose}
    >
      <div className="card" style={{ maxWidth: 640, margin: '0 auto' }} onClick={(e) => e.stopPropagation()}>
        <div className="card__header">
          <h2 id="new-webhook-title">New webhook subscription</h2>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="card__body stack">
            {formError ? <Alert tone="error">{formError}</Alert> : null}

            <Field label="Name" htmlFor="webhook-name" required error={errors.name}>
              <input
                id="webhook-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                required
              />
            </Field>

            <Field
              label="Target URL"
              htmlFor="webhook-url"
              required
              error={errors.target_url}
              hint="Must be a public https endpoint. Internal addresses are rejected to prevent SSRF."
            >
              <input
                id="webhook-url"
                type="url"
                placeholder="https://example.com/hooks/forgecrm"
                value={targetUrl}
                onChange={(event) => setTargetUrl(event.target.value)}
                required
              />
            </Field>

            <Field
              label="Signing secret"
              htmlFor="webhook-secret"
              hint="Optional. If set, each delivery carries an HMAC signature in the X-ForgeCRM-Signature header."
            >
              <input
                id="webhook-secret"
                type="password"
                autoComplete="new-password"
                value={secret}
                onChange={(event) => setSecret(event.target.value)}
              />
            </Field>

            <fieldset style={{ border: 'none', padding: 0, margin: 0 }}>
              <legend className="field__label" style={{ marginBottom: 'var(--space-2)' }}>
                Events <span className="field__required">*</span>
              </legend>
              <div
                style={{
                  display: 'grid',
                  gridTemplateColumns: 'repeat(auto-fill, minmax(190px, 1fr))',
                  gap: 'var(--space-2)',
                  maxHeight: 220,
                  overflowY: 'auto',
                  border: '1px solid var(--color-border)',
                  borderRadius: 'var(--radius-sm)',
                  padding: 'var(--space-3)',
                }}
              >
                {eventsLoading ? (
                  <span className="text-muted">Loading event types…</span>
                ) : eventTypes.length === 0 ? (
                  <span className="field__error">
                    The event list could not be loaded, so a subscription cannot be created. Reload
                    the page and try again.
                  </span>
                ) : (
                  eventTypes.map((type) => (
                    <label key={type} className="checkbox">
                      <input
                        type="checkbox"
                        checked={selected.includes(type)}
                        onChange={(event) =>
                          setSelected((previous) =>
                            event.target.checked
                              ? [...previous, type]
                              : previous.filter((item) => item !== type),
                          )
                        }
                      />
                      <span className="mono">{type}</span>
                    </label>
                  ))
                )}
              </div>
              {errors.event_types ? (
                <span className="field__error">{errors.event_types}</span>
              ) : (
                <span className="field__hint">{selected.length} selected</span>
              )}
            </fieldset>
          </div>
          <div className="form-actions">
            <button type="button" className="btn btn--secondary" onClick={onClose}>
              Cancel
            </button>
            <button
              type="submit"
              className="btn btn--primary"
              disabled={mutation.isPending || selected.length === 0}
            >
              {mutation.isPending ? 'Creating…' : 'Create subscription'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

function DeliveriesDialog({ webhook, onClose }: { webhook: Webhook; onClose: () => void }) {
  const queryClient = useQueryClient()
  const [replaying, setReplaying] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const deliveries = useQuery({
    queryKey: ['webhook', webhook.id, 'deliveries'],
    queryFn: () => webhooksApi.deliveries(webhook.id, { limit: 50 }),
    refetchInterval: 5000,
  })

  const replay = useMutation({
    mutationFn: (deliveryId: string) => webhooksApi.replay(webhook.id, deliveryId),
    onSuccess: () => {
      setReplaying(null)
      setError(null)
      void queryClient.invalidateQueries({ queryKey: ['webhook', webhook.id, 'deliveries'] })
    },
    onError: (err) => {
      setReplaying(null)
      setError(errorMessage(err))
    },
  })

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="deliveries-title"
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(16, 24, 40, 0.45)',
        zIndex: 60,
        overflowY: 'auto',
        padding: 'var(--space-5)',
      }}
      onClick={onClose}
    >
      <div className="card" style={{ maxWidth: 900, margin: '0 auto' }} onClick={(e) => e.stopPropagation()}>
        <div className="card__header">
          <h2 id="deliveries-title">Deliveries — {webhook.name}</h2>
        </div>
        <div className="card__body">
          {error ? (
            <div style={{ marginBottom: 'var(--space-3)' }}>
              <Alert tone="error">{error}</Alert>
            </div>
          ) : null}

          {deliveries.isLoading ? (
            <p className="text-muted">Loading…</p>
          ) : deliveries.data?.items.length ? (
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Event</th>
                    <th>Status</th>
                    <th className="numeric">Attempts</th>
                    <th className="numeric">Replays</th>
                    <th>Result</th>
                    <th>When</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {deliveries.data.items.map((delivery) => (
                    <tr key={delivery.id}>
                      <td className="mono">{delivery.event_type}</td>
                      <td>
                        <StatusBadge status={delivery.status} />
                      </td>
                      <td className="numeric">{delivery.attempts}</td>
                      <td className="numeric">{delivery.replay_count ?? 0}</td>
                      <td
                        className="truncate"
                        style={{ maxWidth: '22rem' }}
                        title={delivery.last_error ?? undefined}
                      >
                        {delivery.last_error ? delivery.last_error : (delivery.response_status ?? '—')}
                      </td>
                      <td>
                        <Timestamp value={delivery.created_at} />
                      </td>
                      <td>
                        {delivery.status !== 'delivered' ? (
                          <button
                            type="button"
                            className="btn btn--secondary btn--sm"
                            onClick={() => {
                              setReplaying(delivery.id)
                              replay.mutate(delivery.id)
                            }}
                            disabled={replaying === delivery.id}
                          >
                            {replaying === delivery.id ? 'Replaying…' : 'Replay'}
                          </button>
                        ) : null}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <p className="text-muted">No deliveries recorded for this subscription yet.</p>
          )}
        </div>
        <div className="form-actions" style={{ margin: 0, padding: 'var(--space-4)' }}>
          <button type="button" className="btn btn--secondary" onClick={onClose}>
            Close
          </button>
        </div>
      </div>
    </div>
  )
}
