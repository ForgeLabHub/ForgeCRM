/**
 * Saved list views: named filter/sort selections for the CRM list screens.
 *
 * The stored `filters` object is exactly the query-parameter set the list
 * endpoints already accept, so applying a view is handing those parameters back
 * to the same endpoint the screen would have called by hand. That keeps one
 * definition of what a filter means, and a view cannot name a filter no endpoint
 * honours.
 */
import { api, type ListResponse } from './client'

/** A CRM list screen a view can be saved for. */
export type ViewEntityType = 'accounts' | 'contacts' | 'leads' | 'deals' | 'tasks' | 'tickets'

/**
 * The stored filter document. Every field is optional; an empty object is a valid
 * view that shows everything.
 */
export interface ViewFilters {
  q?: string
  status?: string
  owner_user_id?: string
  team_id?: string
  account_id?: string
  pipeline_id?: string
  stage_id?: string
  assignee_id?: string
  from?: string
  to?: string
  include_archived?: boolean
  sort?: string
  columns?: string[]
}

export interface SavedView {
  id: string
  organization_id: string
  owner_user_id: string
  entity_type: ViewEntityType
  name: string
  shared: boolean
  pinned: boolean
  filters: ViewFilters
  created_at: string
  updated_at: string
  /** Derived by the server: true when the caller owns the view. */
  is_mine: boolean
}

export interface SavedViewInput {
  entity_type: ViewEntityType
  name: string
  shared?: boolean
  pinned?: boolean
  filters?: ViewFilters
}

export const viewsApi = {
  list: (entityType: ViewEntityType) =>
    api.get<ListResponse<SavedView>>('/api/v1/views', { entity_type: entityType }),
  get: (id: string) => api.get<SavedView>(`/api/v1/views/${id}`),
  create: (body: SavedViewInput) => api.post<SavedView>('/api/v1/views', body),
  update: (id: string, body: SavedViewInput) => api.put<SavedView>(`/api/v1/views/${id}`, body),
  remove: (id: string) => api.delete(`/api/v1/views/${id}`),
}
