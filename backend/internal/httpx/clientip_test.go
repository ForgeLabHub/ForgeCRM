package httpx

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The tests below cover the client-address trust rule. It is worth stating why
// this matters, because the failure is invisible in normal use: the API keeps
// working, and the only symptom is that a limit which exists in the code is not
// enforced in practice.
//
// X-Forwarded-For is a request header, so any client can set it. If the server
// believes it from everyone, then a caller who wants more requests than its
// budget allows sends a different value on each request and is never counted
// against the same bucket twice. The same forged value also becomes the address
// recorded in the authentication log, so a brute-force attempt is attributed to
// addresses the attacker chose.
//
// The rule is therefore that the header is honoured only when the connection
// arrived from an address the operator named as a proxy.

func request(remoteAddr string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contacts", nil)
	r.RemoteAddr = remoteAddr
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestTrustedProxiesFromRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"not-an-ip", "10.0.0.0/33", "example.com", "10.0.0.1,,/8"} {
		if _, err := TrustedProxiesFrom(bad); err == nil {
			t.Errorf("%q was accepted as a trusted proxy; a typo here silently disables proxy awareness", bad)
		}
	}
}

func TestTrustedProxiesFromAcceptsRangesAndBareAddresses(t *testing.T) {
	tp, err := TrustedProxiesFrom("10.0.0.0/8, 192.168.1.5, 2001:db8::/32")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if tp.Len() != 3 {
		t.Fatalf("parsed %d ranges, want 3", tp.Len())
	}
	for _, ip := range []string{"10.1.2.3", "192.168.1.5", "2001:db8::1"} {
		if !tp.Contains(mustIP(t, ip)) {
			t.Errorf("%s is not trusted", ip)
		}
	}
	if tp.Contains(mustIP(t, "192.168.1.6")) {
		t.Error("a bare address trusted a neighbour; it must be a single host")
	}
}

func TestTrustedProxiesEmptyTrustsNothing(t *testing.T) {
	tp, err := TrustedProxiesFrom("")
	if err != nil {
		t.Fatalf("parse empty: %v", err)
	}
	if tp.Len() != 0 || tp.TrustsAny() {
		t.Fatal("an empty list must trust nothing")
	}
}

// TestForwardedHeaderIsIgnoredFromAnUntrustedPeer is the core security case.
func TestForwardedHeaderIsIgnoredFromAnUntrustedPeer(t *testing.T) {
	trusted, err := TrustedProxiesFrom("10.0.0.0/8")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	r := request("203.0.113.7:5000", map[string]string{"X-Forwarded-For": "1.2.3.4"})

	if got := ClientIPFrom(r, trusted); got != "203.0.113.7" {
		t.Fatalf("client address is %q; a caller outside the trusted range had its X-Forwarded-For believed, "+
			"so it can choose its own rate-limit bucket and forge its entry in the auth log", got)
	}
}

func TestForwardedHeaderIsHonouredFromATrustedPeer(t *testing.T) {
	trusted, err := TrustedProxiesFrom("10.0.0.0/8")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	r := request("10.0.0.9:5000", map[string]string{"X-Forwarded-For": "203.0.113.7"})

	if got := ClientIPFrom(r, trusted); got != "203.0.113.7" {
		t.Fatalf("client address is %q, want 203.0.113.7; the proxy's forwarded value was not used, "+
			"so every client behind it shares one rate-limit bucket", got)
	}
}

// TestForwardedChainIsReadRightToLeft covers the case that makes the obvious
// implementation wrong.
//
// A trusted proxy that appends to X-Forwarded-For rather than replacing it lets
// a client prepend an address. Taking the left-most entry — the natural reading
// of "the first proxy saw the client" — returns the address the client wrote.
// The address that cannot be chosen is the right-most entry that is not itself a
// trusted proxy.
func TestForwardedChainIsReadRightToLeft(t *testing.T) {
	trusted, _ := TrustedProxiesFrom("10.0.0.0/8")

	// The client sent "1.2.3.4"; the proxy appended the address it saw.
	r := request("10.0.0.9:5000", map[string]string{
		"X-Forwarded-For": "1.2.3.4, 203.0.113.7",
	})
	if got := ClientIPFrom(r, trusted); got != "203.0.113.7" {
		t.Fatalf("client address is %q; the left-most entry is the one the client wrote, "+
			"so honouring it lets a caller choose its own bucket", got)
	}
}

// TestForwardedChainSkipsTrustedProxies walks a two-proxy chain: the client, then
// a proxy, then another proxy. Both proxy entries are trusted, so the client is
// the entry before them.
func TestForwardedChainSkipsTrustedProxies(t *testing.T) {
	trusted, _ := TrustedProxiesFrom("10.0.0.0/8")
	r := request("10.0.0.9:5000", map[string]string{
		"X-Forwarded-For": "203.0.113.7, 10.0.0.10",
	})
	if got := ClientIPFrom(r, trusted); got != "203.0.113.7" {
		t.Fatalf("client address is %q, want 203.0.113.7", got)
	}
}

// TestForwardedChainOfOnlyProxiesFallsBackToThePeer: when every entry is a
// trusted proxy the header names no client, so the peer is the best answer.
func TestForwardedChainOfOnlyProxiesFallsBackToThePeer(t *testing.T) {
	trusted, _ := TrustedProxiesFrom("10.0.0.0/8")
	r := request("10.0.0.9:5000", map[string]string{
		"X-Forwarded-For": "10.0.0.10, 10.0.0.11",
	})
	if got := ClientIPFrom(r, trusted); got != "10.0.0.9" {
		t.Fatalf("client address is %q, want the peer", got)
	}
}

func TestXRealIPIsUsedWhenNoForwardedFor(t *testing.T) {
	trusted, _ := TrustedProxiesFrom("10.0.0.0/8")
	r := request("10.0.0.9:5000", map[string]string{"X-Real-Ip": "203.0.113.9"})
	if got := ClientIPFrom(r, trusted); got != "203.0.113.9" {
		t.Fatalf("client address is %q, want 203.0.113.9", got)
	}
}

// TestUnparseableForwardedValueFallsBackToThePeer covers a proxy that passes the
// header through unvalidated. The value must not become a rate-limit key or a
// log field in its raw form.
func TestUnparseableForwardedValueFallsBackToThePeer(t *testing.T) {
	trusted, _ := TrustedProxiesFrom("10.0.0.0/8")

	for _, junk := range []string{"not-an-ip", "1.2.3.4, not-an-ip", "<script>", "10.0.0.1 10.0.0.2"} {
		r := request("10.0.0.9:5000", map[string]string{"X-Forwarded-For": junk})
		if got := ClientIPFrom(r, trusted); got != "10.0.0.9" {
			t.Errorf("forwarded value %q produced address %q; an unparseable value must fall back to the peer", junk, got)
		}
	}
}

// TestTrustingEveryPeerIsRecordedSeparately: "*" is a legitimate configuration
// when the API is unreachable except through a proxy, but it must be visible
// rather than indistinguishable from an ordinary range.
func TestTrustingEveryPeerIsRecordedSeparately(t *testing.T) {
	tp, err := TrustedProxiesFrom("*")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !tp.TrustsAny() {
		t.Fatal("'*' did not set the trust-everything flag")
	}

	r := request("203.0.113.7:5000", map[string]string{"X-Forwarded-For": "1.2.3.4"})
	if got := ClientIPFrom(r, tp); got != "1.2.3.4" {
		t.Fatalf("client address is %q; with every peer trusted the header is used", got)
	}
}

// TestClientIPMiddlewareStoresTheResolvedAddress guards the reason the value is
// resolved once and carried in the context: the limiter and the audit trail must
// agree. If they derive it separately, a request can be counted against one
// address and recorded under another.
func TestClientIPMiddlewareStoresTheResolvedAddress(t *testing.T) {
	trusted, _ := TrustedProxiesFrom("10.0.0.0/8")

	var seen string
	handler := ClientIPMiddleware(trusted)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = ClientIP(r)
	}))

	r := request("10.0.0.9:5000", map[string]string{"X-Forwarded-For": "203.0.113.7"})
	handler.ServeHTTP(httptest.NewRecorder(), r)

	if seen != "203.0.113.7" {
		t.Fatalf("ClientIP returned %q from the context, want 203.0.113.7", seen)
	}
}

// TestClientIPWithoutMiddlewareUsesThePeer is the safe fallback: a request that
// did not pass through the middleware — a synthetic request in a handler test —
// must not be attributable to a header.
func TestClientIPWithoutMiddlewareUsesThePeer(t *testing.T) {
	r := request("203.0.113.7:5000", map[string]string{"X-Forwarded-For": "1.2.3.4"})
	if got := ClientIP(r); got != "203.0.113.7" {
		t.Fatalf("ClientIP returned %q without the middleware, want the peer address", got)
	}
}

func TestPeerIPHandlesOddRemoteAddr(t *testing.T) {
	// A bare address, and no address at all. Neither should produce an empty key,
	// because an empty key would put every such request in one bucket that a
	// client could also claim.
	if got := peerIP(request("203.0.113.7", nil)); got.String() != "203.0.113.7" {
		t.Errorf("bare address gave %q", got)
	}
	if got := peerIP(request("", nil)); !got.IsUnspecified() {
		t.Errorf("missing address gave %q, want the unspecified address", got)
	}
}

func mustIP(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	if ip == nil {
		t.Fatalf("test bug: %q is not an IP", s)
	}
	return ip
}
