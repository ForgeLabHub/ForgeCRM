/**
 * The Simulation Lab: saved "what if" scenarios and the runs that compute them.
 *
 * The lab reads CRM aggregates and computes a comparison against them; it never
 * writes CRM data. A result is stored with the parameters and the baseline it was
 * computed from, so a number shown here can always be explained after the CRM
 * data has moved on.
 *
 * Money in a stored result is integer minor units, never a float, so a reloaded
 * result cannot drift. The comparison is returned as the exact document the
 * server produced; the page renders it rather than re-deriving it.
 */
import { api, type PaginationParams } from './client'

export type ScenarioKind = 'funnel' | 'revenue' | 'retention'

export interface ParamSpec {
  key: string
  label: string
  description: string
  default: string
  /** "factor" or "percent"; the control is labelled accordingly. */
  kind: string
  /** True when the parameter is per pipeline stage. */
  stage_scoped: boolean
}

export interface CatalogEntry {
  kind: ScenarioKind
  name: string
  description: string
  parameters: ParamSpec[]
}

export interface Scenario {
  id: string
  name: string
  description?: string
  kind: ScenarioKind
  /** The parameter document. Percentages are relative changes (20 means 20% more). */
  params: Record<string, unknown>
  algorithm_version: string
  owner_user_id?: string
  created_at: string
  updated_at: string
}

export type MetricUnit = 'money' | 'count' | 'percent'

export interface Metric {
  key: string
  label: string
  unit: MetricUnit
  currency?: string
  /** In the metric's unit: minor units for money, a whole number for count. */
  baseline: number
  variant: number
  delta: number
  relative_basis_points?: number
  relative_defined: boolean
}

export interface Comparison {
  kind: ScenarioKind
  algorithm_version: string
  currency: string
  metrics: Metric[]
  /** The stated modelling assumptions; shown next to the numbers. */
  assumptions: string[]
  /** Inputs accepted but worth flagging, e.g. a conversion for an unknown stage. */
  warnings: string[]
}

export interface Baseline {
  Currency: string
  Stages: Array<{
    StageID: string
    StageName: string
    Position: number
    OpenCount: number
    OpenValue: { AmountMinor: number; Currency: string }
    Probability: { basis_points: number }
  }>
  WonCount: number
  LostCount: number
  OpenCount: number
  ActiveCustomers: number
  ChurnedCustomers: number
  PeriodDays: number
}

export interface PreviewResponse {
  comparison: Comparison
  baseline: Baseline
  warnings?: string[]
}

export interface SimulationRun {
  id: string
  scenario_id?: string
  kind: ScenarioKind
  algorithm_version: string
  params: Record<string, unknown>
  baseline: unknown
  result: Comparison
  created_at: string
}

export interface CreateScenarioInput {
  name: string
  description?: string
  kind: ScenarioKind
  params: Record<string, unknown>
}

export interface RunInput {
  pipeline_id?: string
  from?: string
  to?: string
}

export interface PreviewInput {
  kind: ScenarioKind
  params: Record<string, unknown>
  pipeline_id?: string
  from?: string
  to?: string
}

// The list endpoints return bare arrays rather than a paginated envelope: a
// scenario list and a run history are both naturally bounded, so pagination would
// be ceremony.
export const simulationApi = {
  catalog: () => api.get<{ scenarios: CatalogEntry[] }>('/api/v1/simulation/catalog'),

  listScenarios: () => api.get<{ scenarios: Scenario[] }>('/api/v1/simulation/scenarios'),

  createScenario: (body: CreateScenarioInput) =>
    api.post<Scenario>('/api/v1/simulation/scenarios', body),

  getScenario: (id: string) => api.get<Scenario>(`/api/v1/simulation/scenarios/${id}`),

  archiveScenario: (id: string) => api.delete(`/api/v1/simulation/scenarios/${id}`),

  run: (id: string, body: RunInput = {}) =>
    api.post<SimulationRun>(`/api/v1/simulation/scenarios/${id}/runs`, body),

  runs: (id: string) => api.get<{ runs: SimulationRun[] }>(`/api/v1/simulation/scenarios/${id}/runs`),

  preview: (body: PreviewInput) => api.post<PreviewResponse>('/api/v1/simulation/preview', body),
}

// PaginationParams is re-exported so a caller that later pages a list does not
// have to reach into client for the type.
export type { PaginationParams }

/** Renders a metric's integer value in its own unit, with no float conversion. */
export function formatMetricValue(metric: Metric, value: number): string {
  switch (metric.unit) {
    case 'money':
      return formatMinorUnits(value, metric.currency ?? 'USD')
    case 'percent':
      // Basis points: 10000 is 100%.
      return `${(value / 100).toFixed(2)}%`
    case 'count':
    default:
      return value.toLocaleString()
  }
}

/** Formats integer minor units as a currency string without ever using a float. */
export function formatMinorUnits(minor: number, currency = 'USD'): string {
  const negative = minor < 0
  const absolute = Math.abs(minor)
  const major = Math.trunc(absolute / 100)
  const cents = absolute % 100
  const body = `${major.toLocaleString()}.${String(cents).padStart(2, '0')}`
  let formatted: string
  try {
    formatted = new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency,
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(Number(`${negative ? '-' : ''}${major}.${String(cents).padStart(2, '0')}`))
  } catch {
    formatted = `${body} ${currency}`
  }
  return formatted
}
