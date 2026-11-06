/**
 * The application's navigation model.
 *
 * This lives outside the shell because more than the sidebar reads it now: the
 * workspace switcher groups the same entries into role-shaped views, and the
 * global search palette links the same routes. One declaration means a route
 * cannot appear in one place and be missing from another.
 *
 * Navigation is grouped by intent rather than by API prefix, so the sidebar reads
 * as a product rather than as a route table. Each entry declares the permission
 * it needs and is hidden without it, but the server enforces the same permission
 * independently: hiding a link is convenience, not security.
 *
 * Labels are translation keys, not literals, so switching language relabels the
 * navigation as one unit. Every entry carries an icon so the eye can find a
 * section by shape before it reads the label.
 */
import type { JSX } from 'react'
import type { TranslationKey } from './i18n'
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
  type IconProps,
} from './components/icons'

export interface NavEntry {
  to: string
  /** Translation key for the label, resolved at render so a language change
   *  relabels the whole sidebar without a reload. */
  labelKey: TranslationKey
  permission?: string
  icon: (props: IconProps) => JSX.Element
}

export interface NavGroup {
  labelKey: TranslationKey
  items: NavEntry[]
}

export const NAV_GROUPS: NavGroup[] = [
  {
    labelKey: 'nav.workspace',
    items: [
      { to: '/app', labelKey: 'nav.overview', icon: IconGrid },
      { to: '/app/accounts', labelKey: 'nav.accounts', permission: 'accounts.read', icon: IconBuilding },
      { to: '/app/contacts', labelKey: 'nav.contacts', permission: 'contacts.read', icon: IconUsers },
      { to: '/app/leads', labelKey: 'nav.leads', permission: 'leads.read', icon: IconTarget },
      { to: '/app/deals', labelKey: 'nav.deals', permission: 'deals.read', icon: IconKanban },
      { to: '/app/tasks', labelKey: 'nav.tasks', permission: 'tasks.read', icon: IconCheckSquare },
      { to: '/app/tickets', labelKey: 'nav.support', permission: 'tickets.read', icon: IconLifeBuoy },
      { to: '/app/reports', labelKey: 'nav.reports', permission: 'reports.read', icon: IconChart },
      { to: '/app/simulation', labelKey: 'nav.simulation', permission: 'simulation.read', icon: IconFlask },
      { to: '/app/forecast', labelKey: 'nav.forecasting', permission: 'forecast.read', icon: IconTrendingUp },
      { to: '/app/risk', labelKey: 'nav.risk', permission: 'risk.read', icon: IconShieldAlert },
    ],
  },
  {
    labelKey: 'nav.data',
    items: [
      { to: '/app/imports', labelKey: 'nav.importCenter', permission: 'imports.read', icon: IconInbox },
      { to: '/app/migrations', labelKey: 'nav.migrations', permission: 'migrations.read', icon: IconDatabase },
      { to: '/app/integrations', labelKey: 'nav.integrations', permission: 'integrations.read', icon: IconPlug },
    ],
  },
  {
    labelKey: 'nav.platform',
    items: [
      { to: '/app/webhooks', labelKey: 'nav.webhooks', permission: 'webhooks.manage', icon: IconWebhook },
      { to: '/app/events', labelKey: 'nav.events', permission: 'events.read', icon: IconActivity },
      { to: '/app/workflows', labelKey: 'nav.workflows', permission: 'workflows.read', icon: IconWorkflow },
      { to: '/app/jobs', labelKey: 'nav.jobs', permission: 'jobs.read', icon: IconClock },
      { to: '/app/approvals', labelKey: 'nav.approvals', permission: 'approvals.read', icon: IconBadgeCheck },
      { to: '/app/notifications', labelKey: 'nav.notifications', permission: 'notifications.read', icon: IconBell },
      { to: '/app/ai', labelKey: 'nav.ai', permission: 'ai.read', icon: IconSparkles },
    ],
  },
  {
    labelKey: 'nav.administration',
    items: [
      { to: '/app/admin/users', labelKey: 'nav.users', permission: 'users.read', icon: IconUserCog },
      { to: '/app/admin/roles', labelKey: 'nav.roles', permission: 'roles.read', icon: IconKeyRound },
      { to: '/app/admin/organizations', labelKey: 'nav.organizations', permission: 'organizations.read', icon: IconGlobe },
      { to: '/app/admin/teams', labelKey: 'nav.teams', permission: 'teams.manage', icon: IconUsersRound },
      { to: '/app/admin/email', labelKey: 'nav.email', permission: 'email.manage', icon: IconMail },
      { to: '/app/admin/telegram', labelKey: 'nav.telegram', permission: 'email.manage', icon: IconSend },
      { to: '/app/admin/consent', labelKey: 'nav.consent', permission: 'consent.read', icon: IconScroll },
      { to: '/app/admin/sso', labelKey: 'nav.sso', permission: 'sso.read', icon: IconLock },
      { to: '/app/admin/mcp-credentials', labelKey: 'nav.mcpCredentials', permission: 'mcp.credentials.read', icon: IconTerminal },
      { to: '/app/admin/configuration', labelKey: 'nav.configuration', permission: 'tenant.settings.read', icon: IconSliders },
      { to: '/app/admin/audit', labelKey: 'nav.audit', permission: 'audit.read', icon: IconClipboardCheck },
      { to: '/app/admin/security', labelKey: 'nav.security', permission: 'security.read', icon: IconShield },
      { to: '/app/admin/diagnostics', labelKey: 'nav.diagnostics', permission: 'logs.read', icon: IconStethoscope },
    ],
  },
]

/**
 * The CRM's home route. The public site owns `/`, so the application lives
 * behind a prefix; naming it once means the shells, the lookup above and the
 * redirect after sign-in cannot disagree about where "home" is.
 */
export const APP_HOME = '/app'

/** Every route the shell knows, flattened, for lookups by path. */
export const NAV_ENTRIES: NavEntry[] = NAV_GROUPS.flatMap((group) => group.items)

const BY_PATH = new Map(NAV_ENTRIES.map((entry) => [entry.to, entry]))

/** The nav entry for a path, or undefined when the path is not in the sidebar. */
export function navEntryFor(path: string): NavEntry | undefined {
  return BY_PATH.get(path)
}
