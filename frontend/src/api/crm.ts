/** CRM entity endpoints. */
import { api, type ListResponse, type PaginationParams } from './client'
import type {
  Account,
  AccountInput,
  Activity,
  ActivityInput,
  Comment,
  Contact,
  ContactInput,
  Deal,
  DealInput,
  Lead,
  Notification,
  Pipeline,
  PipelineReport,
  SearchResult,
  SupportSummaryReport,
  SalesSummaryReport,
  BulkArchiveResult,
  CustomFieldDefinition,
  CustomFieldDefinitionInput,
  Tag,
  Task,
  TaskInput,
  Ticket,
  TicketInput,
} from '../types/api'

/** Standard list filters the CRM handlers understand. */
export interface CRMListParams extends PaginationParams {
  status?: string
  owner_user_id?: string
  account_id?: string
  pipeline_id?: string
  stage_id?: string
  include_archived?: boolean
}

export const accountsApi = {
  list: (params?: CRMListParams) =>
    api.get<ListResponse<Account>>('/api/v1/accounts', params),
  get: (id: string) => api.get<Account>(`/api/v1/accounts/${id}`),
  create: (body: AccountInput) => api.post<Account>('/api/v1/accounts', body),
  update: (id: string, body: AccountInput) => api.put<Account>(`/api/v1/accounts/${id}`, body),
  archive: (id: string) => api.delete(`/api/v1/accounts/${id}`),
  bulkArchive: (ids: string[]) =>
    api.post<BulkArchiveResult>('/api/v1/accounts/bulk-archive', { ids }),
  contacts: (id: string, params?: CRMListParams) =>
    api.get<ListResponse<Contact>>(`/api/v1/accounts/${id}/contacts`, params),
  deals: (id: string, params?: CRMListParams) =>
    api.get<ListResponse<Deal>>(`/api/v1/accounts/${id}/deals`, params),
  activities: (id: string, params?: CRMListParams) =>
    api.get<ListResponse<Activity>>(`/api/v1/accounts/${id}/activities`, params),
}

export const contactsApi = {
  list: (params?: CRMListParams) => api.get<ListResponse<Contact>>('/api/v1/contacts', params),
  get: (id: string) => api.get<Contact>(`/api/v1/contacts/${id}`),
  create: (body: ContactInput) => api.post<Contact>('/api/v1/contacts', body),
  update: (id: string, body: ContactInput) => api.put<Contact>(`/api/v1/contacts/${id}`, body),
  archive: (id: string) => api.delete(`/api/v1/contacts/${id}`),
  bulkArchive: (ids: string[]) =>
    api.post<BulkArchiveResult>('/api/v1/contacts/bulk-archive', { ids }),
}

export const leadsApi = {
  list: (params?: CRMListParams) => api.get<ListResponse<Lead>>('/api/v1/leads', params),
  get: (id: string) => api.get<Lead>(`/api/v1/leads/${id}`),
  create: (body: Partial<Lead>) => api.post<Lead>('/api/v1/leads', body),
  update: (id: string, body: Partial<Lead>) => api.put<Lead>(`/api/v1/leads/${id}`, body),
  convert: (id: string, body: { create_deal?: boolean; pipeline_id?: string; stage_id?: string }) =>
    api.post<Lead>(`/api/v1/leads/${id}/convert`, body),
}

export const pipelinesApi = {
  list: () => api.get<{ items: Pipeline[] }>('/api/v1/pipelines'),
  create: (body: { name: string; currency?: string }) => api.post<Pipeline>('/api/v1/pipelines', body),
  report: (id: string) => api.get<PipelineReport>(`/api/v1/pipelines/${id}/report`),
}

export const dealsApi = {
  list: (params?: CRMListParams) => api.get<ListResponse<Deal>>('/api/v1/deals', params),
  get: (id: string) => api.get<Deal>(`/api/v1/deals/${id}`),
  create: (body: DealInput) => api.post<Deal>('/api/v1/deals', body),
  update: (id: string, body: DealInput) => api.put<Deal>(`/api/v1/deals/${id}`, body),
  archive: (id: string) => api.delete(`/api/v1/deals/${id}`),
  bulkArchive: (ids: string[]) =>
    api.post<BulkArchiveResult>('/api/v1/deals/bulk-archive', { ids }),
  moveStage: (id: string, stageId: string) =>
    api.post<Deal>(`/api/v1/deals/${id}/stage`, { stage_id: stageId }),
  history: (id: string) =>
    api.get<{ items: { id: string; from_stage_id?: string; to_stage_id: string; changed_at: string }[] }>(
      `/api/v1/deals/${id}/history`,
    ),
}

export const tasksApi = {
  list: (params?: CRMListParams) => api.get<ListResponse<Task>>('/api/v1/tasks', params),
  get: (id: string) => api.get<Task>(`/api/v1/tasks/${id}`),
  create: (body: TaskInput) => api.post<Task>('/api/v1/tasks', body),
  update: (id: string, body: TaskInput) => api.put<Task>(`/api/v1/tasks/${id}`, body),
}

export const activitiesApi = {
  list: (params?: CRMListParams) => api.get<ListResponse<Activity>>('/api/v1/activities', params),
  create: (body: ActivityInput) => api.post<Activity>('/api/v1/activities', body),
}

export const ticketsApi = {
  list: (params?: CRMListParams) => api.get<ListResponse<Ticket>>('/api/v1/tickets', params),
  get: (id: string) => api.get<Ticket>(`/api/v1/tickets/${id}`),
  create: (body: TicketInput) => api.post<Ticket>('/api/v1/tickets', body),
  update: (id: string, body: TicketInput) => api.put<Ticket>(`/api/v1/tickets/${id}`, body),
}

export const tagsApi = {
  list: () => api.get<{ items: Tag[] }>('/api/v1/tags'),
  create: (body: { name: string; color?: string }) => api.post<Tag>('/api/v1/tags', body),
  attach: (id: string, body: { entity_type: string; entity_id: string }) =>
    api.post(`/api/v1/tags/${id}/attach`, body),
  attached: (entityType: string, entityId: string) =>
    api.get<{ items: Tag[] }>('/api/v1/tags/attached', {
      entity_type: entityType,
      entity_id: entityId,
    }),
}

export const commentsApi = {
  list: (entityType: string, entityId: string) =>
    api.get<{ items: Comment[] }>('/api/v1/comments', {
      entity_type: entityType,
      entity_id: entityId,
    }),
  create: (body: { entity_type: string; entity_id: string; body: string }) =>
    api.post<Comment>('/api/v1/comments', body),
}

export const customFieldsApi = {
  list: (entityType: string) =>
    api.get<{ items: CustomFieldDefinition[] }>('/api/v1/custom-fields', {
      entity_type: entityType,
    }),
  create: (body: CustomFieldDefinitionInput) =>
    api.post<CustomFieldDefinition>('/api/v1/custom-fields', body),
}

export const notificationsApi = {
  list: (params?: PaginationParams) =>
    api.get<ListResponse<Notification>>('/api/v1/notifications', params),
  markRead: (id: string) => api.post(`/api/v1/notifications/${id}/read`),
}

export const searchApi = {
  search: (term: string, limit?: number) =>
    api.get<{ items: SearchResult[] }>('/api/v1/search', { q: term, limit }),
}

export const reportsApi = {
  salesSummary: (params?: { from?: string; to?: string }) =>
    api.get<SalesSummaryReport>('/api/v1/reports/sales-summary', params),
  supportSummary: (params?: { from?: string; to?: string }) =>
    api.get<SupportSummaryReport>('/api/v1/reports/support-summary', params),
}
