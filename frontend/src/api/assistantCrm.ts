import { api } from './client'
import type { MCPTool } from './ai'

// Explicit allowlist: a new or reclassified tool never becomes available merely
// because a model names it. Writes must go through the agent approval runtime.
const READ_TOOLS = new Set(['search_accounts', 'account_summary', 'list_tasks', 'pipeline_overview', 'get_report'])

type Plan = { tool: string; arguments: Record<string, unknown> }

export async function readCrm(question: string, can: (permission: string) => boolean, signal: AbortSignal): Promise<{ tool: string; result: unknown }> {
  const catalogue = await api.get<{ tools: MCPTool[] }>('/api/v1/mcp/tools', undefined, signal)
  const tools = catalogue.tools.filter((tool) => READ_TOOLS.has(tool.name) && !tool.dangerous && can(tool.permission))
  if (!tools.length) throw new Error('No permitted CRM read tools are available.')
  const response = await api.request<{ content: string }>('/api/v1/ai/tasks', {
    method: 'POST', signal,
    body: {
      prompt: question,
      system: `Choose exactly one tool from this JSON list for the user's request: ${JSON.stringify(tools.map(({ name, description, input_schema }) => ({ name, description, input_schema })))}. Return only JSON with keys "tool" and "arguments". If the question cannot be answered by exactly one listed tool with known arguments, return {"tool":"","arguments":{}}. Do not guess IDs or invent arguments. Do not answer the user or propose changes.`,
      max_tokens: 512,
      temperature: 0,
    },
  })
  let plan: Plan
  try {
    const parsed: unknown = JSON.parse(response.content)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error()
    const candidate = parsed as Record<string, unknown>
    if (typeof candidate.tool !== 'string' || !candidate.arguments || typeof candidate.arguments !== 'object' || Array.isArray(candidate.arguments)) throw new Error()
    plan = { tool: candidate.tool, arguments: candidate.arguments as Record<string, unknown> }
  } catch {
    throw new Error('The AI did not return a valid CRM query. Please rephrase your request.')
  }
  const selected = tools.find((tool) => tool.name === plan.tool)
  if (!selected) {
    throw new Error('No permitted read tool matched. Please provide more details or use the CRM directly.')
  }
  const properties = selected.input_schema.properties
  if (!properties || typeof properties !== 'object' || Array.isArray(properties) || Object.keys(plan.arguments).some((key) => key === 'confirm' || !Object.hasOwn(properties, key))) {
    throw new Error('The AI returned unsupported tool arguments. Please rephrase your request.')
  }
  // The server checks the authenticated principal and organization again. Never
  // send confirm=true here; a write cannot be authorized by a model response.
  const responseTool = await api.request<{ content: unknown; is_error?: boolean }>('/api/v1/mcp/call', {
    method: 'POST', signal, body: { name: plan.tool, arguments: plan.arguments },
  })
  if (responseTool.is_error) throw new Error('The CRM tool could not complete the request.')
  return { tool: plan.tool, result: responseTool.content }
}
