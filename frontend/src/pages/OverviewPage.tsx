import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { accountsApi, dealsApi, leadsApi, tasksApi, ticketsApi } from '../api/crm'
import { systemApi } from '../api/admin'
import { errorMessage } from '../api/client'
import { useAuth } from '../hooks/useAuth'
import { Alert, Badge, Card, ErrorState, Timestamp } from '../components/ui'
import {
  IconBuilding,
  IconChart,
  IconCheckSquare,
  IconInbox,
  IconKanban,
  IconLifeBuoy,
  IconTarget,
  IconUsers,
  IconWorkflow,
} from '../components/icons'
import { useI18n, type TranslationKey } from '../i18n'

type Tone = 'neutral' | 'success' | 'danger' | 'warning' | 'accent'

/** Priority is a small fixed vocabulary, so its colour is stated once here. */
function priorityTone(priority: string | undefined): Tone {
  switch (priority) {
    case 'urgent':
      return 'danger'
    case 'high':
      return 'warning'
    case 'low':
      return 'neutral'
    default:
      return 'neutral'
  }
}

/**
 * A count tile backed by its own query, so one failing count does not blank the
 * rest of the page. The whole tile is the link, matching the metric stylesheet:
 * the label and the value are one target rather than a number inside a box.
 */
function MetricTile({
  label,
  queryKey,
  fetchCount,
  hint,
  link,
  icon,
}: {
  label: string
  queryKey: string
  fetchCount: () => Promise<number>
  hint?: string
  link: string
  icon: ReactNode
}) {
  const query = useQuery({ queryKey: ['count', queryKey], queryFn: fetchCount })
  const failed = query.isError
  const value = query.isLoading ? '—' : failed ? '!' : query.data

  return (
    <Link to={link} className="metric">
      <div className="metric__top">
        <span className="metric__icon" aria-hidden="true">
          {icon}
        </span>
      </div>
      <div className="metric__label">{label}</div>
      <div className={failed ? 'metric__value metric__value--error' : 'metric__value'}>{value}</div>
      {hint ? <div className="metric__hint">{hint}</div> : null}
    </Link>
  )
}

interface QuickAction {
  to: string
  labelKey: TranslationKey
  permission: string
  icon: ReactNode
}

const QUICK_ACTIONS: QuickAction[] = [
  { to: '/accounts', labelKey: 'nav.accounts', permission: 'accounts.read', icon: <IconBuilding /> },
  { to: '/contacts', labelKey: 'nav.contacts', permission: 'contacts.read', icon: <IconUsers /> },
  { to: '/leads', labelKey: 'nav.leads', permission: 'leads.read', icon: <IconTarget /> },
  { to: '/deals', labelKey: 'nav.deals', permission: 'deals.read', icon: <IconKanban /> },
  { to: '/tasks', labelKey: 'nav.tasks', permission: 'tasks.read', icon: <IconCheckSquare /> },
  { to: '/tickets', labelKey: 'nav.support', permission: 'tickets.read', icon: <IconLifeBuoy /> },
  { to: '/reports', labelKey: 'nav.reports', permission: 'reports.read', icon: <IconChart /> },
  { to: '/workflows', labelKey: 'nav.workflows', permission: 'workflows.read', icon: <IconWorkflow /> },
  { to: '/imports', labelKey: 'nav.importCenter', permission: 'imports.read', icon: <IconInbox /> },
]

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

  const quickActions = QUICK_ACTIONS.filter((action) => can(action.permission))

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
            <Badge tone="neutral">{info.data.service}</Badge>
            <Badge tone={info.data.environment === 'production' ? 'accent' : 'neutral'}>
              {info.data.environment}
            </Badge>
            <Badge tone="neutral">
              {t('overview.version')} {info.data.version}
            </Badge>
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
          <MetricTile
            label={t('overview.statAccounts')}
            queryKey="accounts"
            link="/accounts"
            icon={<IconBuilding size={16} />}
            fetchCount={async () => (await accountsApi.list({ limit: 1 })).total}
          />
        ) : null}
        {can('deals.read') ? (
          <MetricTile
            label={t('overview.statOpenDeals')}
            queryKey="deals-open"
            link="/deals"
            icon={<IconKanban size={16} />}
            hint={t('overview.statOpenDealsHint')}
            fetchCount={async () => (await dealsApi.list({ limit: 1, status: 'open' })).total}
          />
        ) : null}
        {can('leads.read') ? (
          <MetricTile
            label={t('overview.statLeads')}
            queryKey="leads"
            link="/leads"
            icon={<IconTarget size={16} />}
            fetchCount={async () => (await leadsApi.list({ limit: 1 })).total}
          />
        ) : null}
        {can('tasks.read') ? (
          <MetricTile
            label={t('overview.statOpenTasks')}
            queryKey="tasks-open"
            link="/tasks"
            icon={<IconCheckSquare size={16} />}
            fetchCount={async () => (await tasksApi.list({ limit: 1, status: 'open' })).total}
          />
        ) : null}
        {can('tickets.read') ? (
          <MetricTile
            label={t('overview.statOpenTickets')}
            queryKey="tickets-open"
            link="/tickets"
            icon={<IconLifeBuoy size={16} />}
            fetchCount={async () => (await ticketsApi.list({ limit: 1, status: 'open' })).total}
          />
        ) : null}
      </div>

      {quickActions.length ? (
        <div style={{ marginBottom: 'var(--space-5)' }}>
          <Card title={t('overview.quickActions')}>
            <div className="quick-actions">
              {quickActions.map((action) => (
                <Link key={action.to} to={action.to} className="quick-action">
                  <span className="quick-action__icon" aria-hidden="true">
                    {action.icon}
                  </span>
                  {t(action.labelKey)}
                </Link>
              ))}
            </div>
          </Card>
        </div>
      ) : null}

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
              <div>
                {recentTasks.data.items.map((task) => (
                  <Link key={task.id} to={`/tasks/${task.id}`} className="attention-item">
                    <div style={{ minWidth: 0 }}>
                      <div className="attention-item__title truncate">{task.title}</div>
                      <div className="attention-item__meta">
                        {t('overview.due')} <Timestamp value={task.due_at} withTime={false} />
                      </div>
                    </div>
                    <Badge tone={priorityTone(task.priority)}>{task.priority}</Badge>
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
              <div>
                {openTickets.data.items.map((ticket) => (
                  <Link key={ticket.id} to={`/tickets/${ticket.id}`} className="attention-item">
                    <div style={{ minWidth: 0 }}>
                      <div className="attention-item__title truncate">{ticket.subject}</div>
                      <div className="attention-item__meta">
                        {t('overview.opened')} <Timestamp value={ticket.created_at} />
                      </div>
                    </div>
                    <Badge tone={priorityTone(ticket.priority)}>{ticket.priority}</Badge>
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
