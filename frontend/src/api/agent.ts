/**
 * Agent runs and human approvals (ADR 0005).
 *
 * The agent proposes; a human decides. Nothing a model produces takes effect
 * until a person with the review right approves it, and a destructive action
 * additionally requires that the deciding session presented a second factor
 * recently. The step-up is not part of the decision body: the server reads it from
 * the session, so a client cannot assert it.
 */
import { api, type ListResponse, type PaginationParams } from './client'
import type { AgentAction, AgentApproval, AgentRun } from '../types/api'

export const agentApi = {
  runs: (params?: PaginationParams & { status?: string }) =>
    api.get<ListResponse<AgentRun>>('/api/v1/agent/runs', params),

  run: (id: string) => api.get<AgentRun>(`/api/v1/agent/runs/${id}`),

  startRun: (body: { goal: string; budget_tokens?: number; action_cap?: number }) =>
    api.post<AgentRun>('/api/v1/agent/runs', body),

  cancelRun: (id: string) => api.post<AgentRun>(`/api/v1/agent/runs/${id}/cancel`),

  addTokens: (id: string, tokens: number) =>
    api.post<AgentRun>(`/api/v1/agent/runs/${id}/tokens`, { tokens }),

  /** Proposed actions, newest first. Filter by status to find the pending queue. */
  actions: (params?: PaginationParams & { status?: string; run_id?: string }) =>
    api.get<ListResponse<AgentAction>>('/api/v1/agent/actions', params),

  action: (id: string) => api.get<AgentAction>(`/api/v1/agent/actions/${id}`),

  approvals: (id: string) =>
    api.get<{ items: AgentApproval[] }>(`/api/v1/agent/actions/${id}/approvals`),

  /**
   * Approve or reject an action.
   *
   * `argumentsHash` must be the hash the operator was shown. The server refuses a
   * decision that does not match the stored hash, so an approval cannot be replayed
   * against different arguments.
   */
  decide: (
    id: string,
    body: { approve: boolean; arguments_hash: string; comment?: string },
  ) => api.post<AgentAction>(`/api/v1/agent/actions/${id}/decide`, body),
}

export interface StepUpResult {
  status: string
  window_secs: number
}

/**
 * Refresh the session's second-factor window.
 *
 * The factor is a TOTP code when the account has MFA, otherwise the password. The
 * server decides which and records the refresh against the session, not the
 * client.
 */
export function stepUp(body: { code?: string; password?: string }) {
  return api.post<StepUpResult>('/api/v1/auth/step-up', body)
}
