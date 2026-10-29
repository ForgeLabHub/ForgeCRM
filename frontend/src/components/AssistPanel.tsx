import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { assistApi } from '../api/assist'
import type { AssistEntityType, AssistInsight, AssistKind } from '../api/assist'
import { errorMessage } from '../api/client'
import { useAuth } from '../hooks/useAuth'
import { Alert, Badge, Card, Skeleton, Timestamp } from './ui'

/**
 * Contextual AI for one record.
 *
 * Two things this component must not do, because the server cannot stop it:
 *
 *   1. Present the model's analysis as a CRM value. The facts block is labelled
 *      as coming from the CRM and the analysis block as the model's own output,
 *      and the two are never merged.
 *   2. Act on a draft. The draft is shown in a text area the user copies from;
 *      there is no send button, because sending is a CRM action the user takes
 *      deliberately, not something this panel does on the model's behalf.
 */
const KINDS: { value: AssistKind; label: string }[] = [
  { value: 'summary', label: 'Summary' },
  { value: 'risks', label: 'Risks' },
  { value: 'next_actions', label: 'Next actions' },
  { value: 'wrap_up', label: 'Wrap up' },
  { value: 'draft', label: 'Draft a message' },
  { value: 'ask', label: 'Ask a question' },
]

export function AssistPanel({
  entityType,
  entityId,
}: {
  entityType: AssistEntityType
  entityId: string
}) {
  const { can } = useAuth()
  const [kind, setKind] = useState<AssistKind>('summary')
  const [question, setQuestion] = useState('')
  const [recipientHint, setRecipientHint] = useState('')

  const status = useQuery({
    queryKey: ['ai', 'status'],
    queryFn: () => assistApi.status(),
    enabled: can('ai.read'),
  })

  const context = useQuery({
    queryKey: ['ai', 'assist', entityType, entityId, 'context'],
    queryFn: () => assistApi.context(entityType, entityId),
    enabled: can('ai.read') && Boolean(entityId),
  })

  const run = useMutation({
    mutationFn: (refresh: boolean) =>
      assistApi.run(entityType, entityId, {
        kind,
        question: kind === 'ask' ? question : undefined,
        recipient_hint: kind === 'draft' && recipientHint ? recipientHint : undefined,
        refresh,
      }),
  })

  if (!can('ai.read')) {
    return null
  }

  if (status.isLoading) {
    return (
      <Card title="Assistant">
        <Skeleton height={80} />
      </Card>
    )
  }

  if (status.data && !status.data.enabled) {
    return (
      <Card title="Assistant">
        <Alert tone="info">
          No AI provider is configured, so contextual assistance is unavailable. The rest of the
          record works normally.
        </Alert>
      </Card>
    )
  }

  const insight = run.data

  return (
    <Card
      title="Assistant"
      actions={
        insight ? (
          <button
            type="button"
            className="btn btn--ghost btn--sm"
            onClick={() => run.mutate(true)}
            disabled={run.isPending}
          >
            Ask again
          </button>
        ) : null
      }
    >
      <div className="stack">
        <div className="row" style={{ gap: 'var(--space-2)', flexWrap: 'wrap' }}>
          {KINDS.map((k) => (
            <button
              key={k.value}
              type="button"
              className={`btn btn--sm ${kind === k.value ? 'btn--primary' : 'btn--secondary'}`}
              onClick={() => setKind(k.value)}
            >
              {k.label}
            </button>
          ))}
        </div>

        {kind === 'ask' ? (
          <label className="field" htmlFor="assist-question">
            <span className="field__label">Your question</span>
            <input
              id="assist-question"
              className="input"
              value={question}
              maxLength={500}
              placeholder="What is blocking this deal?"
              onChange={(e) => setQuestion(e.target.value)}
            />
            <span className="field__hint">
              Answered only from this record's data, which is limited to what you may see.
            </span>
          </label>
        ) : null}

        {kind === 'draft' ? (
          <label className="field" htmlFor="assist-recipient">
            <span className="field__label">Who is the message for? (optional)</span>
            <input
              id="assist-recipient"
              className="input"
              value={recipientHint}
              maxLength={200}
              placeholder="the customer's procurement lead"
              onChange={(e) => setRecipientHint(e.target.value)}
            />
          </label>
        ) : null}

        <div className="row">
          <button
            type="button"
            className="btn btn--primary btn--sm"
            onClick={() => run.mutate(false)}
            disabled={run.isPending || (kind === 'ask' && question.trim() === '')}
          >
            {run.isPending ? 'Asking…' : 'Run'}
          </button>
          {insight ? (
            <span className="text-muted text-sm">
              {insight.cached ? 'From cache · ' : ''}
              {insight.provider}
              {insight.model ? ` / ${insight.model}` : ''} ·{' '}
              <Timestamp value={insight.generated_at} />
            </span>
          ) : null}
        </div>

        {run.isError ? <Alert tone="error">{errorMessage(run.error)}</Alert> : null}

        {insight ? <InsightView insight={insight} /> : null}

        {!insight && context.data ? <ContextPreview context={context.data} /> : null}
      </div>
    </Card>
  )
}

/** Renders one insight, keeping CRM facts and model output visibly apart. */
function InsightView({ insight }: { insight: AssistInsight }) {
  return (
    <div className="stack">
      {insight.disclosures.length ? (
        <Alert tone="warning">
          <ul style={{ margin: 0, paddingLeft: '1.1rem' }}>
            {insight.disclosures.map((d, i) => (
              <li key={i}>{d.detail}</li>
            ))}
          </ul>
        </Alert>
      ) : null}

      <section>
        <h3 className="text-sm">What the assistant was told (from the CRM)</h3>
        <ul className="text-sm" style={{ margin: 0, paddingLeft: '1.1rem' }}>
          {insight.facts.map((f, i) => (
            <li key={i}>
              <strong>{f.label}:</strong> {f.value}
            </li>
          ))}
        </ul>
      </section>

      <section>
        <h3 className="text-sm">
          Analysis <Badge tone="neutral">model output</Badge>
        </h3>
        <p style={{ whiteSpace: 'pre-wrap' }}>{insight.analysis}</p>
      </section>

      {insight.recommendations.length ? (
        <section>
          <h3 className="text-sm">
            Suggested next actions <Badge tone="neutral">model output</Badge>
          </h3>
          <ul style={{ margin: 0, paddingLeft: '1.1rem' }}>
            {insight.recommendations.map((r, i) => (
              <li key={i}>{r}</li>
            ))}
          </ul>
          <p className="text-muted text-sm">
            These are suggestions, not performed actions. Nothing has been changed.
          </p>
        </section>
      ) : null}

      {insight.draft ? (
        <section>
          <h3 className="text-sm">
            Draft <Badge tone="neutral">model output</Badge>
          </h3>
          {insight.draft.subject ? (
            <p className="text-sm">
              <strong>Subject:</strong> {insight.draft.subject}
            </p>
          ) : null}
          <textarea className="input" rows={8} readOnly value={insight.draft.body} />
          <p className="text-muted text-sm">
            Copy and edit this draft. Nothing is sent automatically.
          </p>
        </section>
      ) : null}
    </div>
  )
}

/** Shows the facts the model would be given, before any request is made. */
function ContextPreview({ context }: { context: import('../api/assist').AssistContext }) {
  return (
    <details className="text-sm">
      <summary>What the assistant can see</summary>
      <ul style={{ margin: 'var(--space-2) 0 0', paddingLeft: '1.1rem' }}>
        {context.facts.map((f, i) => (
          <li key={i}>
            <strong>{f.label}:</strong> {f.value}
          </li>
        ))}
      </ul>
      {context.disclosures.map((d, i) => (
        <p key={i} className="text-muted text-sm">
          {d.detail}
        </p>
      ))}
    </details>
  )
}
