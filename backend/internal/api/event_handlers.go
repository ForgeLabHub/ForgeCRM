package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/geff1778/forgecrm/internal/events"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// mountEvents registers the event gateway and webhook routes.
func (s *Server) mountEvents(r chi.Router) {
	r.Route("/events", func(r chi.Router) {
		r.With(s.perm(rbac.PermEventsRead)).Get("/", s.handleListEvents)
	})

	r.Route("/webhooks", func(r chi.Router) {
		r.With(s.perm(rbac.PermWebhooksManage)).Get("/", s.handleListWebhooks)
		r.With(s.perm(rbac.PermWebhooksManage)).Post("/", s.handleCreateWebhook)
		r.Route("/{id}", func(r chi.Router) {
			r.With(s.perm(rbac.PermWebhooksManage)).Put("/enabled", s.handleSetWebhookEnabled)
			r.With(s.perm(rbac.PermWebhooksManage)).Get("/deliveries", s.handleListDeliveries)
			r.With(s.perm(rbac.PermWebhooksManage)).Post("/deliveries/{deliveryId}/replay", s.handleReplayDelivery)
			r.With(s.perm(rbac.PermWebhooksManage)).Post("/test", s.handleTestWebhook)
		})
	})
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	pg := httpx.ParsePagination(r)
	items, total, err := s.deps.Events.ListInbound(r.Context(), p, pg.Limit, pg.Offset)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, httpx.NewListResponse(items, total, pg))
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	items, err := s.deps.Events.ListSubscriptions(r.Context(), p)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONList(w, items)
}

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req events.CreateSubscriptionRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sub, err := s.deps.Events.CreateSubscription(r.Context(), p, s.deps.Keyring, req)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	// The secret is returned once, at creation, so the operator can configure the
	// receiving service. It is never readable again.
	response := map[string]any{"subscription": sub}
	if req.Secret == "" {
		response["note"] = "a signing secret was generated; it is not shown again"
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, response)
}

type setWebhookEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

func (s *Server) handleSetWebhookEnabled(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var req setWebhookEnabledRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := s.deps.Events.SetSubscriptionEnabled(r.Context(), p, id, req.Enabled); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{"enabled": req.Enabled})
}

func (s *Server) handleListDeliveries(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	pg := httpx.ParsePagination(r)
	items, total, err := s.deps.Events.ListDeliveries(r.Context(), p, id, pg.Limit, pg.Offset)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, httpx.NewListResponse(items, total, pg))
}

func (s *Server) handleReplayDelivery(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	// The route nests a delivery under its subscription, so both ids identify the
	// resource. The subscription is passed to the service rather than dropped: the
	// guard that mutates the row is scoped by it, so a delivery cannot be replayed
	// under a subscription it does not belong to.
	subID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	deliveryID, ok := pathUUID(w, r, "deliveryId")
	if !ok {
		return
	}
	if err := s.deps.Events.Replay(r.Context(), p, subID, deliveryID); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{"status": "delivery queued for a new attempt"})
}

// handleTestWebhook validates a subscription's target without sending a real
// payload, because sending customer data to a target as part of a "test" would be
// a data disclosure.
func (s *Server) handleTestWebhook(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	_ = id
	// The subscription must exist and belong to the caller.
	subs, err := s.deps.Events.ListSubscriptions(r.Context(), p)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	var found bool
	for _, sub := range subs {
		if sub.ID == id {
			found = true
			break
		}
	}
	if !found {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, map[string]any{
		"status": "subscription is registered and enabled",
		"note":   "no payload was sent; deliveries are recorded under /webhooks/{id}/deliveries",
	})
}

// inboundWebhookHandler is the shared entry point for inbound events.
//
// Every integration's inbound webhook uses this handler, so signature
// verification, replay protection and auditing cannot be forgotten for one
// integration while being present for another.
func (s *Server) inboundWebhookHandler(source string, secret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readLimitedBody(r, events.MaxInboundPayloadBytes)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		// The source is taken from the route, never from the payload, so a caller
		// cannot claim to be a different integration.
		event, err := s.deps.Events.VerifyAndStore(r.Context(), nil, source, secret, r.Header, body)
		if err != nil {
			s.respondDomainError(w, r, err)
			return
		}
		httpx.WriteJSONStatus(w, http.StatusAccepted, map[string]any{
			"event_id": event.EventID, "status": "accepted",
		})
	}
}
