/**
 * Consent for one subject.
 *
 * The backend has the record and the policy; this panel is the surface an operator
 * uses to answer two questions the API already answers: "may we contact this person
 * about this?" and "why did the last send succeed or fail?". It shows the current
 * decisions for the subject, lets a permitted user record or withdraw one, and
 * renders the history so a withdrawal is visible as a decision, not a blank.
 *
 * A subject is a contact or a bare email address. The email case matters because a
 * message can be sent to someone who is not in the CRM yet: refusing to record their
 * answer would leave "never asked" as the only reachable state, and the policy blocks
 * that. Both forms are one record addressed two ways, so they share this panel rather
 * than growing a second copy that drifts.
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
  type ConsentSubject,
} from '../api/consent'
import { errorMessage } from '../api/client'
import { useAuth } from '../hooks/useAuth'
import { useT, type TFunction, type TranslationKey } from '../i18n'
import { Alert, Badge, Card, Field, Skeleton, Timestamp } from './ui'

const STATE_TONE: Record<ConsentState, 'success' | 'danger' | 'warning'> = {
  granted: 'success',
  denied: 'danger',
  withdrawn: 'warning',
}

const STATE_LABEL_KEY: Record<ConsentState, TranslationKey> = {
  granted: 'consent.granted',
  denied: 'consent.denied',
  withdrawn: 'consent.withdrawn',
}

function stateLabel(t: TFunction, state: ConsentState): string {
  return t(STATE_LABEL_KEY[state])
}

/**
 * A stable query key for a subject. A contact id and an email are different
 * subjects, so they must not collide in the cache.
 */
function subjectKey(subject: ConsentSubject): string {
  return 'contact_id' in subject ? `contact:${subject.contact_id}` : `email:${subject.email}`
}

/** The body a write carries: a contact id or a bare address, never both. */
function subjectBody(subject: ConsentSubject): { contact_id?: string; subject_email?: string } {
  return 'contact_id' in subject
    ? { contact_id: subject.contact_id }
    : { subject_email: subject.email }
}

/** A purpose the operator can ask about. */
export interface ConsentPanelProps {
  /** Whose consent this panel is about. */
  subject: ConsentSubject
  /** What to call the subject in the empty state, e.g. an email address. */
  subjectLabel?: string
}

export function ConsentPanel({ subject, subjectLabel }: ConsentPanelProps) {
  const t = useT()
  const { can } = useAuth()
  const canRead = can('consent.read')
  const canManage = can('consent.manage')
  const key = subjectKey(subject)

  // Without the read permission there is nothing to show and nothing to offer.
  if (!canRead) return null

  return (
    <div style={{ marginTop: 'var(--space-5)' }}>
      <Card title={t('consent.title')}>
        <CurrentDecisions subject={subject} subjectKey={key} subjectLabel={subjectLabel} />
        {canManage ? <RecordDecisionForm subject={subject} subjectKey={key} /> : null}
        <HistorySection subject={subject} subjectKey={key} />
      </Card>
    </div>
  )
}

/** The decisions still in force for the subject, with the reason for each. */
function CurrentDecisions({
  subject,
  subjectKey: key,
  subjectLabel,
}: {
  subject: ConsentSubject
  subjectKey: string
  subjectLabel?: string
}) {
  const t = useT()
  const current = useQuery({
    queryKey: ['consent', 'current', key],
    queryFn: () => consentApi.current(subject),
  })

  if (current.isError) {
    return <Alert tone="error">{errorMessage(current.error)}</Alert>
  }
  if (current.isLoading) return <Skeleton height={48} />

  const items = current.data?.items ?? []
  if (items.length === 0) {
    return (
      <p className="text-muted text-sm">
        {subjectLabel
          ? t('consent.noneWithSubject', { label: subjectLabel })
          : t('consent.noneWithoutSubject')}
      </p>
    )
  }

  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th>{t('consent.purpose')}</th>
            <th>{t('consent.channel')}</th>
            <th>{t('consent.state')}</th>
            <th>{t('consent.decided')}</th>
            <th>{t('consent.expires')}</th>
          </tr>
        </thead>
        <tbody>
          {items.map((record) => (
            <tr key={record.id}>
              <td>{record.purpose.replace(/_/g, ' ')}</td>
              <td>{record.channel}</td>
              <td>
                <Badge tone={STATE_TONE[record.state]}>{stateLabel(t, record.state)}</Badge>
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
    </div>
  )
}

/** Records a decision, or withdraws the current one. */
function RecordDecisionForm({
  subject,
  subjectKey: key,
}: {
  subject: ConsentSubject
  subjectKey: string
}) {
  const t = useT()
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
        // The subject decides which key the write carries: a contact is keyed by
        // its id so the decision follows the person even if the address changes,
        // a bare address is keyed by the address because there is no person yet.
        ...subjectBody(subject),
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
    mutationFn: () => consentApi.withdraw(subject, purpose, channel, 'withdrawn by an operator'),
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
      <Field label={t('consent.purpose')} htmlFor={`consent-purpose-${key}`}>
        <select
          id={`consent-purpose-${key}`}
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

      <Field label={t('consent.channel')} htmlFor={`consent-channel-${key}`}>
        <select
          id={`consent-channel-${key}`}
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

      <Field label={t('consent.decision')} htmlFor={`consent-state-${key}`}>
        <select
          id={`consent-state-${key}`}
          className="input"
          value={state}
          onChange={(e) => setState(e.target.value as ConsentState)}
        >
          <option value="granted">{t('consent.granted')}</option>
          <option value="denied">{t('consent.denied')}</option>
          <option value="withdrawn">{t('consent.withdrawn')}</option>
        </select>
      </Field>

      <Field
        label={t('consent.basis')}
        htmlFor={`consent-basis-${key}`}
        hint={t('consent.basisHint')}
      >
        <input
          id={`consent-basis-${key}`}
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
          {state === 'withdrawn' ? t('consent.withdrawConsent') : t('consent.recordDecision')}
        </button>
      </div>
    </form>
  )
}

/** The revision chain for one purpose and channel: why the current answer is what it is. */
function HistorySection({
  subject,
  subjectKey: key,
}: {
  subject: ConsentSubject
  subjectKey: string
}) {
  const t = useT()
  const [purpose, setPurpose] = useState<ConsentPurpose>('marketing')
  const [channel, setChannel] = useState<ConsentChannel>('email')

  const history = useQuery({
    queryKey: ['consent', 'history', key, purpose, channel],
    queryFn: () => consentApi.history(subject, purpose, channel),
  })

  return (
    <details style={{ marginTop: 'var(--space-4)' }}>
      <summary className="text-sm">{t('consent.history')}</summary>
      <div className="stack stack--sm" style={{ marginTop: 'var(--space-3)' }}>
        <div className="form-grid">
          <Field label={t('consent.historyPurpose')} htmlFor={`consent-history-purpose-${key}`}>
            <select
              id={`consent-history-purpose-${key}`}
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
          <Field label={t('consent.historyChannel')} htmlFor={`consent-history-channel-${key}`}>
            <select
              id={`consent-history-channel-${key}`}
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
          <p className="text-muted text-sm">{t('consent.historyEmpty')}</p>
        ) : (
          <ul className="stack stack--sm" style={{ listStyle: 'none', padding: 0 }}>
            {(history.data?.items ?? []).map((record: ConsentRecord) => (
              <li key={record.id} className="text-sm">
                <Badge tone={STATE_TONE[record.state]}>{stateLabel(t, record.state)}</Badge>{' '}
                {t('consent.revision', { n: record.revision })}
                {record.current ? t('consent.current') : ''} — <Timestamp value={record.decided_at} />
                {record.basis ? <> · {record.basis}</> : null}
              </li>
            ))}
          </ul>
        )}
      </div>
    </details>
  )
}
