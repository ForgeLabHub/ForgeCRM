package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/ai"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// mountAIAssist registers the contextual AI routes.
//
// The entity type is a query parameter, not a path segment, because every path
// parameter in this API is an identifier and a client generating types from the
// document can then treat them uniformly as UUIDs. The route requires
// ai.tools.execute to reach the endpoint at all; the entity's own read permission
// is enforced inside the service, because a single route serves five entity types
// and a path-level permission cannot express that.
func (s *Server) mountAIAssist(r chi.Router) {
	r.Route("/ai/assist", func(r chi.Router) {
		r.With(s.perm(rbac.PermAIToolsExecute)).Post("/{id}", s.handleAIAssist)
		r.With(s.perm(rbac.PermAIRead)).Get("/{id}/context", s.handleAIAssistContext)
	})
}

type aiAssistRequest struct {
	Kind          string `json:"kind"`
	Question      string `json:"question"`
	Refresh       bool   `json:"refresh"`
	RecipientHint string `json:"recipient_hint"`
}

// aiAssistEntity reads the entity type from the query string and the identifier
// from the path, refusing an unknown type rather than defaulting it.
func aiAssistEntity(w http.ResponseWriter, r *http.Request) (ai.EntityType, uuid.UUID, bool) {
	entityType := ai.EntityType(strings.TrimSpace(r.URL.Query().Get("entity_type")))
	switch entityType {
	case ai.EntityAccount, ai.EntityContact, ai.EntityDeal, ai.EntityTicket, ai.EntityTask:
	default:
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{
			"entity_type": "must be one of account, contact, deal, ticket, task",
		}))
		return "", uuid.Nil, false
	}
	entityID, ok := pathUUID(w, r, "id")
	if !ok {
		return "", uuid.Nil, false
	}
	return entityType, entityID, true
}

// handleAIAssist produces contextual assistance for one entity.
func (s *Server) handleAIAssist(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	entityType, entityID, ok := aiAssistEntity(w, r)
	if !ok {
		return
	}
	var req aiAssistRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	kind := ai.Kind(strings.TrimSpace(req.Kind))
	if kind == "" {
		kind = ai.KindSummary
	}
	insight, err := s.deps.AI.Assist(r.Context(), p, ai.AssistRequest{
		EntityType:    entityType,
		EntityID:      entityID,
		Kind:          kind,
		Question:      req.Question,
		Refresh:       req.Refresh,
		RecipientHint: req.RecipientHint,
	})
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, insight)
}

// handleAIAssistContext returns the facts that would be sent to the model, without
// calling it.
//
// This exists so the interface can show "what the assistant can see" before a
// request is made, and so a user can tell whether an answer was based on
// incomplete data. It runs the same assembly as Assist, so it cannot disclose
// more than Assist would send to the model.
func (s *Server) handleAIAssistContext(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	entityType, entityID, ok := aiAssistEntity(w, r)
	if !ok {
		return
	}
	bundle, err := s.deps.AI.Assemble(r.Context(), p, ai.AssistRequest{
		EntityType: entityType,
		EntityID:   entityID,
		Kind:       ai.KindSummary,
	})
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{
		"entity_type": string(entityType),
		"entity_id":   entityID.String(),
		"subject":     bundle.Subject,
		"facts":       bundle.Facts,
		"disclosures": bundle.Disclosures,
		"available":   s.deps.AI.Enabled(),
	})
}
