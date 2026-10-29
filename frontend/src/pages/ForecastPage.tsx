import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  forecastApi,
  formatMinorUnits,
  formatBasisPoints,
  type ForecastMethod,
  type ForecastRun,
} from '../api/intelligence'
import { errorMessage } from '../api/client'
import { useAuth } from '../hooks/useAuth'
import { Alert, Card, EmptyState, ErrorState, Field, PageHeader, Stat, Timestamp } from '../components/ui'

/**
 * The Forecasting page.
 *
 * A forecast is presented as an estimate, not a fact: the projected points are
 * shown next to the observed series they came from, and the measured
 * out-of-sample accuracy is shown next to the naive baseline it must beat. When a
 * method did not beat the naive baseline the page says so rather than presenting
 * the line as reliable. The page never writes CRM data; running a forecast stores
 * the run.
 */
export function ForecastPage() {
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [method, setMethod] = useState<ForecastMethod>('mean')
  const [window, setWindow] = useState(3)
  const [horizon, setHorizon] = useState(6)
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const canRun = can('forecast.run')

  const catalogQuery = useQuery({
    queryKey: ['forecast', 'catalog'],
    queryFn: () => forecastApi.catalog(),
    staleTime: 5 * 60 * 1000,
  })

  const runsQuery = useQuery({
    queryKey: ['forecast', 'runs'],
    queryFn: () => forecastApi.listRuns(),
  })

  const run = useMutation({
    mutationFn: () => forecastApi.run({ method, window: method === 'moving_average' ? window : undefined, horizon }),
    onSuccess: (created) => {
      void queryClient.invalidateQueries({ queryKey: ['forecast', 'runs'] })
      setSelectedId(created.id)
      setActionError(null)
    },
    onError: (error) => setActionError(errorMessage(error)),
  })

  const catalog = catalogQuery.data?.methods ?? []
  const runs = runsQuery.data?.runs ?? []
  const latest = runs.find((r) => r.id === selectedId) ?? runs[0]
  const selected = catalog.find((c) => c.method === method)

  return (
    <>
      <PageHeader
        title="Forecasting"
        meta="Project revenue from the organization's own history. Each projection is shown with the observed series it came from and the accuracy it measured against a naive baseline."
      />

      {actionError ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="error">{actionError}</Alert>
        </div>
      ) : null}

      <div className="grid" style={{ gridTemplateColumns: 'minmax(280px, 340px) 1fr', gap: 'var(--space-5)' }}>
        <div className="stack">
          <Card title="New forecast">
            <div className="stack">
              <Field label="Method" htmlFor="forecast-method">
                <select
                  id="forecast-method"
                  className="input"
                  value={method}
                  onChange={(event) => setMethod(event.target.value as ForecastMethod)}
                >
                  {catalog.length === 0 ? <option value="mean">Historical average</option> : null}
                  {catalog.map((entry) => (
                    <option key={entry.method} value={entry.method}>
                      {entry.name}
                    </option>
                  ))}
                </select>
              </Field>
              {selected ? <p className="text-subtle">{selected.description}</p> : null}

              {method === 'moving_average' ? (
                <Field label="Window (periods)" htmlFor="forecast-window">
                  <input
                    id="forecast-window"
                    className="input"
                    type="number"
                    min={1}
                    max={24}
                    value={window}
                    onChange={(event) => setWindow(Number(event.target.value))}
                  />
                </Field>
              ) : null}

              <Field label="Horizon (periods)" htmlFor="forecast-horizon">
                <input
                  id="forecast-horizon"
                  className="input"
                  type="number"
                  min={1}
                  max={36}
                  value={horizon}
                  onChange={(event) => setHorizon(Number(event.target.value))}
                />
              </Field>

              {canRun ? (
                <button
                  type="button"
                  className="btn btn--primary"
                  disabled={run.isPending}
                  onClick={() => run.mutate()}
                >
                  {run.isPending ? 'Computing…' : 'Compute forecast'}
                </button>
              ) : (
                <Alert tone="info">You do not have permission to compute a forecast.</Alert>
              )}
            </div>
          </Card>

          <Card title="History">
            {runsQuery.isLoading ? (
              <p className="text-subtle">Loading…</p>
            ) : runsQuery.isError ? (
              <ErrorState message={errorMessage(runsQuery.error)} onRetry={() => void runsQuery.refetch()} />
            ) : runs.length === 0 ? (
              <EmptyState title="No forecasts yet" text="Compute one to see it here." />
            ) : (
              <ul className="stack" style={{ listStyle: 'none', padding: 0 }}>
                {runs.map((r) => (
                  <li key={r.id}>
                    <button
                      type="button"
                      className="btn btn--ghost"
                      onClick={() => setSelectedId(r.id)}
                      style={{ width: '100%', textAlign: 'left' }}
                    >
                      {r.method} · {r.horizon} periods · <Timestamp value={r.created_at} />
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </Card>
        </div>

        <div className="stack">
          {latest ? (
            <ForecastDetail run={latest} />
          ) : (
            <Card title="Forecast">
              <EmptyState
                title="Nothing to show"
                text="Compute a forecast to see the projection, the observed series and the measured accuracy."
              />
            </Card>
          )}
        </div>
      </div>
    </>
  )
}

function ForecastDetail({ run }: { run: ForecastRun }) {
  const result = run.result
  const currency = result.currency
  return (
    <>
      <Card title="Projection">
        <div className="grid" style={{ gridTemplateColumns: 'repeat(3, 1fr)', gap: 'var(--space-4)' }}>
          <Stat label="Method" value={result.method} hint={result.algorithm_version} />
          <Stat label="Horizon" value={`${run.horizon} periods`} />
          <Stat
            label="Beats naive baseline"
            value={result.quality.beats_naive ? 'Yes' : 'No'}
            hint={
              result.quality.beats_naive
                ? 'This method has lower error than carrying the last value forward.'
                : 'This method did not beat the naive baseline; treat the line with caution.'
            }
          />
        </div>

        <table className="table" style={{ marginTop: 'var(--space-4)' }}>
          <thead>
            <tr>
              <th scope="col">Period</th>
              <th scope="col">Projected</th>
              <th scope="col">Error band</th>
            </tr>
          </thead>
          <tbody>
            {result.points.map((point) => (
              <tr key={point.period}>
                <td>{point.period}</td>
                <td>{formatMinorUnits(point.value.amount_minor, currency)}</td>
                <td className="text-subtle">
                  {formatMinorUnits(point.lower.amount_minor, currency)} –{' '}
                  {formatMinorUnits(point.upper.amount_minor, currency)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>

      <Card title="Measured accuracy">
        <div className="grid" style={{ gridTemplateColumns: 'repeat(3, 1fr)', gap: 'var(--space-4)' }}>
          <Stat label="Folds scored" value={result.quality.folds} />
          <Stat label="MAE" value={formatMinorUnits(result.quality.mae.amount_minor, currency)} hint="Mean absolute error, out of sample" />
          <Stat
            label="Naive MAE"
            value={formatMinorUnits(result.quality.naive_mae.amount_minor, currency)}
            hint="The baseline this method must beat"
          />
          <Stat label="MAPE" value={formatBasisPoints(result.quality.mape.basis_points)} hint="Over folds with a non-zero actual" />
          <Stat label="Naive MAPE" value={formatBasisPoints(result.quality.naive_mape.basis_points)} />
          <Stat label="Zero-actual folds" value={result.quality.folds_with_zero_actual} hint="Excluded from MAPE" />
        </div>
      </Card>

      <Card title="Observed series">
        <table className="table">
          <thead>
            <tr>
              <th scope="col">Period</th>
              <th scope="col">Revenue (observed)</th>
            </tr>
          </thead>
          <tbody>
            {result.history.map((point) => (
              <tr key={point.period}>
                <td>{point.period}</td>
                <td>{formatMinorUnits(point.value.amount_minor, currency)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>

      {result.assumptions.length > 0 ? (
        <Card title="Assumptions">
          <ul>
            {result.assumptions.map((a, i) => (
              <li key={i}>{a}</li>
            ))}
          </ul>
        </Card>
      ) : null}

      {result.warnings.length > 0 ? (
        <Card title="Warnings">
          <div className="stack">
            {result.warnings.map((w, i) => (
              <Alert key={i} tone="warning">
                {w}
              </Alert>
            ))}
          </div>
        </Card>
      ) : null}
    </>
  )
}
