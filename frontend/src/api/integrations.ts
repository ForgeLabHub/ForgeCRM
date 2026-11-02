/**
 * Integration Hub, migrations, events and webhooks.
 *
 * Integration secrets never travel to the browser. The server returns a
 * configuration with sensitive values masked, and accepts a full replacement on
 * write, so the UI can show that a value is set without learning what it is.
 */
import { api, type ListResponse, type PaginationParams } from './client'
import type {
  Integration,
  IntegrationRun,
  Webhook,
  WebhookDelivery,
} from '../types/api'

export interface IntegrationKind {
  key: string
  name: string
  direction: string
  description?: string
  config_fields?: { key: string; label: string; required: boolean; secret?: boolean }[]
}

export const integrationsApi = {
  list: (params?: PaginationParams & { status?: string; kind?: string }) =>
    api.get<ListResponse<Integration>>('/api/v1/integrations', params),

  kinds: () => api.get<{ items: IntegrationKind[] }>('/api/v1/integrations/kinds'),

  get: (id: string) => api.get<Integration>(`/api/v1/integrations/${id}`),

  create: (body: { key: string; name: string; config: Record<string, unknown> }) =>
    api.post<Integration>('/api/v1/integrations', body),

  /** Runs the adapter's TestConnection. Does not prove data can be read. */
  test: (id: string) =>
    api.post<{ ok: boolean; message?: string; latency_ms?: number }>(`/api/v1/integrations/${id}/test`),

  setStatus: (id: string, status: 'active' | 'paused' | 'revoked') =>
    api.put<Integration>(`/api/v1/integrations/${id}/status`, { status }),

  revoke: (id: string) => api.delete(`/api/v1/integrations/${id}`),

  status: (id: string) => api.get<Record<string, unknown>>(`/api/v1/integrations/${id}/status`),

  runs: (id: string, params?: PaginationParams) =>
    api.get<ListResponse<IntegrationRun>>(`/api/v1/integrations/${id}/runs`, params),
}

/**
 * The source kind lives inside `config.kind`, not a top-level field. The handler
 * decodes the body with DisallowUnknownFields, so a body carrying `source_type`
 * was rejected before the service ever saw it — the field is named `kind` on
 * `migratex.ConnectionConfig` and echoed back as `source_kind`.
 */
export interface MigrationConnectionConfig {
  kind: string
  host?: string
  port?: number
  database?: string
  username?: string
  path?: string
  base_url?: string
  read_path?: string
  ssl_mode?: string
}

export interface MigrationJob {
  id: string
  organization_id: string
  name: string
  source_kind: string
  strategy: string
  status: string
  // jsonb columns arrive as parsed JSON, not as JSON text: config is an object and
  // source_tables an array. Typing them as strings made the detail dialog render
  // "[object Object]" for a populated config.
  config?: Record<string, unknown>
  mapping?: Record<string, unknown>
  watermark?: Record<string, unknown>
  report?: Record<string, unknown>
  source_tables?: string[]
  created_at: string
  last_run_at?: string | null
  error?: string | null
}

/** Shape of the source schema `discover` reads back: `{tables, read_only}`. */
export interface MigrationDiscovery {
  tables: {
    name: string
    rows: number
    columns: { name: string; type: string; nullable: boolean; primary_key: boolean }[]
  }[]
  read_only: boolean
}

export interface MigrationReconciliation {
  id: string
  source_table: string
  target_entity: string
  source_count: number
  target_count: number
  matched_count: number
  mismatched_count: number
  status: string
  details?: Record<string, unknown>
  created_at: string
}

export const migrationsApi = {
  list: (params?: PaginationParams & { status?: string }) =>
    api.get<ListResponse<MigrationJob>>('/api/v1/migrations', params),

  get: (id: string) => api.get<MigrationJob>(`/api/v1/migrations/${id}`),

  create: (body: {
    name: string
    config: MigrationConnectionConfig
    strategy?: string
    password?: string
    source_tables?: string[]
  }) => api.post<MigrationJob>('/api/v1/migrations', body),

  /** Reads the source schema. Requires read-only credentials. */
  discover: (id: string) =>
    api.post<MigrationDiscovery>(`/api/v1/migrations/${id}/discover`),

  /**
   * The handler decodes `{source_table, target_entity}` from the body and rejects
   * anything else, so a request without them answers 400 — the bare call this
   * replaced never reconciled anything.
   */
  reconcile: (id: string, body: { source_table: string; target_entity: string }) =>
    api.post<MigrationReconciliation>(`/api/v1/migrations/${id}/reconciliation`, body),

  reconciliations: (id: string, params?: PaginationParams) =>
    api.get<ListResponse<MigrationReconciliation>>(`/api/v1/migrations/${id}/reconciliation`, params),
}

export interface CreateWebhookBody {
  name: string
  target_url: string
  event_types: string[]
  secret?: string
  max_attempts?: number
}

export const webhooksApi = {
  list: (params?: PaginationParams) => api.get<ListResponse<Webhook>>('/api/v1/webhooks', params),

  create: (body: CreateWebhookBody) => api.post<Webhook>('/api/v1/webhooks', body),

  setEnabled: (id: string, enabled: boolean) =>
    api.put<Webhook>(`/api/v1/webhooks/${id}/enabled`, { enabled }),

  deliveries: (id: string, params?: PaginationParams) =>
    api.get<ListResponse<WebhookDelivery>>(`/api/v1/webhooks/${id}/deliveries`, params),

  replay: (id: string, deliveryId: string) =>
    api.post(`/api/v1/webhooks/${id}/deliveries/${deliveryId}/replay`),

  /** Sends a signed test delivery to the configured target. */
  test: (id: string) =>
    api.post<{ ok: boolean; response_status?: number; error?: string }>(
      `/api/v1/webhooks/${id}/test`,
    ),
}

export interface EventRow {
  id: string
  event_type: string
  source?: string
  schema_version?: number
  correlation_id?: string
  occurred_at: string
  payload?: Record<string, unknown>
}

export const eventsApi = {
  list: (params?: PaginationParams & { event_type?: string; source?: string }) =>
    api.get<ListResponse<EventRow>>('/api/v1/events', params),
}
