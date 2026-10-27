/** Email and Telegram configuration and send logs. */
import { api, type ListResponse, type PaginationParams } from './client'

export interface EmailStatus {
  enabled: boolean
  configured: boolean
  host?: string
  port?: number
  from?: string
  tls?: boolean
  last_error?: string
}

export interface EmailLogEntry {
  id: string
  to_address: string
  subject?: string
  template?: string
  status: string
  attempts: number
  error?: string
  created_at: string
  sent_at?: string
}

export const emailApi = {
  status: () => api.get<EmailStatus>('/api/v1/admin/email/status'),

  /** Verifies SMTP configuration and sends a message to the given address. */
  test: (to: string) =>
    api.post<{ ok: boolean; message?: string }>('/api/v1/admin/email/test', { to }),

  send: (body: { to: string; subject: string; body: string }) =>
    api.post<{ id: string }>('/api/v1/admin/email/send', body),

  log: (params?: PaginationParams & { status?: string }) =>
    api.get<ListResponse<EmailLogEntry>>('/api/v1/admin/email/log', params),
}

export interface TelegramStatus {
  enabled: boolean
  configured: boolean
  bot_username?: string
  webhook_set?: boolean
  last_error?: string
}

export const telegramApi = {
  status: () => api.get<TelegramStatus>('/api/v1/admin/telegram/status'),

  /** Issues a short-lived code the user sends to the bot to link their account. */
  linkCode: () => api.post<{ code: string; expires_at: string }>('/api/v1/admin/telegram/link-code'),

  unlink: () => api.delete('/api/v1/admin/telegram/link'),
}
