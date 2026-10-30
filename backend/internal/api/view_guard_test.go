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

// TestViewsRoutesRefuseWhenUnwired guards the wiring mistake that would otherwise
// surface as a nil-pointer panic: a route whose backing service was never copied
// into api.Deps must answer 503 naming the subsystem, not 500.
func TestViewsRoutesRefuseWhenUnwired(t *testing.T) {
	srv := &Server{deps: &Deps{
		Config: &config.Config{},
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}}
	r := chi.NewRouter()
	srv.mountViews(r)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/views"},
		{http.MethodPost, "/views"},
		{http.MethodGet, "/views/00000000-0000-0000-0000-000000000000"},
		{http.MethodPut, "/views/00000000-0000-0000-0000-000000000000"},
		{http.MethodDelete, "/views/00000000-0000-0000-0000-000000000000"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code,
					http.StatusServiceUnavailable, rec.Body.String())
			}
		})
	}
}
