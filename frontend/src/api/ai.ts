/**
 * AI and MCP.
 *
 * The AI layer is optional: every call here degrades to a clear "not
 * configured" state when no provider is set up, and the rest of the
 * application is unaffected.
 */
import { api, type ListResponse, type PaginationParams } from './client'

export interface AIProviderStatus {
  enabled: boolean
  provider: string
  model: string
  healthy?: boolean
  error?: string
  reason?: string
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
  input_tokens?: number | null
  output_tokens?: number | null
  error?: string
  created_by?: string
  created_at: string
  finished_at?: string
}

export const aiApi = {
  /** Whether AI is configured and whether the configured provider is healthy. */
  status: () => api.get<AIProviderStatus>('/api/v1/ai/status'),

  providers: () => api.get<{ items: { name: string; description: string; requires_api_key: boolean }[]; configured: string; note: string }>('/api/v1/ai/providers'),

  tasks: (params?: PaginationParams & { status?: string }) =>
    api.get<ListResponse<AITask>>('/api/v1/ai/tasks', params),

  createTask: (body: { prompt: string; system?: string; max_tokens?: number; temperature?: number }) =>
    api.post<{ content: string; model: string; input_tokens: number; output_tokens: number }>('/api/v1/ai/tasks', body),
}

export interface MCPTool {
  name: string
  description: string
  // The catalogue endpoint names this input_schema; the MCP protocol endpoint
  // names the same thing inputSchema. This page reads the catalogue, so it uses
  // the catalogue's name.
  input_schema: Record<string, unknown>
  dangerous: boolean
  permission: string
}

export const mcpApi = {
  /** The tools this caller is allowed to see, filtered by permission. */
  tools: () => api.get<{ tools: MCPTool[] }>('/api/v1/mcp/tools'),

  /**
   * Invokes one tool. The server re-checks the caller's permission for the
   * specific tool, so listing a tool is not enough to be able to run it.
   *
   * This posts to the single-call endpoint rather than to the JSON-RPC one. The
   * two shapes were once crossed — this function sent {name, arguments} to an
   * endpoint that decoded {method, params} — so the call arrived with no method
   * and was refused. The convenience endpoint is the one that speaks this shape.
   */
  call: (name: string, args: Record<string, unknown>, confirm = false) =>
    api.post<{ content: unknown; is_error?: boolean }>('/api/v1/mcp/call', {
      name,
      arguments: args,
      confirm,
    }),
}
