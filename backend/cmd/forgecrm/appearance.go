package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/geff1778/forgecrm/internal/appearance"
)

// runAppearance evaluates caller-supplied examples; it does not look up a user,
// make authorization decisions, or read or write an account preference.
func runAppearance(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("appearance", flag.ContinueOnError)
	fs.SetOutput(errOut)
	rolesArg := fs.String("roles", "", "comma-separated role keys to simulate")
	platformOwner := fs.Bool("platform-owner", false, "simulate a platform owner")
	preference := fs.String("preference", "", "explicit style to simulate (omit for role default)")
	fs.Usage = func() {
		fmt.Fprintln(errOut, "Usage: forgecrm appearance [--roles key,key] [--platform-owner] [--preference style]")
		fmt.Fprintln(errOut, "Offline diagnostic only: inputs are simulated; no account settings or permissions are read or changed.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(errOut, "forgecrm appearance: unexpected positional arguments")
		return 2
	}
	var roles []string
	if *rolesArg != "" {
		for _, value := range strings.Split(*rolesArg, ",") {
			role := strings.TrimSpace(value)
			if role == "" {
				fmt.Fprintln(errOut, "forgecrm appearance: role keys cannot be empty")
				return 2
			}
			roles = append(roles, role)
		}
	}
	selected := appearance.Theme(*preference)
	if selected != "" && !selected.Valid() {
		fmt.Fprintf(errOut, "forgecrm appearance: unsupported preference %q\n", selected)
		return 2
	}
	profile, defaultTheme := appearance.DefaultForRoles(roles, *platformOwner)
	result := struct {
		DiagnosticOnly    bool               `json:"diagnostic_only"`
		Profile           appearance.Profile `json:"profile"`
		DefaultStyle      appearance.Theme   `json:"default_style"`
		EffectiveStyle    appearance.Theme   `json:"effective_style"`
		PreferenceApplied bool               `json:"preference_applied"`
	}{
		DiagnosticOnly:    true,
		Profile:           profile,
		DefaultStyle:      defaultTheme,
		EffectiveStyle:    appearance.Effective(selected, roles, *platformOwner),
		PreferenceApplied: selected != "",
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		fmt.Fprintf(errOut, "forgecrm appearance: write result: %v\n", err)
		return 1
	}
	return 0
}
