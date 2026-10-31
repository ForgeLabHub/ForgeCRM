/**
 * Consent: what a person agreed to be contacted about, on which channel.
 *
 * The backend models a decision as one triple — subject, purpose, channel — with a
 * state of granted / denied / withdrawn, and treats an absent record as "never
 * asked", which is not permission. This client mirrors that: `decide` is the
 * question an operator asks before sending ("may we, and why?"), and the rest is
 * the record an operator or auditor works through.
 *
 * A subject is either a contact in the CRM or a bare email address, never both.
 */
import { api, type ListResponse } from './client'

/** What a person was asked about. Closed set; the server refuses anything else. */
export type ConsentPurpose = 'marketing' | 'transactional' | 'product_updates' | 'research'

/** How we would reach them. Agreeing to email is not agreeing to Telegram. */
export type ConsentChannel = 'email' | 'telegram' | 'webhook'

/** A decision's state. Absent is not a state — it is "never asked". */
export type ConsentState = 'granted' | 'denied' | 'withdrawn'

export const CONSENT_PURPOSES: ConsentPurpose[] = [
  'marketing',
  'transactional',
  'product_updates',
  'research',
]

export const CONSENT_CHANNELS: ConsentChannel[] = ['email', 'telegram', 'webhook']

/** One recorded decision, at one revision. */
export interface ConsentRecord {
  id: string
  organization_id: string
  contact_id?: string
  subject_email?: string
  purpose: ConsentPurpose
  channel: ConsentChannel
  state: ConsentState
  basis?: string
  source?: string
  evidence?: string
  revision: number
  superseded_by?: string
  recorded_by?: string
  decided_at: string
  expires_at?: string
  created_at: string
  updated_at: string
  /** Derived at read time: true when this revision is still in force. */
  current: boolean
}

/** The answer a channel acts on, together with the reason for it. */
export interface ConsentDecision {
  allowed: boolean
  /** Why, in the operator's words. Not decoration: "no record" and "withdrawn" differ. */
  reason: string
  state?: ConsentState
  record_id?: string
  decided_at?: string
  expires_at?: string
}

export interface ConsentInput {
  contact_id?: string
  subject_email?: string
  purpose: ConsentPurpose
  channel: ConsentChannel
  state: ConsentState
  basis?: string
  source?: string
  evidence?: string
  expires_at?: string
  decided_at?: string
}

/** A subject is a contact id or an email, never both. */
export type ConsentSubject = { contact_id: string } | { email: string }

function subjectParams(subject: ConsentSubject): Record<string, string> {
  return 'contact_id' in subject ? { contact_id: subject.contact_id } : { email: subject.email }
}

export const consentApi = {
  /** Every decision recorded for a subject, newest first. */
  list: (subject: ConsentSubject) =>
    api.get<ListResponse<ConsentRecord>>('/api/v1/consent', subjectParams(subject)),
  /** The decisions still in force for a subject. */
  current: (subject: ConsentSubject) =>
    api.get<ListResponse<ConsentRecord>>('/api/v1/consent/current', subjectParams(subject)),
  /** The revision chain for one subject, purpose and channel. */
  history: (
    subject: ConsentSubject,
    purpose: ConsentPurpose,
    channel: ConsentChannel,
  ) =>
    api.get<ListResponse<ConsentRecord>>('/api/v1/consent/history', {
      ...subjectParams(subject),
      purpose,
      channel,
    }),
  /** May we contact them, and why? The same decision an outbound channel makes. */
  decide: (subject: ConsentSubject, purpose: ConsentPurpose, channel: ConsentChannel) =>
    api.get<ConsentDecision>('/api/v1/consent/decide', {
      ...subjectParams(subject),
      purpose,
      channel,
    }),
  record: (body: ConsentInput) => api.post<ConsentRecord>('/api/v1/consent', body),
  withdraw: (
    subject: ConsentSubject,
    purpose: ConsentPurpose,
    channel: ConsentChannel,
    reason?: string,
  ) =>
    api.post<ConsentRecord>('/api/v1/consent/withdraw', {
      ...subjectParams(subject),
      purpose,
      channel,
      reason: reason ?? '',
    }),
}
