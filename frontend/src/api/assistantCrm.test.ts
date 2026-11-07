import { describe, expect, it, vi } from 'vitest'
import { api } from './client'
import { askAssistant } from './assistantCrm'

const read = { name: 'list_tasks', description: 'List tasks', dangerous: false, permission: 'tasks.read', input_schema: { type: 'object', properties: { limit: { type: 'integer' } } } }
const write = { name: 'create_task', description: 'Create task', dangerous: true, permission: 'tasks.create', input_schema: { type: 'object', properties: { title: { type: 'string' } } } }
const signal = new AbortController().signal

describe('assistant tool selection', () => {
  it('reads permitted data and sends it to the model for an answer', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ tools: [read, write] })
    const request = vi.spyOn(api, 'request').mockResolvedValueOnce({ content: '{"tool":"list_tasks","arguments":{"limit":5}}' }).mockResolvedValueOnce({ content: [{ text: 'task data' }] }).mockResolvedValueOnce({ content: '{"tool":"","arguments":{},"answer":"There are tasks."}' })
    expect(await askAssistant('List tasks', () => true, signal)).toEqual({ kind: 'answer', text: 'There are tasks.' })
    expect(request.mock.calls[1][1]?.body).toEqual({ name: 'list_tasks', arguments: { limit: 5 } })
    expect(JSON.stringify(request.mock.calls[2])).toContain('task data')
  })

  it('can read an account before proposing a task for it', async () => {
    const accounts = { name: 'search_accounts', description: 'Search accounts', dangerous: false, permission: 'accounts.read', input_schema: { properties: { query: { type: 'string' } } } }
    const task = { ...write, input_schema: { properties: { title: { type: 'string' }, account_id: { type: 'string' } } } }
    vi.spyOn(api, 'get').mockResolvedValue({ tools: [accounts, task] })
    const request = vi.spyOn(api, 'request')
      .mockResolvedValueOnce({ content: '{"tool":"search_accounts","arguments":{"query":"Acme"}}' })
      .mockResolvedValueOnce({ content: [{ text: 'Acme: account-1' }] })
      .mockResolvedValueOnce({ content: '{"tool":"create_task","arguments":{"title":"Follow up","account_id":"account-1"}}' })
      .mockResolvedValueOnce({ action: { id: 'a1' } })
    expect(await askAssistant('Create a follow-up task for Acme', () => true, signal)).toEqual({ kind: 'proposal', actionId: 'a1' })
    expect(request.mock.calls[1][0]).toBe('/api/v1/mcp/call')
    expect(request.mock.calls[3][0]).toBe('/api/v1/agent/runs')
    expect(request.mock.calls[3][1]?.body).toMatchObject({ tool_name: 'create_task', arguments: { account_id: 'account-1' } })
    expect(JSON.stringify(request.mock.calls[3])).not.toContain('confirm')
  })

  it('stops repeated reads instead of looping indefinitely', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ tools: [read] })
    const request = vi.spyOn(api, 'request')
      .mockResolvedValueOnce({ content: '{"tool":"list_tasks","arguments":{"limit":5}}' })
      .mockResolvedValueOnce({ content: [] })
      .mockResolvedValueOnce({ content: '{"tool":"list_tasks","arguments":{"limit":5}}' })
    await expect(askAssistant('tasks', () => true, signal)).rejects.toThrow(/repeated a CRM read/)
    expect(request).toHaveBeenCalledTimes(3)
  })

  it('bounds the number of distinct reads', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ tools: [read] })
    const request = vi.spyOn(api, 'request')
    for (let i = 1; i <= 4; i++) {
      request.mockResolvedValueOnce({ content: JSON.stringify({ tool: 'list_tasks', arguments: { limit: i } }) })
      if (i < 4) request.mockResolvedValueOnce({ content: [] })
    }
    await expect(askAssistant('tasks', () => true, signal)).rejects.toThrow(/too many CRM reads/)
    expect(request.mock.calls.filter(([path]) => path === '/api/v1/mcp/call')).toHaveLength(3)
  })

  it('does not submit an oversized CRM result to the model', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ tools: [read] })
    const request = vi.spyOn(api, 'request')
      .mockResolvedValueOnce({ content: '{"tool":"list_tasks","arguments":{"limit":5}}' })
      .mockResolvedValueOnce({ content: 'x'.repeat(33_000) })
    await expect(askAssistant('tasks', () => true, signal)).rejects.toThrow(/too large/)
    expect(request).toHaveBeenCalledTimes(2)
  })

  it('creates a proposal, never a confirmed tool call', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ tools: [read, write] })
    const request = vi.spyOn(api, 'request').mockResolvedValueOnce({ content: '{"tool":"create_task","arguments":{"title":"Follow up"}}' }).mockResolvedValueOnce({ action: { id: 'a1' } })
    expect(await askAssistant('Create a task', () => true, signal)).toEqual({ kind: 'proposal', actionId: 'a1' })
    expect(request.mock.calls[1][0]).toBe('/api/v1/agent/runs')
    expect(JSON.stringify(request.mock.calls[1])).not.toContain('confirm')
  })

  it('refuses a model-selected write when the user lacks permission', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ tools: [read, write] })
    const request = vi.spyOn(api, 'request').mockResolvedValue({ content: '{"tool":"create_task","arguments":{"title":"bad"}}' })
    await expect(askAssistant('create task', (p) => p !== 'tasks.create', signal)).rejects.toThrow(/unavailable tool/)
    expect(request).toHaveBeenCalledTimes(1)
  })

  it('refuses a model-supplied confirmation flag', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ tools: [read] })
    const request = vi.spyOn(api, 'request').mockResolvedValue({ content: '{"tool":"list_tasks","arguments":{"confirm":true}}' })
    await expect(askAssistant('tasks', () => true, signal)).rejects.toThrow(/unsupported tool arguments/)
    expect(request).toHaveBeenCalledTimes(1)
  })

  it('answers ordinary questions without a tool', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ tools: [] })
    vi.spyOn(api, 'request').mockResolvedValue({ content: '{"tool":"","arguments":{},"answer":"Hello"}' })
    expect(await askAssistant('Hi', () => true, signal)).toEqual({ kind: 'answer', text: 'Hello' })
  })
})
