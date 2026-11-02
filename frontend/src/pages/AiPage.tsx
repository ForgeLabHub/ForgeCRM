import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { aiApi, mcpApi } from '../api/ai'
import type { MCPTool } from '../api/ai'
import { errorMessage } from '../api/client'
import { useAuth } from '../hooks/useAuth'
import { useT } from '../i18n'
import {
  Alert,
  Badge,
  Card,
  EmptyState,
  Modal,
  PageHeader,
  StatusBadge,
  TableSkeleton,
  Timestamp,
} from '../components/ui'

export function AiPage() {
  const { can } = useAuth()
  const t = useT()

  const status = useQuery({ queryKey: ['ai', 'status'], queryFn: () => aiApi.status() })
  const tools = useQuery({ queryKey: ['mcp', 'tools'], queryFn: () => mcpApi.tools() })
  const tasks = useQuery({ queryKey: ['ai', 'tasks'], queryFn: () => aiApi.tasks({ limit: 20 }) })

  const [selected, setSelected] = useState<MCPTool | null>(null)

  return (
    <>
      <PageHeader title={t('ai.title')} meta={t('ai.meta')} />

      <div className="stack">
        <Card title={t('ai.provider')}>
          {status.isLoading ? (
            <p className="text-muted">{t('ai.checking')}</p>
          ) : status.isError ? (
            <Alert tone="error">{errorMessage(status.error)}</Alert>
          ) : status.data?.enabled ? (
            <div className="stack">
              <div className="row">
                <Badge tone="success">{t('ai.enabled')}</Badge>
                <span className="text-muted text-sm">
                  {t('ai.providerCount', { count: status.data.providers?.length ?? 0 })}
                </span>
              </div>
              {status.data.providers?.length ? (
                <div className="table-wrap">
                  <table className="table--stack">
                    <thead>
                      <tr>
                        <th>{t('ai.columnProvider')}</th>
                        <th>{t('ai.columnModel')}</th>
                        <th>{t('ai.columnHealth')}</th>
                        <th>{t('ai.columnLastError')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {status.data.providers.map((provider) => (
                        <tr key={provider.key}>
                          <td className="table__primary" data-label={t('ai.columnProvider')}>
                            {provider.name}
                          </td>
                          <td className="mono" data-label={t('ai.columnModel')}>
                            {provider.model ?? '—'}
                          </td>
                          <td data-label={t('ai.columnHealth')}>
                            {provider.healthy === undefined ? (
                              <span className="text-subtle">{t('ai.unknown')}</span>
                            ) : provider.healthy ? (
                              <Badge tone="success">{t('ai.healthy')}</Badge>
                            ) : (
                              <Badge tone="danger">{t('ai.unhealthy')}</Badge>
                            )}
                          </td>
                          <td
                            className="truncate"
                            style={{ maxWidth: 260 }}
                            data-label={t('ai.columnLastError')}
                          >
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
              <Alert tone="info">{t('ai.noProviderAlert')}</Alert>
              <p className="text-muted text-sm">{t('ai.noProviderHint')}</p>
            </div>
          )}
        </Card>

        <Card title={t('ai.mcpTools')}>
          {tools.isLoading ? (
            <TableSkeleton rows={4} columns={2} />
          ) : tools.isError ? (
            <Alert tone="error">{errorMessage(tools.error)}</Alert>
          ) : tools.data?.tools.length ? (
            <>
              <p className="text-muted text-sm" style={{ marginBottom: 'var(--space-3)' }}>
                {t('ai.toolsHint')}
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
                      {t('ai.schema')}
                    </button>
                  </div>
                ))}
              </div>
            </>
          ) : (
            <EmptyState title={t('ai.noToolsTitle')} text={t('ai.noToolsText')} />
          )}
        </Card>

        {can('ai.tools.execute') ? <McpConsole tools={tools.data?.tools ?? []} /> : null}

        <Card title={t('ai.recentTasks')} flush>
          {tasks.isLoading ? (
            <TableSkeleton rows={4} columns={4} />
          ) : tasks.isError ? (
            <Alert tone="error">{errorMessage(tasks.error)}</Alert>
          ) : tasks.data?.items.length ? (
            <div className="table-wrap">
              <table className="table--stack">
                <thead>
                  <tr>
                    <th>{t('ai.columnKind')}</th>
                    <th>{t('common.status')}</th>
                    <th>{t('ai.columnModel')}</th>
                    <th>{t('ai.columnTokens')}</th>
                    <th>{t('common.created')}</th>
                  </tr>
                </thead>
                <tbody>
                  {tasks.data.items.map((task) => (
                    <tr key={task.id}>
                      <td className="table__primary" data-label={t('ai.columnKind')}>
                        {task.kind}
                      </td>
                      <td data-label={t('common.status')}>
                        <StatusBadge status={task.status} />
                      </td>
                      <td className="mono" data-label={t('ai.columnModel')}>
                        {task.model ?? '—'}
                      </td>
                      <td className="numeric" data-label={t('ai.columnTokens')}>
                        {task.tokens_in !== undefined || task.tokens_out !== undefined
                          ? `${task.tokens_in ?? 0} / ${task.tokens_out ?? 0}`
                          : '—'}
                      </td>
                      <td data-label={t('common.created')}>
                        <Timestamp value={task.created_at} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <div className="card__body">
              <p className="text-muted">{t('ai.noTasks')}</p>
            </div>
          )}
        </Card>
      </div>

      {selected ? (
        <Modal
          open
          onClose={() => setSelected(null)}
          title={selected.name}
          footer={<>
            <button type="button" className="btn btn--secondary" onClick={() => setSelected(null)}>
              {t('common.close')}
            </button>
          </>}
        >
          <div className="stack">
            <p className="text-muted">{selected.description}</p>
            <div>
              <h3 style={{ marginBottom: 'var(--space-2)' }}>{t('ai.inputSchema')}</h3>
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
        </Modal>
      ) : null}
    </>
  )
}

/** A minimal way to exercise one tool, useful for demonstrating what it does. */
function McpConsole({ tools }: { tools: MCPTool[] }) {
  const t = useT()
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
        throw new Error(t('ai.argsInvalid'))
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
    <Card title={t('ai.tryTool')}>
      <div className="stack">
        <p className="text-muted text-sm">{t('ai.tryToolText')}</p>
        <div className="form-grid">
          <label className="field">
            <span className="field__label">{t('ai.tool')}</span>
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
            <span className="field__label">{t('ai.argumentsJson')}</span>
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
            <span className="text-sm">{t('ai.confirmChange')}</span>
          </label>
        ) : null}
        <div className="row">
          <button
            type="button"
            className="btn btn--primary btn--sm"
            onClick={() => call.mutate()}
            disabled={call.isPending || !toolName || (needsConfirmation && !confirm)}
          >
            {call.isPending ? t('ai.running') : t('ai.runTool')}
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
