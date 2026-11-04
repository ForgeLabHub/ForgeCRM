package api

import (
	"encoding/json"
	"net/http"
)

// maxCSPReportBytes caps a violation report. A browser sends a small JSON
// document per violation; the cap exists so a malicious or broken client cannot
// stream an unbounded body into the log. It is generous relative to a real report
// so a legitimate one is never truncated.
const maxCSPReportBytes = 64 << 10 // 64 KiB

// handleCSPReport accepts Content Security Policy violation reports.
//
// The endpoint exists because a policy that reports to nowhere is not a policy an
// operator can act on: the whole point of the Report-Only mode the middleware
// defaults to is that the browser tells the server what a policy *would* have
// blocked, so the policy can be tightened and eventually enforced against
// evidence. Without this sink the reports are dropped and the default can never
// be justified.
//
// It is deliberately anonymous and outside /api/v1: the browser posts here
// without a session (a violation on the sign-in page has no session) and without
// a CSRF token, and the body is only ever logged, never trusted. The response is
// 204 with no body — the browser ignores it — and every outcome is 204 so a
// probing client cannot learn from the status whether a body was accepted. The
// report is logged at warn, redacted by the logging handler, and bounded.
func (s *Server) handleCSPReport(w http.ResponseWriter, r *http.Request) {
	body, err := readLimitedBody(r, maxCSPReportBytes)
	if err != nil {
		// A body that is too large or unreadable is not the caller's business to
		// distinguish; the browser is told the report was accepted either way.
		s.deps.Log.Warn("CSP report rejected", "error", err)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Both the legacy `report-uri` shape ({"csp-report": {...}}) and the Reporting
	// API shape (an array of {"type":"csp-violation","body":{...}}) are accepted,
	// because which one a browser sends depends on its version. The body is not
	// validated against a schema: a report is diagnostic data, and refusing one
	// would hide the violation the operator needs to see.
	var raw json.RawMessage = body
	var legacy struct {
		CSPReport json.RawMessage `json:"csp-report"`
	}
	if err := json.Unmarshal(body, &legacy); err == nil && len(legacy.CSPReport) > 0 {
		raw = legacy.CSPReport
	}
	if !json.Valid(raw) {
		s.deps.Log.Warn("CSP report was not valid JSON", "bytes", len(body))
		w.WriteHeader(http.StatusNoContent)
		return
	}

	s.deps.Log.Warn("CSP violation reported", "report", string(raw), "user_agent", r.UserAgent())
	w.WriteHeader(http.StatusNoContent)
}
