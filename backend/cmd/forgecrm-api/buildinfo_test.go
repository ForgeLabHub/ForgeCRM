package main

import "testing"

// TestBuildInfoResolution pins the order in which the effective version and
// commit are chosen, because that order is what makes the deployed revision
// trustworthy.
//
// The defect this guards: blitz.cloud builds the repository-root Dockerfile and
// passes no build arguments, so the binary used to report whatever a human had
// last typed into FORGECRM_COMMIT — a value that went stale and named an older
// commit than the one actually serving. The fix stamps the checked-out revision
// into the binary at build time (a linker flag), and these cases assert that the
// linker value is never overridden by the environment, while the environment is
// still a fallback for local development, where nothing stamps the binary.
func TestBuildInfoResolution(t *testing.T) {
	cases := []struct {
		name       string
		version    string
		commit     string
		envVersion string
		envCommit  string
		envRender  string
		wantV      string
		wantC      string
	}{
		{
			name:    "linker values win over the environment",
			version: "1.4.2",
			commit:  "abc123def",
			// A stale environment variable must not override a real build stamp.
			envVersion: "0.0.0-stale",
			envCommit:  "deadbeef",
			wantV:      "1.4.2",
			wantC:      "abc123def",
		},
		{
			name: "environment fills placeholders for local development",
			// Defaults of the package variables, as a `go run` without flags sees.
			version:    "0.1.0-dev",
			commit:     "unknown",
			envVersion: "dev-local",
			envCommit:  "cafef00d",
			wantV:      "dev-local",
			wantC:      "cafef00d",
		},
		{
			name:      "render revision is the last fallback for the commit",
			version:   "0.1.0-dev",
			commit:    "unknown",
			envRender: "0123456789abcdef",
			wantV:     "0.1.0-dev",
			wantC:     "0123456789abcdef",
		},
		{
			name:      "explicit commit beats the render revision",
			version:   "0.1.0-dev",
			commit:    "unknown",
			envCommit: "explicit",
			envRender: "render",
			wantV:     "0.1.0-dev",
			wantC:     "explicit",
		},
		{
			name:    "no metadata at all keeps the explicit marker",
			version: "0.1.0-dev",
			commit:  "unknown",
			// An operator must be able to tell "nothing supplied" from a real SHA.
			wantV: "0.1.0-dev",
			wantC: "unknown",
		},
		{
			name:    "empty linker values are treated as absent",
			version: "",
			commit:  "",
			// Empty must not masquerade as a real build.
			envVersion: "from-env",
			envCommit:  "from-env",
			wantV:      "from-env",
			wantC:      "from-env",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Save and restore the package variables the linker would set.
			origVersion, origCommit := version, commit
			t.Cleanup(func() { version, commit = origVersion, origCommit })

			version, commit = tc.version, tc.commit
			t.Setenv("FORGECRM_VERSION", tc.envVersion)
			t.Setenv("FORGECRM_COMMIT", tc.envCommit)
			t.Setenv("RENDER_GIT_COMMIT", tc.envRender)

			gotV, gotC := buildInfo()
			if gotV != tc.wantV {
				t.Errorf("version = %q, want %q", gotV, tc.wantV)
			}
			if gotC != tc.wantC {
				t.Errorf("commit = %q, want %q", gotC, tc.wantC)
			}
		})
	}
}
