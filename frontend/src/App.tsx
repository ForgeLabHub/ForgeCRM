import { Navigate, Route, Routes } from 'react-router-dom'
import { AppShell } from './components/AppShell'
import { useAuth } from './hooks/useAuth'
import { LoginPage } from './pages/LoginPage'
import { OverviewPage } from './pages/OverviewPage'
import { AccountDetailPage, AccountsPage } from './pages/AccountsPage'
import { ContactDetailPage, ContactsPage } from './pages/ContactsPage'
import { LeadsPage } from './pages/LeadsPage'
import { DealDetailPage, DealsPage } from './pages/DealsPage'
import { TasksPage } from './pages/TasksPage'
import { TicketsPage } from './pages/TicketsPage'
import { ReportsPage } from './pages/ReportsPage'
import { ImportCenterPage } from './pages/ImportCenterPage'
import { ImportDetailPage } from './pages/ImportDetailPage'
import { IntegrationsPage } from './pages/IntegrationsPage'
import { MigrationsPage } from './pages/MigrationsPage'
import { WebhooksPage } from './pages/WebhooksPage'
import { EventsPage } from './pages/EventsPage'
import { WorkflowsPage } from './pages/WorkflowsPage'
import { SimulationPage } from './pages/SimulationPage'
import { ForecastPage } from './pages/ForecastPage'
import { RiskPage } from './pages/RiskPage'
import { JobsPage } from './pages/JobsPage'
import { AiPage } from './pages/AiPage'
import { ApprovalsPage } from './pages/ApprovalsPage'
import { NotificationsPage } from './pages/NotificationsPage'
import { UsersPage } from './pages/admin/UsersPage'
import { RolesPage } from './pages/admin/RolesPage'
import { OrganizationsPage } from './pages/admin/OrganizationsPage'
import { TeamsPage } from './pages/admin/TeamsPage'
import { AuditPage } from './pages/admin/AuditPage'
import { SecurityPage } from './pages/admin/SecurityPage'
import { DiagnosticsPage } from './pages/admin/DiagnosticsPage'
import { EmailPage } from './pages/admin/EmailPage'
import { TelegramPage } from './pages/admin/TelegramPage'
import { SSOPage } from './pages/admin/SSOPage'
import { MCPCredentialsPage } from './pages/admin/MCPCredentialsPage'
import { AccountPage } from './pages/AccountPage'
import { ForbiddenPage, NotFoundPage } from './pages/ErrorPages'
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

        <Route path="account" element={<AccountPage />} />
        <Route path="forbidden" element={<ForbiddenPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  )
}
