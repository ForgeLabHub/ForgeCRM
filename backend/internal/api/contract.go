package api

import (
	"reflect"
	"time"

	"github.com/geff1778/forgecrm/internal/ai"
	"github.com/geff1778/forgecrm/internal/consent"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/events"
	"github.com/geff1778/forgecrm/internal/forecast"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/importx"
	"github.com/geff1778/forgecrm/internal/integrations"
	"github.com/geff1778/forgecrm/internal/jobs"
	"github.com/geff1778/forgecrm/internal/mcpcred"
	"github.com/geff1778/forgecrm/internal/migratex"
	"github.com/geff1778/forgecrm/internal/openapi"
	"github.com/geff1778/forgecrm/internal/tenantconfig"
	"github.com/geff1778/forgecrm/internal/views"
	"github.com/geff1778/forgecrm/internal/workflow"
)

// APIContract declares, for every operation, the real Go types it decodes and
// returns.
//
// It lives in package api rather than in internal/openapi because the request DTOs
// are unexported types in this package: an external package cannot name them, and a
// schema derived from a re-declared copy would describe a different type. Naming the
// real type is the whole point — it is what keeps the specification from drifting
// away from the handler.
//
// Every entry is checked at generation time. A route the contract names that the
// router does not serve fails generation, and a request body or response the
// contract omits is simply left unspecified rather than guessed. The check runs in
// both directions, so the table cannot rot silently.
//
// Nothing here restates a shape. Field names, types, nullability and the required
// set on a response all come from the struct. Only the required set on a *request*
// is declared, because that is a fact about validation, and it is declared next to
// the validation it mirrors.
func APIContract() openapi.Contract {
	c := openapi.Contract{
		RequestBodies:    map[string]reflect.Type{},
		Responses:        map[string]reflect.Type{},
		RequiredBody:     map[string][]string{},
		BodyNames:        map[string]string{},
		BodyDescriptions: map[string]string{},
		QueryParams:      map[string][]openapi.QueryParam{},
		SortColumns:      map[string][]string{},
		TypeNames:        map[reflect.Type]string{},
		SuccessStatus:    map[string]int{},
		StreamingSuccess: map[string]openapi.StreamingResponse{},
		TextSuccess:      map[string]openapi.TextResponse{},
		ExtraResponses:   map[string]map[int]string{},
	}

	// The Prometheus scrape endpoint returns text in the exposition format, not
	// JSON. Without the declaration the generator would document an
	// application/json object, which is exactly what a scraper would fail to parse.
	c.TextSuccess["GET /metrics"] = openapi.TextResponse{
		MediaTypes: []string{"text/plain"},
		Description: "Metrics in the Prometheus text exposition format (version 0.0.4), " +
			"one `# HELP`/`# TYPE` block per metric. Reachable without a session, so it " +
			"requires `FORGECRM_METRICS_TOKEN` in the `X-Metrics-Token` header; with no " +
			"token configured the server answers 503 `disabled`. Unlabelled by tenant: a " +
			"tenant id or target URL in a label would multiply the series without bound and " +
			"leak a customer's endpoint into a monitoring system.",
	}
	// The route is anonymous, so the generator does not infer a 401; the token gate
	// is the reason it can refuse a valid request, and the 503 is the opt-in refusal
	// when no token is configured. Both are stated because a scraper needs to know
	// which one means "misconfigured" rather than "broken".
	c.ExtraResponses["GET /metrics"] = map[int]string{
		401: "The `X-Metrics-Token` header is missing or does not match `FORGECRM_METRICS_TOKEN`.",
		503: "Metrics scraping is not enabled: no `FORGECRM_METRICS_TOKEN` is configured. Set it to enable the endpoint.",
	}

	// --- Authentication ------------------------------------------------------
	c.RequestBodies["POST /api/v1/auth/login"] = reflect.TypeOf(loginRequest{})
	c.RequiredBody["LoginRequest"] = []string{"email", "password"}
	c.Responses["POST /api/v1/auth/login"] = reflect.TypeOf(loginResponse{})

	c.RequestBodies["POST /api/v1/auth/mfa/verify"] = reflect.TypeOf(mfaVerifyRequest{})
	c.RequiredBody["MfaVerifyRequest"] = []string{"ticket", "code"}
	c.Responses["POST /api/v1/auth/mfa/verify"] = reflect.TypeOf(loginResponse{})

	c.RequestBodies["POST /api/v1/auth/mfa/confirm"] = reflect.TypeOf(mfaConfirmRequest{})
	c.RequiredBody["MfaConfirmRequest"] = []string{"code"}

	c.RequestBodies["POST /api/v1/auth/password/forgot"] = reflect.TypeOf(passwordForgotRequest{})
	c.RequiredBody["PasswordForgotRequest"] = []string{"email"}

	c.RequestBodies["POST /api/v1/auth/password/reset"] = reflect.TypeOf(passwordResetRequest{})
	c.RequiredBody["PasswordResetRequest"] = []string{"token", "new_password"}

	c.RequestBodies["POST /api/v1/auth/password/change"] = reflect.TypeOf(passwordChangeRequest{})
	c.RequiredBody["PasswordChangeRequest"] = []string{"current_password", "new_password"}

	c.RequestBodies["POST /api/v1/auth/step-up"] = reflect.TypeOf(stepUpRequest{})
	c.Responses["POST /api/v1/auth/step-up"] = reflect.TypeOf(stepUpResponse{})

	c.Responses["GET /api/v1/auth/me"] = reflect.TypeOf(meResponse{})

	// Single sign-on. The start and callback are browser navigations, so they
	// answer a redirect rather than a JSON body; only the provider list and the
	// administration routes carry a schema.
	c.Responses["GET /api/v1/auth/oidc/providers"] = reflect.TypeOf(ssoProvidersResponse{})
	c.Responses["GET /api/v1/auth/identities"] = reflect.TypeOf(httpx.ListResponse[map[string]any]{})
	c.RequestBodies["POST /api/v1/admin/sso/providers"] = reflect.TypeOf(oidcProviderInput{})
	c.RequiredBody["OidcProviderInput"] = []string{"key", "display_name", "issuer", "client_id"}
	c.Responses["GET /api/v1/admin/sso/providers"] = reflect.TypeOf(oidcProviderListResponse{})
	c.Responses["POST /api/v1/admin/sso/providers"] = reflect.TypeOf(oidcProviderResponse{})
	c.RequestBodies["PUT /api/v1/admin/sso/providers/{id}"] = reflect.TypeOf(oidcProviderInput{})
	c.Responses["PUT /api/v1/admin/sso/providers/{id}"] = reflect.TypeOf(oidcProviderResponse{})

	// --- Users, roles, teams, organizations ---------------------------------
	c.RequestBodies["POST /api/v1/users"] = reflect.TypeOf(inviteUserRequest{})
	c.RequiredBody["InviteUserRequest"] = []string{"email", "display_name"}
	c.Responses["GET /api/v1/users"] = reflect.TypeOf(httpx.ListResponse[userSummary]{})
	c.Responses["GET /api/v1/users/{id}"] = reflect.TypeOf(userSummary{})
	c.Responses["POST /api/v1/users"] = reflect.TypeOf(inviteResponse{})

	c.RequestBodies["PUT /api/v1/users/{id}"] = reflect.TypeOf(updateUserRequest{})
	c.RequestBodies["POST /api/v1/users/{id}/roles"] = reflect.TypeOf(assignRoleRequest{})
	c.RequiredBody["AssignRoleRequest"] = []string{"role_id"}

	c.RequestBodies["POST /api/v1/roles"] = reflect.TypeOf(createRoleRequest{})
	c.RequiredBody["CreateRoleRequest"] = []string{"key", "name"}
	c.Responses["POST /api/v1/roles"] = reflect.TypeOf(roleCreatedResponse{})
	c.RequestBodies["PUT /api/v1/roles/{id}/permissions"] = reflect.TypeOf(setRolePermissionsRequest{})
	c.RequiredBody["SetRolePermissionsRequest"] = []string{"permissions"}

	c.RequestBodies["POST /api/v1/organizations"] = reflect.TypeOf(createOrganizationRequest{})
	c.RequiredBody["CreateOrganizationRequest"] = []string{"name"}
	c.Responses["POST /api/v1/organizations"] = reflect.TypeOf(organizationCreatedResponse{})

	c.RequestBodies["POST /api/v1/teams"] = reflect.TypeOf(createTeamRequest{})
	c.RequiredBody["CreateTeamRequest"] = []string{"name"}
	c.Responses["POST /api/v1/teams"] = reflect.TypeOf(teamCreatedResponse{})
	c.RequestBodies["POST /api/v1/teams/{id}/members"] = reflect.TypeOf(addTeamMemberRequest{})
	c.RequiredBody["AddTeamMemberRequest"] = []string{"user_id"}

	c.RequestBodies["PUT /api/v1/admin/settings"] = reflect.TypeOf(updateSettingRequest{})
	c.QueryParams["PUT /api/v1/admin/settings"] = []openapi.QueryParam{{
		Name: "key", Required: true,
		Schema:      map[string]any{"type": "string"},
		Description: "The setting to write. Required; a request without it is rejected.",
	}}

	// --- CRM: accounts ------------------------------------------------------
	c.RequestBodies["POST /api/v1/accounts"] = reflect.TypeOf(crm.AccountInput{})
	c.RequiredBody["AccountInput"] = []string{"name"}
	c.RequestBodies["PUT /api/v1/accounts/{id}"] = reflect.TypeOf(crm.AccountInput{})
	c.Responses["GET /api/v1/accounts"] = reflect.TypeOf(httpx.ListResponse[crm.Account]{})
	c.Responses["GET /api/v1/accounts/{id}"] = reflect.TypeOf(crm.Account{})
	c.Responses["POST /api/v1/accounts"] = reflect.TypeOf(crm.Account{})
	c.Responses["PUT /api/v1/accounts/{id}"] = reflect.TypeOf(crm.Account{})
	c.Responses["GET /api/v1/accounts/{id}/contacts"] = reflect.TypeOf(httpx.ListResponse[crm.Contact]{})
	c.Responses["GET /api/v1/accounts/{id}/deals"] = reflect.TypeOf(httpx.ListResponse[crm.Deal]{})
	c.Responses["GET /api/v1/accounts/{id}/activities"] = reflect.TypeOf(httpx.ListResponse[crm.Activity]{})

	// Bulk archive. The response type is declared in this package, so it is named
	// directly rather than through crm: it is a transport shape, not a domain one.
	for _, entity := range []string{"accounts", "contacts", "deals"} {
		key := "POST /api/v1/" + entity + "/bulk-archive"
		c.RequestBodies[key] = reflect.TypeOf(crm.BulkArchiveInput{})
		c.Responses[key] = reflect.TypeOf(bulkArchiveResponse{})
	}
	// `ids` is required in the sense that an empty list is refused, and Validate
	// refuses it unconditionally rather than only for a zero value, so it belongs
	// in the required set.
	c.RequiredBody["BulkArchiveInput"] = []string{"ids"}
	// Bulk archive creates nothing. It reports what happened to each requested
	// record, so 200 is the honest status and the generator's POST-means-201
	// convention would misdescribe it.
	for _, entity := range []string{"accounts", "contacts", "deals"} {
		c.SuccessStatus["POST /api/v1/"+entity+"/bulk-archive"] = 200
	}
	// Logout returns an acknowledgement rather than creating a resource. The
	// handler already answers 200; the specification said 201 until this declared
	// the code, which is the kind of drift the generator exists to catch.
	c.SuccessStatus["POST /api/v1/auth/logout"] = 200
	c.SortColumns["GET /api/v1/accounts"] = []string{"name", "created_at", "updated_at", "revenue", "employees"}

	// --- CRM: contacts ------------------------------------------------------
	c.RequestBodies["POST /api/v1/contacts"] = reflect.TypeOf(crm.ContactInput{})
	// A contact needs a name or an email, not a specific field, so the rule is not
	// expressible as a required list. It is stated in the description instead.
	c.RequestBodies["PUT /api/v1/contacts/{id}"] = reflect.TypeOf(crm.ContactInput{})
	c.Responses["GET /api/v1/contacts"] = reflect.TypeOf(httpx.ListResponse[crm.Contact]{})
	c.Responses["GET /api/v1/contacts/{id}"] = reflect.TypeOf(crm.Contact{})
	c.Responses["POST /api/v1/contacts"] = reflect.TypeOf(crm.Contact{})
	c.Responses["PUT /api/v1/contacts/{id}"] = reflect.TypeOf(crm.Contact{})
	c.SortColumns["GET /api/v1/contacts"] = []string{"first_name", "last_name", "email", "created_at", "updated_at"}

	// --- CRM: leads ---------------------------------------------------------
	c.RequestBodies["POST /api/v1/leads"] = reflect.TypeOf(crm.LeadInput{})
	c.RequestBodies["PUT /api/v1/leads/{id}"] = reflect.TypeOf(crm.LeadInput{})
	c.Responses["GET /api/v1/leads"] = reflect.TypeOf(httpx.ListResponse[crm.Lead]{})
	c.Responses["GET /api/v1/leads/{id}"] = reflect.TypeOf(crm.Lead{})
	c.Responses["POST /api/v1/leads"] = reflect.TypeOf(crm.Lead{})
	c.Responses["PUT /api/v1/leads/{id}"] = reflect.TypeOf(crm.Lead{})
	c.Responses["POST /api/v1/leads/{id}/convert"] = reflect.TypeOf(leadConversionResponse{})
	c.SortColumns["GET /api/v1/leads"] = []string{"created_at", "updated_at", "status", "score"}

	// --- CRM: deals and pipelines -------------------------------------------
	c.RequestBodies["POST /api/v1/deals"] = reflect.TypeOf(crm.DealInput{})
	c.RequiredBody["DealInput"] = []string{"title", "pipeline_id", "stage_id"}
	c.RequestBodies["PUT /api/v1/deals/{id}"] = reflect.TypeOf(crm.DealInput{})
	c.RequestBodies["POST /api/v1/deals/{id}/stage"] = reflect.TypeOf(moveStageRequest{})
	c.RequiredBody["MoveStageRequest"] = []string{"stage_id"}
	c.Responses["GET /api/v1/deals"] = reflect.TypeOf(httpx.ListResponse[crm.Deal]{})
	c.Responses["GET /api/v1/deals/{id}"] = reflect.TypeOf(crm.Deal{})
	c.Responses["POST /api/v1/deals"] = reflect.TypeOf(crm.Deal{})
	c.Responses["PUT /api/v1/deals/{id}"] = reflect.TypeOf(crm.Deal{})
	c.Responses["POST /api/v1/deals/{id}/stage"] = reflect.TypeOf(crm.Deal{})
	c.SortColumns["GET /api/v1/deals"] = []string{"title", "amount", "created_at", "updated_at", "expected_close_date"}

	c.RequestBodies["POST /api/v1/pipelines"] = reflect.TypeOf(createPipelineRequest{})
	c.RequiredBody["CreatePipelineRequest"] = []string{"name", "stages"}
	c.Responses["GET /api/v1/pipelines"] = reflect.TypeOf(pipelineListResponse{})
	c.Responses["POST /api/v1/pipelines"] = reflect.TypeOf(crm.Pipeline{})
	c.Responses["GET /api/v1/pipelines/{id}/report"] = reflect.TypeOf(crm.PipelineReport{})

	// --- CRM: tasks, activities, tickets ------------------------------------
	c.RequestBodies["POST /api/v1/tasks"] = reflect.TypeOf(crm.TaskInput{})
	c.RequiredBody["TaskInput"] = []string{"title"}
	c.RequestBodies["PUT /api/v1/tasks/{id}"] = reflect.TypeOf(crm.TaskInput{})
	c.Responses["GET /api/v1/tasks"] = reflect.TypeOf(httpx.ListResponse[crm.Task]{})
	c.Responses["POST /api/v1/tasks"] = reflect.TypeOf(crm.Task{})
	c.Responses["PUT /api/v1/tasks/{id}"] = reflect.TypeOf(crm.Task{})
	c.SortColumns["GET /api/v1/tasks"] = []string{"title", "due_at", "status", "priority", "created_at"}

	c.RequestBodies["POST /api/v1/activities"] = reflect.TypeOf(crm.ActivityInput{})
	// An activity must reference an account, contact, deal or ticket. Which one is
	// the caller's choice, so it is a description, not a required list.
	c.Responses["GET /api/v1/activities"] = reflect.TypeOf(httpx.ListResponse[crm.Activity]{})
	c.Responses["POST /api/v1/activities"] = reflect.TypeOf(crm.Activity{})
	c.SortColumns["GET /api/v1/activities"] = []string{"occurred_at", "created_at"}

	c.RequestBodies["POST /api/v1/tickets"] = reflect.TypeOf(crm.TicketInput{})
	c.RequiredBody["TicketInput"] = []string{"subject"}
	c.RequestBodies["PUT /api/v1/tickets/{id}"] = reflect.TypeOf(crm.TicketInput{})
	c.Responses["GET /api/v1/tickets"] = reflect.TypeOf(httpx.ListResponse[crm.Ticket]{})
	c.Responses["GET /api/v1/tickets/{id}"] = reflect.TypeOf(crm.Ticket{})
	c.Responses["POST /api/v1/tickets"] = reflect.TypeOf(crm.Ticket{})
	c.Responses["PUT /api/v1/tickets/{id}"] = reflect.TypeOf(crm.Ticket{})
	c.SortColumns["GET /api/v1/tickets"] = []string{"subject", "status", "priority", "created_at"}

	// --- CRM: comments, tags, custom fields ---------------------------------
	c.RequestBodies["POST /api/v1/comments"] = reflect.TypeOf(createCommentRequest{})
	c.RequiredBody["CreateCommentRequest"] = []string{"entity_type", "entity_id", "body"}
	c.Responses["GET /api/v1/comments"] = reflect.TypeOf(httpx.ListResponse[crm.Comment]{})
	c.Responses["POST /api/v1/comments"] = reflect.TypeOf(crm.Comment{})

	c.RequestBodies["POST /api/v1/tags"] = reflect.TypeOf(createTagRequest{})
	c.RequiredBody["CreateTagRequest"] = []string{"name"}
	c.RequestBodies["POST /api/v1/tags/{id}/attach"] = reflect.TypeOf(attachTagRequest{})
	c.RequiredBody["AttachTagRequest"] = []string{"entity_type", "entity_id"}
	c.Responses["GET /api/v1/tags"] = reflect.TypeOf(tagListResponse{})
	c.Responses["GET /api/v1/tags/attached"] = reflect.TypeOf(tagListResponse{})
	c.Responses["POST /api/v1/tags"] = reflect.TypeOf(crm.Tag{})

	c.RequestBodies["POST /api/v1/custom-fields"] = reflect.TypeOf(crm.CustomFieldDefinition{})
	c.RequiredBody["CustomFieldDefinitionInput"] = []string{"entity_type", "key", "label", "field_type"}
	// CustomFieldDefinition is also the response type, and its response form carries
	// the id and timestamps the server assigns. One component cannot require
	// `label` for a request and require `id` for a response, so the request gets its
	// own component name.
	c.BodyNames["POST /api/v1/custom-fields"] = "CustomFieldDefinitionInput"
	c.Responses["POST /api/v1/custom-fields"] = reflect.TypeOf(crm.CustomFieldDefinition{})

	// --- Documents ----------------------------------------------------------
	// The upload is multipart/form-data, not JSON, so its body is described in the
	// operation rather than derived from a DTO. The responses are the document shape.
	c.Responses["GET /api/v1/documents"] = reflect.TypeOf(httpx.ListResponse[documentResponse]{})
	c.Responses["GET /api/v1/documents/{id}"] = reflect.TypeOf(documentResponse{})
	c.Responses["POST /api/v1/documents"] = reflect.TypeOf(documentResponse{})
	c.SuccessStatus["POST /api/v1/documents"] = 201
	c.BodyDescriptions["POST /api/v1/documents"] = "A multipart/form-data upload. The file part is named file; the optional parts are title and one of account_id, contact_id, deal_id, ticket_id, lead_id or task_id. At most one entity id may be set."
	c.Responses["GET /api/v1/documents/{id}/url"] = reflect.TypeOf(documentURLResponse{})

	// --- Reports and search -------------------------------------------------
	c.Responses["GET /api/v1/reports/sales-summary"] = reflect.TypeOf(crm.SalesSummaryReport{})
	c.Responses["GET /api/v1/reports/support-summary"] = reflect.TypeOf(crm.SupportSummaryReport{})

	// --- Import and export --------------------------------------------------
	c.RequestBodies["POST /api/v1/imports"] = reflect.TypeOf(createImportRequest{})
	c.RequiredBody["CreateImportRequest"] = []string{"entity_type"}
	c.Responses["GET /api/v1/imports"] = reflect.TypeOf(httpx.ListResponse[importx.Job]{})
	c.Responses["GET /api/v1/imports/{id}"] = reflect.TypeOf(importx.Job{})
	c.Responses["POST /api/v1/imports"] = reflect.TypeOf(importx.Job{})
	// importx.Job and jobs.Job are both named "Job"; without distinct component names
	// one type's fields would describe the other.
	c.TypeNames[reflect.TypeOf(importx.Job{})] = "ImportJob"

	for _, path := range []string{
		"POST /api/v1/imports/{id}/analyze",
		"POST /api/v1/imports/{id}/validate",
		"POST /api/v1/imports/{id}/dry-run",
		"POST /api/v1/imports/{id}/execute",
	} {
		c.RequestBodies[path] = reflect.TypeOf(importRequest{})
	}
	c.Responses["POST /api/v1/imports/{id}/analyze"] = reflect.TypeOf(importx.AnalysisResult{})
	c.Responses["POST /api/v1/imports/{id}/validate"] = reflect.TypeOf(importx.Report{})
	c.Responses["POST /api/v1/imports/{id}/dry-run"] = reflect.TypeOf(importx.Report{})
	c.Responses["POST /api/v1/imports/{id}/execute"] = reflect.TypeOf(jobAcceptedResponse{})
	c.Responses["POST /api/v1/imports/{id}/upload"] = reflect.TypeOf(importUploadResponse{})

	c.RequestBodies["POST /api/v1/exports"] = reflect.TypeOf(createExportRequest{})
	c.RequiredBody["CreateExportRequest"] = []string{"entity_type", "format"}
	// The export streams the records as CSV or JSONL, so it returns 200 with a body
	// that is not JSON. Without the declaration the generator would document a 201
	// created-resource and an application/json object, neither of which is what the
	// handler sends.
	c.SuccessStatus["POST /api/v1/exports"] = 200
	c.StreamingSuccess["POST /api/v1/exports"] = openapi.StreamingResponse{
		MediaTypes:  []string{"text/csv", "application/x-ndjson"},
		Description: "The exported records, streamed. `format=csv` returns text/csv; `format=jsonl` returns one JSON object per line as application/x-ndjson. The filename is carried in Content-Disposition.",
	}

	c.StreamingSuccess["GET /api/v1/documents/{id}/download"] = openapi.StreamingResponse{
		MediaTypes:  []string{"application/octet-stream"},
		Description: "The stored file's bytes. The Content-Type is the type recorded for the document, and Content-Disposition carries its filename. Content-Length is set, and the body is not JSON.",
	}

	// --- Migrations ---------------------------------------------------------
	c.RequestBodies["POST /api/v1/migrations"] = reflect.TypeOf(createMigrationRequest{})
	c.RequiredBody["CreateMigrationRequest"] = []string{"name", "config"}
	c.Responses["GET /api/v1/migrations/{id}"] = reflect.TypeOf(migratex.MigrationJob{})
	c.Responses["POST /api/v1/migrations"] = reflect.TypeOf(migratex.MigrationJob{})
	c.Responses["POST /api/v1/migrations/{id}/discover"] = reflect.TypeOf(schemaDiscoveryResponse{})
	c.RequestBodies["POST /api/v1/migrations/{id}/reconciliation"] = reflect.TypeOf(reconcileRequest{})
	c.RequiredBody["ReconcileRequest"] = []string{"source_table", "target_entity"}
	c.Responses["POST /api/v1/migrations/{id}/reconciliation"] = reflect.TypeOf(migratex.Reconciliation{})

	// --- Integrations -------------------------------------------------------
	c.RequestBodies["POST /api/v1/integrations"] = reflect.TypeOf(integrations.CreateRequest{})
	c.RequiredBody["CreateRequest"] = []string{"name", "kind"}
	c.Responses["GET /api/v1/integrations/{id}"] = reflect.TypeOf(integrations.Integration{})
	c.Responses["POST /api/v1/integrations"] = reflect.TypeOf(integrations.Integration{})
	c.Responses["GET /api/v1/integrations/{id}/status"] = reflect.TypeOf(integrationStatusResponse{})
	c.RequestBodies["PUT /api/v1/integrations/{id}/status"] = reflect.TypeOf(setIntegrationStatusRequest{})
	c.RequiredBody["SetIntegrationStatusRequest"] = []string{"status"}

	// --- Webhooks and events ------------------------------------------------
	c.RequestBodies["POST /api/v1/webhooks"] = reflect.TypeOf(events.CreateSubscriptionRequest{})
	c.RequiredBody["CreateSubscriptionRequest"] = []string{"name", "target_url", "event_types"}
	c.Responses["POST /api/v1/webhooks"] = reflect.TypeOf(webhookCreatedResponse{})
	c.RequestBodies["PUT /api/v1/webhooks/{id}/enabled"] = reflect.TypeOf(setWebhookEnabledRequest{})

	// --- Jobs ---------------------------------------------------------------
	c.Responses["GET /api/v1/jobs/{id}"] = reflect.TypeOf(jobs.Job{})

	// --- AI and MCP ---------------------------------------------------------
	c.RequestBodies["POST /api/v1/ai/tasks"] = reflect.TypeOf(createAITaskRequest{})
	c.RequiredBody["CreateAITaskRequest"] = []string{"prompt"}
	c.Responses["POST /api/v1/ai/tasks"] = reflect.TypeOf(aiCompletionResponse{})

	// Contextual AI. The entity type is a query parameter and the identifier is the
	// path parameter, which keeps every path parameter an identifier.
	c.RequestBodies["POST /api/v1/ai/assist/{id}"] = reflect.TypeOf(aiAssistRequest{})
	c.Responses["POST /api/v1/ai/assist/{id}"] = reflect.TypeOf(ai.Insight{})
	c.Responses["GET /api/v1/ai/assist/{id}/context"] = reflect.TypeOf(aiAssistContextResponse{})
	c.QueryParams["POST /api/v1/ai/assist/{id}"] = []openapi.QueryParam{entityTypeParam}
	c.QueryParams["GET /api/v1/ai/assist/{id}/context"] = []openapi.QueryParam{entityTypeParam}

	// Streaming AI. These routes answer with server-sent events, not JSON, so the
	// request bodies are declared here but the success response is a stream.
	c.RequestBodies["POST /api/v1/ai/stream/chat"] = reflect.TypeOf(aiStreamChatRequest{})
	c.RequiredBody["AiStreamChatRequest"] = []string{"prompt"}
	c.RequestBodies["POST /api/v1/ai/stream/assist/{id}"] = reflect.TypeOf(aiStreamAssistRequest{})
	c.QueryParams["POST /api/v1/ai/stream/assist/{id}"] = []openapi.QueryParam{entityTypeParam}
	c.StreamingSuccess["POST /api/v1/ai/stream/chat"] = openapi.StreamingResponse{
		MediaTypes: []string{"text/event-stream"},
		Description: "A server-sent event stream. Each `delta` event carries `{\"text\": ...}` with a piece " +
			"of the reply as it arrives; the final `done` event carries the model name and token usage. " +
			"An `error` event carries `{code, message, retryable}` and ends the stream, because the status " +
			"line is already committed once the stream is open.",
	}
	c.StreamingSuccess["POST /api/v1/ai/stream/assist/{id}"] = openapi.StreamingResponse{
		MediaTypes: []string{"text/event-stream"},
		Description: "A server-sent event stream. Each `delta` event carries `{\"text\": ...}` with the " +
			"model's raw text as it arrives; the final `done` event carries the whole Insight, with the CRM " +
			"facts, the model's analysis and recommendations kept in separate fields, exactly as the buffered " +
			"route returns them. An `error` event carries `{code, message, retryable}` and ends the stream.",
	}

	// The protocol endpoint. `id` is echoed back unchanged and may be a string or a
	// number, so the schema describes it as either; omit it to send a notification,
	// which receives an empty 202 and no body.
	c.RequestBodies["POST /api/v1/mcp"] = reflect.TypeOf(ai.Request{})
	c.Responses["POST /api/v1/mcp"] = reflect.TypeOf(ai.Response{})
	c.TypeNames[reflect.TypeOf(ai.Request{})] = "MCPRequest"
	c.TypeNames[reflect.TypeOf(ai.Response{})] = "MCPResponse"
	c.BodyDescriptions["POST /api/v1/mcp"] = "A JSON-RPC 2.0 request. Methods: " +
		"`initialize`, `notifications/initialized`, `notifications/cancelled`, `ping`, " +
		"`tools/list`, `tools/call`. Omit `id` to send a notification; the server " +
		"answers 202 with no body. A failed tool call is returned as a JSON-RPC error " +
		"with the HTTP status in `data`."

	// The single-call convenience shape for the admin UI. `name` and `arguments` are
	// shorthand for the same fields inside `params`.
	c.RequestBodies["POST /api/v1/mcp/call"] = reflect.TypeOf(mcpCallRequest{})
	c.BodyDescriptions["POST /api/v1/mcp/call"] = "Invokes one tool. Equivalent to a " +
		"`tools/call` JSON-RPC request; `name` and `arguments` are shorthand for the " +
		"same fields inside `params`. A mutating tool requires `confirm: true`."

	// --- Notifications and email --------------------------------------------
	c.RequestBodies["POST /api/v1/admin/email/send"] = reflect.TypeOf(sendEmailRequest{})
	c.RequiredBody["SendEmailRequest"] = []string{"to", "subject"}
	c.BodyDescriptions["POST /api/v1/admin/email/send"] = "`purpose` is one of " +
		"`marketing`, `transactional`, `product_updates` or `research`, and defaults to " +
		"`marketing` when omitted. An unrecognised value is refused rather than treated " +
		"as transactional. Consent is checked for every address in `to`, and the send is " +
		"refused with 403 before the transport is consulted if any recipient lacks a " +
		"grant the purpose requires."

	// --- Workflows ------------------------------------------------------------
	// The workflow CRUD routes are served but were never given contract rows; the
	// template catalog and the import/export pair are the new surface, so they are
	// described here. Import reads the portable export document as raw bytes, so its
	// request is declared as the document type and the required set names the fields
	// ImportWorkflow insists on.
	c.RequestBodies["POST /api/v1/workflows"] = reflect.TypeOf(workflow.CreateInput{})
	c.RequiredBody["CreateInput"] = []string{"name", "definition", "trigger_type"}
	c.Responses["POST /api/v1/workflows"] = reflect.TypeOf(workflow.Workflow{})
	c.Responses["GET /api/v1/workflows"] = reflect.TypeOf(httpx.ListResponse[workflow.Workflow]{})
	c.Responses["GET /api/v1/workflows/{id}"] = reflect.TypeOf(workflow.Workflow{})
	c.RequestBodies["PUT /api/v1/workflows/{id}"] = reflect.TypeOf(workflow.CreateInput{})
	c.RequiredBody["CreateInput"] = []string{"name", "definition", "trigger_type"}
	c.Responses["PUT /api/v1/workflows/{id}"] = reflect.TypeOf(workflow.Workflow{})
	c.Responses["POST /api/v1/workflows/{id}/run"] = reflect.TypeOf(workflow.RunResult{})
	c.Responses["GET /api/v1/workflows/{id}/runs"] = reflect.TypeOf(httpx.ListResponse[workflow.Run]{})
	c.Responses["GET /api/v1/workflow-runs/{id}"] = reflect.TypeOf(workflow.Run{})
	c.Responses["GET /api/v1/workflow-runs/{id}/steps"] = reflect.TypeOf(httpx.ListResponse[workflow.StepRun]{})

	c.RequestBodies["POST /api/v1/workflows/import"] = reflect.TypeOf(workflow.ExportDocument{})
	c.RequiredBody["ExportDocument"] = []string{"format", "format_version", "name", "definition"}
	c.Responses["POST /api/v1/workflows/import"] = reflect.TypeOf(workflow.Workflow{})
	c.Responses["GET /api/v1/workflows/{id}/export"] = reflect.TypeOf(workflow.ExportDocument{})

	c.Responses["GET /api/v1/workflow-templates"] = reflect.TypeOf(httpx.ListResponse[workflow.Template]{})
	c.Responses["GET /api/v1/workflow-templates/{id}"] = reflect.TypeOf(workflow.Template{})
	c.Responses["POST /api/v1/workflow-templates/{id}/create"] = reflect.TypeOf(workflow.Workflow{})

	// --- Saved list views ------------------------------------------------------
	// A view stores the same list-query parameters the CRM list endpoints parse, so
	// its request and response are the view document itself. The request's required
	// set names the fields validate() insists on; `filters` is optional because an
	// empty filter is a valid view that shows everything.
	c.RequestBodies["POST /api/v1/views"] = reflect.TypeOf(views.Input{})
	c.Responses["POST /api/v1/views"] = reflect.TypeOf(views.View{})
	c.Responses["GET /api/v1/views"] = reflect.TypeOf(httpx.ListResponse[views.View]{})
	c.Responses["GET /api/v1/views/{id}"] = reflect.TypeOf(views.View{})
	c.RequestBodies["PUT /api/v1/views/{id}"] = reflect.TypeOf(views.Input{})
	c.Responses["PUT /api/v1/views/{id}"] = reflect.TypeOf(views.View{})
	// `Input` is a bare name a second type could share, so the component is renamed
	// the way the workflow and simulation request types are. The required set is
	// keyed by that component name.
	c.BodyNames["POST /api/v1/views"] = "SavedViewInput"
	c.BodyNames["PUT /api/v1/views/{id}"] = "SavedViewInput"
	c.TypeNames[reflect.TypeOf(views.Input{})] = "SavedViewInput"
	c.RequiredBody["SavedViewInput"] = []string{"entity_type", "name"}
	c.QueryParams["GET /api/v1/views"] = []openapi.QueryParam{
		{Name: "entity_type", Required: true,
			Schema:      map[string]any{"type": "string", "enum": []string{"accounts", "contacts", "leads", "deals", "tasks", "tickets"}},
			Description: "The list screen to load views for."},
	}
	c.BodyDescriptions["POST /api/v1/views"] = "A saved list view. `entity_type` and `name` are required; " +
		"`filters` is the same parameter set the corresponding list endpoint accepts. A name the caller " +
		"already uses for the same screen is refused with 422 rather than silently creating a second view."

	// --- Consent ----------------------------------------------------------------
	// A consent record is a decision about one subject, purpose and channel, so the
	// request body is the record's writable projection. The subject is either
	// `contact_id` or `subject_email`; that exclusivity is a conditional rule the
	// required list cannot express, so it is described in the body text. The reads
	// take the subject as a query parameter for the same reason the body carries it
	// either way: a subject is an id or an address, not a path segment.
	c.RequestBodies["POST /api/v1/consent"] = reflect.TypeOf(consent.Input{})
	c.Responses["POST /api/v1/consent"] = reflect.TypeOf(consent.Record{})
	c.SuccessStatus["POST /api/v1/consent"] = 201
	c.Responses["GET /api/v1/consent"] = reflect.TypeOf(httpx.ListResponse[consent.Record]{})
	c.Responses["GET /api/v1/consent/current"] = reflect.TypeOf(httpx.ListResponse[consent.Record]{})
	c.Responses["GET /api/v1/consent/history"] = reflect.TypeOf(httpx.ListResponse[consent.Record]{})
	c.Responses["GET /api/v1/consent/decide"] = reflect.TypeOf(consent.Decision{})
	c.RequestBodies["POST /api/v1/consent/withdraw"] = reflect.TypeOf(withdrawConsentRequest{})
	c.Responses["POST /api/v1/consent/withdraw"] = reflect.TypeOf(consent.Record{})
	c.SuccessStatus["POST /api/v1/consent/withdraw"] = 201
	// `Input` is a bare name a second type could share, so the component is renamed and
	// the required set is keyed by that name. `subject_email` is required in effect but
	// only when `contact_id` is absent, which is the conditional rule described below.
	c.BodyNames["POST /api/v1/consent"] = "ConsentInput"
	c.TypeNames[reflect.TypeOf(consent.Input{})] = "ConsentInput"
	c.RequiredBody["ConsentInput"] = []string{"purpose", "channel", "state"}
	for _, op := range []string{
		"GET /api/v1/consent", "GET /api/v1/consent/current",
		"GET /api/v1/consent/history", "GET /api/v1/consent/decide",
	} {
		c.QueryParams[op] = []openapi.QueryParam{
			{Name: "contact_id", Description: "The contact the decision is about. Pass this or `email`, not both."},
			{Name: "email", Description: "The email address the decision is about, for a person not in the CRM."},
		}
	}
	c.QueryParams["GET /api/v1/consent/history"] = append(c.QueryParams["GET /api/v1/consent/history"],
		openapi.QueryParam{Name: "purpose", Required: true,
			Schema:      map[string]any{"type": "string", "enum": []string{"marketing", "transactional", "product_updates", "research"}},
			Description: "The purpose to show the history of."},
		openapi.QueryParam{Name: "channel", Required: true,
			Schema:      map[string]any{"type": "string", "enum": []string{"email", "telegram", "webhook"}},
			Description: "The channel to show the history of."})
	c.QueryParams["GET /api/v1/consent/decide"] = append(c.QueryParams["GET /api/v1/consent/decide"],
		openapi.QueryParam{Name: "purpose", Required: true,
			Schema:      map[string]any{"type": "string", "enum": []string{"marketing", "transactional", "product_updates", "research"}},
			Description: "The purpose to decide for."},
		openapi.QueryParam{Name: "channel", Required: true,
			Schema:      map[string]any{"type": "string", "enum": []string{"email", "telegram", "webhook"}},
			Description: "The channel to decide for."})
	c.BodyDescriptions["POST /api/v1/consent"] = "Records a consent decision. Exactly one of `contact_id` " +
		"or `subject_email` names the person; passing both, or neither, is refused with 422. `state` is " +
		"`granted`, `denied` or `withdrawn`; a missing record is never read as permission, so an absent " +
		"decision blocks a send rather than allowing it. Recording a new decision supersedes the previous " +
		"one for the same subject, purpose and channel without overwriting it, so the history is kept."
	c.BodyDescriptions["POST /api/v1/consent/withdraw"] = "Withdraws consent for one subject, purpose and " +
		"channel. It is the same write as recording a decision with state `withdrawn`, named for the act."

	// --- Tenant configuration ---------------------------------------------------
	// The configuration surface is a closed catalog plus the overrides stored against
	// it. The key is a path segment rather than a body field because it names the
	// resource: reading, changing and resetting one setting are three operations on
	// the same address, and a body-keyed write would make "which setting" a property
	// of the payload rather than of the target.
	//
	// `tenantconfig.Setting.Value` is `any`: the catalog declares the kind, so the
	// shape of a value is a property of its key, not of the type. Publishing it as a
	// typed component would describe one kind and be wrong for the other three. The
	// catalog endpoint carries the same type, which is what lets a client render the
	// right editor per key from one document.
	c.Responses["GET /api/v1/settings"] = reflect.TypeOf(httpx.ListResponse[tenantconfig.Setting]{})
	c.Responses["GET /api/v1/settings/catalog"] = reflect.TypeOf(httpx.ListResponse[tenantconfig.Setting]{})
	c.Responses["GET /api/v1/settings/{key}"] = reflect.TypeOf(tenantconfig.Setting{})
	c.Responses["GET /api/v1/settings/{key}/history"] = reflect.TypeOf(httpx.ListResponse[tenantconfig.Revision]{})
	c.RequestBodies["PUT /api/v1/settings/{key}"] = reflect.TypeOf(setTenantSettingRequest{})
	c.Responses["PUT /api/v1/settings/{key}"] = reflect.TypeOf(tenantconfig.Setting{})
	c.Responses["DELETE /api/v1/settings/{key}"] = reflect.TypeOf(tenantconfig.Setting{})
	c.BodyNames["PUT /api/v1/settings/{key}"] = "SetTenantSettingRequest"
	c.TypeNames[reflect.TypeOf(setTenantSettingRequest{})] = "SetTenantSettingRequest"
	c.RequiredBody["SetTenantSettingRequest"] = []string{"value"}
	c.QueryParams["GET /api/v1/settings/{key}/history"] = append(
		c.QueryParams["GET /api/v1/settings/{key}/history"],
		openapi.QueryParam{Name: "limit",
			Schema:      map[string]any{"type": "integer", "minimum": 1, "maximum": 200, "default": 50},
			Description: "How many revisions to return, newest first. Defaults to 50."})
	c.BodyDescriptions["PUT /api/v1/settings/{key}"] = "Sets one setting for the caller's organization. " +
		"The key must be in the published catalog; an unknown key is a 404, because the catalog is the " +
		"platform's and a setting nothing reads would be a control that does nothing. `value` is validated " +
		"against the key's kind and bounds, and the reply carries the effective value that now applies, " +
		"together with the default it was layered over. The write appends a revision recording the value it " +
		"replaced, so a change to a shared default is not a silent one."
	c.BodyDescriptions["DELETE /api/v1/settings/{key}"] = "Removes the organization's override so the " +
		"catalog default applies again. It is recorded as a revision with the removed value as its " +
		"predecessor, so a reset is part of the history rather than an erasure of it. The reply carries the " +
		"key's state after the reset."

		// --- MCP client credentials ------------------------------------------------
	// A credential is minted once and its token is returned exactly once; the list
	// and every later read carry only the non-secret prefix. `permissions` is
	// required in effect but is validated as a non-empty list rather than a presence
	// check, so it is described in the body text rather than the required list.
	c.RequestBodies["POST /api/v1/mcp/credentials"] = reflect.TypeOf(mcpcred.CreateInput{})
	c.Responses["POST /api/v1/mcp/credentials"] = reflect.TypeOf(mcpcred.Created{})
	c.SuccessStatus["POST /api/v1/mcp/credentials"] = 201
	c.Responses["GET /api/v1/mcp/credentials"] = reflect.TypeOf(httpx.ListResponse[mcpcred.Credential]{})
	c.Responses["POST /api/v1/mcp/credentials/{id}/rotate"] = reflect.TypeOf(mcpcred.Created{})
	c.BodyNames["POST /api/v1/mcp/credentials"] = "CreateMCPCredentialInput"
	c.TypeNames[reflect.TypeOf(mcpcred.CreateInput{})] = "CreateMCPCredentialInput"
	c.BodyDescriptions["POST /api/v1/mcp/credentials"] = "Mints a scoped MCP client credential. " +
		"`permissions` must be a non-empty subset of the permissions the caller holds — a credential narrows " +
		"authority, it never widens it — and every entry must be a permission in the catalog. The response is " +
		"the only time the token is returned; only its hash is stored, and a lost token is replaced by rotating " +
		"the credential, not recovered."

	c.BodyDescriptions["POST /api/v1/workflows/import"] = "A portable workflow document, as produced by " +
		"`GET /api/v1/workflows/{id}/export`. The `definition` is the same document the workflow stores, so an " +
		"import is not a second format. A document whose `format` is not `forgecrm.workflow`, whose " +
		"`format_version` is newer than this server understands, or whose trigger names an event the platform " +
		"does not emit is refused with the reason."
	c.BodyDescriptions["POST /api/v1/workflow-templates/{id}/create"] = "Optional. `name` overrides the " +
		"template's own name; every other field comes from the template."

	// --- Simulation Lab -----------------------------------------------------
	// The scenario catalog is session-only metadata: it describes the engine, not
	// the tenant. The scenario CRUD and the run/preview pair are the data surface.
	// A run's `result` carries money as integer minor units, so the document is a
	// passthrough rather than a typed component.
	c.Responses["GET /api/v1/simulation/catalog"] = reflect.TypeOf(catalogResponse{})
	c.Responses["GET /api/v1/simulation/scenarios"] = reflect.TypeOf(scenarioListResponse{})
	c.RequestBodies["POST /api/v1/simulation/scenarios"] = reflect.TypeOf(createScenarioRequest{})
	c.RequiredBody["CreateScenarioRequest"] = []string{"name", "kind"}
	c.Responses["POST /api/v1/simulation/scenarios"] = reflect.TypeOf(scenarioResponse{})
	c.SuccessStatus["POST /api/v1/simulation/scenarios"] = 201
	c.Responses["GET /api/v1/simulation/scenarios/{id}"] = reflect.TypeOf(scenarioResponse{})
	c.Responses["POST /api/v1/simulation/scenarios/{id}/runs"] = reflect.TypeOf(runResponse{})
	c.SuccessStatus["POST /api/v1/simulation/scenarios/{id}/runs"] = 201
	c.Responses["GET /api/v1/simulation/scenarios/{id}/runs"] = reflect.TypeOf(runListResponse{})
	c.Responses["POST /api/v1/simulation/preview"] = reflect.TypeOf(previewResponse{})
	c.RequestBodies["POST /api/v1/simulation/preview"] = reflect.TypeOf(previewRequest{})
	c.RequiredBody["PreviewRequest"] = []string{"kind"}
	c.RequestBodies["POST /api/v1/simulation/scenarios/{id}/runs"] = reflect.TypeOf(simulationRunRequest{})

	c.BodyDescriptions["POST /api/v1/simulation/scenarios"] = "A saved scenario. `params` is the " +
		"scenario parameter document; percentages are changes relative to the current value (20 means 20% more). " +
		"Parameters are validated on save, so a stored scenario cannot hold a value that will fail at run time. " +
		"The scenario kind must be one of `funnel`, `revenue` or `retention`."
	c.BodyDescriptions["POST /api/v1/simulation/preview"] = "An unsaved parameter set to compute without " +
		"persisting. Same parameter document as a saved scenario. The reply carries the comparison, the baseline " +
		"it was computed against, and any warnings. Insufficient CRM history is reported as a 422 with the reason, " +
		"never as a fabricated number."
	c.BodyDescriptions["POST /api/v1/simulation/scenarios/{id}/runs"] = "Optional. `pipeline_id` restricts the " +
		"funnel baseline; `from` and `to` restrict the period. An empty body uses the defaults."

	// --- Forecasting --------------------------------------------------------
	// The method catalog is session-only metadata. A run stores the series it was
	// computed from and its result, both exact JSON documents written by
	// internal/analytics; `result` carries money as integer minor units, so it is a
	// passthrough rather than a typed component.
	//
	// simulate.CatalogEntry and forecast.CatalogEntry are both named "CatalogEntry";
	// without distinct component names one type's fields would describe the other.
	c.TypeNames[reflect.TypeOf(forecast.CatalogEntry{})] = "ForecastCatalogEntry"
	c.Responses["GET /api/v1/forecast/catalog"] = reflect.TypeOf(forecastCatalogResponse{})
	c.Responses["GET /api/v1/forecast/runs"] = reflect.TypeOf(forecastRunListResponse{})
	c.RequestBodies["POST /api/v1/forecast/runs"] = reflect.TypeOf(createForecastRunRequest{})
	c.RequiredBody["CreateForecastRunRequest"] = []string{"method", "horizon"}
	c.Responses["POST /api/v1/forecast/runs"] = reflect.TypeOf(forecastRunResponse{})
	c.SuccessStatus["POST /api/v1/forecast/runs"] = 201
	c.Responses["GET /api/v1/forecast/runs/{id}"] = reflect.TypeOf(forecastRunResponse{})
	c.BodyDescriptions["POST /api/v1/forecast/runs"] = "Compute and store a revenue forecast. `method` is " +
		"`mean`, `moving_average` or `trend`; `horizon` is how many future periods to project. `window` is " +
		"required for `moving_average`. The reply carries the projected points, the observed series they were " +
		"computed from, and the measured out-of-sample accuracy next to a naive baseline. Too little history is " +
		"reported as a 422 with the reason, never as a fabricated number."

	// --- Risk ---------------------------------------------------------------
	// A signal's `evidence` is the observed values behind the decision, so the
	// document is a passthrough. Thresholds are a per-organization setting.
	c.Responses["GET /api/v1/risk/signals"] = reflect.TypeOf(riskSignalListResponse{})
	c.Responses["POST /api/v1/risk/scan"] = reflect.TypeOf(riskScanResponse{})
	c.Responses["GET /api/v1/risk/signals/{id}"] = reflect.TypeOf(riskSignalResponse{})
	c.RequestBodies["POST /api/v1/risk/signals/{id}/status"] = reflect.TypeOf(setRiskStatusRequest{})
	c.RequiredBody["SetRiskStatusRequest"] = []string{"status"}
	c.Responses["POST /api/v1/risk/signals/{id}/status"] = reflect.TypeOf(riskSignalResponse{})
	c.Responses["GET /api/v1/risk/thresholds"] = reflect.TypeOf(riskThresholdsResponse{})
	c.RequestBodies["PUT /api/v1/risk/thresholds"] = reflect.TypeOf(riskThresholdsResponse{})
	c.Responses["PUT /api/v1/risk/thresholds"] = reflect.TypeOf(riskThresholdsResponse{})
	c.QueryParams["GET /api/v1/risk/signals"] = []openapi.QueryParam{
		{Name: "status", Schema: map[string]any{"type": "string"}, Description: "Restrict to one status: open, acknowledged, resolved or dismissed."},
		{Name: "severity", Schema: map[string]any{"type": "string"}, Description: "Restrict to one severity: info, warn, high or critical."},
		{Name: "type", Schema: map[string]any{"type": "string"}, Description: "Restrict to one signal type, for example stalled_deal."},
		{Name: "include_terminal", Schema: map[string]any{"type": "boolean"}, Description: "Include resolved and dismissed signals, which are hidden by default."},
	}
	c.BodyDescriptions["POST /api/v1/risk/signals/{id}/status"] = "Move a signal to `acknowledged`, " +
		"`dismissed` or `resolved`. Any other value is refused. The move is recorded in the audit log."
	c.BodyDescriptions["PUT /api/v1/risk/thresholds"] = "The organization's risk thresholds. A day count " +
		"or a ratio left at zero keeps the default. Ratios are basis points (10000 = 1.0), matching the " +
		"detector's own representation."

	// --- System -------------------------------------------------------------
	c.Responses["GET /api/v1/system/info"] = reflect.TypeOf(systemInfoResponse{})
	c.Responses["GET /api/v1/system/capabilities"] = reflect.TypeOf(capabilitiesResponse{})
	c.Responses["GET /health"] = reflect.TypeOf(healthResponse{})
	c.Responses["GET /ready"] = reflect.TypeOf(readyResponse{})

	// --- Query parameters ---------------------------------------------------
	c.QueryParams["GET /api/v1/search"] = []openapi.QueryParam{
		{Name: "q", Required: true, Schema: map[string]any{"type": "string"},
			Description: "The term to search for. Required; a request without it is rejected."},
	}

	return c
}

// The response shapes below are wrappers around a single record.
//
// The handlers write them as map literals rather than named types, so the literal
// is the only description of the shape that exists. Declaring the type here makes
// the shape checkable: the runtime contract test compares a real response against
// this schema, so a key added to the literal without a matching field fails the
// build rather than silently missing from the documentation.

type inviteResponse struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Note       string `json:"note"`
	ResetToken string `json:"reset_token,omitempty"`
}

type roleCreatedResponse struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

type organizationCreatedResponse struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

type teamCreatedResponse struct {
	ID string `json:"id"`
}

type leadConversionResponse struct {
	Lead    *crm.Lead    `json:"lead"`
	Account *crm.Account `json:"account"`
	Contact *crm.Contact `json:"contact"`
}

type pipelineListResponse struct {
	Items []crm.Pipeline `json:"items"`
}

type tagListResponse struct {
	Items []crm.Tag `json:"items"`
}

type jobAcceptedResponse struct {
	ImportID string `json:"import_id"`
	JobID    string `json:"job_id"`
	Status   string `json:"status"`
	Note     string `json:"note,omitempty"`
}

type importUploadResponse struct {
	ID             string `json:"id"`
	ChecksumSHA256 string `json:"checksum_sha256"`
	SizeBytes      int64  `json:"size_bytes"`
}

type schemaDiscoveryResponse struct {
	Tables   []map[string]any `json:"tables"`
	ReadOnly bool             `json:"read_only"`
}

// integrationStatusResponse mirrors the status payload, which is a projection of
// integrations.Integration with the stored configuration and secrets omitted. The
// timestamps are *time.Time so they serialise exactly as the handler writes them.
type integrationStatusResponse struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Kind           string     `json:"kind"`
	Status         string     `json:"status"`
	AllowWrite     bool       `json:"allow_write"`
	HasSecrets     bool       `json:"has_secrets"`
	LastTestAt     *time.Time `json:"last_test_at"`
	LastTestOK     *bool      `json:"last_test_ok"`
	LastTestError  *string    `json:"last_test_error"`
	LastSyncAt     *time.Time `json:"last_sync_at"`
	TimeoutSeconds int        `json:"timeout_seconds"`
	MaxConcurrency int        `json:"max_concurrency"`
}

type webhookCreatedResponse struct {
	Subscription *events.Subscription `json:"subscription"`
}

// entityTypeParam is the shared entity-type selector for the contextual AI routes.
// It is declared once because both routes read it the same way.
var entityTypeParam = openapi.QueryParam{
	Name:        "entity_type",
	Description: "The kind of record the identifier names.",
	Required:    true,
	Schema:      map[string]any{"type": "string", "enum": []string{"account", "contact", "deal", "ticket", "task"}},
}

// aiAssistContextResponse is what the model would be told about a record, returned
// without calling the model. It is a distinct type from ai.Insight because it has
// no analysis: showing a caller an empty analysis field would suggest the model had
// nothing to say, when in fact it was never asked.
type aiAssistContextResponse struct {
	EntityType  string          `json:"entity_type"`
	EntityID    string          `json:"entity_id"`
	Subject     string          `json:"subject"`
	Facts       []ai.Fact       `json:"facts"`
	Disclosures []ai.Disclosure `json:"disclosures"`
	Available   bool            `json:"available"`
}

type aiCompletionResponse struct {
	Content      string `json:"content"`
	Model        string `json:"model"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

type healthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
}

type readyResponse struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
	Detail string `json:"detail,omitempty"`
	// Version and Commit are reported on the success path so a deployment can
	// confirm which build is serving without a second request.
	Version string `json:"version,omitempty"`
	Commit  string `json:"commit,omitempty"`
}
