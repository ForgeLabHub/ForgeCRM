import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { accountsApi, dealsApi, leadsApi, tasksApi, ticketsApi } from '../api/crm'
import { systemApi } from '../api/admin'
import { errorMessage } from '../api/client'
import { useAuth } from '../hooks/useAuth'
import { Alert, Badge, Card, ErrorState, PageHeader, StatusBadge, Timestamp } from '../components/ui'

/** A single count tile backed by its own query, so one failure does not blank the page. */
function CountStat({
  label,
  queryKey,
  fetchCount,
  hint,
  link,
}: {
  label: string
  queryKey: string
  fetchCount: () => Promise<number>
  hint?: string
  link?: string
}) {
  const query = useQuery({ queryKey: ['count', queryKey], queryFn: fetchCount })
  const value = query.isLoading ? '—' : query.isError ? '!' : query.data

  return (
    <div className="stat">
      <div className="stat__label">{label}</div>
      <div className="stat__value">
        {link ? (
          <Link to={link} style={{ color: 'inherit' }}>
            {value}
          </Link>
        ) : (
          value
        )}
      </div>
      {hint ? <div className="stat__hint">{hint}</div> : null}
    </div>
  )
}

export function OverviewPage() {
  const { session, can } = useAuth()

  const info = useQuery({ queryKey: ['system', 'info'], queryFn: systemApi.info })
  const recentTasks = useQuery({
    queryKey: ['tasks', 'overview'],
    queryFn: () => tasksApi.list({ limit: 5, status: 'open' }),
    enabled: can('tasks.read'),
  })
  const openTickets = useQuery({
    queryKey: ['tickets', 'overview'],
    queryFn: () => ticketsApi.list({ limit: 5, status: 'open' }),
    enabled: can('tickets.read'),
  })

  return (
    <>
      <PageHeader
        title={`Welcome back, ${session?.user.display_name ?? ''}`}
        meta={
          <>
            Signed in to <strong>{session?.organization.name}</strong> with{' '}
            {session?.permissions.length ?? 0} permissions
          </>
        }
      />

      {info.isError ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="error">
            Could not load system information: {errorMessage(info.error)}
          </Alert>
        </div>
      ) : null}

      <div className="grid grid--stats" style={{ marginBottom: 'var(--space-5)' }}>
        {can('accounts.read') ? (
          <CountStat
            label="Accounts"
            queryKey="accounts"
            link="/accounts"
            fetchCount={async () => (await accountsApi.list({ limit: 1 })).total}
          />
        ) : null}
        {can('deals.read') ? (
          <CountStat
            label="Open deals"
            queryKey="deals-open"
            link="/deals"
            hint="Deals not yet won or lost"
            fetchCount={async () => (await dealsApi.list({ limit: 1, status: 'open' })).total}
          />
        ) : null}
        {can('leads.read') ? (
          <CountStat
            label="Leads"
            queryKey="leads"
            link="/leads"
            fetchCount={async () => (await leadsApi.list({ limit: 1 })).total}
          />
        ) : null}
        {can('tasks.read') ? (
          <CountStat
            label="Open tasks"
            queryKey="tasks-open"
            link="/tasks"
            fetchCount={async () => (await tasksApi.list({ limit: 1, status: 'open' })).total}
          />
        ) : null}
        {can('tickets.read') ? (
          <CountStat
            label="Open tickets"
            queryKey="tickets-open"
            link="/tickets"
            fetchCount={async () => (await ticketsApi.list({ limit: 1, status: 'open' })).total}
          />
        ) : null}
      </div>

      <div className="grid grid--split">
        {can('tasks.read') ? (
          <Card
            title="Your open tasks"
            actions={
              <Link to="/tasks" className="btn btn--ghost btn--sm">
                View all
              </Link>
            }
          >
            {recentTasks.isError ? (
              <ErrorState message={errorMessage(recentTasks.error)} />
            ) : recentTasks.data?.items.length ? (
              <div className="stack stack--sm">
                {recentTasks.data.items.map((task) => (
                  <div key={task.id} className="row row--between">
                    <div style={{ minWidth: 0 }}>
                      <div className="truncate table__primary">{task.title}</div>
                      <div className="table__secondary">
                        Due <Timestamp value={task.due_at} withTime={false} />
                      </div>
                    </div>
                    <Badge tone={task.priority === 'urgent' ? 'danger' : 'neutral'}>
                      {task.priority}
                    </Badge>
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-muted text-sm">No open tasks.</p>
            )}
          </Card>
        ) : null}

        {can('tickets.read') ? (
          <Card
            title="Open support tickets"
            actions={
              <Link to="/tickets" className="btn btn--ghost btn--sm">
                View all
              </Link>
            }
          >
            {openTickets.isError ? (
              <ErrorState message={errorMessage(openTickets.error)} />
            ) : openTickets.data?.items.length ? (
              <div className="stack stack--sm">
                {openTickets.data.items.map((ticket) => (
                  <div key={ticket.id} className="row row--between">
                    <div style={{ minWidth: 0 }}>
                      <div className="truncate table__primary">{ticket.subject}</div>
                      <div className="table__secondary">
                        Opened <Timestamp value={ticket.created_at} />
                      </div>
                    </div>
                    <StatusBadge status={ticket.priority} />
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-muted text-sm">No open tickets.</p>
            )}
          </Card>
        ) : null}
      </div>

      <div style={{ marginTop: 'var(--space-5)' }}>
        <Card title="Platform">
          {info.data ? (
            <dl className="dl">
              <dt>Version</dt>
              <dd>{info.data.version}</dd>
              <dt>Environment</dt>
              <dd>
                <Badge tone={info.data.environment === 'production' ? 'accent' : 'neutral'}>
                  {info.data.environment}
                </Badge>
              </dd>
              <dt>Started</dt>
              <dd>
                <Timestamp value={info.data.started_at} />
              </dd>
              {info.data.commit ? (
                <>
                  <dt>Build</dt>
                  <dd className="mono">{info.data.commit.slice(0, 12)}</dd>
                </>
              ) : null}
            </dl>
          ) : (
            <p className="text-muted text-sm">Loading…</p>
          )}
        </Card>
      </div>
    </>
  )
}
