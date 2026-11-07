import { afterEach, describe, expect, it, vi } from 'vitest'
import { askAssistant } from './assistantCrm'
import { csrfTokenStore } from './client'

const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })

afterEach(() => csrfTokenStore.clear())

describe('assistant API contract', () => {
  it('reads CRM using the real HTTP client, then creates only an approval proposal', async () => {
    csrfTokenStore.set('csrf-example')
    const calls: { path: string; method: string; body: Record<string, unknown>; csrf: string | null }[] = []
    const fetchStub = vi.fn(async (url: RequestInfo | URL, init?: RequestInit) => {
      const path = String(url)
      const body = init?.body ? JSON.parse(String(init.body)) as Record<string, unknown> : {}
      calls.push({ path, method: init?.method ?? 'GET', body, csrf: new Headers(init?.headers).get('X-CSRF-Token') })
      if (path === '/api/v1/mcp/tools') return json({ tools: [
        { name: 'search_accounts', dangerous: false, permission: 'accounts.read', description: 'search', input_schema: { properties: { query: { type: 'string' } } } },
        { name: 'create_task', dangerous: true, permission: 'tasks.create', description: 'task', input_schema: { properties: { title: { type: 'string' }, account_id: { type: 'string' } } } },
      ] })
      if (path === '/api/v1/mcp/call') return json({ content: [{ type: 'text', text: '[{"id":"account-id","name":"Acme"}]' }] })
      if (path === '/api/v1/ai/tasks') {
        const prompt = JSON.parse(String(body.prompt)) as { results: unknown[] }
        return json({ content: prompt.results.length === 0
          ? JSON.stringify({ tool: 'search_accounts', arguments: { query: 'Acme' } })
          : JSON.stringify({ tool: 'create_task', arguments: { title: 'Follow up', account_id: 'account-id' } }) })
      }
      if (path === '/api/v1/agent/runs') return json({ action: { id: 'proposal-id' } })
      throw new Error(`Unexpected endpoint ${path}`)
    })
    vi.stubGlobal('fetch', fetchStub)
    expect(await askAssistant('Follow up with Acme', () => true, new AbortController().signal)).toEqual({ kind: 'proposal', actionId: 'proposal-id' })
    expect(calls.map((call) => call.path)).toEqual(['/api/v1/mcp/tools', '/api/v1/ai/tasks', '/api/v1/mcp/call', '/api/v1/ai/tasks', '/api/v1/agent/runs'])
    expect(calls.filter((call) => call.method === 'POST').every((call) => call.csrf === 'csrf-example')).toBe(true)
    expect(calls[4].body).toMatchObject({ tool_name: 'create_task', arguments: { title: 'Follow up', account_id: 'account-id' } })
    expect(JSON.stringify(calls[4].body)).not.toContain('confirm')
  })
})
