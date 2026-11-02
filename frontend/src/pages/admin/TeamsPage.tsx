import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { teamsApi } from '../../api/admin'
import type { TeamRow } from '../../api/admin'
import { usersApi } from '../../api/auth'
import { errorMessage, fieldErrors } from '../../api/client'
import { useAuth } from '../../hooks/useAuth'
import { useT } from '../../i18n'
import { Alert, Card, ErrorState, Field, PageHeader, TableSkeleton, Timestamp } from '../../components/ui'

export function TeamsPage() {
  const { can } = useAuth()
  const t = useT()
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [addingTo, setAddingTo] = useState<TeamRow | null>(null)

  const teams = useQuery({ queryKey: ['teams'], queryFn: () => teamsApi.list() })

  if (teams.isError) {
    return (
      <Card title={t('teams.title')}>
        <ErrorState message={errorMessage(teams.error)} onRetry={() => void teams.refetch()} />
      </Card>
    )
  }

  return (
    <>
      <PageHeader title={t('teams.title')} meta={t('teams.meta')} />

      <Card
        title={t('teams.allTeams')}
        flush
        actions={
          can('teams.manage') ? (
            <button type="button" className="btn btn--primary btn--sm" onClick={() => setCreating(true)}>
              {t('teams.newTeam')}
            </button>
          ) : null
        }
      >
        {teams.isLoading ? (
          <TableSkeleton rows={4} columns={4} />
        ) : teams.data?.items.length ? (
          <div className="table-wrap">
            <table className="table--stack">
              <thead>
                <tr>
                  <th>{t('teams.columnName')}</th>
                  <th>{t('teams.columnDescription')}</th>
                  <th className="numeric">{t('teams.columnMembers')}</th>
                  <th>{t('teams.columnCreated')}</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {teams.data.items.map((team) => (
                  <tr key={team.id}>
                    <td className="table__primary" data-label={t('teams.columnName')}>
                      {team.name}
                    </td>
                    <td data-label={t('teams.columnDescription')}>
                      {team.description ?? <span className="text-subtle">—</span>}
                    </td>
                    <td className="numeric" data-label={t('teams.columnMembers')}>
                      {team.member_count}
                    </td>
                    <td data-label={t('teams.columnCreated')}>
                      <Timestamp value={team.created_at} />
                    </td>
                    <td>
                      {can('teams.manage') ? (
                        <button
                          type="button"
                          className="btn btn--secondary btn--sm"
                          onClick={() => setAddingTo(team)}
                        >
                          {t('teams.addMember')}
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
            <p className="text-muted">{t('teams.empty')}</p>
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
  const t = useT()
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
        background: 'var(--color-overlay)',
        zIndex: 60,
        padding: 'var(--space-4)',
      }}
      onClick={onClose}
    >
      <div className="card" style={{ maxWidth: 460, width: '100%' }} onClick={(e) => e.stopPropagation()}>
        <div className="card__header">
          <h2 id="new-team-title">{t('teams.newTeamTitle')}</h2>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="card__body stack">
            {formError ? <Alert tone="error">{formError}</Alert> : null}
            <Field label={t('teams.columnName')} htmlFor="team-name" required error={errors.name}>
              <input
                id="team-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                required
              />
            </Field>
            <Field label={t('teams.description')} htmlFor="team-description">
              <input
                id="team-description"
                value={description}
                onChange={(event) => setDescription(event.target.value)}
              />
            </Field>
          </div>
          <div className="form-actions">
            <button type="button" className="btn btn--secondary" onClick={onClose}>
              {t('common.cancel')}
            </button>
            <button type="submit" className="btn btn--primary" disabled={mutation.isPending}>
              {mutation.isPending ? t('teams.creating') : t('common.create')}
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
  const t = useT()
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
        background: 'var(--color-overlay)',
        zIndex: 60,
        padding: 'var(--space-4)',
      }}
      onClick={onClose}
    >
      <div className="card" style={{ maxWidth: 460, width: '100%' }} onClick={(e) => e.stopPropagation()}>
        <div className="card__header">
          <h2 id="add-member-title">{t('teams.addMemberTitle', { team: team.name })}</h2>
        </div>
        <div className="card__body stack">
          {error ? <Alert tone="error">{error}</Alert> : null}
          <Field label={t('teams.user')} htmlFor="team-member" required>
            <select id="team-member" value={userId} onChange={(event) => setUserId(event.target.value)}>
              <option value="">{t('teams.selectUser')}</option>
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
            {mutation.isPending ? t('teams.adding') : t('teams.addMember')}
          </button>
        </div>
      </div>
    </div>
  )
}
