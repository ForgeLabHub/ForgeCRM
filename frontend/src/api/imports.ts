/**
 * Data Import Center.
 *
 * The flow is deliberately split across several requests. Creating a job and
 * uploading its file are separate so that a failure in one does not waste the
 * other, and analysis, validation and execution are separate so a user can see
 * what will happen before anything is written.
 */
import { api, type ListResponse, type PaginationParams } from './client'
import type {
  ImportAnalysis,
  ImportErrorRow,
  ImportFieldList,
  ImportJob,
  ImportReport,
  ImportRunRequest,
} from '../types/api'

export interface CreateImportBody {
  name: string
  entity_type: string
  original_filename?: string
}

export const importsApi = {
  list: (params?: PaginationParams & { status?: string; entity_type?: string }) =>
    api.get<ListResponse<ImportJob>>('/api/v1/imports', params),

  get: (id: string) => api.get<ImportJob>(`/api/v1/imports/${id}`),

  create: (body: CreateImportBody) => api.post<ImportJob>('/api/v1/imports', body),

  /** Uploads the source file for a job. Returns the server-computed checksum. */
  upload: (id: string, file: File) => {
    const form = new FormData()
    form.append('file', file)
    return api.post<{ id: string; checksum_sha256: string; size_bytes: number }>(
      `/api/v1/imports/${id}/upload`,
      form,
    )
  },

  /** Parses the file and reports its structure. Safe to call repeatedly. */
  analyze: (id: string, body: ImportRunRequest = {}) =>
    api.post<ImportAnalysis>(`/api/v1/imports/${id}/analyze`, body),

  preview: (id: string) => api.get<ImportAnalysis>(`/api/v1/imports/${id}/preview`),

  /** Checks the mapping against the target schema without writing anything. */
  validate: (id: string, body: ImportRunRequest) =>
    api.post<ImportReport>(`/api/v1/imports/${id}/validate`, body),

  /** Runs the full pipeline in a transaction that is rolled back. */
  dryRun: (id: string, body: ImportRunRequest) =>
    api.post<ImportReport>(`/api/v1/imports/${id}/dry-run`, body),

  /** Queues the real import and returns immediately with a job to poll. */
  execute: (id: string, body: ImportRunRequest) => api.post<ImportJob>(`/api/v1/imports/${id}/execute`, body),

  cancel: (id: string) => api.post<ImportJob>(`/api/v1/imports/${id}/cancel`),

  errors: (id: string, params?: PaginationParams) =>
    api.get<ListResponse<ImportErrorRow>>(`/api/v1/imports/${id}/errors`, params),

  report: (id: string) => api.get<ImportReport>(`/api/v1/imports/${id}/report`),

  /** The writable fields an entity type can be mapped onto. */
  fields: (entityType: string) =>
    api.get<ImportFieldList>('/api/v1/imports/fields', { entity_type: entityType }),
}

export const exportsApi = {
  create: (body: { entity_type: string; format?: string; filters?: Record<string, unknown> }) =>
    api.post<{ id: string }>('/api/v1/exports', body),
}
