# API_INVENTORY — ForgeCRM

> Сгенерировано из `docs/api/openapi.json` (OpenAPI 3.1.0), который сам генерируется из
> живого роутера (`cmd/openapi-gen`), поэтому таблица отражает код, а не намерения.
> Baseline: HEAD `cc65d3f`.

## 1. Как читать эту таблицу

| Поле | Значение |
|---|---|
| Method / Path | HTTP-операция под префиксом `/api/v1` (если не указано иное) |
| Permission | Право RBAC, которое требуется операции (из описания OpenAPI). `—` = право не заявлено в описании (чаще публичный маршрут или маршрут, где проверка вынесена в middleware) |
| operationId | Идентификатор операции в спецификации |

**Общие свойства всех операций (проверено по коду `internal/api/server.go`):**

- **Authentication** — session-cookie `forgecrm_session` (opaque). Исключения:
  `POST /auth/login`, `POST /auth/mfa/verify`, `POST /auth/password/forgot|reset`,
  `GET /auth/oidc/*`, `POST /telegram/webhook` (свой секрет-заголовок),
  `GET /healthz|/ready|/metrics` и MCP-эндпоинты (cookie **или** `Bearer fcmcp_...`).
- **Authorization** — проверка права RBAC на каждом защищённом маршруте (`s.perm(...)`),
  плюс tenant scope по `organization_id` в доменном слое.
- **CSRF** — обязателен `X-CSRF-Token` на всех изменяющих запросах (session-группа).
- **Rate limiting** — `RateLimitMiddleware` на всём поддереве `/api/v1` (subject = client IP),
  отдельные лимитеры для `login` и `telegram/webhook`.
- **Idempotency** — middleware `idempotency.Middleware` на session- и MCP-группах;
  запрос с заголовком `Idempotency-Key` выполняется не более одного раза.
- **Tenant scope** — все данные организации изолированы по `organization_id`;
  cross-tenant доступ должен давать 404/403 (проверяется в `internal/testsupport`).
- **Ошибки** — единый формат RFC 9457 (`application/problem+json`), см. `docs/api/errors.md`.
- **Pagination** — `limit` (по умолчанию 50, максимум 200, значение выше максимума
  *clamp-ится*, не отклоняется) и `offset`.

> ⚠️ Зарегистрированный endpoint ≠ работающий endpoint. Статус покрытия — в
> `FUNCTION_INVENTORY.md` и `QUALITY_DASHBOARD.md`.

## 2. Полная таблица операций (236)

| # | Method | Path | Permission | operationId |
|---|--------|------|-----------|-------------|
| | **authentication** | | | |
| 1 | GET | `/api/v1/auth/identities` | — | getAuthIdentities |
| 2 | POST | `/api/v1/auth/login` | — | postAuthLogin |
| 3 | POST | `/api/v1/auth/logout` | — | postAuthLogout |
| 4 | GET | `/api/v1/auth/me` | — | getAuthMe |
| 5 | POST | `/api/v1/auth/mfa/confirm` | — | postAuthMfaConfirm |
| 6 | POST | `/api/v1/auth/mfa/disable` | — | postAuthMfaDisable |
| 7 | POST | `/api/v1/auth/mfa/enroll` | — | postAuthMfaEnroll |
| 8 | POST | `/api/v1/auth/mfa/verify` | — | postAuthMfaVerify |
| 9 | GET | `/api/v1/auth/oidc/callback` | — | getAuthOidcCallback |
| 10 | GET | `/api/v1/auth/oidc/providers` | — | getAuthOidcProviders |
| 11 | GET | `/api/v1/auth/oidc/start` | — | getAuthOidcStart |
| 12 | POST | `/api/v1/auth/password/change` | — | postAuthPasswordChange |
| 13 | POST | `/api/v1/auth/password/forgot` | — | postAuthPasswordForgot |
| 14 | POST | `/api/v1/auth/password/reset` | — | postAuthPasswordReset |
| 15 | GET | `/api/v1/auth/sessions` | — | getAuthSessions |
| 16 | DELETE | `/api/v1/auth/sessions/{id}` | — | deleteAuthSessionsById |
| 17 | POST | `/api/v1/auth/step-up` | — | postAuthStep-up |
| | **users-and-permissions** | | | |
| 18 | GET | `/api/v1/organizations` | organizations.read | getOrganizations |
| 19 | POST | `/api/v1/organizations` | organizations.manage | postOrganizations |
| 20 | GET | `/api/v1/roles` | roles.read | getRoles |
| 21 | POST | `/api/v1/roles` | roles.manage | postRoles |
| 22 | GET | `/api/v1/roles/permissions` | roles.read | getRolesPermissions |
| 23 | PUT | `/api/v1/roles/{id}/permissions` | roles.manage | putRolesByIdPermissions |
| 24 | GET | `/api/v1/teams` | teams.manage | getTeams |
| 25 | POST | `/api/v1/teams` | teams.manage | postTeams |
| 26 | POST | `/api/v1/teams/{id}/members` | teams.manage | postTeamsByIdMembers |
| 27 | GET | `/api/v1/users` | users.read | getUsers |
| 28 | POST | `/api/v1/users` | users.manage | postUsers |
| 29 | GET | `/api/v1/users/{id}` | users.read | getUsersById |
| 30 | PUT | `/api/v1/users/{id}` | users.manage | putUsersById |
| 31 | POST | `/api/v1/users/{id}/block` | users.manage | postUsersByIdBlock |
| 32 | POST | `/api/v1/users/{id}/roles` | users.manage | postUsersByIdRoles |
| 33 | DELETE | `/api/v1/users/{id}/roles/{roleId}` | users.manage | deleteUsersByIdRolesByRoleId |
| 34 | POST | `/api/v1/users/{id}/sessions/revoke` | users.manage | postUsersByIdSessionsRevoke |
| 35 | POST | `/api/v1/users/{id}/unblock` | users.manage | postUsersByIdUnblock |
| | **crm** | | | |
| 36 | GET | `/api/v1/accounts` | accounts.read | getAccounts |
| 37 | POST | `/api/v1/accounts` | accounts.create | postAccounts |
| 38 | POST | `/api/v1/accounts/bulk-archive` | accounts.archive | postAccountsBulk-archive |
| 39 | DELETE | `/api/v1/accounts/{id}` | accounts.archive | deleteAccountsById |
| 40 | GET | `/api/v1/accounts/{id}` | accounts.read | getAccountsById |
| 41 | PUT | `/api/v1/accounts/{id}` | accounts.update | putAccountsById |
| 42 | GET | `/api/v1/accounts/{id}/activities` | activities.read | getAccountsByIdActivities |
| 43 | GET | `/api/v1/accounts/{id}/contacts` | contacts.read | getAccountsByIdContacts |
| 44 | GET | `/api/v1/accounts/{id}/deals` | deals.read | getAccountsByIdDeals |
| 45 | GET | `/api/v1/activities` | activities.read | getActivities |
| 46 | POST | `/api/v1/activities` | activities.create | postActivities |
| 47 | GET | `/api/v1/agent/actions` | approvals.read | getAgentActions |
| 48 | GET | `/api/v1/agent/actions/{id}` | approvals.read | getAgentActionsById |
| 49 | GET | `/api/v1/agent/actions/{id}/approvals` | approvals.read | getAgentActionsByIdApprovals |
| 50 | POST | `/api/v1/agent/actions/{id}/decide` | approvals.decide | postAgentActionsByIdDecide |
| 51 | GET | `/api/v1/agent/runs` | agent.read | getAgentRuns |
| 52 | POST | `/api/v1/agent/runs` | agent.run | postAgentRuns |
| 53 | GET | `/api/v1/agent/runs/{id}` | agent.read | getAgentRunsById |
| 54 | POST | `/api/v1/agent/runs/{id}/cancel` | agent.run | postAgentRunsByIdCancel |
| 55 | POST | `/api/v1/agent/runs/{id}/tokens` | agent.run | postAgentRunsByIdTokens |
| 56 | GET | `/api/v1/comments` | contacts.read | getComments |
| 57 | POST | `/api/v1/comments` | comments.create | postComments |
| 58 | GET | `/api/v1/consent` | consent.read | getConsent |
| 59 | POST | `/api/v1/consent` | consent.manage | postConsent |
| 60 | GET | `/api/v1/consent/current` | consent.read | getConsentCurrent |
| 61 | GET | `/api/v1/consent/decide` | consent.read | getConsentDecide |
| 62 | GET | `/api/v1/consent/history` | consent.read | getConsentHistory |
| 63 | POST | `/api/v1/consent/withdraw` | consent.manage | postConsentWithdraw |
| 64 | GET | `/api/v1/contacts` | contacts.read | getContacts |
| 65 | POST | `/api/v1/contacts` | contacts.create | postContacts |
| 66 | POST | `/api/v1/contacts/bulk-archive` | contacts.archive | postContactsBulk-archive |
| 67 | DELETE | `/api/v1/contacts/{id}` | contacts.archive | deleteContactsById |
| 68 | GET | `/api/v1/contacts/{id}` | contacts.read | getContactsById |
| 69 | PUT | `/api/v1/contacts/{id}` | contacts.update | putContactsById |
| 70 | GET | `/api/v1/custom-fields` | contacts.read | getCustom-fields |
| 71 | POST | `/api/v1/custom-fields` | custom_fields.manage | postCustom-fields |
| 72 | GET | `/api/v1/deals` | deals.read | getDeals |
| 73 | POST | `/api/v1/deals` | deals.create | postDeals |
| 74 | POST | `/api/v1/deals/bulk-archive` | deals.archive | postDealsBulk-archive |
| 75 | DELETE | `/api/v1/deals/{id}` | deals.archive | deleteDealsById |
| 76 | GET | `/api/v1/deals/{id}` | deals.read | getDealsById |
| 77 | PUT | `/api/v1/deals/{id}` | deals.update | putDealsById |
| 78 | GET | `/api/v1/deals/{id}/history` | deals.read | getDealsByIdHistory |
| 79 | POST | `/api/v1/deals/{id}/stage` | deals.change_stage | postDealsByIdStage |
| 80 | GET | `/api/v1/documents` | documents.read | getDocuments |
| 81 | POST | `/api/v1/documents` | documents.create | postDocuments |
| 82 | DELETE | `/api/v1/documents/{id}` | documents.archive | deleteDocumentsById |
| 83 | GET | `/api/v1/documents/{id}` | documents.read | getDocumentsById |
| 84 | GET | `/api/v1/documents/{id}/download` | documents.read | getDocumentsByIdDownload |
| 85 | GET | `/api/v1/documents/{id}/url` | documents.read | getDocumentsByIdUrl |
| 86 | GET | `/api/v1/forecast/catalog` | — | getForecastCatalog |
| 87 | GET | `/api/v1/forecast/runs` | forecast.read | getForecastRuns |
| 88 | POST | `/api/v1/forecast/runs` | forecast.run | postForecastRuns |
| 89 | GET | `/api/v1/forecast/runs/{id}` | forecast.read | getForecastRunsById |
| 90 | GET | `/api/v1/leads` | leads.read | getLeads |
| 91 | POST | `/api/v1/leads` | leads.create | postLeads |
| 92 | GET | `/api/v1/leads/{id}` | leads.read | getLeadsById |
| 93 | PUT | `/api/v1/leads/{id}` | leads.update | putLeadsById |
| 94 | POST | `/api/v1/leads/{id}/convert` | leads.convert | postLeadsByIdConvert |
| 95 | GET | `/api/v1/pipelines` | pipelines.read | getPipelines |
| 96 | POST | `/api/v1/pipelines` | pipelines.manage | postPipelines |
| 97 | GET | `/api/v1/pipelines/{id}/report` | pipelines.read | getPipelinesByIdReport |
| 98 | GET | `/api/v1/reports/sales-summary` | reports.read | getReportsSales-summary |
| 99 | GET | `/api/v1/reports/support-summary` | reports.read | getReportsSupport-summary |
| 100 | POST | `/api/v1/risk/scan` | risk.read | postRiskScan |
| 101 | GET | `/api/v1/risk/signals` | risk.read | getRiskSignals |
| 102 | GET | `/api/v1/risk/signals/{id}` | risk.read | getRiskSignalsById |
| 103 | POST | `/api/v1/risk/signals/{id}/status` | risk.read | postRiskSignalsByIdStatus |
| 104 | GET | `/api/v1/risk/thresholds` | risk.read | getRiskThresholds |
| 105 | PUT | `/api/v1/risk/thresholds` | risk.manage | putRiskThresholds |
| 106 | GET | `/api/v1/search` | contacts.read | getSearch |
| 107 | GET | `/api/v1/security-events` | security.read | getSecurity-events |
| 108 | GET | `/api/v1/settings` | — | getSettings |
| 109 | GET | `/api/v1/settings/catalog` | — | getSettingsCatalog |
| 110 | DELETE | `/api/v1/settings/{key}` | — | deleteSettingsByKey |
| 111 | GET | `/api/v1/settings/{key}` | — | getSettingsByKey |
| 112 | PUT | `/api/v1/settings/{key}` | — | putSettingsByKey |
| 113 | GET | `/api/v1/settings/{key}/history` | — | getSettingsByKeyHistory |
| 114 | GET | `/api/v1/simulation/catalog` | — | getSimulationCatalog |
| 115 | POST | `/api/v1/simulation/preview` | simulation.run | postSimulationPreview |
| 116 | GET | `/api/v1/simulation/scenarios` | simulation.read | getSimulationScenarios |
| 117 | POST | `/api/v1/simulation/scenarios` | simulation.manage | postSimulationScenarios |
| 118 | DELETE | `/api/v1/simulation/scenarios/{id}` | simulation.manage | deleteSimulationScenariosById |
| 119 | GET | `/api/v1/simulation/scenarios/{id}` | simulation.read | getSimulationScenariosById |
| 120 | GET | `/api/v1/simulation/scenarios/{id}/runs` | simulation.read | getSimulationScenariosByIdRuns |
| 121 | POST | `/api/v1/simulation/scenarios/{id}/runs` | simulation.run | postSimulationScenariosByIdRuns |
| 122 | GET | `/api/v1/tags` | contacts.read | getTags |
| 123 | POST | `/api/v1/tags` | tags.manage | postTags |
| 124 | GET | `/api/v1/tags/attached` | contacts.read | getTagsAttached |
| 125 | POST | `/api/v1/tags/{id}/attach` | tags.manage | postTagsByIdAttach |
| 126 | GET | `/api/v1/tasks` | tasks.read | getTasks |
| 127 | POST | `/api/v1/tasks` | tasks.create | postTasks |
| 128 | GET | `/api/v1/tasks/{id}` | tasks.read | getTasksById |
| 129 | PUT | `/api/v1/tasks/{id}` | tasks.update | putTasksById |
| 130 | POST | `/api/v1/telegram/webhook` | — | postTelegramWebhook |
| 131 | GET | `/api/v1/tickets` | tickets.read | getTickets |
| 132 | POST | `/api/v1/tickets` | tickets.create | postTickets |
| 133 | GET | `/api/v1/tickets/{id}` | tickets.read | getTicketsById |
| 134 | PUT | `/api/v1/tickets/{id}` | tickets.update | putTicketsById |
| 135 | GET | `/api/v1/views` | views.read | getViews |
| 136 | POST | `/api/v1/views` | views.manage | postViews |
| 137 | DELETE | `/api/v1/views/{id}` | views.manage | deleteViewsById |
| 138 | GET | `/api/v1/views/{id}` | views.read | getViewsById |
| 139 | PUT | `/api/v1/views/{id}` | views.manage | putViewsById |
| 140 | GET | `/api/v1/workflow-runs/{id}` | workflows.read | getWorkflow-runsById |
| 141 | GET | `/api/v1/workflow-runs/{id}/steps` | workflows.read | getWorkflow-runsByIdSteps |
| 142 | GET | `/api/v1/workflow-templates` | workflows.read | getWorkflow-templates |
| 143 | GET | `/api/v1/workflow-templates/{id}` | workflows.read | getWorkflow-templatesById |
| 144 | POST | `/api/v1/workflow-templates/{id}/create` | workflows.manage | postWorkflow-templatesByIdCreate |
| 145 | GET | `/api/v1/workflows` | workflows.read | getWorkflows |
| 146 | POST | `/api/v1/workflows` | workflows.manage | postWorkflows |
| 147 | POST | `/api/v1/workflows/import` | workflows.manage | postWorkflowsImport |
| 148 | DELETE | `/api/v1/workflows/{id}` | workflows.manage | deleteWorkflowsById |
| 149 | GET | `/api/v1/workflows/{id}` | workflows.read | getWorkflowsById |
| 150 | PUT | `/api/v1/workflows/{id}` | workflows.manage | putWorkflowsById |
| 151 | PUT | `/api/v1/workflows/{id}/enabled` | workflows.manage | putWorkflowsByIdEnabled |
| 152 | GET | `/api/v1/workflows/{id}/export` | workflows.read | getWorkflowsByIdExport |
| 153 | POST | `/api/v1/workflows/{id}/run` | workflows.run | postWorkflowsByIdRun |
| 154 | GET | `/api/v1/workflows/{id}/runs` | workflows.read | getWorkflowsByIdRuns |
| | **import-and-export** | | | |
| 155 | POST | `/api/v1/exports` | exports.execute | postExports |
| 156 | GET | `/api/v1/imports` | imports.read | getImports |
| 157 | POST | `/api/v1/imports` | imports.create | postImports |
| 158 | GET | `/api/v1/imports/fields` | imports.read | getImportsFields |
| 159 | GET | `/api/v1/imports/{id}` | imports.read | getImportsById |
| 160 | POST | `/api/v1/imports/{id}/analyze` | imports.read | postImportsByIdAnalyze |
| 161 | POST | `/api/v1/imports/{id}/cancel` | imports.execute | postImportsByIdCancel |
| 162 | POST | `/api/v1/imports/{id}/dry-run` | imports.execute | postImportsByIdDry-run |
| 163 | GET | `/api/v1/imports/{id}/errors` | imports.read | getImportsByIdErrors |
| 164 | POST | `/api/v1/imports/{id}/execute` | imports.execute | postImportsByIdExecute |
| 165 | GET | `/api/v1/imports/{id}/preview` | imports.read | getImportsByIdPreview |
| 166 | GET | `/api/v1/imports/{id}/report` | imports.read | getImportsByIdReport |
| 167 | POST | `/api/v1/imports/{id}/upload` | imports.create | postImportsByIdUpload |
| 168 | POST | `/api/v1/imports/{id}/validate` | imports.execute | postImportsByIdValidate |
| | **integrations** | | | |
| 169 | GET | `/api/v1/integrations` | integrations.read | getIntegrations |
| 170 | POST | `/api/v1/integrations` | integrations.configure | postIntegrations |
| 171 | GET | `/api/v1/integrations/kinds` | integrations.read | getIntegrationsKinds |
| 172 | DELETE | `/api/v1/integrations/{id}` | integrations.configure | deleteIntegrationsById |
| 173 | GET | `/api/v1/integrations/{id}` | integrations.read | getIntegrationsById |
| 174 | GET | `/api/v1/integrations/{id}/runs` | integrations.read | getIntegrationsByIdRuns |
| 175 | GET | `/api/v1/integrations/{id}/status` | integrations.read | getIntegrationsByIdStatus |
| 176 | PUT | `/api/v1/integrations/{id}/status` | integrations.configure | putIntegrationsByIdStatus |
| 177 | POST | `/api/v1/integrations/{id}/test` | integrations.configure | postIntegrationsByIdTest |
| | **events** | | | |
| 178 | GET | `/api/v1/events` | events.read | getEvents |
| 179 | GET | `/api/v1/webhooks` | webhooks.manage | getWebhooks |
| 180 | POST | `/api/v1/webhooks` | webhooks.manage | postWebhooks |
| 181 | GET | `/api/v1/webhooks/{id}/deliveries` | webhooks.manage | getWebhooksByIdDeliveries |
| 182 | POST | `/api/v1/webhooks/{id}/deliveries/{deliveryId}/replay` | webhooks.manage | postWebhooksByIdDeliveriesByDeliveryIdReplay |
| 183 | PUT | `/api/v1/webhooks/{id}/enabled` | webhooks.manage | putWebhooksByIdEnabled |
| 184 | POST | `/api/v1/webhooks/{id}/test` | webhooks.manage | postWebhooksByIdTest |
| | **jobs** | | | |
| 185 | GET | `/api/v1/jobs` | jobs.read | getJobs |
| 186 | GET | `/api/v1/jobs/stats` | jobs.read | getJobsStats |
| 187 | GET | `/api/v1/jobs/{id}` | jobs.read | getJobsById |
| 188 | POST | `/api/v1/jobs/{id}/cancel` | jobs.manage | postJobsByIdCancel |
| 189 | POST | `/api/v1/jobs/{id}/retry` | jobs.manage | postJobsByIdRetry |
| | **ai-and-mcp** | | | |
| 190 | POST | `/api/v1/ai/assist/{id}` | — | postAiAssistById |
| 191 | GET | `/api/v1/ai/assist/{id}/context` | ai.read | getAiAssistByIdContext |
| 192 | GET | `/api/v1/ai/providers` | ai.read | getAiProviders |
| 193 | GET | `/api/v1/ai/status` | ai.read | getAiStatus |
| 194 | POST | `/api/v1/ai/stream/assist/{id}` | — | postAiStreamAssistById |
| 195 | POST | `/api/v1/ai/stream/chat` | — | postAiStreamChat |
| 196 | GET | `/api/v1/ai/tasks` | ai.read | getAiTasks |
| 197 | POST | `/api/v1/ai/tasks` | — | postAiTasks |
| 198 | POST | `/api/v1/mcp` | — | postMcp |
| 199 | POST | `/api/v1/mcp/call` | — | postMcpCall |
| 200 | GET | `/api/v1/mcp/credentials` | — | getMcpCredentials |
| 201 | POST | `/api/v1/mcp/credentials` | — | postMcpCredentials |
| 202 | DELETE | `/api/v1/mcp/credentials/{id}` | — | deleteMcpCredentialsById |
| 203 | POST | `/api/v1/mcp/credentials/{id}/rotate` | — | postMcpCredentialsByIdRotate |
| 204 | GET | `/api/v1/mcp/tools` | ai.read | getMcpTools |
| | **migrations** | | | |
| 205 | GET | `/api/v1/migrations` | migrations.read | getMigrations |
| 206 | POST | `/api/v1/migrations` | migrations.manage | postMigrations |
| 207 | GET | `/api/v1/migrations/{id}` | migrations.read | getMigrationsById |
| 208 | POST | `/api/v1/migrations/{id}/discover` | migrations.read | postMigrationsByIdDiscover |
| 209 | GET | `/api/v1/migrations/{id}/reconciliation` | migrations.read | getMigrationsByIdReconciliation |
| 210 | POST | `/api/v1/migrations/{id}/reconciliation` | migrations.read | postMigrationsByIdReconciliation |
| | **system** | | | |
| 211 | GET | `/api/v1/system/capabilities` | — | getSystemCapabilities |
| 212 | GET | `/api/v1/system/info` | — | getSystemInfo |
| 213 | POST | `/csp-report` | — | postCsp-report |
| 214 | GET | `/health` | — | getHealth |
| 215 | GET | `/metrics` | — | getMetrics |
| 216 | GET | `/ready` | — | getReady |
| | **notifications** | | | |
| 217 | GET | `/api/v1/notifications` | notifications.read | getNotifications |
| 218 | POST | `/api/v1/notifications/{id}/read` | notifications.read | postNotificationsByIdRead |
| | **audit-and-diagnostics** | | | |
| 219 | GET | `/api/v1/admin/diagnostics` | metrics.read | getAdminDiagnostics |
| 220 | GET | `/api/v1/admin/email/log` | notifications.read | getAdminEmailLog |
| 221 | POST | `/api/v1/admin/email/send` | email.manage | postAdminEmailSend |
| 222 | GET | `/api/v1/admin/email/status` | email.manage | getAdminEmailStatus |
| 223 | POST | `/api/v1/admin/email/test` | email.manage | postAdminEmailTest |
| 224 | GET | `/api/v1/admin/logs` | logs.read | getAdminLogs |
| 225 | GET | `/api/v1/admin/metrics` | metrics.read | getAdminMetrics |
| 226 | GET | `/api/v1/admin/settings` | system.settings | getAdminSettings |
| 227 | PUT | `/api/v1/admin/settings` | system.settings | putAdminSettings |
| 228 | GET | `/api/v1/admin/sso/providers` | sso.read | getAdminSsoProviders |
| 229 | POST | `/api/v1/admin/sso/providers` | sso.manage | postAdminSsoProviders |
| 230 | DELETE | `/api/v1/admin/sso/providers/{id}` | sso.manage | deleteAdminSsoProvidersById |
| 231 | PUT | `/api/v1/admin/sso/providers/{id}` | sso.manage | putAdminSsoProvidersById |
| 232 | DELETE | `/api/v1/admin/telegram/link` | — | deleteAdminTelegramLink |
| 233 | POST | `/api/v1/admin/telegram/link-code` | — | postAdminTelegramLink-code |
| 234 | GET | `/api/v1/admin/telegram/status` | — | getAdminTelegramStatus |
| 235 | GET | `/api/v1/audit` | audit.read | getAudit |
| 236 | GET | `/api/v1/audit/verify` | audit.read | getAuditVerify |
