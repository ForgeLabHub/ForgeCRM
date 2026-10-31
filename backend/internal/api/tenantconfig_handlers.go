package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/tenantconfig"
)

// mountTenantConfig registers the organization configuration routes.
//
// The routes are scoped to the caller's own organization rather than addressed by
// an organization id in the path. A tenant cannot read or write another tenant's
// configuration by naming it, because there is nowhere to name it: the
// organization comes from the session's resolved context, the same way every
// other tenant-scoped read does.
//
// Reading the effective configuration and changing it are separate permissions.
// The values steer defaults the whole organization inherits, so an auditor needs
// to see which currency the numbers are in without being able to move it.
func (s *Server) mountTenantConfig(r chi.Router) {
	r.Route("/settings", func(r chi.Router) {
		r.Use(s.requireService(func() bool { return s.deps.TenantConfig != nil }, "tenant configuration"))
		r.With(s.perm(rbac.PermTenantSettingsRead)).Get("/", s.handleListTenantSettings)
		r.With(s.perm(rbac.PermTenantSettingsRead)).Get("/catalog", s.handleTenantSettingsCatalog)
		r.With(s.perm(rbac.PermTenantSettingsRead)).Get("/{key}", s.handleGetTenantSetting)
		r.With(s.perm(rbac.PermTenantSettingsRead)).Get("/{key}/history", s.handleTenantSettingHistory)
		r.With(s.perm(rbac.PermTenantSettingsManage)).Put("/{key}", s.handleSetTenantSetting)
		r.With(s.perm(rbac.PermTenantSettingsManage)).Delete("/{key}", s.handleResetTenantSetting)
	})
}

// tenantSettingKey reads and validates the key path parameter.
func (s *Server) tenantSettingKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := chi.URLParam(r, "key")
	if _, ok := tenantconfig.Lookup(key); !ok {
		// An unknown key is reported as not found rather than as a validation error:
		// the catalog is the platform's, not the caller's, so "there is no such
		// setting" is a fact about the request's target, not about its syntax.
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return "", false
	}
	return key, true
}

func (s *Server) handleListTenantSettings(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	items, err := s.deps.TenantConfig.List(r.Context(), p.OrganizationID)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONList(w, items)
}

// handleTenantSettingsCatalog returns the catalog with defaults but no overrides.
//
// It is separate from the listing so a client can render the editor for an
// organization before any value has been stored, and so the shape of the catalog
// is readable without the reader's organization being part of the answer. The
// definition is the same either way; only the effective values differ.
func (s *Server) handleTenantSettingsCatalog(w http.ResponseWriter, r *http.Request) {
	definitions := tenantconfig.Catalog()
	items := make([]tenantconfig.Setting, 0, len(definitions))
	for _, d := range definitions {
		items = append(items, tenantconfig.Setting{
			Key:           d.Key,
			Title:         d.Title,
			Description:   d.Description,
			Kind:          string(d.Kind),
			Enum:          d.Enum,
			Min:           d.Min,
			Max:           d.Max,
			MaxLength:     d.MaxLength,
			Default:       d.Default,
			Effective:     d.Default,
			SchemaVersion: d.Since,
		})
	}
	httpx.WriteJSONList(w, items)
}

func (s *Server) handleGetTenantSetting(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	key, ok := s.tenantSettingKey(w, r)
	if !ok {
		return
	}
	setting, err := s.deps.TenantConfig.Get(r.Context(), p.OrganizationID, key)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, setting)
}

func (s *Server) handleTenantSettingHistory(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	key, ok := s.tenantSettingKey(w, r)
	if !ok {
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			httpx.WriteError(w, r, httpx.ValidationError(map[string]string{
				"limit": "must be a whole number between 1 and 200",
			}))
			return
		}
		limit = n
	}
	items, err := s.deps.TenantConfig.History(r.Context(), p.OrganizationID, key, limit)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONList(w, items)
}

type setTenantSettingRequest struct {
	Value any `json:"value"`
}

func (s *Server) handleSetTenantSetting(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	key, ok := s.tenantSettingKey(w, r)
	if !ok {
		return
	}
	var req setTenantSettingRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	// A body without `value` is refused rather than treated as null. `null` is not a
	// value any kind accepts, so accepting the field's absence as one would produce a
	// confusing per-field error about the type; saying the field is missing is the
	// accurate description.
	if req.Value == nil {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{
			"value": "is required",
		}))
		return
	}
	setting, err := s.deps.TenantConfig.Set(r.Context(), p, key, req.Value)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, setting)
}

func (s *Server) handleResetTenantSetting(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	key, ok := s.tenantSettingKey(w, r)
	if !ok {
		return
	}
	setting, err := s.deps.TenantConfig.Reset(r.Context(), p, key)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, setting)
}
