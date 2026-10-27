/**
 * AI and MCP.
 *
 * The AI layer is optional: every call here degrades to a clear "not
 * configured" state when no provider is set up, and the rest of the
 * application is unaffected.
 */
import { api, type ListResponse, type PaginationParams } from './client'

export interface AIProviderStatus {
  key: string
  name: string
  kind: string
  enabled: boolean
  configured: boolean
  healthy?: boolean
  model?: string
  last_error?: string
}

export interface AITask {
  id: string
  organization_id?: string
  kind: string
  status: string
  prompt?: string
  result?: string
  provider?: string
  model?: string
  tokens_in?: number
  tokens_out?: number
  error?: string
  created_by?: string
  created_at: string
  finished_at?: string
}

export const aiApi = {
  /** Whether AI is available at all, and with which providers. */
  status: () => api.get<{ enabled: boolean; providers?: AIProviderStatus[] }>('/api/v1/ai/status'),

  providers: () => api.get<{ items: AIProviderStatus[] }>('/api/v1/ai/providers'),

  tasks: (params?: PaginationParams & { status?: string }) =>
    api.get<ListResponse<AITask>>('/api/v1/ai/tasks', params),

  createTask: (body: { kind: string; prompt?: string; input?: Record<string, unknown> }) =>
    api.post<AITask>('/api/v1/ai/tasks', body),
}

export interface MCPTool {
  name: string
  description: string
  input_schema: Record<string, unknown>
}

export const mcpApi = {
  /** The tools this caller is allowed to see, filtered by permission. */
  tools: () => api.get<{ items: MCPTool[] }>('/api/v1/mcp/tools'),

  /**
   * Invokes one tool. The server re-checks the caller's permission for the
   * specific tool, so listing a tool is not enough to be able to run it.
   */
  call: (name: string, args: Record<string, unknown>) =>
    api.post<{ content: unknown; is_error?: boolean }>('/api/v1/mcp', {
      name,
      arguments: args,
    }),
}
