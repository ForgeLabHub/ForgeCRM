package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/geff1778/forgecrm/internal/agent"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// mountAgent registers the human-in-the-loop agent routes (ADR 0005).
//
// The two halves are guarded by different permissions. Reading the queue is
// agent.read; deciding is approvals.decide; starting a run is agent.run. A role
// can therefore review without operating, and operate without reviewing.
func (s *Server) mountAgent(r chi.Router) {
	r.Route("/agent", func(r chi.Router) {
		r.With(s.perm(rbac.PermAgentRead)).Get("/runs", s.handleListAgentRuns)
		r.With(s.perm(rbac.PermAgentRead)).Get("/runs/{id}", s.handleGetAgentRun)
		r.With(s.perm(rbac.PermAgentRun)).Post("/runs", s.handleStartAgentRun)
		r.With(s.perm(rbac.PermAgentRun)).Post("/runs/{id}/cancel", s.handleCancelAgentRun)
		r.With(s.perm(rbac.PermAgentRun)).Post("/runs/{id}/tokens", s.handleAddAgentTokens)

		r.With(s.perm(rbac.PermApprovalsRead)).Get("/actions", s.handleListAgentActions)
		r.With(s.perm(rbac.PermApprovalsRead)).Get("/actions/{id}", s.handleGetAgentAction)
		r.With(s.perm(rbac.PermApprovalsRead)).Get("/actions/{id}/approvals", s.handleListAgentApprovals)
		// Deciding requires both: the reviewer right and, for a destructive action,
		// a second factor presented by the deciding session within the step-up
		// window (see /auth/step-up). The factor is resolved from the session, never
		// from the request body.
		r.With(s.perm(rbac.PermApprovalsDecide)).Post("/actions/{id}/decide", s.handleDecideAgentAction)
	})
}

// ---------------------------------------------------------------------------
// Runs
// ---------------------------------------------------------------------------

func (s *Server) handleListAgentRuns(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	pg := httpx.ParsePagination(r)
	status := r.URL.Query().Get("status")
	items, total, err := s.deps.Agent.ListRuns(r.Context(), p.OrganizationID, status, pg.Limit, pg.Offset)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, httpx.NewListResponse(items, total, pg))
}

func (s *Server) handleGetAgentRun(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	run, err := s.deps.Agent.GetRun(r.Context(), p.OrganizationID, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, run)
}

type startAgentRunRequest struct {
	Goal         string `json:"goal"`
	BudgetTokens int    `json:"budget_tokens"`
	ActionCap    int    `json:"action_cap"`
}

// handleStartAgentRun opens a run. The goal is stored as data, never interpreted
// as an instruction by the server: it is the agent's context, and it confers no
// authority.
func (s *Server) handleStartAgentRun(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req startAgentRunRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if len(req.Goal) == 0 || len(req.Goal) > 4000 {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{"goal": "must be between 1 and 4000 characters"}))
		return
	}
	if req.BudgetTokens <= 0 {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{"budget_tokens": "must be greater than zero"}))
		return
	}
	run, err := s.deps.Agent.StartRun(r.Context(), p.OrganizationID, p.UserID, req.Goal, req.BudgetTokens, req.ActionCap)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, run)
}

func (s *Server) handleCancelAgentRun(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := s.deps.Agent.CancelRun(r.Context(), p.OrganizationID, id); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{"status": "cancelled"})
}

type addAgentTokensRequest struct {
	Tokens int `json:"tokens"`
}

// handleAddAgentTokens records token usage and enforces the budget.
func (s *Server) handleAddAgentTokens(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var req addAgentTokensRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	run, err := s.deps.Agent.AddTokens(r.Context(), p.OrganizationID, id, req.Tokens)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, run)
}

// ---------------------------------------------------------------------------
// Actions and approvals
// ---------------------------------------------------------------------------

func (s *Server) handleListAgentActions(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	pg := httpx.ParsePagination(r)
	// The default view is the queue an operator works: proposals awaiting a
	// decision. Any other status must be asked for explicitly.
	status := r.URL.Query().Get("status")
	if status == "" {
		status = agent.ActionProposed
	}
	items, total, err := s.deps.Agent.ListActions(r.Context(), p.OrganizationID, status, pg.Limit, pg.Offset)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, httpx.NewListResponse(items, total, pg))
}

func (s *Server) handleGetAgentAction(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	action, err := s.deps.Agent.GetAction(r.Context(), p.OrganizationID, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, action)
}

func (s *Server) handleListAgentApprovals(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	items, err := s.deps.Agent.ListApprovals(r.Context(), p.OrganizationID, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{"items": items})
}

type decideAgentActionRequest struct {
	// Approve is explicit: an absent field is a rejection, never an approval, so a
	// malformed body cannot approve anything.
	Approve bool `json:"approve"`
	// ArgumentsHash must be the hash of the arguments the operator saw rendered.
	// The server compares it with the stored hash; a mismatch is refused. This is
	// what stops an action being swapped between display and decision.
	ArgumentsHash string `json:"arguments_hash"`
	Comment       string `json:"comment"`
}

// handleDecideAgentAction records a decision and, on approval, executes it.
func (s *Server) handleDecideAgentAction(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var req decideAgentActionRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	// The hash is mandatory. Without it the decision cannot be bound to what the
	// operator saw, and the time-of-check/time-of-use guard would have nothing to
	// compare against.
	if req.ArgumentsHash == "" {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{
			"arguments_hash": "is required: it binds the decision to the action you reviewed",
		}))
		return
	}
	if len(req.Comment) > 2000 {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{"comment": "must be at most 2000 characters"}))
		return
	}

	decider := agent.Decider{UserID: p.UserID, OrganizationID: p.OrganizationID}
	// The step-up factor is a property of the session, resolved here from the
	// authenticated session — never from the request body, where a caller could
	// simply claim it. A destructive action decided from a session that has not
	// presented a second factor within the window is refused by the state machine.
	if sess, ok := sessionFrom(r.Context()); ok {
		decider.StepUpVerified = sess.SteppedUp()
	}
	action, approval, err := s.deps.Agent.Decide(r.Context(), decider, id, agent.Decision{
		Approve:       req.Approve,
		ArgumentsHash: req.ArgumentsHash,
		Comment:       req.Comment,
	})
	if err != nil {
		s.respondAgentDecisionError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{"action": action, "approval": approval})
}

// respondAgentDecisionError maps the HITL domain errors to status codes.
//
// A changed action is a 409 rather than a 400: the request was well formed and
// the operator's decision was valid for what they saw — the conflict is that the
// action moved. A forged signature is a 403: the request is not authorized to
// cause this effect.
func (s *Server) respondAgentDecisionError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, agent.ErrActionChanged):
		httpx.WriteError(w, r, httpx.NewError(http.StatusConflict, httpx.CodeConflict,
			"the action changed since it was displayed; reload and review it again"))
	case errors.Is(err, agent.ErrAlreadyDecided):
		httpx.WriteError(w, r, httpx.NewError(http.StatusConflict, httpx.CodeConflict,
			"the action has already been decided"))
	case errors.Is(err, agent.ErrExpired):
		httpx.WriteError(w, r, httpx.NewError(http.StatusConflict, httpx.CodeConflict,
			"the approval request has expired"))
	case errors.Is(err, agent.ErrBadSignature):
		httpx.WriteError(w, r, httpx.NewError(http.StatusForbidden, httpx.CodeForbidden,
			"the approval signature is not valid"))
	case errors.Is(err, agent.ErrBudgetExceeded):
		httpx.WriteError(w, r, httpx.NewError(http.StatusConflict, httpx.CodeConflict,
			"the run has exhausted its budget"))
	case errors.Is(err, agent.ErrRunNotActive):
		httpx.WriteError(w, r, httpx.NewError(http.StatusConflict, httpx.CodeConflict,
			"the run is not in a state that permits this action"))
	case errors.Is(err, agent.ErrStepUpRequired):
		httpx.WriteError(w, r, httpx.NewError(http.StatusForbidden, httpx.CodeForbidden,
			"a destructive action requires a second authentication factor; re-authenticate and try again"))
	default:
		s.respondDomainError(w, r, err)
	}
}
