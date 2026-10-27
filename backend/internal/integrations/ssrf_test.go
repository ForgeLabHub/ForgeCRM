package integrations

import (
	"context"
	"net"
	"strings"
	"testing"
)

// TestIsBlockedIP is the unit-level check on the address classification that the
// SSRF guard is built from. A mistake here is a server-side request forgery hole,
// so the cases are spelled out rather than derived.
func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
		why     string
	}{
		// Loopback: reaching these means the server is talking to itself.
		{"127.0.0.1", true, "IPv4 loopback"},
		{"127.1.2.3", true, "IPv4 loopback range"},
		{"::1", true, "IPv6 loopback"},

		// Cloud metadata: the highest-value SSRF target, since it hands out
		// credentials on AWS, GCP, Azure and DigitalOcean.
		{"169.254.169.254", true, "link-local metadata service"},
		{"169.254.0.1", true, "link-local range"},
		{"fd00:ec2::254", true, "IPv6 metadata address"},

		// Private ranges: internal services that must not be reachable.
		{"10.0.0.1", true, "RFC1918 10/8"},
		{"172.16.0.1", true, "RFC1918 172.16/12 lower bound"},
		{"172.31.255.255", true, "RFC1918 172.16/12 upper bound"},
		{"192.168.1.1", true, "RFC1918 192.168/16"},
		{"fc00::1", true, "IPv6 unique local"},
		{"fe80::1", true, "IPv6 link-local"},

		// Other special-purpose ranges.
		{"0.0.0.0", true, "unspecified address"},
		{"100.64.0.1", true, "carrier-grade NAT"},
		{"224.0.0.1", true, "multicast"},
		{"255.255.255.255", true, "broadcast"},

		// A public address must pass, or the guard would be useless.
		{"93.184.216.34", false, "public IPv4"},
		{"2606:2800:220:1:248:1893:25c8:1946", false, "public IPv6"},
	}

	for _, tc := range cases {
		t.Run(tc.ip, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("test case %q is not a valid IP", tc.ip)
			}
			if got := IsBlockedIP(ip); got != tc.blocked {
				t.Errorf("IsBlockedIP(%s) = %v, want %v (%s)", tc.ip, got, tc.blocked, tc.why)
			}
		})
	}
}

// TestValidateOutboundURLRejectsDangerousTargets checks the guard at the URL
// level, which is the entry point every outbound request goes through. Both the
// scheme and the resolved destination must be validated, because a hostname can
// resolve to an internal address.
func TestValidateOutboundURLRejectsDangerousTargets(t *testing.T) {
	ctx := context.Background()

	rejected := []struct {
		url string
		why string
	}{
		{"http://127.0.0.1/admin", "loopback address"},
		{"http://localhost/admin", "loopback hostname"},
		{"http://169.254.169.254/latest/meta-data/iam/security-credentials/", "cloud metadata"},
		{"http://10.0.0.5/internal", "private address"},
		{"http://192.168.1.1/router", "private address"},
		{"http://[::1]/admin", "IPv6 loopback"},
		{"file:///etc/passwd", "non-HTTP scheme"},
		{"gopher://127.0.0.1:6379/_INFO", "non-HTTP scheme"},
		{"http://0.0.0.0/", "unspecified address"},
	}

	for _, tc := range rejected {
		t.Run(tc.why, func(t *testing.T) {
			if _, err := ValidateOutboundURL(ctx, tc.url); err == nil {
				t.Errorf("ValidateOutboundURL(%q) was accepted, want a refusal (%s)", tc.url, tc.why)
			} else if !strings.Contains(err.Error(), "refused") {
				t.Errorf("ValidateOutboundURL(%q) failed with %q, want a refusal reason", tc.url, err)
			}
		})
	}
}

// TestValidateOutboundURLAcceptsPublicTarget confirms the guard is not simply
// refusing everything, which would silently break every integration.
func TestValidateOutboundURLAcceptsPublicTarget(t *testing.T) {
	ctx := context.Background()

	for _, raw := range []string{
		"https://example.com/hooks",
		"http://93.184.216.34/hooks",
	} {
		t.Run(raw, func(t *testing.T) {
			u, err := ValidateOutboundURL(ctx, raw)
			if err != nil {
				t.Fatalf("a public target was refused: %v", err)
			}
			if u == nil {
				t.Fatal("ValidateOutboundURL returned a nil URL without an error")
			}
		})
	}
}

// TestGuardedClientRefusesInternalTarget is the end-to-end check: the client the
// worker uses for webhook delivery must refuse an internal destination. Creating
// a subscription to a metadata address is allowed (the guard runs at connect
// time, not at configuration time), so this is the boundary that matters.
func TestGuardedClientRefusesInternalTarget(t *testing.T) {
	client := NewGuardedClient(5 * 1e9) // 5s

	targets := []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://127.0.0.1:5432/",
		"http://10.0.0.1/",
	}

	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			_, _, err := client.PostJSON(context.Background(), target, []byte(`{}`), nil)
			if err == nil {
				t.Fatalf("the guarded client connected to %s; it must refuse internal destinations", target)
			}
		})
	}
}
