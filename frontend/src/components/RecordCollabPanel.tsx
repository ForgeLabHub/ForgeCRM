/**
 * Tags and comments for one CRM record.
 *
 * Both features were complete on the server — routes, permissions, audit — but
 * had no interface, so a capability the API advertised could not be reached from
 * the product. This panel is the missing consumer.
 *
 * Every list and write goes through the server: reads are scoped to the caller's
 * organization and narrowed to the record the panel is bound to, and each
 * control is gated by the permission the route enforces. The panel hides a
 * control the user cannot use, but hiding is not the access control — the server
 * refuses the request regardless.
 */
import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { commentsApi, tagsApi } from '../api/crm'
import { errorMessage } from '../api/client'
import type { Comment, Tag } from '../types/api'
import { useAuth } from '../hooks/useAuth'
import { Alert, Card, Skeleton, Timestamp } from './ui'

export type CollabEntityType = 'account' | 'contact' | 'lead' | 'deal' | 'ticket' | 'task'

interface RecordCollabPanelProps {
  entityType: CollabEntityType
  entityId: string
}

export function RecordCollabPanel({ entityType, entityId }: RecordCollabPanelProps) {
  const { can } = useAuth()

  // The read routes for both tags and comments require contacts.read, so without
  // it there is nothing this panel can show and it renders nothing at all.
  const canRead = can('contacts.read')
  if (!canRead) return null

  return (
    <div className="grid grid--split" style={{ marginTop: 'var(--space-5)' }}>
      <TagsCard entityType={entityType} entityId={entityId} />
      <CommentsCard entityType={entityType} entityId={entityId} />
    </div>
  )
}

function TagsCard({ entityType, entityId }: RecordCollabPanelProps) {
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const canManage = can('tags.manage')

  const attached = useQuery({
    queryKey: ['tags', 'attached', entityType, entityId],
    queryFn: () => tagsApi.attached(entityType, entityId),
  })
  const allTags = useQuery({
    queryKey: ['tags', 'list'],
    queryFn: () => tagsApi.list(),
    enabled: canManage,
  })

  const [selected, setSelected] = useState('')
  const [newTag, setNewTag] = useState('')
  const [error, setError] = useState<string | null>(null)

  const attachedTagIds = new Set((attached.data?.items ?? []).map((t) => t.id))
  const available = (allTags.data?.items ?? []).filter((t) => !attachedTagIds.has(t.id))

  const refreshTags = () => {
    void queryClient.invalidateQueries({ queryKey: ['tags', 'attached', entityType, entityId] })
    void queryClient.invalidateQueries({ queryKey: ['tags', 'list'] })
  }

  const attach = useMutation({
    mutationFn: (tagId: string) =>
      tagsApi.attach(tagId, { entity_type: entityType, entity_id: entityId }),
    onSuccess: () => {
      setSelected('')
      setError(null)
      refreshTags()
    },
    onError: (err) => setError(errorMessage(err)),
  })

  const createAndAttach = useMutation({
    mutationFn: async (name: string) => {
      const tag = await tagsApi.create({ name })
      await tagsApi.attach(tag.id, { entity_type: entityType, entity_id: entityId })
      return tag
    },
    onSuccess: () => {
      setNewTag('')
      setError(null)
      refreshTags()
    },
    onError: (err) => setError(errorMessage(err)),
  })

  function onAttach(event: FormEvent) {
    event.preventDefault()
    setError(null)
    if (selected) attach.mutate(selected)
  }

  function onCreate(event: FormEvent) {
    event.preventDefault()
    setError(null)
    const name = newTag.trim()
    if (!name) {
      setError('A tag needs a name.')
      return
    }
    createAndAttach.mutate(name)
  }

  const busy = attach.isPending || createAndAttach.isPending

  return (
    <Card title={`Tags${attachedTagIds.size ? ` (${attachedTagIds.size})` : ''}`}>
      {error ? <Alert tone="error">{error}</Alert> : null}
      {attached.isError ? <Alert tone="error">{errorMessage(attached.error)}</Alert> : null}

      {attached.isLoading ? (
        <Skeleton height={24} />
      ) : attachedTagIds.size ? (
        <div className="row row--wrap" style={{ gap: 'var(--space-2)' }}>
          {(attached.data?.items ?? []).map((tag: Tag) => (
            <span
              key={tag.id}
              className="badge"
              style={{
                borderColor: tag.color,
                color: tag.color,
                background: 'transparent',
              }}
            >
              {tag.name}
            </span>
          ))}
        </div>
      ) : (
        <p className="text-muted text-sm">No tags on this record.</p>
      )}

      {canManage ? (
        <div className="stack stack--sm" style={{ marginTop: 'var(--space-3)' }}>
          <form className="row" style={{ gap: 'var(--space-2)' }} onSubmit={onAttach}>
            <select
              aria-label="Attach an existing tag"
              value={selected}
              onChange={(event) => setSelected(event.target.value)}
              disabled={busy || available.length === 0}
            >
              <option value="">
                {available.length === 0 ? 'No tags left to attach' : 'Attach a tag…'}
              </option>
              {available.map((tag) => (
                <option key={tag.id} value={tag.id}>
                  {tag.name}
                </option>
              ))}
            </select>
            <button
              type="submit"
              className="btn btn--secondary btn--sm"
              disabled={busy || !selected}
            >
              Attach
            </button>
          </form>

          <form className="row" style={{ gap: 'var(--space-2)' }} onSubmit={onCreate}>
            <input
              aria-label="New tag name"
              placeholder="New tag name"
              value={newTag}
              onChange={(event) => setNewTag(event.target.value)}
              disabled={busy}
            />
            <button type="submit" className="btn btn--ghost btn--sm" disabled={busy}>
              Create &amp; attach
            </button>
          </form>
        </div>
      ) : null}
    </Card>
  )
}

function CommentsCard({ entityType, entityId }: RecordCollabPanelProps) {
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const canComment = can('comments.create')

  const list = useQuery({
    queryKey: ['comments', entityType, entityId],
    queryFn: () => commentsApi.list(entityType, entityId),
  })

  const [body, setBody] = useState('')
  const [error, setError] = useState<string | null>(null)

  const create = useMutation({
    mutationFn: (text: string) =>
      commentsApi.create({ entity_type: entityType, entity_id: entityId, body: text }),
    onSuccess: () => {
      setBody('')
      setError(null)
      void queryClient.invalidateQueries({ queryKey: ['comments', entityType, entityId] })
    },
    onError: (err) => setError(errorMessage(err)),
  })

  function onSubmit(event: FormEvent) {
    event.preventDefault()
    setError(null)
    const text = body.trim()
    if (!text) {
      setError('A comment needs some text.')
      return
    }
    create.mutate(text)
  }

  const comments: Comment[] = list.data?.items ?? []

  return (
    <Card title={`Comments${comments.length ? ` (${comments.length})` : ''}`}>
      {error ? <Alert tone="error">{error}</Alert> : null}
      {list.isError ? <Alert tone="error">{errorMessage(list.error)}</Alert> : null}

      {list.isLoading ? (
        <Skeleton height={48} />
      ) : comments.length ? (
        <ul className="stack stack--sm" style={{ listStyle: 'none', padding: 0 }}>
          {comments.map((comment) => (
            <li key={comment.id} className="stack stack--sm">
              <div className="row row--between">
                <span className="table__primary text-sm">
                  {comment.author_name ?? 'Unattributed'}
                </span>
                <Timestamp value={comment.created_at} />
              </div>
              <p className="text-sm" style={{ whiteSpace: 'pre-wrap', margin: 0 }}>
                {comment.body}
              </p>
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-muted text-sm">No comments yet.</p>
      )}

      {canComment ? (
        <form className="stack stack--sm" style={{ marginTop: 'var(--space-3)' }} onSubmit={onSubmit}>
          <textarea
            aria-label="Add a comment"
            placeholder="Add a comment…"
            rows={3}
            value={body}
            onChange={(event) => setBody(event.target.value)}
            disabled={create.isPending}
          />
          <div className="row row--end">
            <button type="submit" className="btn btn--primary btn--sm" disabled={create.isPending}>
              {create.isPending ? 'Posting…' : 'Comment'}
            </button>
          </div>
        </form>
      ) : null}
    </Card>
  )
}
