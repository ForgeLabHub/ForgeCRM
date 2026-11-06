import { lazy, type ReactNode } from 'react'
import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { AppShell } from './components/AppShell'
import { AiAssistantLauncher } from './components/AiAssistant'
import { useAuth } from './hooks/useAuth'
import { LoginPage } from './pages/LoginPage'
import { OverviewPage } from './pages/OverviewPage'
import { ForbiddenPage, NotFoundPage } from './pages/ErrorPages'
const AccountDetailPage = lazy(() => import('./pages/AccountsPage').then((m) => ({ default: m.AccountDetailPage })))
const AccountsPage = lazy(() => import('./pages/AccountsPage').then((m) => ({ default: m.AccountsPage })))
const CrmContactDetailPage = lazy(() => import('./pages/ContactsPage').then((m) => ({ default: m.ContactDetailPage })))
const CrmContactsPage = lazy(() => import('./pages/ContactsPage').then((m) => ({ default: m.ContactsPage })))
const LeadsPage = lazy(() => import('./pages/LeadsPage').then((m) => ({ default: m.LeadsPage })))
const LeadDetailPage = lazy(() => import('./pages/LeadsPage').then((m) => ({ default: m.LeadDetailPage })))
const DealDetailPage = lazy(() => import('./pages/DealsPage').then((m) => ({ default: m.DealDetailPage })))
const DealsPage = lazy(() => import('./pages/DealsPage').then((m) => ({ default: m.DealsPage })))
const TasksPage = lazy(() => import('./pages/TasksPage').then((m) => ({ default: m.TasksPage })))
const TaskDetailPage = lazy(() => import('./pages/TasksPage').then((m) => ({ default: m.TaskDetailPage })))
const TicketsPage = lazy(() => import('./pages/TicketsPage').then((m) => ({ default: m.TicketsPage })))
const TicketDetailPage = lazy(() => import('./pages/TicketsPage').then((m) => ({ default: m.TicketDetailPage })))
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
const AppearancePage = lazy(() => import('./pages/AppearancePage').then((m) => ({ default: m.AppearancePage })))

const HomePage = lazy(() => import('./site/pages/HomePage').then((m) => ({ default: m.HomePage })))
const ServicesPage = lazy(() => import('./site/pages/ServicesPage').then((m) => ({ default: m.ServicesPage })))
const ServiceDetailPage = lazy(() => import('./site/pages/ServiceDetailPage').then((m) => ({ default: m.ServiceDetailPage })))
const PricingPage = lazy(() => import('./site/pages/PricingPage').then((m) => ({ default: m.PricingPage })))
const CasesPage = lazy(() => import('./site/pages/CasesPage').then((m) => ({ default: m.CasesPage })))
const CaseDetailPage = lazy(() => import('./site/pages/CaseDetailPage').then((m) => ({ default: m.CaseDetailPage })))
const BlogPage = lazy(() => import('./site/pages/BlogPage').then((m) => ({ default: m.BlogPage })))
const ArticlePage = lazy(() => import('./site/pages/ArticlePage').then((m) => ({ default: m.ArticlePage })))
const AboutPage = lazy(() => import('./site/pages/AboutPage').then((m) => ({ default: m.AboutPage })))
const SiteContactsPage = lazy(() => import('./site/pages/ContactsPage').then((m) => ({ default: m.ContactsPage })))
const PrivacyPage = lazy(() => import('./site/pages/PrivacyPage').then((m) => ({ default: m.PrivacyPage })))
const SiteNotFoundPage = lazy(() => import('./site/pages/SiteNotFoundPage').then((m) => ({ default: m.SiteNotFoundPage })))
const SiteLayout = lazy(() => import('./site/SiteLayout').then((m) => ({ default: m.SiteLayout })))
import { RequirePermission } from './components/RequirePermission'

function FullPageSkeleton() {
  return (
    <div className="content" aria-busy="true">
      <div className="skeleton" style={{ width: 200, height: 26, marginBottom: 24 }} />
      <div className="skeleton" style={{ width: '100%', height: 220 }} />
    </div>
  )
}

/**
 * Keeps the application behind a session while leaving the public site open.
 *
 * The marketing pages are readable by anyone, so the old top-level "anonymous
 * means login only" branch cannot stay: it turned every public URL into a
 * redirect. The check moves to the one boundary that actually needs it, and it
 * remembers where the visitor was going so signing in does not dump them on the
 * overview regardless of the link they followed.
 */
function SignedIn({ children }: { children: ReactNode }) {
  const { status } = useAuth()
  const location = useLocation()
  if (status === 'loading') return <FullPageSkeleton />
  if (status === 'anonymous') {
    const from = location.pathname + location.search
    return <Navigate to="/login" replace state={{ from }} />
  }
  return <>{children}</>
}

/**
 * The two halves of the product.
 *
 * The public site owns the root of the origin: services, prices, cases, the blog
 * and the legal pages. The application lives behind `/app`, so a marketing URL
 * never collides with a CRM route and a bookmarked board keeps working after the
 * site changes.
 */
export function App() {
  return (
    <>
    <Routes>
      {/* The public site. Its routes are unprefixed and end in the catch-all,
          so an unknown URL anywhere outside /app lands on the site's 404. */}
      <Route element={<SiteLayout />}>
          <Route index element={<HomePage />} />
          <Route path="services" element={<ServicesPage />} />
          <Route path="services/:slug" element={<ServiceDetailPage />} />
          <Route path="pricing" element={<PricingPage />} />
          <Route path="cases" element={<CasesPage />} />
          <Route path="cases/:slug" element={<CaseDetailPage />} />
          <Route path="blog" element={<BlogPage />} />
          <Route path="blog/:slug" element={<ArticlePage />} />
          <Route path="about" element={<AboutPage />} />
          <Route path="contacts" element={<SiteContactsPage />} />
          <Route path="privacy" element={<PrivacyPage />} />
          <Route path="*" element={<SiteNotFoundPage />} />
        </Route>


      <Route path="/login" element={<LoginPage />} />

      <Route
        path="app"
          element={
            <SignedIn>
              <AppShell />
            </SignedIn>
          }
        >
          <Route index element={<OverviewPage />} />

          <Route
            path="/app/accounts"
            element={
              <RequirePermission permission="accounts.read">
                <AccountsPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/accounts/:id"
            element={
              <RequirePermission permission="accounts.read">
                <AccountDetailPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/contacts"
            element={
              <RequirePermission permission="contacts.read">
                <CrmContactsPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/contacts/:id"
            element={
              <RequirePermission permission="contacts.read">
                <CrmContactDetailPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/leads"
            element={
              <RequirePermission permission="leads.read">
                <LeadsPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/leads/:id"
            element={
              <RequirePermission permission="leads.read">
                <LeadDetailPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/deals"
            element={
              <RequirePermission permission="deals.read">
                <DealsPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/deals/:id"
            element={
              <RequirePermission permission="deals.read">
                <DealDetailPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/tasks"
            element={
              <RequirePermission permission="tasks.read">
                <TasksPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/tasks/:id"
            element={
              <RequirePermission permission="tasks.read">
                <TaskDetailPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/tickets"
            element={
              <RequirePermission permission="tickets.read">
                <TicketsPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/tickets/:id"
            element={
              <RequirePermission permission="tickets.read">
                <TicketDetailPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/reports"
            element={
              <RequirePermission permission="reports.read">
                <ReportsPage />
              </RequirePermission>
            }
          />

          <Route
            path="/app/imports"
            element={
              <RequirePermission permission="imports.read">
                <ImportCenterPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/imports/:id"
            element={
              <RequirePermission permission="imports.read">
                <ImportDetailPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/migrations"
            element={
              <RequirePermission permission="migrations.read">
                <MigrationsPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/integrations"
            element={
              <RequirePermission permission="integrations.read">
                <IntegrationsPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/webhooks"
            element={
              <RequirePermission permission="webhooks.manage">
                <WebhooksPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/events"
            element={
              <RequirePermission permission="events.read">
                <EventsPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/workflows"
            element={
              <RequirePermission permission="workflows.read">
                <WorkflowsPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/simulation"
            element={
              <RequirePermission permission="simulation.read">
                <SimulationPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/forecast"
            element={
              <RequirePermission permission="forecast.read">
                <ForecastPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/risk"
            element={
              <RequirePermission permission="risk.read">
                <RiskPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/jobs"
            element={
              <RequirePermission permission="jobs.read">
                <JobsPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/ai"
            element={
              <RequirePermission permission="ai.read">
                <AiPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/approvals"
            element={
              <RequirePermission permission="approvals.read">
                <ApprovalsPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/notifications"
            element={
              <RequirePermission permission="notifications.read">
                <NotificationsPage />
              </RequirePermission>
            }
          />

          <Route
            path="/app/admin/users"
            element={
              <RequirePermission permission="users.read">
                <UsersPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/admin/roles"
            element={
              <RequirePermission permission="roles.read">
                <RolesPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/admin/organizations"
            element={
              <RequirePermission permission="organizations.read">
                <OrganizationsPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/admin/teams"
            element={
              <RequirePermission permission="teams.manage">
                <TeamsPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/admin/audit"
            element={
              <RequirePermission permission="audit.read">
                <AuditPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/admin/consent"
            element={
              <RequirePermission permission="consent.read">
                <ConsentPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/admin/security"
            element={
              <RequirePermission permission="security.read">
                <SecurityPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/admin/diagnostics"
            element={
              <RequirePermission permission="logs.read">
                <DiagnosticsPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/admin/email"
            element={
              <RequirePermission permission="email.manage">
                <EmailPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/admin/telegram"
            element={<TelegramPage />}
          />
          <Route
            path="/app/admin/sso"
            element={
              <RequirePermission permission="sso.read">
                <SSOPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/admin/mcp-credentials"
            element={
              <RequirePermission permission="mcp.credentials.read">
                <MCPCredentialsPage />
              </RequirePermission>
            }
          />
          <Route
            path="/app/admin/configuration"
            element={
              <RequirePermission permission="tenant.settings.read">
                <TenantSettingsPage />
              </RequirePermission>
            }
          />

          <Route path="/app/account" element={<AccountPage />} />
          <Route path="/app/appearance" element={<AppearancePage />} />
          <Route path="/app/forbidden" element={<ForbiddenPage />} />
          <Route path="/app/*" element={<NotFoundPage />} />
        </Route>
    </Routes>
    <AiAssistantLauncher />
    </>
  )
}
