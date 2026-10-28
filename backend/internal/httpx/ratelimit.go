package httpx

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/geff1778/forgecrm/internal/ratelimit"
)

// ---------------------------------------------------------------------------
// Rate limiting
// ---------------------------------------------------------------------------

// SubjectFunc derives the value a limiter counts against, from a request.
//
// It is a function rather than a fixed header read because the right subject
// depends on what the route protects: a sign-in is limited per client address,
// because there is no authenticated user yet, while an authenticated call is
// limited per user so that one client behind a shared address cannot exhaust
// another's budget.
//
// A SubjectFunc must never return a value the client chooses. The address used
// here comes from ClientIP, which consults forwarding headers only when the
// direct peer is a configured proxy; the user id comes from the session.
type SubjectFunc func(r *http.Request) string

// RateLimitOptions configures one rate-limiting middleware.
type RateLimitOptions struct {
	// Limiter decides whether a request may proceed.
	Limiter *ratelimit.Limiter
	// Subject derives the key. Required.
	Subject SubjectFunc
	// Log records degraded decisions. Optional; when nil, degraded decisions are
	// silent, which is acceptable only in tests.
	Log *slog.Logger
	// Now overrides the clock. Optional; tests use it to step across a window
	// boundary without sleeping.
	Now func() time.Time
}

// RateLimitMiddleware applies a limiter to a route group and writes the standard
// rate-limit headers on every response it touches.
//
// Headers are emitted for allowed requests too, not only for rejections. A client
// that can see its remaining budget can slow down before it is refused, and an
// operator debugging "why is this 429" needs the limit on the response that
// succeeded to see the budget it was spending against.
func RateLimitMiddleware(opts RateLimitOptions) func(http.Handler) http.Handler {
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if opts.Limiter == nil {
				next.ServeHTTP(w, r)
				return
			}

			decision := opts.Limiter.Allow(r.Context(), opts.Subject(r), now())

			writeRateLimitHeaders(w, decision)

			if decision.Degraded && opts.Log != nil {
				// Logged at warn, not error: the request still succeeded or was
				// still refused according to policy. What an operator needs to see
				// is that the limit is not currently being enforced, and that is a
				// warning about the dependency, not a failure of this request.
				opts.Log.Warn("rate limit store unavailable; policy fail mode applied",
					"policy", opts.Limiter.Policy().Name,
					"fail_mode", opts.Limiter.Policy().FailMode.String(),
					"allowed", decision.Allowed,
				)
			}

			if !decision.Allowed {
				// A degraded decision is not a rate-limit refusal and must not be
				// reported as one. A 429 tells the client to slow down and retry later,
				// which is the wrong instruction when the truth is that the server
				// cannot count: the client would back off from a limit that is not
				// actually being applied. 503 with a distinct code says "this dependency
				// is unavailable", which is what a caller and an operator both need.
				if decision.Degraded {
					WriteError(w, r, NewError(http.StatusServiceUnavailable, CodeUnavailable,
						"the rate limit cannot be evaluated because its store is unavailable"))
					return
				}

				// Retry-After is in seconds per RFC 9110. Round up, so a client that
				// waits exactly the advertised time does not retry a moment early and
				// be refused again.
				seconds := int(decision.RetryAfter.Seconds())
				if decision.RetryAfter > time.Duration(seconds)*time.Second {
					seconds++
				}
				if seconds < 1 {
					seconds = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(seconds))
				WriteError(w, r, NewError(http.StatusTooManyRequests, CodeRateLimited, "too many requests"))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// writeRateLimitHeaders emits the RFC 9331 RateLimit fields.
//
// The draft field names are used alongside the long-standing X-RateLimit-* ones
// because clients exist for both and the cost of emitting both is a few bytes.
func writeRateLimitHeaders(w http.ResponseWriter, d ratelimit.Decision) {
	h := w.Header()
	limit := strconv.Itoa(d.Limit)
	remaining := strconv.Itoa(d.Remaining)
	h.Set("RateLimit-Limit", limit)
	h.Set("RateLimit-Remaining", remaining)
	h.Set("X-RateLimit-Limit", limit)
	h.Set("X-RateLimit-Remaining", remaining)
	if !d.Allowed && d.RetryAfter > 0 {
		reset := int(d.RetryAfter.Seconds())
		if reset < 1 {
			reset = 1
		}
		h.Set("RateLimit-Reset", strconv.Itoa(reset))
		h.Set("X-RateLimit-Reset", strconv.Itoa(reset))
	}
}

// ---------------------------------------------------------------------------
// Client address
// ---------------------------------------------------------------------------

// ClientIP extracts the client address for a request.
//
// Forwarding headers are only consulted when the direct peer is a trusted proxy.
//
// This is the part that is easy to get wrong. X-Forwarded-For is a header the
// client can set, so honouring it unconditionally means any caller can pick its
// own rate-limit bucket by inventing a new address on each request — the limit
// then exists in the code and nowhere else. It also lets a caller forge the
// address that lands in the auth and audit logs.
//
// The rule is therefore: trust the header only when the connection came from an
// address we were told to trust. With no proxies configured, which is the
// default, the peer address is always used and the header is ignored.
//
// The trust list is process configuration, so it is resolved once by
// ClientIPMiddleware and read back from the request context here. Calling this
// outside a request that went through that middleware — a synthetic test
// request, for instance — falls back to the peer address, which is the safe
// direction to be wrong in.
func ClientIP(r *http.Request) string {
	if ip, ok := r.Context().Value(clientIPCtxKey{}).(string); ok {
		return ip
	}
	return peerIP(r).String()
}

// ClientIPMiddleware resolves the client address once per request and stores it
// in the context, so the rate limiter, the auth log and the audit trail all
// agree on who the caller was. Deriving it separately in each place is how two
// of them end up disagreeing, and a rate limit keyed differently from the audit
// record is a limit an attacker can step around.
func ClientIPMiddleware(trusted TrustedProxies) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ClientIPFrom(r, trusted)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPCtxKey{}, ip)))
		})
	}
}

type clientIPCtxKey struct{}

// ClientIPFrom resolves the client address against an explicit trust set.
//
// trusted may be the zero value, which trusts nothing.
func ClientIPFrom(r *http.Request, trusted TrustedProxies) string {
	peer := peerIP(r)

	if trusted.Contains(peer) {
		if ip := forwardedClient(trusted, r); ip != "" {
			return ip
		}
	}
	return peer.String()
}

// forwardedClient returns the client address implied by the forwarding headers,
// or "" when they do not name one we can trust.
//
// The list is read right to left, skipping entries that are themselves trusted
// proxies, and the first address that is not trusted is the client.
//
// Reading it left to right and taking the first entry — the obvious approach —
// is wrong when a trusted proxy appends rather than replaces. A client that
// sends `X-Forwarded-For: 1.2.3.4` reaches a proxy that appends its real address,
// so the header becomes `1.2.3.4, <real client>`. The left-most entry is the one
// the client invented; taking it hands the attacker the bucket it wanted. The
// right-most untrusted entry is the address the last trusted hop actually
// observed, which is the one the client cannot choose.
//
// If every entry is trusted, the chain is entirely proxies and names no client,
// so "" is returned and the peer address is used instead.
func forwardedClient(trusted TrustedProxies, r *http.Request) string {
	var entries []string
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		for _, part := range strings.Split(fwd, ",") {
			entries = append(entries, strings.TrimSpace(part))
		}
	} else if rip := strings.TrimSpace(r.Header.Get("X-Real-Ip")); rip != "" {
		entries = append(entries, rip)
	}

	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry == "" {
			continue
		}
		// An entry that is not an address cannot be trusted or skipped, so the
		// whole header is treated as unusable. Returning the peer is the safe
		// direction: it attributes the request to something the client cannot
		// control, rather than to a value it wrote.
		ip := net.ParseIP(entry)
		if ip == nil {
			return ""
		}
		if trusted.TrustsAny() {
			// Every peer is trusted, so the proxy chain cannot be told apart from
			// the client. The last entry is the one the proxy closest to us
			// appended, which is the address it observed and the client did not
			// write, so it is the best available answer.
			return ip.String()
		}
		if !trusted.Contains(ip) {
			return ip.String()
		}
	}

	return ""
}

// peerIP returns the address of the connection, without any header input.
func peerIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr is not in host:port form. It may be a bare address, or it may
		// be absent entirely for a synthetic request. Parse what is there and fall
		// back to an unparseable sentinel rather than guessing.
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip
	}
	// A request that reached a handler without a usable RemoteAddr is not
	// attributable. Returning the unspecified address keeps every such request in
	// one bucket instead of scattering them, and it is not an address a client can
	// claim as its own peer address.
	return net.IPv4zero
}
