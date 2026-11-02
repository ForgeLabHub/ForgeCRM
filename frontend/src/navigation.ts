/**
 * The application's navigation model.
 *
 * One list feeds both the sidebar and the command palette, so a section cannot
 * appear in one and be missing from the other. Each entry names the permission
 * it needs; the sidebar hides what the user cannot reach and the palette does
 * the same, but the server enforces the permission independently — hiding a
 * link is convenience, never security.
 *
 * Labels are translation keys rather than literals, so switching language
 * relabels the whole navigation as one unit instead of leaving half of it in
 * the previous tongue.
 */
import type { JSX } from 'react'
import type { TranslationKey } from './i18n'
import type { IconProps } from './components/icons'
import {
  IconActivity,
  IconBadgeCheck,
  IconBell,
  IconBuilding,
  IconChart,
  IconCheckSquare,
  IconClipboardCheck,
  IconClock,
  IconDatabase,
  IconFlask,
  IconGlobe,
  IconGrid,
  IconInbox,
  IconKanban,
  IconKeyRound,
  IconLifeBuoy,
  IconLock,
  IconMail,
  IconPlug,
  IconScroll,
  IconSend,
  IconShield,
  IconShieldAlert,
  IconSliders,
  IconSparkles,
  IconStethoscope,
  IconTarget,
  IconTerminal,
  IconTrendingUp,
  IconUserCog,
  IconUsers,
  IconUsersRound,
  IconWebhook,
  IconWorkflow,
} from './components/icons'

export interface NavEntry {
  to: string
  labelKey: TranslationKey
  permission?: string
  icon: (props: IconProps) => JSX.Element
  /** Extra words the palette matches on, so "companies" finds Accounts. */
  keywords?: string[]
}

export interface NavGroup {
  labelKey: TranslationKey
  items: NavEntry[]
}

/**
 * Navigation is grouped by intent rather than by API prefix, so the sidebar
 * reads as a product rather than as a route table.
 */
export const NAV_GROUPS: NavGroup[] = [
  {
    labelKey: 'nav.workspace',
    items: [
      { to: '/', labelKey: 'nav.overview', icon: IconGrid, keywords: ['home', 'dashboard'] },
      {
        to: '/accounts',
        labelKey: 'nav.accounts',
        permission: 'accounts.read',
        icon: IconBuilding,
        keywords: ['companies', 'organizations', 'клиенты', 'компании'],
      },
      {
        to: '/contacts',
        labelKey: 'nav.contacts',
        permission: 'contacts.read',
        icon: IconUsers,
        keywords: ['people', 'контакты'],
      },
      {
        to: '/leads',
        labelKey: 'nav.leads',
        permission: 'leads.read',
        icon: IconTarget,
        keywords: ['prospects', 'лиды'],
      },
      {
        to: '/deals',
        labelKey: 'nav.deals',
        permission: 'deals.read',
        icon: IconKanban,
        keywords: ['pipeline', 'opportunities', 'сделки'],
      },
      {
        to: '/tasks',
        labelKey: 'nav.tasks',
        permission: 'tasks.read',
        icon: IconCheckSquare,
        keywords: ['todo', 'задачи'],
      },
      {
        to: '/tickets',
        labelKey: 'nav.support',
        permission: 'tickets.read',
        icon: IconLifeBuoy,
        keywords: ['support', 'helpdesk', 'поддержка', 'тикеты'],
      },
      {
        to: '/reports',
        labelKey: 'nav.reports',
        permission: 'reports.read',
        icon: IconChart,
        keywords: ['analytics', 'отчёты'],
      },
      {
        to: '/simulation',
        labelKey: 'nav.simulation',
        permission: 'simulation.read',
        icon: IconFlask,
        keywords: ['scenarios', 'сценарии'],
      },
      {
        to: '/forecast',
        labelKey: 'nav.forecasting',
        permission: 'forecast.read',
        icon: IconTrendingUp,
        keywords: ['прогноз'],
      },
      {
        to: '/risk',
        labelKey: 'nav.risk',
        permission: 'risk.read',
        icon: IconShieldAlert,
        keywords: ['риски'],
      },
    ],
  },
  {
    labelKey: 'nav.data',
    items: [
      {
        to: '/imports',
        labelKey: 'nav.importCenter',
        permission: 'imports.read',
        icon: IconInbox,
        keywords: ['csv', 'импорт'],
      },
      {
        to: '/migrations',
        labelKey: 'nav.migrations',
        permission: 'migrations.read',
        icon: IconDatabase,
        keywords: ['миграции'],
      },
      {
        to: '/integrations',
        labelKey: 'nav.integrations',
        permission: 'integrations.read',
        icon: IconPlug,
        keywords: ['интеграции'],
      },
    ],
  },
  {
    labelKey: 'nav.platform',
    items: [
      { to: '/webhooks', labelKey: 'nav.webhooks', permission: 'webhooks.manage', icon: IconWebhook },
      { to: '/events', labelKey: 'nav.events', permission: 'events.read', icon: IconActivity },
      { to: '/workflows', labelKey: 'nav.workflows', permission: 'workflows.read', icon: IconWorkflow },
      { to: '/jobs', labelKey: 'nav.jobs', permission: 'jobs.read', icon: IconClock },
      {
        to: '/approvals',
        labelKey: 'nav.approvals',
        permission: 'approvals.read',
        icon: IconBadgeCheck,
      },
      {
        to: '/notifications',
        labelKey: 'nav.notifications',
        permission: 'notifications.read',
        icon: IconBell,
      },
      { to: '/ai', labelKey: 'nav.ai', permission: 'ai.read', icon: IconSparkles },
    ],
  },
  {
    labelKey: 'nav.administration',
    items: [
      { to: '/admin/users', labelKey: 'nav.users', permission: 'users.read', icon: IconUserCog },
      { to: '/admin/roles', labelKey: 'nav.roles', permission: 'roles.read', icon: IconKeyRound },
      {
        to: '/admin/organizations',
        labelKey: 'nav.organizations',
        permission: 'organizations.read',
        icon: IconGlobe,
      },
      {
        to: '/admin/teams',
        labelKey: 'nav.teams',
        permission: 'teams.manage',
        icon: IconUsersRound,
      },
      { to: '/admin/email', labelKey: 'nav.email', permission: 'email.manage', icon: IconMail },
      { to: '/admin/telegram', labelKey: 'nav.telegram', permission: 'email.manage', icon: IconSend },
      {
        to: '/admin/consent',
        labelKey: 'nav.consent',
        permission: 'consent.read',
        icon: IconScroll,
      },
      { to: '/admin/sso', labelKey: 'nav.sso', permission: 'sso.read', icon: IconLock },
      {
        to: '/admin/mcp-credentials',
        labelKey: 'nav.mcpCredentials',
        permission: 'mcp.credentials.read',
        icon: IconTerminal,
      },
      {
        to: '/admin/configuration',
        labelKey: 'nav.configuration',
        permission: 'tenant.settings.read',
        icon: IconSliders,
      },
      {
        to: '/admin/audit',
        labelKey: 'nav.audit',
        permission: 'audit.read',
        icon: IconClipboardCheck,
      },
      {
        to: '/admin/security',
        labelKey: 'nav.security',
        permission: 'security.read',
        icon: IconShield,
      },
      {
        to: '/admin/diagnostics',
        labelKey: 'nav.diagnostics',
        permission: 'logs.read',
        icon: IconStethoscope,
      },
    ],
  },
]

/** The nav entries the given permission check allows, grouped as declared. */
export function visibleNavGroups(can: (permission: string) => boolean): NavGroup[] {
  return NAV_GROUPS.map((group) => ({
    ...group,
    items: group.items.filter((item) => !item.permission || can(item.permission)),
  })).filter((group) => group.items.length > 0)
}

/** A flat list of every reachable destination, for the command palette. */
export function flatNavEntries(can: (permission: string) => boolean): NavEntry[] {
  return visibleNavGroups(can).flatMap((group) => group.items)
}
