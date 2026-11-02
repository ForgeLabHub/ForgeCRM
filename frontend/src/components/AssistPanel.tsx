import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { assistApi } from '../api/assist'
import type {
  AssistConfidence,
  AssistEntityType,
  AssistInsight,
  AssistKind,
} from '../api/assist'
import { ApiError, errorMessage } from '../api/client'
import { useAuth } from '../hooks/useAuth'
import { useT } from '../i18n'
import type { TranslationKey } from '../i18n'
import { Alert, Badge, Card, Skeleton, Timestamp } from './ui'

/**
 * Reports whether the server marked the failure as one that could succeed if
 * sent again. The panel offers a retry button only in that case: a retry offered
 * for a deterministic failure — a validation error, a permission refusal — is a
 * button that cannot work, which teaches the user to distrust the panel.
 */
function isRetryable(error: unknown): boolean {
  return error instanceof ApiError && error.retryable
}

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
const KIND_LABELS: { value: AssistKind; key: TranslationKey }[] = [
  { value: 'summary', key: 'assist.kindSummary' },
  { value: 'risks', key: 'assist.kindRisks' },
  { value: 'next_actions', key: 'assist.kindNextActions' },
  { value: 'wrap_up', key: 'assist.kindWrapUp' },
  { value: 'draft', key: 'assist.kindDraft' },
  { value: 'ask', key: 'assist.kindAsk' },
]

export function AssistPanel({
  entityType,
  entityId,
}: {
  entityType: AssistEntityType
  entityId: string
}) {
  const t = useT()
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

  // Streaming state. The stream is used for the same request as `run`; `run` stays
  // for the "ask again" path and as the fallback when the stream cannot be read.
  // The deltas are kept only while a request is in flight: once the final insight
  // arrives, the structured view replaces the raw text so the user never reads the
  // half-formed JSON the model emits before it is parsed.
  const [streaming, setStreaming] = useState(false)
  const [streamText, setStreamText] = useState('')
  const [streamError, setStreamError] = useState<unknown>(null)
  const [streamInsight, setStreamInsight] = useState<AssistInsight | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  // A stream that is left open when the panel unmounts keeps a provider call and a
  // database write alive for a record the user has navigated away from.
  useEffect(() => {
    return () => abortRef.current?.abort()
  }, [])

  const startStream = (refresh = false) => {
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    setStreaming(true)
    setStreamText('')
    setStreamError(null)
    setStreamInsight(null)
    let gotDone = false
    void assistApi
      .stream(
        entityType,
        entityId,
        {
          kind,
          question: kind === 'ask' ? question : undefined,
          recipient_hint: kind === 'draft' && recipientHint ? recipientHint : undefined,
          refresh,
        },
        (text) => setStreamText((prev) => prev + text),
        (insight) => {
          gotDone = true
          setStreamInsight(insight)
        },
        controller.signal,
      )
      .then(() => {
        // A stream that ends without a `done` event means an intermediary swallowed
        // the body (a proxy that buffered it, a deployment without the streaming
        // route). Falling back to the buffered request keeps the feature working
        // rather than showing the user a blank panel and calling it success.
        if (!gotDone && !controller.signal.aborted) {
          run.mutate(refresh)
        }
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setStreamError(err)
      })
      .finally(() => setStreaming(false))
  }

  const stopStream = () => {
    abortRef.current?.abort()
    abortRef.current = null
    setStreaming(false)
  }

  if (!can('ai.read')) {
    return null
  }

  if (status.isLoading) {
    return (
      <Card title={t('assist.title')}>
        <Skeleton height={80} />
      </Card>
    )
  }

  if (status.data && !status.data.enabled) {
    return (
      <Card title={t('assist.title')}>
        <Alert tone="info">{t('assist.noProvider')}</Alert>
      </Card>
    )
  }

  // A provider that is configured but unreachable is worth saying before the user
  // spends a request on it. The run path reports the same condition if it is hit
  // anyway (the probe and the call can disagree), but a warning here saves the
  // round trip and explains the outage rather than only reporting it.
  const providerDown = status.data?.enabled === true && status.data.healthy === false

  // The streamed insight, once it arrives, is the current answer; the buffered
  // `run` result is used only by the fallback path.
  const insight = streamInsight ?? run.data

  return (
    <Card
      title={t('assist.title')}
      actions={
        insight ? (
          <button
            type="button"
            className="btn btn--ghost btn--sm"
            onClick={() => startStream(true)}
            disabled={streaming}
          >
            {t('assist.askAgain')}
          </button>
        ) : null
      }
    >
      <div className="stack">
        {providerDown ? (
          <Alert tone="warning">{t('assist.providerDown')}</Alert>
        ) : null}

        <div className="row" style={{ gap: 'var(--space-2)', flexWrap: 'wrap' }}>
          {KIND_LABELS.map((k) => (
            <button
              key={k.value}
              type="button"
              className={`btn btn--sm ${kind === k.value ? 'btn--primary' : 'btn--secondary'}`}
              onClick={() => setKind(k.value)}
            >
              {t(k.key)}
            </button>
          ))}
        </div>

        {kind === 'ask' ? (
          <label className="field" htmlFor="assist-question">
            <span className="field__label">{t('assist.question')}</span>
            <input
              id="assist-question"
              className="input"
              value={question}
              maxLength={500}
              placeholder={t('assist.questionPlaceholder')}
              onChange={(e) => setQuestion(e.target.value)}
            />
            <span className="field__hint">{t('assist.questionHint')}</span>
          </label>
        ) : null}

        {kind === 'draft' ? (
          <label className="field" htmlFor="assist-recipient">
            <span className="field__label">{t('assist.recipient')}</span>
            <input
              id="assist-recipient"
              className="input"
              value={recipientHint}
              maxLength={200}
              placeholder={t('assist.recipientPlaceholder')}
              onChange={(e) => setRecipientHint(e.target.value)}
            />
          </label>
        ) : null}

        <div className="row">
          <button
            type="button"
            className="btn btn--primary btn--sm"
            onClick={() => startStream(false)}
            disabled={streaming || (kind === 'ask' && question.trim() === '')}
          >
            {streaming ? t('assist.asking') : t('assist.run')}
          </button>
          {streaming ? (
            // A stream can run for many seconds; without a way to stop it the user
            // is stuck watching a provider they may not want to wait for.
            <button type="button" className="btn btn--secondary btn--sm" onClick={stopStream}>
              {t('assist.stop')}
            </button>
          ) : null}
          {insight ? (
            <span className="text-muted text-sm">
              {insight.cached ? t('assist.fromCache') : ''}
              {insight.provider}
              {insight.model ? ` / ${insight.model}` : ''} ·{' '}
              <Timestamp value={insight.generated_at} />
            </span>
          ) : null}
        </div>

        {streamError ? (
          <Alert tone="error">
            <div className="stack">
              <span>{errorMessage(streamError)}</span>
              {isRetryable(streamError) ? (
                <div>
                  <button
                    type="button"
                    className="btn btn--secondary btn--sm"
                    onClick={() => startStream(false)}
                    disabled={streaming}
                  >
                    {streaming ? t('assist.trying') : t('assist.retry')}
                  </button>
                </div>
              ) : null}
            </div>
          </Alert>
        ) : null}

        {/* While the reply is forming, the raw text is shown as the model's own
            output, clearly labelled. It is replaced by the structured view once the
            final insight arrives, so a user never mistakes a half-written JSON
            fragment for a CRM value. */}
        {streaming ? (
          <section>
            <h3 className="text-sm">
              {t('assist.reply')} <Badge tone="neutral">{t('assist.inProgress')}</Badge>
            </h3>
            <p className="text-muted text-sm" style={{ whiteSpace: 'pre-wrap' }}>
              {streamText || t('assist.waiting')}
            </p>
          </section>
        ) : null}

        {run.isError ? (
          <Alert tone="error">
            <div className="stack">
              <span>{errorMessage(run.error)}</span>
              {isRetryable(run.error) ? (
                // A retryable failure is worth offering as a button, not just as
                // advice in the message: the whole reason the server marks it
                // retryable is that sending it again could work, so the panel
                // gives the user the one action that helps.
                <div>
                  <button
                    type="button"
                    className="btn btn--secondary btn--sm"
                    onClick={() => run.mutate(false)}
                    disabled={run.isPending}
                  >
                    {run.isPending ? t('assist.trying') : t('assist.retry')}
                  </button>
                </div>
              ) : null}
            </div>
          </Alert>
        ) : null}

        {streamInsight ? <InsightView insight={streamInsight} /> : null}
        {!streamInsight && insight ? <InsightView insight={insight} /> : null}

        {!insight && !streamInsight && context.data ? <ContextPreview context={context.data} /> : null}
      </div>
    </Card>
  )
}

/** Renders one insight, keeping CRM facts and model output visibly apart. */
function InsightView({ insight }: { insight: AssistInsight }) {
  const t = useT()
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

      <ConfidenceNote confidence={insight.confidence} />

      <section>
        <h3 className="text-sm">{t('assist.toldFromCrm')}</h3>
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
          {t('assist.analysis')} <Badge tone="neutral">{t('assist.modelOutput')}</Badge>
        </h3>
        <p style={{ whiteSpace: 'pre-wrap' }}>{insight.analysis}</p>
      </section>

      {insight.recommendations.length ? (
        <section>
          <h3 className="text-sm">
            {t('assist.nextActions')} <Badge tone="neutral">{t('assist.modelOutput')}</Badge>
          </h3>
          <ul style={{ margin: 0, paddingLeft: '1.1rem' }}>
            {insight.recommendations.map((r, i) => (
              <li key={i}>{r}</li>
            ))}
          </ul>
          <p className="text-muted text-sm">{t('assist.suggestionsNote')}</p>
        </section>
      ) : null}

      {insight.draft ? (
        <section>
          <h3 className="text-sm">
            {t('assist.draft')} <Badge tone="neutral">{t('assist.modelOutput')}</Badge>
          </h3>
          {insight.draft.subject ? (
            <p className="text-sm">
              <strong>{t('assist.subject')}</strong> {insight.draft.subject}
            </p>
          ) : null}
          <textarea className="input" rows={8} readOnly value={insight.draft.body} />
          <p className="text-muted text-sm">{t('assist.draftNote')}</p>
        </section>
      ) : null}
    </div>
  )
}

/**
 * Shows how well the answer is grounded in the record.
 *
 * The level is a badge and the basis is spelled out, so the user sees why the
 * server scored it that way rather than being handed a bare number. A low score
 * is a prompt to check the record, not a claim that the answer is wrong.
 */
function ConfidenceNote({ confidence }: { confidence: AssistConfidence }) {
  const t = useT()
  const tone =
    confidence.level === 'high'
      ? 'success'
      : confidence.level === 'medium'
        ? 'warning'
        : 'danger'
  const label =
    confidence.level === 'high'
      ? t('assist.wellGrounded')
      : confidence.level === 'medium'
        ? t('assist.partlyGrounded')
        : t('assist.weaklyGrounded')
  return (
    <section>
      <h3 className="text-sm">
        {t('assist.grounding')} <Badge tone={tone}>{label}</Badge>
      </h3>
      <ul className="text-muted text-sm" style={{ margin: 0, paddingLeft: '1.1rem' }}>
        {confidence.basis.map((b, i) => (
          <li key={i}>{b}</li>
        ))}
      </ul>
      <p className="text-muted text-sm">{t('assist.groundingNote')}</p>
    </section>
  )
}

/** Shows the facts the model would be given, before any request is made. */
function ContextPreview({ context }: { context: import('../api/assist').AssistContext }) {
  const t = useT()
  return (
    <details className="text-sm">
      <summary>{t('assist.canSee')}</summary>
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
