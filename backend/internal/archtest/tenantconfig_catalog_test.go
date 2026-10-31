package archtest

import (
	"testing"

	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/tenantconfig"
	"github.com/geff1778/forgecrm/internal/workflow"
)

// TestTenantCatalogKeysMatchTheConsumers pins the setting-key spellings.
//
// The domain packages spell the keys they read as their own constants so they hold
// no dependency on the configuration package — the arrow points from configuration
// to domain, not the other way. That is the right direction, but it puts the same
// string in two places, and a typo on either side would leave a setting that reads
// as configured and is silently ignored.
//
// The check lives here rather than in the configuration package's own tests
// because proving it needs to import the consumers, and a package that imports the
// domain it configures is exactly the coupling the extraction boundary forbids:
// `internal/tenantconfig` must lift out of the monolith with only `audit`,
// `database` and `rbac`. A cross-module invariant belongs in the architecture
// tests, which are the one place allowed to see every module at once.
func TestTenantCatalogKeysMatchTheConsumers(t *testing.T) {
	cases := []struct {
		key      string
		consumer string
	}{
		{crm.SettingDefaultCurrency, "crm.SettingDefaultCurrency"},
		{crm.SettingDefaultTaskPriority, "crm.SettingDefaultTaskPriority"},
		{workflow.SettingDefaultTimezone, "workflow.SettingDefaultTimezone"},
	}
	read := make(map[string]bool, len(cases))
	for _, tc := range cases {
		read[tc.key] = true
		def, ok := tenantconfig.Lookup(tc.key)
		if !ok {
			t.Errorf("%s names %q, which is not in the catalog; the setting would never be readable",
				tc.consumer, tc.key)
			continue
		}
		if def.Default == nil {
			t.Errorf("%s: the catalog default for %q is nil", tc.consumer, tc.key)
		}
	}

	// The reverse direction: a catalog key that no consumer reads would be a control
	// that does nothing, which is the failure this feature exists to avoid. Every key
	// in the catalog must be named by one of the consumers above.
	for _, d := range tenantconfig.Catalog() {
		if !read[d.Key] {
			t.Errorf("catalog key %q is read by no consumer; a setting nothing reads is a button that does nothing", d.Key)
		}
	}
}
