//go:build integration

// Integration tests for the consent HTTP surface. They drive the real router, the
// real RBAC middleware and the real database, because what matters is that the
// enforcement and the "why" question are reachable and permission-checked at the
// boundary, not merely present in the domain package.
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// consentHarness is a tenant with a manager who can manage consent, a
// representative who can only read it, and an email operator who can reach the
// outbound email endpoint.
type consentHarness struct {
	*harness
	manager *actor
	reader  *actor
	// emailer holds email.manage, which the send endpoint requires. It is separate
	// from the manager because managing consent and operating the mail transport are
	// different jobs, and the email tests must not borrow the consent permission.
	emailer *actor
}

func newConsentHarness(t *testing.T) *consentHarness {
	t.Helper()
	h := newHarness(t)
	org := h.seedOrganization("Consent Co")

	managerID := h.seedUser("manager@consent.test", "correct-horse-battery", "Manager")
	h.joinOrganization(managerID, org, "sales_manager")

	readerID := h.seedUser("rep@consent.test", "correct-horse-battery", "Rep")
	h.joinOrganization(readerID, org, "sales_representative")

	emailerID := h.seedUser("emailer@consent.test", "correct-horse-battery", "Emailer")
	h.joinOrganization(emailerID, org, "tenant_admin")

	return &consentHarness{
		harness: h,
		manager: h.login("manager@consent.test", "correct-horse-battery"),
		reader:  h.login("rep@consent.test", "correct-horse-battery"),
		emailer: h.login("emailer@consent.test", "correct-horse-battery"),
	}
}

func (c *consentHarness) record(t *testing.T, a *actor, email, purpose, channel, state string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"subject_email": email, "purpose": purpose, "channel": channel, "state": state,
		"basis": "signup form",
	})
	return c.do(a, http.MethodPost, "/api/v1/consent", string(body))
}

// TestConsentRecordAndDecideOverHTTP drives the happy path: a decision is recorded,
// then the decision endpoint answers with the same answer a channel would act on.
func TestConsentRecordAndDecideOverHTTP(t *testing.T) {
	c := newConsentHarness(t)

	rec := c.record(t, c.manager, "person@consent.test", "marketing", "email", "granted")
	if rec.Code != http.StatusCreated {
		t.Fatalf("record: status %d, body %s", rec.Code, rec.Body.String())
	}

	dec := c.do(c.manager, http.MethodGet,
		"/api/v1/consent/decide?email=person@consent.test&purpose=marketing&channel=email", "")
	if dec.Code != http.StatusOK {
		t.Fatalf("decide: status %d, body %s", dec.Code, dec.Body.String())
	}
	var decision struct {
		Allowed bool   `json:"allowed"`
		Reason  string `json:"reason"`
		State   string `json:"state"`
	}
	if err := json.Unmarshal(dec.Body.Bytes(), &decision); err != nil {
		t.Fatalf("decode decision: %v (body %s)", err, dec.Body.String())
	}
	if !decision.Allowed || decision.State != "granted" {
		t.Fatalf("decision = %+v, want allowed granted", decision)
	}
	if decision.Reason == "" {
		t.Error("the decision carries no reason; the point of the endpoint is to explain the answer")
	}
}

// TestConsentDecideIsDenyByDefaultOverHTTP checks that the boundary reports the
// deny-by-default rule for marketing, not just the domain package.
func TestConsentDecideIsDenyByDefaultOverHTTP(t *testing.T) {
	c := newConsentHarness(t)

	dec := c.do(c.manager, http.MethodGet,
		"/api/v1/consent/decide?email=unknown@consent.test&purpose=marketing&channel=email", "")
	if dec.Code != http.StatusOK {
		t.Fatalf("decide: status %d, body %s", dec.Code, dec.Body.String())
	}
	var decision struct {
		Allowed bool   `json:"allowed"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(dec.Body.Bytes(), &decision); err != nil {
		t.Fatalf("decode decision: %v", err)
	}
	if decision.Allowed {
		t.Fatal("marketing with no record must be blocked at the HTTP boundary")
	}
	if decision.Reason == "" {
		t.Error("a blocked decision must carry a reason")
	}
}

// TestConsentManageIsSeparateFromRead is the permission check: a representative may
// read a decision but may not record one.
func TestConsentManageIsSeparateFromRead(t *testing.T) {
	c := newConsentHarness(t)

	// The representative can read.
	if got := c.do(c.reader, http.MethodGet,
		"/api/v1/consent/decide?email=x@consent.test&purpose=marketing&channel=email", "").Code; got != http.StatusOK {
		t.Fatalf("representative read: status %d, want 200", got)
	}

	// But not write.
	rec := c.record(t, c.reader, "x@consent.test", "marketing", "email", "granted")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("representative write: status %d, want 403, body %s", rec.Code, rec.Body.String())
	}
}

// TestConsentWithdrawStopsAnAllowedSend drives the withdrawal path over HTTP and
// confirms the decision flips.
func TestConsentWithdrawStopsAnAllowedSend(t *testing.T) {
	c := newConsentHarness(t)

	if rec := c.record(t, c.manager, "person@consent.test", "marketing", "email", "granted"); rec.Code != http.StatusCreated {
		t.Fatalf("record: status %d, body %s", rec.Code, rec.Body.String())
	}

	body, _ := json.Marshal(map[string]any{
		"email": "person@consent.test", "purpose": "marketing", "channel": "email",
		"reason": "unsubscribe link",
	})
	rec := c.do(c.manager, http.MethodPost, "/api/v1/consent/withdraw", string(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("withdraw: status %d, body %s", rec.Code, rec.Body.String())
	}

	dec := c.do(c.manager, http.MethodGet,
		"/api/v1/consent/decide?email=person@consent.test&purpose=marketing&channel=email", "")
	var decision struct {
		Allowed bool `json:"allowed"`
	}
	if err := json.Unmarshal(dec.Body.Bytes(), &decision); err != nil {
		t.Fatalf("decode decision: %v", err)
	}
	if decision.Allowed {
		t.Fatal("a withdrawn decision must block the send at the HTTP boundary")
	}
}

// TestConsentHistoryIsReachableOverHTTP checks the "why" question is answerable: the
// history endpoint returns the chain of decisions for a subject, purpose and channel.
func TestConsentHistoryIsReachableOverHTTP(t *testing.T) {
	c := newConsentHarness(t)

	if rec := c.record(t, c.manager, "person@consent.test", "marketing", "email", "granted"); rec.Code != http.StatusCreated {
		t.Fatalf("record: status %d, body %s", rec.Code, rec.Body.String())
	}
	body, _ := json.Marshal(map[string]any{
		"email": "person@consent.test", "purpose": "marketing", "channel": "email", "reason": "unsubscribe",
	})
	if rec := c.do(c.manager, http.MethodPost, "/api/v1/consent/withdraw", string(body)); rec.Code != http.StatusCreated {
		t.Fatalf("withdraw: status %d, body %s", rec.Code, rec.Body.String())
	}

	hist := c.do(c.manager, http.MethodGet,
		"/api/v1/consent/history?email=person@consent.test&purpose=marketing&channel=email", "")
	if hist.Code != http.StatusOK {
		t.Fatalf("history: status %d, body %s", hist.Code, hist.Body.String())
	}
	var out struct {
		Items []struct {
			State   string `json:"state"`
			Current bool   `json:"current"`
		} `json:"items"`
	}
	if err := json.Unmarshal(hist.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode history: %v (body %s)", err, hist.Body.String())
	}
	if len(out.Items) != 2 {
		t.Fatalf("history has %d items, want 2", len(out.Items))
	}
	current := 0
	for _, it := range out.Items {
		if it.Current {
			current++
		}
	}
	if current != 1 {
		t.Fatalf("history reports %d current decisions, want 1", current)
	}
}

// TestConsentSubjectMustBeUnambiguous checks the boundary refuses a request naming
// both a contact and an address, rather than silently preferring one.
func TestConsentSubjectMustBeUnambiguous(t *testing.T) {
	c := newConsentHarness(t)

	rec := c.do(c.manager, http.MethodGet,
		"/api/v1/consent/decide?email=x@consent.test&contact_id="+uuid.NewString()+
			"&purpose=marketing&channel=email", "")
	if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
		t.Fatalf("ambiguous subject: status %d, want a validation error, body %s", rec.Code, rec.Body.String())
	}
}

// TestConsentRecordRefusesAnUnknownPurpose checks a closed vocabulary is enforced at
// the boundary: a purpose the policy cannot look up is refused, not stored.
func TestConsentRecordRefusesAnUnknownPurpose(t *testing.T) {
	c := newConsentHarness(t)

	rec := c.record(t, c.manager, "person@consent.test", "advertising", "email", "granted")
	if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown purpose: status %d, want a validation error, body %s", rec.Code, rec.Body.String())
	}
}

// TestEmailSendRefusesOnConsentBeforeTransport is the enforcement test for the
// outbound email channel. A marketing send to an address with no consent must be
// refused on the policy, and the refusal must name consent rather than report the
// mail transport. The order matters: an answer of "SMTP is not configured" would
// tell an operator the send would go through once mail is set up, which is exactly
// the belief this gate exists to correct. No SMTP server is configured in the test
// environment, so a transport-first handler would answer 503 and fail here.
func TestEmailSendRefusesOnConsentBeforeTransport(t *testing.T) {
	c := newConsentHarness(t)

	body, _ := json.Marshal(map[string]any{
		"to": []string{"no-consent@consent.test"}, "subject": "Bulk mail",
		"body": "Should not be sent", "purpose": "marketing",
	})
	rec := c.do(c.emailer, http.MethodPost, "/api/v1/admin/email/send", string(body))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("blocked marketing send: status %d, want 403, body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "consent") {
		t.Fatalf("the refusal does not mention consent: %s", rec.Body.String())
	}
}

// TestEmailSendAllowsTransactionalWithoutConsent confirms the gate is specific: a
// transactional message is not blocked by a missing marketing record. It reaches the
// transport check, so the answer is the transport's, not a consent refusal.
func TestEmailSendAllowsTransactionalWithoutConsent(t *testing.T) {
	c := newConsentHarness(t)

	body, _ := json.Marshal(map[string]any{
		"to": []string{"receipt@consent.test"}, "subject": "Your receipt",
		"body": "Thanks", "purpose": "transactional",
	})
	rec := c.do(c.emailer, http.MethodPost, "/api/v1/admin/email/send", string(body))
	if rec.Code == http.StatusForbidden {
		t.Fatalf("a transactional send was blocked on consent: %s", rec.Body.String())
	}
}

// TestEmailSendChecksConsentForEveryRecipient is the multi-recipient bypass check.
//
// The request body accepts a list of recipients, but the handler once asked consent
// only about the first address. A caller could therefore put an address with a
// recorded grant first and a withdrawn address second: the first answered the
// consent question and the second received marketing mail nobody agreed to. The
// rule is that every recipient is a subject, so every one of them is checked.
func TestEmailSendChecksConsentForEveryRecipient(t *testing.T) {
	c := newConsentHarness(t)

	// The first address is granted marketing; the second is not on record at all.
	if rec := c.record(t, c.manager, "granted@consent.test", "marketing", "email", "granted"); rec.Code != http.StatusCreated {
		t.Fatalf("record granted: status %d, body %s", rec.Code, rec.Body.String())
	}

	body, _ := json.Marshal(map[string]any{
		"to":      []string{"granted@consent.test", "stranger@consent.test"},
		"subject": "Bulk mail",
		"body":    "One of these people never agreed to this",
		"purpose": "marketing",
	})
	rec := c.do(c.emailer, http.MethodPost, "/api/v1/admin/email/send", string(body))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a recipient without consent was not blocked: status %d, body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "consent") {
		t.Fatalf("the refusal does not mention consent: %s", rec.Body.String())
	}
}

// TestEmailSendRefusesUnknownPurpose checks that a purpose outside the closed set is
// refused rather than treated as transactional. Purpose is what selects the rule, so
// an unrecognised value must not fall through to the permissive one: a caller could
// otherwise name a marketing send "newsletter" and have it treated as a receipt.
func TestEmailSendRefusesUnknownPurpose(t *testing.T) {
	c := newConsentHarness(t)

	body, _ := json.Marshal(map[string]any{
		"to": []string{"stranger@consent.test"}, "subject": "Not a receipt",
		"body": "Marketed as something else", "purpose": "newsletter",
	})
	rec := c.do(c.emailer, http.MethodPost, "/api/v1/admin/email/send", string(body))
	if rec.Code == http.StatusOK {
		t.Fatalf("an unknown purpose was accepted: %s", rec.Body.String())
	}
	if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown purpose: status %d, want 422 or 400, body %s", rec.Code, rec.Body.String())
	}
}
