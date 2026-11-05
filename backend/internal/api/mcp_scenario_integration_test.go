//go:build integration

// The end-to-end scenario for an autonomous agent: a headless client authenticates
// with a bearer credential — the only way an agent can connect, since it holds no
// session — discovers the tool catalogue, performs a read, performs each of the
// three mutating operations, and then loses access when the credential is revoked.
//
// The other MCP tests prove each property in isolation. This one proves they hold
// together in the order an agent actually uses them, and — the part no unit test
// can show — that the whole path Agent -> MCP -> RBAC -> CRM lands in the audit
// trail. It runs against the real router, the real permission middleware and the
// real database.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// mcpBearerJSONRPC sends one JSON-RPC message authenticated by a credential rather
// than a session cookie, as an MCP client does, and decodes the reply.
func (h *harness) mcpBearerJSONRPC(token, body string) (map[string]any, int) {
	h.t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.server.ServeHTTP(rec, req)
	var decoded map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
			h.t.Fatalf("decode MCP response: %v (status %d, body %s)", err, rec.Code, rec.Body.String())
		}
	}
	return decoded, rec.Code
}

// mcpResultObject unwraps a tools/call reply into the object the tool returned.
// The result is content, not an error, so a tool failure still has to be read out
// of the text; this fails the test only when the transport itself errored.
func mcpResultObject(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	if e, hasErr := resp["error"]; hasErr {
		t.Fatalf("the tool call returned a protocol error: %v", e)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("the reply has no result object: %v", resp)
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("the result carries no content: %v", resp)
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	var obj map[string]any
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		t.Fatalf("the tool result text is not an object: %v (%q)", err, text)
	}
	return obj
}

// mcpToolNames reads the tool names out of a tools/list reply.
func mcpToolNames(t *testing.T, resp map[string]any) []string {
	t.Helper()
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list has no result: %v", resp)
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("tools/list has no tools array: %v", resp)
	}
	names := make([]string, 0, len(tools))
	for _, raw := range tools {
		if tm, ok := raw.(map[string]any); ok {
			if name, ok := tm["name"].(string); ok {
				names = append(names, name)
			}
		}
	}
	return names
}

// seedPipelineAndStages creates a default pipeline with two stages and returns
// their ids, so update_deal_stage has a target stage to move into.
func (c *credHarness) seedPipelineAndStages(t *testing.T) (pipelineID, stageA, stageB uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	if err := c.pool().QueryRow(ctx,
		`INSERT INTO pipelines (organization_id, name, is_default) VALUES ($1,$2,true) RETURNING id`,
		c.org, "Scenario Pipeline").Scan(&pipelineID); err != nil {
		t.Fatalf("seed pipeline: %v", err)
	}
	if err := c.pool().QueryRow(ctx,
		`INSERT INTO pipeline_stages (organization_id, pipeline_id, name, position, probability)
		 VALUES ($1,$2,'Qualified',0,20) RETURNING id`, c.org, pipelineID).Scan(&stageA); err != nil {
		t.Fatalf("seed stage A: %v", err)
	}
	if err := c.pool().QueryRow(ctx,
		`INSERT INTO pipeline_stages (organization_id, pipeline_id, name, position, probability)
		 VALUES ($1,$2,'Negotiation',1,80) RETURNING id`, c.org, pipelineID).Scan(&stageB); err != nil {
		t.Fatalf("seed stage B: %v", err)
	}
	return pipelineID, stageA, stageB
}

// seedDeal inserts a deal in the given stage.
func (c *credHarness) seedDeal(t *testing.T, pipelineID, stageID, accountID uuid.UUID, title string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := c.pool().QueryRow(context.Background(),
		`INSERT INTO deals (organization_id, pipeline_id, stage_id, account_id, title, amount)
		 VALUES ($1,$2,$3,$4,$5,1000) RETURNING id`,
		c.org, pipelineID, stageID, accountID, title).Scan(&id); err != nil {
		t.Fatalf("seed deal: %v", err)
	}
	return id
}

// countAudit counts audit entries for an action in this organization.
func (c *credHarness) countAudit(t *testing.T, action string) int {
	t.Helper()
	var n int
	if err := c.pool().QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE organization_id=$1 AND action=$2`, c.org, action).Scan(&n); err != nil {
		t.Fatalf("count audit %q: %v", action, err)
	}
	return n
}

// countAllowedToolInvocations counts successful ai.tool_invoked entries for a tool.
func (c *credHarness) countAllowedToolInvocations(t *testing.T, tool string) int {
	t.Helper()
	var n int
	if err := c.pool().QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log
		 WHERE organization_id=$1 AND action='ai.tool_invoked' AND entity_id=$2
		   AND metadata->>'allowed'='true'`, c.org, tool).Scan(&n); err != nil {
		t.Fatalf("count tool invocations for %q: %v", tool, err)
	}
	return n
}

// TestMCPScenarioAgentCreatesViaBearerCredentialAndIsAudited walks the full path a
// real agent takes: connect with a credential, list tools, read, then create a
// task, open a ticket and move a deal — each with the confirmation a mutation
// requires — and finally confirm that revoking the credential cuts access off.
func TestMCPScenarioAgentCreatesViaBearerCredentialAndIsAudited(t *testing.T) {
	c := newCredHarness(t)

	// A least-privilege credential: exactly the reads and writes the scenario uses,
	// and deliberately NOT tickets.read or deals.read, so the boundary can be shown.
	token, credID := c.mint(t, "scenario agent", []string{
		"accounts.read", "activities.read", "ai.tools.execute",
		"tasks.create", "tickets.create", "deals.change_stage",
	})

	account := c.seedAccount(c.org, "Scenario Client")
	c.seedActivity(c.org, account, "note", "Kickoff call")
	acct := account
	c.seedTask(c.org, &acct, "Existing task")
	pipelineID, stageA, stageB := c.seedPipelineAndStages(t)
	deal := c.seedDeal(t, pipelineID, stageA, account, "Scenario Deal")

	// 1. Discovery: the agent sees every tool.
	listResp, code := c.mcpBearerJSONRPC(token, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if code != http.StatusOK {
		t.Fatalf("tools/list with a credential: status %d", code)
	}
	names := mcpToolNames(t, listResp)
	if len(names) != 8 {
		t.Errorf("the agent discovered %d tools, want 8: %v", len(names), names)
	}

	// 2. Read: account_summary returns what the credential may read and withholds the
	// rest. It holds activities.read but not deals.read or tickets.read, so the
	// summary must carry recent_activity and must NOT carry deals or tickets — a
	// tool that leaked them would widen the credential's scope.
	sumResp, code := c.mcpBearerJSONRPC(token,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"account_summary","arguments":{"account_id":"`+account.String()+`"}}}`)
	if code != http.StatusOK {
		t.Fatalf("account_summary: status %d", code)
	}
	summary := mcpResultObject(t, sumResp)
	if _, ok := summary["account"]; !ok {
		t.Errorf("account_summary did not return the account: %v", summary)
	}
	if _, ok := summary["recent_activity"]; !ok {
		t.Errorf("account_summary omitted recent_activity though activities.read was granted: %v", summary)
	}
	if _, leaked := summary["deals"]; leaked {
		t.Errorf("account_summary leaked deals without deals.read: %v", summary)
	}
	if _, leaked := summary["tickets"]; leaked {
		t.Errorf("account_summary leaked tickets without tickets.read: %v", summary)
	}

	// 3. create_task, with the confirmation a mutation requires.
	if n := c.countAudit(t, "tasks.create"); n != 0 {
		t.Fatalf("the organization already has %d task-create audit entries; the fixture is not clean", n)
	}
	taskResp, code := c.mcpBearerJSONRPC(token,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create_task","arguments":{"title":"Agent task","priority":"high","confirm":true}}}`)
	if code != http.StatusOK {
		t.Fatalf("create_task: status %d", code)
	}
	if _, hasErr := taskResp["error"]; hasErr {
		t.Fatalf("create_task returned an error: %v", taskResp["error"])
	}
	var taskCount int
	if err := c.pool().QueryRow(context.Background(),
		`SELECT count(*) FROM tasks WHERE organization_id=$1 AND title='Agent task'`, c.org).Scan(&taskCount); err != nil {
		t.Fatalf("count the created task: %v", err)
	}
	if taskCount != 1 {
		t.Errorf("create_task wrote %d tasks, want 1", taskCount)
	}

	// 4. create_ticket, with confirmation.
	ticketResp, code := c.mcpBearerJSONRPC(token,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"create_ticket","arguments":{"subject":"Agent ticket","priority":"urgent","confirm":true}}}`)
	if code != http.StatusOK {
		t.Fatalf("create_ticket: status %d", code)
	}
	if _, hasErr := ticketResp["error"]; hasErr {
		t.Fatalf("create_ticket returned an error: %v", ticketResp["error"])
	}
	var ticketCount int
	if err := c.pool().QueryRow(context.Background(),
		`SELECT count(*) FROM tickets WHERE organization_id=$1 AND subject='Agent ticket'`, c.org).Scan(&ticketCount); err != nil {
		t.Fatalf("count the created ticket: %v", err)
	}
	if ticketCount != 1 {
		t.Errorf("create_ticket wrote %d tickets, want 1", ticketCount)
	}

	// 5. update_deal_stage, with confirmation. The deal moves from stage A to B.
	moveResp, code := c.mcpBearerJSONRPC(token,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"update_deal_stage","arguments":{"deal_id":"`+deal.String()+`","stage_id":"`+stageB.String()+`","note":"advanced by agent","confirm":true}}}`)
	if code != http.StatusOK {
		t.Fatalf("update_deal_stage: status %d", code)
	}
	if _, hasErr := moveResp["error"]; hasErr {
		t.Fatalf("update_deal_stage returned an error: %v", moveResp["error"])
	}
	var gotStage uuid.UUID
	if err := c.pool().QueryRow(context.Background(),
		`SELECT stage_id FROM deals WHERE id=$1`, deal).Scan(&gotStage); err != nil {
		t.Fatalf("read back the deal stage: %v", err)
	}
	if gotStage != stageB {
		t.Errorf("the deal is in stage %s, want %s", gotStage, stageB)
	}

	// 6. Audit: the Agent -> MCP -> RBAC -> CRM path is recorded. Each tool call
	// leaves an ai.tool_invoked entry attributed to the agent, and each mutation
	// leaves the domain action the CRM service writes.
	for _, tool := range []string{"account_summary", "create_task", "create_ticket", "update_deal_stage"} {
		if n := c.countAllowedToolInvocations(t, tool); n < 1 {
			t.Errorf("no allowed ai.tool_invoked audit entry for %q", tool)
		}
	}
	for _, action := range []string{"tasks.create", "tickets.create", "deals.change_stage"} {
		if n := c.countAudit(t, action); n < 1 {
			t.Errorf("the mutation left no %q entry in the audit trail", action)
		}
	}
	var agentActorType string
	if err := c.pool().QueryRow(context.Background(),
		`SELECT actor_type FROM audit_log
		 WHERE organization_id=$1 AND action='ai.tool_invoked' AND entity_id='create_task'
		 ORDER BY id DESC LIMIT 1`, c.org).Scan(&agentActorType); err != nil {
		t.Fatalf("read the actor type of the tool invocation: %v", err)
	}
	if agentActorType != "ai_agent" {
		t.Errorf("the tool invocation is attributed to %q, want %q", agentActorType, "ai_agent")
	}

	// 7. Revocation cuts the agent off at the HTTP boundary.
	if rec := c.do(c.admin, http.MethodDelete, "/api/v1/mcp/credentials/"+credID.String(), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke the credential: status %d", rec.Code)
	}
	if _, code := c.mcpBearerJSONRPC(token, `{"jsonrpc":"2.0","id":6,"method":"tools/list"}`); code != http.StatusUnauthorized {
		t.Errorf("a revoked credential still reached the protocol: status %d, want 401", code)
	}
}

// TestMCPAccountSummaryIncludesSectionsWhenPermitted is the positive control for
// the boundary the scenario test checks. Without it, a bug that dropped the deals
// and tickets sections unconditionally would still pass the scenario's
// "not leaked" assertions. Here the same tool, on the same kind of account, is
// given a credential that holds the reads — and the sections must appear.
func TestMCPAccountSummaryIncludesSectionsWhenPermitted(t *testing.T) {
	c := newCredHarness(t)
	token, _ := c.mint(t, "broad reader", []string{
		"accounts.read", "deals.read", "tickets.read", "activities.read", "ai.tools.execute",
	})

	account := c.seedAccount(c.org, "Broad Client")
	c.seedActivity(c.org, account, "note", "Called")
	pipelineID, stageA, _ := c.seedPipelineAndStages(t)
	c.seedDeal(t, pipelineID, stageA, account, "Broad Deal")
	if _, err := c.pool().Exec(context.Background(),
		`INSERT INTO tickets (organization_id, account_id, subject) VALUES ($1,$2,'Broad Ticket')`,
		c.org, account); err != nil {
		t.Fatalf("seed ticket: %v", err)
	}

	resp, code := c.mcpBearerJSONRPC(token,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"account_summary","arguments":{"account_id":"`+account.String()+`"}}}`)
	if code != http.StatusOK {
		t.Fatalf("account_summary: status %d", code)
	}
	summary := mcpResultObject(t, resp)
	for _, section := range []string{"deals", "tickets", "recent_activity"} {
		if _, ok := summary[section]; !ok {
			t.Errorf("account_summary omitted %q though the credential held the read permission: %v", section, summary)
		}
	}
}

// TestMCPEveryMutatingToolRefusesWithoutConfirmation covers all three write tools,
// not just create_task. The scenario test shows each mutation succeeding with
// confirm=true; this one shows the other half of the rule — that a missing
// acknowledgement is a refusal, not a silent write — for the whole set. A guard
// that held for create_task and leaked on update_deal_stage would be a mutation an
// agent could perform by omission.
func TestMCPEveryMutatingToolRefusesWithoutConfirmation(t *testing.T) {
	c := newCredHarness(t)

	// Least privilege for the scenario: the three write permissions plus the read
	// needed to identify the target, and deliberately no confirm bypass.
	token, _ := c.mint(t, "unconfirmed agent", []string{
		"accounts.read", "ai.tools.execute",
		"tasks.create", "tickets.create", "deals.change_stage",
	})

	account := c.seedAccount(c.org, "Confirm Boundary Client")
	pipelineID, stageA, stageB := c.seedPipelineAndStages(t)
	deal := c.seedDeal(t, pipelineID, stageA, account, "Confirm Boundary Deal")

	calls := []struct {
		tool string
		args string
	}{
		{"create_task", `{"title":"Should not be written"}`},
		{"create_ticket", `{"subject":"Should not be written"}`},
		{"update_deal_stage", `{"deal_id":"` + deal.String() + `","stage_id":"` + stageB.String() + `"}`},
	}
	for _, call := range calls {
		resp, code := c.mcpBearerJSONRPC(token,
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+call.tool+`","arguments":`+call.args+`}}`)
		if code != http.StatusOK {
			t.Fatalf("%s without confirmation: transport status %d", call.tool, code)
		}
		if _, hasErr := resp["error"]; !hasErr {
			t.Errorf("%s ran without confirm=true: %v", call.tool, resp)
		}
	}

	// The refusals must be real: no row may have been written and the deal must
	// not have moved. A guard that refused the reply but still performed the
	// write would pass the checks above.
	ctx := context.Background()
	var taskCount, ticketCount int
	if err := c.pool().QueryRow(ctx,
		`SELECT count(*) FROM tasks WHERE organization_id=$1 AND title='Should not be written'`, c.org).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if err := c.pool().QueryRow(ctx,
		`SELECT count(*) FROM tickets WHERE organization_id=$1 AND subject='Should not be written'`, c.org).Scan(&ticketCount); err != nil {
		t.Fatalf("count tickets: %v", err)
	}
	if taskCount != 0 || ticketCount != 0 {
		t.Errorf("a refused mutation wrote data: %d tasks, %d tickets", taskCount, ticketCount)
	}
	var stage uuid.UUID
	if err := c.pool().QueryRow(ctx, `SELECT stage_id FROM deals WHERE id=$1`, deal).Scan(&stage); err != nil {
		t.Fatalf("read the deal stage: %v", err)
	}
	if stage != stageA {
		t.Errorf("a refused update_deal_stage moved the deal to %s, want it to stay in %s", stage, stageA)
	}
}
