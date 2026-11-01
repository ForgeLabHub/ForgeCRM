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
      // other failure is surfaced as-is.
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
      <PageHeader
        title="Agent approvals"
        meta="Actions an agent proposed. Nothing takes effect until a person approves it."
      />

      <div className="stack">
        {actionError ? <Alert tone="error">{actionError}</Alert> : null}

        <Card title="Awaiting a decision" flush>
          {pending.isLoading ? (
            <TableSkeleton rows={5} columns={5} />
          ) : pending.isError ? (
            <div className="card__body">
              <Alert tone="error">{errorMessage(pending.error)}</Alert>
            </div>
          ) : items.length === 0 ? (
            <div className="card__body">
              <EmptyState
                title="Nothing to review"
                text="No agent action is waiting for a decision. New proposals appear here."
              />
            </div>
          ) : (
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Action</th>
                    <th>Risk</th>
                    <th>Status</th>
                    <th>Proposed</th>
                    <th aria-label="Actions" />
                  </tr>
                </thead>
                <tbody>
                  {items.map((action) => (
                    <tr key={action.id}>
                      <td className="table__primary">
                        <div>{action.summary}</div>
                        <div className="table__secondary mono">{action.tool_name}</div>
                      </td>
                      <td>
                        <Badge tone={TIER_TONE[action.risk_tier] ?? 'neutral'}>
                          {action.risk_tier}
                        </Badge>
                      </td>
                      <td>
                        <StatusBadge status={action.status} />
                      </td>
                      <td>
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
                          Review
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
        title="Approve a destructive action?"
        text="This action is marked destructive and may be difficult or impossible to undo. It runs as soon as you approve it."
        confirmLabel="Approve and run"
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
        title="Reject this action?"
        text="The action is recorded as rejected and is never executed. The reason, if you gave one, is kept with the decision."
        confirmLabel="Reject"
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
      <div
        className="card"
        style={{ maxWidth: 720, margin: '0 auto' }}
        onClick={(e) => e.stopPropagation()}
      >
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
            <h3 className="text-sm">Arguments</h3>
            <pre className="mono text-sm" style={{ whiteSpace: 'pre-wrap', margin: 0 }}>
              {JSON.stringify(action.arguments, null, 2)}
            </pre>
          </div>

          <div>
            <h3 className="text-sm">Binding hash</h3>
            <p className="text-muted text-sm" style={{ marginBottom: 'var(--space-1)' }}>
              Your decision is recorded against this exact payload. If the arguments changed after
              you opened this screen, the server refuses the decision.
            </p>
            <div className="mono text-sm" style={{ wordBreak: 'break-all' }}>
              {action.arguments_hash}
            </div>
          </div>

          <div className="text-muted text-sm">
            Expires <Timestamp value={action.expires_at} />
          </div>

          <Field label="Comment (optional)" htmlFor="review-comment">
            <textarea
              id="review-comment"
              rows={2}
              value={comment}
              onChange={(e) => onComment(e.target.value)}
            />
          </Field>

          {stepUpNeeded ? (
            <Alert tone="warning">
              <strong>A second factor is required.</strong> This action is destructive, so the
              server refused the decision until you confirm your identity. Enter a code from your
              authenticator, or your password if the account has no second factor, then approve
              again.
              <form
                className="row"
                style={{ marginTop: 'var(--space-2)' }}
                onSubmit={(e) => {
                  e.preventDefault()
                  onStepUp()
                }}
              >
                <Field label="Multi-factor code or password" htmlFor="stepup-secret">
                  <input
                    id="stepup-secret"
                    type="password"
                    autoComplete="one-time-code"
                    value={stepUpSecret}
                    onChange={(e) => onStepUpSecret(e.target.value)}
                  />
                </Field>
                <button
                  type="submit"
                  className="btn btn--primary"
                  disabled={stepUpBusy || stepUpSecret === ''}
                >
                  {stepUpBusy ? 'Verifying…' : 'Verify'}
                </button>
              </form>
            </Alert>
          ) : null}

          {!canDecide ? (
            <Alert tone="info">
              You can review this action but not decide it. Deciding requires the approval right.
            </Alert>
          ) : null}
        </div>

        <div className="form-actions" style={{ margin: 0, padding: 'var(--space-4)' }}>
          <button type="button" className="btn btn--secondary" onClick={onClose}>
            Close
          </button>
          {canDecide ? (
            <>
              <button type="button" className="btn btn--danger" onClick={onReject} disabled={busy}>
                Reject
              </button>
              <button
                type="button"
                className="btn btn--primary"
                onClick={onApprove}
                disabled={busy || stepUpNeeded}
                title={stepUpNeeded ? 'Confirm your identity above first' : undefined}
              >
                {busy ? 'Working…' : 'Approve'}
              </button>
            </>
          ) : null}
        </div>
      </div>
    </div>
  )
}
