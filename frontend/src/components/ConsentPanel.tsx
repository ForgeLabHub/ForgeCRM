/**
 * Consent for one contact.
 *
 * The backend has the record and the policy; this panel is the surface an operator
 * uses to answer two questions the API already answers: "may we contact this person
 * about this?" and "why did the last send succeed or fail?". It shows the current
 * decisions for the contact, lets a permitted user record or withdraw one, and
 * renders the history so a withdrawal is visible as a decision, not a blank.
 *
 * It is read-only without `consent.read` and cannot change anything without
 * `consent.manage`; both are enforced on the server, so this is presentation.
 */
import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  CONSENT_CHANNELS,
  CONSENT_PURPOSES,
  consentApi,
  type ConsentChannel,
  type ConsentPurpose,
  type ConsentRecord,
  type ConsentState,
} from '../api/consent'
import { errorMessage } from '../api/client'
import { useAuth } from '../hooks/useAuth'
import { Alert, Badge, Card, Field, Skeleton, Timestamp } from './ui'

const STATE_TONE: Record<ConsentState, 'success' | 'danger' | 'warning'> = {
  granted: 'success',
  denied: 'danger',
  withdrawn: 'warning',
}

const STATE_LABEL: Record<ConsentState, string> = {
  granted: 'Granted',
  denied: 'Denied',
  withdrawn: 'Withdrawn',
}

/** A purpose the operator can ask about. */
export interface ConsentPanelProps {
  /** The contact the panel is about. */
  contactId: string
  /** The contact's email, used to record a decision against the person. */
  email?: string
}

export function ConsentPanel({ contactId, email }: ConsentPanelProps) {
  const { can } = useAuth()
  const canRead = can('consent.read')
  const canManage = can('consent.manage')

  // Without the read permission there is nothing to show and nothing to offer.
  if (!canRead) return null

  return (
    <div style={{ marginTop: 'var(--space-5)' }}>
      <Card title="Consent">
        <CurrentDecisions contactId={contactId} />
        {canManage ? <RecordDecisionForm contactId={contactId} email={email} /> : null}
        <HistorySection contactId={contactId} />
      </Card>
    </div>
  )
}

/** The decisions still in force for the contact, with the reason for each. */
function CurrentDecisions({ contactId }: { contactId: string }) {
  const current = useQuery({
    queryKey: ['consent', 'current', contactId],
    queryFn: () => consentApi.current({ contact_id: contactId }),
  })

  if (current.isError) {
    return <Alert tone="error">{errorMessage(current.error)}</Alert>
  }
  if (current.isLoading) return <Skeleton height={48} />

  const items = current.data?.items ?? []
  if (items.length === 0) {
    return (
      <p className="text-muted text-sm">
        No consent on record. Marketing and product messages are blocked until a decision is
        recorded; transactional messages are still allowed.
      </p>
    )
  }

  return (
    <table>
      <thead>
        <tr>
          <th>Purpose</th>
          <th>Channel</th>
          <th>State</th>
          <th>Decided</th>
          <th>Expires</th>
        </tr>
      </thead>
      <tbody>
        {items.map((record) => (
          <tr key={record.id}>
            <td>{record.purpose.replace(/_/g, ' ')}</td>
            <td>{record.channel}</td>
            <td>
              <Badge tone={STATE_TONE[record.state]}>{STATE_LABEL[record.state]}</Badge>
            </td>
            <td>
              <Timestamp value={record.decided_at} />
            </td>
            <td>
              {record.expires_at ? <Timestamp value={record.expires_at} /> : <span className="text-subtle">—</span>}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

/** Records a decision, or withdraws the current one. */
function RecordDecisionForm({ contactId, email }: { contactId: string; email?: string }) {
  const queryClient = useQueryClient()
  const [purpose, setPurpose] = useState<ConsentPurpose>('marketing')
  const [channel, setChannel] = useState<ConsentChannel>('email')
  const [state, setState] = useState<ConsentState>('granted')
  const [basis, setBasis] = useState('')
  const [error, setError] = useState<string | null>(null)

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['consent'] })
  }

  const record = useMutation({
    mutationFn: () =>
      consentApi.record({
        // A contact is keyed by its id, so the decision follows the person even if
        // the address changes; the email is a fallback for a contact with none.
        ...(contactId ? { contact_id: contactId } : { subject_email: email ?? '' }),
        purpose,
        channel,
        state,
        basis,
      }),
    onSuccess: () => {
      setBasis('')
      setError(null)
      invalidate()
    },
    onError: (err) => setError(errorMessage(err)),
  })

  const withdraw = useMutation({
    mutationFn: () =>
      consentApi.withdraw({ contact_id: contactId }, purpose, channel, 'withdrawn from contact page'),
    onSuccess: () => {
      setError(null)
      invalidate()
    },
    onError: (err) => setError(errorMessage(err)),
  })

  const busy = record.isPending || withdraw.isPending

  const onSubmit = (e: FormEvent) => {
    e.preventDefault()
    if (state === 'withdrawn') {
      withdraw.mutate()
    } else {
      record.mutate()
    }
  }

  return (
    <form className="form-grid" onSubmit={onSubmit} style={{ marginTop: 'var(--space-4)' }}>
      <Field label="Purpose" htmlFor="consent-purpose">
        <select
          id="consent-purpose"
          className="input"
          value={purpose}
          onChange={(e) => setPurpose(e.target.value as ConsentPurpose)}
        >
          {CONSENT_PURPOSES.map((p) => (
            <option key={p} value={p}>
              {p.replace(/_/g, ' ')}
            </option>
          ))}
        </select>
      </Field>

      <Field label="Channel" htmlFor="consent-channel">
        <select
          id="consent-channel"
          className="input"
          value={channel}
          onChange={(e) => setChannel(e.target.value as ConsentChannel)}
        >
          {CONSENT_CHANNELS.map((c) => (
            <option key={c} value={c}>
              {c}
            </option>
          ))}
        </select>
      </Field>

      <Field label="Decision" htmlFor="consent-state">
        <select
          id="consent-state"
          className="input"
          value={state}
          onChange={(e) => setState(e.target.value as ConsentState)}
        >
          <option value="granted">Granted</option>
          <option value="denied">Denied</option>
          <option value="withdrawn">Withdrawn</option>
        </select>
      </Field>

      <Field label="Basis" htmlFor="consent-basis" hint="On what grounds, in your words. Optional.">
        <input
          id="consent-basis"
          className="input"
          value={basis}
          onChange={(e) => setBasis(e.target.value)}
          disabled={state === 'withdrawn'}
        />
      </Field>

      {error ? (
        <div className="form-grid__full">
          <Alert tone="error">{error}</Alert>
        </div>
      ) : null}

      <div className="form-grid__full">
        <button type="submit" className="btn btn--primary btn--sm" disabled={busy}>
          {state === 'withdrawn' ? 'Withdraw consent' : 'Record decision'}
        </button>
      </div>
    </form>
  )
}

/** The revision chain for one purpose and channel: why the current answer is what it is. */
function HistorySection({ contactId }: { contactId: string }) {
  const [purpose, setPurpose] = useState<ConsentPurpose>('marketing')
  const [channel, setChannel] = useState<ConsentChannel>('email')

  const history = useQuery({
    queryKey: ['consent', 'history', contactId, purpose, channel],
    queryFn: () => consentApi.history({ contact_id: contactId }, purpose, channel),
  })

  return (
    <details style={{ marginTop: 'var(--space-4)' }}>
      <summary className="text-sm">History</summary>
      <div className="stack stack--sm" style={{ marginTop: 'var(--space-3)' }}>
        <div className="form-grid">
          <Field label="History purpose" htmlFor="consent-history-purpose">
            <select
              id="consent-history-purpose"
              className="input"
              value={purpose}
              onChange={(e) => setPurpose(e.target.value as ConsentPurpose)}
            >
              {CONSENT_PURPOSES.map((p) => (
                <option key={p} value={p}>
                  {p.replace(/_/g, ' ')}
                </option>
              ))}
            </select>
          </Field>
          <Field label="History channel" htmlFor="consent-history-channel">
            <select
              id="consent-history-channel"
              className="input"
              value={channel}
              onChange={(e) => setChannel(e.target.value as ConsentChannel)}
            >
              {CONSENT_CHANNELS.map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </select>
          </Field>
        </div>

        {history.isError ? (
          <Alert tone="error">{errorMessage(history.error)}</Alert>
        ) : history.isLoading ? (
          <Skeleton height={40} />
        ) : (history.data?.items ?? []).length === 0 ? (
          <p className="text-muted text-sm">No decisions recorded for this purpose and channel.</p>
        ) : (
          <ul className="stack stack--sm" style={{ listStyle: 'none', padding: 0 }}>
            {(history.data?.items ?? []).map((record: ConsentRecord) => (
              <li key={record.id} className="text-sm">
                <Badge tone={STATE_TONE[record.state]}>{STATE_LABEL[record.state]}</Badge>{' '}
                revision {record.revision}
                {record.current ? ' (current)' : ''} — <Timestamp value={record.decided_at} />
                {record.basis ? <> · {record.basis}</> : null}
              </li>
            ))}
          </ul>
        )}
      </div>
    </details>
  )
}
