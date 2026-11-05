package api

import (
	"errors"
	"net/http"

	"github.com/geff1778/forgecrm/internal/appearance"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/jackc/pgx/v5"
)

// Appearance is a personal preference, never a source of authorization.
// A nil style asks the server to resolve the default for the active organization.
type appearanceInput struct {
	Style   *string `json:"style"`
	Mode    string  `json:"mode"`
	Accent  string  `json:"accent"`
	Density string  `json:"density"`
}

type appearanceResponse struct {
	Style          *string            `json:"style"`
	Mode           string             `json:"mode"`
	Accent         string             `json:"accent"`
	Density        string             `json:"density"`
	Profile        appearance.Profile `json:"profile"`
	DefaultStyle   appearance.Theme   `json:"default_style"`
	EffectiveStyle string             `json:"effective_style"`
}

func validAppearance(in appearanceInput) map[string]string {
	fields := map[string]string{}
	if in.Style != nil && *in.Style != "default" && !appearance.Theme(*in.Style).Valid() {
		fields["style"] = "unsupported style"
	}
	if in.Mode != "system" && in.Mode != "light" && in.Mode != "dark" {
		fields["mode"] = "unsupported mode"
	}
	switch in.Accent {
	case "theme", "indigo", "emerald", "amber", "rose", "cyan":
	default:
		fields["accent"] = "unsupported accent"
	}
	switch in.Density {
	case "compact", "standard", "spacious":
	default:
		fields["density"] = "unsupported density"
	}
	return fields
}

func (s *Server) appearanceDefault(r *http.Request, p *rbac.Principal) (appearance.Profile, appearance.Theme, error) {
	rows, err := s.deps.DB.Q(r.Context()).Query(r.Context(), `
		SELECT DISTINCT roles.key FROM user_roles ur
		JOIN roles ON roles.id = ur.role_id
		WHERE ur.user_id = $1 AND (ur.organization_id IS NULL OR ur.organization_id = $2)`, p.UserID, p.OrganizationID)
	if err != nil {
		return "", "", err
	}
	defer rows.Close()
	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return "", "", err
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return "", "", err
	}
	profile, theme := appearance.DefaultForRoles(roles, p.IsPlatformOwner)
	return profile, theme, nil
}

func appearanceResult(in appearanceInput, profile appearance.Profile, defaultStyle appearance.Theme) appearanceResponse {
	style := string(defaultStyle)
	if in.Style != nil {
		style = *in.Style
	}
	return appearanceResponse{
		Style: in.Style, Mode: in.Mode, Accent: in.Accent, Density: in.Density,
		Profile: profile, DefaultStyle: defaultStyle, EffectiveStyle: style,
	}
}

func (s *Server) handleGetAppearance(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	in := appearanceInput{Mode: "system", Accent: "theme", Density: "standard"}
	err := s.deps.DB.Q(r.Context()).QueryRow(r.Context(), `
		SELECT style, mode, accent, density FROM user_appearance_preferences WHERE user_id = $1`, p.UserID).
		Scan(&in.Style, &in.Mode, &in.Accent, &in.Density)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		s.respondDomainError(w, r, err)
		return
	}
	profile, theme, err := s.appearanceDefault(r, p)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, appearanceResult(in, profile, theme))
}

func (s *Server) handlePutAppearance(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var in appearanceInput
	if err := decodeBody(w, r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if fields := validAppearance(in); len(fields) != 0 {
		httpx.WriteError(w, r, httpx.ValidationError(fields))
		return
	}
	// Resolve before committing, so an error response cannot conceal a saved write.
	profile, theme, err := s.appearanceDefault(r, p)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	_, err = s.deps.DB.Q(r.Context()).Exec(r.Context(), `
		INSERT INTO user_appearance_preferences (user_id, style, mode, accent, density)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id) DO UPDATE SET style = EXCLUDED.style, mode = EXCLUDED.mode,
		accent = EXCLUDED.accent, density = EXCLUDED.density`, p.UserID, in.Style, in.Mode, in.Accent, in.Density)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, appearanceResult(in, profile, theme))
}
