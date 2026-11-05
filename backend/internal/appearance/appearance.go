// Package appearance describes visual defaults. It never participates in
// authorization: role keys here are used exclusively to choose presentation.
package appearance

// Theme is one of the supported interface concepts.
type Theme string

const (
	Minimal   Theme = "minimal"
	Midnight  Theme = "midnight"
	Glass     Theme = "glass"
	Premium   Theme = "premium"
	Brutalist Theme = "brutalist"
	Cyberpunk Theme = "cyberpunk"
	Soft      Theme = "soft"
	Editorial Theme = "editorial"
	Warm      Theme = "warm"
	Colorful  Theme = "colorful"
)

// Valid reports whether a persisted preference is a supported theme.
func (t Theme) Valid() bool {
	switch t {
	case Minimal, Midnight, Glass, Premium, Brutalist, Cyberpunk, Soft, Editorial, Warm, Colorful:
		return true
	default:
		return false
	}
}

// Profile is an appearance classification, not an RBAC role or permission.
type Profile string

const (
	PlatformAdmin       Profile = "platform_admin"
	OrganizationAdmin   Profile = "organization_admin"
	Executive           Profile = "executive"
	SalesManager        Profile = "sales_manager"
	BusinessAnalyst     Profile = "business_analyst"
	MarketingSpecialist Profile = "marketing_specialist"
	SupportAgent        Profile = "support_agent"
	AccountManager      Profile = "account_manager"
	SalesRepresentative Profile = "sales_representative"
	DataOperator        Profile = "data_operator"
)

type roleAppearance struct {
	role    string
	profile Profile
	theme   Theme
}

// Highest priority first. Some keys are not seeded as roles in the current
// installation; recognizing a custom role key here does not create or grant it.
var roleDefaults = [...]roleAppearance{
	{"platform_admin", PlatformAdmin, Midnight},
	{"tenant_admin", OrganizationAdmin, Warm},
	{"executive", Executive, Premium},
	{"sales_manager", SalesManager, Brutalist},
	{"analyst", BusinessAnalyst, Editorial},
	{"marketing_specialist", MarketingSpecialist, Colorful},
	{"support_agent", SupportAgent, Glass},
	{"account_manager", AccountManager, Soft},
	{"sales_representative", SalesRepresentative, Cyberpunk},
	{"data_steward", DataOperator, Minimal},
}

// DefaultForRoles resolves an appearance-only classification from role keys.
// A platform owner can be recognized independently of organization role
// assignments. Unknown roles deliberately fall back to the neutral theme. The
// result never changes the permissions resolved by the RBAC service.
func DefaultForRoles(roleKeys []string, platformOwner bool) (Profile, Theme) {
	if platformOwner {
		return PlatformAdmin, Midnight
	}
	for _, candidate := range roleDefaults {
		for _, role := range roleKeys {
			if role == candidate.role {
				return candidate.profile, candidate.theme
			}
		}
	}
	return "", Minimal
}

// Effective returns the saved preference when valid, otherwise the role default.
// An absent preference represents reset-to-default rather than freezing a theme
// at the moment the user resets it.
func Effective(preference Theme, roleKeys []string, platformOwner bool) Theme {
	if preference.Valid() {
		return preference
	}
	_, theme := DefaultForRoles(roleKeys, platformOwner)
	return theme
}
