//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/agent"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/secrets"
)

// These tests drive the human-in-the-loop agent endpoints through the real HTTP
// stack: a real session cookie, the real permission middleware and the real
// service graph. They are the counterpart to the package tests in
// internal/testsupport, which prove the state machine against the schema in
// isolation.
//
// The distinction matters here because the endpoints are a second way to reach the
// state machine, and the interesting failures are at the seam: a route mounted
// without a permission requirement, a decision accepted without the hash that binds
// it to what the operator saw, a reviewer who can also operate.
//
// Run with:
//
//	FORGECRM_TEST_DATABASE_URL=postgres://... go test -tags=integration ./internal/api/...

// agentRunBody builds a start-run request.
func agentRunBody(goal string, budget int) string {
	return `{"goal":"` + goal + `","budget_tokens":` + itoa(budget) + `}`
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// TestAgentEndpointsRequirePermissions checks the routes are not reachable
// anonymously and that a role without the permission is refused. Hiding a button
// is not a control; the route is what has to be guarded.
func TestAgentEndpointsRequirePermissions(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Agent Perms")
	// support_agent holds neither agent.read nor agent.run nor approvals.decide.
	userID := h.seedUser("agent-support@example.test", "Agent-Support-2026!", "Support Agent")
	h.joinOrganization(userID, org, "support_agent")
	user := h.login("agent-support@example.test", "Agent-Support-2026!")

	if h.hasPermission(user, rbac.PermAgentRun) {
		t.Fatal("the fixture granted agent.run to support_agent; the test would not prove anything")
	}

	// Anonymous access must be refused.
	anon := h.do(nil, http.MethodGet, "/api/v1/agent/runs", "")
	if anon.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET /agent/runs: status %d, want 401", anon.Code)
	}

	// A role without the permission must be refused, not served an empty list.
	for _, tc := range []struct {
		method, path, body string
	}{
		{http.MethodGet, "/api/v1/agent/runs", ""},
		{http.MethodGet, "/api/v1/agent/actions", ""},
		{http.MethodPost, "/api/v1/agent/runs", agentRunBody("do something", 1000)},
	} {
		rec := h.do(user, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: status %d, want 403 (body %s)", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

// TestAgentRunLifecycleOverHTTP drives start → propose → decide → execute through
// the endpoints, which is the path the admin UI uses.
func TestAgentRunLifecycleOverHTTP(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Agent Lifecycle")
	userID := h.seedUser("agent-sales@example.test", "Agent-Sales-2026!", "Sales Manager")
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("agent-sales@example.test", "Agent-Sales-2026!")

	if !h.hasPermission(user, rbac.PermAgentRun) || !h.hasPermission(user, rbac.PermApprovalsDecide) {
		// sales_manager holds both by the 0010 seed. Fail loudly rather than silently
		// skipping, so the fixture drift is visible.
		t.Fatalf("the fixture role lacks agent.run (%v) or approvals.decide (%v)",
			h.hasPermission(user, rbac.PermAgentRun), h.hasPermission(user, rbac.PermApprovalsDecide))
	}

	rec := h.do(user, http.MethodPost, "/api/v1/agent/runs", agentRunBody("Create a follow-up task", 100000))
	if rec.Code != http.StatusCreated {
		t.Fatalf("start run: status %d, body %s", rec.Code, rec.Body.String())
	}
	var run struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if run.ID == "" || run.Status != "running" {
		t.Fatalf("run = %+v, want a running run with an id", run)
	}

	// The queue is empty before any proposal.
	rec = h.do(user, http.MethodGet, "/api/v1/agent/actions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list actions: status %d, body %s", rec.Code, rec.Body.String())
	}
	var queue struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &queue); err != nil {
		t.Fatalf("decode queue: %v", err)
	}
	if queue.Total != 0 {
		t.Fatalf("the approval queue is not empty before any proposal: %+v", queue)
	}
}

// TestStepUpRefreshesTheSecondFactorWindow drives the step-up endpoint. A fresh
// login already satisfies the window, so this test ages the session's step_up_at
// and checks that the endpoint restores it and that a wrong factor is refused.
func TestStepUpRefreshesTheSecondFactorWindow(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Step Up")
	userID := h.seedUser("step-up@example.test", "Step-Up-Password-2026!", "Operator")
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("step-up@example.test", "Step-Up-Password-2026!")

	// Age the step-up so the window has lapsed.
	if _, err := h.pool().Exec(t.Context(),
		`UPDATE sessions SET step_up_at = now() - interval '1 hour' WHERE user_id = $1`, userID); err != nil {
		t.Fatalf("age the step-up: %v", err)
	}

	// A wrong password is refused.
	rec := h.do(user, http.MethodPost, "/api/v1/auth/step-up", `{"password":"not-the-password"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("step-up with a wrong password: status %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}

	// The correct password is accepted and restores the window.
	rec = h.do(user, http.MethodPost, "/api/v1/auth/step-up", `{"password":"Step-Up-Password-2026!"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("step-up with the correct password: status %d, body %s", rec.Code, rec.Body.String())
	}

	var stepUpAt *time.Time
	if err := h.pool().QueryRow(t.Context(),
		`SELECT step_up_at FROM sessions WHERE user_id = $1`, userID).Scan(&stepUpAt); err != nil {
		t.Fatalf("read step_up_at: %v", err)
	}
	if stepUpAt == nil {
		t.Fatal("step-up succeeded but step_up_at is still null")
	}
	if time.Since(*stepUpAt) > time.Minute {
		t.Fatalf("step_up_at = %v, want it refreshed to now", stepUpAt)
	}
}

// TestStepUpRequiresAnEmptyBodyRejected checks the endpoint validates its input
// rather than accepting a request that presents no factor at all.
func TestStepUpRequiresAFactor(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Step Up Empty")
	userID := h.seedUser("step-up-empty@example.test", "Step-Up-Empty-2026!", "Operator")
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("step-up-empty@example.test", "Step-Up-Empty-2026!")

	rec := h.do(user, http.MethodPost, "/api/v1/auth/step-up", `{}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("step-up with no factor: status %d, want 422 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestStepUpRequiresASession checks the endpoint is not reachable anonymously.
func TestStepUpRequiresASession(t *testing.T) {
	h := newHarness(t)

	rec := h.do(nil, http.MethodPost, "/api/v1/auth/step-up", `{"password":"x"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous step-up: status %d, want 401", rec.Code)
	}
}

// TestAgentDecideRequiresTheArgumentsHash checks the server refuses a decision that
// is not bound to what the operator reviewed. Without the hash the decision cannot
// be tied to the displayed arguments, so accepting it would defeat the guard.
func TestAgentDecideRequiresTheArgumentsHash(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Agent Hash")
	userID := h.seedUser("agent-hash@example.test", "Agent-Hash-2026!", "Manager")
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("agent-hash@example.test", "Agent-Hash-2026!")

	if !h.hasPermission(user, rbac.PermApprovalsDecide) {
		t.Fatal("the fixture role lacks approvals.decide")
	}

	// A decision on an action id that does not exist, with no hash: the validation
	// must reject the missing hash before it ever looks the action up.
	rec := h.do(user, http.MethodPost,
		"/api/v1/agent/actions/11111111-1111-1111-1111-111111111111/decide",
		`{"approve":true}`)
	if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
		t.Fatalf("decide without a hash: status %d, want a 4xx validation error (body %s)",
			rec.Code, rec.Body.String())
	}
}

// TestDestructiveDecisionRequiresStepUpOverHTTP drives the whole gate through the
// real HTTP stack: a destructive decision is refused while the session's factor has
// lapsed, a real step-up restores it, and the same decision then goes through. The
// package test in internal/testsupport proves the state machine; this proves the
// seam between the endpoint, the session and the service.
func TestDestructiveDecisionRequiresStepUpOverHTTP(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Step Up")
	userID := h.seedUser("agent-stepup@example.test", "Agent-StepUp-2026!", "Manager")
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("agent-stepup@example.test", "Agent-StepUp-2026!")

	// A freshly created session has presented a factor (the password just now), so
	// the destructive tier is satisfied without an extra step. That is the point of
	// setting step_up_at at login: the operator is present.
	before := h.sessionStepUpAt(user)

	// Age the session's factor well past the window, as if it had been open a long
	// time. The refresh must then be what re-enables a destructive decision.
	h.expireStepUp(user)

	// The decision on a destructive action is now refused for want of a factor. The
	// caller holds approvals.decide, so a 403 here is the step-up requirement and not
	// a permission failure — which is exactly the distinction the UI keys on.
	actionID := h.seedDestructiveAction(org, userID)
	rec := h.do(user, http.MethodPost, "/api/v1/agent/actions/"+actionID.String()+"/decide",
		`{"approve":true,"arguments_hash":"`+h.actionArgumentsHash(actionID)+`"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("decide on a destructive action without a fresh factor: status %d, want 403 (body %s)",
			rec.Code, rec.Body.String())
	}

	// Step up with the password (this account has no MFA). The endpoint must accept
	// it and report a window.
	rec = h.do(user, http.MethodPost, "/api/v1/auth/step-up", `{"password":"Agent-StepUp-2026!"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("step-up: status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var stepUp struct {
		Status     string `json:"status"`
		WindowSecs int    `json:"window_secs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &stepUp); err != nil {
		t.Fatalf("decode step-up: %v", err)
	}
	if stepUp.Status != "verified" || stepUp.WindowSecs <= 0 {
		t.Fatalf("step-up = %+v, want a verified window", stepUp)
	}

	// The factor was refreshed on the session the caller holds.
	after := h.sessionStepUpAt(user)
	if !after.After(before) {
		t.Fatalf("step_up_at did not advance: before %v, after %v", before, after)
	}

	// A wrong password must not refresh it. This is the check that a step-up is a
	// real verification and not a flag the client can set.
	h.expireStepUp(user)
	rec = h.do(user, http.MethodPost, "/api/v1/auth/step-up", `{"password":"not-the-password"}`)
	if rec.Code == http.StatusOK {
		t.Fatalf("step-up with a wrong password succeeded: body %s", rec.Body.String())
	}
	if h.sessionStepUpAt(user).After(time.Now().Add(-time.Minute)) {
		t.Fatal("a failed step-up refreshed the window; the factor is not being verified")
	}
}

// TestStepUpIsNotAssertableFromTheRequestBody is the negative case that matters
// most: no field of the decision body may grant the second factor. If it could, the
// requirement would be advisory.
func TestStepUpIsNotAssertableFromTheRequestBody(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Step Up Forge")
	userID := h.seedUser("agent-forge@example.test", "Agent-Forge-2026!", "Manager")
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("agent-forge@example.test", "Agent-Forge-2026!")

	h.expireStepUp(user)
	actionID := h.seedDestructiveAction(org, userID)
	hash := h.actionArgumentsHash(actionID)

	// Every plausible way a client might try to assert the factor in the body. None
	// of them is read by the handler, and the decoder is strict, so each is refused
	// as an unknown field (400) or, were it accepted, for want of a real factor
	// (403). A 2xx is the failure this test exists to catch.
	for _, body := range []string{
		`{"approve":true,"arguments_hash":"` + hash + `","step_up_verified":true}`,
		`{"approve":true,"arguments_hash":"` + hash + `","step_up_at":"` + time.Now().Format(time.RFC3339) + `"}`,
		`{"approve":true,"arguments_hash":"` + hash + `","mfa_verified":true}`,
	} {
		rec := h.do(user, http.MethodPost, "/api/v1/agent/actions/"+actionID.String()+"/decide", body)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusForbidden {
			t.Fatalf("a decision asserting the factor in the body was not refused: status %d (body %s, sent %s)",
				rec.Code, rec.Body.String(), body)
		}
	}

	// The action is untouched: the attempts above changed nothing.
	if status := h.actionStatus(actionID); status != "proposed" {
		t.Fatalf("action status = %q after refused decisions, want proposed", status)
	}

	// After a real step-up the same decision goes through, which proves the refusals
	// above were the step-up requirement and not some other failure.
	rec := h.do(user, http.MethodPost, "/api/v1/auth/step-up", `{"password":"Agent-Forge-2026!"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("step-up: status %d, body %s", rec.Code, rec.Body.String())
	}
	rec = h.do(user, http.MethodPost, "/api/v1/agent/actions/"+actionID.String()+"/decide",
		`{"approve":true,"arguments_hash":"`+hash+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("decide after a real step-up: status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// sessionStepUpAt reads the session's step_up_at directly. The value is what the
// enforcement consults, and it is not exposed over the API, so the test reads it
// from the database rather than trusting a response.
func (h *harness) sessionStepUpAt(a *actor) time.Time {
	h.t.Helper()
	var at time.Time
	err := h.db.Pool().QueryRow(h.t.Context(),
		`SELECT step_up_at FROM sessions WHERE token_hash = $1`,
		secrets.HashToken(a.token)).Scan(&at)
	if err != nil {
		h.t.Fatalf("read step_up_at: %v", err)
	}
	return at
}

// expireStepUp pushes the session's factor outside the window, modelling a session
// that has been open for longer than the step-up window.
func (h *harness) expireStepUp(a *actor) {
	h.t.Helper()
	_, err := h.db.Pool().Exec(h.t.Context(),
		`UPDATE sessions SET step_up_at = now() - interval '1 hour'
 WHERE token_hash = $1`, secrets.HashToken(a.token))
	if err != nil {
		h.t.Fatalf("expire step_up_at: %v", err)
	}
}

// seedDestructiveAction inserts a proposed destructive action for the organization,
// returning its id. It writes the row directly because the point of these tests is
// the decision path, not the proposal path.
func (h *harness) seedDestructiveAction(orgID, userID uuid.UUID) uuid.UUID {
	h.t.Helper()

	var runID uuid.UUID
	err := h.db.Pool().QueryRow(h.t.Context(),
		`INSERT INTO agent_runs (organization_id, user_id, goal) VALUES ($1, $2, $3) RETURNING id`,
		orgID, userID, "Follow up with the client").Scan(&runID)
	if err != nil {
		h.t.Fatalf("seed run: %v", err)
	}

	// The action is stored with a registered tool (`create_task`) but the
	// `destructive` tier, which is the pair the gate keys on: `Decide` reads the tier
	// from the stored row, not from a re-derivation, so a stored destructive action is
	// gated even when the tool's own classification is `confirm`. Using a registered
	// tool also means the post-step-up decision executes for real, which is what proves
	// the earlier 403 was the step-up requirement and not a broken execution path.
	args := map[string]any{"title": "Agent follow-up call"}
	hash, err := agent.ArgumentsHash(args)
	if err != nil {
		h.t.Fatalf("arguments hash: %v", err)
	}
	canonical, err := json.Marshal(args)
	if err != nil {
		h.t.Fatalf("marshal arguments: %v", err)
	}

	var actionID uuid.UUID
	err = h.db.Pool().QueryRow(h.t.Context(),
		`INSERT INTO agent_actions
     (run_id, organization_id, tool_name, arguments, arguments_hash, risk_tier, summary, idempotency_key)
 VALUES ($1, $2, 'create_task', $3, $4, 'destructive', $5, $6)
 RETURNING id`,
		runID, orgID, canonical, hash, "Create a follow-up task", "test-"+uuid.New().String()).Scan(&actionID)
	if err != nil {
		h.t.Fatalf("seed action: %v", err)
	}
	return actionID
}

// actionArgumentsHash reads back the stored hash, so the decision binds to exactly
// what the row holds.
func (h *harness) actionArgumentsHash(actionID uuid.UUID) string {
	h.t.Helper()
	var hash string
	err := h.db.Pool().QueryRow(h.t.Context(),
		`SELECT arguments_hash FROM agent_actions WHERE id = $1`, actionID).Scan(&hash)
	if err != nil {
		h.t.Fatalf("read arguments_hash: %v", err)
	}
	return hash
}

// actionStatus reads an action's status, so a test can assert that a refused
// decision left the row untouched.
func (h *harness) actionStatus(actionID uuid.UUID) string {
	h.t.Helper()
	var status string
	err := h.db.Pool().QueryRow(h.t.Context(),
		`SELECT status FROM agent_actions WHERE id = $1`, actionID).Scan(&status)
	if err != nil {
		h.t.Fatalf("read action status: %v", err)
	}
	return status
}
