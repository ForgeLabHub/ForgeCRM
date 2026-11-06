import { describe, expect, it, vi } from 'vitest'
import { api } from './client'
import { askAssistant } from './assistantCrm'

const read = { name: 'list_tasks', description: 'List tasks', dangerous: false, permission: 'tasks.read', input_schema: { type: 'object', properties: { limit: { type: 'integer' } } } }
const write = { name: 'create_task', description: 'Create task', dangerous: true, permission: 'tasks.create', input_schema: { type: 'object', properties: { title: { type: 'string' } } } }
const signal = new AbortController().signal

describe('assistant tool selection', () => {
  it('reads permitted data and sends it to the model for an answer', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ tools: [read, write] })
    const request = vi.spyOn(api, 'request').mockResolvedValueOnce({ content: '{"tool":"list_tasks","arguments":{"limit":5}}' }).mockResolvedValueOnce({ content: [{ text: 'task data' }] }).mockResolvedValueOnce({ content: 'There are tasks.' })
    expect(await askAssistant('List tasks', () => true, signal)).toEqual({ kind: 'answer', text: 'There are tasks.' })
    expect(request.mock.calls[1][1]?.body).toEqual({ name: 'list_tasks', arguments: { limit: 5 } })
    expect(JSON.stringify(request.mock.calls[2])).toContain('task data')
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
