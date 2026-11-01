/**
 * Types mirroring the server's JSON contracts.
 *
 * These are written by hand against the Go structs. They are the contract the
 * API actually speaks, so a mismatch is a real bug rather than a nuisance: the
 * alternative, generating them from an OpenAPI document, needs the document to
 * exist first.
 */

export interface User {
  id: string
  email: string
  display_name: string
  status: string
  is_platform_owner: boolean
  mfa_enabled: boolean
  last_login_at?: string
}

export interface OrganizationSummary {
  id: string
  name: string
}

export interface MeResponse {
  user: User
  organization: OrganizationSummary
  permissions: string[]
  teams: string[]
  csrf_token?: string
}

export interface LoginResponse {
  user?: User
  csrf_token?: string
  mfa_required: boolean
  mfa_ticket?: string
  expires_at?: string
}

export type CustomFields = Record<string, unknown>

export interface Account {
  id: string
  organization_id: string
  name: string
  legal_name?: string
  industry?: string
  website?: string
  phone?: string
  email?: string
  tax_id?: string
  address_line1?: string
  city?: string
  region?: string
  postal_code?: string
  country?: string
  annual_revenue?: number
  employee_count?: number
  owner_user_id?: string
  team_id?: string
  external_source?: string
  external_id?: string
  custom_fields: CustomFields
  created_at: string
  updated_at: string
  archived_at?: string
}

export interface AccountInput {
  name: string
  legal_name?: string | null
  industry?: string | null
  website?: string | null
  phone?: string | null
  email?: string | null
  tax_id?: string | null
  address_line1?: string | null
  city?: string | null
  region?: string | null
  postal_code?: string | null
  country?: string | null
  annual_revenue?: number | null
  employee_count?: number | null
  owner_user_id?: string | null
  team_id?: string | null
  custom_fields?: CustomFields
}

export interface Contact {
  id: string
  organization_id: string
  account_id?: string
  first_name?: string
  last_name?: string
  email?: string
  phone?: string
  mobile?: string
  job_title?: string
  department?: string
  is_primary: boolean
  owner_user_id?: string
  team_id?: string
  external_source?: string
  external_id?: string
  custom_fields: CustomFields
  created_at: string
  updated_at: string
  archived_at?: string
}

export interface ContactInput {
  account_id?: string | null
  first_name?: string | null
  last_name?: string | null
  email?: string | null
  phone?: string | null
  mobile?: string | null
  job_title?: string | null
  department?: string | null
  is_primary?: boolean
  owner_user_id?: string | null
  team_id?: string | null
  custom_fields?: CustomFields
}

export interface Lead {
  id: string
  organization_id: string
  first_name?: string
  last_name?: string
  email?: string
  phone?: string
  company?: string
  /** The lead's job title. The server field is `title`, not `job_title`. */
  title?: string
  source?: string
  status: string
  score?: number
  notes?: string
  owner_user_id?: string
  team_id?: string
  converted_account_id?: string
  converted_contact_id?: string
  converted_at?: string
  external_source?: string
  external_id?: string
  custom_fields: CustomFields
  created_at: string
  updated_at: string
  archived_at?: string
}

export interface Pipeline {
  id: string
  organization_id: string
  name: string
  is_default: boolean
  currency: string
  created_at: string
  stages?: PipelineStage[]
  archived_at?: string
}

export interface PipelineStage {
  id: string
  pipeline_id: string
  name: string
  position: number
  probability: number
  is_won: boolean
  is_lost: boolean
}

export interface Deal {
  id: string
  organization_id: string
  pipeline_id: string
  stage_id: string
  account_id?: string
  primary_contact_id?: string
  title: string
  description?: string
  amount: number
  currency: string
  discount_percent: number
  discount_approved_by?: string
  discount_approved_at?: string
  status: string
  expected_close_date?: string
  closed_at?: string
  lost_reason?: string
  owner_user_id?: string
  team_id?: string
  external_source?: string
  external_id?: string
  custom_fields: CustomFields
  created_at: string
  updated_at: string
  archived_at?: string
}

export interface DealInput {
  pipeline_id: string
  stage_id: string
  account_id?: string | null
  primary_contact_id?: string | null
  title: string
  description?: string | null
  amount?: number
  currency?: string
  discount_percent?: number
  expected_close_date?: string | null
  owner_user_id?: string | null
  team_id?: string | null
  custom_fields?: CustomFields
}

export interface Task {
  id: string
  organization_id: string
  title: string
  description?: string
  status: string
  priority: string
  due_at?: string
  completed_at?: string
  assignee_user_id?: string
  team_id?: string
  account_id?: string
  contact_id?: string
  deal_id?: string
  ticket_id?: string
  created_at: string
  updated_at: string
  archived_at?: string
}

export interface TaskInput {
  title: string
  description?: string | null
  status?: string
  priority?: string
  due_at?: string | null
  assignee_user_id?: string | null
  team_id?: string | null
  account_id?: string | null
  contact_id?: string | null
  deal_id?: string | null
  ticket_id?: string | null
}

export interface Activity {
  id: string
  organization_id: string
  type: string
  subject: string
  body?: string
  occurred_at: string
  duration_minutes?: number
  outcome?: string
  account_id?: string
  contact_id?: string
  deal_id?: string
  ticket_id?: string
  user_id?: string
  created_at: string
}

export interface ActivityInput {
  type: string
  subject?: string | null
  body?: string | null
  occurred_at?: string | null
  duration_minutes?: number | null
  outcome?: string | null
  account_id?: string | null
  contact_id?: string | null
  deal_id?: string | null
  ticket_id?: string | null
}

export interface Ticket {
  id: string
  organization_id: string
  /** The per-organization ticket number the server assigns. */
  number: number
  subject: string
  description?: string
  status: string
  priority: string
  channel: string
  account_id?: string
  contact_id?: string
  assignee_user_id?: string
  team_id?: string
  sla_policy_id?: string
  first_response_due_at?: string
  resolution_due_at?: string
  first_responded_at?: string
  resolved_at?: string
  sla_breached: boolean
  created_at: string
  updated_at: string
  archived_at?: string
}

export interface TicketInput {
  subject: string
  description?: string | null
  status?: string
  priority?: string
  channel?: string | null
  account_id?: string | null
  contact_id?: string | null
  assignee_user_id?: string | null
  team_id?: string | null
}

export interface Tag {
  id: string
  name: string
  color: string
  created_at: string
}

export interface Comment {
  id: string
  entity_type: string
  entity_id: string
  body: string
  author_user_id?: string
  author_name?: string
  created_at: string
}

/** The field types the server accepts on a custom field definition. */
export type CustomFieldType = 'text' | 'number' | 'boolean' | 'date' | 'select' | 'multiselect'

/**
 * A tenant-defined field. Values are not stored here — a record holds them in
 * its own `custom_fields` map, keyed by this definition's `key`.
 */
export interface CustomFieldDefinition {
  id: string
  entity_type: string
  key: string
  label: string
  field_type: CustomFieldType
  /** Free-form metadata; for select fields it holds `{ choices: string[] }`. */
  options?: Record<string, unknown>
  required: boolean
  position: number
  created_at: string
}

export interface CustomFieldDefinitionInput {
  entity_type: string
  key: string
  label: string
  field_type: CustomFieldType
  options?: Record<string, unknown>
  required?: boolean
  position?: number
}

export interface Notification {
  id: string
  organization_id: string
  user_id: string
  type: string
  title: string
  body?: string
  entity_type?: string
  entity_id?: string
  read_at?: string
  created_at: string
}

export interface ImportSchemaInfo {
  format?: string
  archive?: boolean
  files?: number
  types?: Record<string, string>
  columns?: string[]
  total_rows?: number
  selected_file?: string
  sheets?: string[]
  warnings?: string[]
}

export interface ImportJob {
  id: string
  organization_id: string
  name?: string
  source_type: string
  entity_type?: string
  format?: string
  status: string
  original_filename?: string
  checksum_sha256?: string
  size_bytes?: number
  config?: Record<string, unknown>
  mapping?: Record<string, unknown>
  options?: Record<string, unknown>
  schema_info?: ImportSchemaInfo
  preview?: Record<string, unknown>[]
  report?: Record<string, unknown>
  total_rows: number
  imported_rows: number
  skipped_rows: number
  failed_rows: number
  error?: string | null
  created_by?: string
  created_at: string
  started_at?: string
  finished_at?: string
}

/** The response of POST /imports/{id}/analyze. */
export interface ImportAnalysis {
  id: string
  format?: string
  has_file?: boolean
  schema?: ImportSchemaInfo
  preview?: Record<string, unknown>[]
  error?: string
}

/** One row that could not be imported. */
export interface ImportErrorRow {
  row_number: number
  message: string
  field?: string
  value?: string
}

/** The response of GET /imports/{id}/report, and of a validate or dry-run. */
export interface ImportReport {
  id: string
  status: string
  entity_type?: string
  format?: string
  checksum_sha256?: string
  size_bytes?: number
  total_rows: number
  imported_rows: number
  skipped_rows: number
  failed_rows: number
  report?: Record<string, unknown>
  error?: string | null
  started_at?: string | null
  finished_at?: string | null
}

/** A mapping from a source column to a target field. */
export interface MappingEntry {
  /** The column in the source file. The server field is `source_column`. */
  source_column: string
  /** The importable field it feeds. The server field is `target_field`. */
  target_field: string
  required?: boolean
}

/** Import processing options, mirroring the server's `options` object. */
export interface ImportOptions {
  /** Mirrors importx.DuplicatesSkip/Update/Create/Fail on the server. */
  duplicates?: 'skip' | 'update' | 'create' | 'fail' | ''
  duplicate_key?: string[]
  external_source?: string
  delimiter?: string
  has_header?: boolean
  sheet_name?: string
  xml_row_element?: string
  default_owner_id?: string
  default_pipeline_id?: string
  default_stage_id?: string
  skip_rows?: number
  max_rows?: number
  archive_entry?: string
}

/** The writable payload shared by validate, dry-run and execute. */
export interface ImportRunRequest {
  mapping?: MappingEntry[]
  options?: ImportOptions
}

/** The response of GET /imports/fields. */
export interface ImportFieldList {
  entity_type: string
  /** The writable field names, in the order the writer expects them. */
  fields: string[]
}

/** Entity types the import writer can target. */
export const IMPORT_ENTITY_TYPES = [
  'account',
  'contact',
  'lead',
  'deal',
  'task',
  'ticket',
  'activity',
] as const

export type ImportEntityType = (typeof IMPORT_ENTITY_TYPES)[number]

export interface Job {
  id: string
  organization_id?: string
  kind: string
  status: string
  attempts: number
  max_attempts: number
  priority: number
  payload?: Record<string, unknown>
  cancel_requested: boolean
  last_error?: string
  run_after: string
  created_at: string
  updated_at: string
  started_at?: string
  finished_at?: string
}

export interface Integration {
  id: string
  organization_id: string
  name: string
  /** The adapter kind, e.g. `rest_api`. Named `kind` on the server, not `key`. */
  kind: string
  status: string
  direction: string
  config?: Record<string, unknown>
  allow_write: boolean
  timeout_seconds: number
  max_concurrency: number
  quota_per_hour?: number
  last_test_at?: string
  last_test_ok?: boolean
  last_test_error?: string
  last_sync_at?: string
  created_at: string
  archived_at?: string
  /** Whether credentials are stored, without revealing them. */
  has_secrets: boolean
}

/** The adapter's health, as returned by POST /integrations/{id}/test. */
export interface IntegrationStatus {
  healthy: boolean
  message?: string
  checked_at?: string
  last_error?: string
}

export interface IntegrationRun {
  id: string
  integration_id: string
  status: string
  operation?: string
  records_read?: number
  records_written?: number
  error?: string
  started_at: string
  finished_at?: string
}

export interface Webhook {
  id: string
  organization_id: string
  name: string
  target_url: string
  event_types: string[]
  enabled: boolean
  max_attempts: number
  created_at: string
  updated_at: string
}

export interface WebhookDelivery {
  id: string
  subscription_id: string
  event_type: string
  status: string
  attempts: number
  /** Operator replays so far. Each replay grants a fresh attempt budget. */
  replay_count: number
  response_status?: number
  /** Why the last attempt failed. Present on failed and dead deliveries. */
  last_error?: string
  created_at: string
  delivered_at?: string
  /** Set when the delivery exhausted its attempts; cleared by a replay. */
  dead_lettered_at?: string
}

export interface AuditEntry {
  /** Sequential within the audit table, not a UUID. */
  id: number
  organization_id?: string | null
  actor_type?: string
  actor_user_id?: string | null
  action: string
  entity_type?: string | null
  entity_id?: string | null
  metadata?: Record<string, unknown>
  ip?: string | null
  correlation_id?: string | null
  created_at: string
}

export interface SecurityEvent {
  id: number
  organization_id?: string | null
  user_id?: string | null
  event_type: string
  severity: string
  message?: string
  metadata?: Record<string, unknown>
  ip?: string | null
  correlation_id?: string | null
  created_at: string
}

export interface Role {
  id: string
  key: string
  name: string
  description?: string
  is_system: boolean
  permissions: string[]
  created_at: string
}

export interface Permission {
  key: string
  category: string
  description: string
}

export interface Team {
  id: string
  organization_id: string
  name: string
  description?: string
  created_at: string
  updated_at: string
}

export interface Session {
  id: string
  created_at: string
  expires_at: string
  last_seen_at?: string | null
  ip?: string | null
  user_agent?: string | null
  mfa_verified?: boolean
  current: boolean
}

export interface SearchResult {
  entity_type: string
  id: string
  title: string
  subtitle?: string
}

export interface PipelineStageStat {
  stage_id: string
  stage_name: string
  deal_count: number
  total_amount: number
  weighted_amount: number
}

export interface PipelineReport {
  pipeline_id: string
  pipeline_name: string
  currency: string
  stages: PipelineStageStat[]
  total_open: number
  total_won: number
  total_lost: number
}

export interface SalesSummaryReport {
  from?: string
  to?: string
  deals_created: number
  deals_won: number
  deals_lost: number
  won_value: number
  lost_value: number
  open_value: number
  open_count: number
  avg_deal_amount: number
  win_rate: number
}

export interface SupportSummaryReport {
  total: number
  open: number
  resolved: number
  sla_breached: number
  avg_resolution_hours: number
  by_priority: Record<string, number>
}

export interface SystemInfo {
  service: string
  version: string
  commit: string
  environment: string
  started_at: string
  uptime_seconds: number
  database: string
  server_time: string
}

/**
 * Capabilities of the running instance.
 *
 * `oidc_configured` is not `oidc`: it reports that deployment-level OIDC settings
 * are present, not that SSO can be used. A provider must also be configured and
 * enabled, so the interface must not offer it on the strength of this flag alone;
 * the providers a visitor may choose from come from the SSO providers endpoint.
 */
export interface Capabilities {
  email: boolean
  telegram: boolean
  ai: boolean
  mfa: boolean
  webhooks: boolean
  /** Tools over HTTP (`tools/list`, `tools/call`). Not a full MCP server. */
  tool_api: boolean
  /** File attachments can be stored. False when no object store is configured. */
  storage: boolean
  /** The single-object upload limit in bytes. Zero when storage is off. */
  max_upload_bytes: number
  /** OIDC settings present. Whether sign-in can be offered also needs a provider. */
  oidc_configured: boolean
  import_formats: string[]
  archive_formats: string[]
  migration_sources: string[]
  /** Canonical webhook event vocabulary. The subscription form must use this list. */
  event_types: string[]
  [key: string]: unknown
}

/** The outcome of a bulk archive: which records were archived, and which were not. */
export interface BulkArchiveResult {
  archived: string[]
  failed: { id: string; reason: string }[]
}

/** One working cycle of an agent acting on behalf of a user (ADR 0005). */
export interface AgentRun {
  id: string
  organization_id: string
  user_id: string
  goal: string
  status: string
  budget_tokens: number
  tokens_used: number
  action_cap: number
  actions_used: number
  error?: string
  created_at: string
  updated_at: string
  finished_at?: string
}

/** A proposed agent operation awaiting a human decision, or the record of one. */
export interface AgentAction {
  id: string
  run_id: string
  organization_id: string
  tool_name: string
  arguments: Record<string, unknown>
  /**
   * The hash of the arguments as proposed. A decision must echo it, so the
   * operator approves exactly what was displayed and not a re-substituted payload.
   */
  arguments_hash: string
  risk_tier: 'safe' | 'confirm' | 'destructive'
  summary: string
  status: string
  idempotency_key: string
  result?: unknown
  error?: string
  expires_at: string
  proposed_at: string
  decided_at?: string
  executed_at?: string
}

/** An append-only record of one human decision on an agent action. */
export interface AgentApproval {
  id: string
  action_id: string
  decided_by: string
  decision: string
  comment?: string
  arguments_hash: string
  /** HMAC binding the decision to the action and its arguments. */
  signature: string
  decided_at: string
}
