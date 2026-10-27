import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { pipelinesApi, reportsApi } from '../api/crm'
import { errorMessage } from '../api/client'
import { Alert, Card, ErrorState, PageHeader, Stat, TableSkeleton, formatMoney } from '../components/ui'

function defaultRange(): { from: string; to: string } {
  const to = new Date()
  const from = new Date()
  from.setDate(from.getDate() - 30)
  const iso = (date: Date) => date.toISOString().slice(0, 10)
  return { from: iso(from), to: iso(to) }
}

export function ReportsPage() {
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
        title="Reports"
        meta="Sales and support performance over a chosen period."
        actions={
          <div className="row">
            <label className="field">
              <span className="field__label">From</span>
              <input
                type="date"
                value={range.from}
                onChange={(event) => setRange((previous) => ({ ...previous, from: event.target.value }))}
              />
            </label>
            <label className="field">
              <span className="field__label">To</span>
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
        <h2 style={{ marginBottom: 'var(--space-3)' }}>Sales</h2>
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
              <Stat
                label="Won"
                value={sales.data.won_count}
                hint={formatMoney(sales.data.won_amount)}
              />
              <Stat
                label="Lost"
                value={sales.data.lost_count}
                hint={formatMoney(sales.data.lost_amount)}
              />
              <Stat
                label="Open"
                value={sales.data.open_count}
                hint={formatMoney(sales.data.open_amount)}
              />
              <Stat
                label="Win rate"
                value={`${(sales.data.win_rate * 100).toFixed(1)}%`}
                hint="Won ÷ (won + lost)"
              />
            </div>

            {sales.data.by_owner?.length ? (
              <div style={{ marginTop: 'var(--space-4)' }}>
                <Card title="By owner">
                  <div className="table-wrap">
                    <table>
                      <thead>
                        <tr>
                          <th>Owner</th>
                          <th className="numeric">Deals</th>
                          <th className="numeric">Amount</th>
                        </tr>
                      </thead>
                      <tbody>
                        {sales.data.by_owner.map((row, index) => (
                          <tr key={row.owner_user_id ?? index}>
                            <td className="mono">{row.owner_user_id?.slice(0, 8) ?? 'Unassigned'}</td>
                            <td className="numeric">{row.count}</td>
                            <td className="numeric">{formatMoney(row.amount)}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </Card>
              </div>
            ) : null}
          </>
        ) : null}
      </section>

      <section style={{ marginBottom: 'var(--space-5)' }}>
        <h2 style={{ marginBottom: 'var(--space-3)' }}>Support</h2>
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
            <Stat label="Opened" value={support.data.opened} />
            <Stat label="Resolved" value={support.data.resolved} />
            <Stat label="Still open" value={support.data.open} />
            <Stat
              label="SLA breached"
              value={support.data.sla_breached}
              hint={
                support.data.avg_resolution_minutes !== undefined
                  ? `Avg resolution ${Math.round(support.data.avg_resolution_minutes)} min`
                  : undefined
              }
            />
          </div>
        ) : null}
      </section>

      <section>
        <h2 style={{ marginBottom: 'var(--space-3)' }}>
          Pipeline{activePipeline ? `: ${activePipeline.name}` : ''}
        </h2>
        {!activePipeline ? (
          <Card>
            <p className="text-muted">No pipeline is configured.</p>
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
            title="Stage breakdown"
            actions={
              <span className="text-muted text-sm">
                Weighted {formatMoney(pipelineReport.data.weighted_amount)} of{' '}
                {formatMoney(pipelineReport.data.total_amount)}
              </span>
            }
            flush
          >
            {pipelineReport.data.stages.length === 0 ? (
              <div className="card__body">
                <p className="text-muted">No stages defined for this pipeline.</p>
              </div>
            ) : (
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>Stage</th>
                      <th className="numeric">Deals</th>
                      <th className="numeric">Total value</th>
                      <th className="numeric">Weighted</th>
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
        <Alert tone="info">
          Reports reflect the period selected above. Amounts are shown in each deal's own currency,
          so a mixed-currency pipeline sums nominal values rather than converting them.
        </Alert>
      </div>
    </>
  )
}
