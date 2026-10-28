package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/agent"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// AgentExecutor adapts the tool registry to the agent runtime's Executor
// interface.
//
// It lives here, not in the agent package, so the dependency runs one way: agent
// knows nothing about tools, and tools know nothing about approvals. The agent
// runtime asks this adapter for an execution and hands it the action and the
// decider; the adapter rebuilds the caller's principal from the database at
// execution time and invokes the tool through the same permission check a human
// request would pass through.
type AgentExecutor struct {
	db    *database.DB
	rbac  *rbac.Service
	crm   *crmService
	tools *ToolRegistry
}

// crmService is the concrete CRM service the registry holds, named locally to
// avoid an import cycle in the signature.
type crmService = interface{}

// NewAgentExecutor builds the adapter.
func NewAgentExecutor(db *database.DB, rbacSvc *rbac.Service, registry *ToolRegistry) *AgentExecutor {
	return &AgentExecutor{db: db, rbac: rbacSvc, tools: registry}
}

// RiskTier classifies a tool for the agent runtime. The mapping is by tool name
// and is the single place the classification lives, so the API and the runtime
// cannot disagree about what is safe.
//
// Reads are safe; writes are confirm; anything that could remove data or change
// access is destructive. The list is explicit rather than derived from the
// `Dangerous` flag, because a tool can be dangerous-but-confirmable (creating a
// task) and the runtime needs to know which.
func RiskTier(toolName string) agent.RiskTier {
	switch toolName {
	case "search_accounts", "account_summary", "list_tasks", "pipeline_overview", "get_report":
		return agent.TierSafe
	case "create_task", "create_ticket", "update_deal_stage":
		return agent.TierConfirm
	default:
		// An unrecognised tool is treated as the most restrictive tier. A new tool
		// therefore cannot reach an agent's hands as safe by omission.
		return agent.TierDestructive
	}
}

// DeniedToolNames returns the tools an agent may never invoke, regardless of
// approval. The agent has no tool that administers the platform, but the list is
// explicit so that adding one later cannot silently grant the agent access to it.
func DeniedToolNames() []string {
	return []string{
		"users.delete",
		"users.update_permissions",
		"roles.update",
		"integrations.rotate_secret",
		"security.configure",
		"system.settings",
	}
}

// Execution resolves a tool name to a function the agent runtime can call.
//
// The returned function re-derives the decider's permissions from the database at
// execution time, not from anything captured at propose time. A role revoked
// between the proposal and the approval therefore takes effect: the tool call is
// authorized against the permission set the user has now, not the one they had
// when the agent first asked. This is the "no stale authority in background work"
// rule applied to agent execution.
func (e *AgentExecutor) Execution(toolName string) (agent.ExecutionFunc, error) {
	tool, ok := e.tools.Get(toolName)
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", toolName)
	}
	return func(ctx context.Context, action *agent.Action, decider agent.Decider) (any, error) {
		principal, err := e.principalFor(ctx, decider)
		if err != nil {
			return nil, err
		}
		// The tool registry performs the permission check itself, so the adapter does
		// not duplicate it. What it guarantees is that the check runs against a
		// freshly resolved principal.
		var args map[string]any
		if len(action.Arguments) > 0 {
			if err := json.Unmarshal(action.Arguments, &args); err != nil {
				return nil, fmt.Errorf("decode action arguments: %w", err)
			}
		}
		if args == nil {
			args = map[string]any{}
		}
		// The confirmation flag is not a security control for the agent path: the
		// approval is. It is set here so a tool that checks it does not reject a
		// call that a human has already approved.
		if tool.Dangerous {
			args["confirm"] = true
		}
		return e.tools.Call(ctx, principal, toolName, args)
	}, nil
}

// principalFor rebuilds the decider's principal from the database.
func (e *AgentExecutor) principalFor(ctx context.Context, d agent.Decider) (*rbac.Principal, error) {
	const q = `
		SELECT u.id, u.email, u.display_name, u.status
		FROM users u
		JOIN memberships m ON m.user_id = u.id AND m.organization_id = $2
		WHERE u.id = $1`
	var (
		id, email, name, status string
	)
	if err := e.db.Q(ctx).QueryRow(ctx, q, d.UserID, d.OrganizationID).
		Scan(&id, &email, &name, &status); err != nil {
		return nil, httpx.NewError(403, httpx.CodeForbidden, "the deciding user is not a member of this organization")
	}
	if status != "active" {
		return nil, httpx.NewError(403, httpx.CodeForbidden, "the deciding user's account is not active")
	}
	userID, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("parse user id: %w", err)
	}
	perms, err := e.rbac.EffectivePermissions(ctx, userID, d.OrganizationID)
	if err != nil {
		return nil, err
	}
	return rbac.NewPrincipal(userID, email, name, d.OrganizationID, perms), nil
}

// ProposeForAgent builds a proposal for a tool call, deriving the tier and
// summary from the tool definition rather than from anything the model supplied.
func (e *AgentExecutor) ProposeForAgent(toolName string, args map[string]any) (agent.RiskTier, string, error) {
	tool, ok := e.tools.Get(toolName)
	if !ok {
		return "", "", fmt.Errorf("unknown tool %q", toolName)
	}
	// The summary is server-written: the tool description plus a compact rendering
	// of the arguments. Model prose never reaches the operator's diff, so the diff
	// cannot be a lie.
	summary := tool.Description
	if len(args) > 0 {
		if b, err := json.Marshal(args); err == nil {
			summary = fmt.Sprintf("%s\nArguments: %s", summary, string(b))
		}
	}
	return RiskTier(toolName), summary, nil
}
