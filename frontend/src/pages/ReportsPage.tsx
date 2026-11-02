import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { pipelinesApi, reportsApi } from '../api/crm'
import { errorMessage } from '../api/client'
import { Alert, Card, ErrorState, PageHeader, Stat, TableSkeleton, formatMoney } from '../components/ui'
import { useT } from '../i18n'

function defaultRange(): { from: string; to: string } {
  const to = new Date()
  const from = new Date()
  from.setDate(from.getDate() - 30)
  const iso = (date: Date) => date.toISOString().slice(0, 10)
  return { from: iso(from), to: iso(to) }
}

export function ReportsPage() {
  const t = useT()
  const [range, setRange] = useState(defaultRange)

  const sales = useQuery({
    queryKey: ['reports', 'sales', range],
    queryFn: () => reportsApi.salesSummary(range),
  })

  const support = useQuery({
    queryKey: ['reports', 'support', range],
    queryFn: () => reportsApi.supportSummary(range),
  })

  const pipelines = useQuery({ queryKey: ['pipelines'], queryFn: () => pipelinesApi.list() })

  const activePipeline = pipelines.data?.items?.[0]
  const pipelineReport = useQuery({
    queryKey: ['reports', 'pipeline', activePipeline?.id],
    queryFn: () => pipelinesApi.report(activePipeline!.id),
    enabled: Boolean(activePipeline?.id),
  })

  return (
    <>
      <PageHeader
        title={t('reports.title')}
        meta={t('reports.meta')}
        actions={
          <div className="row">
            <label className="field">
              <span className="field__label">{t('reports.from')}</span>
              <input
                type="date"
                value={range.from}
                onChange={(event) => setRange((previous) => ({ ...previous, from: event.target.value }))}
              />
            </label>
            <label className="field">
              <span className="field__label">{t('reports.to')}</span>
              <input
                type="date"
                value={range.to}
                onChange={(event) => setRange((previous) => ({ ...previous, to: event.target.value }))}
              />
            </label>
          </div>
        }
      />

      <section style={{ marginBottom: 'var(--space-5)' }}>
        <h2 style={{ marginBottom: 'var(--space-3)' }}>{t('reports.sales')}</h2>
        {sales.isError ? (
          <ErrorState message={errorMessage(sales.error)} onRetry={() => void sales.refetch()} />
        ) : sales.isLoading ? (
          <div className="grid grid--stats" aria-busy="true">
            {Array.from({ length: 4 }, (_, index) => (
              <div key={index} className="skeleton" style={{ height: 96 }} />
            ))}
          </div>
        ) : sales.data ? (
          <>
            <div className="grid grid--stats">
              <Stat label={t('reports.won')} value={sales.data.deals_won} hint={formatMoney(sales.data.won_value)} />
              <Stat label={t('reports.lost')} value={sales.data.deals_lost} hint={formatMoney(sales.data.lost_value)} />
              <Stat label={t('reports.open')} value={sales.data.open_count} hint={formatMoney(sales.data.open_value)} />
              <Stat
                label={t('reports.winRate')}
                value={`${(sales.data.win_rate * 100).toFixed(1)}%`}
                hint={t('reports.winRateHint')}
              />
            </div>
          </>
        ) : null}
      </section>

      <section style={{ marginBottom: 'var(--space-5)' }}>
        <h2 style={{ marginBottom: 'var(--space-3)' }}>{t('reports.support')}</h2>
        {support.isError ? (
          <ErrorState message={errorMessage(support.error)} onRetry={() => void support.refetch()} />
        ) : support.isLoading ? (
          <div className="grid grid--stats" aria-busy="true">
            {Array.from({ length: 4 }, (_, index) => (
              <div key={index} className="skeleton" style={{ height: 96 }} />
            ))}
          </div>
        ) : support.data ? (
          <div className="grid grid--stats">
            <Stat label={t('reports.opened')} value={support.data.total} />
            <Stat label={t('reports.resolved')} value={support.data.resolved} />
            <Stat label={t('reports.stillOpen')} value={support.data.open} />
            <Stat
              label={t('reports.slaBreached')}
              value={support.data.sla_breached}
              hint={
                support.data.avg_resolution_hours !== undefined
                  ? t('reports.avgResolution', { hours: Math.round(support.data.avg_resolution_hours) })
                  : undefined
              }
            />
          </div>
        ) : null}
      </section>

      <section>
        <h2 style={{ marginBottom: 'var(--space-3)' }}>
          {t('reports.pipeline')}
          {activePipeline ? `: ${activePipeline.name}` : ''}
        </h2>
        {!activePipeline ? (
          <Card>
            <p className="text-muted">{t('reports.noPipeline')}</p>
          </Card>
        ) : pipelineReport.isError ? (
          <ErrorState
            message={errorMessage(pipelineReport.error)}
            onRetry={() => void pipelineReport.refetch()}
          />
        ) : pipelineReport.isLoading ? (
          <TableSkeleton rows={4} columns={4} />
        ) : pipelineReport.data ? (
          <Card
            title={t('reports.stageBreakdown')}
            actions={
              <span className="text-muted text-sm">
                {t('reports.openWon', {
                  open: formatMoney(pipelineReport.data.total_open),
                  won: formatMoney(pipelineReport.data.total_won),
                })}
              </span>
            }
            flush
          >
            {pipelineReport.data.stages.length === 0 ? (
              <div className="card__body">
                <p className="text-muted">{t('reports.noStages')}</p>
              </div>
            ) : (
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>{t('reports.stage')}</th>
                      <th className="numeric">{t('reports.deals')}</th>
                      <th className="numeric">{t('reports.totalValue')}</th>
                      <th className="numeric">{t('reports.weighted')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pipelineReport.data.stages.map((stage) => (
                      <tr key={stage.stage_id}>
                        <td className="table__primary">{stage.stage_name}</td>
                        <td className="numeric">{stage.deal_count}</td>
                        <td className="numeric">{formatMoney(stage.total_amount)}</td>
                        <td className="numeric">{formatMoney(stage.weighted_amount)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>
        ) : null}
      </section>

      <div style={{ marginTop: 'var(--space-5)' }}>
        <Alert tone="info">{t('reports.note')}</Alert>
      </div>
    </>
  )
}
