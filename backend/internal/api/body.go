package api

import (
	"io"
	"net/http"

	"github.com/geff1778/forgecrm/internal/httpx"
)

// readLimitedBody reads a request body with a hard size cap.
//
// http.MaxBytesReader makes the server close the connection when the limit is
// exceeded, so a client cannot stream an unbounded body.
func readLimitedBody(r *http.Request, max int64) ([]byte, error) {
	limited := io.LimitReader(r.Body, max+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, httpx.NewError(400, httpx.CodeBadRequest, "could not read the request body")
	}
	if int64(len(body)) > max {
		return nil, httpx.NewError(413, httpx.CodePayloadTooLarge, "the request body is too large")
	}
	return body, nil
}
