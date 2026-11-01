import { lazy } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'
import { AppShell } from './components/AppShell'
import { useAuth } from './hooks/useAuth'
import { LoginPage } from './pages/LoginPage'
import { OverviewPage } from './pages/OverviewPage'
const AccountDetailPage = lazy(() => import('./pages/AccountsPage').then((m) => ({ default: m.AccountDetailPage })))
const AccountsPage = lazy(() => import('./pages/AccountsPage').then((m) => ({ default: m.AccountsPage })))
const ContactDetailPage = lazy(() => import('./pages/ContactsPage').then((m) => ({ default: m.ContactDetailPage })))
const ContactsPage = lazy(() => import('./pages/ContactsPage').then((m) => ({ default: m.ContactsPage })))
const LeadsPage = lazy(() => import('./pages/LeadsPage').then((m) => ({ default: m.LeadsPage })))
const DealDetailPage = lazy(() => import('./pages/DealsPage').then((m) => ({ default: m.DealDetailPage })))
const DealsPage = lazy(() => import('./pages/DealsPage').then((m) => ({ default: m.DealsPage })))
const TasksPage = lazy(() => import('./pages/TasksPage').then((m) => ({ default: m.TasksPage })))
const TicketsPage = lazy(() => import('./pages/TicketsPage').then((m) => ({ default: m.TicketsPage })))
const ReportsPage = lazy(() => import('./pages/ReportsPage').then((m) => ({ default: m.ReportsPage })))
const ImportCenterPage = lazy(() => import('./pages/ImportCenterPage').then((m) => ({ default: m.ImportCenterPage })))
const ImportDetailPage = lazy(() => import('./pages/ImportDetailPage').then((m) => ({ default: m.ImportDetailPage })))
const IntegrationsPage = lazy(() => import('./pages/IntegrationsPage').then((m) => ({ default: m.IntegrationsPage })))
const MigrationsPage = lazy(() => import('./pages/MigrationsPage').then((m) => ({ default: m.MigrationsPage })))
const WebhooksPage = lazy(() => import('./pages/WebhooksPage').then((m) => ({ default: m.WebhooksPage })))
const EventsPage = lazy(() => import('./pages/EventsPage').then((m) => ({ default: m.EventsPage })))
const WorkflowsPage = lazy(() => import('./pages/WorkflowsPage').then((m) => ({ default: m.WorkflowsPage })))
const SimulationPage = lazy(() => import('./pages/SimulationPage').then((m) => ({ default: m.SimulationPage })))
const ForecastPage = lazy(() => import('./pages/ForecastPage').then((m) => ({ default: m.ForecastPage })))
const RiskPage = lazy(() => import('./pages/RiskPage').then((m) => ({ default: m.RiskPage })))
const JobsPage = lazy(() => import('./pages/JobsPage').then((m) => ({ default: m.JobsPage })))
const AiPage = lazy(() => import('./pages/AiPage').then((m) => ({ default: m.AiPage })))
const ApprovalsPage = lazy(() => import('./pages/ApprovalsPage').then((m) => ({ default: m.ApprovalsPage })))
const NotificationsPage = lazy(() => import('./pages/NotificationsPage').then((m) => ({ default: m.NotificationsPage })))
const UsersPage = lazy(() => import('./pages/admin/UsersPage').then((m) => ({ default: m.UsersPage })))
const RolesPage = lazy(() => import('./pages/admin/RolesPage').then((m) => ({ default: m.RolesPage })))
const OrganizationsPage = lazy(() => import('./pages/admin/OrganizationsPage').then((m) => ({ default: m.OrganizationsPage })))
const TeamsPage = lazy(() => import('./pages/admin/TeamsPage').then((m) => ({ default: m.TeamsPage })))
const AuditPage = lazy(() => import('./pages/admin/AuditPage').then((m) => ({ default: m.AuditPage })))
const ConsentPage = lazy(() => import('./pages/admin/ConsentPage').then((m) => ({ default: m.ConsentPage })))
const SecurityPage = lazy(() => import('./pages/admin/SecurityPage').then((m) => ({ default: m.SecurityPage })))
const DiagnosticsPage = lazy(() => import('./pages/admin/DiagnosticsPage').then((m) => ({ default: m.DiagnosticsPage })))
const EmailPage = lazy(() => import('./pages/admin/EmailPage').then((m) => ({ default: m.EmailPage })))
const TelegramPage = lazy(() => import('./pages/admin/TelegramPage').then((m) => ({ default: m.TelegramPage })))
const SSOPage = lazy(() => import('./pages/admin/SSOPage').then((m) => ({ default: m.SSOPage })))
const MCPCredentialsPage = lazy(() => import('./pages/admin/MCPCredentialsPage').then((m) => ({ default: m.MCPCredentialsPage })))
const TenantSettingsPage = lazy(() => import('./pages/admin/TenantSettingsPage').then((m) => ({ default: m.TenantSettingsPage })))
const AccountPage = lazy(() => import('./pages/AccountPage').then((m) => ({ default: m.AccountPage })))
const ForbiddenPage = lazy(() => import('./pages/ErrorPages').then((m) => ({ default: m.ForbiddenPage })))
const NotFoundPage = lazy(() => import('./pages/ErrorPages').then((m) => ({ default: m.NotFoundPage })))
import { RequirePermission } from './components/RequirePermission'

function FullPageSkeleton() {
  return (
    <div className="content" aria-busy="true">
      <div className="skeleton" style={{ width: 200, height: 26, marginBottom: 24 }} />
      <div className="skeleton" style={{ width: '100%', height: 220 }} />
    </div>
  )
}

export function App() {
  const { status } = useAuth()

  // Nothing is rendered until the session is known, so a signed-in user is not
  // shown a login form for a frame, and an anonymous user is not shown an
  // empty shell.
  if (status === 'loading') return <FullPageSkeleton />

  if (status === 'anonymous') {
    return (
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="*" element={<Navigate to="/login" replace />} />
      </Routes>
    )
  }

  return (
    <Routes>
      <Route path="/login" element={<Navigate to="/" replace />} />
      <Route element={<AppShell />}>
        <Route index element={<OverviewPage />} />

        <Route
          path="accounts"
          element={
            <RequirePermission permission="accounts.read">
              <AccountsPage />
            </RequirePermission>
          }
        />
        <Route
          path="accounts/:id"
          element={
            <RequirePermission permission="accounts.read">
              <AccountDetailPage />
            </RequirePermission>
          }
        />
        <Route
          path="contacts"
          element={
            <RequirePermission permission="contacts.read">
              <ContactsPage />
            </RequirePermission>
          }
        />
        <Route
          path="contacts/:id"
          element={
            <RequirePermission permission="contacts.read">
              <ContactDetailPage />
            </RequirePermission>
          }
        />
        <Route
          path="leads"
          element={
            <RequirePermission permission="leads.read">
              <LeadsPage />
            </RequirePermission>
          }
        />
        <Route
          path="deals"
          element={
            <RequirePermission permission="deals.read">
              <DealsPage />
            </RequirePermission>
          }
        />
        <Route
          path="deals/:id"
          element={
            <RequirePermission permission="deals.read">
              <DealDetailPage />
            </RequirePermission>
          }
        />
        <Route
          path="tasks"
          element={
            <RequirePermission permission="tasks.read">
              <TasksPage />
            </RequirePermission>
          }
        />
        <Route
          path="tickets"
          element={
            <RequirePermission permission="tickets.read">
              <TicketsPage />
            </RequirePermission>
          }
        />
        <Route
          path="reports"
          element={
            <RequirePermission permission="reports.read">
              <ReportsPage />
            </RequirePermission>
          }
        />

        <Route
          path="imports"
          element={
            <RequirePermission permission="imports.read">
              <ImportCenterPage />
            </RequirePermission>
          }
        />
        <Route
          path="imports/:id"
          element={
            <RequirePermission permission="imports.read">
              <ImportDetailPage />
            </RequirePermission>
          }
        />
        <Route
          path="migrations"
          element={
            <RequirePermission permission="migrations.read">
              <MigrationsPage />
            </RequirePermission>
          }
        />
        <Route
          path="integrations"
          element={
            <RequirePermission permission="integrations.read">
              <IntegrationsPage />
            </RequirePermission>
          }
        />
        <Route
          path="webhooks"
          element={
            <RequirePermission permission="webhooks.manage">
              <WebhooksPage />
            </RequirePermission>
          }
        />
        <Route
          path="events"
          element={
            <RequirePermission permission="events.read">
              <EventsPage />
            </RequirePermission>
          }
        />
        <Route
          path="workflows"
          element={
            <RequirePermission permission="workflows.read">
              <WorkflowsPage />
            </RequirePermission>
          }
        />
        <Route
          path="simulation"
          element={
            <RequirePermission permission="simulation.read">
              <SimulationPage />
            </RequirePermission>
          }
        />
        <Route
          path="forecast"
          element={
            <RequirePermission permission="forecast.read">
              <ForecastPage />
            </RequirePermission>
          }
        />
        <Route
          path="risk"
          element={
            <RequirePermission permission="risk.read">
              <RiskPage />
            </RequirePermission>
          }
        />
        <Route
          path="jobs"
          element={
            <RequirePermission permission="jobs.read">
              <JobsPage />
            </RequirePermission>
          }
        />
        <Route
          path="ai"
          element={
            <RequirePermission permission="ai.read">
              <AiPage />
            </RequirePermission>
          }
        />
        <Route
          path="approvals"
          element={
            <RequirePermission permission="approvals.read">
              <ApprovalsPage />
            </RequirePermission>
          }
        />
        <Route
          path="notifications"
          element={
            <RequirePermission permission="notifications.read">
              <NotificationsPage />
            </RequirePermission>
          }
        />

        <Route
          path="admin/users"
          element={
            <RequirePermission permission="users.read">
              <UsersPage />
            </RequirePermission>
          }
        />
        <Route
          path="admin/roles"
          element={
            <RequirePermission permission="roles.read">
              <RolesPage />
            </RequirePermission>
          }
        />
        <Route
          path="admin/organizations"
          element={
            <RequirePermission permission="organizations.read">
              <OrganizationsPage />
            </RequirePermission>
          }
        />
        <Route
          path="admin/teams"
          element={
            <RequirePermission permission="teams.manage">
              <TeamsPage />
            </RequirePermission>
          }
        />
        <Route
          path="admin/audit"
          element={
            <RequirePermission permission="audit.read">
              <AuditPage />
            </RequirePermission>
          }
        />
        <Route
          path="admin/consent"
          element={
            <RequirePermission permission="consent.read">
              <ConsentPage />
            </RequirePermission>
          }
        />
        <Route
          path="admin/security"
          element={
            <RequirePermission permission="security.read">
              <SecurityPage />
            </RequirePermission>
          }
        />
        <Route
          path="admin/diagnostics"
          element={
            <RequirePermission permission="logs.read">
              <DiagnosticsPage />
            </RequirePermission>
          }
        />
        <Route
          path="admin/email"
          element={
            <RequirePermission permission="email.manage">
              <EmailPage />
            </RequirePermission>
          }
        />
        <Route
          path="admin/telegram"
          element={
            <RequirePermission permission="email.manage">
              <TelegramPage />
            </RequirePermission>
          }
        />
        <Route
          path="admin/sso"
          element={
            <RequirePermission permission="sso.read">
              <SSOPage />
            </RequirePermission>
          }
        />
        <Route
          path="admin/mcp-credentials"
          element={
            <RequirePermission permission="mcp.credentials.read">
              <MCPCredentialsPage />
            </RequirePermission>
          }
        />
        <Route
          path="admin/configuration"
          element={
            <RequirePermission permission="tenant.settings.read">
              <TenantSettingsPage />
            </RequirePermission>
          }
        />

        <Route path="account" element={<AccountPage />} />
        <Route path="forbidden" element={<ForbiddenPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  )
}
