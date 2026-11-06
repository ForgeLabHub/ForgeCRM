/**
 * Personal workspaces.
 *
 * A workspace is a saved shape of the same application, not a separate app: it
 * decides which of the existing routes lead the sidebar and in what order, so a
 * salesperson sees deals and the funnel first while an executive sees KPIs. It
 * grants nothing — every route still passes the same permission check it always
 * did, and a route the user cannot read is dropped before it is shown. Switching
 * workspaces reorders and narrows navigation; it never widens access.
 *
 * The routes named here are the ones declared in navigation.ts. A route that is
 * not listed in a workspace stays reachable through global search and its URL,
 * so a workspace is a lens on the app rather than a gate around it.
 */
import type { TranslationKey } from './i18n'

export const WORKSPACES = [
  'sales',
  'executive',
  'support',
  'marketing',
  'operations',
  'analyst',
  'admin',
  'all',
] as const

export type Workspace = (typeof WORKSPACES)[number]

export const DEFAULT_WORKSPACE: Workspace = 'all'
export const WORKSPACE_STORAGE_KEY = 'forgecrm.workspace'

export interface WorkspaceDefinition {
  id: Workspace
  labelKey: TranslationKey
  descriptionKey: TranslationKey
  /** A short glyph shown on the switcher button, so the current view is legible
   *  at a glance without reading the label. */
  glyph: string
  /**
   * Routes this workspace leads with, most important first. Order is the whole
   * point: the first routes are what the role opens the app to see.
   */
  routes: string[]
}

export const WORKSPACE_DEFINITIONS: WorkspaceDefinition[] = [
  {
    id: 'sales',
    labelKey: 'workspace.sales.name',
    descriptionKey: 'workspace.sales.description',
    glyph: '◧',
    routes: [
      '/app',
      '/app/deals',
      '/app/leads',
      '/app/contacts',
      '/app/accounts',
      '/app/tasks',
      '/app/forecast',
      '/app/reports',
      '/app/simulation',
    ],
  },
  {
    id: 'executive',
    labelKey: 'workspace.executive.name',
    descriptionKey: 'workspace.executive.description',
    glyph: '◨',
    routes: [
      '/app',
      '/app/reports',
      '/app/forecast',
      '/app/risk',
      '/app/simulation',
      '/app/deals',
      '/app/tickets',
      '/app/tasks',
      '/app/approvals',
    ],
  },
  {
    id: 'support',
    labelKey: 'workspace.support.name',
    descriptionKey: 'workspace.support.description',
    glyph: '◎',
    routes: ['/app', '/app/tickets', '/app/contacts', '/app/accounts', '/app/tasks', '/app/notifications'],
  },
  {
    id: 'marketing',
    labelKey: 'workspace.marketing.name',
    descriptionKey: 'workspace.marketing.description',
    glyph: '◆',
    routes: ['/app', '/app/leads', '/app/contacts', '/app/accounts', '/app/reports', '/app/imports'],
  },
  {
    id: 'operations',
    labelKey: 'workspace.operations.name',
    descriptionKey: 'workspace.operations.description',
    glyph: '▦',
    routes: [
      '/app',
      '/app/tasks',
      '/app/imports',
      '/app/jobs',
      '/app/integrations',
      '/app/workflows',
      '/app/events',
      '/app/webhooks',
      '/app/migrations',
    ],
  },
  {
    id: 'analyst',
    labelKey: 'workspace.analyst.name',
    descriptionKey: 'workspace.analyst.description',
    glyph: '▲',
    routes: ['/app', '/app/reports', '/app/forecast', '/app/risk', '/app/simulation', '/app/deals', '/app/tickets'],
  },
  {
    id: 'admin',
    labelKey: 'workspace.admin.name',
    descriptionKey: 'workspace.admin.description',
    glyph: '⚙',
    routes: [
      '/app',
      '/app/admin/users',
      '/app/admin/roles',
      '/app/admin/security',
      '/app/admin/audit',
      '/app/admin/configuration',
      '/app/admin/mcp-credentials',
      '/app/admin/sso',
      '/app/admin/consent',
      '/app/admin/diagnostics',
      '/app/admin/email',
      '/app/admin/telegram',
      '/app/admin/organizations',
      '/app/admin/teams',
    ],
  },
  {
    id: 'all',
    labelKey: 'workspace.all.name',
    descriptionKey: 'workspace.all.description',
    glyph: '≣',
    // The empty list means "no reordering": the full grouped navigation shows as
    // it always has. `all` is the way back to the ungrouped product.
    routes: [],
  },
]

const BY_ID = new Map(WORKSPACE_DEFINITIONS.map((definition) => [definition.id, definition]))

export function workspaceDefinition(id: Workspace): WorkspaceDefinition {
  return BY_ID.get(id) ?? BY_ID.get(DEFAULT_WORKSPACE)!
}

export function isWorkspace(value: string | null): value is Workspace {
  return value !== null && (WORKSPACES as readonly string[]).includes(value)
}

export function workspaceLabelKey(id: Workspace): TranslationKey {
  return workspaceDefinition(id).labelKey
}

export function workspaceDescriptionKey(id: Workspace): TranslationKey {
  return workspaceDefinition(id).descriptionKey
}
