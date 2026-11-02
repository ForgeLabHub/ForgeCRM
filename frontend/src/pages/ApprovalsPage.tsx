import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { agentApi, stepUp } from '../api/agent'
import { errorMessage } from '../api/client'
import type { AgentAction } from '../types/api'
import { useAuth } from '../hooks/useAuth'
import {
  Alert,
  Badge,
  Card,
  ConfirmDialog,
  EmptyState,
  Field,
  PageHeader,
  StatusBadge,
  TableSkeleton,
  Timestamp,
} from '../components/ui'
import { useT, type TFunction } from '../i18n'

const TIER_TONE = {
  safe: 'neutral',
  confirm: 'warning',
  destructive: 'danger',
} as const

/**
 * Human-in-the-loop approvals (ADR 0005).
 *
 * The agent proposes; a person decides. Two properties of this screen matter and
 * are enforced by the server, not here:
 *
 *  - The decision carries the arguments hash the operator was shown. The server
 *    refuses a decision that does not match the stored hash, so approving cannot be
 *    turned into approving something else.
 *  - A destructive action additionally requires that the deciding session presented
 *    a second factor recently. When the server answers 403 with that reason, the
 *    operator steps up and retries. The step-up is deliberately not a checkbox: it
 *    re-verifies a factor and refreshes the session, which is a server fact.
 */
export function ApprovalsPage() {
  const t = useT()
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [detail, setDetail] = useState<AgentAction | null>(null)
  const [rejecting, setRejecting] = useState<AgentAction | null>(null)
  const [confirming, setConfirming] = useState<AgentAction | null>(null)
  const [comment, setComment] = useState('')
  const [actionError, setActionError] = useState<string | null>(null)
  const [stepUpNeeded, setStepUpNeeded] = useState(false)
  const [stepUpSecret, setStepUpSecret] = useState('')

  const pending = useQuery({
    queryKey: ['agent', 'actions', 'proposed'],
    queryFn: () => agentApi.actions({ status: 'proposed', limit: 50 }),
  })

  const decide = useMutation({
    mutationFn: (args: { action: AgentAction; approve: boolean; comment?: string }) =>
      agentApi.decide(args.action.id, {
        approve: args.approve,
        arguments_hash: args.action.arguments_hash,
        comment: args.comment,
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['agent', 'actions'] })
      setDetail(null)
      setRejecting(null)
      setConfirming(null)
      setComment('')
      setActionError(null)
      setStepUpNeeded(false)
    },
    onError: (error) => {
      // The one failure the operator can act on is a missing second factor. Every
      // other failure is surfaced as-is. The server's message is matched in English
      // because it is part of the API contract, not a localized string.
      const message = errorMessage(error)
      setActionError(message)
      if (message.toLowerCase().includes('second authentication factor')) {
        // Drop the confirmation overlay so the step-up form in the review dialog is
        // reachable. Leaving it up would stack two modals and hide the only control
        // that can resolve the refusal.
        setConfirming(null)
        setStepUpNeeded(true)
      }
    },
  })

  const doStepUp = useMutation({
    mutationFn: (secret: string) =>
      // With MFA the server expects a TOTP code, without it the password. Sending
      // both fields lets the server decide, and the UI does not have to know.
      stepUp({ code: secret, password: secret }),
    onSuccess: () => {
      setStepUpNeeded(false)
      setStepUpSecret('')
      setActionError(null)
    },
    onError: (error) => setActionError(errorMessage(error)),
  })

  const items = pending.data?.items ?? []
  const canDecide = can('approvals.decide')

  return (
    <>
      <PageHeader title={t('approvals.title')} meta={t('approvals.meta')} />

      <div className="stack">
        {actionError ? <Alert tone="error">{actionError}</Alert> : null}

        <Card title={t('approvals.awaiting')} flush>
          {pending.isLoading ? (
            <TableSkeleton rows={5} columns={5} />
          ) : pending.isError ? (
            <div className="card__body">
              <Alert tone="error">{errorMessage(pending.error)}</Alert>
            </div>
          ) : items.length === 0 ? (
            <div className="card__body">
              <EmptyState title={t('approvals.emptyTitle')} text={t('approvals.emptyText')} />
            </div>
          ) : (
            <div className="table-wrap">
              <table className="table--stack">
                <thead>
                  <tr>
                    <th>{t('approvals.columnAction')}</th>
                    <th>{t('approvals.columnRisk')}</th>
                    <th>{t('approvals.columnStatus')}</th>
                    <th>{t('approvals.columnProposed')}</th>
                    <th aria-label={t('approvals.columnAction')} />
                  </tr>
                </thead>
                <tbody>
                  {items.map((action) => (
                    <tr key={action.id}>
                      <td className="table__primary" data-label={t('approvals.columnAction')}>
                        <div>{action.summary}</div>
                        <div className="table__secondary mono">{action.tool_name}</div>
                      </td>
                      <td data-label={t('approvals.columnRisk')}>
                        <Badge tone={TIER_TONE[action.risk_tier] ?? 'neutral'}>{action.risk_tier}</Badge>
                      </td>
                      <td data-label={t('approvals.columnStatus')}>
                        <StatusBadge status={action.status} />
                      </td>
                      <td data-label={t('approvals.columnProposed')}>
                        <Timestamp value={action.proposed_at} />
                      </td>
                      <td className="table__actions">
                        <button
                          type="button"
                          className="btn btn--secondary btn--sm"
                          onClick={() => {
                            setDetail(action)
                            setActionError(null)
                            setStepUpNeeded(false)
                          }}
                        >
                          {t('approvals.review')}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      </div>

      {detail ? (
        <ReviewDialog
          t={t}
          action={detail}
          canDecide={canDecide}
          busy={decide.isPending}
          comment={comment}
          onComment={setComment}
          stepUpNeeded={stepUpNeeded}
          stepUpSecret={stepUpSecret}
          onStepUpSecret={setStepUpSecret}
          stepUpBusy={doStepUp.isPending}
          onStepUp={() => doStepUp.mutate(stepUpSecret)}
          onClose={() => {
            setDetail(null)
            setActionError(null)
            setStepUpNeeded(false)
            setStepUpSecret('')
            setComment('')
          }}
          onApprove={() => {
            if (detail.risk_tier === 'destructive') {
              setConfirming(detail)
            } else {
              decide.mutate({ action: detail, approve: true, comment: comment.trim() || undefined })
            }
          }}
          onReject={() => setRejecting(detail)}
        />
      ) : null}

      <ConfirmDialog
        open={confirming !== null}
        title={t('approvals.approveDestructiveTitle')}
        text={t('approvals.approveDestructiveText')}
        confirmLabel={t('approvals.approveAndRun')}
        danger
        busy={decide.isPending}
        onCancel={() => setConfirming(null)}
        onConfirm={() => {
          if (confirming) {
            decide.mutate({
              action: confirming,
              approve: true,
              comment: comment.trim() || undefined,
            })
          }
        }}
      />

      <ConfirmDialog
        open={rejecting !== null}
        title={t('approvals.rejectTitle')}
        text={t('approvals.rejectText')}
        confirmLabel={t('approvals.reject')}
        danger
        busy={decide.isPending}
        onCancel={() => {
          setRejecting(null)
          setComment('')
        }}
        onConfirm={() => {
          if (rejecting) {
            decide.mutate({
              action: rejecting,
              approve: false,
              comment: comment.trim() || undefined,
            })
          }
        }}
      />
    </>
  )
}

/** ReviewDialog shows exactly what will run, and binds the decision to it. */
function ReviewDialog({
  t,
  action,
  canDecide,
  busy,
  comment,
  onComment,
  stepUpNeeded,
  stepUpSecret,
  onStepUpSecret,
  stepUpBusy,
  onStepUp,
  onClose,
  onApprove,
  onReject,
}: {
  t: TFunction
  action: AgentAction
  canDecide: boolean
  busy: boolean
  comment: string
  onComment: (value: string) => void
  stepUpNeeded: boolean
  stepUpSecret: string
  onStepUpSecret: (value: string) => void
  stepUpBusy: boolean
  onStepUp: () => void
  onClose: () => void
  onApprove: () => void
  onReject: () => void
}) {
  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="agent-action-title"
      style={{
        position: 'fixed',
        inset: 0,
        background: 'var(--color-overlay)',
        zIndex: 60,
        overflowY: 'auto',
        padding: 'var(--space-5)',
      }}
      onClick={onClose}
    >
      <div className="card" style={{ maxWidth: 720, margin: '0 auto' }} onClick={(e) => e.stopPropagation()}>
        <div className="card__header">
          <h2 id="agent-action-title">{action.summary}</h2>
          <div className="row" style={{ marginTop: 'var(--space-2)' }}>
            <Badge tone={TIER_TONE[action.risk_tier] ?? 'neutral'}>{action.risk_tier}</Badge>
            <StatusBadge status={action.status} />
            <span className="mono text-sm text-muted">{action.tool_name}</span>
          </div>
        </div>

        <div className="card__body stack">
          <div>
            <h3 className="text-sm">{t('approvals.arguments')}</h3>
            <pre className="mono text-sm" style={{ whiteSpace: 'pre-wrap', margin: 0 }}>
              {JSON.stringify(action.arguments, null, 2)}
            </pre>
          </div>

          <div>
            <h3 className="text-sm">{t('approvals.bindingHash')}</h3>
            <p className="text-muted text-sm" style={{ marginBottom: 'var(--space-1)' }}>
              {t('approvals.bindingHashNote')}
            </p>
            <div className="mono text-sm" style={{ wordBreak: 'break-all' }}>
              {action.arguments_hash}
            </div>
          </div>

          <div className="text-muted text-sm">
            {t('approvals.expires')} <Timestamp value={action.expires_at} />
          </div>

          <Field label={t('approvals.commentOptional')} htmlFor="review-comment">
            <textarea id="review-comment" rows={2} value={comment} onChange={(e) => onComment(e.target.value)} />
          </Field>

          {stepUpNeeded ? (
            <Alert tone="warning">
              <strong>{t('approvals.stepUpTitle')}</strong> {t('approvals.stepUpText')}
              <form
                className="row"
                style={{ marginTop: 'var(--space-2)' }}
                onSubmit={(e) => {
                  e.preventDefault()
                  onStepUp()
                }}
              >
                <Field label={t('approvals.stepUpField')} htmlFor="stepup-secret">
                  <input
                    id="stepup-secret"
                    type="password"
                    autoComplete="one-time-code"
                    value={stepUpSecret}
                    onChange={(e) => onStepUpSecret(e.target.value)}
                  />
                </Field>
                <button type="submit" className="btn btn--primary" disabled={stepUpBusy || stepUpSecret === ''}>
                  {stepUpBusy ? t('approvals.verifying') : t('approvals.verify')}
                </button>
              </form>
            </Alert>
          ) : null}

          {!canDecide ? <Alert tone="info">{t('approvals.readOnly')}</Alert> : null}
        </div>

        <div className="form-actions" style={{ margin: 0, padding: 'var(--space-4)' }}>
          <button type="button" className="btn btn--secondary" onClick={onClose}>
            {t('common.close')}
          </button>
          {canDecide ? (
            <>
              <button type="button" className="btn btn--danger" onClick={onReject} disabled={busy}>
                {t('approvals.reject')}
              </button>
              <button
                type="button"
                className="btn btn--primary"
                onClick={onApprove}
                disabled={busy || stepUpNeeded}
                title={stepUpNeeded ? t('approvals.stepUpFirst') : undefined}
              >
                {busy ? t('common.working') : t('approvals.approve')}
              </button>
            </>
          ) : null}
        </div>
      </div>
    </div>
  )
}
