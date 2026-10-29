/**
 * Attachments for one CRM record.
 *
 * The panel is only rendered when the server reports `storage: true` on
 * `/system/capabilities`, so a deployment with no object store shows nothing rather
 * than an upload that would answer 503. Every action goes through the server: the
 * upload is multipart, the download link is server-built, and the list is filtered
 * server-side by the record the panel is bound to.
 */
import { useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { errorMessage } from '../api/client'
import { documentsApi, type DocumentEntityType, type DocumentRecord } from '../api/documents'
import { systemApi } from '../api/admin'
import { Alert, Card, Timestamp, formatBytes } from './ui'
import { useAuth } from '../hooks/useAuth'

/** The capability check, shared so the panel and its tests agree on the condition. */
export function useStorageCapability() {
  const query = useQuery({
    queryKey: ['system', 'capabilities'],
    queryFn: systemApi.capabilities,
    staleTime: 5 * 60 * 1000,
  })
  return {
    storageEnabled: query.data?.storage === true,
    maxUploadBytes: query.data?.max_upload_bytes ?? 0,
    isLoading: query.isLoading,
  }
}

interface AttachmentPanelProps {
  entityType: DocumentEntityType
  entityId: string
}

export function AttachmentPanel({ entityType, entityId }: AttachmentPanelProps) {
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const { storageEnabled, maxUploadBytes, isLoading } = useStorageCapability()
  const [error, setError] = useState<string | null>(null)
  const fileInput = useRef<HTMLInputElement>(null)

  const listQuery = useQuery({
    queryKey: ['documents', entityType, entityId],
    queryFn: () => documentsApi.list(),
    enabled: storageEnabled,
  })

  const uploadMutation = useMutation({
    mutationFn: (file: File) => documentsApi.upload({ file, entityType, entityId }),
    onSuccess: () => {
      setError(null)
      if (fileInput.current) fileInput.current.value = ''
      void queryClient.invalidateQueries({ queryKey: ['documents', entityType, entityId] })
    },
    onError: (err) => setError(errorMessage(err)),
  })

  const archiveMutation = useMutation({
    mutationFn: (id: string) => documentsApi.archive(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['documents', entityType, entityId] }),
    onError: (err) => setError(errorMessage(err)),
  })

  if (isLoading || !storageEnabled) return null

  // The server filters the list by permission; the panel narrows it to this record's
  // attachments by matching the id the panel is bound to.
  const idField = `${entityType}_id` as keyof DocumentRecord
  const attachments = (listQuery.data?.items ?? []).filter((doc) => doc[idField] === entityId)

  function onPickFile(file: File | undefined) {
    if (!file) return
    setError(null)
    if (maxUploadBytes > 0 && file.size > maxUploadBytes) {
      setError(`That file is ${formatBytes(file.size)}; the limit is ${formatBytes(maxUploadBytes)}.`)
      return
    }
    uploadMutation.mutate(file)
  }

  return (
    <Card title={`Attachments${attachments.length ? ` (${attachments.length})` : ''}`}>
      {error ? <Alert tone="error">{error}</Alert> : null}
      {listQuery.isError ? <Alert tone="error">{errorMessage(listQuery.error)}</Alert> : null}

      {attachments.length ? (
        <ul className="stack stack--sm" style={{ listStyle: 'none', padding: 0 }}>
          {attachments.map((doc) => (
            <li key={doc.id} className="row row--between">
              <a className="table__primary truncate" href={doc.download_url}>
                {doc.title || doc.filename}
              </a>
              <span className="row" style={{ gap: 'var(--space-2)' }}>
                <span className="text-subtle text-sm">{formatBytes(doc.size_bytes)}</span>
                <Timestamp value={doc.created_at} />
                {can('documents.archive') ? (
                  <button
                    type="button"
                    className="btn btn--ghost btn--sm"
                    onClick={() => archiveMutation.mutate(doc.id)}
                  >
                    Remove
                  </button>
                ) : null}
              </span>
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-muted text-sm">No files attached yet.</p>
      )}

      {can('documents.create') ? (
        <div className="row" style={{ gap: 'var(--space-2)', marginTop: 'var(--space-3)' }}>
          <input
            ref={fileInput}
            type="file"
            aria-label="Attach a file"
            onChange={(e) => onPickFile(e.target.files?.[0])}
            disabled={uploadMutation.isPending}
          />
          {uploadMutation.isPending ? <span className="text-subtle text-sm">Uploading…</span> : null}
        </div>
      ) : null}

      {maxUploadBytes > 0 ? (
        <span className="field__hint">Files up to {formatBytes(maxUploadBytes)}.</span>
      ) : null}
    </Card>
  )
}
