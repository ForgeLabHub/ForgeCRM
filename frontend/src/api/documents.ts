/**
 * File attachments.
 *
 * A document is a file bound to an organization and optionally to one CRM record.
 * The server decides the object key from the organization and document ids, so a
 * filename contributes at most an extension and cannot influence the bucket layout.
 * Nothing here builds a download URL by hand: the server returns one, and it is
 * re-checked on every request.
 */
import { api } from './client'

/** The CRM entities an attachment may be bound to. At most one may be set. */
export type DocumentEntityType = 'account' | 'contact' | 'deal' | 'ticket' | 'lead' | 'task'

export interface DocumentRecord {
  id: string
  organization_id: string
  filename: string
  content_type: string
  size_bytes: number
  checksum_sha256: string
  title?: string
  account_id?: string
  deal_id?: string
  contact_id?: string
  ticket_id?: string
  lead_id?: string
  task_id?: string
  uploaded_by?: string
  created_at: string
  archived_at?: string
  /** A server-built path that streams the file through the permission check. */
  download_url: string
}

export interface UploadDocumentInput {
  file: File
  title?: string
  entityType?: DocumentEntityType
  entityId?: string
}

export const documentsApi = {
  list: (params?: { include_archived?: boolean }) =>
    api.get<{ items: DocumentRecord[] }>('/api/v1/documents', params),

  get: (id: string) => api.get<DocumentRecord>(`/api/v1/documents/${id}`),

  /**
   * Uploads a file. The multipart body is built here so the api client keeps its
   * generated boundary; the entity id is named after the entity type, which the
   * server maps through a fixed switch rather than trusting a column name.
   */
  upload: ({ file, title, entityType, entityId }: UploadDocumentInput) => {
    const form = new FormData()
    form.append('file', file)
    if (title) form.append('title', title)
    if (entityType && entityId) form.append(`${entityType}_id`, entityId)
    return api.post<DocumentRecord>('/api/v1/documents', form)
  },

  archive: (id: string) => api.delete(`/api/v1/documents/${id}`),

  /** A time-limited presigned URL, for a client that fetches directly from the store. */
  presignedURL: (id: string) =>
    api.get<{ url: string; expires_at: string }>(`/api/v1/documents/${id}/url`),
}
