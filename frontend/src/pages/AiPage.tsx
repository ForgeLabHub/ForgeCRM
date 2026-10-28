import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { aiApi, mcpApi } from '../api/ai'
import type { MCPTool } from '../api/ai'
import { errorMessage } from '../api/client'
import { useAuth } from '../hooks/useAuth'
import {
  Alert,
  Badge,
  Card,
  EmptyState,
  PageHeader,
  StatusBadge,
  TableSkeleton,
  Timestamp,
} from '../components/ui'

export function AiPage() {
  const { can } = useAuth()

  const status = useQuery({ queryKey: ['ai', 'status'], queryFn: () => aiApi.status() })
  const tools = useQuery({ queryKey: ['mcp', 'tools'], queryFn: () => mcpApi.tools() })
  const tasks = useQuery({ queryKey: ['ai', 'tasks'], queryFn: () => aiApi.tasks({ limit: 20 }) })

  const [selected, setSelected] = useState<MCPTool | null>(null)

  return (
    <>
      <PageHeader
        title="AI & MCP"
        meta="The optional AI layer and the tools it may call. Both work with the same permissions as the API."
      />

      <div className="stack">
        <Card title="AI provider">
          {status.isLoading ? (
            <p className="text-muted">Checking…</p>
          ) : status.isError ? (
            <Alert tone="error">{errorMessage(status.error)}</Alert>
          ) : status.data?.enabled ? (
            <div className="stack">
              <div className="row">
                <Badge tone="success">enabled</Badge>
                <span className="text-muted text-sm">
                  {status.data.providers?.length ?? 0} provider
                  {(status.data.providers?.length ?? 0) === 1 ? '' : 's'} configured
                </span>
              </div>
              {status.data.providers?.length ? (
                <div className="table-wrap">
                  <table>
                    <thead>
                      <tr>
                        <th>Provider</th>
                        <th>Model</th>
                        <th>Health</th>
                        <th>Last error</th>
                      </tr>
                    </thead>
                    <tbody>
                      {status.data.providers.map((provider) => (
                        <tr key={provider.key}>
                          <td className="table__primary">{provider.name}</td>
                          <td className="mono">{provider.model ?? '—'}</td>
                          <td>
                            {provider.healthy === undefined ? (
                              <span className="text-subtle">unknown</span>
                            ) : provider.healthy ? (
                              <Badge tone="success">healthy</Badge>
                            ) : (
                              <Badge tone="danger">unhealthy</Badge>
                            )}
                          </td>
                          <td className="truncate" style={{ maxWidth: 260 }}>
                            {provider.last_error ?? <span className="text-subtle">—</span>}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              ) : null}
            </div>
          ) : (
            <div className="stack">
              <Alert tone="info">
                No AI provider is configured. ForgeCRM works fully without one: imports, rules,
                webhooks and reporting do not depend on AI.
              </Alert>
              <p className="text-muted text-sm">
                Set a provider in the server configuration to enable assisted summaries and the MCP
                tools that call a model.
              </p>
            </div>
          )}
        </Card>

        <Card title="MCP tools">
          {tools.isLoading ? (
            <TableSkeleton rows={4} columns={2} />
          ) : tools.isError ? (
            <Alert tone="error">{errorMessage(tools.error)}</Alert>
          ) : tools.data?.tools.length ? (
            <>
              <p className="text-muted text-sm" style={{ marginBottom: 'var(--space-3)' }}>
                These tools are the ones your account is allowed to see. Each one re-checks your
                permission when it runs, and every call is audited.
              </p>
              <div className="stack stack--sm">
                {tools.data.tools.map((tool) => (
                  <div key={tool.name} className="row row--between">
                    <div style={{ minWidth: 0 }}>
                      <div className="mono table__primary">{tool.name}</div>
                      <div className="table__secondary">{tool.description}</div>
                    </div>
                    <button
                      type="button"
                      className="btn btn--secondary btn--sm"
                      onClick={() => setSelected(tool)}
                    >
                      Schema
                    </button>
                  </div>
                ))}
              </div>
            </>
          ) : (
            <EmptyState
              title="No tools available"
              text="Your account does not hold any of the permissions the MCP tools require."
            />
          )}
        </Card>

        {can('ai.tools.execute') ? <McpConsole tools={tools.data?.tools ?? []} /> : null}

        <Card title="Recent AI tasks" flush>
          {tasks.isLoading ? (
            <TableSkeleton rows={4} columns={4} />
          ) : tasks.isError ? (
            <Alert tone="error">{errorMessage(tasks.error)}</Alert>
          ) : tasks.data?.items.length ? (
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Kind</th>
                    <th>Status</th>
                    <th>Model</th>
                    <th>Tokens</th>
                    <th>Created</th>
                  </tr>
                </thead>
                <tbody>
                  {tasks.data.items.map((task) => (
                    <tr key={task.id}>
                      <td className="table__primary">{task.kind}</td>
                      <td>
                        <StatusBadge status={task.status} />
                      </td>
                      <td className="mono">{task.model ?? '—'}</td>
                      <td className="numeric">
                        {task.tokens_in !== undefined || task.tokens_out !== undefined
                          ? `${task.tokens_in ?? 0} / ${task.tokens_out ?? 0}`
                          : '—'}
                      </td>
                      <td>
                        <Timestamp value={task.created_at} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <div className="card__body">
              <p className="text-muted">No AI tasks have been run.</p>
            </div>
          )}
        </Card>
      </div>

      {selected ? (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="tool-schema-title"
          style={{
            position: 'fixed',
            inset: 0,
            background: 'rgba(16, 24, 40, 0.45)',
            zIndex: 60,
            overflowY: 'auto',
            padding: 'var(--space-5)',
          }}
          onClick={() => setSelected(null)}
        >
          <div className="card" style={{ maxWidth: 640, margin: '0 auto' }} onClick={(e) => e.stopPropagation()}>
            <div className="card__header">
              <h2 id="tool-schema-title" className="mono">
                {selected.name}
              </h2>
            </div>
            <div className="card__body stack">
              <p className="text-muted">{selected.description}</p>
              <div>
                <h3 style={{ marginBottom: 'var(--space-2)' }}>Input schema</h3>
                <pre
                  className="mono"
                  style={{
                    background: 'var(--color-bg)',
                    padding: 'var(--space-3)',
                    borderRadius: 'var(--radius-sm)',
                    overflowX: 'auto',
                    margin: 0,
                  }}
                >
                  {JSON.stringify(selected.input_schema, null, 2)}
                </pre>
              </div>
            </div>
            <div className="form-actions" style={{ margin: 0, padding: 'var(--space-4)' }}>
              <button type="button" className="btn btn--secondary" onClick={() => setSelected(null)}>
                Close
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </>
  )
}

/** A minimal way to exercise one tool, useful for demonstrating what it does. */
function McpConsole({ tools }: { tools: MCPTool[] }) {
  const [toolName, setToolName] = useState(tools[0]?.name ?? '')
  const [argsText, setArgsText] = useState('{}')
  const [confirm, setConfirm] = useState(false)
  const [result, setResult] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  // A mutating tool is refused without an acknowledgement, so the interface must
  // offer one. Showing the checkbox only for such a tool keeps the requirement
  // visible instead of leaving the user with an unexplained refusal.
  const selected = tools.find((tool) => tool.name === toolName)
  const needsConfirmation = selected?.dangerous ?? false

  const call = useMutation({
    mutationFn: () => {
      let args: Record<string, unknown>
      try {
        args = JSON.parse(argsText) as Record<string, unknown>
      } catch {
        throw new Error('Arguments must be valid JSON.')
      }
      return mcpApi.call(toolName, args, needsConfirmation ? confirm : false)
    },
    onSuccess: (response) => {
      setError(null)
      setResult(JSON.stringify(response.content, null, 2))
    },
    onError: (err) => {
      setResult(null)
      setError(errorMessage(err))
    },
  })

  if (tools.length === 0) return null

  return (
    <Card title="Try a tool">
      <div className="stack">
        <p className="text-muted text-sm">
          Runs one tool with the arguments you provide. Results reflect exactly the data your
          permissions allow, so a tool can legitimately return nothing.
        </p>
        <div className="form-grid">
          <label className="field">
            <span className="field__label">Tool</span>
            <select
              value={toolName}
              onChange={(event) => {
                setToolName(event.target.value)
                setConfirm(false)
                setResult(null)
                setError(null)
              }}
            >
              {tools.map((tool) => (
                <option key={tool.name} value={tool.name}>
                  {tool.name}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span className="field__label">Arguments (JSON)</span>
            <input className="mono" value={argsText} onChange={(event) => setArgsText(event.target.value)} />
          </label>
        </div>
        {needsConfirmation ? (
          <label className="row" style={{ gap: 'var(--space-2)', alignItems: 'center' }}>
            <input
              type="checkbox"
              checked={confirm}
              onChange={(event) => setConfirm(event.target.checked)}
            />
            <span className="text-sm">
              This tool changes stored data. I confirm I want to run it.
            </span>
          </label>
        ) : null}
        <div className="row">
          <button
            type="button"
            className="btn btn--primary btn--sm"
            onClick={() => call.mutate()}
            disabled={call.isPending || !toolName || (needsConfirmation && !confirm)}
          >
            {call.isPending ? 'Running…' : 'Run tool'}
          </button>
        </div>

        {error ? <Alert tone="error">{error}</Alert> : null}

        {result ? (
          <pre
            className="mono"
            style={{
              background: 'var(--color-bg)',
              padding: 'var(--space-3)',
              borderRadius: 'var(--radius-sm)',
              overflowX: 'auto',
              maxHeight: 320,
              margin: 0,
            }}
          >
            {result}
          </pre>
        ) : null}
      </div>
    </Card>
  )
}
