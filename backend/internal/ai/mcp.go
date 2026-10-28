package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// Tool is an MCP tool exposed to an AI agent.
//
// Every tool runs the same domain service and the same permission check as the
// REST API. There is no privileged path: an agent acting for a user has exactly
// that user's authority and nothing more.
type Tool struct {
	Name        string
	Description string
	// Permission is required to invoke the tool at all.
	Permission rbac.Permission
	// InputSchema is a JSON Schema for the arguments, published to the client.
	InputSchema map[string]any
	// Dangerous marks an operation that mutates state, which the API requires an
	// explicit confirmation flag for.
	Dangerous bool
	// Handler performs the tool call.
	Handler func(ctx context.Context, p *rbac.Principal, args map[string]any) (any, error)
}

// ToolRegistry holds the available tools.
type ToolRegistry struct {
	tools map[string]*Tool
	crm   *crm.Service
	audit *audit.Logger
}

// NewToolRegistry builds the registry. The tool set is deliberately small and
// entirely composed of CRM operations; there is no raw SQL, shell, filesystem or
// unbounded export tool, by construction.
func NewToolRegistry(crmSvc *crm.Service, auditLog *audit.Logger) *ToolRegistry {
	r := &ToolRegistry{tools: map[string]*Tool{}, crm: crmSvc, audit: auditLog}
	for _, t := range r.buildTools() {
		r.tools[t.Name] = t
	}
	return r
}

// List returns the tools in a stable order.
func (r *ToolRegistry) List() []*Tool {
	out := make([]*Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get returns a tool by name.
func (r *ToolRegistry) Get(name string) (*Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// PromptDescriptors renders the tool list for the prompt's tool contract.
//
// It is derived from the registry rather than hand-written into the prompt text,
// so the list the model sees cannot disagree with the tools the server will
// actually authorize. A nil registry yields an empty list, which lets a caller
// assemble a prompt before AI is configured.
func (r *ToolRegistry) PromptDescriptors() []ToolDescriptor {
	if r == nil {
		return nil
	}
	tools := r.List()
	out := make([]ToolDescriptor, 0, len(tools))
	for _, t := range tools {
		out = append(out, ToolDescriptor{
			Name:                 t.Name,
			Description:          t.Description,
			RequiresConfirmation: t.Dangerous,
		})
	}
	return out
}

// Call invokes a tool after checking the caller's permission.
//
// The permission check happens here, before the handler, so a handler cannot
// accidentally bypass authorization. The invocation is audited, including
// denied attempts.
func (r *ToolRegistry) Call(ctx context.Context, p *rbac.Principal, name string,
	args map[string]any) (any, error) {

	tool, ok := r.tools[name]
	if !ok {
		return nil, httpx.NewError(404, httpx.CodeNotFound, fmt.Sprintf("unknown tool %q", name))
	}
	if !p.Has(tool.Permission) {
		r.record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID, ActorType: "ai_agent",
			Action: string(audit.ActionAIToolInvoked), EntityType: "ai_tool", EntityID: name,
			Metadata: map[string]any{"allowed": false, "required_permission": string(tool.Permission)},
		})
		return nil, httpx.NewError(403, httpx.CodeForbidden,
			fmt.Sprintf("permission %s is required to use this tool", tool.Permission))
	}
	result, err := tool.Handler(ctx, p, args)
	entry := audit.Entry{
		OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID, ActorType: "ai_agent",
		Action: string(audit.ActionAIToolInvoked), EntityType: "ai_tool", EntityID: name,
		Metadata: map[string]any{"allowed": true, "ok": err == nil},
	}
	if err != nil {
		entry.Metadata["error"] = err.Error()
	}
	r.record(ctx, entry)
	return result, err
}

// record writes an audit entry, tolerating a registry built without a logger.
//
// The audit write never decides the outcome. A denial is enforced because the
// permission is absent, not because the entry was written, so a missing logger
// must not turn a refusal into a crash — which is what dereferencing it on the
// denial path would do.
func (r *ToolRegistry) record(ctx context.Context, e audit.Entry) {
	if r.audit == nil {
		return
	}
	_ = r.audit.Record(ctx, e)
}

// Descriptors renders the tool list in MCP-compatible form.
func (r *ToolRegistry) Descriptors() []map[string]any {
	out := make([]map[string]any, 0, len(r.tools))
	for _, t := range r.List() {
		out = append(out, map[string]any{
			"name":         t.Name,
			"description":  t.Description,
			"input_schema": t.InputSchema,
			"dangerous":    t.Dangerous,
			"permission":   string(t.Permission),
		})
	}
	return out
}

// MCPDescriptors renders the tool list for the MCP protocol.
//
// It differs from Descriptors in the field names, and that is the point: the
// protocol defines `inputSchema`, while the admin UI reads `input_schema`. A
// client reading the protocol shape finds the schema where it expects it, and a
// tool with no declared input schema reports an empty object rather than a null,
// which some clients refuse.
func (r *ToolRegistry) MCPDescriptors() []map[string]any {
	out := make([]map[string]any, 0, len(r.tools))
	for _, t := range r.List() {
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		if t.Dangerous {
			schema = withConfirmation(schema)
		}
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": schema,
		})
	}
	return out
}

// withConfirmation adds the confirm argument a mutating tool requires.
//
// The requirement is published rather than enforced invisibly: a client that
// reads the schema learns it must acknowledge the write before calling, instead
// of receiving a refusal it cannot explain. The input schema is copied rather
// than mutated, because the same map is served to the admin UI, which has no
// confirmation flag.
func withConfirmation(schema map[string]any) map[string]any {
	props, _ := schema["properties"].(map[string]any)
	copied := make(map[string]any, len(props)+1)
	for k, v := range props {
		copied[k] = v
	}
	copied["confirm"] = map[string]any{
		"type": "boolean",
		"description": "Must be true. Acknowledges that this tool changes stored data; " +
			"the call is refused without it.",
	}

	required, _ := schema["required"].([]string)
	next := make([]string, 0, len(required)+1)
	for _, name := range required {
		if name != "confirm" {
			next = append(next, name)
		}
	}
	next = append(next, "confirm")

	out := make(map[string]any, len(schema)+1)
	for k, v := range schema {
		out[k] = v
	}
	out["properties"] = copied
	out["required"] = next
	return out
}

// CallMCP handles an MCP-style JSON-RPC tools/call request.
func (r *ToolRegistry) CallMCP(ctx context.Context, p *rbac.Principal, method string,
	params map[string]any) (any, error) {

	switch method {
	case "tools/list":
		return map[string]any{"tools": r.Descriptors()}, nil
	case "tools/call":
		name, _ := params["name"].(string)
		if name == "" {
			return nil, httpx.NewError(400, httpx.CodeBadRequest, "tool name is required")
		}
		args, _ := params["arguments"].(map[string]any)
		if args == nil {
			args = map[string]any{}
		}
		// A mutating tool requires explicit confirmation. This is the
		// "confirm dangerous operations" requirement: an agent cannot silently
		// perform a write.
		if err := r.RequireConfirmation(name, args); err != nil {
			return nil, err
		}
		result, err := r.Call(ctx, p, name, args)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"content":  []map[string]any{{"type": "text", "text": string(encoded)}},
			"is_error": false,
		}, nil
	default:
		return nil, httpx.NewError(400, httpx.CodeBadRequest,
			fmt.Sprintf("unsupported MCP method %q", method))
	}
}

// RequireConfirmation enforces the acknowledgement a mutating tool needs.
//
// It is one function called from both transports, so the rule cannot hold on the
// JSON-RPC path and be forgotten on the other, which is exactly the kind of gap
// that turns a safety check into a formality.
func (r *ToolRegistry) RequireConfirmation(name string, args map[string]any) error {
	tool, ok := r.Get(name)
	if !ok || !tool.Dangerous {
		return nil
	}
	if confirmed, _ := args["confirm"].(bool); confirmed {
		return nil
	}
	return httpx.NewError(400, httpx.CodeBadRequest,
		fmt.Sprintf("tool %q changes data and requires confirm=true", name))
}

func (r *ToolRegistry) buildTools() []*Tool {
	return []*Tool{
		{
			Name:        "search_accounts",
			Description: "Search client organisations by name or email.",
			Permission:  rbac.PermAccountsRead,
			InputSchema: objectSchema(map[string]any{
				"query": map[string]any{"type": "string", "description": "search term (at least 2 characters)"},
				"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 50},
			}, []string{"query"}),
			Handler: func(ctx context.Context, p *rbac.Principal, args map[string]any) (any, error) {
				q, err := requiredString(args, "query")
				if err != nil {
					return nil, err
				}
				limit := intArg(args, "limit", 10)
				res, err := r.crm.Search(ctx, p, q, limit)
				if err != nil {
					return nil, err
				}
				filtered := make([]crm.SearchResult, 0, len(res))
				for _, item := range res {
					if item.EntityType == "account" {
						filtered = append(filtered, item)
					}
				}
				return filtered, nil
			},
		},
		{
			Name:        "account_summary",
			Description: "Summarise a client organisation: open deals, recent activity and open tickets.",
			Permission:  rbac.PermAccountsRead,
			InputSchema: objectSchema(map[string]any{
				"account_id": map[string]any{"type": "string", "description": "account UUID"},
			}, []string{"account_id"}),
			Handler: func(ctx context.Context, p *rbac.Principal, args map[string]any) (any, error) {
				id, err := uuidArg(args, "account_id")
				if err != nil {
					return nil, err
				}
				account, err := r.crm.GetAccount(ctx, p, id)
				if err != nil {
					return nil, err
				}
				summary := map[string]any{
					"account": map[string]any{
						"id": account.ID, "name": account.Name, "industry": account.Industry,
						"email": account.Email,
					},
				}
				// Each section is included only when the caller may read it.
				if p.Has(rbac.PermDealsRead) {
					deals, total, err := r.crm.ListDeals(ctx, p, crm.ListQuery{
						AccountID: &id, Pagination: httpx.Pagination{Limit: 10},
					})
					if err == nil {
						var openValue float64
						for _, d := range deals {
							if d.Status == "open" {
								openValue += d.Amount
							}
						}
						summary["deals"] = map[string]any{
							"total": total, "recent": deals, "open_value": openValue,
						}
					}
				}
				if p.Has(rbac.PermTicketsRead) {
					tickets, total, err := r.crm.ListTickets(ctx, p, crm.ListQuery{
						AccountID: &id, Pagination: httpx.Pagination{Limit: 5},
					})
					if err == nil {
						summary["tickets"] = map[string]any{"total": total, "recent": tickets}
					}
				}
				if p.Has(rbac.PermActivitiesRead) {
					activities, _, err := r.crm.ListActivities(ctx, p, crm.ListQuery{
						AccountID: &id, Pagination: httpx.Pagination{Limit: 5},
					})
					if err == nil {
						summary["recent_activity"] = activities
					}
				}
				return summary, nil
			},
		},
		{
			Name:        "list_tasks",
			Description: "List tasks visible to the caller, optionally filtered by status.",
			Permission:  rbac.PermTasksRead,
			InputSchema: objectSchema(map[string]any{
				"status": map[string]any{"type": "string", "enum": []string{"open", "in_progress", "completed", "cancelled"}},
				"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 50},
			}, nil),
			Handler: func(ctx context.Context, p *rbac.Principal, args map[string]any) (any, error) {
				limit := intArg(args, "limit", 20)
				status, _ := args["status"].(string)
				tasks, total, err := r.crm.ListTasks(ctx, p, crm.ListQuery{
					Status: status, Pagination: httpx.Pagination{Limit: limit},
				})
				if err != nil {
					return nil, err
				}
				return map[string]any{"total": total, "tasks": tasks}, nil
			},
		},
		{
			Name:        "pipeline_overview",
			Description: "Show pipelines with their stages and per-stage deal totals.",
			Permission:  rbac.PermPipelinesRead,
			InputSchema: objectSchema(map[string]any{
				"pipeline_id": map[string]any{"type": "string", "description": "optional pipeline UUID"},
			}, nil),
			Handler: func(ctx context.Context, p *rbac.Principal, args map[string]any) (any, error) {
				if raw, ok := args["pipeline_id"]; ok && raw != nil && fmt.Sprint(raw) != "" {
					id, err := uuidArg(args, "pipeline_id")
					if err != nil {
						return nil, err
					}
					return r.crm.PipelineReportFor(ctx, p, id)
				}
				pipelines, err := r.crm.ListPipelines(ctx, p)
				if err != nil {
					return nil, err
				}
				out := make([]map[string]any, 0, len(pipelines))
				for _, pl := range pipelines {
					rep, err := r.crm.PipelineReportFor(ctx, p, pl.ID)
					if err != nil {
						continue
					}
					out = append(out, map[string]any{"pipeline": pl, "report": rep})
				}
				return out, nil
			},
		},
		{
			Name:        "create_task",
			Description: "Create a task. Requires confirmation because it writes data.",
			Permission:  rbac.PermTasksCreate,
			Dangerous:   true,
			InputSchema: objectSchema(map[string]any{
				"title":       map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
				"priority":    map[string]any{"type": "string", "enum": []string{"low", "normal", "high", "urgent"}},
				"due_at":      map[string]any{"type": "string", "description": "RFC3339 timestamp"},
				"deal_id":     map[string]any{"type": "string"},
				"account_id":  map[string]any{"type": "string"},
			}, []string{"title"}),
			Handler: func(ctx context.Context, p *rbac.Principal, args map[string]any) (any, error) {
				title, err := requiredString(args, "title")
				if err != nil {
					return nil, err
				}
				in := crm.TaskInput{Title: title, Priority: stringArg(args, "priority", "normal")}
				if v := stringArg(args, "description", ""); v != "" {
					in.Description = &v
				}
				if raw := stringArg(args, "due_at", ""); raw != "" {
					t, err := time.Parse(time.RFC3339, raw)
					if err != nil {
						return nil, httpx.ValidationError(map[string]string{"due_at": "must be an RFC3339 timestamp"})
					}
					in.DueAt = &t
				}
				if raw := stringArg(args, "deal_id", ""); raw != "" {
					id, err := uuid.Parse(raw)
					if err != nil {
						return nil, httpx.ValidationError(map[string]string{"deal_id": "must be a UUID"})
					}
					in.DealID = &id
				}
				if raw := stringArg(args, "account_id", ""); raw != "" {
					id, err := uuid.Parse(raw)
					if err != nil {
						return nil, httpx.ValidationError(map[string]string{"account_id": "must be a UUID"})
					}
					in.AccountID = &id
				}
				// The assignee defaults to the caller: an agent creating a task on
				// someone else's behalf would need tasks.assign.
				if p.Has(rbac.PermTasksAssign) {
					in.AssigneeUserID = &p.UserID
				} else {
					in.AssigneeUserID = &p.UserID
				}
				return r.crm.CreateTask(ctx, p, in)
			},
		},
		{
			Name:        "create_ticket",
			Description: "Open a support ticket. Requires confirmation because it writes data.",
			Permission:  rbac.PermTicketsCreate,
			Dangerous:   true,
			InputSchema: objectSchema(map[string]any{
				"subject":     map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
				"priority":    map[string]any{"type": "string", "enum": []string{"low", "normal", "high", "urgent"}},
				"account_id":  map[string]any{"type": "string"},
				"contact_id":  map[string]any{"type": "string"},
			}, []string{"subject"}),
			Handler: func(ctx context.Context, p *rbac.Principal, args map[string]any) (any, error) {
				subject, err := requiredString(args, "subject")
				if err != nil {
					return nil, err
				}
				in := crm.TicketInput{Subject: subject, Priority: stringArg(args, "priority", "normal"), Channel: "api"}
				if v := stringArg(args, "description", ""); v != "" {
					in.Description = &v
				}
				if raw := stringArg(args, "account_id", ""); raw != "" {
					id, err := uuid.Parse(raw)
					if err != nil {
						return nil, httpx.ValidationError(map[string]string{"account_id": "must be a UUID"})
					}
					in.AccountID = &id
				}
				if raw := stringArg(args, "contact_id", ""); raw != "" {
					id, err := uuid.Parse(raw)
					if err != nil {
						return nil, httpx.ValidationError(map[string]string{"contact_id": "must be a UUID"})
					}
					in.ContactID = &id
				}
				return r.crm.CreateTicket(ctx, p, in)
			},
		},
		{
			Name:        "update_deal_stage",
			Description: "Move a deal to another stage. Requires deals.change_stage and confirmation.",
			Permission:  rbac.PermDealsChangeStage,
			Dangerous:   true,
			InputSchema: objectSchema(map[string]any{
				"deal_id":  map[string]any{"type": "string"},
				"stage_id": map[string]any{"type": "string"},
				"note":     map[string]any{"type": "string"},
			}, []string{"deal_id", "stage_id"}),
			Handler: func(ctx context.Context, p *rbac.Principal, args map[string]any) (any, error) {
				dealID, err := uuidArg(args, "deal_id")
				if err != nil {
					return nil, err
				}
				stageID, err := uuidArg(args, "stage_id")
				if err != nil {
					return nil, err
				}
				return r.crm.MoveDealStage(ctx, p, dealID, stageID, stringArg(args, "note", ""))
			},
		},
		{
			Name:        "get_report",
			Description: "Return a permitted report: sales summary, support summary or a pipeline report.",
			Permission:  rbac.PermReportsRead,
			InputSchema: objectSchema(map[string]any{
				"report":      map[string]any{"type": "string", "enum": []string{"sales_summary", "support_summary", "pipeline"}},
				"pipeline_id": map[string]any{"type": "string"},
				"from":        map[string]any{"type": "string", "description": "RFC3339"},
				"to":          map[string]any{"type": "string", "description": "RFC3339"},
			}, []string{"report"}),
			Handler: func(ctx context.Context, p *rbac.Principal, args map[string]any) (any, error) {
				report, err := requiredString(args, "report")
				if err != nil {
					return nil, err
				}
				switch report {
				case "sales_summary":
					var from, to *time.Time
					if raw := stringArg(args, "from", ""); raw != "" {
						t, err := time.Parse(time.RFC3339, raw)
						if err != nil {
							return nil, httpx.ValidationError(map[string]string{"from": "must be RFC3339"})
						}
						from = &t
					}
					if raw := stringArg(args, "to", ""); raw != "" {
						t, err := time.Parse(time.RFC3339, raw)
						if err != nil {
							return nil, httpx.ValidationError(map[string]string{"to": "must be RFC3339"})
						}
						to = &t
					}
					return r.crm.SalesSummary(ctx, p, from, to)
				case "support_summary":
					return r.crm.SupportSummary(ctx, p)
				case "pipeline":
					id, err := uuidArg(args, "pipeline_id")
					if err != nil {
						return nil, err
					}
					return r.crm.PipelineReportFor(ctx, p, id)
				default:
					return nil, httpx.ValidationError(map[string]string{
						"report": "must be sales_summary, support_summary or pipeline"})
				}
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Argument helpers
// ---------------------------------------------------------------------------

func objectSchema(props map[string]any, required []string) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func requiredString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", httpx.ValidationError(map[string]string{key: "is required"})
	}
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", httpx.ValidationError(map[string]string{key: "must be a non-empty string"})
	}
	return strings.TrimSpace(s), nil
}

func stringArg(args map[string]any, key, def string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	s, ok := v.(string)
	if !ok {
		return def
	}
	return strings.TrimSpace(s)
}

func intArg(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case json.Number:
		n, err := t.Int64()
		if err != nil {
			return def
		}
		return int(n)
	}
	return def
}

func uuidArg(args map[string]any, key string) (uuid.UUID, error) {
	raw, err := requiredString(args, key)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, httpx.ValidationError(map[string]string{key: "must be a UUID"})
	}
	return id, nil
}
