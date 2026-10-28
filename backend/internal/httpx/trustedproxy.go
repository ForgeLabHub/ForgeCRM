package httpx

import (
	"fmt"
	"net"
	"strings"
)

// TrustedProxies is the set of addresses whose forwarding headers are believed.
type TrustedProxies struct {
	nets []*net.IPNet
	// any is true when the configuration trusts every peer. It exists so the
	// common case of "the only way in is a proxy I control" does not have to
	// enumerate 0.0.0.0/0 and ::/0 as two entries, but it is deliberately a
	// distinct value from "nothing configured": the two behave differently.
	any bool
}

// TrustsAny reports whether every peer is trusted.
func (t TrustedProxies) TrustsAny() bool { return t.any }

// Len reports how many ranges are configured, for diagnostics.
func (t TrustedProxies) Len() int {
	if t.any {
		return 1
	}
	return len(t.nets)
}

// TrustedProxiesFrom parses a comma-separated list of CIDR ranges or bare IPs.
//
// An empty list returns a set that trusts nothing, which is the safe default:
// the peer address is then used and forwarding headers are ignored. A deployment
// that puts a reverse proxy in front of the API must say so explicitly.
//
// A bare IP is accepted as a single-host range, because "trust 10.0.0.5" is what
// an operator means when the proxy has a fixed address, and requiring them to
// write 10.0.0.5/32 invites the mistake of writing 10.0.0.5/8.
func TrustedProxiesFrom(list string) (TrustedProxies, error) {
	list = strings.TrimSpace(list)
	if list == "" {
		return TrustedProxies{}, nil
	}

	var out TrustedProxies
	for _, raw := range strings.Split(list, ",") {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}

		if entry == "*" || entry == "0.0.0.0/0" || entry == "::/0" {
			// Trusting every peer means the header is trusted from anywhere, which
			// defeats the purpose of the check. It is allowed, because a deployment
			// where the API is unreachable except through a proxy is legitimate, but
			// it is recorded as the distinct value it is so a diagnostics page can
			// report it rather than hide it among ordinary ranges.
			out.any = true
			continue
		}

		if !strings.Contains(entry, "/") {
			ip := net.ParseIP(entry)
			if ip == nil {
				return TrustedProxies{}, fmt.Errorf("trusted proxy %q is not an IP address or CIDR range", entry)
			}
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			out.nets = append(out.nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}

		_, network, err := net.ParseCIDR(entry)
		if err != nil {
			return TrustedProxies{}, fmt.Errorf("trusted proxy %q is not a valid CIDR range: %w", entry, err)
		}
		out.nets = append(out.nets, network)
	}

	return out, nil
}

// Contains reports whether ip is in the trusted set.
func (t TrustedProxies) Contains(ip net.IP) bool {
	if t.any {
		return true
	}
	for _, n := range t.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
