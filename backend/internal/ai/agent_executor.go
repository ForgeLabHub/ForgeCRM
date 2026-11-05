package ai

import (
	"context"
	"encoding/json"
	"errors"
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
// decider. The adapter reloads the run's original user and the approver at
// execution time, then invokes the tool as the original user, never as a more
// privileged approver.
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

// Execution rechecks both the run owner and the approver. A human approval is
// not authority to expand the run owner's permissions. The tool runs as the
// owner; the existing requirement that the approver also has its permission is
// retained. The ambient execution transaction holds the identity and relevant
// permission rows through the effect, so revocation cannot race the check.
func (e *AgentExecutor) Execution(toolName string) (agent.ExecutionFunc, error) {
	tool, ok := e.tools.Get(toolName)
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", toolName)
	}
	return func(ctx context.Context, action *agent.Action, decider agent.Decider) (any, error) {
		if _, ok := database.TxFromContext(ctx); !ok {
			return nil, errors.New("agent execution requires a transaction")
		}
		if action.OrganizationID != decider.OrganizationID {
			return nil, httpx.ErrNotFound
		}
		var ownerID uuid.UUID
		if err := e.db.Q(ctx).QueryRow(ctx, `SELECT user_id FROM agent_runs WHERE id=$1 AND organization_id=$2 FOR SHARE`, action.RunID, action.OrganizationID).Scan(&ownerID); err != nil {
			if database.IsNoRows(err) {
				return nil, httpx.ErrNotFound
			}
			return nil, fmt.Errorf("resolve agent run owner: %w", err)
		}
		owner, err := e.principalFor(ctx, ownerID, action.OrganizationID, tool.Permission)
		if err != nil {
			return nil, err
		}
		if decider.UserID != ownerID {
			if _, err := e.principalFor(ctx, decider.UserID, action.OrganizationID, tool.Permission); err != nil {
				return nil, err
			}
		}
		var args map[string]any
		if len(action.Arguments) > 0 {
			if err := json.Unmarshal(action.Arguments, &args); err != nil {
				return nil, fmt.Errorf("decode action arguments: %w", err)
			}
		}
		if args == nil {
			args = map[string]any{}
		}
		// The signed approval, not this transport flag, authorizes the write.
		if tool.Dangerous {
			args["confirm"] = true
		}
		return e.tools.Call(ctx, owner, toolName, args)
	}, nil
}

// principalFor locks active identities and one grant of the needed permission
// for the duration of the transaction. Permission lookup is organization-scoped.
func (e *AgentExecutor) principalFor(ctx context.Context, userID, orgID uuid.UUID, required rbac.Permission) (*rbac.Principal, error) {
	const identity = `SELECT u.id, u.email, u.display_name
		FROM users u JOIN memberships m ON m.user_id=u.id AND m.organization_id=$2
		JOIN organizations o ON o.id=m.organization_id
		WHERE u.id=$1 AND u.status='active' AND m.status='active' AND o.status='active'
		FOR SHARE OF u,m,o`
	var id uuid.UUID
	var email, name string
	if err := e.db.Q(ctx).QueryRow(ctx, identity, userID, orgID).Scan(&id, &email, &name); err != nil {
		if database.IsNoRows(err) {
			return nil, httpx.NewError(403, httpx.CodeForbidden, "agent user is not an active member of this organization")
		}
		return nil, fmt.Errorf("resolve agent identity: %w", err)
	}
	const grant = `SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_id=ur.role_id
		WHERE ur.user_id=$1 AND (ur.organization_id IS NULL OR ur.organization_id=$2)
		AND rp.permission_key=$3 LIMIT 1 FOR SHARE OF ur,rp`
	var granted int
	if err := e.db.Q(ctx).QueryRow(ctx, grant, userID, orgID, string(required)).Scan(&granted); err != nil {
		if database.IsNoRows(err) {
			return nil, httpx.NewError(403, httpx.CodeForbidden, "agent user lacks the required tool permission")
		}
		return nil, fmt.Errorf("resolve agent permission: %w", err)
	}
	perms, err := e.rbac.EffectivePermissions(ctx, id, orgID)
	if err != nil {
		return nil, err
	}
	p := rbac.NewPrincipal(id, email, name, orgID, perms)
	if teams, err := e.rbac.TeamIDsForUser(ctx, id, orgID); err == nil {
		p.TeamIDs = teams
	}
	return p, nil
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
