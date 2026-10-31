/**
 * Organization configuration: the defaults a tenant chooses over the platform's.
 *
 * The catalog is closed and lives on the server, so this client does not restate
 * the keys or their kinds — it reads them. That is deliberate: a hard-coded list
 * here would drift from the server's the first time a key was added, and the
 * editor would offer a setting the server refuses. The catalog endpoint carries
 * the kind, the bounds and the default for each key, which is everything needed
 * to render the right editor without a second description of the same thing.
 *
 * Two facts the interface relies on:
 *   - `has_override` distinguishes "the organization chose this" from "this is the
 *     platform default". A value equal to the default is not the same as no value,
 *     so the UI reads the flag rather than comparing values.
 *   - a write returns the setting's new state, so the page shows the effective
 *     value the server resolved rather than the one it guessed it sent.
 */
import { api, type ListResponse } from './client'

/** The kind of a setting's value. Closed; the server declares which keys are which. */
export type SettingKind = 'string' | 'enum' | 'integer' | 'boolean'

/** One setting, as the server describes it: definition, default, override, effective. */
export interface TenantSetting {
  key: string
  title: string
  description: string
  kind: SettingKind
  /** Permitted values, for `enum` settings. */
  enum?: string[]
  min?: number
  max?: number
  max_length?: number
  /** The value that applies when the organization has no override. */
  default: unknown
  /** The organization's own value, present only when `has_override` is true. */
  override?: unknown
  has_override: boolean
  /** What actually applies now: the override if present, else the default. */
  effective: unknown
  schema_version: number
  stored_under_older_schema?: boolean
  override_updated_at?: string
  override_updated_by?: string
}

/** One entry in a setting's change history. */
export interface TenantSettingRevision {
  id: string
  organization_id: string
  key: string
  revision: number
  value?: unknown
  reset_to_default: boolean
  previous_value?: unknown
  schema_version: number
  changed_by?: string
  created_at: string
}

export const tenantSettingsApi = {
  /** The catalog with the organization's overrides layered on. */
  list: () => api.get<ListResponse<TenantSetting>>('/api/v1/settings'),
  /** The catalog with defaults only, for rendering editors before a value exists. */
  catalog: () => api.get<ListResponse<TenantSetting>>('/api/v1/settings/catalog'),
  get: (key: string) => api.get<TenantSetting>(`/api/v1/settings/${encodeURIComponent(key)}`),
  history: (key: string, limit = 50) =>
    api.get<ListResponse<TenantSettingRevision>>(
      `/api/v1/settings/${encodeURIComponent(key)}/history`,
      { limit },
    ),
  set: (key: string, value: unknown) =>
    api.put<TenantSetting>(`/api/v1/settings/${encodeURIComponent(key)}`, { value }),
  reset: (key: string) =>
    api.delete<TenantSetting>(`/api/v1/settings/${encodeURIComponent(key)}`),
}
