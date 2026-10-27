package integrations

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// newGuardedRequest builds a request with the supplied headers.
func newGuardedRequest(ctx context.Context, method, target string, headers map[string]string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

// probeTCP checks that a host and port accept a connection.
//
// The host is validated with the same rules as an outbound URL so that an
// integration cannot be pointed at an internal service.
func probeTCP(ctx context.Context, host string, port int, timeout time.Duration) error {
	if err := validateHostForDial(ctx, host); err != nil {
		return err
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		return fmt.Errorf("could not connect to %s:%d: %w", host, port, err)
	}
	return conn.Close()
}

// validateHostForDial applies the SSRF rules to a bare host.
func validateHostForDial(ctx context.Context, host string) error {
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("host is required")
	}
	_, err := ValidateOutboundURL(ctx, "https://"+host)
	return err
}
