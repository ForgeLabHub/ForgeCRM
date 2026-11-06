import { describe, expect, it, vi } from 'vitest'
import { api } from './client'
import { readCrm } from './assistantCrm'

const read = { name: 'list_tasks', description: 'List tasks', dangerous: false, permission: 'tasks.read', input_schema: { type: 'object', properties: { limit: { type: 'integer' } } } }
const write = { name: 'create_task', description: 'Create task', dangerous: true, permission: 'tasks.create', input_schema: { type: 'object', properties: { title: { type: 'string' } } } }
const signal = new AbortController().signal

describe('assistant CRM read mode', () => {
  it('executes a permitted read with no confirmation and does not send its result to the model', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ tools: [read, write] })
    const request = vi.spyOn(api, 'request').mockResolvedValueOnce({ content: '{"tool":"list_tasks","arguments":{"limit":5}}' }).mockResolvedValueOnce({ content: [{ text: 'private data' }] })
    const result = await readCrm('List tasks', () => true, signal)
    expect(result.tool).toBe('list_tasks')
    expect(request).toHaveBeenCalledTimes(2)
    expect(request.mock.calls[1][1]?.body).toEqual({ name: 'list_tasks', arguments: { limit: 5 } })
    expect(JSON.stringify(request.mock.calls[0])).not.toContain('private data')
    expect(JSON.stringify(request.mock.calls[0])).not.toContain('create_task')
  })

  it('refuses a write named by the model, even when the user could normally run it', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ tools: [read, write] })
    const request = vi.spyOn(api, 'request').mockResolvedValue({ content: '{"tool":"create_task","arguments":{"title":"bad"}}' })
    await expect(readCrm('create task', () => true, signal)).rejects.toThrow(/No permitted read tool/)
    expect(request).toHaveBeenCalledTimes(1)
  })

  it('never allows a model to inject a confirmation flag', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ tools: [read] })
    const request = vi.spyOn(api, 'request').mockResolvedValue({ content: '{"tool":"list_tasks","arguments":{"confirm":true}}' })
    await expect(readCrm('tasks', () => true, signal)).rejects.toThrow(/unsupported tool arguments/)
    expect(request).toHaveBeenCalledTimes(1)
  })

  it('refuses a tool without the user permission before consulting the model', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ tools: [read] })
    const request = vi.spyOn(api, 'request')
    await expect(readCrm('tasks', () => false, signal)).rejects.toThrow(/No permitted CRM read tools/)
    expect(request).not.toHaveBeenCalled()
  })
})
