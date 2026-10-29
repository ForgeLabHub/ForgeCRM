// Package netguard holds the outbound-destination guard shared by every part of
// ForgeCRM that opens a connection to an operator- or user-supplied address: the
// Integration Hub, the outbound webhook sender, the migration connectors and the
// S3-compatible object store.
//
// It is a leaf: it imports nothing from this module and nothing outside the
// standard library. That is deliberate. The object store needs exactly these
// rules — an endpoint is an SSRF target — but it must not drag in the Integration
// Hub to get them. Keeping the guard here lets a storage package depend on the
// rules without depending on the Hub, and lets both be extracted independently.
package netguard

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ssrfError describes a refused destination.
type ssrfError struct{ reason string }

func (e *ssrfError) Error() string { return "outbound destination refused: " + e.reason }

// blockedNets are the address ranges an outbound connection must never reach.
//
// Reaching the loopback interface, a private network or a cloud metadata service
// is the classic SSRF escalation: it turns a "fetch this URL" feature into access
// to internal services or instance credentials.
var blockedNets = func() []*net.IPNet {
	cidrs := []string{
		"0.0.0.0/8",          // "this network"
		"10.0.0.0/8",         // private
		"100.64.0.0/10",      // carrier-grade NAT
		"127.0.0.0/8",        // loopback
		"169.254.0.0/16",     // link-local, includes cloud metadata
		"172.16.0.0/12",      // private
		"192.0.0.0/24",       // IETF protocol assignments
		"192.0.2.0/24",       // documentation
		"192.168.0.0/16",     // private
		"198.18.0.0/15",      // benchmarking
		"198.51.100.0/24",    // documentation
		"203.0.113.0/24",     // documentation
		"224.0.0.0/4",        // multicast
		"240.0.0.0/4",        // reserved
		"255.255.255.255/32", // broadcast
		"::1/128",            // IPv6 loopback
		"fc00::/7",           // unique local
		"fe80::/10",          // IPv6 link-local
		"ff00::/8",           // IPv6 multicast
		"::/128",             // unspecified
		"64:ff9b::/96",       // IPv4-IPv6 translation
	}
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}()

// metadataHosts are cloud instance metadata endpoints, blocked by name as well as
// by address so that a DNS alias cannot slip through.
var metadataHosts = []string{
	"metadata.google.internal",
	"metadata.goog",
	"instance-data",
	"metadata.azure.com",
}

// IsBlockedIP reports whether an address must not be contacted.
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsUnspecified() || ip.IsPrivate() {
		return true
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateOutboundURL checks scheme, host and every resolved address.
//
// Resolution is done here so the check applies to the addresses the client will
// actually connect to, not merely the hostname.
func ValidateOutboundURL(ctx context.Context, raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, &ssrfError{"malformed URL"}
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return nil, &ssrfError{fmt.Sprintf("scheme %q is not allowed", u.Scheme)}
	}
	host := u.Hostname()
	if host == "" {
		return nil, &ssrfError{"URL has no host"}
	}
	lower := strings.ToLower(host)
	for _, m := range metadataHosts {
		if lower == m || strings.HasSuffix(lower, "."+m) {
			return nil, &ssrfError{"cloud metadata endpoints are not reachable"}
		}
	}
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") ||
		strings.HasSuffix(lower, ".internal") || strings.HasSuffix(lower, ".local") {
		return nil, &ssrfError{"internal hostnames are not reachable"}
	}

	// A literal IP is checked directly; a name is resolved and every answer is
	// checked, so a DNS record pointing at a private address is refused.
	if ip := net.ParseIP(host); ip != nil {
		if IsBlockedIP(ip) {
			return nil, &ssrfError{"destination address is in a blocked range"}
		}
		return u, nil
	}

	resolver := &net.Resolver{}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrs, err := resolver.LookupIPAddr(lookupCtx, host)
	if err != nil {
		return nil, &ssrfError{"host could not be resolved"}
	}
	if len(addrs) == 0 {
		return nil, &ssrfError{"host resolved to no addresses"}
	}
	for _, a := range addrs {
		if IsBlockedIP(a.IP) {
			return nil, &ssrfError{"host resolves to a blocked address range"}
		}
	}
	return u, nil
}

// GuardedClient is an HTTP client that refuses SSRF destinations on every hop.
//
// The dialer re-validates the concrete address, which is what actually closes
// DNS-rebinding: the name may resolve differently between the initial check and
// the connection, so the address being dialled is checked too. Redirects are
// capped and each one is validated.
type GuardedClient struct {
	client *http.Client
}

// NewGuardedClient builds a client with the given timeout.
func NewGuardedClient(timeout time.Duration) *GuardedClient {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fmt.Errorf("malformed dial address: %w", err)
			}
			// Resolve and vet here so the checked address is the dialled address.
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, a := range ips {
				if IsBlockedIP(a.IP) {
					return nil, &ssrfError{fmt.Sprintf("resolved address %s is blocked", a.IP)}
				}
			}
			// Dial the first vetted address explicitly.
			var lastErr error
			for _, a := range ips {
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(a.IP.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		IdleConnTimeout:       60 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		DisableCompression:    false,
	}
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return &ssrfError{"too many redirects"}
			}
			// Re-validate every redirect target: an attacker-controlled server can
			// redirect to an internal address.
			if _, err := ValidateOutboundURL(req.Context(), req.URL.String()); err != nil {
				return err
			}
			return nil
		},
	}
	return &GuardedClient{client: client}
}

// Do performs a guarded request.
func (c *GuardedClient) Do(req *http.Request) (*http.Response, error) {
	if _, err := ValidateOutboundURL(req.Context(), req.URL.String()); err != nil {
		return nil, err
	}
	return c.client.Do(req)
}

// PostJSON sends a JSON body with a bounded response read.
func (c *GuardedClient) PostJSON(ctx context.Context, target string, body []byte,
	headers map[string]string) (int, []byte, error) {

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(string(body)))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	// Bound the response so a hostile endpoint cannot exhaust memory.
	limited := io.LimitReader(resp.Body, 1<<20)
	data, err := io.ReadAll(limited)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, data, nil
}
