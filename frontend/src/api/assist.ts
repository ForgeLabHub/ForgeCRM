/**
 * Contextual AI.
 *
 * These calls ask the server to summarise or analyse one CRM record. They are
 * separate from the free-form chat in ai.ts because they carry a record context
 * the server assembles and permission-checks, and because their answer keeps
 * CRM facts and the model's inference in separate fields.
 *
 * Nothing here writes to a record. The server exposes no path that lets an
 * assistant change CRM data; a suggestion is text a user chooses to act on.
 */
import { api } from './client'

export type AssistEntityType = 'account' | 'contact' | 'deal' | 'ticket' | 'task'

export type AssistKind = 'summary' | 'risks' | 'next_actions' | 'draft' | 'wrap_up' | 'ask'

/** One CRM value the server gave the model, shown so the user can see its input. */
export interface AssistFact {
  label: string
  value: string
}

/** Something the user should know about the context: a mask, a truncation or an omission. */
export interface AssistDisclosure {
  kind: string
  detail: string
}

export interface AssistDraft {
  subject?: string
  body: string
}

/**
 * The server's grounding assessment of an insight.
 *
 * This is not the model's self-reported confidence — the server computes it from
 * signals it can check: whether the reply was structured, whether the figures in
 * it appear in the CRM facts the model was given, and whether the context was
 * complete. `basis` says which of those applied, so the number is never shown
 * without its reason.
 */
export interface AssistConfidence {
  level: 'high' | 'medium' | 'low'
  score: number
  basis: string[]
}

/**
 * The three groups are separate on purpose. Facts come from the CRM; analysis and
 * recommendations are the model's output. The interface must label the last two
 * as such — they are not CRM values.
 */
export interface AssistInsight {
  kind: AssistKind
  entity_type: AssistEntityType
  entity_id: string
  cached: boolean
  provider: string
  model: string
  facts: AssistFact[]
  analysis: string
  recommendations: string[]
  draft?: AssistDraft
  disclosures: AssistDisclosure[]
  confidence: AssistConfidence
  generated_at: string
}

export interface AssistContext {
  entity_type: AssistEntityType
  entity_id: string
  subject: string
  facts: AssistFact[]
  disclosures: AssistDisclosure[]
  available: boolean
}

export interface AssistRequest {
  kind: AssistKind
  question?: string
  refresh?: boolean
  recipient_hint?: string
}

export const assistApi = {
  /**
   * Whether a provider is configured. Delegated to the AI status endpoint so the
   * panel and the AI page cannot disagree about whether AI is available.
   */
  status: () => api.get<{ enabled: boolean }>('/api/v1/ai/status'),

  /**
   * Produces assistance for one record. The entity type is a query parameter and
   * the identifier is the path, matching the server's route.
   */
  run: (entityType: AssistEntityType, entityId: string, body: AssistRequest) =>
    api.post<AssistInsight>(
      `/api/v1/ai/assist/${entityId}?entity_type=${encodeURIComponent(entityType)}`,
      body,
    ),

  /**
   * Returns the facts the model would be given, without calling it. Used to show
   * "what the assistant can see" before a request is made.
   */
  context: (entityType: AssistEntityType, entityId: string) =>
    api.get<AssistContext>(
      `/api/v1/ai/assist/${entityId}/context?entity_type=${encodeURIComponent(entityType)}`,
    ),
}
