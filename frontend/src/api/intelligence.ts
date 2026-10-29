/**
 * The Forecasting & Risk Lab: revenue forecasts and the risk signals the platform
 * raises from the CRM.
 *
 * Both read CRM aggregates and never write CRM data. A forecast keeps the series
 * it was computed from and the accuracy it measured, so a projected number can be
 * judged rather than trusted; a risk signal keeps the observed values behind it,
 * so a warning can be explained rather than believed.
 *
 * Money in a stored document is integer minor units, never a float, so a reloaded
 * result cannot drift. Documents the server produced are rendered as-is rather
 * than re-derived here.
 */
import { api } from './client'

export type ForecastMethod = 'mean' | 'moving_average' | 'trend'

export interface ForecastCatalogEntry {
  method: ForecastMethod
  name: string
  description: string
  /** True when the method takes a moving-average window. */
  windowed: boolean
  default_window?: number
}

export interface PeriodPoint {
  period: string
  value: { amount_minor: number; currency: string }
}

export interface ForecastPoint {
  period: string
  value: { amount_minor: number; currency: string }
  lower: { amount_minor: number; currency: string }
  upper: { amount_minor: number; currency: string }
}

export interface ForecastQuality {
  method: ForecastMethod
  folds: number
  mae: { amount_minor: number; currency: string }
  rmse: { amount_minor: number; currency: string }
  /** Basis points; only defined over folds with a non-zero actual. */
  mape: { basis_points: number }
  folds_with_zero_actual: number
  naive_mae: { amount_minor: number; currency: string }
  naive_mape: { basis_points: number }
  /** True when this method beat the naive baseline on the observed series. */
  beats_naive: boolean
}

export interface ForecastResult {
  method: ForecastMethod
  algorithm_version: string
  currency: string
  /** The observed series the forecast was computed from. */
  history: PeriodPoint[]
  points: ForecastPoint[]
  quality: ForecastQuality
  assumptions: string[]
  warnings: string[]
}

export interface ForecastRun {
  id: string
  method: ForecastMethod
  algorithm_version: string
  horizon: number
  history: PeriodPoint[]
  result: ForecastResult
  created_at: string
}

export interface RunForecastInput {
  method: ForecastMethod
  window?: number
  horizon: number
  /** Trailing months of history to read; zero uses the server default. */
  months?: number
}

export type RiskSignalType =
  | 'stalled_deal'
  | 'overdue_task'
  | 'account_without_contact'
  | 'amount_spike'
  | 'conversion_drop'
  | 'stage_concentration'

export type RiskSeverity = 'info' | 'warn' | 'high' | 'critical'
export type RiskSignalStatus = 'open' | 'acknowledged' | 'resolved' | 'dismissed'

export interface RiskEvidence {
  key: string
  label: string
  value: string
}

export interface RiskSignal {
  id: string
  detected_at: string
  type: RiskSignalType
  severity: RiskSeverity
  rule: string
  entity_type: string
  entity_id: string
  entity_label: string
  rationale: string
  evidence: RiskEvidence[]
  status: RiskSignalStatus
  status_changed_at?: string
  resolved_by_scan: boolean
}

export interface RiskScanResult {
  detected: number
  opened: number
  updated: number
  resolved: number
  signals: RiskSignal[]
}

export interface RiskThresholds {
  stalled_deal_days: number
  overdue_task_days: number
  account_no_contact_days: number
  /** Basis points (10000 = 1.0), matching the detector. */
  amount_spike_ratio: number
  conversion_drop_ratio: number
  stage_concentration_ratio: number
}

export interface RiskSignalFilter {
  status?: RiskSignalStatus
  severity?: RiskSeverity
  type?: RiskSignalType
  includeTerminal?: boolean
}

export const forecastApi = {
  catalog: () => api.get<{ methods: ForecastCatalogEntry[] }>('/api/v1/forecast/catalog'),

  listRuns: () => api.get<{ runs: ForecastRun[] }>('/api/v1/forecast/runs'),

  run: (body: RunForecastInput) => api.post<ForecastRun>('/api/v1/forecast/runs', body),

  getRun: (id: string) => api.get<ForecastRun>(`/api/v1/forecast/runs/${id}`),
}

export const riskApi = {
  listSignals: (filter: RiskSignalFilter = {}) => {
    const params = new URLSearchParams()
    if (filter.status) params.set('status', filter.status)
    if (filter.severity) params.set('severity', filter.severity)
    if (filter.type) params.set('type', filter.type)
    if (filter.includeTerminal) params.set('include_terminal', 'true')
    const query = params.toString()
    return api.get<{ signals: RiskSignal[] }>(`/api/v1/risk/signals${query ? `?${query}` : ''}`)
  },

  scan: () => api.post<RiskScanResult>('/api/v1/risk/scan', {}),

  getSignal: (id: string) => api.get<RiskSignal>(`/api/v1/risk/signals/${id}`),

  setStatus: (id: string, status: RiskSignalStatus) =>
    api.post<RiskSignal>(`/api/v1/risk/signals/${id}/status`, { status }),

  getThresholds: () => api.get<RiskThresholds>('/api/v1/risk/thresholds'),

  setThresholds: (body: RiskThresholds) => api.put<RiskThresholds>('/api/v1/risk/thresholds', body),
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

/** Renders a basis-point ratio as a percentage. 10000 bp is 100%. */
export function formatBasisPoints(bp: number): string {
  return `${(bp / 100).toFixed(2)}%`
}

/** The human label for a signal type. */
export function signalTypeLabel(t: RiskSignalType): string {
  switch (t) {
    case 'stalled_deal':
      return 'Stalled deal'
    case 'overdue_task':
      return 'Overdue task'
    case 'account_without_contact':
      return 'Account without contact'
    case 'amount_spike':
      return 'Unusual deal amount'
    case 'conversion_drop':
      return 'Conversion drop'
    case 'stage_concentration':
      return 'Stage concentration'
  }
}
