import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  riskApi,
  signalTypeLabel,
  type RiskSignal,
  type RiskSignalStatus,
  type RiskThresholds,
} from '../api/intelligence'
import { errorMessage } from '../api/client'
import { useAuth } from '../hooks/useAuth'
import {
  Alert,
  Badge,
  Card,
  EmptyState,
  ErrorState,
  Field,
  PageHeader,
  Stat,
  StatusBadge,
  Timestamp,
} from '../components/ui'
import { useT, type TFunction } from '../i18n'

/**
 * The Risk page.
 *
 * Every signal is explainable: the page shows the reason the platform raised it
 * and the observed values behind the decision, so a warning can be judged rather
 * than trusted. A scan reconciles — it opens a signal for a condition that holds,
 * keeps an operator's acknowledgement across a re-scan, and closes a signal whose
 * condition no longer holds — so the list reflects the CRM's current state and not
 * an ever-growing log.
 *
 * The page never writes CRM data. Moving a signal's status records the operator's
 * decision; it does not change the CRM record the signal is about.
 */
export function RiskPage() {
  const t = useT()
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [showTerminal, setShowTerminal] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)

  const canManage = can('risk.manage')

  const signalsQuery = useQuery({
    queryKey: ['risk', 'signals', showTerminal],
    queryFn: () => riskApi.listSignals({ includeTerminal: showTerminal }),
  })

  const scan = useMutation({
    mutationFn: () => riskApi.scan(),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['risk', 'signals'] })
      setActionError(null)
    },
    onError: (error) => setActionError(errorMessage(error)),
  })

  const setStatus = useMutation({
    mutationFn: ({ id, status }: { id: string; status: RiskSignalStatus }) => riskApi.setStatus(id, status),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['risk', 'signals'] })
      setActionError(null)
    },
    onError: (error) => setActionError(errorMessage(error)),
  })

  const signals = signalsQuery.data?.signals ?? []
  const bySeverity = countBySeverity(signals)

  return (
    <>
      <PageHeader
        title={t('risk.title')}
        meta={t('risk.meta')}
        actions={
          <button type="button" className="btn btn--primary" disabled={scan.isPending} onClick={() => scan.mutate()}>
            {scan.isPending ? t('risk.scanning') : t('risk.scanNow')}
          </button>
        }
      />

      {actionError ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="error">{actionError}</Alert>
        </div>
      ) : null}

      {scan.data ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="info">
            {t('risk.scanSummary', {
              detected: scan.data.detected,
              opened: scan.data.opened,
              updated: scan.data.updated,
              resolved: scan.data.resolved,
            })}
          </Alert>
        </div>
      ) : null}

      <div className="grid grid--stats" style={{ marginBottom: 'var(--space-5)' }}>
        <Stat label={t('risk.critical')} value={bySeverity.critical} />
        <Stat label={t('risk.high')} value={bySeverity.high} />
        <Stat label={t('risk.warning')} value={bySeverity.warn} />
        <Stat label={t('risk.info')} value={bySeverity.info} />
      </div>

      <Card
        title={t('risk.signals')}
        actions={
          <label className="field__label" style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'center' }}>
            <input type="checkbox" checked={showTerminal} onChange={(event) => setShowTerminal(event.target.checked)} />
            {t('risk.showResolved')}
          </label>
        }
      >
        {signalsQuery.isLoading ? (
          <p className="text-subtle">{t('common.loading')}</p>
        ) : signalsQuery.isError ? (
          <ErrorState message={errorMessage(signalsQuery.error)} onRetry={() => void signalsQuery.refetch()} />
        ) : signals.length === 0 ? (
          <EmptyState title={t('risk.noSignalsTitle')} text={t('risk.noSignalsText')} />
        ) : (
          <div className="stack">
            {signals.map((signal) => (
              <SignalCard
                key={signal.id}
                t={t}
                signal={signal}
                busy={setStatus.isPending}
                onStatus={(status) => setStatus.mutate({ id: signal.id, status })}
              />
            ))}
          </div>
        )}
      </Card>

      <div style={{ marginTop: 'var(--space-5)' }}>
        <ThresholdsCard t={t} canManage={canManage} />
      </div>
    </>
  )
}

function SignalCard({
  t,
  signal,
  busy,
  onStatus,
}: {
  t: TFunction
  signal: RiskSignal
  busy: boolean
  onStatus: (status: RiskSignalStatus) => void
}) {
  return (
    <div className="card" style={{ padding: 'var(--space-4)' }}>
      <div className="row" style={{ justifyContent: 'space-between', alignItems: 'flex-start' }}>
        <div>
          <div className="row" style={{ gap: 'var(--space-2)', alignItems: 'center' }}>
            <Badge tone={severityTone(signal.severity)}>{signal.severity}</Badge>
            <strong>{signalTypeLabel(signal.type)}</strong>
            {signal.entity_label ? <span className="text-subtle">· {signal.entity_label}</span> : null}
          </div>
          <p style={{ marginTop: 'var(--space-2)' }}>{signal.rationale}</p>
        </div>
        <div className="stack" style={{ alignItems: 'flex-end', gap: 'var(--space-2)' }}>
          <StatusBadge status={signal.status} />
          <Timestamp value={signal.detected_at} />
        </div>
      </div>

      {signal.evidence.length > 0 ? (
        <dl className="row" style={{ gap: 'var(--space-4)', marginTop: 'var(--space-3)', flexWrap: 'wrap' }}>
          {signal.evidence.map((e) => (
            <div key={e.key}>
              <dt className="text-subtle text-sm">{e.label}</dt>
              <dd>{e.value}</dd>
            </div>
          ))}
        </dl>
      ) : null}

      {signal.status === 'open' || signal.status === 'acknowledged' ? (
        <div className="row" style={{ gap: 'var(--space-2)', marginTop: 'var(--space-3)' }}>
          {signal.status === 'open' ? (
            <button type="button" className="btn btn--secondary" disabled={busy} onClick={() => onStatus('acknowledged')}>
              {t('risk.acknowledge')}
            </button>
          ) : null}
          <button type="button" className="btn btn--secondary" disabled={busy} onClick={() => onStatus('resolved')}>
            {t('risk.resolve')}
          </button>
          <button type="button" className="btn btn--ghost" disabled={busy} onClick={() => onStatus('dismissed')}>
            {t('risk.dismiss')}
          </button>
        </div>
      ) : (
        <p className="text-subtle" style={{ marginTop: 'var(--space-2)' }}>
          {signal.resolved_by_scan ? t('risk.closedAuto') : t('risk.closedByOperator')}
        </p>
      )}
    </div>
  )
}

function ThresholdsCard({ t, canManage }: { t: TFunction; canManage: boolean }) {
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<RiskThresholds | null>(null)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const query = useQuery({ queryKey: ['risk', 'thresholds'], queryFn: () => riskApi.getThresholds() })

  const save = useMutation({
    mutationFn: (body: RiskThresholds) => riskApi.setThresholds(body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['risk', 'thresholds'] })
      setSaved(true)
      setError(null)
    },
    onError: (e) => {
      setSaved(false)
      setError(errorMessage(e))
    },
  })

  const current = draft ?? query.data ?? null
  if (!current) {
    return (
      <Card title={t('risk.thresholds')}>
        {query.isError ? (
          <ErrorState message={errorMessage(query.error)} onRetry={() => void query.refetch()} />
        ) : (
          <p className="text-subtle">{t('common.loading')}</p>
        )}
      </Card>
    )
  }

  const field = (key: keyof RiskThresholds, label: string, hint?: string) => (
    <Field label={label} htmlFor={`threshold-${key}`} hint={hint}>
      <input
        id={`threshold-${key}`}
        className="input"
        type="number"
        min={0}
        value={current[key]}
        disabled={!canManage}
        onChange={(event) => {
          setSaved(false)
          setDraft({ ...current, [key]: Number(event.target.value) })
        }}
      />
    </Field>
  )

  return (
    <Card
      title={t('risk.thresholds')}
      actions={saved ? <Badge tone="success">{t('risk.saved')}</Badge> : undefined}
    >
      {error ? (
        <div style={{ marginBottom: 'var(--space-3)' }}>
          <Alert tone="error">{error}</Alert>
        </div>
      ) : null}
      <div className="grid grid--stats">
        {field('stalled_deal_days', t('risk.stalledDealDays'), t('risk.stalledDealDaysHint'))}
        {field('overdue_task_days', t('risk.overdueTaskDays'), t('risk.overdueTaskDaysHint'))}
        {field('account_no_contact_days', t('risk.accountNoContactDays'))}
        {field('amount_spike_ratio', t('risk.amountSpikeRatio'), t('risk.amountSpikeRatioHint'))}
        {field('conversion_drop_ratio', t('risk.conversionDropRatio'), t('risk.conversionDropRatioHint'))}
        {field('stage_concentration_ratio', t('risk.stageConcentrationRatio'), t('risk.stageConcentrationRatioHint'))}
      </div>
      {canManage ? (
        <div style={{ marginTop: 'var(--space-4)' }}>
          <button
            type="button"
            className="btn btn--primary"
            disabled={save.isPending}
            onClick={() => save.mutate(current)}
          >
            {save.isPending ? t('common.saving') : t('risk.saveThresholds')}
          </button>
        </div>
      ) : (
        <Alert tone="info">{t('risk.noThresholdPermission')}</Alert>
      )}
    </Card>
  )
}

function countBySeverity(signals: RiskSignal[]) {
  const out = { info: 0, warn: 0, high: 0, critical: 0 }
  for (const s of signals) {
    if (s.status === 'open' || s.status === 'acknowledged') out[s.severity] += 1
  }
  return out
}

function severityTone(severity: RiskSignal['severity']): 'neutral' | 'success' | 'danger' | 'warning' | 'accent' {
  switch (severity) {
    case 'critical':
      return 'danger'
    case 'high':
      return 'warning'
    case 'warn':
      return 'accent'
    default:
      return 'neutral'
  }
}
