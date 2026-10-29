/**
 * Workflow automation.
 *
 * A workflow is a trigger plus an ordered list of steps. The engine runs it as its
 * author, with that author's permissions resolved on every run, so the UI never
 * offers a step the author could not perform — but the server is what enforces it,
 * and a refusal is surfaced rather than hidden.
 */
import { api, type ListResponse, type PaginationParams } from './client'

export interface Workflow {
  id: string
  organization_id: string
  name: string
  description?: string
  definition: { steps: WorkflowStep[] }
  definition_schema_version: number
  enabled: boolean
  trigger_type: 'event' | 'schedule' | 'manual'
  event_type?: string
  schedule_cron?: string
  schedule_timezone?: string
  next_run_at?: string
  created_by: string
  max_depth: number
  created_at: string
  updated_at: string
  archived_at?: string
}

export interface WorkflowStep {
  id: string
  type: string
  args?: Record<string, unknown>
}

export interface WorkflowRun {
  id: string
  workflow_id: string
  organization_id: string
  trigger_event_id?: string
  trigger_type: string
  status: 'running' | 'succeeded' | 'failed' | 'skipped' | 'paused'
  run_depth: number
  correlation_id?: string
  summary?: string
  error?: string
  steps_total: number
  steps_succeeded: number
  steps_failed: number
  next_step_index: number
  paused_until?: string
  started_at: string
  finished_at?: string
}

export interface WorkflowStepRun {
  id: string
  run_id: string
  step_index: number
  step_id: string
  step_type: string
  status: 'succeeded' | 'failed' | 'skipped'
  detail: Record<string, unknown>
  error?: string
  started_at: string
  finished_at?: string
}

export interface RunResult {
  run_id?: string
  status: string
  summary?: string
}

export interface WorkflowInput {
  name: string
  description?: string
  definition: { steps: WorkflowStep[] }
  trigger_type: 'event' | 'schedule' | 'manual'
  event_type?: string
  schedule_cron?: string
  schedule_timezone?: string
  max_depth?: number
}

export const workflowsApi = {
  list: (params?: PaginationParams & { include_archived?: boolean }) =>
    api.get<ListResponse<Workflow>>('/api/v1/workflows', params),

  get: (id: string) => api.get<Workflow>(`/api/v1/workflows/${id}`),

  create: (body: WorkflowInput) => api.post<Workflow>('/api/v1/workflows', body),

  update: (id: string, body: WorkflowInput) => api.put<Workflow>(`/api/v1/workflows/${id}`, body),

  setEnabled: (id: string, enabled: boolean) =>
    api.put<Workflow>(`/api/v1/workflows/${id}/enabled`, { enabled }),

  archive: (id: string) => api.delete(`/api/v1/workflows/${id}`),

  /** Runs on demand. Returns a paused result when the workflow delays. */
  run: (id: string) => api.post<RunResult>(`/api/v1/workflows/${id}/run`),

  runs: (id: string, params?: PaginationParams & { status?: string }) =>
    api.get<ListResponse<WorkflowRun>>(`/api/v1/workflows/${id}/runs`, params),

  runDetail: (runId: string) => api.get<WorkflowRun>(`/api/v1/workflow-runs/${runId}`),

  // The step log is returned as a bare list, not a paginated envelope: a run has at
  // most the definition's step count, so pagination would be ceremony.
  runSteps: (runId: string) =>
    api.get<{ items: WorkflowStepRun[] }>(`/api/v1/workflow-runs/${runId}/steps`),
}
