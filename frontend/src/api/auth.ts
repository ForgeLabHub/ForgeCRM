/** Auth, session and identity endpoints. */
import { api, csrfTokenStore, type ListResponse } from './client'
import type { LoginResponse, MeResponse, Session, User } from '../types/api'

export const authApi = {
  /**
   * Signs in. The session cookie is set by the server; the CSRF token in the
   * response is kept in memory so subsequent mutations can be sent.
   */
  async login(email: string, password: string): Promise<LoginResponse> {
    const result = await api.post<LoginResponse>(
      '/api/v1/auth/login',
      { email, password },
      // No CSRF header: there is no session yet to bind the token to.
      { skipCSRF: true },
    )
    if (!result.mfa_required) csrfTokenStore.set(result.csrf_token)
    return result
  },

  /** Completes a second-factor challenge and establishes the session. */
  async verifyMFA(ticket: string, code: string): Promise<LoginResponse> {
    const result = await api.post<LoginResponse>(
      '/api/v1/auth/mfa/verify',
      { ticket, code },
      { skipCSRF: true },
    )
    csrfTokenStore.set(result.csrf_token)
    return result
  },

  /**
   * Loads the current session. Called on every page load, so it also refreshes
   * the CSRF token that a reload would otherwise lose.
   */
  async me(): Promise<MeResponse> {
    const me = await api.get<MeResponse>('/api/v1/auth/me')
    csrfTokenStore.set(me.csrf_token)
    return me
  },

  async logout(): Promise<void> {
    try {
      await api.post('/api/v1/auth/logout')
    } finally {
      // Clear regardless of the outcome: keeping a token for a session that may
      // already be gone would only cause confusing 403s.
      csrfTokenStore.clear()
    }
  },

  async requestPasswordReset(email: string): Promise<void> {
    await api.post('/api/v1/auth/password/forgot', { email }, { skipCSRF: true })
  },

  async resetPassword(token: string, newPassword: string): Promise<void> {
    await api.post(
      '/api/v1/auth/password/reset',
      { token, new_password: newPassword },
      { skipCSRF: true },
    )
  },

  async changePassword(currentPassword: string, newPassword: string): Promise<void> {
    await api.post('/api/v1/auth/password/change', {
      current_password: currentPassword,
      new_password: newPassword,
    })
  },

  listSessions(): Promise<ListResponse<Session>> {
    return api.get<ListResponse<Session>>('/api/v1/auth/sessions')
  },

  revokeSession(id: string): Promise<void> {
    return api.delete(`/api/v1/auth/sessions/${id}`)
  },

  async enrollMFA(): Promise<{ secret: string; otpauth_url: string }> {
    return api.post('/api/v1/auth/mfa/enroll')
  },

  async confirmMFA(code: string): Promise<{ recovery_codes: string[] }> {
    return api.post('/api/v1/auth/mfa/confirm', { code })
  },

  async disableMFA(password: string): Promise<void> {
    await api.post('/api/v1/auth/mfa/disable', { password })
  },
}

export const usersApi = {
  list: (params?: { q?: string; limit?: number; offset?: number }) =>
    api.get<ListResponse<User>>('/api/v1/users', params),
  get: (id: string) => api.get<User>(`/api/v1/users/${id}`),
  invite: (body: { email: string; display_name: string; role_keys?: string[] }) =>
    api.post<User>('/api/v1/users', body),
  update: (id: string, body: { display_name?: string; status?: string }) =>
    api.put<User>(`/api/v1/users/${id}`, body),
  block: (id: string) => api.post(`/api/v1/users/${id}/block`),
  unblock: (id: string) => api.post(`/api/v1/users/${id}/unblock`),
  assignRole: (id: string, roleKey: string) =>
    api.post(`/api/v1/users/${id}/roles`, { role_key: roleKey }),
  revokeRole: (id: string, roleId: string) => api.delete(`/api/v1/users/${id}/roles/${roleId}`),
  revokeSessions: (id: string) => api.post(`/api/v1/users/${id}/sessions/revoke`),
}
