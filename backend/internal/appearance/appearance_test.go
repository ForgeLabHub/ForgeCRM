package appearance

import "testing"

func TestDefaultForEachRole(t *testing.T) {
	tests := []struct {
		role    string
		profile Profile
		theme   Theme
	}{
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
	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			profile, theme := DefaultForRoles([]string{tt.role}, false)
			if profile != tt.profile || theme != tt.theme {
				t.Fatalf("got %q, %q; want %q, %q", profile, theme, tt.profile, tt.theme)
			}
		})
	}
}

func TestRoleOrderDoesNotChangePriority(t *testing.T) {
	pairs := [][]string{
		{"sales_representative", "executive", "support_agent"},
		{"support_agent", "sales_representative", "executive"},
		{"executive", "support_agent", "sales_representative"},
	}
	for _, roles := range pairs {
		profile, theme := DefaultForRoles(roles, false)
		if profile != Executive || theme != Premium {
			t.Errorf("roles %v yielded %q/%q, want executive/premium", roles, profile, theme)
		}
	}
	if profile, theme := DefaultForRoles([]string{"sales_representative"}, true); profile != PlatformAdmin || theme != Midnight {
		t.Errorf("platform owner yielded %q/%q", profile, theme)
	}
	if profile, theme := DefaultForRoles([]string{"unknown"}, false); profile != "" || theme != Minimal {
		t.Errorf("unknown role yielded %q/%q", profile, theme)
	}
}

func TestThemeValidationAndPreference(t *testing.T) {
	all := []Theme{Minimal, Midnight, Glass, Premium, Brutalist, Cyberpunk, Soft, Editorial, Warm, Colorful}
	seen := map[Theme]bool{}
	for _, theme := range all {
		if !theme.Valid() || seen[theme] {
			t.Errorf("invalid or duplicate theme %q", theme)
		}
		seen[theme] = true
		if got := Effective(theme, []string{"executive"}, false); got != theme {
			t.Errorf("explicit %q got overridden by %q", theme, got)
		}
	}
	for _, invalid := range []Theme{"", "dark", "PREMIUM", "unknown"} {
		if invalid.Valid() {
			t.Errorf("accepted invalid theme %q", invalid)
		}
		if got := Effective(invalid, []string{"sales_representative"}, false); got != Cyberpunk {
			t.Errorf("invalid preference %q resolved to %q", invalid, got)
		}
	}
}

func TestEveryPriorityPairAndEveryThemeDefault(t *testing.T) {
	used := make(map[Theme]bool)
	profiles := make(map[Profile]bool)
	for i, higher := range roleDefaults {
		if !higher.theme.Valid() || used[higher.theme] || profiles[higher.profile] {
			t.Fatalf("duplicate or invalid role default: %+v", higher)
		}
		used[higher.theme] = true
		profiles[higher.profile] = true
		for _, lower := range roleDefaults[i+1:] {
			for _, roles := range [][]string{{higher.role, lower.role}, {lower.role, higher.role}} {
				profile, theme := DefaultForRoles(roles, false)
				if profile != higher.profile || theme != higher.theme {
					t.Errorf("roles %v: got %q/%q, want %q/%q", roles, profile, theme, higher.profile, higher.theme)
				}
			}
		}
	}
	if len(used) != 10 || len(profiles) != 10 {
		t.Fatalf("defaults: %d themes, %d profiles; want 10 of each", len(used), len(profiles))
	}
}
