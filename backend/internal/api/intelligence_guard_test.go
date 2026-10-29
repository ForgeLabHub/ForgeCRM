package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/geff1778/forgecrm/internal/config"
)

// TestIntelligenceRoutesRefuseWhenUnwired guards a real defect: the API binary
// constructed the server without copying the Forecast and Risk services into
// api.Deps, so every intelligence handler dereferenced a nil service and the
// recovery middleware turned the panic into an opaque 500. A route whose backing
// service is missing must answer 503 naming the subsystem, not panic.
//
// The routes are mounted on a bare router so the guard is exercised without the
// session layer, which needs a real auth dependency the other packages own.
func TestIntelligenceRoutesRefuseWhenUnwired(t *testing.T) {
	srv := &Server{deps: &Deps{
		Config: &config.Config{},
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}}
	r := chi.NewRouter()
	srv.mountIntelligence(r)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/forecast/catalog"},
		{http.MethodGet, "/forecast/runs"},
		{http.MethodPost, "/forecast/runs"},
		{http.MethodGet, "/risk/signals"},
		{http.MethodPost, "/risk/scan"},
		{http.MethodGet, "/risk/thresholds"},
		{http.MethodPut, "/risk/thresholds"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
			}
		})
	}
}
