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
import { useT } from '../i18n'
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
  const t = useT()
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
      <PageHeader title={t('forecast.title')} meta={t('forecast.meta')} />

      {actionError ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="error">{actionError}</Alert>
        </div>
      ) : null}

      <div className="layout--aside">
        <div className="stack">
          <Card title={t('forecast.newForecast')}>
            <div className="stack">
              <Field label={t('forecast.method')} htmlFor="forecast-method">
                <select
                  id="forecast-method"
                  className="input"
                  value={method}
                  onChange={(event) => setMethod(event.target.value as ForecastMethod)}
                >
                  {catalog.length === 0 ? (
                    <option value="mean">{t('forecast.historicalAverage')}</option>
                  ) : null}
                  {catalog.map((entry) => (
                    <option key={entry.method} value={entry.method}>
                      {entry.name}
                    </option>
                  ))}
                </select>
              </Field>
              {selected ? <p className="text-subtle">{selected.description}</p> : null}

              {method === 'moving_average' ? (
                <Field label={t('forecast.window')} htmlFor="forecast-window">
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

              <Field label={t('forecast.horizon')} htmlFor="forecast-horizon">
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
                  {run.isPending ? t('forecast.computing') : t('forecast.computeForecast')}
                </button>
              ) : (
                <Alert tone="info">{t('forecast.noPermission')}</Alert>
              )}
            </div>
          </Card>

          <Card title={t('forecast.history')}>
            {runsQuery.isLoading ? (
              <p className="text-subtle">{t('common.loading')}</p>
            ) : runsQuery.isError ? (
              <ErrorState message={errorMessage(runsQuery.error)} onRetry={() => void runsQuery.refetch()} />
            ) : runs.length === 0 ? (
              <EmptyState title={t('forecast.noForecastsTitle')} text={t('forecast.noForecastsText')} />
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
                      {t('forecast.historyItem', { method: r.method, horizon: r.horizon })}
                      <Timestamp value={r.created_at} />
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
            <Card title={t('forecast.forecastTitle')}>
              <EmptyState
                title={t('forecast.nothingTitle')}
                text={t('forecast.nothingText')}
              />
            </Card>
          )}
        </div>
      </div>
    </>
  )
}

function ForecastDetail({ run }: { run: ForecastRun }) {
  const t = useT()
  const result = run.result
  const currency = result.currency
  return (
    <>
      <Card title={t('forecast.projection')}>
        <div className="grid grid--stats">
          <Stat label={t('forecast.method')} value={result.method} hint={result.algorithm_version} />
          <Stat
            label={t('forecast.horizon')}
            value={t('forecast.horizonValue', { count: run.horizon })}
          />
          <Stat
            label={t('forecast.beatsNaive')}
            value={result.quality.beats_naive ? t('forecast.yes') : t('forecast.no')}
            hint={
              result.quality.beats_naive
                ? t('forecast.beatsNaiveHint')
                : t('forecast.notBeatsNaiveHint')
            }
          />
        </div>

        <div className="table-wrap" style={{ marginTop: 'var(--space-4)' }}>
          <table className="table table--stack">
            <thead>
              <tr>
                <th scope="col">{t('forecast.period')}</th>
                <th scope="col">{t('forecast.projected')}</th>
                <th scope="col">{t('forecast.errorBand')}</th>
              </tr>
            </thead>
            <tbody>
              {result.points.map((point) => (
                <tr key={point.period}>
                  <td className="table__primary" data-label={t('forecast.period')}>
                    {point.period}
                  </td>
                  <td data-label={t('forecast.projected')}>
                    {formatMinorUnits(point.value.amount_minor, currency)}
                  </td>
                  <td className="text-subtle" data-label={t('forecast.errorBand')}>
                    {formatMinorUnits(point.lower.amount_minor, currency)} –{' '}
                    {formatMinorUnits(point.upper.amount_minor, currency)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>

      <Card title={t('forecast.measuredAccuracy')}>
        <div className="grid grid--stats">
          <Stat label={t('forecast.foldsScored')} value={result.quality.folds} />
          <Stat
            label={t('forecast.mae')}
            value={formatMinorUnits(result.quality.mae.amount_minor, currency)}
            hint={t('forecast.maeHint')}
          />
          <Stat
            label={t('forecast.naiveMae')}
            value={formatMinorUnits(result.quality.naive_mae.amount_minor, currency)}
            hint={t('forecast.naiveMaeHint')}
          />
          <Stat
            label={t('forecast.mape')}
            value={formatBasisPoints(result.quality.mape.basis_points)}
            hint={t('forecast.mapeHint')}
          />
          <Stat
            label={t('forecast.naiveMape')}
            value={formatBasisPoints(result.quality.naive_mape.basis_points)}
          />
          <Stat
            label={t('forecast.zeroActualFolds')}
            value={result.quality.folds_with_zero_actual}
            hint={t('forecast.zeroActualFoldsHint')}
          />
        </div>
      </Card>

      <Card title={t('forecast.observedSeries')}>
        <div className="table-wrap">
          <table className="table table--stack">
            <thead>
              <tr>
                <th scope="col">{t('forecast.period')}</th>
                <th scope="col">{t('forecast.revenueObserved')}</th>
              </tr>
            </thead>
            <tbody>
              {result.history.map((point) => (
                <tr key={point.period}>
                  <td className="table__primary" data-label={t('forecast.period')}>
                    {point.period}
                  </td>
                  <td data-label={t('forecast.revenueObserved')}>
                    {formatMinorUnits(point.value.amount_minor, currency)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>

      {result.assumptions.length > 0 ? (
        <Card title={t('forecast.assumptions')}>
          <ul>
            {result.assumptions.map((a, i) => (
              <li key={i}>{a}</li>
            ))}
          </ul>
        </Card>
      ) : null}

      {result.warnings.length > 0 ? (
        <Card title={t('forecast.warnings')}>
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
