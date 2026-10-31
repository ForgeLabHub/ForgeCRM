import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { accountsApi, dealsApi, leadsApi, tasksApi, ticketsApi } from '../api/crm'
import { systemApi } from '../api/admin'
import { errorMessage } from '../api/client'
import { useAuth } from '../hooks/useAuth'
import { Alert, Badge, Card, ErrorState, PageHeader, StatusBadge, Timestamp } from '../components/ui'
import { useI18n } from '../i18n'

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
  const { t } = useI18n()

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
        title={t('overview.greeting', { name: session?.user.display_name ?? '' })}
        meta={t('overview.meta', {
          org: session?.organization.name ?? '',
          count: session?.permissions.length ?? 0,
        })}
      />

      {info.isError ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="error">
              {t('overview.loadError', { detail: errorMessage(info.error) })}
          </Alert>
        </div>
      ) : null}

      <div className="grid grid--stats" style={{ marginBottom: 'var(--space-5)' }}>
        {can('accounts.read') ? (
          <CountStat
            label={t('overview.statAccounts')}
            queryKey="accounts"
            link="/accounts"
            fetchCount={async () => (await accountsApi.list({ limit: 1 })).total}
          />
        ) : null}
        {can('deals.read') ? (
          <CountStat
            label={t('overview.statOpenDeals')}
            queryKey="deals-open"
            link="/deals"
            hint={t('overview.statOpenDealsHint')}
            fetchCount={async () => (await dealsApi.list({ limit: 1, status: 'open' })).total}
          />
        ) : null}
        {can('leads.read') ? (
          <CountStat
            label={t('overview.statLeads')}
            queryKey="leads"
            link="/leads"
            fetchCount={async () => (await leadsApi.list({ limit: 1 })).total}
          />
        ) : null}
        {can('tasks.read') ? (
          <CountStat
            label={t('overview.statOpenTasks')}
            queryKey="tasks-open"
            link="/tasks"
            fetchCount={async () => (await tasksApi.list({ limit: 1, status: 'open' })).total}
          />
        ) : null}
        {can('tickets.read') ? (
          <CountStat
            label={t('overview.statOpenTickets')}
            queryKey="tickets-open"
            link="/tickets"
            fetchCount={async () => (await ticketsApi.list({ limit: 1, status: 'open' })).total}
          />
        ) : null}
      </div>

      <div className="grid grid--split">
        {can('tasks.read') ? (
          <Card
            title={t('overview.yourOpenTasks')}
            actions={
              <Link to="/tasks" className="btn btn--ghost btn--sm">
                {t('overview.viewAll')}
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
                        {t('overview.due')} <Timestamp value={task.due_at} withTime={false} />
                      </div>
                    </div>
                    <Badge tone={task.priority === 'urgent' ? 'danger' : 'neutral'}>
                      {task.priority}
                    </Badge>
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-muted text-sm">{t('overview.noOpenTasks')}</p>
            )}
          </Card>
        ) : null}

        {can('tickets.read') ? (
          <Card
            title={t('overview.openSupportTickets')}
            actions={
              <Link to="/tickets" className="btn btn--ghost btn--sm">
                {t('overview.viewAll')}
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
                        {t('overview.opened')} <Timestamp value={ticket.created_at} />
                      </div>
                    </div>
                    <StatusBadge status={ticket.priority} />
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-muted text-sm">{t('overview.noOpenTickets')}</p>
            )}
          </Card>
        ) : null}
      </div>

      <div style={{ marginTop: 'var(--space-5)' }}>
        <Card title={t('overview.platform')}>
          {info.data ? (
            <dl className="dl">
              <dt>{t('overview.version')}</dt>
              <dd>{info.data.version}</dd>
              <dt>{t('overview.environment')}</dt>
              <dd>
                <Badge tone={info.data.environment === 'production' ? 'accent' : 'neutral'}>
                  {info.data.environment}
                </Badge>
              </dd>
              <dt>{t('overview.started')}</dt>
              <dd>
                <Timestamp value={info.data.started_at} />
              </dd>
              {info.data.commit ? (
                <>
                  <dt>{t('overview.build')}</dt>
                  <dd className="mono">{info.data.commit.slice(0, 12)}</dd>
                </>
              ) : null}
            </dl>
          ) : (
            <p className="text-muted text-sm">{t('overview.loading')}</p>
          )}
        </Card>
      </div>
    </>
  )
}
