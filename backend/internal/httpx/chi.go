package httpx

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// chiURLParam is isolated so the rest of the package does not depend on chi.
func chiURLParam(r *http.Request, key string) string {
	return chi.URLParam(r, key)
}
