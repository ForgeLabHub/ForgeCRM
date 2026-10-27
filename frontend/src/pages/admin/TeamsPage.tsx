import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { teamsApi } from '../../api/admin'
import type { TeamRow } from '../../api/admin'
import { usersApi } from '../../api/auth'
import { errorMessage, fieldErrors } from '../../api/client'
import { useAuth } from '../../hooks/useAuth'
import { Alert, Card, ErrorState, Field, PageHeader, TableSkeleton, Timestamp } from '../../components/ui'

export function TeamsPage() {
  const { can } = useAuth()
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [addingTo, setAddingTo] = useState<TeamRow | null>(null)

  const teams = useQuery({ queryKey: ['teams'], queryFn: () => teamsApi.list() })

  if (teams.isError) {
    return (
      <Card title="Teams">
        <ErrorState message={errorMessage(teams.error)} onRetry={() => void teams.refetch()} />
      </Card>
    )
  }

  return (
    <>
      <PageHeader
        title="Teams"
        meta="Groups used to share record ownership and to scope access."
      />

      <Card
        title="All teams"
        flush
        actions={
          can('teams.manage') ? (
            <button type="button" className="btn btn--primary btn--sm" onClick={() => setCreating(true)}>
              New team
            </button>
          ) : null
        }
      >
        {teams.isLoading ? (
          <TableSkeleton rows={4} columns={4} />
        ) : teams.data?.items.length ? (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Description</th>
                  <th className="numeric">Members</th>
                  <th>Created</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {teams.data.items.map((team) => (
                  <tr key={team.id}>
                    <td className="table__primary">{team.name}</td>
                    <td>{team.description ?? <span className="text-subtle">—</span>}</td>
                    <td className="numeric">{team.member_count}</td>
                    <td>
                      <Timestamp value={team.created_at} />
                    </td>
                    <td>
                      {can('teams.manage') ? (
                        <button
                          type="button"
                          className="btn btn--secondary btn--sm"
                          onClick={() => setAddingTo(team)}
                        >
                          Add member
                        </button>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="card__body">
            <p className="text-muted">No teams yet.</p>
          </div>
        )}
      </Card>

      <NewTeamDialog
        open={creating}
        onClose={() => setCreating(false)}
        onCreated={() => {
          setCreating(false)
          void queryClient.invalidateQueries({ queryKey: ['teams'] })
        }}
      />

      {addingTo ? (
        <AddMemberDialog
          team={addingTo}
          onClose={() => setAddingTo(null)}
          onAdded={() => {
            setAddingTo(null)
            void queryClient.invalidateQueries({ queryKey: ['teams'] })
          }}
        />
      ) : null}
    </>
  )
}

function NewTeamDialog({
  open,
  onClose,
  onCreated,
}: {
  open: boolean
  onClose: () => void
  onCreated: () => void
}) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)

  const mutation = useMutation({
    mutationFn: () => teamsApi.create({ name, description: description || undefined }),
    onSuccess: onCreated,
    onError: (error) => {
      setErrors(fieldErrors(error))
      setFormError(errorMessage(error))
    },
  })

  function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setFormError(null)
    setErrors({})
    mutation.mutate()
  }

  if (!open) return null

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="new-team-title"
      style={{
        position: 'fixed',
        inset: 0,
        display: 'grid',
        placeItems: 'center',
        background: 'rgba(16, 24, 40, 0.45)',
        zIndex: 60,
        padding: 'var(--space-4)',
      }}
      onClick={onClose}
    >
      <div className="card" style={{ maxWidth: 460, width: '100%' }} onClick={(e) => e.stopPropagation()}>
        <div className="card__header">
          <h2 id="new-team-title">New team</h2>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="card__body stack">
            {formError ? <Alert tone="error">{formError}</Alert> : null}
            <Field label="Name" htmlFor="team-name" required error={errors.name}>
              <input
                id="team-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                required
              />
            </Field>
            <Field label="Description" htmlFor="team-description">
              <input
                id="team-description"
                value={description}
                onChange={(event) => setDescription(event.target.value)}
              />
            </Field>
          </div>
          <div className="form-actions">
            <button type="button" className="btn btn--secondary" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="btn btn--primary" disabled={mutation.isPending}>
              {mutation.isPending ? 'Creating…' : 'Create'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

function AddMemberDialog({
  team,
  onClose,
  onAdded,
}: {
  team: TeamRow
  onClose: () => void
  onAdded: () => void
}) {
  const [userId, setUserId] = useState('')
  const [error, setError] = useState<string | null>(null)

  const users = useQuery({
    queryKey: ['users', 'for-select'],
    queryFn: () => usersApi.list({ limit: 200 }),
  })

  const mutation = useMutation({
    mutationFn: () => teamsApi.addMember(team.id, userId),
    onSuccess: onAdded,
    onError: (err) => setError(errorMessage(err)),
  })

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="add-member-title"
      style={{
        position: 'fixed',
        inset: 0,
        display: 'grid',
        placeItems: 'center',
        background: 'rgba(16, 24, 40, 0.45)',
        zIndex: 60,
        padding: 'var(--space-4)',
      }}
      onClick={onClose}
    >
      <div className="card" style={{ maxWidth: 460, width: '100%' }} onClick={(e) => e.stopPropagation()}>
        <div className="card__header">
          <h2 id="add-member-title">Add a member to {team.name}</h2>
        </div>
        <div className="card__body stack">
          {error ? <Alert tone="error">{error}</Alert> : null}
          <Field label="User" htmlFor="team-member" required>
            <select id="team-member" value={userId} onChange={(event) => setUserId(event.target.value)}>
              <option value="">Select a user</option>
              {(users.data?.items ?? []).map((user) => (
                <option key={user.id} value={user.id}>
                  {user.display_name} ({user.email})
                </option>
              ))}
            </select>
          </Field>
        </div>
        <div className="form-actions" style={{ margin: 0, padding: 'var(--space-4)' }}>
          <button type="button" className="btn btn--secondary" onClick={onClose}>
            Cancel
          </button>
          <button
            type="button"
            className="btn btn--primary"
            disabled={!userId || mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            {mutation.isPending ? 'Adding…' : 'Add member'}
          </button>
        </div>
      </div>
    </div>
  )
}
