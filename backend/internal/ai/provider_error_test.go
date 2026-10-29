package ai

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"

	"github.com/geff1778/forgecrm/internal/httpx"
)

// A provider that cannot be reached or does not answer in time is a transient
// condition, not a defect in the request. These tests pin the distinction, because
// getting it wrong is invisible: a caller that receives a 500 stops retrying, and
// a caller that receives a 503 knows to wait. The mapping is the difference
// between an assistant that recovers and one that looks permanently broken.

func TestUnreachableProviderBecomesRetryableUnavailable(t *testing.T) {
	// A dial error is a net.Error, which is what an unreachable host produces.
	dialErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	classified := classifyProviderError(context.Background(), dialErr)

	if !errors.Is(classified, ErrProviderUnavailable) {
		t.Fatalf("a dial error should classify as unavailable, got %v", classified)
	}

	mapped := HTTPErrorFrom(classified)
	var apiErr *httpx.APIError
	if !errors.As(mapped, &apiErr) {
		t.Fatalf("expected an APIError, got %T", mapped)
	}
	if apiErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", apiErr.Status)
	}
	if apiErr.Code != httpx.CodeUnavailable {
		t.Fatalf("code = %q, want %q", apiErr.Code, httpx.CodeUnavailable)
	}
}

func TestProviderTimeoutBecomesUnavailable(t *testing.T) {
	// The provider call runs under a context with the configured timeout; an
	// expired deadline surfaces as context.DeadlineExceeded.
	classified := classifyProviderError(context.Background(), context.DeadlineExceeded)
	if !errors.Is(classified, ErrProviderUnavailable) {
		t.Fatalf("a timeout should classify as unavailable, got %v", classified)
	}
}

func TestProviderApplicationErrorIsNotDisguisedAsRetryable(t *testing.T) {
	// A 4xx from the provider is the provider answering, not failing to answer.
	// Dressing it as "try again shortly" would send the caller into a retry loop
	// that cannot succeed.
	appErr := errors.New("the provider returned HTTP 400")
	classified := classifyProviderError(context.Background(), appErr)

	if errors.Is(classified, ErrProviderUnavailable) {
		t.Fatal("an application error must not be classified as a transient outage")
	}
	mapped := HTTPErrorFrom(classified)
	if mapped != appErr {
		t.Fatalf("an unclassified error should pass through unchanged, got %v", mapped)
	}
}

func TestDisabledAndRateLimitedKeepTheirOwnStatus(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   httpx.ErrorCode
	}{
		{"disabled", ErrDisabled, http.StatusServiceUnavailable, httpx.CodeDisabled},
		{"rate limited", ErrRateLimited, http.StatusTooManyRequests, httpx.CodeRateLimited},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var apiErr *httpx.APIError
			if !errors.As(HTTPErrorFrom(tc.err), &apiErr) {
				t.Fatalf("expected an APIError for %v", tc.err)
			}
			if apiErr.Status != tc.status || apiErr.Code != tc.code {
				t.Fatalf("got %d/%s, want %d/%s", apiErr.Status, apiErr.Code, tc.status, tc.code)
			}
		})
	}
}

// A wrapped error keeps its class through fmt.Errorf, which is how Complete
// returns it.
func TestClassificationSurvivesWrapping(t *testing.T) {
	wrapped := fmt.Errorf("model call failed: %w",
		classifyProviderError(context.Background(), context.DeadlineExceeded))
	if !errors.Is(wrapped, ErrProviderUnavailable) {
		t.Fatalf("the class must survive wrapping, got %v", wrapped)
	}
}
