/** Administration: users' roles, teams, organizations, audit and diagnostics. */
import { api, type ListResponse, type PaginationParams } from './client'
import type {
  AuditEntry,
  Capabilities,
  Job,
  Permission,
  Role,
  SecurityEvent,
  Session,
  SystemInfo,
  Team,
  User,
} from '../types/api'

export interface OrganizationRow {
  id: string
  name: string
  slug: string
  status: string
  created_at: string
}

export interface TeamRow {
  id: string
  name: string
  description?: string
  created_at: string
  member_count: number
}

export const rolesApi = {
  list: () => api.get<{ items: Role[] }>('/api/v1/roles'),
  permissions: () => api.get<{ items: Permission[] }>('/api/v1/roles/permissions'),
  create: (body: { key: string; name: string; description?: string }) =>
    api.post<Role>('/api/v1/roles', body),
  setPermissions: (id: string, permissions: string[]) =>
    api.put<Role>(`/api/v1/roles/${id}/permissions`, { permissions }),
}

export const organizationsApi = {
  list: () => api.get<{ items: OrganizationRow[] }>('/api/v1/organizations'),
  create: (body: { name: string; slug: string }) =>
    api.post<OrganizationRow>('/api/v1/organizations', body),
}

export const teamsApi = {
  list: () => api.get<{ items: TeamRow[] }>('/api/v1/teams'),
  create: (body: { name: string; description?: string }) => api.post<Team>('/api/v1/teams', body),
  addMember: (id: string, userId: string) =>
    api.post(`/api/v1/teams/${id}/members`, { user_id: userId }),
}

export const auditApi = {
  list: (params?: PaginationParams & { action?: string; actor_user_id?: string; from?: string; to?: string }) =>
    api.get<ListResponse<AuditEntry>>('/api/v1/audit', params),
  securityEvents: (params?: PaginationParams & { type?: string; from?: string; to?: string }) =>
    api.get<ListResponse<SecurityEvent>>('/api/v1/security-events', params),
}

export interface LogLine {
  time: string
  level: string
  component?: string
  message: string
  actor_user_id?: string
  organization_id?: string
  correlation_id?: string
}

export const diagnosticsApi = {
  logs: (params?: PaginationParams & { level?: string; component?: string; from?: string; to?: string }) =>
    api.get<ListResponse<LogLine>>('/api/v1/admin/logs', params),
  metrics: () => api.get<Record<string, unknown>>('/api/v1/admin/metrics'),
  diagnostics: () => api.get<Record<string, unknown>>('/api/v1/admin/diagnostics'),
  settings: () => api.get<{ items: Record<string, { value: unknown; updated_at: string }> }>(
    '/api/v1/admin/settings',
  ),
  // The setting to write is a query parameter and only the value is the body,
  // matching the route: a request that puts the key in the body is rejected as a
  // missing key.
  updateSetting: (key: string, value: unknown) =>
    api.put(`/api/v1/admin/settings?key=${encodeURIComponent(key)}`, { value }),
}

export const systemApi = {
  info: () => api.get<SystemInfo>('/api/v1/system/info'),
  capabilities: () => api.get<Capabilities>('/api/v1/system/capabilities'),
}

export const jobsApi = {
  list: (params?: PaginationParams & { kind?: string; status?: string }) =>
    api.get<ListResponse<Job>>('/api/v1/jobs', params),
  // The route wraps the counts in `by_status`; reading them from the top level
  // would put an object where a number is expected and blank the page.
  stats: () => api.get<{ by_status: Record<string, number> }>('/api/v1/jobs/stats'),
  get: (id: string) => api.get<Job>(`/api/v1/jobs/${id}`),
  cancel: (id: string) => api.post(`/api/v1/jobs/${id}/cancel`),
  retry: (id: string) => api.post(`/api/v1/jobs/${id}/retry`),
}

export const sessionsApi = {
  list: () => api.get<ListResponse<Session>>('/api/v1/auth/sessions'),
  revoke: (id: string) => api.delete(`/api/v1/auth/sessions/${id}`),
}

/**
 * A registered identity provider, as the administration list reports it. The
 * client secret is never returned: `has_secret` says whether one is stored, so a
 * screen can show "configured" without holding the secret.
 */
export interface SSOProviderRow {
  id: string
  organization_id?: string
  key: string
  display_name: string
  issuer: string
  client_id: string
  scopes: string[]
  allowed_email_domains: string[]
  jit_provisioning: boolean
  default_role_key?: string
  enabled: boolean
  has_secret: boolean
}

/** The provider document a create or update sends. */
export interface SSOProviderInput {
  key: string
  display_name: string
  issuer: string
  client_id: string
  /** Omitted on update keeps the stored secret; empty never clears it. */
  client_secret?: string
  scopes?: string[]
  allowed_email_domains?: string[]
  jit_provisioning?: boolean
  default_role_key?: string
  enabled?: boolean
}

export const ssoAdminApi = {
  /** Providers plus the exact redirect URL to register with each one. */
  list: () =>
    api.get<{ items: SSOProviderRow[]; redirect_url: string }>('/api/v1/admin/sso/providers'),
  create: (body: SSOProviderInput) =>
    api.post<SSOProviderRow>('/api/v1/admin/sso/providers', body),
  update: (id: string, body: SSOProviderInput) =>
    api.put<SSOProviderRow>(`/api/v1/admin/sso/providers/${id}`, body),
  remove: (id: string) => api.delete(`/api/v1/admin/sso/providers/${id}`),
}

export type { User }
