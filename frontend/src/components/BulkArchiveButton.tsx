import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { errorMessage } from '../api/client'
import { useT } from '../i18n'
import type { BulkArchiveResult } from '../types/api'
import { Alert, ConfirmDialog } from './ui'

/**
 * The bulk archive control rendered in a list's selection bar.
 *
 * It confirms before acting and reports the outcome afterwards. Both matter more
 * here than for a single-record action:
 *
 * - The confirmation names the count, so the user can see that the number of
 *   records about to be archived is the number they selected.
 * - The report distinguishes "archived" from "not archived" per record. The
 *   server refuses some records — already archived, or not visible to the
 *   caller — and reports them rather than failing the whole request. A control
 *   that showed only a success message would leave the user believing a partial
 *   archive was complete.
 *
 * The selection is cleared only when every record was archived. Clearing it after
 * a partial result would hide the records that were not archived, and those are
 * the ones the user needs to act on.
 */
export function BulkArchiveButton({
  ids,
  entityLabel,
  archive,
  onDone,
}: {
  ids: string[]
  /** Plural noun for the confirmation and report, e.g. "accounts". */
  entityLabel: string
  archive: (ids: string[]) => Promise<BulkArchiveResult>
  /** Clears the selection. Called only when every record was archived. */
  onDone: () => void
}) {
  const t = useT()
  const queryClient = useQueryClient()
  const [confirming, setConfirming] = useState(false)
  const [result, setResult] = useState<BulkArchiveResult | null>(null)

  const mutation = useMutation({
    mutationFn: () => archive(ids),
    onSuccess: (data) => {
      setConfirming(false)
      setResult(data)
      // Invalidate regardless of the outcome: a partial archive still changed
      // records, so the list is stale either way.
      void queryClient.invalidateQueries({ queryKey: [entityLabel] })
      if (data.failed.length === 0) onDone()
    },
  })

  const count = ids.length
  const noun = count === 1 ? entityLabel.replace(/s$/, '') : entityLabel

  return (
    <>
      <button
        type="button"
        className="btn btn--danger btn--sm"
        onClick={() => {
          setResult(null)
          setConfirming(true)
        }}
      >
        {t('bulkArchive.button', { count, noun })}
      </button>

      {result ? (
        <div style={{ width: '100%', marginTop: 'var(--space-2)' }}>
          <Alert tone={result.failed.length === 0 ? 'success' : 'warning'}>
            {result.failed.length === 0
              ? t('bulkArchive.archived', {
                  count: result.archived.length,
                  noun: result.archived.length === 1 ? noun : entityLabel,
                })
              : t('bulkArchive.partial', {
                  archived: result.archived.length,
                  count,
                  failed: result.failed.length,
                  reasons: result.failed
                    .map((f) => f.reason)
                    .filter((reason, index, all) => all.indexOf(reason) === index)
                    .join('; '),
                })}
          </Alert>
        </div>
      ) : null}

      {mutation.isError ? (
        <div style={{ width: '100%', marginTop: 'var(--space-2)' }}>
          <Alert tone="error">{errorMessage(mutation.error)}</Alert>
        </div>
      ) : null}

      <ConfirmDialog
        open={confirming}
        title={t('bulkArchive.confirmTitle', { count, noun })}
        text={t('bulkArchive.confirmText')}
        confirmLabel={t('bulkArchive.confirmLabel', { count })}
        danger
        busy={mutation.isPending}
        onConfirm={() => mutation.mutate()}
        onCancel={() => setConfirming(false)}
      />
    </>
  )
}
