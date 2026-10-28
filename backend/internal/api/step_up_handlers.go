package api

import (
	"net/http"

	"github.com/geff1778/forgecrm/internal/authn"
	"github.com/geff1778/forgecrm/internal/httpx"
)

// stepUpRequest carries the factor an operator presents to authorise a
// destructive agent action.
//
// Which field is used depends on the account: with MFA enabled, `code` (a TOTP or
// recovery code); without it, `password`. Both are accepted so the client does not
// have to know which, and the service decides.
type stepUpRequest struct {
	Code     string `json:"code"`
	Password string `json:"password"`
}

// stepUpResponse is the acknowledgement of a successful step-up.
type stepUpResponse struct {
	Status string `json:"status"`
	// WindowSecs is how long the refreshed step-up remains valid, so the client can
	// tell the operator when they will be asked again.
	WindowSecs int `json:"window_secs"`
}

// handleStepUp re-verifies a factor and refreshes the session's step-up window.
//
// The destructive tier of the agent approval model (ADR 0005) requires that the
// deciding session has presented a second factor recently. This endpoint is how a
// session satisfies that requirement after the window has lapsed. The refresh is
// recorded against the session row, not returned to the client, so the fact of the
// step-up is durable and cannot be asserted by a request body.
func (s *Server) handleStepUp(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	sess, ok := sessionFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, httpx.ErrUnauth)
		return
	}

	var req stepUpRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if req.Code == "" && req.Password == "" {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{
			"code": "provide a multi-factor code or your password",
		}))
		return
	}

	if err := s.deps.Auth.StepUpFactor(r.Context(), sess.ID, p.UserID, req.Code, req.Password); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, stepUpResponse{
		Status:     "verified",
		WindowSecs: int(authn.StepUpWindow.Seconds()),
	})
}
