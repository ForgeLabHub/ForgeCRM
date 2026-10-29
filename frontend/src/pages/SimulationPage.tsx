import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  simulationApi,
  formatMetricValue,
  type CatalogEntry,
  type Comparison,
  type Metric,
  type ParamSpec,
  type Scenario,
  type ScenarioKind,
  type SimulationRun,
} from '../api/simulation'
import { pipelinesApi } from '../api/crm'
import { errorMessage } from '../api/client'
import { useAuth } from '../hooks/useAuth'
import { Alert, Badge, Card, EmptyState, ErrorState, PageHeader, Stat, Timestamp } from '../components/ui'

/**
 * The Simulation Lab.
 *
 * Every number on this page is either an observed CRM figure or a figure the
 * engine computed from one, and the two are never blended: the baseline column is
 * what the CRM holds, the variant column is what the model produced under the
 * user's parameters, and the assumptions the model depends on are shown next to
 * the numbers rather than hidden. A scenario with too little CRM history is
 * reported as such, never filled in with a fabricated zero.
 *
 * The page never writes CRM data. Running a scenario stores the run; the CRM is
 * read-only from here.
 */
export function SimulationPage() {
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [selected, setSelected] = useState<Scenario | null>(null)
  const [building, setBuilding] = useState<ScenarioKind | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const canManage = can('simulation.manage')
  const canRun = can('simulation.run')

  const catalogQuery = useQuery({
    queryKey: ['simulation', 'catalog'],
    queryFn: () => simulationApi.catalog(),
    staleTime: 5 * 60 * 1000,
  })

  const scenariosQuery = useQuery({
    queryKey: ['simulation', 'scenarios'],
    queryFn: () => simulationApi.listScenarios(),
  })

  const pipelinesQuery = useQuery({ queryKey: ['pipelines'], queryFn: () => pipelinesApi.list() })

  const archive = useMutation({
    mutationFn: (id: string) => simulationApi.archiveScenario(id),
    onSuccess: (_result, id) => {
      void queryClient.invalidateQueries({ queryKey: ['simulation', 'scenarios'] })
      setSelected((current) => (current?.id === id ? null : current))
      setActionError(null)
    },
    onError: (error) => setActionError(errorMessage(error)),
  })

  const catalog = catalogQuery.data?.scenarios ?? []
  const scenarios = scenariosQuery.data?.scenarios ?? []
  const pipelineId = pipelinesQuery.data?.items?.[0]?.id

  return (
    <>
      <PageHeader
        title="Simulation Lab"
        meta="Ask “what if” against the real pipeline. Observed figures and modelled figures are kept apart, and every assumption is stated."
      />

      {actionError ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="error">{actionError}</Alert>
        </div>
      ) : null}

      <div className="grid" style={{ gridTemplateColumns: 'minmax(280px, 340px) 1fr', gap: 'var(--space-5)' }}>
        <div className="stack">
          <Card title="Scenarios">
            {scenariosQuery.isLoading ? (
              <p className="text-subtle">Loading…</p>
            ) : scenariosQuery.isError ? (
              <ErrorState
                message={errorMessage(scenariosQuery.error)}
                onRetry={() => void scenariosQuery.refetch()}
              />
            ) : scenarios.length === 0 ? (
              <EmptyState
                title="No scenarios yet"
                text="Start from one of the scenario types below."
              />
            ) : (
              <ul className="stack" style={{ listStyle: 'none', padding: 0, gap: 6 }}>
                {scenarios.map((scenario) => (
                  <li key={scenario.id}>
                    <button
                      type="button"
                      className="card"
                      style={{
                        width: '100%',
                        textAlign: 'left',
                        cursor: 'pointer',
                        borderColor: selected?.id === scenario.id ? 'var(--color-accent)' : undefined,
                      }}
                      onClick={() => {
                        setSelected(scenario)
                        setBuilding(null)
                        setActionError(null)
                      }}
                    >
                      <div className="card__body stack" style={{ gap: 4 }}>
                        <strong>{scenario.name}</strong>
                        <span className="row" style={{ gap: 6 }}>
                          <Badge tone="neutral">{scenario.kind}</Badge>
                          <span className="text-subtle text-sm">{scenario.algorithm_version}</span>
                        </span>
                      </div>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </Card>

          <Card title="Start a scenario">
            {catalogQuery.isLoading ? (
              <p className="text-subtle">Loading…</p>
            ) : catalogQuery.isError ? (
              <ErrorState
                message={errorMessage(catalogQuery.error)}
                onRetry={() => void catalogQuery.refetch()}
              />
            ) : (
              <div className="stack" style={{ gap: 8 }}>
                {catalog.map((entry) => (
                  <button
                    key={entry.kind}
                    type="button"
                    className="btn btn--secondary"
                    style={{ justifyContent: 'flex-start', textAlign: 'left' }}
                    onClick={() => {
                      setBuilding(entry.kind)
                      setSelected(null)
                      setActionError(null)
                    }}
                  >
                    {entry.name}
                  </button>
                ))}
              </div>
            )}
          </Card>
        </div>

        <div className="stack">
          {building ? (
            <ScenarioBuilder
              entry={catalog.find((c) => c.kind === building)!}
              pipelineId={pipelineId}
              canManage={canManage}
              canRun={canRun}
              onSaved={(scenario) => {
                void queryClient.invalidateQueries({ queryKey: ['simulation', 'scenarios'] })
                setBuilding(null)
                setSelected(scenario)
              }}
            />
          ) : selected ? (
            <ScenarioRunner
              scenario={selected}
              pipelineId={pipelineId}
              canRun={canRun}
              canManage={canManage}
              onArchive={() => archive.mutate(selected.id)}
            />
          ) : (
            <Card title="How this works">
              <div className="stack">
                <p>
                  Pick a saved scenario or start a new one. A scenario changes a small number of
                  assumptions — how many deals enter the funnel, how a stage's win probability moves,
                  how churn changes — and reports the expected effect as a comparison.
                </p>
                <p className="text-subtle text-sm">
                  The baseline column is read from the CRM and is never modified. The variant column is
                  computed from the baseline and your parameters. Both are shown so a figure can always
                  be traced back to the data it came from.
                </p>
                <Alert tone="info">
                  These are deterministic models, not forecasts. The assumptions behind each result are
                  shown with it.
                </Alert>
              </div>
            </Card>
          )}
        </div>
      </div>
    </>
  )
}

/**
 * The builder turns a catalog entry into a parameter form. Each parameter's
 * control is driven by the catalog's ParamSpec, so a parameter the server added is
 * offered without a client change, and one it removed stops being offered.
 */
function ScenarioBuilder({
  entry,
  pipelineId,
  canManage,
  canRun,
  onSaved,
}: {
  entry: CatalogEntry
  pipelineId?: string
  canManage: boolean
  canRun: boolean
  onSaved: (scenario: Scenario) => void
}) {
  const [name, setName] = useState(entry.name)
  const [values, setValues] = useState<Record<string, string>>(() =>
    Object.fromEntries(entry.parameters.map((p) => [p.key, p.default])),
  )
  const [stages, setStages] = useState<Array<{ id: string; name: string; value: string }>>([])
  const [error, setError] = useState<string | null>(null)
  const [preview, setPreview] = useState<Comparison | null>(null)

  // A stage-scoped parameter needs the pipeline's stages, which only the funnel
  // has. They come from the pipeline the runner already loaded, so the stage ids
  // are the ones the engine looks up rather than a client guess.
  const stagesQuery = useQuery({
    queryKey: ['pipelines'],
    queryFn: () => pipelinesApi.list(),
    enabled: Boolean(pipelineId) && entry.parameters.some((p) => p.stage_scoped),
  })

  const stageList = (stagesQuery.data?.items ?? []).find((pl) => pl.id === pipelineId)?.stages ?? []

  const params = useMemo(() => buildParams(entry.parameters, values, stages), [entry.parameters, values, stages])

  const previewMutation = useMutation({
    mutationFn: () =>
      simulationApi.preview({
        kind: entry.kind,
        params: params as Record<string, unknown>,
        pipeline_id: pipelineId,
      }),
    onSuccess: (res) => {
      setPreview(res.comparison)
      setError(null)
    },
    onError: (err) => {
      setPreview(null)
      setError(errorMessage(err))
    },
  })

  const saveMutation = useMutation({
    mutationFn: () =>
      simulationApi.createScenario({
        name: name.trim(),
        kind: entry.kind,
        params: params as Record<string, unknown>,
      }),
    onSuccess: (scenario) => onSaved(scenario),
    onError: (err) => setError(errorMessage(err)),
  })

  return (
    <Card
      title={entry.name}
      actions={<Badge tone="neutral">{entry.kind}</Badge>}
    >
      <div className="stack">
        <p className="text-subtle text-sm">{entry.description}</p>

        {error ? <Alert tone="error">{error}</Alert> : null}

        <label className="field" htmlFor="sim-name">
          <span className="field__label">Name</span>
          <input
            id="sim-name"
            className="input"
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </label>

        {entry.parameters.map((param) => (
          <ParameterControl
            key={param.key}
            spec={param}
            value={values[param.key] ?? ''}
            onChange={(next) => setValues((previous) => ({ ...previous, [param.key]: next }))}
          />
        ))}

        {entry.parameters.some((p) => p.stage_scoped) ? (
          <div className="stack" style={{ gap: 6 }}>
            <span className="field__label">Per-stage conversion</span>
            {stagesQuery.isLoading ? <p className="text-subtle text-sm">Loading stages…</p> : null}
            {stagesQuery.isError ? (
              <p className="text-subtle text-sm">{errorMessage(stagesQuery.error)}</p>
            ) : null}
            {stageList.length === 0 && !stagesQuery.isLoading ? (
              <p className="text-subtle text-sm">
                No pipeline stages are configured, so a per-stage change has nothing to apply to.
              </p>
            ) : null}
            {stageList.map((stage) => (
              <label key={stage.id} className="field" htmlFor={`sim-stage-${stage.id}`}>
                <span className="field__label">{stage.name}</span>
                <input
                  id={`sim-stage-${stage.id}`}
                  className="input"
                  inputMode="decimal"
                  placeholder="0"
                  value={stages.find((s) => s.id === stage.id)?.value ?? ''}
                  onChange={(event) =>
                    setStages((previous) => {
                      const next = previous.filter((s) => s.id !== stage.id)
                      next.push({ id: stage.id, name: stage.name, value: event.target.value })
                      return next
                    })
                  }
                />
              </label>
            ))}
          </div>
        ) : null}

        <div className="form-actions" style={{ margin: 0 }}>
          <button
            type="button"
            className="btn btn--secondary"
            disabled={!canRun || previewMutation.isPending}
            onClick={() => previewMutation.mutate()}
          >
            {previewMutation.isPending ? 'Computing…' : 'Preview'}
          </button>
          <button
            type="button"
            className="btn btn--primary"
            disabled={!canManage || !name.trim() || saveMutation.isPending}
            onClick={() => saveMutation.mutate()}
          >
            {saveMutation.isPending ? 'Saving…' : 'Save scenario'}
          </button>
        </div>

        {!canRun ? (
          <p className="text-subtle text-sm">You can view scenarios but not compute them.</p>
        ) : null}

        {preview ? <ComparisonView comparison={preview} /> : null}
      </div>
    </Card>
  )
}

/** One parameter control, driven by the catalog's ParamSpec. */
function ParameterControl({
  spec,
  value,
  onChange,
}: {
  spec: ParamSpec
  value: string
  onChange: (value: string) => void
}) {
  return (
    <label className="field" htmlFor={`sim-param-${spec.key}`}>
      <span className="field__label">
        {spec.label} <span className="text-subtle">({spec.kind === 'percent' ? '%' : spec.kind})</span>
      </span>
      <input
        id={`sim-param-${spec.key}`}
        className="input"
        inputMode="decimal"
        value={value}
        onChange={(event) => onChange(event.target.value)}
      />
      <span className="field__hint">{spec.description}</span>
    </label>
  )
}

/**
 * The runner computes a saved scenario, shows the result, and keeps the run
 * history. A stored run keeps its own baseline and result, so an old run can be
 * reopened and read exactly as it was computed.
 */
function ScenarioRunner({
  scenario,
  pipelineId,
  canRun,
  canManage,
  onArchive,
}: {
  scenario: Scenario
  pipelineId?: string
  canRun: boolean
  canManage: boolean
  onArchive: () => void
}) {
  const [error, setError] = useState<string | null>(null)
  const [comparison, setComparison] = useState<Comparison | null>(null)
  const [openRun, setOpenRun] = useState<SimulationRun | null>(null)

  const runsQuery = useQuery({
    queryKey: ['simulation', 'runs', scenario.id],
    queryFn: () => simulationApi.runs(scenario.id),
  })

  const runMutation = useMutation({
    mutationFn: () => simulationApi.run(scenario.id, pipelineId ? { pipeline_id: pipelineId } : {}),
    onSuccess: (run) => {
      setComparison(run.result)
      setError(null)
      void runsQuery.refetch()
    },
    onError: (err) => {
      setComparison(null)
      setError(errorMessage(err))
    },
  })

  const runs = runsQuery.data?.runs ?? []
  const shown = openRun?.result ?? comparison

  return (
    <Card
      title={scenario.name}
      actions={
        <div className="row" style={{ gap: 8 }}>
          <Badge tone="neutral">{scenario.kind}</Badge>
          {canManage ? (
            <button type="button" className="btn btn--ghost btn--sm" onClick={onArchive}>
              Archive
            </button>
          ) : null}
        </div>
      }
    >
      <div className="stack">
        {error ? <Alert tone="error">{error}</Alert> : null}

        <div className="row" style={{ gap: 8 }}>
          <button
            type="button"
            className="btn btn--primary"
            disabled={!canRun || runMutation.isPending}
            onClick={() => {
              setOpenRun(null)
              runMutation.mutate()
            }}
          >
            {runMutation.isPending ? 'Computing…' : 'Run scenario'}
          </button>
          {openRun ? (
            <button type="button" className="btn btn--secondary btn--sm" onClick={() => setOpenRun(null)}>
              Show latest
            </button>
          ) : null}
        </div>

        {shown ? <ComparisonView comparison={shown} /> : null}

        <div>
          <h3 style={{ marginBottom: 'var(--space-2)' }}>Run history</h3>
          {runsQuery.isLoading ? (
            <p className="text-subtle">Loading…</p>
          ) : runsQuery.isError ? (
            <ErrorState message={errorMessage(runsQuery.error)} onRetry={() => void runsQuery.refetch()} />
          ) : runs.length === 0 ? (
            <p className="text-subtle text-sm">This scenario has not been run yet.</p>
          ) : (
            <ul className="stack" style={{ listStyle: 'none', padding: 0, gap: 6 }}>
              {runs.map((run) => (
                <li key={run.id} className="row" style={{ justifyContent: 'space-between' }}>
                  <span className="text-sm">
                    <Timestamp value={run.created_at} /> · <span className="mono">{run.algorithm_version}</span>
                  </span>
                  <button
                    type="button"
                    className="btn btn--ghost btn--sm"
                    onClick={() => setOpenRun(run)}
                  >
                    Open
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </Card>
  )
}

/**
 * The comparison view separates the three things a reader must not confuse: the
 * observed CRM figures (baseline), the model's computed figures (variant), and the
 * assumptions and warnings the model depends on.
 */
function ComparisonView({ comparison }: { comparison: Comparison }) {
  const moneyMetrics = comparison.metrics.filter((m) => m.unit === 'money')
  const otherMetrics = comparison.metrics.filter((m) => m.unit !== 'money')

  return (
    <div className="stack">
      <div className="grid grid--stats">
        {moneyMetrics.map((metric) => (
          <Stat
            key={metric.key}
            label={metric.label}
            value={formatMetricValue(metric, metric.variant)}
            hint={
              <span>
                from <strong>{formatMetricValue(metric, metric.baseline)}</strong>{' '}
                {metric.relative_defined ? (
                  <Badge tone={metric.delta >= 0 ? 'success' : 'danger'}>
                    {metric.delta >= 0 ? '+' : ''}
                    {(metric.relative_basis_points! / 100).toFixed(1)}%
                  </Badge>
                ) : null}
              </span>
            }
          />
        ))}
      </div>

      <div className="card" style={{ overflow: 'hidden' }}>
        <table className="table">
          <caption className="sr-only">Baseline and variant by metric</caption>
          <thead>
            <tr>
              <th scope="col">Metric</th>
              <th scope="col">Observed (CRM)</th>
              <th scope="col">Modelled</th>
              <th scope="col">Change</th>
            </tr>
          </thead>
          <tbody>
            {[...moneyMetrics, ...otherMetrics].map((metric) => (
              <tr key={metric.key}>
                <td>{metric.label}</td>
                <td>{formatMetricValue(metric, metric.baseline)}</td>
                <td>{formatMetricValue(metric, metric.variant)}</td>
                <td>{formatChange(metric)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {comparison.assumptions.length > 0 ? (
        <div>
          <h4 style={{ marginBottom: 'var(--space-1)' }}>Assumptions</h4>
          <ul className="text-sm text-subtle">
            {comparison.assumptions.map((assumption) => (
              <li key={assumption}>{assumption}</li>
            ))}
          </ul>
        </div>
      ) : null}

      {comparison.warnings.length > 0 ? (
        <Alert tone="warning">
          <ul style={{ margin: 0 }}>
            {comparison.warnings.map((warning) => (
              <li key={warning}>{warning}</li>
            ))}
          </ul>
        </Alert>
      ) : null}
    </div>
  )
}

function formatChange(metric: Metric): string {
  const sign = metric.delta > 0 ? '+' : ''
  const delta = `${sign}${formatMetricValue(metric, metric.delta)}`
  if (!metric.relative_defined) return delta
  return `${delta} (${sign}${(metric.relative_basis_points! / 100).toFixed(1)}%)`
}

/**
 * buildParams turns the form's string values into the parameter document the
 * server expects. Percentages are relative changes; empty and stage fields are
 * omitted rather than sent as zero, so a blank control does not silently change a
 * stage's conversion.
 */
function buildParams(
  specs: ParamSpec[],
  values: Record<string, string>,
  stages: Array<{ id: string; name: string; value: string }>,
): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const spec of specs) {
    if (spec.stage_scoped) continue
    const raw = (values[spec.key] ?? '').trim()
    if (raw === '' || raw === '0') continue
    out[spec.key] = raw
  }
  const stageConversions: Record<string, string> = {}
  for (const stage of stages) {
    const raw = stage.value.trim()
    if (raw === '' || raw === '0') continue
    stageConversions[stage.id] = raw
  }
  if (Object.keys(stageConversions).length > 0) out.stage_conversions = stageConversions
  return out
}
