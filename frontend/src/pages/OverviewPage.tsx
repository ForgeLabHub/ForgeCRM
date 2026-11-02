import type { JSX } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { accountsApi, dealsApi, leadsApi, tasksApi, ticketsApi } from '../api/crm'
import { systemApi } from '../api/admin'
import { errorMessage } from '../api/client'
import { useAuth } from '../hooks/useAuth'
import {
  Alert,
  Badge,
  Card,
  ErrorState,
  Skeleton,
  StatusBadge,
  Timestamp,
} from '../components/ui'
import {
  IconBuilding,
  IconChart,
  IconCheckSquare,
  IconKanban,
  IconLifeBuoy,
  IconTarget,
  IconWorkflow,
  type IconProps,
} from '../components/icons'
import { useI18n, type TranslationKey } from '../i18n'

/** A count tile. Each owns its query so one failing endpoint does not blank the
 *  page, and the whole tile is the link so the number and the label are one
 *  target rather than a link buried inside a card. */
function Metric({
  label,
  queryKey,
  fetchCount,
  hint,
  link,
  icon: Icon,
}: {
  label: string
  queryKey: string
  fetchCount: () => Promise<number>
  hint?: string
  link: string
  icon: (props: IconProps) => JSX.Element
}) {
  const { t } = useI18n()
  const query = useQuery({
    queryKey: ['overview', 'metric', queryKey],
    queryFn: fetchCount,
    retry: false,
  })

  return (
    <Link to={link} className="metric">
      <div className="metric__top">
        <span className="metric__label">{label}</span>
        <span className="metric__icon">
          <Icon size={16} />
        </span>
      </div>
      {query.isLoading ? (
        <Skeleton width="56px" height={26} />
      ) : (
        <div className={query.isError ? 'metric__value metric__value--error' : 'metric__value'}>
          {query.isError ? '—' : query.data}
        </div>
      )}
      {query.isError ? (
        <div className="metric__hint">{t('overview.metricUnavailable')}</div>
      ) : hint ? (
        <div className="metric__hint">{hint}</div>
      ) : null}
    </Link>
  )
}

/** One quick action: a real link to the page where the work happens, not a tile
 *  that describes work the user cannot start from here. */
function QuickAction({
  to,
  labelKey,
  icon: Icon,
}: {
  to: string
  labelKey: TranslationKey
  icon: (props: IconProps) => JSX.Element
}) {
  const { t } = useI18n()
  return (
    <Link to={to} className="quick-action">
      <span className="quick-action__icon">
        <Icon size={16} />
      </span>
      {t(labelKey)}
    </Link>
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

  const quickActions = (
    [
      { to: '/accounts?new=1', labelKey: 'overview.actionNewAccount', icon: IconBuilding, permission: 'accounts.create' },
      { to: '/deals?new=1', labelKey: 'overview.actionNewDeal', icon: IconKanban, permission: 'deals.create' },
      { to: '/tasks?new=1', labelKey: 'overview.actionNewTask', icon: IconCheckSquare, permission: 'tasks.create' },
      { to: '/tickets?new=1', labelKey: 'overview.actionNewTicket', icon: IconLifeBuoy, permission: 'tickets.create' },
      { to: '/reports', labelKey: 'overview.actionReports', icon: IconChart, permission: 'reports.read' },
      { to: '/workflows', labelKey: 'overview.actionWorkflows', icon: IconWorkflow, permission: 'workflows.read' },
    ] as { to: string; labelKey: TranslationKey; icon: (p: IconProps) => JSX.Element; permission: string }[]
  ).filter((action) => can(action.permission))

  return (
    <>
      <header className="hero">
        <h1 className="hero__title">
          {t('overview.greeting', { name: session?.user.display_name ?? '' })}
        </h1>
        <div className="hero__meta">
          {t('overview.meta', {
            org: session?.organization.name ?? '',
            count: session?.permissions.length ?? 0,
          })}
        </div>
        {info.data ? (
          <div className="hero__chips">
            <Badge tone={info.data.environment === 'production' ? 'accent' : 'neutral'}>
              {info.data.environment}
            </Badge>
            <Badge>
              {t('overview.version')} {info.data.version}
            </Badge>
            {info.data.commit ? <Badge>{info.data.commit.slice(0, 12)}</Badge> : null}
          </div>
        ) : null}
      </header>

      {info.isError ? (
        <div style={{ marginBottom: 'var(--space-4)' }}>
          <Alert tone="error">
            {t('overview.loadError', { detail: errorMessage(info.error) })}
          </Alert>
        </div>
      ) : null}

      <div className="grid grid--stats" style={{ marginBottom: 'var(--space-5)' }}>
        {can('accounts.read') ? (
          <Metric
            label={t('overview.statAccounts')}
            queryKey="accounts"
            link="/accounts"
            icon={IconBuilding}
            fetchCount={async () => (await accountsApi.list({ limit: 1 })).total}
          />
        ) : null}
        {can('deals.read') ? (
          <Metric
            label={t('overview.statOpenDeals')}
            queryKey="deals-open"
            link="/deals"
            icon={IconKanban}
            hint={t('overview.statOpenDealsHint')}
            fetchCount={async () => (await dealsApi.list({ limit: 1, status: 'open' })).total}
          />
        ) : null}
        {can('leads.read') ? (
          <Metric
            label={t('overview.statLeads')}
            queryKey="leads"
            link="/leads"
            icon={IconTarget}
            fetchCount={async () => (await leadsApi.list({ limit: 1 })).total}
          />
        ) : null}
        {can('tasks.read') ? (
          <Metric
            label={t('overview.statOpenTasks')}
            queryKey="tasks-open"
            link="/tasks"
            icon={IconCheckSquare}
            fetchCount={async () => (await tasksApi.list({ limit: 1, status: 'open' })).total}
          />
        ) : null}
        {can('tickets.read') ? (
          <Metric
            label={t('overview.statOpenTickets')}
            queryKey="tickets-open"
            link="/tickets"
            icon={IconLifeBuoy}
            fetchCount={async () => (await ticketsApi.list({ limit: 1, status: 'open' })).total}
          />
        ) : null}
      </div>

      {quickActions.length > 0 ? (
        <div style={{ marginBottom: 'var(--space-5)' }}>
          <Card title={t('overview.quickActions')}>
            <div className="quick-actions">
              {quickActions.map((action) => (
                <QuickAction
                  key={action.to}
                  to={action.to}
                  labelKey={action.labelKey}
                  icon={action.icon}
                />
              ))}
            </div>
          </Card>
        </div>
      ) : null}

      <div className="grid grid--split">
        {can('tasks.read') ? (
          <Card
            title={t('overview.openTasks')}
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
                  <Link key={task.id} to={`/tasks/${task.id}`} className="attention-item">
                    <div style={{ minWidth: 0 }}>
                      <div className="truncate attention-item__title">{task.title}</div>
                      <div className="attention-item__meta">
                        {t('overview.due')} <Timestamp value={task.due_at} withTime={false} />
                      </div>
                    </div>
                    <Badge tone={task.priority === 'urgent' ? 'danger' : 'neutral'}>
                      {task.priority}
                    </Badge>
                  </Link>
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
                  <Link key={ticket.id} to={`/tickets/${ticket.id}`} className="attention-item">
                    <div style={{ minWidth: 0 }}>
                      <div className="truncate attention-item__title">{ticket.subject}</div>
                      <div className="attention-item__meta">
                        {t('overview.opened')} <Timestamp value={ticket.created_at} />
                      </div>
                    </div>
                    <StatusBadge status={ticket.priority} />
                  </Link>
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
